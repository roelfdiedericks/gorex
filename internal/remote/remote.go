// Package remote connects GoRex to a session server on another machine
// over ssh: it copies the server for that machine's system there when it
// is missing, starts it with -bridge, and carries every connection to it
// over the one ssh, multiplexed.
package remote

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/hashicorp/yamux"
)

// Target is a machine to reach over ssh: Spec as the user gave it, and
// what ssh's configuration makes of it.
type Target struct{ Spec, User, Host, Port string }

// Key identifies the machine and account, however it was named.
func (t Target) Key() string { return t.User + "@" + t.Host + ":" + t.Port }

// Resolve asks ssh what spec, a destination as ssh takes it, stands for.
func Resolve(spec string) (Target, error) {
	if spec == "" || strings.HasPrefix(spec, "-") || strings.ContainsFunc(spec, unicode.IsSpace) {
		return Target{}, fmt.Errorf("not an ssh destination: %q", spec)
	}
	ssh, err := sshPath()
	if err != nil {
		return Target{}, err
	}
	cmd := exec.Command(ssh, "-G", "--", spec)
	hide(cmd)
	out, err := cmd.CombinedOutput()
	t := Target{Spec: spec}
	for l := range strings.SplitSeq(string(out), "\n") {
		l = strings.TrimRight(l, "\r")
		if v, ok := strings.CutPrefix(l, "user "); ok {
			t.User = v
		} else if v, ok := strings.CutPrefix(l, "hostname "); ok {
			t.Host = v
		} else if v, ok := strings.CutPrefix(l, "port "); ok {
			t.Port = v
		}
	}
	if t.User == "" || t.Host == "" || t.Port == "" {
		msg := strings.TrimSpace(string(out))
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return Target{}, fmt.Errorf("ssh -G %s: %s", spec, msg)
	}
	return t, nil
}

// Link is a running bridge on a remote machine, over ssh.
type Link struct {
	sess *yamux.Session
	exe  string
}

// Dial opens a connection to the remote server.
func (l *Link) Dial() (net.Conn, error) { return l.sess.Open() }

// Exe returns the path of the remote server's executable.
func (l *Link) Exe() string { return l.exe }

// Close takes the link down; the remote server and its sessions go on.
func (l *Link) Close() { l.sess.Close() }

// Progress tells the user how connecting goes: Status is a short line of
// what is happening now, and Log each line of detail, as what ssh prints.
type Progress struct {
	Status func(string)
	Log    func(string)
}

// Open connects to the server of profile on t, copying p there first when
// it is not.
func Open(t Target, profile string, p Payload, prog Progress) (*Link, error) {
	for try := 0; ; try++ {
		prog.Status("Connecting to " + t.Spec + "…")
		prog.Log("ssh " + t.Spec + " (" + t.Key() + "): starting GoRex's bridge")
		l, system, err := bridge(t, profile, p, prog.Log)
		if err != nil || l != nil {
			if l != nil {
				prog.Log("bridge ready: " + l.Exe())
			}
			return l, err
		}
		if try > 0 {
			return nil, fmt.Errorf("the GoRex copied to %s does not start", t.Spec)
		}
		s, err := p.forSystem(system)
		if err != nil {
			return nil, err
		}
		prog.Status("Copying GoRex to " + t.Spec + "…")
		prog.Log(fmt.Sprintf("%s is %s: copying the %s server (%s)", t.Spec, system, s.name, s.id))
		if err := upload(t, s, prog.Log); err != nil {
			return nil, err
		}
		prog.Log("copied")
	}
}

// bridge runs the bridge of p on t: it returns the link, or, when p is not
// there yet, the remote system as uname names it.
func bridge(t Target, profile string, p Payload, logf func(string)) (*Link, string, error) {
	cmd, err := sshCmd(t, p.bridgeScript(profile), logf)
	if err != nil {
		return nil, "", err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	var timedOut atomic.Bool
	timer := time.AfterFunc(30*time.Second, func() {
		timedOut.Store(true)
		cmd.Process.Kill()
	})
	br := bufio.NewReaderSize(stdout, 64<<10)
	kind, value, err := readHandshake(br)
	if err != nil || kind == "deploy" {
		stdin.Close()
		if err != nil {
			// Nothing reads its output any more: a host still printing
			// would block on it and never exit by itself.
			cmd.Process.Kill()
		}
		cmd.Wait()
		timer.Stop()
		switch {
		case timedOut.Load():
			return nil, "", fmt.Errorf("ssh %s: no answer in 30 seconds", t.Spec)
		case errors.Is(err, errNoisyHost):
			return nil, "", fmt.Errorf("ssh %s: %w", t.Spec, err)
		case err != nil:
			return nil, "", cmd.err()
		}
		return nil, value, nil
	}
	timer.Stop()
	conn := rwc{Reader: br, Writer: stdin, close: func() { cmd.Process.Kill() }}
	cfg := yamux.DefaultConfig()
	// Pings every few seconds tell a link that stopped answering within
	// the ping's timeout after, so that calls fail then, not later.
	cfg.KeepAliveInterval = 5 * time.Second
	cfg.LogOutput = io.Discard
	sess, err := yamux.Client(conn, cfg)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, "", err
	}
	// The session ends when ssh does, as it reads the end of its output.
	go cmd.Wait()
	return &Link{sess: sess, exe: value}, "", nil
}

// upload copies the server s to t, where it lands in ~/.cache/gorex/<id>
// whole or not at all. Its files go as a gzipped tar archive made on the
// way, which the host's tar unpacks.
func upload(t Target, s server, logf func(string)) error {
	script := `set -e; c="$HOME/.cache/gorex"; mkdir -p "$c"; t=$(mktemp -d "$c/.upload.XXXXXX"); ` +
		`trap 'rm -rf "$t"' EXIT; tar -xzf - -C "$t"; chmod 755 "$t/gorex"; ` +
		`if [ ! -d "$c/` + s.id + `" ]; then mv "$t" "$c/` + s.id + `"; fi; echo GOREX-DEPLOYED`
	cmd, err := sshCmd(t, script, logf)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(writeArchive(pw, s.files())) }()
	defer pr.Close()
	var out bytes.Buffer
	cmd.Stdin, cmd.Stdout = pr, &out
	if err := cmd.Run(); err != nil || !strings.Contains(out.String(), "GOREX-DEPLOYED") {
		return cmd.err()
	}
	return nil
}

// writeArchive writes the files to w as a gzipped tar archive, by their
// base names.
func writeArchive(w io.Writer, paths []string) error {
	zw := gzip.NewWriter(w)
	tw := tar.NewWriter(zw)
	for _, path := range paths {
		if err := addFile(tw, path); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

func addFile(tw *tar.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: filepath.Base(path), Mode: 0o644, Size: st.Size(), ModTime: st.ModTime(), Format: tar.FormatUSTAR}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// rwc makes ssh's output and input one connection for yamux.
type rwc struct {
	io.Reader
	io.Writer
	close func()
}

func (c rwc) Close() error {
	c.close()
	return nil
}
