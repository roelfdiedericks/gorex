// Command remotepayload builds the session servers GoRex copies to the
// hosts it connects to over ssh, one directory per platform: the server,
// GoRex built for that platform, and the libghostty-vt it needs. Run it
// from the module's root:
//
//	go run ./tools/remotepayload                      # into resources/remote
//	go run ./tools/remotepayload -out dev/remote -targets linux/amd64
//
// The platforms build side by side, and go build relinks only what
// changed. The libraries are GoRex's own, for the platforms it builds them
// for (remote-libs, by tools/libbuild), and otherwise those of the MyGo
// that go.mod pins; both are checked against their SHA-256 sums. MyGo's
// come from MyGo's download cache, where the mygo CLI keeps them, and are
// downloaded there when missing. GoRex's own must be built from the
// Ghostty commit MyGo binds: when MyGo's pin moves to another, this fails
// until tools/libbuild has built them again.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"gorex/internal/remotelibs"
)

// allTargets are the platforms of the hosts GoRex connects to.
var allTargets = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"}

func main() {
	log.SetFlags(0)
	out := flag.String("out", "resources/remote", "directory of the servers, one directory per platform")
	targets := flag.String("targets", strings.Join(allTargets, ","), "comma separated GOOS/GOARCH targets")
	flag.Parse()
	commit, mygoLibs, err := remotelibs.MyGo()
	if err != nil {
		log.Fatal(err)
	}
	own, err := remotelibs.Read(".")
	if err != nil {
		log.Fatal(err)
	}
	if own != nil && own.Ghostty != commit {
		log.Fatalf("GoRex's own libghostty-vt (%s) is built from Ghostty %s, but the pinned MyGo binds %s: build them again, on Linux, with go run ./tools/libbuild",
			filepath.Join(remotelibs.Dir, remotelibs.ManifestName), own.Ghostty, commit)
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, t := range strings.Split(*targets, ",") {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(t), "/")
		if !ok {
			log.Fatalf("bad target %q: want GOOS/GOARCH", t)
		}
		platform := goos + "-" + goarch
		lib, ok := mygoLibs[platform]
		if !ok {
			log.Fatalf("MyGo's terminal plugin has no libghostty-vt for %s/%s", goos, goarch)
		}
		name, sum, src := lib.Name, lib.SHA256, func() (string, error) { return cached(lib) }
		if own != nil {
			if f, ok := own.Files[platform]; ok {
				name, sum, src = f.Name, f.SHA256, func() (string, error) { return own.Path(".", platform), nil }
			}
		}
		wg.Go(func() {
			if err := buildTarget(*out, goos, goarch, name, sum, src); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s/%s: %w", goos, goarch, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		log.Fatal(err)
	}
}

// buildTarget builds the server for goos/goarch in out/<goos>-<goarch>,
// with the library beside it: name, of SHA-256 sum, which src gives. go
// build relinks the server only when it changed, and the library is
// copied only when it differs. The tag gorexserver leaves out what only
// the app draws, as Mermaid diagrams.
func buildTarget(out, goos, goarch, name, sum string, src func() (string, error)) error {
	dir := filepath.Join(out, goos+"-"+goarch)
	build := exec.Command("go", "build", "-trimpath", "-tags=gorexserver", "-ldflags=-s -w", "-o", filepath.Join(dir, "gorex"), ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("building the server: %w", err)
	}
	dst := filepath.Join(dir, name)
	if fileSum(dst) == sum {
		return nil
	}
	path, err := src()
	if err != nil {
		return err
	}
	if s := fileSum(path); s != sum {
		return fmt.Errorf("%s: its SHA-256 is %q, not %s", path, s, sum)
	}
	return copyFile(path, dst)
}

// cached returns the library in MyGo's download cache, where the mygo CLI
// keeps it, downloading it there when it is missing.
func cached(f remotelibs.NativeFile) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mygo", "natives", f.SHA256, f.Name)
	if fileSum(path) == f.SHA256 {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	log.Printf("downloading %s", f.URL)
	return path, download(f.URL, path, f.SHA256)
}

// copyFile copies src to dst, which it replaces only once it is whole.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// fileSum returns the SHA-256 of a file in hex, "" when it can't be read.
func fileSum(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// download fetches url to path, which it replaces only once the file has
// the SHA-256 sum.
func download(url, path, sum string) error {
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, res.Status)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), res.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != sum {
		err = fmt.Errorf("%s: sha256 mismatch", url)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
