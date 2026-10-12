package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// hostChip shows the machine the sessions run on; a click shows more of
// it, and of the session server.
func (a *App) hostChip(c *ui.Context, k *colors) {
	h := a.hello.Host
	name, model := h.Name, h.Model
	if a.eff.Appearance.Name != "" {
		name = a.eff.Appearance.Name
	}
	if name == "" && a.bind.remote {
		name = a.bind.target.Spec
	} else if name == "" {
		name, _ = os.Hostname()
		name = strings.TrimSuffix(name, ".local")
	}
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
			a.remotes, a.hostErr = loadRemotes(), ""
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
		panel.Margin(8, 0, 0, 0).Width(320).Padding(14).Radius(14).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 12, 32, 0, k.shadowFocused).Gap(12)
		a.hostPanel(c, k, name, glyph)
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
				if a.bind.remote {
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
		if a.bind.remote {
			row("Connection", "ssh "+a.bind.target.Key())
		}
		row("Model", h.Model)
		row("Chip", h.Chip)
		if h.Memory > 0 {
			row("Memory", fmt.Sprintf("%d GB", h.Memory>>30))
		}
		row("System", h.OS)
		row("User", h.User)
	})
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
// machine, those connected to before, and one to type.
func (a *App) hostsSection(c *ui.Context, k *colors) {
	type entry struct {
		title, detail string
		current       bool
		// open switches the window to the host; spec is its ssh
		// destination, "" for this machine; key, to forget it.
		spec, key string
	}
	entries := []entry{{title: thisMachine(), detail: "local sessions", current: !a.bind.remote}}
	listed := false
	for _, r := range a.remotes {
		cur := "ssh/"+r.Key == a.bind.key()
		listed = listed || cur
		detail := r.Target
		if !cur {
			detail += " · " + roundDur(time.Since(r.LastUsed)) + " ago"
		}
		entries = append(entries, entry{title: r.title(), detail: detail, current: cur, spec: r.Target, key: r.Key})
	}
	if a.bind.remote && !listed {
		// A host the window connects to for the first time.
		entries = append(entries, entry{title: a.bind.target.Spec, detail: a.bind.target.Key(), current: true, spec: a.bind.target.Spec})
	}

	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, "HOSTS").FontSize(11).FontWeight(700).TextColor(k.textFaint).Padding(0, 0, 4, 0)
		for _, e := range entries {
			row := ui.Row(c.Key("host/"+e.spec)).Height(34).Padding(0, 4, 0, 6).Gap(4).Radius(8).AlignItems(ui.Center)
			if e.current {
				row.Background(k.panelSel)
			} else if row.Hovered() {
				row.Background(k.hover)
			}
			hovered := row.Hovered()
			row.Children(func() {
				main := ui.Row(c).Grow(1).MinWidth(0).Height(34).Gap(8).AlignItems(ui.Center).
					Role(ui.RoleButton).Label("Switch to " + e.title)
				if !e.current {
					main.Cursor(ui.CursorPointer)
					if main.Clicked() {
						a.hostOpen = false
						a.later(c, func() { a.openHost(e.spec) })
					}
				}
				main.Children(func() {
					ui.Box(c).Size(14, 14).Center().Shrink(0).Children(func() {
						if e.current {
							ui.Icon(c, icon("check")).Size(14, 14).TextColor(k.text)
						}
					})
					ui.Text(c, e.title).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(150)
					ui.Text(c, e.detail).FontSize(12).TextColor(k.textFaint).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				})
				if e.current {
					if b := iconButton(c, k, "rotate-ccw", "Reconnect", 24, 13).Tooltip("Reconnect"); b.Clicked() {
						a.hostOpen = false
						a.later(c, func() { a.switchTo(a.bind, true) })
					}
					return
				}
				if b := iconButton(c, k, "app-window", "Open in New Window", 24, 13).Tooltip("Open in New Window"); b.Clicked() {
					a.hostOpen = false
					if err := launchWindow(e.spec); err != nil {
						a.err = "Can't open a window: " + err.Error()
					}
				}
				if e.key == "" {
					return
				}
				// Forgetting shows on hover; its room stays, so that the
				// button beside it doesn't move.
				if !hovered {
					ui.Box(c).Size(24, 24).Shrink(0)
					return
				}
				if b := iconButton(c, k, "x", "Forget", 24, 12).Tooltip("Forget"); b.Clicked() {
					forgetRemote(e.key)
					a.remotes = loadRemotes()
				}
			})
		}
	})

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
			in := ui.TextInputBase(c, &a.hostQuery).Grow(1).FontSize(12.5).Placeholder("ssh user@host")
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

// launchWindow opens another window, as another GoRex, on the host of an
// ssh destination, or on this machine's for "".
func launchWindow(spec string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var args []string
	if spec != "" {
		args = []string{"ssh", spec}
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
