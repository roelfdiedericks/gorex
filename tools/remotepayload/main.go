// Command remotepayload builds the session servers GoRex copies to the
// hosts it connects to over ssh, one directory per platform: the server,
// GoRex built for that platform, and the libghostty-vt it needs. Run it
// from the module's root:
//
//	go run ./tools/remotepayload                      # into resources/remote
//	go run ./tools/remotepayload -out dev/remote -targets linux/amd64
//
// The platforms build side by side, and go build relinks only what
// changed. The libraries are those of the MyGo that go.mod pins, checked
// against its SHA-256 sums; they come from MyGo's download cache, where the
// mygo CLI keeps them, and are downloaded there when missing.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

// allTargets are the platforms of the hosts GoRex connects to.
var allTargets = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"}

type nativeFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

func main() {
	log.SetFlags(0)
	out := flag.String("out", "resources/remote", "directory of the servers, one directory per platform")
	targets := flag.String("targets", strings.Join(allTargets, ","), "comma separated GOOS/GOARCH targets")
	flag.Parse()
	libs, err := libraries()
	if err != nil {
		log.Fatal(err)
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
		lib, ok := libs[goos+"-"+goarch]
		if !ok {
			log.Fatalf("MyGo's terminal plugin has no libghostty-vt for %s/%s", goos, goarch)
		}
		wg.Go(func() {
			if err := buildTarget(*out, goos, goarch, lib); err != nil {
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

// libraries reads the libghostty-vt files of the pinned MyGo's terminal
// plugin, by GOOS-GOARCH.
func libraries() (map[string]nativeFile, error) {
	dir, err := mygoDir()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "plugins", "terminal", "mygo-plugin.json"))
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Libraries []struct {
			Name  string                `json:"name"`
			Files map[string]nativeFile `json:"files"`
		} `json:"libraries"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return nil, fmt.Errorf("reading MyGo's terminal plugin: %w", err)
	}
	for _, l := range manifest.Libraries {
		if l.Name == "libghostty-vt" {
			return l.Files, nil
		}
	}
	return nil, fmt.Errorf("MyGo's terminal plugin has no libghostty-vt")
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

// buildTarget builds the server for goos/goarch in out/<goos>-<goarch>,
// with the library beside it. go build relinks the server only when it
// changed, and the library is copied only when it differs.
func buildTarget(out, goos, goarch string, lib nativeFile) error {
	dir := filepath.Join(out, goos+"-"+goarch)
	build := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(dir, "gorex"), ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("building the server: %w", err)
	}
	dst := filepath.Join(dir, lib.Name)
	if fileSum(dst) == lib.SHA256 {
		return nil
	}
	src, err := cached(lib)
	if err != nil {
		return err
	}
	return copyFile(src, dst)
}

// cached returns the library in MyGo's download cache, where the mygo CLI
// keeps it, downloading it there when it is missing.
func cached(f nativeFile) (string, error) {
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
