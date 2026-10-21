package rex

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startTestServer serves from a temporary directory and connects to it.
func startTestServer(t *testing.T) *Client {
	t.Helper()
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = ""
	}
	dir, err := os.MkdirTemp(base, "rex")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
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
	t.Cleanup(func() { c.Shutdown(); time.Sleep(100 * time.Millisecond) })
	return c
}

// waitEvent takes the next event of a kind, within a moment.
func waitEvent(t *testing.T, c *Client, event, path string) bool {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.Events:
			if ev.Event == event && (path == "" || ev.Path == path) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// TestWatchEvents: a file written in a watched directory tells every
// connection once, however many files change; unwatching stops it.
func TestWatchEvents(t *testing.T) {
	c := startTestServer(t)
	c2, err := Connect(LocalDialer())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.Hello(); err != nil { // a control connection is known once it speaks
		t.Fatal(err)
	}
	dir := filepath.Join(os.Getenv("GOREX_DIR"), "watched")
	os.MkdirAll(dir, 0o755)
	if err := c.Watch(dir); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		os.WriteFile(filepath.Join(dir, "f"+string(rune('a'+i))), []byte("x"), 0o644)
	}
	if !waitEvent(t, c, "changed", dir) {
		t.Fatal("the watcher did not tell of the files")
	}
	if !waitEvent(t, c2, "changed", dir) {
		t.Fatal("the other connection was not told")
	}
	// One event for the burst: none follows within a moment.
	select {
	case ev := <-c.Events:
		if ev.Event == "changed" {
			t.Errorf("a second changed event for one burst: %+v", ev)
		}
	case <-time.After(400 * time.Millisecond):
	}
	if err := c.Unwatch(dir); err != nil {
		t.Fatal(err)
	}
	c2.Unwatch(dir)
	os.WriteFile(filepath.Join(dir, "later"), []byte("x"), 0o644)
	select {
	case ev := <-c.Events:
		if ev.Event == "changed" {
			t.Errorf("told after unwatching: %+v", ev)
		}
	case <-time.After(400 * time.Millisecond):
	}
	if err := c.Watch(filepath.Join(dir, "missing")); err == nil {
		t.Error("watching a missing directory succeeded")
	}
}

// TestSessionEvents: creating and killing a session tells the
// connections.
func TestSessionEvents(t *testing.T) {
	c := startTestServer(t)
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}
	info, err := c.Create(CreateOptions{Command: []string{shell}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if !waitEvent(t, c, "sessions", "") {
		t.Fatal("no sessions event on create")
	}
	for len(c.Events) > 0 {
		<-c.Events
	}
	// A program starting in the session is told too, from the server's
	// own comparison.
	typed, program := "sleep 2\n", "sleep"
	if runtime.GOOS == "windows" {
		typed, program = "ping -n 3 127.0.0.1 >nul\r", "PING"
	}
	s := c.Stream(info.ID, 80, 24)
	s.ResizePixels(80, 24, 0, 0)
	s.Write([]byte(typed))
	told := false
	deadline := time.Now().Add(4 * time.Second)
	for !told && time.Now().Before(deadline) {
		if !waitEvent(t, c, "sessions", "") {
			break
		}
		list, _ := c.List()
		told = len(list) == 1 && strings.EqualFold(list[0].Program, program)
	}
	if !told {
		list, _ := c.List()
		t.Errorf("no sessions event as %s started: %+v", program, list)
	}
	s.Close()
	c.Kill(info.ID)
	if !waitEvent(t, c, "sessions", "") {
		t.Fatal("no sessions event on kill")
	}
}

// TestParsePorcelain reads git's porcelain v2 records: changed, renamed,
// unmerged, untracked and ignored files, with a space in a path.
func TestParsePorcelain(t *testing.T) {
	out := strings.Join([]string{
		"1 .M N... 100644 100644 100644 3c3e2ad 3c3e2ad README.md",
		"1 A. N... 000000 100644 100644 0000000 1111111 new dir/added.go",
		"1 .D N... 100644 100644 000000 2222222 2222222 gone.txt",
		"2 R. N... 100644 100644 100644 3333333 3333333 R100 src/new.go", "src/old.go",
		"u UU N... 100644 100644 100644 100644 4444444 5555555 6666666 conflict.txt",
		"? notes.txt",
		"! build/",
		"",
	}, "\x00")
	files, truncated := parsePorcelain([]byte(out), 100)
	want := map[string]string{"README.md": "M", "new dir/added.go": "A", "gone.txt": "D", "src/new.go": "R", "conflict.txt": "C", "notes.txt": "U", "build/": "I"}
	if truncated || len(files) != len(want) {
		t.Fatalf("parsed %+v, truncated %v", files, truncated)
	}
	for _, f := range files {
		if want[f.Path] != f.Status {
			t.Errorf("%s: %s, want %s", f.Path, f.Status, want[f.Path])
		}
	}
	if files, truncated := parsePorcelain([]byte(out), 2); len(files) != 2 || !truncated {
		t.Errorf("cap: %d files, truncated %v", len(files), truncated)
	}
}

// TestGitStatus runs git in a temporary repository with each kind of
// change, through the server.
func TestGitStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	c := startTestServer(t)
	repo := filepath.Join(os.Getenv("GOREX_DIR"), "repo")
	os.MkdirAll(filepath.Join(repo, "sub"), 0o755)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(repo, "kept.txt"), []byte("k\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "edit.txt"), []byte("e\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "old.txt"), []byte("o\n"), 0o644)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.log\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "one")
	os.WriteFile(filepath.Join(repo, "edit.txt"), []byte("changed\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "sub", "new.txt"), []byte("n\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "ignored.log"), []byte("i\n"), 0o644)
	run("mv", "old.txt", "renamed.txt")

	st, err := c.GitStatus(filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(st.Root, repo) {
		t.Errorf("root %q, want %q", st.Root, repo)
	}
	got := map[string]string{}
	for _, f := range st.Files {
		got[f.Path] = f.Status
	}
	for path, want := range map[string]string{"edit.txt": "M", "sub/new.txt": "U", "ignored.log": "I", "renamed.txt": "R"} {
		if got[path] != want {
			t.Errorf("%s: %q, want %s (all: %v)", path, got[path], want, got)
		}
	}
	if _, ok := got["kept.txt"]; ok {
		t.Error("an unchanged file is listed")
	}
	// Outside any repository: empty.
	if st, err := c.GitStatus(os.Getenv("GOREX_DIR")); err != nil || st.Root != "" || len(st.Files) != 0 {
		t.Errorf("outside a repository: %+v %v", st, err)
	}
}
