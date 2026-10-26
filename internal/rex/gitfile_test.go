package rex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A revision passes unless git could take it for an option or more than
// one argument, or its colon would name another path.
func TestGitRevOK(t *testing.T) {
	for rev, ok := range map[string]bool{
		"HEAD": true, "HEAD~2": true, "main": true, "origin/main": true, "v1.2^{commit}": true,
		"0123456789abcdef0123456789abcdef01234567": true, ":0": true, ":3": true,
		"": false, "-p": false, "--output=x": false, "HEAD x": false, "HEAD\n": false, "a\x00b": false,
		":4": false, ":": false, "HEAD:other.txt": false, ":0:x": false,
	} {
		if err := gitRevOK(rev); (err == nil) != ok {
			t.Errorf("%q: %v, want ok %v", rev, err, ok)
		}
	}
}

// A path is named from its nearest folder that exists, the rest of it
// with slashes.
func TestGitAnchor(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a"), 0o755)
	for _, c := range []struct{ path, dir, rel string }{
		{filepath.Join(root, "a", "f.png"), filepath.Join(root, "a"), "f.png"},
		{filepath.Join(root, "a", "gone", "deeper", "f.png"), filepath.Join(root, "a"), "gone/deeper/f.png"},
		{filepath.Join(root, "f.png"), root, "f.png"},
	} {
		if dir, rel := gitAnchor(c.path); dir != c.dir || rel != c.rel {
			t.Errorf("%s: %s, %s, want %s, %s", c.path, dir, rel, c.dir, c.rel)
		}
	}
}

// readGitFile reads a file at HEAD and in the index apart from the disk,
// one whose folder is gone, answers a client that has the blob without
// the bytes until it changes, and refuses one over the cap and a bad
// revision; it needs git.
func TestReadGitFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	file := filepath.Join(repo, "img", "a.txt")
	gone := filepath.Join(repo, "sub", "b.txt")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.MkdirAll(filepath.Dir(gone), 0o755)
	os.WriteFile(file, []byte("one"), 0o644)
	os.WriteFile(gone, []byte("bee"), 0o644)
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	os.WriteFile(file, []byte("two"), 0o644)
	git("add", ".")
	os.WriteFile(file, []byte("three"), 0o644)
	os.RemoveAll(filepath.Dir(gone))

	read := func(path, rev string, max int64) (string, error) {
		v, err := readGitFile(path, rev, "", max)
		if err != nil {
			return "", err
		}
		return string(v.(GitFileData).Data), nil
	}
	for _, c := range []struct{ path, rev, want string }{
		{file, "HEAD", "one"},
		{file, ":0", "two"},
		{gone, "HEAD", "bee"},
	} {
		if got, err := read(c.path, c.rev, 0); err != nil || got != c.want {
			t.Errorf("%s at %s: %q, %v, want %q", c.path, c.rev, got, err, c.want)
		}
	}
	if _, err := read(file, "HEAD", 2); err == nil || !strings.Contains(err.Error(), ErrTooLarge) {
		t.Errorf("over the cap: %v", err)
	}
	first, err := readGitFile(file, ":0", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	blob := first.(GitFileData).Blob
	if v, err := readGitFile(file, ":0", blob, 0); err != nil || !v.(GitFileData).Unchanged || v.(GitFileData).Data != nil || v.(GitFileData).Blob != blob {
		t.Errorf("the blob the client has: %+v, %v", v, err)
	}
	git("add", ".") // "three"
	if v, err := readGitFile(file, ":0", blob, 0); err != nil || v.(GitFileData).Unchanged || string(v.(GitFileData).Data) != "three" || v.(GitFileData).Blob == blob {
		t.Errorf("a blob changed in the index: %+v, %v", v, err)
	}
	// The window tells a missing file by git's words (tsp_media.go).
	if _, err := read(filepath.Join(repo, "img", "none.txt"), "HEAD", 0); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a file not in HEAD: %v", err)
	}
	if _, err := read(file, "--output=x", 0); err == nil {
		t.Error("an option as a revision read")
	}
}
