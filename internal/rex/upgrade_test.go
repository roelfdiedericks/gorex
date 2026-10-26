//go:build darwin || linux

package rex

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary be the server too, as the app is:
// TestUpgrade runs it with -server, and the upgrade execs it again.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-server" {
		var err error
		if len(os.Args) > 3 && os.Args[2] == "-resume" {
			err = Resume(os.Args[3])
		} else {
			err = Serve()
		}
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestUpgrade upgrades a server in place: its sessions go on in the same
// process, their screens as they were, and what a program prints while
// the server replaces itself reaches the new one.
func TestUpgrade(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rex")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("GOREX_DIR", dir)
	exe, _ := Executable()
	srv := exec.Command(exe, "-server")
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Process.Kill()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(dir, "server.sock")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	dial := func() (net.Conn, error) { return net.Dial("unix", SocketPath()) }
	c, err := Connect(dial)
	if err != nil {
		t.Fatal(err)
	}
	h, err := c.Hello()
	if err != nil {
		t.Fatal(err)
	}
	shell, err := c.Create(CreateOptions{Command: []string{"/bin/sh"}, Dir: "/tmp", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	s := c.Stream(shell.ID, 80, 24)
	s.Write([]byte("echo he''llo\n"))
	readUntil(t, s, "hello")
	s.Close()
	// A program printing through the upgrade: a line every 10ms, 2s long.
	const lines = 200
	counter, err := c.Create(CreateOptions{
		Command: []string{"/bin/sh", "-c", fmt.Sprintf("i=0; while [ $i -lt %d ]; do echo line$i; i=$((i+1)); sleep 0.01; done; sleep 30", lines)},
		Dir:     "/tmp", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	nc, err := c.Upgrade(h, exe)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Shutdown()
	nh, err := nc.Hello()
	if err != nil || nh.PID != h.PID || !nh.Started.After(h.Started) {
		t.Fatalf("hello after the upgrade %+v %v; before %+v", nh, err, h)
	}
	list, err := nc.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("sessions after the upgrade %+v %v", list, err)
	}
	for i, want := range []SessionInfo{shell, counter} {
		if got := list[i]; got.ID != want.ID || got.PID != want.PID || got.Exited {
			t.Errorf("session %d after the upgrade %+v, was %+v", i, got, want)
		}
	}
	// The shell's screen is as it was, and it still runs what is typed.
	s = nc.Stream(shell.ID, 80, 24)
	readUntil(t, s, "hello")
	s.Write([]byte("echo wo''rld\n"))
	readUntil(t, s, "world")
	s.Close()
	// Every line the counter printed is there, those printed while the
	// server replaced itself too.
	time.Sleep(2500 * time.Millisecond)
	s = nc.Stream(counter.ID, 80, 24)
	out := readUntil(t, s, fmt.Sprintf("line%d", lines-1))
	s.Close()
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`line(\d+)`).FindAllStringSubmatch(out, -1) {
		seen[m[1]] = true
	}
	for i := range lines {
		if !seen[fmt.Sprint(i)] {
			at := strings.Index(out, fmt.Sprintf("line%d", i-1))
			t.Errorf("line%d is missing after the upgrade: %q", i, out[max(at, 0):min(max(at, 0)+80, len(out))])
		}
	}
}

// readUntil reads the stream until what it read has want, and returns it.
func readUntil(t *testing.T, s *Stream, want string) string {
	t.Helper()
	var out bytes.Buffer
	buf := make([]byte, 64<<10)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) && time.Now().Before(deadline) {
		n, err := s.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(out.String(), want) {
		t.Fatalf("no %q in %q", want, out.String())
	}
	return out.String()
}

// A state from a server that kept the scrollback in bytes, 256 a line,
// comes back in lines: the number the session asked for, the default
// for the 8 MiB it had without one.
func TestKeptLines(t *testing.T) {
	for _, c := range []struct{ keep, want int }{{2_560_000, 10000}, {500 * 256, 500}, {8 << 20, ScrollbackLines}, {0, ScrollbackLines}} {
		if got := keptLines(c.keep); got != c.want {
			t.Errorf("keptLines(%d) = %d, want %d", c.keep, got, c.want)
		}
	}
}
