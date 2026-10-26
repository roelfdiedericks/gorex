//go:build darwin || linux

package rex

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTSPEveryWindow: every window on a session gets its surfaces, as
// they are when it attaches and after each change, not only the first
// window to draw a frame after the change.
func TestTSPEveryWindow(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rex")
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
	// The program opens a surface and draws it, and a second later draws
	// it again (printf's escapes: a backslash doubled, Escape as \033).
	sh := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "\x1b", `\033`) }
	script := "printf '" + sh(tspOpen+tspFrame1) + "'; sleep 1; printf '" + sh(tspFrame2) + "'; sleep 5"
	info, err := c.Create(CreateOptions{Command: []string{"/bin/sh", "-c", script}, Dir: "/tmp", Cols: 80, Rows: 24, TSP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Kill(info.ID)

	var mu sync.Mutex
	got := map[string]string{}
	watch := func(name string, s *Stream) {
		s.OnSurfaces(func(sfs []Surface) {
			mu.Lock()
			defer mu.Unlock()
			got[name] = ""
			if len(sfs) == 1 && sfs[0].Root != nil {
				var walk func(n *TSPNode)
				walk = func(n *TSPNode) {
					if n.ID == "main.card.row.count" {
						got[name], _ = n.Props["text"].(string)
					}
					for _, ch := range n.Children {
						walk(ch)
					}
				}
				walk(sfs[0].Root)
			}
		})
		go io.Copy(io.Discard, s)
	}
	has := func(name, want string) func() bool {
		return func() bool {
			mu.Lock()
			defer mu.Unlock()
			return got[name] == want
		}
	}
	a := c.Stream(info.ID, 80, 24)
	defer a.Close()
	watch("a", a)
	b := c.Stream(info.ID, 80, 24)
	defer b.Close()
	watch("b", b)
	t.Cleanup(func() {
		if t.Failed() {
			mu.Lock()
			t.Logf("the windows have %q", got)
			mu.Unlock()
		}
	})
	waitFor(t, "the first window's surface", has("a", "clicked 0 times, tick 0"))
	waitFor(t, "the second window's surface", has("b", "clicked 0 times, tick 0"))
	// The program draws again a moment later.
	waitFor(t, "the change in the first window", has("a", "clicked 0 times, tick 1"))
	waitFor(t, "the change in the second window", has("b", "clicked 0 times, tick 1"))
}
