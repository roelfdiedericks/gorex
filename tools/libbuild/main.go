// Libbuild builds the libghostty-vt that GoRex's servers for other hosts
// load on Linux, from Ghostty's sources at the commit the pinned MyGo
// binds, and writes them with remote-libs.json into remote-libs, which the
// repository keeps:
//
//	go run ./tools/libbuild
//
// MyGo's own Linux libraries are built for glibc 2.28, so a host older
// than Debian 10 cannot run a server with them; these are built for glibc
// 2.17 (-glibc), the glibc of CentOS 7, so that Debian 9 and its
// contemporaries do.
//
// Run it on Linux, with the Zig that Ghostty's build.zig.zon asks for in
// minimum_zig_version, whenever go.mod's MyGo moves to a MyGo that binds
// another Ghostty commit: the library's types are those MyGo's terminal
// package reads, so the commits must match, and remotepayload refuses to
// use libraries built from another one.
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gorex/internal/remotelibs"
)

// targets are the platforms GoRex builds its own library for: the Linux
// ones, whose MyGo libraries need too late a glibc.
var targets = []struct {
	goos, goarch string
	// zig is the target triple, without the glibc version, which -glibc
	// gives; name is the file GoRex's servers load, and built is where
	// Zig puts it under the prefix.
	zig, name, built string
}{
	{"linux", "amd64", "x86_64-linux-gnu", "libghostty-vt.so", "lib/libghostty-vt.so"},
	{"linux", "arm64", "aarch64-linux-gnu", "libghostty-vt.so", "lib/libghostty-vt.so"},
}

func main() {
	log.SetFlags(0)
	src := flag.String("src", "", "a checkout of Ghostty (cloned into the user's cache when empty)")
	glibc := flag.String("glibc", "2.17", "the oldest glibc the libraries must run with")
	out := flag.String("out", remotelibs.Dir, "the directory of the libraries, from the module's root")
	flag.Parse()

	commit, _, err := remotelibs.MyGo()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Ghostty %s, glibc %s", commit, *glibc)
	if *src == "" {
		*src = checkout(commit)
	} else {
		run(*src, "git", "checkout", "--detach", commit)
	}

	manifest := remotelibs.Manifest{
		Ghostty: commit,
		Glibc:   *glibc,
		Files:   map[string]remotelibs.File{},
	}
	for _, t := range targets {
		platform := t.goos + "-" + t.goarch
		prefix, err := os.MkdirTemp("", "libghostty-vt-")
		if err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(prefix)
		log.Printf("building %s", platform)
		run(*src, "zig", "build", "-Demit-lib-vt", "-Dtarget="+t.zig+"."+*glibc,
			"-Doptimize=ReleaseFast", "-Dstrip=true", "--prefix", prefix)
		// The build keeps the debug information of ELF libraries, four
		// fifths of them.
		built := stripELF(filepath.Join(prefix, t.built))
		needs, err := remotelibs.Glibc(built)
		if err != nil {
			log.Fatal(err)
		}
		if remotelibs.Compare(needs, *glibc) > 0 {
			log.Fatalf("%s: the library needs glibc %s, later than %s", platform, needs, *glibc)
		}
		log.Printf("%s needs glibc %s", platform, needs)
		dir := filepath.Join(*out, platform)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal(err)
		}
		sum := copyFile(built, filepath.Join(dir, t.name))
		manifest.Files[platform] = remotelibs.File{Name: t.name, SHA256: sum}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	path := filepath.Join(*out, remotelibs.ManifestName)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", path)
}

// checkout returns a checkout of Ghostty at the commit in the user's
// cache, cloning or fetching it.
func checkout(commit string) string {
	cache, err := os.UserCacheDir()
	if err != nil {
		log.Fatal(err)
	}
	dir := filepath.Join(cache, "gorex", "ghostty-src")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		run("", "git", "clone", "--filter=blob:none", "https://github.com/ghostty-org/ghostty", dir)
	} else {
		run(dir, "git", "fetch", "origin")
	}
	run(dir, "git", "checkout", "--detach", commit)
	return dir
}

func run(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}
}

// copyFile copies src, following links, to dst and returns its SHA-256.
func copyFile(src, dst string) string {
	in, err := os.Open(src)
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()
	f, err := os.Create(dst)
	if err != nil {
		log.Fatal(err)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), in); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// stripELF writes a copy of the ELF library at path without the contents
// of its debug sections, as `strip --strip-debug` would, and returns its
// path. What loading uses (the segments, and the sections in them) stays
// in place; the other sections after the segments move down, and the
// debug sections keep their headers, empty, so that no index changes.
func stripELF(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	if f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB {
		log.Fatalf("%s: not a 64-bit little-endian ELF file", path)
	}
	end := uint64(0)
	for _, p := range f.Progs {
		end = max(end, p.Off+p.Filesz)
	}
	out := append([]byte(nil), data[:end]...)
	le := binary.LittleEndian
	shoff := le.Uint64(data[0x28:])
	shentsize, shnum := uint64(le.Uint16(data[0x3a:])), uint64(le.Uint16(data[0x3c:]))
	headers := append([]byte(nil), data[shoff:shoff+shentsize*shnum]...)
	for i, s := range f.Sections {
		h := headers[uint64(i)*shentsize:]
		if s.Type == elf.SHT_NULL || s.Type == elf.SHT_NOBITS || s.Flags&elf.SHF_ALLOC != 0 || s.Offset < end {
			continue
		}
		size := s.FileSize
		if strings.HasPrefix(s.Name, ".debug") || strings.HasPrefix(s.Name, ".zdebug") {
			size = 0
		}
		align := max(s.Addralign, 1)
		for uint64(len(out))%align != 0 {
			out = append(out, 0)
		}
		le.PutUint64(h[0x18:], uint64(len(out))) // sh_offset
		le.PutUint64(h[0x20:], size)             // sh_size
		out = append(out, data[s.Offset:s.Offset+size]...)
	}
	for len(out)%8 != 0 {
		out = append(out, 0)
	}
	le.PutUint64(out[0x28:], uint64(len(out))) // e_shoff
	out = append(out, headers...)
	stripped := path + ".stripped"
	if err := os.WriteFile(stripped, out, 0o755); err != nil {
		log.Fatal(err)
	}
	return stripped
}
