package remote

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"gorex/internal/rex"
)

// sshPath finds OpenSSH's ssh.
func sshPath() (string, error) {
	if p, err := exec.LookPath("ssh"); err == nil {
		return p, nil
	}
	if p := systemSSH(); p != "" {
		return p, nil
	}
	return "", errors.New("GoRex needs OpenSSH's ssh: install the OpenSSH Client")
}

// shQuote quotes s for a POSIX shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sshProc is an ssh running a script on a target, whose error output it
// keeps the end of.
type sshProc struct {
	*exec.Cmd
	spec   string
	stderr *tail
}

// sshCmd makes an ssh that runs script with sh on t: never asking for a
// password, which the app has no way to, and giving up on a link that
// stopped answering. Each line ssh prints goes to logf.
func sshCmd(t Target, script string, logf func(string)) (*sshProc, error) {
	ssh, err := sshPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(ssh, "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
		"--", t.Spec, "sh -c "+shQuote(script))
	hide(cmd)
	p := &sshProc{Cmd: cmd, spec: t.Spec, stderr: &tail{logf: logf}}
	cmd.Stderr = p.stderr
	return p, nil
}

// err describes why ssh failed, from what it printed last.
func (p *sshProc) err() error {
	msg := p.stderr.last()
	if msg == "" {
		msg = "the connection closed"
	}
	if strings.Contains(p.stderr.String(), "Host key verification failed") {
		msg += " (run ssh " + p.spec + " once in a terminal to accept its host key)"
	}
	return fmt.Errorf("ssh %s: %s", p.spec, msg)
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
