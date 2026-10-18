package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// platform is a system GoRex has a server for.
type platform struct {
	// name is its GOOS-GOARCH, and that of the directory of its server
	// among the app's resources: remote/<name>.
	name string
	// unames are the ways uname -s -m names it.
	unames []string
	// lib is the libghostty-vt beside the server.
	lib string
}

var platforms = []platform{
	{"linux-amd64", []string{"Linux x86_64", "Linux amd64"}, "libghostty-vt.so"},
	{"linux-arm64", []string{"Linux aarch64", "Linux arm64"}, "libghostty-vt.so"},
	{"darwin-amd64", []string{"Darwin x86_64"}, "libghostty-vt.dylib"},
	{"darwin-arm64", []string{"Darwin arm64"}, "libghostty-vt.dylib"},
}

// platformOf returns the platform uname -s -m names, as "Linux x86_64".
func platformOf(system string) (platform, bool) {
	system = strings.Join(strings.Fields(system), " ")
	for _, p := range platforms {
		for _, u := range p.unames {
			if u == system {
				return p, true
			}
		}
	}
	return platform{}, false
}

// server is the directory of a platform's server, and its ID, which names
// the directory it lands in on a host: another build, another ID.
type server struct {
	platform
	dir, id string
}

// files are the server's files: the server, and the libghostty-vt beside
// it.
func (s server) files() []string {
	return []string{filepath.Join(s.dir, "gorex"), filepath.Join(s.dir, s.lib)}
}

// Payload is the servers GoRex copies to the hosts it connects to, for the
// platforms it has them for.
type Payload struct{ servers []server }

// LoadPayload finds the servers among the app's resources.
func LoadPayload(resources string) (Payload, error) {
	var p Payload
	for _, pl := range platforms {
		s := server{platform: pl, dir: filepath.Join(resources, "remote", pl.name)}
		id, err := filesID(s.files())
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Payload{}, err
		}
		s.id = id
		p.servers = append(p.servers, s)
	}
	if len(p.servers) == 0 {
		return Payload{}, errors.New("this GoRex has no servers for other hosts: build them with go run ./tools/remotepayload")
	}
	return p, nil
}

// filesID returns the first 16 hex digits of the SHA-256 of the files'
// contents, one after the other.
func filesID(paths []string) (string, error) {
	h := sha256.New()
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// forSystem returns the server for the system uname -s -m names.
func (p Payload) forSystem(system string) (server, error) {
	if pl, ok := platformOf(system); ok {
		for _, s := range p.servers {
			if s.name == pl.name {
				return s, nil
			}
		}
	}
	return server{}, fmt.Errorf("GoRex has no server for %s", system)
}

// bridgeScript starts the bridge of profile with the server for the host's
// system, when it is there, and otherwise asks for it, naming the system.
// The server loads the libghostty-vt beside it, and nothing else: it never
// downloads one.
func (p Payload) bridgeScript(profile string) string {
	var b strings.Builder
	b.WriteString(`case "$(uname -s) $(uname -m)" in `)
	for _, s := range p.servers {
		for i, u := range s.unames {
			if i > 0 {
				b.WriteString("|")
			}
			b.WriteString(`"` + u + `"`)
		}
		b.WriteString(`) id=` + s.id + ` lib=` + s.lib + `;; `)
	}
	b.WriteString(`*) id=;; esac; d="$HOME/.cache/gorex/$id"; `)
	b.WriteString(`if [ -n "$id" ] && [ -x "$d/gorex" ]; then export MYGO_GHOSTTY_VT="$d/$lib"; exec "$d/gorex" -bridge ` + shQuote(profile) + `; fi; `)
	b.WriteString(`echo "` + deployMarker + `$(uname -s) $(uname -m)"`)
	return b.String()
}
