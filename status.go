package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"gorex/internal/rex"
)

// What programs say they are doing (OSC 7501, kept by the server for each
// session): the pane's and tab's badge, and a notification when one waits
// on the user, finishes or fails out of sight.

// statusKey tells program statuses apart, for what the user saw.
func statusKey(st *rex.ProgramStatus) string {
	if st == nil {
		return ""
	}
	return st.State + "\x00" + st.Kind + "\x00" + st.Msg
}

// statusTip says a program status, in a tooltip.
func statusTip(st *rex.ProgramStatus) string {
	var s string
	switch st.State {
	case "working":
		s = "Working"
		if st.Progress >= 0 {
			s += " · " + strconv.Itoa(st.Progress) + "%"
		}
	case "blocked":
		switch st.Kind {
		case "permission":
			s = "Waiting for approval"
		case "question":
			s = "Waiting for an answer"
		case "auth":
			s = "Waiting to sign in"
		default:
			s = "Waiting for you"
		}
	case "done":
		s = "Done"
	case "error":
		s = "Failed"
	default:
		s = "Idle"
	}
	if msg := plainText(st.Msg); msg != "" {
		s += ": " + msg
	}
	if st.App != "" {
		s = st.App + " · " + s
	}
	return s
}

// plainText disarms what a program's text could do outside the grid:
// direction overrides and other invisible formatting characters go.
func plainText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c, r == 0xfeff:
			return -1
		}
		return r
	}, s)
}

// statusNoticeEvery is the least time between a pane's notifications of
// program status.
const statusNoticeEvery = 10 * time.Second

// statusNotice notifies when a pane's program newly says it waits on the
// user, has finished or failed, while the pane is out of sight.
func (a *App) statusNotice(p *Pane, was, now *rex.ProgramStatus, seen bool, dir string) {
	if now == nil || seen || !is(a.eff.Terminal.StatusNotify) || statusKey(was) == statusKey(now) {
		return
	}
	var what string
	switch now.State {
	case "blocked":
		what = " needs you"
	case "done":
		what = " finished"
	case "error":
		what = " failed"
	default:
		return
	}
	if time.Since(p.noticed) < statusNoticeEvery {
		return
	}
	p.noticed = time.Now()
	name := now.App
	if name == "" {
		name = programOf(p.info.Program).Name
	}
	body := "in " + shortDir(dir)
	if msg := plainText(now.Msg); msg != "" {
		body = msg + "\n" + body
	}
	mygo.NewNotification(mygo.NotificationOptions{Title: name + what, Body: body}).Show()
}
