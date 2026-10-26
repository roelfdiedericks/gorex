package remote

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"gorex/internal/askpass"
	"gorex/internal/openssh"
	"gorex/internal/rex"
)

// shQuote quotes s for a POSIX shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sshProc is an ssh running a script on a target, whose error output it
// keeps the end of.
type sshProc struct {
	*exec.Cmd
	t      Target
	stderr *tail
	login  *askpass.Session
}

// sshCmd makes an ssh that runs script with sh on t, with openssh.Options.
// Each line ssh prints goes to logf. It asks the user through login, or,
// with none, nothing.
func sshCmd(t Target, script string, logf func(string), login *askpass.Session) (*sshProc, error) {
	ssh, err := openssh.Path()
	if err != nil {
		return nil, err
	}
	args := slices.Clone(openssh.Options)
	var env []string
	if login == nil {
		args = append(args, openssh.Batch...)
	} else {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		env = append(os.Environ(), login.Env(exe)...)
	}
	cmd := exec.Command(ssh, append(args, "--", t.Spec, "sh -c "+shQuote(script))...)
	cmd.Env = env
	openssh.Detach(cmd)
	p := &sshProc{Cmd: cmd, t: t, stderr: &tail{logf: logf}, login: login}
	cmd.Stderr = p.stderr
	return p, nil
}

// start starts ssh, which stops when the user cancels a prompt of its
// login.
func (p *sshProc) start() error {
	if err := p.Start(); err != nil {
		return err
	}
	if p.login != nil {
		go func() {
			select {
			case <-p.login.Done():
				p.Process.Kill()
			case <-p.login.Ended():
			}
		}()
	}
	return nil
}

// limit kills p once it has run d, not counting the time its login waits
// for the user, unless the returned stop is called first, which reports
// whether it was killed.
func (p *sshProc) limit(d time.Duration) (stop func() bool) {
	done := make(chan struct{})
	killed := make(chan bool, 1)
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		last := time.Now()
		for {
			select {
			case <-done:
				killed <- false
				return
			case now := <-tick.C:
				if p.login == nil || !p.login.Asking() {
					d -= now.Sub(last)
				}
				last = now
				if d <= 0 {
					p.Process.Kill()
					<-done
					killed <- true
					return
				}
			}
		}
	}()
	return func() bool {
		close(done)
		return <-killed
	}
}

// LoginError is why logging in to a host failed where trying again by
// itself won't help: the user cancelled a prompt, refused a host's key,
// or gave a password ssh stopped asking for, or the host's key changed.
type LoginError struct{ Reason string }

func (e *LoginError) Error() string { return e.Reason }

// err describes why ssh failed, from what it printed last and what the
// user did with its prompts.
func (p *sshProc) err() error {
	msg := p.stderr.last()
	if msg == "" {
		msg = "the connection closed"
	}
	out := p.stderr.String()
	if p.login != nil {
		asked, cancelled, refused := p.login.Outcome()
		switch {
		case cancelled:
			return &LoginError{"Login to " + p.t.Spec + " cancelled"}
		case refused:
			return &LoginError{"Not connected: the key of " + p.t.Spec + " was not trusted"}
		case asked && strings.Contains(out, "Permission denied"):
			return &LoginError{"Login to " + p.t.Spec + " failed: " + msg}
		}
	}
	if strings.Contains(out, "REMOTE HOST IDENTIFICATION HAS CHANGED") {
		host := p.t.Host
		if p.t.Port != "" && p.t.Port != "22" {
			host = "[" + host + "]:" + p.t.Port
		}
		return &LoginError{"The key of " + p.t.Spec + " has changed since ssh last saw it, which ssh refuses. If the host was reinstalled, remove its old key with: ssh-keygen -R \"" + host + "\", then Retry."}
	}
	if p.login == nil && strings.Contains(out, "Host key verification failed") {
		msg += " (run ssh " + p.t.Spec + " once in a terminal to accept its host key)"
	}
	return fmt.Errorf("ssh %s: %s", p.t.Spec, msg)
}

// tail keeps the last 4 KiB written to it, and passes each line to logf.
type tail struct {
	logf    func(string)
	mu      sync.Mutex
	buf     []byte
	partial []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4<<10 {
		t.buf = t.buf[len(t.buf)-4<<10:]
	}
	t.partial = append(t.partial, p...)
	for {
		i := bytes.IndexByte(t.partial, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimRight(string(t.partial[:i]), "\r"); line != "" && t.logf != nil {
			t.logf("ssh: " + line)
		}
		t.partial = t.partial[i+1:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// last returns the last line that is not empty.
func (t *tail) last() string {
	lines := strings.Split(t.String(), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

const deployMarker = "GOREX-DEPLOY "

// handshakeLines is how many lines of a host's login noise readHandshake
// reads past, before giving up on it.
const handshakeLines = 200

// errNoisyHost tells a host whose login printed handshakeLines lines with
// no answer from GoRex among them, and may go on printing.
var errNoisyHost = fmt.Errorf("no answer from GoRex among the first %d lines the host printed", handshakeLines)

// readHandshake reads the first line of the remote script's own: a
// bridge's ready line, or the request of a deployment with the system
// that needs it. It skips what the shell's startup files print.
func readHandshake(r *bufio.Reader) (kind, value string, err error) {
	for range handshakeLines {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if v, ok := strings.CutPrefix(line, rex.BridgeReady); ok {
			return "bridge", v, nil
		}
		if v, ok := strings.CutPrefix(line, deployMarker); ok {
			return "deploy", v, nil
		}
		if err != nil {
			if err == io.EOF {
				return "", "", errors.New("no answer from GoRex")
			}
			return "", "", err
		}
	}
	return "", "", errNoisyHost
}
