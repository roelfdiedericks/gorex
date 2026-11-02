package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
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

// hostChip shows the machine the sessions run on; a click shows more of
// it, and of the session server.
func (a *App) hostChip(c *ui.Context, k *colors) {
	h := a.hello.Host
	name, model := a.hostName(), h.Model
	// The server's system, or while the window connects, the one it will
	// likely have.
	goos := h.GOOS
	if goos == "" {
		goos = runtime.GOOS
		if a.bind.remote {
			goos = "linux"
		}
	}
	glyph := "mac-studio"
	switch {
	case a.bind.serial != "":
		// The server, of a serial port, is this machine's.
		glyph, model = "cable", "serial · "+a.bind.port+" · "+a.bind.line
	case a.bind.plain:
		// What the server tells is this machine's, not the host's.
		glyph, model = "globe", "plain ssh"
	case strings.Contains(model, "MacBook"), h.Portable:
		glyph = "laptop"
	case strings.Contains(model, "iMac"), goos == "windows":
		glyph = "monitor"
	case model == "Linux", goos == "linux":
		glyph = "server"
	}
	chip := ui.Row(c).Gap(9).Padding(3, 8, 3, 6).Radius(9).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label("Host " + name).MaxWidth(190).Shrink(0)
	if chip.Hovered() || a.hostOpen {
		chip.Background(k.hover)
	}
	if chip.Clicked() {
		a.hostOpen = !a.hostOpen
		if a.hostOpen {
			a.remotes, a.hostErr, a.hostForget = loadRemotes(), "", ""
		}
	}
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
				dot, state := k.busy, "connected"
				switch {
				case a.connDown:
					dot, state = ui.Hex("#ef4444"), "not connected"
				case a.client == nil:
					dot, state = k.attention, "connecting…"
				case a.status != "":
					dot, state = k.attention, "reconnecting…"
				}
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
		ui.Text(c, "A plain ssh host: its tabs run ssh from this PC, and GoRex shows none of its files or git.").
			FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
	case a.bind.serial != "":
		ui.Text(c, "A serial port of this PC: its session stays open while GoRex is closed, and waits for the port when it is unplugged.").
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
		ui.Text(c, "Sessions live in a server on this PC: quit GoRex and they keep running, and come back as they were when it opens again, as long as their connections last.").
			FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
		return
	}
	if a.bind.serial != "" {
		return
	}
	ui.Text(c, "Sessions live in the server: quit GoRex and they keep running, and come back as they were when it opens again.").
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

// hostsSection lists the hosts the window can show the sessions of: this
// machine, those connected to before, the serial hosts, and one to type.
func (a *App) hostsSection(c *ui.Context, k *colors) {
	type entry struct {
		title, detail string
		current       bool
		// open switches the window to the host, and window opens it in
		// a window of its own, as GoRex's arguments; key is a saved
		// host's, for its settings and to forget it; pc marks this
		// machine, whose settings are the Defaults.
		open   func()
		window []string
		key    string
		pc     bool
	}
	entries := []entry{{title: thisMachine(), detail: "local sessions", current: !a.bind.away(), open: func() { a.openHost("") }, pc: true}}
	listed := false
	for _, r := range a.remotes {
		cur := r.bindKey() == a.bind.key()
		listed = listed || cur
		detail, window := r.Target, []string{"ssh", r.Target}
		switch r.Kind {
		case kindPlain:
			detail = "plain ssh · " + detail
		case kindSerial:
			c := r.config()
			detail, window = "serial · "+r.Port+" · "+strconv.Itoa(c.Baud)+" "+c.Framing(), []string{"serial", r.Key}
		}
		if !cur {
			detail += " · " + roundDur(time.Since(r.LastUsed)) + " ago"
		}
		entries = append(entries, entry{title: r.title(), detail: detail, current: cur, open: func() { a.openSaved(r) }, window: window, key: r.Key})
	}
	if a.bind.host() && !listed {
		// A host the window connects to for the first time.
		spec := a.bind.target.Spec
		entries = append(entries, entry{title: spec, detail: a.bind.target.Key(), current: true, open: func() { a.openHost(spec) }, window: []string{"ssh", spec}})
	}

	connect := func() {
		spec := strings.TrimSpace(a.hostQuery)
		spec = strings.TrimSpace(strings.TrimPrefix(spec, "ssh "))
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
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		field := ui.Row(c).Grow(1).MinWidth(0).Height(30).Padding(0, 10).Radius(8).Border(1, k.panelBorder).AlignItems(ui.Center)
		field.Children(func() {
			// The popover opens with the keyboard here: click the chip,
			// type a destination, Enter.
			in := ui.TextInputBase(c, &a.hostQuery).Grow(1).FontSize(12.5).Placeholder("ssh user@host").AutoFocus()
			if in.Changed() {
				a.hostErr = ""
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
		for _, e := range entries {
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
			if e.current {
				row.Background(k.panelSel)
			} else if row.Hovered() {
				row.Background(k.hover)
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
					ui.Box(c).Size(14, 14).Center().Shrink(0).Children(func() {
						if e.current {
							ui.Icon(c, icon("check")).Size(14, 14).TextColor(k.text)
						}
					})
					ui.Text(c, e.title).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(300)
					ui.Text(c, e.detail).FontSize(12).TextColor(k.textFaint).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
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
