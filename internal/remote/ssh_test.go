package remote

import (
	"errors"
	"strings"
	"testing"
)

// A host whose key changed fails with a StopError, which the window
// does not retry, naming the known_hosts entry to remove: the host, or
// [host]:port off port 22. Other failures are plain errors, retried.
func TestStopError(t *testing.T) {
	changed := "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n" +
		"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n" +
		"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n" +
		"Host key verification failed.\n"
	for _, c := range []struct {
		port, stderr, want string
		stop               bool
	}{
		{"22", changed, `ssh-keygen -R "box"`, true},
		{"2222", changed, `ssh-keygen -R "[box]:2222"`, true},
		{"22", "ssh: connect to host box port 22: Connection timed out\n", "Connection timed out", false},
	} {
		p := &sshProc{t: Target{Spec: "me@box", Host: "box", Port: c.port}, stderr: &tail{logf: func(string) {}}}
		p.stderr.Write([]byte(c.stderr))
		err := p.err()
		var se *StopError
		if errors.As(err, &se) != c.stop || !strings.Contains(err.Error(), c.want) {
			t.Errorf("port %s, %q: %v (stops %v)", c.port, c.stderr, err, errors.As(err, &se))
		}
	}
}
