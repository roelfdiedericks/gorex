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
	serial       serialForm
	err          string
	busy         bool
	fromSettings bool
	// ppk is the panel of the PuTTY key the identity file names, or the
	// host was imported with (ppkconvert.go).
	ppk *ppkForm
}

// hostKinds are the ways a host can be reached, as the host dialog
// offers them.
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
	r, ok := findRemote(key)
	if ok && key != "" {
		f.orig = r
		f.label, f.target, f.identity, f.options = r.Label, r.Target, r.Identity, r.Options
		f.serverDir, f.command, f.agent = r.ServerDir, r.Command, r.ForwardAgent
		switch r.Kind {
		case kindPlain:
			f.kind = formPlain
		case kindSerial:
			f.kind = formSerial
		}
	}
	// A serial port's settings, or those it starts with: 115200 8N1.
	f.serial.set(f.orig)
	a.hostForm = f
	a.hostOpen = false
	if fromSettings {
		a.settings.open = false
	}
}

// closeHostForm closes the host dialog, and brings back the settings it
// was opened from.
func (a *App) closeHostForm() {
	f := a.hostForm
	if f != nil {
		f.stopPorts()
	}
	if f != nil && f.fromSettings {
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
		f.err = "Enter a destination: user@host, or a Host from ~/.ssh/config."
		return
	}
	r := f.orig
	if r.Kind == kindSerial {
		// A serial port's host that becomes one of ssh keeps none of
		// the port's settings.
		r = savedRemote{LastUsed: r.LastUsed}
	}
	r.Target, r.Label = spec, strings.TrimSpace(f.label)
	r.Identity, r.Options = strings.TrimSpace(f.identity), strings.TrimSpace(f.options)
	if r.Identity != "" {
		// The key the host was imported with is replaced.
		r.PuttyKey = ""
	}
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
				f.err = "ssh can't resolve " + spec + ": " + err.Error()
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
// server there or the command of a plain host's tabs; or a serial port's
// settings (serialFields).
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
			help("GoRex copies its server to the host. Sessions keep running when the connection drops, and GoRex shows the host's files and git.")
		case formPlain:
			help("The host's tabs run ssh on this PC. Nothing is copied to the host, and a tab's session ends when its connection ends.")
		case formSerial:
			help("A serial port on this PC, such as the console of a switch or router. Its single session stays open while GoRex is closed.")
		}
		if f.kind == formSerial {
			a.serialFields(c, k, f, row, help)
		} else {
			f.stopPorts()
			row("Name", func() { input(&f.label, "the name the host reports", false) })
			row("Destination", func() { input(&f.target, "user@host, or a Host from ~/.ssh/config", f.old == "") })
			row("Identity file", func() { input(&f.identity, "ssh's default keys", false) })
			// A PuTTY key, which ssh can't read: in the agent, or to
			// convert, which makes the converted file the identity.
			id, key := strings.TrimSpace(f.identity), ""
			switch {
			case isPPK(id):
				key = id
			case id == "" && f.orig.PuttyKey != "":
				key = f.orig.PuttyKey
			}
			if key != "" && (f.ppk == nil || f.ppk.path != key) {
				f.ppk = a.newPPKForm(key)
			}
			if f.ppk != nil && (key == f.ppk.path || f.ppk.done != "" && id == f.ppk.done) {
				a.ppkPanel(c, k, f.ppk, 142, func(out string) { f.identity = out })
			}
			row("More ssh options", func() { input(&f.options, "-p 2222 -o ProxyJump=bastion", false) })
			row("Forward ssh agent", func() { ui.Switch(c, &f.agent) })
			switch f.kind {
			case formServer:
				row("Server folder", func() { input(&f.serverDir, "~/.cache/gorex", false) })
				help("The folder on the host where GoRex keeps its server. Programs must be able to run there, so it can't be on a noexec mount. If GoRex creates the folder, only your user can use it.")
			case formPlain:
				row("Command", func() { input(&f.command, "the login shell", false) })
				if f.orig.Note != "" {
					help("Switched to plain ssh because: " + f.orig.Note)
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
			if f.busy {
				return
			}
			save := a.saveHost
			if f.kind == formSerial {
				save = a.saveSerial
			}
			if textButton(c, k, "Save").Clicked() {
				a.later(c, func() { save(f, false) })
			}
			if textButton(c, k, "Save and Connect").Clicked() {
				a.later(c, func() { save(f, true) })
			}
		})
	})
	if !open {
		// Escape, or a click beside the dialog.
		a.later(c, a.closeHostForm)
	}
}
