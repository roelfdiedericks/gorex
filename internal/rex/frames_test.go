//go:build darwin || linux || windows

package rex

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// TestFrames: a window's terminal, fed the server's frames, shows what the
// session's program printed, and takes the size another window gives the
// session, then the size the window in use claims back, before the frames
// drawn at it.
func TestFrames(t *testing.T) {
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
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(dir, "server.sock")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	c, err := Connect(LocalDialer())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()
	info, err := c.Create(CreateOptions{Command: []string{shell}, Dir: work, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Kill(info.ID)

	a := c.Stream(info.ID, 80, 24)
	defer a.Close()
	term, err := terminal.New(terminal.Options{Conn: a, ResizeByConn: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Resize(80, 24)
	a.OnSize(term.Resize)
	a.ResizePixels(80, 24, 8, 16)
	a.Write([]byte(typed))
	waitFor(t, "the output", func() bool { return strings.Contains(term.Text(), "hello") })

	b := c.Stream(info.ID, 100, 30)
	defer b.Close()
	go io.Copy(io.Discard, b)
	b.ResizePixels(100, 30, 8, 16)
	waitFor(t, "the other window's size", func() bool {
		cols, rows := term.Size()
		return cols == 100 && rows == 30
	})
	a.Claim()
	waitFor(t, "the size claimed back", func() bool {
		cols, rows := term.Size()
		return cols == 80 && rows == 24
	})
	if !strings.Contains(term.Text(), "hello") {
		t.Errorf("after the resizes, the window shows %q", term.Text())
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("no %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
