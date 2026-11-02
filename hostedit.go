package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/remote"
)

// hostForm is a host's settings as the host dialog edits them: of the
// saved host of key old, or, with old "", a host to add. Kind is the
// index of the choice among hostKinds. fromSettings brings the settings
// back when the dialog closes, as they close for it.
type hostForm struct {
	old          string
	orig         savedRemote
	kind         int
	label        string
	target       string
	identity     string
	options      string
	serverDir    string
	command      string
	agent        bool
	err          string
	busy         bool
	fromSettings bool
}

// hostKinds are the ways a host can be reached, as the host dialog
// offers them; the serial port's is to come.
var hostKinds = []string{"GoRex's server", "Plain ssh", "Serial port"}

const (
	formServer = iota
	formPlain
	formSerial
)

// editHost opens the host dialog on the saved host of key, or on a new
// one for "". From the settings, which close for it, and come back after.
func (a *App) editHost(key string, fromSettings bool) {
	f := &hostForm{old: key, fromSettings: fromSettings}
	if r, ok := findRemote(key); ok && key != "" {
		f.orig = r
		f.label, f.target, f.identity, f.options = r.Label, r.Target, r.Identity, r.Options
		f.serverDir, f.command, f.agent = r.ServerDir, r.Command, r.ForwardAgent
		if r.Kind == kindPlain {
			f.kind = formPlain
		}
	}
	a.hostForm = f
	a.hostOpen = false
	if fromSettings {
		a.settings.open = false
	}
}

// closeHostForm closes the host dialog, and brings back the settings it
// was opened from.
func (a *App) closeHostForm() {
	if f := a.hostForm; f != nil && f.fromSettings {
		a.openSettings()
	}
	a.hostForm = nil
}

// saveHost saves the host as the dialog has it, under the key ssh makes
// of its destination and options, and then, with connect, connects the
// window to it: again, as its settings now say, when it is the one the
// window shows.
func (a *App) saveHost(f *hostForm, connect bool) {
	spec := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(f.target), "ssh "))
	if spec == "" {
		f.err = "Name the host: user@host, or a Host of ~/.ssh/config."
		return
	}
	r := f.orig
	r.Target, r.Label = spec, strings.TrimSpace(f.label)
	r.Identity, r.Options = strings.TrimSpace(f.identity), strings.TrimSpace(f.options)
	r.ServerDir, r.Command = strings.TrimSpace(f.serverDir), strings.TrimSpace(f.command)
	r.ForwardAgent = f.agent
	r.Kind = kindServer
	if f.kind == formPlain {
		r.Kind = kindPlain
	} else {
		r.Note = ""
	}
	if r.LastUsed.IsZero() {
		r.LastUsed = time.Now()
	}
	f.busy, f.err = true, ""
	win := a.win
	go func() {
		t, err := remote.Resolve(spec, r.sshOpts())
		win.Update(func() {
			f.busy = false
			if a.hostForm != f {
				return
			}
			if err != nil {
				f.err = "ssh makes no host of " + spec + ": " + err.Error()
				a.changed()
				return
			}
			r.Key = t.Key()
			putRemote(f.old, r)
			a.remotes = loadRemotes()
			if a.bind.host() && r.Key == a.bind.target.Key() {
				a.hostLabel = r.Label
			}
			if connect {
				// The window connects, and what connecting asks shows
				// over it: the settings stay closed.
				f.fromSettings = false
			}
			a.closeHostForm()
			if connect {
				if b, err := hostBinding(spec, ""); err != nil {
					a.err = "Can't connect to " + spec + ": " + err.Error()
				} else {
					a.switchTo(b, b.key() == a.bind.key())
				}
			}
			a.changed()
		})
	}()
}

// hostDialog draws the host dialog: its name, how it is reached, ssh's
// destination and options, and what the kind adds, the folder of GoRex's
// server there or the command of a plain host's tabs.
func (a *App) hostDialog(c *ui.Context, k *colors) {
	f := a.hostForm
	if f == nil {
		return
	}
	open := true
	title := "Add a host"
	if f.old != "" {
		title = f.orig.title()
	}
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(560).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Padding(18, 20).Gap(12).Clip()
		ui.Text(c, title).FontSize(14).FontWeight(700).TextColor(k.text)
		row := func(label string, field func()) {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).FontSize(12.5).TextColor(k.textMuted).Width(130).Shrink(0)
				ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(field)
			})
		}
		input := func(v *string, placeholder string, focus bool) {
			in := ui.TextInput(c, v).FillWidth().FontSize(13).Placeholder(placeholder)
			if focus {
				in.AutoFocus()
			}
			if in.Changed() {
				f.err = ""
			}
			if in.Submitted() && !f.busy && f.kind != formSerial {
				a.later(c, func() { a.saveHost(f, false) })
			}
		}
		help := func(s string) {
			ui.Text(c, s).FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35).Padding(0, 0, 0, 142)
		}
		row("Connect with", func() {
			ui.Segmented(c, &f.kind, hostKinds...)
		})
		switch f.kind {
		case formServer:
			help("GoRex copies its server to the host: sessions stay when the connection goes, and its files and git show in GoRex.")
		case formPlain:
			help("The host's tabs run ssh from this PC: nothing is copied there, and a tab's session ends with its connection.")
		}
		if f.kind == formSerial {
			ui.Text(c, "Serial ports, as a switch's or a router's console, are to come: their settings are TBA.").
				FontSize(12.5).TextColor(k.textMuted).LineHeight(1.45)
		} else {
			row("Name", func() { input(&f.label, "as the host names itself", false) })
			row("Destination", func() { input(&f.target, "user@host, or a Host of ~/.ssh/config", f.old == "") })
			row("Identity file", func() { input(&f.identity, "ssh's default keys", false) })
			row("More ssh options", func() { input(&f.options, "-p 2222 -o ProxyJump=bastion", false) })
			row("Forward ssh agent", func() { ui.Switch(c, &f.agent) })
			switch f.kind {
			case formServer:
				row("Server folder", func() { input(&f.serverDir, "~/.cache/gorex", false) })
				help("Where on the host GoRex keeps its server, which must run programs there (not mounted noexec). A folder GoRex makes is the user's alone.")
			case formPlain:
				row("Command", func() { input(&f.command, "the login shell", false) })
				if f.orig.Note != "" {
					help("Plain since: " + f.orig.Note)
				}
			}
		}
		if f.err != "" {
			ui.Text(c, f.err).FontSize(12).TextColor(ui.Hex("#ef4444")).LineHeight(1.35).Selectable()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if textButton(c, k, "Cancel").Clicked() {
				a.later(c, a.closeHostForm)
			}
			if f.kind == formSerial || f.busy {
				return
			}
			if textButton(c, k, "Save").Clicked() {
				a.later(c, func() { a.saveHost(f, false) })
			}
			if textButton(c, k, "Save and Connect").Clicked() {
				a.later(c, func() { a.saveHost(f, true) })
			}
		})
	})
	if !open {
		// Escape, or a click beside the dialog.
		a.later(c, a.closeHostForm)
	}
}
