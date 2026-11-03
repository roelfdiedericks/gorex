package main

import (
	"bufio"
	"bytes"
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/remote"
)

// Importing hosts from PuTTY and KiTTY: their saved ssh sessions, read
// where each keeps them (PuTTY's registry key, KiTTY's, and the folders of
// a KiTTY in portable mode; importhosts_windows.go), become hosts of
// GoRex's server, one a user@host:port, in a dialog the palette opens. The
// values are as PuTTY's source has them (settings.c, sessprep.c): strings,
// and numbers as decimal text here.

// puttySession is a saved session of PuTTY or KiTTY: its name, where it
// was found (PuTTY, KiTTY, or a KiTTY folder's path), its subfolder of a
// KiTTY folder, and its values by name.
type puttySession struct {
	Name, Source, Folder string
	Values               map[string]string
}

// importDefaultSettings is the session PuTTY and KiTTY start new ones
// from, which names no host: a session without a user name takes its.
const importDefaultSettings = "Default Settings"

// importCandidate is a session as the host it becomes: its ssh
// destination (spec) and options, forwarding the agent, its key, and what
// of the session the host leaves out (notes). Once ssh resolved spec, key
// is the host's, and blocked says why it is not imported: a host GoRex
// has, the same host as another session, or ssh making no host of it.
type importCandidate struct {
	s        puttySession
	spec     string
	options  []string
	agent    bool
	identity string
	puttyKey string
	notes    []string
	key      string
	blocked  string
	checked  bool
}

// importProxies names PuTTY's proxy methods (ProxyMethod) that ssh can't
// take as they are; 6, an ssh jump host, is ssh's -J.
var importProxies = map[string]string{
	"1": "SOCKS 4", "2": "SOCKS 5", "3": "HTTP", "4": "Telnet",
	"5": "local command", "7": "SSH command", "8": "SSH subsystem",
}

// puttyDestination is the user, host and port of a session's values, as
// PuTTY makes them: blanks taken out of HostName, a user@ in it over
// UserName (which the caller applies), a :port after it dropped, as PuTTY
// drops it, brackets taken off an IPv6 address; the port is PortNumber's.
func puttyDestination(v map[string]string) (user, host string, port int) {
	host = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, v["HostName"])
	if i := strings.LastIndex(host, "@"); i >= 0 {
		user, host = host[:i], host[i+1:]
	}
	switch {
	case strings.HasPrefix(host, "["):
		if j := strings.Index(host, "]"); j > 0 {
			host = host[1:j]
		}
	case strings.Count(host, ":") == 1:
		host = host[:strings.Index(host, ":")]
	}
	port = 22
	if p, err := strconv.Atoi(v["PortNumber"]); err == nil && p > 0 {
		port = p
	}
	return user, host, port
}

// sessionHost makes the host of the ssh session s; false for a session of
// another protocol or of no host. defaults are the values of the Default
// Settings of its source, whose user name a session without one takes,
// unless it asks for the system's; sessions are those of its source, by
// name, as a jump host can name one.
func sessionHost(s puttySession, defaults map[string]string, sessions map[string]puttySession) (importCandidate, bool) {
	v := s.Values
	// A session that names no protocol is ssh's, PuTTY's default.
	if p := v["Protocol"]; p != "" && p != "ssh" {
		return importCandidate{}, false
	}
	user, host, port := puttyDestination(v)
	if host == "" || s.Name == importDefaultSettings {
		return importCandidate{}, false
	}
	c := importCandidate{s: s, agent: v["AgentFwd"] == "1"}
	if user == "" {
		user = v["UserName"]
	}
	if user == "" && v["UserNameFromEnvironment"] != "1" {
		if d := defaults["UserName"]; d != "" {
			user = d
			c.notes = append(c.notes, "user "+d+", as Default Settings has it")
		}
	}
	c.spec = host
	if user != "" {
		c.spec = user + "@" + host
	}
	if port != 22 {
		c.options = append(c.options, "-p", strconv.Itoa(port))
	}
	method := v["ProxyMethod"]
	if method == "" && v["ProxyType"] != "" && v["ProxyType"] != "0" {
		// Before ProxyMethod, PuTTY kept ProxyType.
		c.notes = append(c.notes, "its proxy isn't imported")
	}
	switch name := importProxies[method]; {
	case method == "6":
		if jump := jumpHost(v, sessions); jump != "" {
			c.options = append(c.options, "-J", jump)
		} else {
			c.notes = append(c.notes, "its jump host names no host")
		}
	case name != "":
		c.notes = append(c.notes, "its proxy ("+name+") isn't imported")
	}
	if sel := v["ProxySelection"]; sel != "" && sel != "- Session defined proxy -" {
		c.notes = append(c.notes, "its KiTTY proxy "+sel+" isn't imported")
	}
	if v["Compression"] == "1" {
		c.options = append(c.options, "-C")
	}
	switch v["AddressFamily"] {
	case "1":
		c.options = append(c.options, "-4")
	case "2":
		c.options = append(c.options, "-6")
	}
	if key := strings.TrimSpace(v["PublicKeyFile"]); key != "" {
		if strings.EqualFold(filepath.Ext(key), ".ppk") {
			c.puttyKey = key
			c.notes = append(c.notes, "key "+key+" is PuTTY's: export it from PuTTYgen (Conversions → Export OpenSSH key)")
		} else {
			c.identity = key
		}
	}
	if strings.TrimSpace(v["RemoteCommand"]) != "" {
		c.notes = append(c.notes, "its remote command isn't imported")
	}
	return c, true
}

// jumpHost is the -J of a session whose proxy is an ssh jump host:
// ProxyHost names a saved session, as PuTTY tries first, or a host, at
// ProxyPort; ProxyUsername is the user over either's. "" for no host.
func jumpHost(v map[string]string, sessions map[string]puttySession) string {
	ph := strings.TrimSpace(v["ProxyHost"])
	var user, host string
	port := 22
	if js, ok := sessions[ph]; ok && ph != "" {
		user, host, port = puttyDestination(js.Values)
		if user == "" {
			user = js.Values["UserName"]
		}
	} else {
		user, host, _ = puttyDestination(map[string]string{"HostName": ph})
		if p, err := strconv.Atoi(v["ProxyPort"]); err == nil && p > 0 {
			port = p
		}
	}
	if pu := v["ProxyUsername"]; pu != "" {
		user = pu
	}
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != 22 {
		host += ":" + strconv.Itoa(port)
	}
	if user != "" {
		host = user + "@" + host
	}
	return host
}

// unescapeBytes decodes the %XX escapes of PuTTY's names and KiTTY's
// values into the bytes they stand for.
func unescapeBytes(s string) []byte {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b = append(b, byte(v))
				i += 2
				continue
			}
		}
		b = append(b, s[i])
	}
	return b
}

// kittyFile reads a session file of a KiTTY folder: a line a value,
// Key\value\, the value's bytes escaped as %XX (KiTTY's mungestr), which
// decode turns into text (Windows' code page).
func kittyFile(b []byte, decode func([]byte) string) map[string]string {
	v := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		name, value, ok := strings.Cut(line, `\`)
		if !ok || name == "" {
			continue
		}
		value = strings.TrimSuffix(value, `\`)
		v[name] = decode(unescapeBytes(value))
	}
	return v
}

// kittyIni reads what kitty.ini says of where sessions are: [KiTTY]'s
// savemode (registry, file, dir) and configdir.
func kittyIni(b []byte) (savemode, configdir string) {
	section := ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, ";"):
		case strings.HasPrefix(line, "["):
			section = strings.Trim(line, "[]")
		case strings.EqualFold(section, "KiTTY"):
			name, value, _ := strings.Cut(line, "=")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "savemode":
				savemode = strings.ToLower(strings.TrimSpace(value))
			case "configdir":
				configdir = strings.TrimSpace(value)
			}
		}
	}
	return savemode, configdir
}

// kittyFolder reads the sessions of a KiTTY folder (its Sessions), its
// subfolders too, as KiTTY's folders: source names it.
func kittyFolder(dir, source string, decode func([]byte) string) []puttySession {
	var sessions []puttySession
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		folder, _ := filepath.Rel(dir, filepath.Dir(path))
		if folder == "." {
			folder = ""
		}
		sessions = append(sessions, puttySession{
			Name: decode(unescapeBytes(d.Name())), Source: source, Folder: filepath.ToSlash(folder),
			Values: kittyFile(b, decode),
		})
		return nil
	})
	return sessions
}

// sessionsDir is the folder of a KiTTY's sessions: dir's Sessions when it
// has one, as a KiTTY's own folder does, else dir.
func sessionsDir(dir string) string {
	if st, err := os.Stat(filepath.Join(dir, "Sessions")); err == nil && st.IsDir() {
		return filepath.Join(dir, "Sessions")
	}
	return dir
}

// importCandidates makes the hosts of sessions, those of each source with
// its own Default Settings and names, sorted by source, folder and name.
func importCandidates(sessions []puttySession) []*importCandidate {
	bySource := map[string]map[string]puttySession{}
	for _, s := range sessions {
		if bySource[s.Source] == nil {
			bySource[s.Source] = map[string]puttySession{}
		}
		bySource[s.Source][s.Name] = s
	}
	var out []*importCandidate
	for _, s := range sessions {
		named := bySource[s.Source]
		if c, ok := sessionHost(s, named[importDefaultSettings].Values, named); ok {
			out = append(out, &c)
		}
	}
	slices.SortStableFunc(out, func(a, b *importCandidate) int {
		return cmp.Or(cmp.Compare(a.s.Source, b.s.Source), cmp.Compare(a.s.Folder, b.s.Folder),
			cmp.Compare(strings.ToLower(a.s.Name), strings.ToLower(b.s.Name)))
	})
	return out
}

// importForm is the import dialog's state: the sessions' hosts once read
// and resolved, the folders chosen besides those found, and the problems
// reading them.
type importForm struct {
	loading  bool
	rows     []*importCandidate
	folders  []string
	problems []string
}

// openImport opens the import dialog, which reads the sessions.
func (a *App) openImport() {
	f := &importForm{}
	a.importForm = f
	a.scanImport(f)
}

// scanImport reads the sessions of PuTTY and KiTTY, and those of the
// folders chosen, has ssh resolve each one's host, and marks those GoRex
// has and those of a host another session has before them.
func (a *App) scanImport(f *importForm) {
	f.loading = true
	win, folders := a.win, slices.Clone(f.folders)
	go func() {
		sessions, problems := importSources(folders)
		rows := importCandidates(sessions)
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, c := range rows {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				opts := slices.Clone(c.options)
				if c.identity != "" {
					opts = append(opts, "-i", c.identity)
				}
				t, err := remote.Resolve(c.spec, opts)
				if err != nil {
					c.blocked = "ssh makes no host of it: " + err.Error()
					return
				}
				c.key = t.Key()
			}()
		}
		wg.Wait()
		have := map[string]bool{}
		for _, r := range loadRemotes() {
			have[r.Key] = true
		}
		first := map[string]string{}
		for _, c := range rows {
			switch {
			case c.blocked != "":
			case have[c.key]:
				c.blocked = "already in GoRex"
			case first[c.key] != "":
				c.blocked = "the same host as " + first[c.key]
			default:
				first[c.key] = c.s.Name
			}
			c.checked = c.blocked == ""
		}
		update := func() {
			if a.importForm != f {
				return
			}
			f.rows, f.problems, f.loading = rows, problems, false
			a.changed()
		}
		if win == nil {
			update()
			return
		}
		win.Update(update)
	}()
}

// importChosen saves the hosts ticked, of GoRex's server, named as their
// sessions, and closes the dialog.
func (a *App) importChosen(f *importForm) {
	n := 0
	for _, c := range f.rows {
		if !c.checked || c.blocked != "" {
			continue
		}
		putRemote("", savedRemote{
			Key: c.key, Target: c.spec, Label: c.s.Name, LastUsed: time.Now(), Kind: kindServer,
			Options: strings.Join(c.options, " "), Identity: c.identity, PuttyKey: c.puttyKey, ForwardAgent: c.agent,
		})
		n++
	}
	a.remotes, a.importForm = loadRemotes(), nil
	what := strconv.Itoa(n) + " hosts"
	if n == 1 {
		what = "1 host"
	}
	a.toast(ui.Toast{ID: "import", Title: "Imported " + what, Description: "from PuTTY and KiTTY: they are in the host list.", Timeout: 3 * time.Second})
}

// importDialog draws the import dialog: the sessions found, each as the
// host it becomes, what of it is left out, and why one can't be imported.
func (a *App) importDialog(c *ui.Context, k *colors) {
	f := a.importForm
	if f == nil {
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(640).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Padding(18, 20).Gap(12).Clip()
		ui.Text(c, "Import hosts from PuTTY and KiTTY").FontSize(14).FontWeight(700).TextColor(k.text)
		switch {
		case f.loading:
			ui.Text(c, "Reading the saved sessions…").FontSize(12.5).TextColor(k.textMuted)
		case len(f.rows) == 0:
			ui.Text(c, "No saved ssh sessions of PuTTY or KiTTY were found. A KiTTY that keeps its sessions in a folder of its own may be chosen.").
				FontSize(12.5).TextColor(k.textMuted).LineHeight(1.45)
		default:
			ui.Text(c, "Each becomes a host of GoRex's server, named as its session. Sessions of hosts GoRex has are left out.").
				FontSize(12).TextColor(k.textFaint).LineHeight(1.4)
			ui.Scroll(c.Key("import-rows")).MaxHeight(420).Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					for i, r := range f.rows {
						a.importRow(c.Key("import/"+strconv.Itoa(i)), k, r)
					}
				})
			})
		}
		for _, p := range f.problems {
			ui.Text(c, p).FontSize(11.5).TextColor(ui.Hex("#ef4444")).LineHeight(1.35).Selectable()
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if textButton(c, k, "Choose KiTTY folder…").Clicked() && !f.loading {
				a.chooseKittyFolder(f)
			}
			// One button ticks every session that can be imported, or,
			// when all are, none; greyed when none can be.
			all, some := true, false
			for _, r := range f.rows {
				if r.blocked == "" {
					some = true
					all = all && r.checked
				}
			}
			switch {
			case f.loading || len(f.rows) == 0:
			case !some:
				ui.Box(c).Height(22).Padding(0, 9).Center().Shrink(0).Children(func() {
					ui.Text(c, "Select All").FontSize(12).FontWeight(600).TextColor(k.textFaint)
				})
			default:
				label := "Select All"
				if all {
					label = "Select None"
				}
				if textButton(c, k, label).Clicked() {
					for _, r := range f.rows {
						if r.blocked == "" {
							r.checked = !all
						}
					}
				}
			}
			ui.Box(c).Grow(1)
			if textButton(c, k, "Cancel").Clicked() {
				a.importForm = nil
			}
			n := 0
			for _, r := range f.rows {
				if r.checked && r.blocked == "" {
					n++
				}
			}
			if n > 0 && !f.loading && textButton(c, k, "Import "+strconv.Itoa(n)).Clicked() {
				a.later(c, func() { a.importChosen(f) })
			}
		})
	})
	if !open {
		a.importForm = nil
	}
}

// importRow draws a session's row: a tick, its name and where it was
// found, the host it becomes, and its notes.
func (a *App) importRow(c *ui.Context, k *colors, r *importCandidate) {
	ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Checkbox(c, &r.checked, "").Disabled(r.blocked != "")
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			where := r.s.Source
			if r.s.Folder != "" {
				where += " · " + r.s.Folder
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, r.s.Name).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(260)
				ui.Text(c, where).FontSize(11.5).TextColor(k.textFaint).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			})
			dest := r.spec
			if len(r.options) > 0 {
				dest += "   " + strings.Join(r.options, " ")
			}
			ui.Text(c, dest).FontSize(12).TextColor(k.textMuted).SingleLine().Ellipsis("…")
			if r.blocked != "" {
				ui.Text(c, r.blocked).FontSize(11.5).TextColor(k.attention).LineHeight(1.35)
			}
			for _, n := range r.notes {
				ui.Text(c, n).FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
			}
		})
	})
}

// chooseKittyFolder asks for the folder of a KiTTY that keeps its sessions
// in one (its own, or its Sessions), and reads the sessions again with it.
func (a *App) chooseKittyFolder(f *importForm) {
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "KiTTY's folder", Directory: true})
		if err != nil || len(paths) == 0 || win == nil {
			return
		}
		win.Update(func() {
			if a.importForm != f {
				return
			}
			f.folders = append(f.folders, paths[0])
			a.scanImport(f)
			a.changed()
		})
	}()
}
