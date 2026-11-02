package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// hostMenu is the host chip's context menu: what the window can do with
// the host it shows, then the hosts to switch to.
func (a *App) hostMenu(m *ui.Menu) {
	b := a.bind
	if m.Item("Reconnect").Chosen() {
		a.switchTo(b, true)
	}
	settings := m.Item("Host Settings…")
	key := a.hostKey()
	if b.away() && key == "" {
		// A host connected to for the first time has none yet.
		settings.Disabled(true)
	}
	if settings.Chosen() {
		if b.away() {
			a.editHost(key, false)
		} else {
			// This machine's settings are the Defaults.
			a.settings.page = scopeDefaults + "/Appearance"
			a.openSettings()
		}
	}
	if m.Item("Open in New Window").Chosen() {
		if err := launchWindow(a.windowArgs()); err != nil {
			a.err = "Can't open a window: " + err.Error()
		}
	}
	if addr := a.hostAddress(); addr != "" && m.Item("Copy Address").Chosen() {
		a.services.WriteClipboard(addr)
	}
	if m.Item("Connection Log…").Chosen() {
		a.logOpen = true
	}
	if b.serial != "" {
		if m.Item(cmdSendBreak.Title).Chosen() {
			a.sendBreak()
		}
		if m.Item(cmdReleasePort.Title).Chosen() {
			a.releasePort()
		}
	}
	m.Separator()
	// The server answers the restart: not connected, there is none to.
	if m.Item("Restart Session Server…").Disabled(a.client == nil).Chosen() {
		a.restartAsk = true
	}
	m.Separator()
	m.Submenu("Switch to", func(m *ui.Menu) {
		// The hosts used last, as many as a menu shows well; the
		// popover lists them all.
		const most = 10
		n := 0
		if b.away() {
			n++
			if m.Item(thisMachine()).Chosen() {
				a.openHost("")
			}
		}
		for _, r := range a.remotes {
			if r.bindKey() == b.key() {
				continue
			}
			if n == most {
				if m.Item("More…").Chosen() {
					a.remotes, a.hostErr, a.hostForget, a.hostOpen = loadRemotes(), "", "", true
				}
				break
			}
			n++
			if m.Item(r.title()).Chosen() {
				a.openSaved(r)
			}
		}
	})
	if m.Item("Add Host…").Chosen() {
		a.editHost("", false)
	}
	if m.Item("Settings…").Chosen() {
		a.openSettings()
	}
}

// hostKey is the key of the window's host among the saved ones, "" for
// this machine and for a host not saved.
func (a *App) hostKey() string {
	key := a.bind.serial
	if a.bind.host() {
		key = a.bind.target.Key()
	}
	if _, ok := findRemote(key); !ok || key == "" {
		return ""
	}
	return key
}

// windowArgs are GoRex's arguments for another window on the window's
// host, reached as this one is.
func (a *App) windowArgs() []string {
	b := a.bind
	switch {
	case b.serial != "":
		return []string{"serial", b.serial}
	case b.plain:
		return []string{"ssh", "--plain", b.target.Spec}
	case b.remote:
		return []string{"ssh", "--server", b.target.Spec}
	}
	return nil
}

// hostAddress is what reaches the window's host: user@host:port, a serial
// port's name; "" for this machine.
func (a *App) hostAddress() string {
	switch b := a.bind; {
	case b.serial != "":
		r, _ := findRemote(b.serial)
		return r.Port
	case b.host():
		return b.target.Key()
	}
	return ""
}

// restartDialog asks before the window's session server restarts: in
// place where the server can replace itself, its sessions going on, else
// a new server, which ends them, as the dialog says.
func (a *App) restartDialog(c *ui.Context, k *colors) {
	if !a.restartAsk {
		return
	}
	if a.client == nil {
		// The connection went meanwhile.
		a.restartAsk = false
		return
	}
	n := 0
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.SID != "" {
				n++
			}
		}
	}
	sessions := fmt.Sprintf("Its %d sessions", n)
	if n == 1 {
		sessions = "Its session"
	}
	// A server on Windows can't replace itself in place: a new one
	// starts.
	inPlace := a.hello.Host.GOOS != "" && a.hello.Host.GOOS != "windows"
	what, action := sessions+" end: their programs are closed.", "End Sessions and Restart"
	if inPlace {
		what, action = sessions+" keep running: the server replaces itself in place.", "Restart"
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(440).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Padding(18, 20).Gap(12).Clip()
		ui.Text(c, "Restart the session server of "+a.hostName()+"?").FontSize(14).FontWeight(700).TextColor(k.text)
		ui.Text(c, what).FontSize(12.5).TextColor(k.text).LineHeight(1.45)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if textButton(c, k, "Cancel").Clicked() {
				a.restartAsk = false
			}
			if textButton(c, k, action).Clicked() {
				a.restartAsk = false
				a.later(c, a.restartServer)
			}
		})
	})
	if !open {
		// Escape, or a click beside the dialog.
		a.restartAsk = false
	}
}
