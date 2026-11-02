package remotelibs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"2.17", "2.17", 0},
		{"2.17", "2.24", -1},
		{"2.28", "2.27", 1},
		{"2.9", "2.17", -1},
		{"3.0", "2.40", 1},
		{"2.17.1", "2.17", 1},
		{"", "2.2.5", -1},
		{"", "", 0},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestOwnLibraries checks GoRex's own libraries in the repository: built
// from the Ghostty commit the pinned MyGo binds, as its manifest says,
// with the SHA-256 it names, and needing no later glibc than it says. It
// fails when MyGo's pin moves to another Ghostty commit, until
// tools/libbuild has built them again.
func TestOwnLibraries(t *testing.T) {
	const root = "../.."
	m, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatalf("no %s/%s", Dir, ManifestName)
	}
	commit, _, err := MyGo()
	if err != nil {
		t.Fatal(err)
	}
	if m.Ghostty != commit {
		t.Errorf("GoRex's own libghostty-vt is built from Ghostty %s, but the pinned MyGo binds %s: build them again, on Linux, with go run ./tools/libbuild", m.Ghostty, commit)
	}
	if len(m.Files) == 0 {
		t.Errorf("%s names no files", ManifestName)
	}
	for platform, f := range m.Files {
		path := m.Path(root, platform)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Error(err)
			continue
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Errorf("%s: SHA-256 %x, not %s", path, sum, f.SHA256)
		}
		g, err := Glibc(path)
		if err != nil {
			t.Error(err)
			continue
		}
		if g == "" || Compare(g, m.Glibc) > 0 {
			t.Errorf("%s needs glibc %q, not %s or earlier", path, g, m.Glibc)
		}
	}
}
