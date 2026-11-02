// Package remotelibs is the libghostty-vt that GoRex's servers for other
// hosts load: MyGo's, from the terminal plugin of the MyGo that go.mod
// pins, but for the platforms GoRex builds its own for, kept in the
// repository's remote-libs directory (tools/libbuild): the Linux ones, for
// an older glibc than MyGo's (2.28), so that hosts as old as Debian 9 run
// a server. Both are built from the Ghostty commit MyGo binds, which they
// must be: the library's types are those MyGo's terminal package reads.
package remotelibs

import (
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Dir is the directory of GoRex's own libraries, from the module's root.
const Dir = "remote-libs"

// ManifestName is the file in Dir that names them.
const ManifestName = "remote-libs.json"

// Manifest names GoRex's own libraries: the Ghostty commit they are built
// from, the glibc they are built for (none needs a later one), and the
// files, by GOOS-GOARCH, each at Dir/<GOOS-GOARCH>/<name>.
type Manifest struct {
	Ghostty string          `json:"ghostty"`
	Glibc   string          `json:"glibc"`
	Files   map[string]File `json:"files"`
}

// File is a library: its name, and the SHA-256 of its contents.
type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Path is where the library of platform (GOOS-GOARCH) is, under root.
func (m *Manifest) Path(root, platform string) string {
	return filepath.Join(root, Dir, platform, m.Files[platform].Name)
}

// Read reads the manifest under root: nil without one.
func Read(root string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(root, Dir, ManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("reading %s: %w", ManifestName, err)
	}
	return &m, nil
}

// NativeFile is a library of MyGo's terminal plugin: its name, the
// SHA-256 of its contents, and where it is downloaded from.
type NativeFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

// MyGo reads the libghostty-vt files of the pinned MyGo's terminal plugin,
// by GOOS-GOARCH, and the Ghostty commit they are built from.
func MyGo() (commit string, files map[string]NativeFile, err error) {
	dir, err := mygoDir()
	if err != nil {
		return "", nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "plugins", "terminal", "mygo-plugin.json"))
	if err != nil {
		return "", nil, err
	}
	var manifest struct {
		Libraries []struct {
			Name   string                `json:"name"`
			Source string                `json:"source"`
			Files  map[string]NativeFile `json:"files"`
		} `json:"libraries"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return "", nil, fmt.Errorf("reading MyGo's terminal plugin: %w", err)
	}
	for _, l := range manifest.Libraries {
		if l.Name != "libghostty-vt" {
			continue
		}
		_, commit, ok := strings.Cut(l.Source, "/tree/")
		if !ok || commit == "" {
			return "", nil, fmt.Errorf("MyGo's terminal plugin names no Ghostty commit (source %q)", l.Source)
		}
		return commit, l.Files, nil
	}
	return "", nil, fmt.Errorf("MyGo's terminal plugin has no libghostty-vt")
}

// mygoDir finds the pinned MyGo's directory: a replacement's, or the
// module cache's, downloading it there when it is not yet.
func mygoDir() (string, error) {
	list := func() (string, error) {
		out, err := exec.Command("go", "list", "-m", "-json", "github.com/egoist/mygo").Output()
		if err != nil {
			return "", fmt.Errorf("finding MyGo: %w", err)
		}
		var mod struct {
			Dir     string
			Replace *struct{ Dir string }
		}
		if err := json.Unmarshal(out, &mod); err != nil {
			return "", fmt.Errorf("finding MyGo: %w", err)
		}
		if mod.Replace != nil && mod.Replace.Dir != "" {
			return mod.Replace.Dir, nil
		}
		return mod.Dir, nil
	}
	dir, err := list()
	if err != nil || dir != "" {
		return dir, err
	}
	if out, err := exec.Command("go", "mod", "download", "github.com/egoist/mygo").CombinedOutput(); err != nil {
		return "", fmt.Errorf("downloading MyGo: %v\n%s", err, out)
	}
	if dir, err = list(); err == nil && dir == "" {
		err = fmt.Errorf("finding MyGo: go list -m gives no directory")
	}
	return dir, err
}

// Glibc returns the latest glibc version an ELF file needs of the
// libraries it links (GLIBC_x.y), as "x.y": "" for none, as a file that
// is not ELF, or links nothing of glibc's by version.
func Glibc(path string) (string, error) {
	f, err := elf.Open(path)
	if err != nil {
		var fe *elf.FormatError
		if errors.As(err, &fe) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	needs, err := f.DynamicVersionNeeds()
	if err != nil {
		// No version needs section: nothing by version.
		return "", nil
	}
	latest := ""
	for _, n := range needs {
		for _, d := range n.Needs {
			v, ok := strings.CutPrefix(d.Dep, "GLIBC_")
			if ok && Compare(v, latest) > 0 {
				latest = v
			}
		}
	}
	return latest, nil
}

// Compare compares two glibc versions ("2.17"), -1, 0 or 1; "" is before
// any.
func Compare(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}
