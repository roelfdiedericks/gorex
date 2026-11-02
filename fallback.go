package main

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"gorex/internal/remote"
)

// When GoRex's server can't run on a host where ssh works, the window
// falls back: to another folder there, where the one the server lands in
// runs no programs (noexec), or to plain ssh. What it does for a host the
// user has not decided for is General.ServerFallback's: ask, try other
// folders then plain ssh, or plain ssh. Either way the host's settings
// keep what was done, its folder (savedRemote.ServerDir) or its kind
// (kindPlain, with a note of why), which later connections follow, and
// its settings change.

// cantRunAsk is the window's question when GoRex's server can't run on
// the host of b, while connecting to it (gen): with reason, and whether
// another folder may do.
type cantRunAsk struct {
	b       binding
	reason  string
	folders bool
	gen     int64
}

// serverCantRun does what the settings say for a host where GoRex's
// server can't run, for the reason e. Another folder may do where the
// server does not start, but not when a folder of the host's settings is
// the one that failed.
func (a *App) serverCantRun(b binding, e *remote.StopError) {
	folders := e.CantRun == remote.NoStart && b.target.Dir == ""
	switch a.eff.General.ServerFallback {
	case "plain":
		a.goPlain(b, e.Reason)
	case "folders":
		if folders {
			a.findFolder(b, e.Reason)
		} else {
			a.goPlain(b, e.Reason)
		}
	default:
		a.cantRun = &cantRunAsk{b: b, reason: e.Reason, folders: folders, gen: a.gen.Load()}
	}
}

// goPlain makes the host of b a plain host, why the note of it, and
// connects the window to it so.
func (a *App) goPlain(b binding, why string) {
	editRemote(b.target.Key(), b.target.Spec, func(r *savedRemote) { r.Kind, r.Note = kindPlain, why })
	a.logConn("connecting to " + b.label() + " with plain ssh, as from now on: the host's settings change it")
	a.switchTo(binding{plain: true, target: b.target}, true)
}

// findFolder looks for a folder on the host of b where programs run,
// for GoRex's server, which the host's settings then keep, and connects
// the window to the server there; with none, it makes the host a plain
// one, the note of it why.
func (a *App) findFolder(b binding, why string) {
	win, gen := a.win, a.gen.Load()
	a.status, a.connDown = "Looking for a folder on "+b.label()+" where GoRex's server can run…", false
	a.logConn("looking for a folder on " + b.label() + " where programs run")
	go func() {
		login := a.loginSession(win, b, gen)
		dir, err := remote.FindDir(b.target, a.logConn, login)
		if login != nil {
			login.Close()
		}
		win.Update(func() {
			if a.gen.Load() != gen {
				return
			}
			switch {
			case err != nil:
				a.status, a.connDown = "Can't look for a folder on "+b.label()+": "+err.Error(), true
			case dir == "":
				a.goPlain(b, why+"; no other folder there runs programs")
			default:
				a.logConn("GoRex's server can run in " + dir + ", which the host's settings keep")
				editRemote(b.target.Key(), b.target.Spec, func(r *savedRemote) { r.ServerDir = dir })
				nb := b
				nb.target.Dir = dir
				a.switchTo(nb, true)
			}
			a.changed()
		})
	}()
}

// cantRunDialog asks what to do on a host where GoRex's server can't
// run: look for another folder, when one may do, or connect with plain
// ssh. Cancelling leaves the window as it is, not connected.
func (a *App) cantRunDialog(c *ui.Context, k *colors) {
	q := a.cantRun
	if q == nil {
		return
	}
	if q.gen != a.gen.Load() {
		// The window moved on: another host, or connecting again.
		a.cantRun = nil
		return
	}
	end := func(then func()) {
		a.later(c, func() {
			a.cantRun = nil
			if then != nil {
				then()
			}
		})
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(500).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Padding(18, 20).Gap(12).Clip()
		ui.Text(c, "GoRex's server can't run on "+q.b.label()).FontSize(14).FontWeight(700).TextColor(k.text)
		ui.Text(c, q.reason).FontSize(12.5).TextColor(k.text).Selectable().LineHeight(1.45)
		var why strings.Builder
		if q.folders {
			why.WriteString("Another folder on the host may run programs: GoRex can try a few of yours there (in /run/user, /var/tmp, /dev/shm and /tmp). ")
		}
		why.WriteString("With plain ssh, the host's tabs run ssh from this PC: no files or git of the host in GoRex, and a tab's session ends with its connection. GoRex keeps your answer for this host; its settings change it.")
		ui.Text(c, why.String()).FontSize(12.5).TextColor(k.textMuted).LineHeight(1.45)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if textButton(c, k, "Cancel").Clicked() {
				end(nil)
			}
			if textButton(c, k, "Connect with plain ssh").Clicked() {
				end(func() { a.goPlain(q.b, q.reason) })
			}
			if q.folders && textButton(c, k, "Try other folders").Clicked() {
				end(func() { a.findFolder(q.b, q.reason) })
			}
		})
	})
	if !open {
		// Escape, or a click beside the dialog.
		end(nil)
	}
}
