package main

import (
	"context"
	"os"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/askpass"
	"gorex/internal/rex"
)

// loginPrompt is what ssh asks while the window connects (gen): a
// password, a passphrase, a code, whether to trust a host's key. The
// window shows it until the user answers, which goes back to ssh by
// reply, or the prompt goes.
type loginPrompt struct {
	askpass.Prompt
	host  string
	gen   int64
	reply chan loginReply
}

type loginReply struct {
	answer string
	ok     bool
}

// loginSession starts the session through which the ssh's of a connect
// of the window (gen) to b ask the user. It is nil when the window can't
// ask, whose ssh's then ask nothing.
func (a *App) loginSession(win *mygo.Window, b binding, gen int64) *askpass.Session {
	a.askOnce.Do(func() {
		if err := os.MkdirAll(rex.Dir(), 0o700); err != nil {
			a.askErr = err
			return
		}
		a.askSrv, a.askErr = askpass.Listen(rex.Dir())
	})
	if a.askSrv == nil {
		a.logConn("GoRex can't ask for passwords: " + a.askErr.Error())
		return nil
	}
	return a.askSrv.Session(func(ctx context.Context, p askpass.Prompt) (string, bool) {
		lp := &loginPrompt{Prompt: p, host: b.label(), gen: gen, reply: make(chan loginReply, 1)}
		a.logConn("ssh asks: " + firstLine(p.Text))
		win.Update(func() {
			if a.gen.Load() != gen {
				lp.reply <- loginReply{}
				return
			}
			a.login, a.loginText = lp, ""
		})
		select {
		case r := <-lp.reply:
			return r.answer, r.ok
		case <-ctx.Done():
			win.Update(func() {
				if a.login == lp {
					a.login, a.loginText = nil, ""
				}
			})
			return "", false
		}
	})
}

// endLogin answers lp, if it still shows, and takes it away. The
// Connection Log tells what the user did, never what they typed.
func (a *App) endLogin(lp *loginPrompt, answer string, ok bool) {
	if lp == nil || a.login != lp {
		return
	}
	a.login, a.loginText = nil, ""
	switch {
	case !ok:
		a.logConn("cancelled")
	case lp.Kind == askpass.HostKey && answer == "yes":
		a.logConn("trusted the host's key")
	case lp.Kind == askpass.HostKey:
		a.logConn("did not trust the host's key")
	default:
		a.logConn("answered")
	}
	lp.reply <- loginReply{answer, ok}
}

// loginDialog shows what ssh asks while the window connects: its words,
// as ssh wrote them, and a field for a secret, which never shows, or
// buttons for a yes or no.
func (a *App) loginDialog(c *ui.Context, k *colors) {
	lp := a.login
	if lp == nil {
		return
	}
	if lp.gen != a.gen.Load() {
		// The window moved on: another host, or connecting again.
		a.endLogin(lp, "", false)
		return
	}
	end := func(answer string, ok bool) { a.later(c, func() { a.endLogin(lp, answer, ok) }) }
	open := true
	title := "Log in to " + lp.host
	switch lp.Kind {
	case askpass.HostKey:
		title = "A host GoRex has not seen: " + lp.host
	case askpass.Confirm, askpass.Notice:
		title = lp.host
	}
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(480).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Padding(18, 20).Gap(12).Clip()
		ui.Text(c, title).FontSize(14).FontWeight(700).TextColor(k.text)
		ui.Text(c, strings.TrimSpace(lp.Text)).FontSize(12.5).TextColor(k.text).Selectable().LineHeight(1.45)
		if lp.Again {
			ui.Text(c, "That was not right: try again.").FontSize(12.5).TextColor(ui.Hex("#ef4444"))
		}
		if lp.Kind == askpass.Secret {
			in := ui.TextInput(c, &a.loginText).Password().FillWidth().FontSize(13).AutoFocus()
			if in.Submitted() {
				end(a.loginText, true)
			}
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			switch lp.Kind {
			case askpass.HostKey:
				if textButton(c, k, "Don't connect").Clicked() {
					end("no", true)
				}
				if textButton(c, k, "Trust and connect").Clicked() {
					end("yes", true)
				}
			case askpass.Notice:
				if textButton(c, k, "Cancel").Clicked() {
					end("", false)
				}
			default:
				if textButton(c, k, "Cancel").Clicked() {
					end("", false)
				}
				if textButton(c, k, "OK").Clicked() {
					end(a.loginText, true)
				}
			}
		})
	})
	if !open {
		// Escape, or a click beside the dialog.
		end("", false)
	}
}

// firstLine is the first line of s.
func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(l)
}
