// Package rex is GoRex's session server and its client.
//
// The server owns the pseudo-terminals: shells keep running when the app
// quits, and the next launch attaches to them again, with what they printed
// replayed. The app talks to it over a Unix socket: a control connection of
// JSON lines (requests and their responses), and one connection per
// attached session that carries the terminal's bytes both ways.
package rex

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"
)

// ProtocolVersion changes whenever the wire protocol does.
const ProtocolVersion = 3

// BridgeReady starts the line a bridge prints before it multiplexes its
// standard input and output; the rest of the line is its executable.
const BridgeReady = "GOREX-BRIDGE "

// Request is a line of the control connection, from the client.
type Request struct {
	ID     int64          `json:"id"`
	Op     string         `json:"op"`
	SID    string         `json:"sid,omitempty"`
	Cols   int            `json:"cols,omitempty"`
	Rows   int            `json:"rows,omitempty"`
	Create *CreateOptions `json:"create,omitempty"`
	// Key names a document of the app's, for get and set, which keeps
	// Value; the server stores it and does not read it.
	Key   string          `json:"key,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
	// Path is a file of the host's for stat, readDir, readFile and
	// writeFile; Data what writeFile writes. Max caps what readFile
	// reads, 0 for no cap; IfModTime, when set, has writeFile refuse a
	// file modified since then.
	Path      string    `json:"path,omitempty"`
	Data      []byte    `json:"data,omitempty"`
	Max       int64     `json:"max,omitempty"`
	IfModTime time.Time `json:"ifModTime,omitzero"`
}

// FileInfo describes a file of the host's, as stat and readDir report.
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"modTime"`
	Dir     bool      `json:"dir,omitempty"`
}

// FileData is a file's bytes as readFile answers, with the modification
// time they were read at; writeFile answers the time it wrote.
type FileData struct {
	Data    []byte    `json:"data,omitempty"`
	ModTime time.Time `json:"modTime"`
}

// ErrTooLarge is readFile's error for a file over the cap asked.
const ErrTooLarge = "too large"

// ErrChanged is writeFile's error for a file modified since IfModTime.
const ErrChanged = "changed on disk"

// Response answers the Request with the same ID.
type Response struct {
	ID    int64           `json:"id"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// CreateOptions start a session.
type CreateOptions struct {
	// Command runs instead of the user's login shell; Login starts it as a
	// login shell too, as the user's shell is, with "-" before its name.
	Command []string `json:"command,omitempty"`
	Login   bool     `json:"login,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Env     []string `json:"env,omitempty"`
	Cols    int      `json:"cols,omitempty"`
	Rows    int      `json:"rows,omitempty"`
	// Scrollback is about how many lines of output the session keeps
	// above its screen, for the windows that attach to it: the server's
	// default when zero.
	Scrollback int `json:"scrollback,omitempty"`
}

// SessionInfo describes a session and what runs in it now.
type SessionInfo struct {
	ID      string    `json:"id"`
	PID     int       `json:"pid"`
	Shell   string    `json:"shell"`
	Created time.Time `json:"created"`
	// Program is the name of the program in the foreground, and Args its
	// arguments; Idle tells that it is the shell itself, at its prompt.
	Program string   `json:"program"`
	Args    []string `json:"args,omitempty"`
	Idle    bool     `json:"idle"`
	// Dir is the working directory of the program in the foreground.
	Dir string `json:"dir"`
	// Title is the last title a program set (OSC 0, OSC 2).
	Title      string    `json:"title,omitempty"`
	LastOutput time.Time `json:"lastOutput"`
	LastInput  time.Time `json:"lastInput"`
	Output     uint64    `json:"output"`
	Bells      int       `json:"bells"`
	Exited     bool      `json:"exited"`
	ExitCode   int       `json:"exitCode"`
	Attached   int       `json:"attached"`
	Cols       int       `json:"cols"`
	Rows       int       `json:"rows"`
}

// Hello is the server's answer to "hello".
type Hello struct {
	Version int       `json:"version"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	Host    HostInfo  `json:"host"`
	// Exe is the server's executable, and ExeTime when it was modified
	// last as the server started: a development build compares them with
	// its own, to replace a server of an older build.
	Exe     string    `json:"exe,omitempty"`
	ExeTime time.Time `json:"exeTime,omitzero"`
}

// HostInfo describes the machine the server runs on.
type HostInfo struct {
	Name   string `json:"name"`
	Model  string `json:"model"`
	Chip   string `json:"chip"`
	Memory uint64 `json:"memory"`
	OS     string `json:"os"`
	User   string `json:"user"`
	Home   string `json:"home"`
	// Portable tells a machine with a battery, as a laptop, where the
	// model does not.
	Portable bool `json:"portable,omitempty"`
	// GOOS is the server's runtime.GOOS.
	GOOS string `json:"goos,omitempty"`
}

// Attach is the first line of a connection attaching to a session.
type Attach struct {
	Op   string `json:"op"` // "attach"
	SID  string `json:"sid"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// Dir returns the directory of the server's socket, log and state.
func Dir() string {
	if d := os.Getenv("GOREX_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "GoRex")
}

// SocketPath returns the path of the server's socket: in Dir, unless that
// is too long a path for a socket, then in the temporary directory.
func SocketPath() string {
	p := filepath.Join(Dir(), "server.sock")
	if len(p) < 100 {
		return p
	}
	h := fnv.New32a()
	h.Write([]byte(p))
	name := fmt.Sprintf("gorex-%x.sock", h.Sum32())
	if uid := os.Getuid(); uid >= 0 { // Windows has none, and a temporary directory per user
		name = fmt.Sprintf("gorex-%d-%x.sock", uid, h.Sum32())
	}
	return filepath.Join(os.TempDir(), name)
}
