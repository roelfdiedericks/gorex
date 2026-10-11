// Command remotepayload builds the session servers GoRex copies to the
// hosts it connects to over ssh, one archive per platform in
// resources/remote: the server and the libghostty-vt it needs. Run it from
// the module's root:
//
//	go run ./tools/remotepayload
//	go run ./tools/remotepayload -targets linux/amd64
//
// The libraries are those of the MyGo that go.mod pins, checked against
// its SHA-256 sums; they come from MyGo's download cache, where the mygo
// CLI keeps them, and are downloaded there when missing.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const out = "resources/remote"

// allTargets are the platforms of the hosts GoRex connects to.
var allTargets = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"}

type nativeFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

func main() {
	log.SetFlags(0)
	targets := flag.String("targets", strings.Join(allTargets, ","), "comma separated GOOS/GOARCH targets")
	flag.Parse()
	libs, err := libraries()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	work, err := os.MkdirTemp("", "remotepayload")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(work)
	for _, t := range strings.Split(*targets, ",") {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(t), "/")
		if !ok {
			log.Fatalf("bad target %q: want GOOS/GOARCH", t)
		}
		lib, ok := libs[goos+"-"+goarch]
		if !ok {
			log.Fatalf("MyGo's terminal plugin has no libghostty-vt for %s/%s", goos, goarch)
		}
		if err := buildTarget(work, goos, goarch, lib); err != nil {
			log.Fatalf("%s/%s: %v", goos, goarch, err)
		}
	}
}

// libraries reads the libghostty-vt files of the pinned MyGo's terminal
// plugin, by GOOS-GOARCH.
func libraries() (map[string]nativeFile, error) {
	// go mod download puts the module in the cache when it is not there
	// yet, which go list -m would not, and tells where.
	out, err := exec.Command("go", "mod", "download", "-json", "github.com/egoist/mygo").Output()
	if err != nil {
		return nil, fmt.Errorf("finding MyGo: %w", err)
	}
	var mod struct{ Dir string }
	if err := json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		return nil, fmt.Errorf("finding MyGo: go mod download gave %q", out)
	}
	b, err := os.ReadFile(filepath.Join(mod.Dir, "plugins", "terminal", "mygo-plugin.json"))
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

// buildTarget builds the server for goos/goarch and writes its archive.
func buildTarget(work, goos, goarch string, lib nativeFile) error {
	exe := filepath.Join(work, goos+"-"+goarch, "gorex")
	build := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", exe, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("building the server: %w", err)
	}
	libPath, err := cached(lib)
	if err != nil {
		return err
	}
	archive := filepath.Join(out, goos+"-"+goarch+".tar.gz")
	if err := writeArchive(archive, []entry{{"gorex", exe, 0o755}, {lib.Name, libPath, 0o644}}); err != nil {
		return err
	}
	st, err := os.Stat(archive)
	if err != nil {
		return err
	}
	fmt.Printf("%s\t%d bytes\n", archive, st.Size())
	return nil
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

type entry struct {
	name, path string
	mode       int64
}

// writeArchive writes the files as a gzipped tar archive. It records no
// time, so that the same files make the same archive, and the same ID.
func writeArchive(path string, entries []entry) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		if err = addFile(tw, e); err != nil {
			break
		}
	}
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = zw.Close()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func addFile(tw *tar.Writer, e entry) error {
	f, err := os.Open(e.path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: e.name, Mode: e.mode, Size: st.Size(), ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
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
