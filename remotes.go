package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gorex/internal/remote"
	"gorex/internal/rex"
)

// savedRemote is a host the app connected to over ssh, or the user added,
// or a serial port's (serialhost.go), which the host list and the palette
// offer to switch to. Its settings are this PC's: ForwardAgent has ssh
// forward the agent to it (remote.Target.Agent).
type savedRemote struct {
	Key          string    `json:"key"`
	Target       string    `json:"target"`
	Name         string    `json:"name,omitempty"`
	LastUsed     time.Time `json:"lastUsed"`
	ForwardAgent bool      `json:"forwardAgent,omitempty"`
	// Kind is how the window reaches the host: kindServer, with GoRex's
	// server there, kindPlain, with ssh alone, or kindSerial, a serial
	// port of this machine's.
	Kind string `json:"kind,omitempty"`
	// Label is the user's name for the host, over Name, its own.
	Label string `json:"label,omitempty"`
	// Identity is the key ssh logs in with (-i), and Options more of
	// ssh's options, as typed (-p 2222 -o ProxyJump=bastion). PuttyKey is
	// the PuTTY key file (.ppk) of a host imported from PuTTY or KiTTY,
	// which ssh can't read: its dialog says so until it has an Identity.
	Identity string `json:"identity,omitempty"`
	Options  string `json:"options,omitempty"`
	PuttyKey string `json:"puttyKey,omitempty"`
	// ServerDir is the folder GoRex's server lands in on the host, its
	// ~/.cache/gorex when "".
	ServerDir string `json:"serverDir,omitempty"`
	// Command is what a plain host's tabs run there, its login shell
	// when "".
	Command string `json:"command,omitempty"`
	// Note says why a host is plain: what kept GoRex's server from
	// running there.
	Note string `json:"note,omitempty"`
	// Port is a serial host's port as it was chosen, and PortVID,
	// PortPID and PortSerial its adapter's USB IDs and serial number,
	// by which it is found again when it comes back at another path.
	Port       string `json:"port,omitempty"`
	PortVID    string `json:"portVid,omitempty"`
	PortPID    string `json:"portPid,omitempty"`
	PortSerial string `json:"portSerial,omitempty"`
	// Baud and Framing (8N1) are a serial host's speed and framing;
	// Enter ("", "crlf", "lf"), Backspace ("", "bs") and Echo what typing
	// sends there (rex.SerialOptions).
	Baud      int    `json:"baud,omitempty"`
	Framing   string `json:"framing,omitempty"`
	Enter     string `json:"enter,omitempty"`
	Backspace string `json:"backspace,omitempty"`
	Echo      bool   `json:"echo,omitempty"`
}

// The kinds of hosts (savedRemote.Kind).
const (
	kindServer = ""
	kindPlain  = "plain"
	kindSerial = "serial"
)

// title names the remote as the user did, or as its server does.
func (r savedRemote) title() string {
	if r.Label != "" {
		return r.Label
	}
	if r.Name != "" {
		return r.Name
	}
	return r.Target
}

// bindKey is the key of the binding of the host (binding.key).
func (r savedRemote) bindKey() string {
	switch r.Kind {
	case kindPlain:
		return "plain/" + r.Key
	case kindSerial:
		return "serial/" + r.Key
	}
	return "ssh/" + r.Key
}

// sshOpts are the options of every ssh to the host, by its settings.
func (r savedRemote) sshOpts() []string {
	opts := splitArgs(r.Options)
	if r.Identity != "" {
		opts = append(opts, "-i", r.Identity)
	}
	return opts
}

// splitArgs splits s into arguments at spaces, as a shell does, but for
// quotes, single or double, which keep spaces in an argument.
func splitArgs(s string) []string {
	var (
		args  []string
		cur   strings.Builder
		quote rune
		in    bool
	)
	for _, c := range s {
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(c)
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == ' ' || c == '\t' || c == '\n':
			if in {
				args = append(args, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(c)
			in = true
		}
	}
	if in {
		args = append(args, cur.String())
	}
	return args
}

func remotesPath() string { return filepath.Join(rex.Dir(), "remotes.json") }

// loadRemotes returns the known remotes, the latest used first.
func loadRemotes() []savedRemote {
	b, err := os.ReadFile(remotesPath())
	if err != nil {
		return nil
	}
	var f struct {
		Remotes []savedRemote `json:"remotes"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	slices.SortFunc(f.Remotes, func(a, b savedRemote) int { return b.LastUsed.Compare(a.LastUsed) })
	return f.Remotes
}

// saveRemotes writes the known remotes; with no lock, as they change
// seldom, the last window to write wins.
func saveRemotes(rs []savedRemote) {
	b, err := json.MarshalIndent(struct {
		Remotes []savedRemote `json:"remotes"`
	}{rs}, "", "  ")
	if err != nil {
		return
	}
	p := remotesPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		log.Printf("saving remotes: %v", err)
		return
	}
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		log.Printf("saving remotes: %v", err)
		return
	}
	if err := os.Rename(p+".tmp", p); err != nil {
		log.Printf("saving remotes: %v", err)
	}
}

// touchRemote adds r to the known remotes, or updates the one of its key
// with r's target, name, kind and use, keeping its settings. A host the
// window reached with GoRex's server there keeps no note of why it was
// plain.
func touchRemote(r savedRemote) {
	rs := loadRemotes()
	if i := slices.IndexFunc(rs, func(o savedRemote) bool { return o.Key == r.Key }); i >= 0 {
		o := rs[i]
		o.Target, o.Kind, o.LastUsed = r.Target, r.Kind, r.LastUsed
		if r.Name != "" {
			o.Name = r.Name
		}
		if r.Kind == kindServer {
			o.Note = ""
		}
		r = o
		rs = slices.Delete(rs, i, i+1)
	}
	saveRemotes(append(rs, r))
}

// editRemote changes the remote of key by edit, adding one of target
// spec when there is none.
func editRemote(key, spec string, edit func(*savedRemote)) {
	rs := loadRemotes()
	i := slices.IndexFunc(rs, func(o savedRemote) bool { return o.Key == key })
	if i < 0 {
		rs = append(rs, savedRemote{Key: key, Target: spec, LastUsed: time.Now()})
		i = len(rs) - 1
	}
	edit(&rs[i])
	saveRemotes(rs)
}

// putRemote saves r in place of the remote of key old, or adds it; a
// remote of r's key other than that one goes.
func putRemote(old string, r savedRemote) {
	rs := slices.DeleteFunc(loadRemotes(), func(o savedRemote) bool { return o.Key == r.Key && o.Key != old })
	if i := slices.IndexFunc(rs, func(o savedRemote) bool { return o.Key == old }); i >= 0 {
		rs[i] = r
	} else {
		rs = append(rs, r)
	}
	saveRemotes(rs)
}

// setForwardAgent sets whether ssh forwards the agent to the remote of
// key, from its next connection on.
func setForwardAgent(key string, on bool) {
	rs := loadRemotes()
	for i := range rs {
		if rs[i].Key == key {
			rs[i].ForwardAgent = on
		}
	}
	saveRemotes(rs)
}

// findRemote returns the remote of key.
func findRemote(key string) (savedRemote, bool) {
	for _, r := range loadRemotes() {
		if r.Key == key {
			return r, true
		}
	}
	return savedRemote{}, false
}

// forwardsAgent reports whether ssh forwards the agent to the remote of
// key, as this PC's setting for it says.
func forwardsAgent(key string) bool {
	r, _ := findRemote(key)
	return r.ForwardAgent
}

func forgetRemote(key string) {
	saveRemotes(slices.DeleteFunc(loadRemotes(), func(o savedRemote) bool { return o.Key == key }))
}

// hostBinding resolves the ssh destination spec into the binding of its
// host, by its saved kind and settings, unless force, "plain" or
// "server", says the kind.
func hostBinding(spec, force string) (binding, error) {
	var r savedRemote
	ok := false
	for _, o := range loadRemotes() {
		if o.Target == spec {
			r, ok = o, true
			break
		}
	}
	t, err := remote.Resolve(spec, r.sshOpts())
	if err != nil {
		return binding{}, err
	}
	if !ok {
		// Named another way than when saved.
		if r, ok = findRemote(t.Key()); ok && len(r.sshOpts()) > 0 {
			if t, err = remote.Resolve(spec, r.sshOpts()); err != nil {
				return binding{}, err
			}
		}
	}
	t.Dir = r.ServerDir
	plain := r.Kind == kindPlain
	switch force {
	case "plain":
		plain = true
	case "server":
		plain = false
	}
	return binding{remote: !plain, plain: plain, target: t}, nil
}

// sshArgs reads the arguments of gorex ssh: -A, which forwards the ssh
// agent, or -a, which doesn't, as ssh's own, --plain, which connects with
// ssh alone, or --server, with GoRex's server on the host, over the
// host's saved kind, then the destination. agent is "yes", "no", or ""
// when neither is given (remote.Target.Agent); force is "plain",
// "server" or "" (hostBinding).
func sshArgs(args []string) (agent, force, spec string, err error) {
	const usage = "gorex ssh takes -A or -a, --plain or --server, then one destination, user@host"
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-A":
			agent = "yes"
		case "-a":
			agent = "no"
		case "--plain":
			force = "plain"
		case "--server":
			force = "server"
		default:
			return "", "", "", fmt.Errorf("unknown option %s: %s", args[0], usage)
		}
		args = args[1:]
	}
	if len(args) != 1 {
		return "", "", "", errors.New(usage)
	}
	return agent, force, args[0], nil
}

// switchTo connects the window to the server of b instead, again when
// force is set.
func (a *App) switchTo(b binding, force bool) {
	if !force && b.key() == a.bind.key() {
		return
	}
	if !force && !a.confirmUnsaved(a.modifiedPanes()) {
		return
	}
	a.saveNow()
	if a.settingsDue {
		a.saveHostSettings()
	}
	a.detachAll()
	a.reset()
	a.hello = rex.Hello{}
	a.bind = b
	a.hostLabel = labelOf(b)
	// Until the host's own settings are known, the Defaults.
	a.host = Settings{}
	a.applySettings()
	a.startConnecting(a.win, b, "Connecting to "+b.label()+"…")
}

// connectSpec connects the window to the host of an ssh destination, as
// its saved kind says; it fails when ssh can't make one of it.
func (a *App) connectSpec(spec string) error {
	b, err := hostBinding(spec, "")
	if err != nil {
		return fmt.Errorf("Can't connect to %s: %w", spec, err)
	}
	a.switchTo(b, false)
	return nil
}
