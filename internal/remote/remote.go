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
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/hashicorp/yamux"

	"gorex/internal/askpass"
	"gorex/internal/openssh"
)

// Target is a machine to reach over ssh: Spec as the user gave it, and
// what ssh's configuration makes of it. Agent, "yes" or "no", has ssh
// forward the user's ssh agent to the host or not (ForwardAgent), over
// what ssh's configuration says, which "" leaves it to: the host's
// sessions use the agent while the window is connected. Opts are more
// options of every ssh to it, as -i, -p or -o, from the host's settings.
// Dir is the folder the server lands in there, ~/.cache/gorex when "".
type Target struct {
	Spec, User, Host, Port string
	Agent                  string
	Opts                   []string
	Dir                    string
}

// Key identifies the machine and account, however it was named.
func (t Target) Key() string { return t.User + "@" + t.Host + ":" + t.Port }

// Resolve asks ssh what spec, a destination as ssh takes it, stands for,
// with the more options opts.
func Resolve(spec string, opts []string) (Target, error) {
	if spec == "" || strings.HasPrefix(spec, "-") || strings.ContainsFunc(spec, unicode.IsSpace) {
		return Target{}, fmt.Errorf("not an ssh destination: %q", spec)
	}
	ssh, err := openssh.Path()
	if err != nil {
		return Target{}, err
	}
	cmd := exec.Command(ssh, slices.Concat([]string{"-G"}, opts, []string{"--", spec})...)
	openssh.Detach(cmd)
	out, err := cmd.CombinedOutput()
	t := Target{Spec: spec, Opts: opts}
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
// it is not. ssh asks the user through login, or, with none, nothing.
func Open(t Target, profile string, p Payload, prog Progress, login *askpass.Session) (*Link, error) {
	for try := 0; ; try++ {
		prog.Status("Connecting to " + t.Spec + "…")
		prog.Log("ssh " + t.Spec + " (" + t.Key() + "): starting GoRex's bridge")
		l, system, err := bridge(t, profile, p, prog.Log, login)
		if err != nil || l != nil {
			if l != nil {
				prog.Log("bridge ready: " + l.Exe())
			}
			return l, err
		}
		if try > 0 {
			// The bridge's script told the Connection Log why (host:
			// lines); trying again would only copy it again.
			return nil, &StopError{"The GoRex server copied to " + t.Spec + " does not start. The Connection Log shows why.", NoStart}
		}
		s, err := p.forSystem(system)
		if err != nil {
			return nil, &StopError{err.Error(), NoServer}
		}
		prog.Status("Copying GoRex to " + t.Spec + "…")
		prog.Log(fmt.Sprintf("%s runs %s; copying the %s server (%s)", t.Spec, system, s.name, s.id))
		if err := upload(t, s, prog.Log, login); err != nil {
			return nil, err
		}
		prog.Log("copied")
	}
}

// bridge runs the bridge of p on t: it returns the link, or, when p is not
// there yet, the remote system as uname names it.
func bridge(t Target, profile string, p Payload, logf func(string), login *askpass.Session) (*Link, string, error) {
	var opts []string
	if t.Agent != "" {
		opts = []string{"-o", "ForwardAgent=" + t.Agent}
	}
	cmd, err := sshCmd(t, p.bridgeScript(profile, t.Dir), logf, login, opts...)
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
	if err := cmd.start(); err != nil {
		return nil, "", err
	}
	// 30 seconds to answer, not counting what the user takes to answer
	// ssh's prompts.
	stop := cmd.limit(30 * time.Second)
	br := bufio.NewReaderSize(stdout, 64<<10)
	kind, value, err := readHandshake(br)
	if err != nil || kind == "deploy" || kind == "glibc" {
		stdin.Close()
		if err != nil {
			// Nothing reads its output any more: a host still printing
			// would block on it and never exit by itself.
			cmd.Process.Kill()
		}
		cmd.Wait()
		timedOut := stop()
		switch {
		case timedOut:
			return nil, "", fmt.Errorf("ssh %s: no answer in 30 seconds", t.Spec)
		case errors.Is(err, errNoisyHost):
			return nil, "", fmt.Errorf("ssh %s: %w", t.Spec, err)
		case err != nil:
			return nil, "", cmd.err()
		case kind == "glibc":
			has, needs, _ := strings.Cut(value, " ")
			logf("host: glibc is " + has + "; the server needs " + needs + " or later")
			return nil, "", &StopError{"GoRex cannot run on " + t.Spec + ". The host has glibc " + has + ", and the GoRex server needs " + needs + " or later.", OldGlibc}
		}
		return nil, value, nil
	}
	stop()
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

// upload copies the server s to t, where it lands in <folder>/<id>
// (serverDir of t.Dir) whole or not at all. A folder GoRex makes is the
// user's alone (0700); one that is another user's, or can't be made, is
// no place for it: the error is a StopError then, as another folder may
// do. Its files go as a gzipped tar archive made on the way, which the
// host's tar unpacks. A server there already is kept, as another window
// may run it, unless it can't be run, which the copy then replaces. It
// says on its error output, which the Connection Log shows, which it did,
// and the files' modes there.
func upload(t Target, s server, logf func(string), login *askpass.Session) error {
	dir := `"$c/` + s.id + `"`
	script := `c=` + serverDir(t.Dir) + `; ` +
		`if ! { ` + makeDir + ` && ` + mine + `; }; then echo "host: GoRex cannot keep a server in $c, because the folder cannot be created or belongs to another user" >&2; echo ` + noDirMarker + `; exit 1; fi; ` +
		`set -e; t=$(mktemp -d "$c/.upload.XXXXXX"); ` +
		`trap 'rm -rf "$t"' EXIT; tar -xzf - -C "$t"; chmod 755 "$t/gorex"; ` +
		`if [ -d ` + dir + ` ] && [ ! -x ` + dir + `/gorex ]; then rm -rf ` + dir + `; echo "host: replacing "` + dir + `", because its server cannot run" >&2; fi; ` +
		`if [ ! -d ` + dir + ` ]; then mv "$t" ` + dir + `; echo "host: copied to "` + dir + ` >&2; ` +
		`else echo "host: "` + dir + `" already exists; keeping it" >&2; fi; ` +
		`ls -l ` + dir + ` 2>&1 | sed 's/^/host: /' >&2; echo GOREX-DEPLOYED`
	cmd, err := sshCmd(t, script, logf, login)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(writeArchive(pw, s.files())) }()
	defer pr.Close()
	var out bytes.Buffer
	cmd.Stdin, cmd.Stdout = pr, &out
	err = cmd.start()
	if err == nil {
		err = cmd.Wait()
	}
	if strings.Contains(out.String(), noDirMarker) {
		return &StopError{"GoRex cannot keep its server in " + t.dirName() + " on " + t.Spec + ". The Connection Log shows why.", NoStart}
	}
	if err != nil || !strings.Contains(out.String(), "GOREX-DEPLOYED") {
		return cmd.err()
	}
	return nil
}

// noDirMarker is upload's answer when the server's folder can't be made,
// or is not the user's.
const noDirMarker = "GOREX-NODIR"

// dirName names the folder the server lands in on t, for messages.
func (t Target) dirName() string {
	if t.Dir == "" {
		return "~/.cache/gorex"
	}
	return t.Dir
}

// dirCandidates are the folders FindDir tries, as shell words, in order:
// the user's runtime folder, then folders of the user's own in the
// system's temporary ones. Some are emptied when the host restarts, which
// costs another copy of the server then.
var dirCandidates = []string{
	`"${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/gorex"`,
	`"/var/tmp/gorex-$(id -un)"`,
	`"/dev/shm/gorex-$(id -un)"`,
	`"/tmp/gorex-$(id -un)"`,
}

// FindDir finds a folder on t where programs run, for the server, when
// the one it lands in runs none (noexec): the first of dirCandidates that
// can be made the user's alone and runs a script copied there. It returns
// the folder as the host names it, or "" when none does.
func FindDir(t Target, logf func(string), login *askpass.Session) (string, error) {
	cmd, err := sshCmd(t, findDirScript(dirCandidates), logf, login)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.start(); err == nil {
		cmd.Wait()
	}
	if dir, ok := dirAnswer(out.String()); ok {
		return dir, nil
	}
	return "", cmd.err()
}

// findDirScript tries the folders cands, shell words, in turn, for
// FindDir: it answers with the first that is the user's, or can be made
// so, and runs a script copied there, else with none, and tells on its
// error output why each other one would not do.
func findDirScript(cands []string) string {
	return `for c in ` + strings.Join(cands, " ") + `; do ` +
		`if ! { ` + makeDir + ` && ` + mine + `; }; then echo "host: $c: the folder cannot be created or belongs to another user" >&2; continue; fi; ` +
		`p="$c/.probe.$$"; ` +
		`if printf '#!/bin/sh\nexit 0\n' > "$p" && chmod 755 "$p" && "$p"; then rm -f "$p"; echo "` + dirMarker + `$c"; exit 0; fi; ` +
		`rm -f "$p"; rmdir "$c" 2>/dev/null; echo "host: $c: programs cannot run in this folder" >&2; ` +
		`done; echo "` + dirMarker + `"`
}

// dirAnswer reads findDirScript's answer from its output: the folder, ""
// for none, and whether it answered.
func dirAnswer(out string) (string, bool) {
	for l := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(l, "\r"), dirMarker); ok {
			return v, true
		}
	}
	return "", false
}

// dirMarker begins findDirScript's answer, the folder found or nothing.
const dirMarker = "GOREX-DIR "

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
