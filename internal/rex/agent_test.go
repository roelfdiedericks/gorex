//go:build darwin || linux || windows

package rex

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeAgent listens at a socket and answers every connection with name.
func fakeAgent(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(os.Getenv("GOREX_DIR"), name+".agent")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte(name))
			c.Close()
		}
	}()
	return path
}

// The sessions' agent goes to the agent of the latest bridge connected,
// to the one before once that one is gone, and to none once none is: a
// connection then closes, as with no agent. New sessions are told it.
func TestAgentRelay(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	startTestServer(t)
	a, b := fakeAgent(t, "first"), fakeAgent(t, "second")
	ask := func() string {
		t.Helper()
		c, err := net.DialTimeout("unix", AgentPath(), time.Second)
		if err != nil {
			t.Fatalf("dialing the relay: %v", err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(2 * time.Second))
		got, _ := io.ReadAll(c)
		return string(got)
	}
	// until asks the relay until it answers want, for a moment: a bridge
	// that goes takes its agent away once the server sees it go.
	until := func(want string) {
		t.Helper()
		got := ""
		for range 50 {
			if got = ask(); got == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Errorf("the relay answers %q, want %q", got, want)
	}

	doneA, doneB := make(chan struct{}), make(chan struct{})
	offerAgent(a, LocalDialer(), doneA)
	if got := os.Getenv("SSH_AUTH_SOCK"); got != AgentPath() {
		t.Errorf("sessions are told SSH_AUTH_SOCK=%q, want %q", got, AgentPath())
	}
	until("first")
	offerAgent(b, LocalDialer(), doneB)
	until("second")
	close(doneB)
	until("first")
	close(doneA)
	until("")
}
