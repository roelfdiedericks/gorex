package rex

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestFiles: the file operations through the server, against a temporary
// directory: stat, readDir's order and dotfiles, readFile and its cap,
// writeFile creating, replacing with the mode kept, and refusing a file
// changed since it was read.
func TestFiles(t *testing.T) {
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = ""
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

	files := filepath.Join(dir, "files")
	os.MkdirAll(filepath.Join(files, "sub"), 0o755)
	os.WriteFile(filepath.Join(files, "b.txt"), []byte("bee"), 0o644)
	os.WriteFile(filepath.Join(files, ".hidden"), []byte("h"), 0o644)
	os.WriteFile(filepath.Join(files, "A.go"), []byte("package a\n"), 0o644)

	fi, err := c.Stat(files)
	if err != nil || !fi.Dir || fi.Name != "files" {
		t.Fatalf("stat %+v %v", fi, err)
	}
	fis, err := c.ReadDir(files)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, fi := range fis {
		names = append(names, fi.Name)
	}
	if want := []string{".hidden", "A.go", "b.txt", "sub"}; !slices.Equal(names, want) {
		t.Errorf("readDir = %v, want %v", names, want)
	}
	if !fis[3].Dir || fis[2].Size != 3 {
		t.Errorf("readDir entries %+v", fis)
	}
	if _, err := c.ReadDir(filepath.Join(files, "b.txt")); err == nil {
		t.Error("readDir of a file succeeded")
	}

	fd, err := c.ReadFile(filepath.Join(files, "b.txt"), 0)
	if err != nil || string(fd.Data) != "bee" || fd.ModTime.IsZero() {
		t.Fatalf("readFile %+v %v", fd, err)
	}
	if _, err := c.ReadFile(filepath.Join(files, "b.txt"), 2); err == nil || !strings.Contains(err.Error(), ErrTooLarge) {
		t.Errorf("readFile over the cap: %v", err)
	}
	if _, err := c.ReadFile(filepath.Join(files, "sub"), 0); err == nil {
		t.Error("readFile of a directory succeeded")
	}
	if _, err := c.ReadFile(filepath.Join(files, "none"), 0); err == nil {
		t.Error("readFile of a missing file succeeded")
	}

	// Creating.
	newPath := filepath.Join(files, "sub", "new.txt")
	mod, err := c.WriteFile(newPath, []byte("fresh"), time.Time{})
	if err != nil || mod.IsZero() {
		t.Fatalf("writeFile creating: %v", err)
	}
	if b, _ := os.ReadFile(newPath); string(b) != "fresh" {
		t.Errorf("created %q", b)
	}
	if entries, _ := os.ReadDir(filepath.Join(files, "sub")); len(entries) != 1 {
		t.Errorf("leftovers: %v", entries)
	}
	// Replacing with the time it was read: fine, and the mode kept.
	if runtime.GOOS != "windows" {
		os.Chmod(newPath, 0o600)
	}
	st, _ := os.Stat(newPath)
	mod2, err := c.WriteFile(newPath, []byte("second"), st.ModTime())
	if err != nil {
		t.Fatalf("writeFile replacing: %v", err)
	}
	st2, _ := os.Stat(newPath)
	if string(must(os.ReadFile(newPath))) != "second" || st2.Mode().Perm() != st.Mode().Perm() || !st2.ModTime().Equal(mod2) {
		t.Errorf("replaced: %q mode %v (was %v) mod %v (told %v)", must(os.ReadFile(newPath)), st2.Mode().Perm(), st.Mode().Perm(), st2.ModTime(), mod2)
	}
	// Replacing with a stale time: refused, the file as it was.
	stale := st.ModTime().Add(-time.Hour)
	if _, err := c.WriteFile(newPath, []byte("third"), stale); err == nil || !strings.Contains(err.Error(), ErrChanged) {
		t.Errorf("stale write: %v", err)
	}
	if string(must(os.ReadFile(newPath))) != "second" {
		t.Error("a stale write changed the file")
	}
	if _, err := c.WriteFile(filepath.Join(files, "sub"), []byte("x"), time.Time{}); err == nil {
		t.Error("writing over a directory succeeded")
	}

	// A large file goes through the control connection whole.
	big := make([]byte, 9<<20)
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	bigPath := filepath.Join(files, "big.txt")
	if _, err := c.WriteFile(bigPath, big, time.Time{}); err != nil {
		t.Fatalf("writing 9 MB: %v", err)
	}
	if fd, err := c.ReadFile(bigPath, 0); err != nil || len(fd.Data) != len(big) || fd.Data[len(big)-1] != big[len(big)-1] {
		t.Errorf("reading 9 MB: %d bytes, %v", len(fd.Data), err)
	}
	if _, err := c.ReadFile(bigPath, 8<<20); err == nil || !strings.Contains(err.Error(), ErrTooLarge) {
		t.Errorf("9 MB over an 8 MB cap: %v", err)
	}
}

func must(b []byte, err error) []byte { return b }
