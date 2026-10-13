package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gorex/internal/remote"
	"gorex/internal/rex"
)

// savedRemote is a server the app connected to over ssh, which the
// palette offers to switch to.
type savedRemote struct {
	Key      string    `json:"key"`
	Target   string    `json:"target"`
	Name     string    `json:"name,omitempty"`
	LastUsed time.Time `json:"lastUsed"`
}

// title names the remote as its server does, or as the user did.
func (r savedRemote) title() string {
	if r.Name != "" {
		return r.Name
	}
	return r.Target
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

// touchRemote adds r to the known remotes, or updates the one of its key.
func touchRemote(r savedRemote) {
	rs := slices.DeleteFunc(loadRemotes(), func(o savedRemote) bool { return o.Key == r.Key })
	saveRemotes(append(rs, r))
}

func forgetRemote(key string) {
	saveRemotes(slices.DeleteFunc(loadRemotes(), func(o savedRemote) bool { return o.Key == key }))
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
	// Until the host's own settings are known, the Defaults.
	a.host = Settings{}
	a.applySettings()
	a.startConnecting(a.win, b, "Connecting to "+b.label()+"…")
}

// connectSpec connects the window to the server of an ssh destination; it
// fails when ssh can't make one of it.
func (a *App) connectSpec(spec string) error {
	t, err := remote.Resolve(spec)
	if err != nil {
		return fmt.Errorf("Can't connect to %s: %w", spec, err)
	}
	a.switchTo(binding{remote: true, target: t}, false)
	return nil
}
