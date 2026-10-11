//go:build darwin || linux || windows

package rex

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
)

// TestBridge talks to a server through a bridge, as a GoRex at the other
// end of ssh does.
func TestBridge(t *testing.T) {
	base, shell, work := "/tmp", "/bin/sh", "/tmp"
	typed := "echo he''llo\n"
	if runtime.GOOS == "windows" {
		base, shell, work = "", "cmd.exe", os.TempDir()
		typed = "echo he^llo\r"
	}
	dir, err := os.MkdirTemp(base, "rex")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("GOREX_DIR", dir)
	go Serve()
	for range 100 {
		if _, err := os.Stat(filepath.Join(dir, "server.sock")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	bridged := make(chan error, 1)
	go func() { bridged <- Bridge(c2sR, s2cW) }()
	br := bufio.NewReader(s2cR)
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, BridgeReady) {
		t.Fatalf("ready line %q %v", line, err)
	}
	sess, err := yamux.Client(rwc{br, c2sW}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Connect(func() (net.Conn, error) { return sess.Open() })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		lc, err := Connect(LocalDialer())
		if err == nil {
			lc.Shutdown()
		}
	}()
	if h, err := c.Hello(); err != nil || h.Version != ProtocolVersion || h.Host.GOOS != runtime.GOOS {
		t.Fatalf("hello %+v %v", h, err)
	}
	info, err := c.Create(CreateOptions{Command: []string{shell}, Dir: work, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	s := c.Stream(info.ID, 80, 24)
	s.Resize(80, 24)
	s.Write([]byte(typed))
	var out bytes.Buffer
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "hello") && time.Now().Before(deadline) {
		n, err := s.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("output %q", out.String())
	}
	sess.Close()
	select {
	case err := <-bridged:
		if err != nil {
			t.Fatalf("bridge: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge goes on without its client")
	}
}
