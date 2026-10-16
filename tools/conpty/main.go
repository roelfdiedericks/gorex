// Command conpty fetches Microsoft's ConPTY, the one Windows Terminal
// ships, into resources/windows-amd64, where builds for Windows put it
// beside GoRex.exe: conpty.dll, and OpenConsole.exe in x64\ as the
// package lays it out. Run it from the module's root:
//
//	go run ./tools/conpty
//
// Windows' own ConPTY drops what it cannot model, as OSC sequences a
// terminal writes to a program (the Kitty clipboard protocol's paste
// events); this one passes them through. GoRex uses Windows' own when
// these files are missing.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	version = "1.25.260930003"
	sum     = "02b07b349af66d801159bdf9e440d4a1ce78bb951f37fc8609731665afdae7ee"
	url     = "https://api.nuget.org/v3-flatcontainer/microsoft.windows.console.conpty/" + version + "/microsoft.windows.console.conpty." + version + ".nupkg"
	out     = "resources/windows-amd64"
)

// files are the package's files GoRex ships, and where.
var files = map[string]string{
	"runtimes/win-x64/native/conpty.dll":        "conpty.dll",
	"build/native/runtimes/x64/OpenConsole.exe": "x64/OpenConsole.exe",
}

func main() {
	log.SetFlags(0)
	res, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", url, res.Status)
	}
	pkg, err := io.ReadAll(res.Body)
	if err != nil {
		log.Fatal(err)
	}
	if h := sha256.Sum256(pkg); hex.EncodeToString(h[:]) != sum {
		log.Fatalf("sha256 mismatch for %s", url)
	}
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range zr.File {
		dest, ok := files[f.Name]
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			log.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			log.Fatal(err)
		}
		path := filepath.Join(out, dest)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o755); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s (%d bytes)\n", path, len(b))
		delete(files, f.Name)
	}
	for name := range files {
		log.Fatalf("%s is not in ConPTY %s", name, version)
	}
}
