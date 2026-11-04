package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// hostName names the machine the window shows: the name its settings
// give it, else its own (ownHostName).
func (a *App) hostName() string {
	if a.eff.Appearance.Name != "" {
		return a.eff.Appearance.Name
	}
	return a.ownHostName()
}

// ownHostName names the machine the window shows when its settings give
// it no name: as the user named it in the host's dialog, else as its
// server does, but for a plain host, whose server is this machine's, else
// as it is reached.
func (a *App) ownHostName() string {
	if a.hostLabel != "" {
		return a.hostLabel
	}
	if a.bind.host() && (a.bind.plain || a.hello.Host.Name == "") {
		return a.bind.target.Spec
	}
	if a.hello.Host.Name != "" {
		return a.hello.Host.Name
	}
	name, _ := os.Hostname()
	return strings.TrimSuffix(name, ".local")
}

// labelOf is the user's name for the host of b, from its dialog: "" for
// none, and for this machine. A serial host's is its name, the user's or
// the one it got from its port, as it has no server to name it.
func labelOf(b binding) string {
	switch {
	case b.serial != "":
		r, _ := findRemote(b.serial)
		return r.title()
	case b.host():
		r, _ := findRemote(b.target.Key())
		return r.Label
	}
	return ""
}

// hostGlyph is the icon of a host of kind, by what its server told of the
// machine: a serial port's, a plain host's, a laptop, a desktop, Linux,
// else a Mac; a host it told nothing of yet, a server.
func hostGlyph(kind, model, goos string, portable bool) string {
	switch {
	case kind == kindSerial:
		return "cable"
	case kind == kindPlain:
		return "globe"
	case strings.Contains(model, "MacBook"), portable:
		return "laptop"
	case strings.Contains(model, "iMac"), goos == "windows":
		return "monitor"
	case model == "Linux", goos == "linux", goos == "":
		return "server"
	}
	return "mac-studio"
}

// windowGlyph is the icon of the host the window shows: as its server
// tells, or while the window connects, as it likely is.
func (a *App) windowGlyph() string {
	h := a.hello.Host
	goos := h.GOOS
	if goos == "" {
		goos = runtime.GOOS
		if a.bind.remote {
			goos = "linux"
		}
	}
	kind := kindServer
	switch {
	case a.bind.serial != "":
		// The server, of a serial port, is this machine's.
		kind = kindSerial
	case a.bind.plain:
		// What the server tells is this machine's, not the host's.
		kind = kindPlain
	}
	return hostGlyph(kind, h.Model, goos, h.Portable)
}

// connState is the color of the window's connection and what it is.
func (a *App) connState(k *colors) (ui.Color, string) {
	switch {
	case a.connDown:
		return ui.Hex("#ef4444"), "not connected"
	case a.client == nil:
		return k.attention, "connecting…"
	case a.status != "":
		return k.attention, "reconnecting…"
	}
	return k.busy, "connected"
}

// hostChip shows the machine the sessions run on; a click shows more of
// it, and of the session server.
func (a *App) hostChip(c *ui.Context, k *colors) {
	h := a.hello.Host
	name, model := a.hostName(), h.Model
	switch {
	case a.bind.serial != "":
		model = "serial · " + a.bind.port + " · " + a.bind.line
	case a.bind.plain:
		model = "plain ssh"
	}
	glyph := a.windowGlyph()
	chip := ui.Row(c).Gap(9).Padding(3, 8, 3, 6).Radius(9).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label("Host " + name).MaxWidth(190).Shrink(0)
	if chip.Hovered() || a.hostOpen {
		chip.Background(k.hover)
	}
	if chip.Clicked() {
		a.hostOpen = !a.hostOpen
		if a.hostOpen {
			a.remotes, a.hostErr, a.hostForget, a.hostQuery, a.hostSel = loadRemotes(), "", "", "", 0
		}
	}
	chip.ContextMenu(func(m *ui.Menu) { a.hostMenu(m) })
	chip.Children(func() {
		ui.Icon(c, icon(glyph)).Size(19, 19).TextColor(k.textMuted)
		ui.Column(c).MinWidth(0).Children(func() {
			ui.Text(c, name).FontSize(13.5).FontWeight(700).TextColor(k.text.Alpha(0.85)).SingleLine().Ellipsis("…")
			ui.Text(c, model).FontSize(11.5).TextColor(k.textFaint).SingleLine().Ellipsis("…")
		})
	})
	ui.PopoverBase(c, chip, &a.hostOpen, func(panel ui.Element) {
		panel.Margin(8, 0, 0, 0).Width(560).Radius(14).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 12, 32, 0, k.shadowFocused).Clip()
		// Taller than the window has room for under the title bar, as
		// with many hosts, it scrolls.
		s := ui.Scroll(c.Key("host-panel"))
		if h := c.Root().Bounds().H; h > 0 {
			s.MaxHeight(max(h-titleH-16, 200))
		}
		s.Children(func() {
			ui.Column(c).Padding(14).Gap(12).Children(func() { a.hostPanel(c, k, name, glyph) })
		})
	})
}

func (a *App) hostPanel(c *ui.Context, k *colors, name, glyph string) {
	h := a.hello.Host
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(38, 38).Radius(10).Background(k.hover).Center().Children(func() {
			ui.Icon(c, icon(glyph)).Size(24, 24).TextColor(k.text)
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, name).FontSize(14).FontWeight(700).TextColor(k.text).SingleLine().Ellipsis("…")
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				where := thisMachine()
				switch {
				case a.bind.serial != "":
					where = "serial port"
				case a.bind.plain:
					where = "plain ssh"
				case a.bind.remote:
					where = "ssh"
				}
				dot, state := a.connState(k)
				ui.Box(c).Size(7, 7).Radius(4).Background(dot)
				ui.Text(c, where+" · "+state).FontSize(12).TextColor(k.textFaint)
			})
		})
	})
	if a.status != "" {
		ui.Text(c, a.status).FontSize(11.5).TextColor(k.textMuted).LineHeight(1.35)
	}
	ui.Divider(c)
	row := func(label, value string) {
		if value == "" {
			return
		}
		ui.Row(c).Gap(12).Children(func() {
			ui.Text(c, label).FontSize(12.5).TextColor(k.textFaint).Width(78)
			ui.Text(c, value).FontSize(12.5).TextColor(k.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		})
	}
	ui.Column(c).Gap(7).Children(func() {
		switch {
		case a.bind.host():
			row("Connection", "ssh "+a.bind.target.Key())
		case a.bind.serial != "":
			r, _ := findRemote(a.bind.serial)
			row("Port", r.Port)
		}
		if a.bind.noFiles() {
			// The rest the server tells is this machine's.
			return
		}
		row("Model", h.Model)
		row("Chip", h.Chip)
		if h.Memory > 0 {
			row("Memory", fmt.Sprintf("%d GB", h.Memory>>30))
		}
		row("System", h.OS)
		row("User", h.User)
	})
	switch {
	case a.bind.plain:
		ui.Text(c, "A plain ssh host. Its tabs run ssh on this PC, and GoRex doesn't show its files or git.").
			FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
	case a.bind.serial != "":
		ui.Text(c, "A serial port on this PC. Its session stays open while GoRex is closed. If the port is unplugged, the session waits for it to come back.").
			FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
	}
	ui.Divider(c)
	a.hostsSection(c, k)
	ui.Divider(c)
	sessions, running := 0, 0
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			sessions++
			if !p.info.Idle && p.info.Program != "" {
				running++
			}
		}
	}
	ui.Column(c).Gap(7).Children(func() {
		if a.hello.PID > 0 {
			row("Server", fmt.Sprintf("pid %d · up %s", a.hello.PID, roundDur(time.Since(a.hello.Started))))
		}
		row("Sessions", fmt.Sprintf("%d open, %d running a program", sessions, running))
	})
	if a.bind.plain {
		ui.Text(c, "Sessions run in a server on this PC. If you quit GoRex, they keep running. When GoRex opens again, they come back as they were, as long as their connections are still up.").
			FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
		return
	}
	if a.bind.serial != "" {
		return
	}
	ui.Text(c, "Sessions run in the server. If you quit GoRex, they keep running. When GoRex opens again, they come back as they were.").
		FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
}

func roundDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// hostEntry is a row of the host popover's list: its icon (hostGlyph), its
// name, the line after it (the address, or a serial port's) and how long
// ago it was used; whether the window shows it. open switches the window
// to it, and window opens it in a window of its own, as GoRex's arguments;
// key is a saved host's, for its settings and to forget it; pc marks this
// machine, whose settings are the Defaults; addrs are its ssh
// destinations, by which the text typed picks it only in full.
type hostEntry struct {
	glyph, title, detail, age string
	current                   bool
	open                      func()
	window                    []string
	key                       string
	pc                        bool
	addrs                     []string
}

// hostQueryText is the text typed in the host popover, without an ssh in
// front.
func hostQueryText(q string) string {
	q = strings.TrimSpace(q)
	return strings.TrimSpace(strings.TrimPrefix(q, "ssh "))
}

// filterHosts answers the text typed in the host popover: the rows of the
// entries whose name or line has it, ignoring case (all for none), and the
// one Enter switches to, -1 for none: the only one left, or, for an
// address (a user@, an IP), only a host of that address in full, as the
// only one left of 192.168.0.18 is a 192.168.0.180, which isn't it.
func filterHosts(entries []hostEntry, q string) (shown []int, pick int) {
	q = strings.ToLower(hostQueryText(q))
	addr := strings.Contains(q, "@") || isAddress(q)
	for i, e := range entries {
		if q == "" || strings.Contains(strings.ToLower(e.title+" "+e.detail), q) || addr && hasAddr(e, q) {
			shown = append(shown, i)
		}
	}
	pick = -1
	switch {
	case q == "":
	case !addr:
		if len(shown) == 1 {
			pick = shown[0]
		}
	default:
		for _, i := range shown {
			if hasAddr(entries[i], q) {
				if pick >= 0 {
					// Two hosts of the address, as two kinds of it:
					// neither.
					return shown, -1
				}
				pick = i
			}
		}
	}
	return shown, pick
}

// isAddress tells an IP address, with a port or not.
func isAddress(q string) bool {
	if h, _, err := net.SplitHostPort(q); err == nil {
		q = h
	}
	return net.ParseIP(strings.Trim(q, "[]")) != nil
}

// hasAddr tells whether q, lower case, is one of the entry's destinations,
// with or without its user and its port.
func hasAddr(e hostEntry, q string) bool {
	for _, a := range e.addrs {
		a = strings.ToLower(a)
		user, host := "", a
		if i := strings.LastIndex(a, "@"); i >= 0 {
			user, host = a[:i+1], a[i+1:]
		}
		forms := []string{a, host}
		if h, _, err := net.SplitHostPort(host); err == nil {
			forms = append(forms, h, user+h)
		}
		if slices.Contains(forms, q) {
			return true
		}
	}
	return false
}

// hostsSection lists the hosts the window can show the sessions of: this
// machine, those connected to before, the serial hosts, and one to type.
// What is typed filters them: Enter switches to the one it picks (or the
// arrows), else connects to it as an ssh destination.
func (a *App) hostsSection(c *ui.Context, k *colors) {
	pcGOOS := a.pcHost.GOOS
	if pcGOOS == "" {
		pcGOOS = runtime.GOOS
	}
	entries := []hostEntry{{glyph: hostGlyph(kindServer, a.pcHost.Model, pcGOOS, a.pcHost.Portable), title: thisMachine(), detail: "local sessions",
		current: !a.bind.away(), open: func() { a.openHost("") }, pc: true}}
	listed := false
	for _, r := range a.remotes {
		cur := r.bindKey() == a.bind.key()
		listed = listed || cur
		detail, window, addrs := r.Target, []string{"ssh", r.Target}, []string{r.Target, r.Key}
		switch r.Kind {
		case kindPlain:
			detail = "plain ssh · " + detail
		case kindSerial:
			c := r.config()
			detail, window, addrs = "serial · "+r.Port+" · "+strconv.Itoa(c.Baud)+" "+c.Framing(), []string{"serial", r.Key}, nil
		}
		age := ""
		if !cur {
			age = " · " + roundDur(time.Since(r.LastUsed)) + " ago"
		}
		entries = append(entries, hostEntry{glyph: hostGlyph(r.Kind, r.Model, r.GOOS, r.Portable), title: r.title(), detail: detail, age: age, current: cur,
			open: func() { a.openSaved(r) }, window: window, key: r.Key, addrs: addrs})
	}
	if a.bind.host() && !listed {
		// A host the window connects to for the first time.
		spec := a.bind.target.Spec
		entries = append(entries, hostEntry{glyph: a.windowGlyph(), title: spec, detail: a.bind.target.Key(), current: true, open: func() { a.openHost(spec) }, window: []string{"ssh", spec},
			addrs: []string{spec, a.bind.target.Key()}})
	}
	shown, auto := filterHosts(entries, a.hostQuery)
	a.hostSel = min(a.hostSel, len(shown))
	// pick is the row Enter switches to: the one the arrows chose, else
	// the one the text picks.
	pick := func() int {
		if a.hostSel > 0 {
			return shown[a.hostSel-1]
		}
		return auto
	}

	connect := func() {
		if p := pick(); p >= 0 {
			e := entries[p]
			a.hostQuery, a.hostErr, a.hostSel, a.hostOpen = "", "", 0, false
			if !e.current {
				a.later(c, e.open)
			}
			return
		}
		spec := hostQueryText(a.hostQuery)
		if spec == "" {
			return
		}
		a.later(c, func() {
			if err := a.connectSpec(spec); err != nil {
				a.hostErr = err.Error()
				return
			}
			a.hostQuery, a.hostErr, a.hostOpen = "", "", false
		})
	}
	// reveal scrolls the row the arrows chose into view, in the frame they
	// move it only, that the wheel scrolls the list.
	reveal := false
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		field := ui.Row(c).Grow(1).MinWidth(0).Height(30).Padding(0, 10).Radius(8).Border(1, k.panelBorder).AlignItems(ui.Center)
		field.Children(func() {
			// The popover opens with the keyboard here: click the chip,
			// type a host's name or a destination, Enter.
			in := ui.TextInputBase(c, &a.hostQuery).Grow(1).FontSize(12.5).Placeholder("Filter hosts, or ssh user@host").AutoFocus()
			if in.Changed() {
				a.hostErr, a.hostSel = "", 0
				shown, auto = filterHosts(entries, a.hostQuery)
			}
			if in.Shortcut(0, ui.KeyDown) && len(shown) > 0 {
				a.hostSel, reveal = min(a.hostSel+1, len(shown)), true
			}
			if in.Shortcut(0, ui.KeyUp) && a.hostSel > 0 {
				// Up from the first row leaves the rows to the text.
				a.hostSel, reveal = a.hostSel-1, true
			}
			// Escape clears the text first, then closes the popover.
			if a.hostQuery != "" && in.Shortcut(0, ui.KeyEscape) {
				a.hostQuery, a.hostErr, a.hostSel = "", "", 0
				shown, auto = filterHosts(entries, "")
			}
			if in.Submitted() {
				connect()
			}
		})
		b := ui.Box(c).Height(30).Padding(0, 12).Radius(8).Center().Background(k.hover).Cursor(ui.CursorPointer).
			Role(ui.RoleButton).Label("Connect")
		if b.Pressed() {
			b.Background(k.pressed)
		}
		if b.Clicked() {
			connect()
		}
		b.Children(func() {
			ui.Text(c, "Connect").FontSize(12.5).FontWeight(600).TextColor(k.text)
		})
	})
	// What Enter does, as the one field both filters and connects.
	hint := ""
	switch p := pick(); {
	case p >= 0 && entries[p].current:
		hint = "↵ stay on " + entries[p].title
	case p >= 0:
		hint = "↵ switch to " + entries[p].title
	case hostQueryText(a.hostQuery) != "":
		hint = "↵ connect with ssh to " + hostQueryText(a.hostQuery)
	}
	if hint != "" {
		ui.Text(c, hint).FontSize(11.5).TextColor(k.textFaint).SingleLine().Ellipsis("…")
	}
	if a.hostErr != "" {
		ui.Text(c, a.hostErr).FontSize(11.5).TextColor(ui.Hex("#ef4444")).LineHeight(1.35)
	}
	ui.Column(c).Gap(2).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(0, 0, 4, 0).Children(func() {
			ui.Text(c, "HOSTS").FontSize(11).FontWeight(700).TextColor(k.textFaint).Grow(1)
			if textButton(c, k, "Add Host…").Clicked() {
				a.later(c, func() { a.editHost("", false) })
			}
			if textButton(c, k, "Settings…").Clicked() {
				a.hostOpen = false
				a.later(c, a.openSettings)
			}
		})
		// Each row's actions stand in columns of their own, a gap where a
		// host has none: its settings, a window of its own (Reconnect for
		// the window's own host), and forgetting it, which asks first.
		slot := func() { ui.Box(c).Size(24, 24).Shrink(0) }
		if len(shown) == 0 {
			ui.Text(c, "No saved hosts match.").FontSize(12).TextColor(k.textFaint).Padding(6, 6)
		}
		picked := pick()
		for _, i := range shown {
			e := entries[i]
			row := ui.Row(c.Key("host/"+e.key+"/"+e.title)).Height(34).Padding(0, 4, 0, 6).Gap(4).Radius(8).AlignItems(ui.Center)
			if e.key != "" && a.hostForget == e.key {
				row.Background(k.hover).Children(func() {
					ui.Text(c, "Forget "+e.title+"?").FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
					if textButton(c, k, "Cancel").Clicked() {
						a.hostForget = ""
					}
					if textButton(c, k, "Forget").Clicked() {
						forgetRemote(e.key)
						a.remotes, a.hostForget = loadRemotes(), ""
					}
				})
				continue
			}
			// One row is blue: the one Enter switches to, else the
			// window's own, which keeps its tick.
			switch {
			case i == picked, e.current && picked < 0:
				row.Background(k.panelSel)
			case row.Hovered():
				row.Background(k.hover)
			}
			if i == picked && reveal {
				row.ScrollIntoView()
			}
			row.Children(func() {
				main := ui.Row(c).Grow(1).MinWidth(0).Height(34).Gap(8).AlignItems(ui.Center).
					Role(ui.RoleButton).Label("Switch to " + e.title)
				if !e.current {
					main.Cursor(ui.CursorPointer)
					if main.Clicked() {
						a.hostOpen = false
						a.later(c, e.open)
					}
				}
				main.Children(func() {
					// The host's icon; the window's own host has a dot of
					// its connection's color on it, as the popover's top.
					ui.Box(c).Size(16, 16).Center().Shrink(0).Children(func() {
						ui.Icon(c, icon(e.glyph)).Size(16, 16).TextColor(k.textMuted)
						if e.current {
							dot, _ := a.connState(k)
							ui.Box(c).Absolute().Right(-2).Bottom(-2).Size(7, 7).Radius(4).Background(dot).Border(1, k.panel)
						}
					})
					ui.Text(c, e.title).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(300)
					ui.Text(c, e.detail+e.age).FontSize(12).TextColor(k.textFaint).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				})
				switch {
				case e.pc:
					// This machine is reached by no connection: its
					// settings are the Defaults.
					if b := iconButton(c, k, "settings-2", "This PC's Settings", 24, 13).Tooltip("This PC's Settings"); b.Clicked() {
						a.hostOpen = false
						a.later(c, func() {
							a.settings.page = scopeDefaults + "/Appearance"
							a.openSettings()
						})
					}
				case e.key == "":
					slot()
				default:
					if b := iconButton(c, k, "settings-2", "Host Settings", 24, 13).Tooltip("Host Settings"); b.Clicked() {
						a.later(c, func() { a.editHost(e.key, false) })
					}
				}
				if e.current {
					if b := iconButton(c, k, "rotate-ccw", "Reconnect", 24, 13).Tooltip("Reconnect"); b.Clicked() {
						a.hostOpen = false
						a.later(c, func() { a.switchTo(a.bind, true) })
					}
				} else if b := iconButton(c, k, "app-window", "Open in New Window", 24, 13).Tooltip("Open in New Window"); b.Clicked() {
					a.hostOpen = false
					if err := launchWindow(e.window); err != nil {
						a.err = "Can't open a window: " + err.Error()
					}
				}
				if e.key == "" || e.current {
					slot()
				} else if b := iconButton(c, k, "x", "Forget", 24, 12).Tooltip("Forget"); b.Clicked() {
					a.hostForget = e.key
				}
			})
		}
	})
}

// openHost switches the window to the host of an ssh destination, or to
// this machine's for "".
func (a *App) openHost(spec string) {
	if spec == "" {
		a.switchTo(binding{}, false)
		return
	}
	if err := a.connectSpec(spec); err != nil {
		a.err = err.Error()
	}
}

// launchWindow opens another window, as another GoRex run with args: on
// a host (ssh <destination>), a serial host (serial <key>), or on this
// machine's sessions for none.
func launchWindow(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// thisMachine names the machine the app runs on, as its system does.
func thisMachine() string {
	switch runtime.GOOS {
	case "darwin":
		return "This Mac"
	case "windows":
		return "This PC"
	}
	return "This computer"
}
