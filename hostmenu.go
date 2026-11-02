package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"gorex/internal/remote"
)

// hostMenu is the host chip's context menu: what the window can do with
// the host it shows, then the hosts to switch to.
func (a *App) hostMenu(m *ui.Menu) {
	b := a.bind
	if m.Item("Reconnect").Chosen() {
		a.switchTo(b, true)
	}
	settings := m.Item(cmdHostSettings.Title)
	if !cmdHostSettings.Shown(a) {
		// A host connected to for the first time has none yet.
		settings.Disabled(true)
	}
	if settings.Chosen() {
		a.hostSettings()
	}
	if m.Item("Open in New Window").Chosen() {
		if err := launchWindow(a.windowArgs()); err != nil {
			a.err = "Can't open a window: " + err.Error()
		}
	}
	if addr := a.hostAddress(); addr != "" && m.Item("Copy Address").Chosen() {
		a.services.WriteClipboard(addr)
	}
	if a.bind.host() && m.Item("Copy ssh Command").Chosen() {
		a.services.WriteClipboard(sshCommand(a.bind.target))
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
	if m.Item(cmdRestartServer.Title).Disabled(!cmdRestartServer.Shown(a)).Chosen() {
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

// hostSettings opens the settings of the window's host: its dialog, or
// for this machine the Defaults, which are its own.
func (a *App) hostSettings() {
	if a.bind.away() {
		if key := a.hostKey(); key != "" {
			a.editHost(key, false)
		}
		return
	}
	a.settings.page = scopeDefaults + "/Appearance"
	a.openSettings()
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

// hostAddress is what reaches the window's host: as ssh takes it after
// "ssh " (sshAddress), or a serial port's name; "" for this machine.
func (a *App) hostAddress() string {
	switch b := a.bind; {
	case b.serial != "":
		r, _ := findRemote(b.serial)
		return r.Port
	case b.host():
		return sshAddress(b.target)
	}
	return ""
}

// sshAddress is the host of t as ssh takes it after "ssh ": user@host, or
// ssh://user@host:port on a port other than 22.
func sshAddress(t remote.Target) string {
	if t.Port == "" || t.Port == "22" {
		return t.User + "@" + t.Host
	}
	host := t.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "ssh://" + t.User + "@" + host + ":" + t.Port
}

// sshCommand is an ssh command line that reaches the host of t as GoRex
// does, to paste in another terminal: the host's options (its identity
// file and more), its port when not 22, -A where the agent goes along,
// and user@host.
func sshCommand(t remote.Target) string {
	args := []string{"ssh"}
	for i := 0; i < len(t.Opts); i++ {
		o := t.Opts[i]
		if o == "-p" {
			// The port, as ssh resolved it, follows.
			i++
			continue
		}
		if strings.HasPrefix(o, "-p") {
			continue
		}
		args = append(args, shellQuote(o))
	}
	if t.Port != "" && t.Port != "22" {
		args = append(args, "-p", t.Port)
	}
	if t.Agent == "yes" || t.Agent == "" && forwardsAgent(t.Key()) {
		args = append(args, "-A")
	}
	return strings.Join(append(args, shellQuote(t.User+"@"+t.Host)), " ")
}

// shellQuote quotes s for a POSIX shell when it has more than letters,
// digits and the punctuation of paths and options.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./_-~") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
