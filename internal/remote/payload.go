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

	"gorex/internal/remotelibs"
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
// the directory it lands in on a host: another build, another ID; and the
// glibc its files need, as "2.17", "" for none.
type server struct {
	platform
	dir, id, glibc string
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
		for _, f := range s.files() {
			g, err := remotelibs.Glibc(f)
			if err != nil {
				return Payload{}, err
			}
			if remotelibs.Compare(g, s.glibc) > 0 {
				s.glibc = g
			}
		}
		p.servers = append(p.servers, s)
	}
	if len(p.servers) == 0 {
		return Payload{}, errors.New("this GoRex build has no servers for other hosts; build them with go run ./tools/remotepayload")
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
// system, when it is there, in the folder dir (serverDir), and otherwise
// asks for it, naming the system, and says on its error output, which the
// Connection Log shows, why: no server for the system, none copied yet,
// or one there that can't be run, with its modes and the mount it is on
// (noexec). It runs no server from a folder that is not the user's. The
// server loads the libghostty-vt beside it, and nothing else: it never
// downloads one. A server that can't start says so on its error output
// too, as the shell does when exec fails. On a host whose glibc is older
// than the server's files need, it starts nothing and copies nothing: it
// answers with the host's glibc and the one needed.
func (p Payload) bridgeScript(profile, dir string) string {
	var b strings.Builder
	b.WriteString(`case "$(uname -s) $(uname -m)" in `)
	for _, s := range p.servers {
		for i, u := range s.unames {
			if i > 0 {
				b.WriteString("|")
			}
			b.WriteString(`"` + u + `"`)
		}
		b.WriteString(`) id=` + s.id + ` lib=` + s.lib + ` g=` + s.glibc + `;; `)
	}
	b.WriteString(`*) id= g=;; esac; c=` + serverDir(dir) + `; d="$c/$id"; `)
	// getconf names the glibc as "glibc 2.24", and nothing on a host
	// without one, which is not checked.
	b.WriteString(`if [ -n "$g" ]; then h=$(getconf GNU_LIBC_VERSION 2>/dev/null); h=${h#glibc }; ` +
		`if [ -n "$h" ] && [ "$(printf '%s\n%s\n' "$g" "$h" | sort -t. -k1,1n -k2,2n | head -n 1)" != "$g" ]; then ` +
		`echo "` + glibcMarker + `$h $g"; exit 0; fi; fi; `)
	b.WriteString(`if [ -n "$id" ] && [ -x "$d/gorex" ] && ` + mine + `; then export MYGO_GHOSTTY_VT="$d/$lib"; exec "$d/gorex" -bridge ` + shQuote(profile) + `; fi; `)
	b.WriteString(`if [ -z "$id" ]; then echo "host: GoRex has no server for $(uname -s) $(uname -m)" >&2; ` +
		`elif [ -e "$d/gorex" ] && ! ` + mine + `; then echo "host: $c belongs to another user, so GoRex does not run a server from it" >&2; ` +
		`elif [ -e "$d/gorex" ]; then echo "host: $d/gorex exists but cannot run:" >&2; ls -ld "$d" "$d/gorex" "$d/$lib" 2>&1 | sed 's/^/host: /' >&2; ` +
		`(findmnt -no TARGET,FSTYPE,OPTIONS -T "$d" 2>/dev/null || df -P "$d") 2>&1 | sed 's/^/host: mount: /' >&2; ` +
		`else echo "host: no GoRex server in $d yet" >&2; fi; `)
	b.WriteString(`echo "` + deployMarker + `$(uname -s) $(uname -m)"`)
	return b.String()
}

// serverDir returns, as shell words, the folder on a host the servers
// land in, each in a folder of its ID: dir, "~/" at its start the user's
// home, or ~/.cache/gorex when "".
func serverDir(dir string) string {
	switch {
	case dir == "":
		return `"$HOME/.cache/gorex"`
	case dir == "~":
		return `"$HOME"`
	case strings.HasPrefix(dir, "~/"):
		return `"$HOME"/` + shQuote(dir[2:])
	}
	return shQuote(dir)
}

// mine is a shell test of whether the folder $c is the user's: a server
// in a folder another user owns, as one in /tmp, might be theirs.
const mine = `[ "$(ls -ldn "$c" 2>/dev/null | awk '{print $3}')" = "$(id -u)" ]`

// makeDir is a shell command that makes the folder $c the user's alone
// (0700) when it is not there; a folder that is, as one the host's
// settings name, keeps its modes.
const makeDir = `{ [ -d "$c" ] || { mkdir -p "$c" 2>/dev/null && chmod 700 "$c"; }; }`
