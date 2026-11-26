package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// omp's tray of what works in the background, in the dock above the
// composer, as Tern draws it (the protocol's feature "tray").
//
// While something runs, it is one row, 64 DIPs tall, of compact cards, a
// subagent's or a background job's and one of what finished, after a
// strip for the main session. A click on a card opens it: omp shows that
// subagent's conversation in the pane, and the composer says it views it,
// its card marked with an eye; a click on MAIN goes back to the main
// session. Enter opens the card the keyboard is on, and Space, or the
// small mark at a card's corner, peeks at it: omp sends a panel of its
// detail, with buttons to open, stop or dismiss it, which shows above the
// row. The row scrolls sideways, the wheel too, fading at an end that has
// more, with arrows there under the pointer; when it has more than shows,
// a bar under it has a segment of each card and a line of those in sight.
//
// While nothing runs (omp's quiet), the tray is a dot of each card at the
// start of the status row (omp.hud.activity), grey for what finished, and
// a pill of how many finished before. The pointer over a dot shows its
// card above it; a click on a dot, or on the pill, shows the row with
// that card chosen, until Escape, or a press elsewhere while the pane
// shows the main session.

// The tray's sizes, as Tern's, in DIPs: a card's width and the row's
// height, the main session's strip, the peek's height, the fade at an
// end of the row with more, and the bar of segments under a row with
// more than shows. A lit card's box is set in trayLitInset from its top
// and bottom, one less from its sides, and rounded trayLitR, the tray's
// radius less the inset and a little; the row is taller than Tern's by
// the inset above and below, for the box to clear a card's last line.
const (
	trayCardW    = 180
	trayRowH     = 64 + 2*trayLitInset
	trayMainW    = 20
	trayPeekH    = 152
	trayFade     = 36
	trayScrub    = 12
	trayLitInset = 4
	trayLitR     = 10
)

// trayState is how a pane shows a surface's tray, from frame to frame.
type trayState struct {
	// focus is the last focus omp asked for: a larger one gives the
	// first card the keyboard.
	focus float64
	// open shows a quiet tray as cards, from its dots; sel is the card
	// chosen there, or clicked, by its ref, and seen the one chosen there
	// that the row brought into sight.
	open      bool
	sel, seen string
	// scroll is how far the row of cards is scrolled, done how far a
	// peek's finished list is.
	scroll, done ui.ScrollState
	// peek is the card peeked at as last drawn, "" for none, and id the
	// tray's node, which an action names.
	peek, id string
	// unpeek is the card opened, by its ref and its subagent's name,
	// whose peek closes once the composer views it (tspTray).
	unpeek, unpeekName string
	// shown is the peek drawn, kept while it closes; moving tells that
	// it opens or closes, the dock's height changing each frame, which
	// the surface tells omp once it settles (inlineSurface).
	shown  *rex.TSPNode
	moving bool
	// rowShown tells that the row of cards showed; rowUntil, once omp or
	// the user folds it, until when it shrinks before its dots show;
	// rowH its height grown, and rowMoving that it grows or shrinks.
	// rowDock and dotsDock are the dock's heights with the row grown and
	// with the dots, tuck how far the row goes under the composer.
	rowShown          bool
	rowUntil          time.Time
	rowH, tuck        float32
	rowDock, dotsDock float32
	rowMoving         bool
}

// trayRowMove is how long the row takes to grow or shrink, as Tern's.
const trayRowMove = 180 * time.Millisecond

// rowLow is the row's height that takes as much of the dock as its dots
// do, which it grows from and shrinks to; its tuck until the dock's
// heights with each are known.
func (s *trayState) rowLow() float32 {
	if s.rowDock == 0 || s.dotsDock == 0 {
		return s.tuck
	}
	return min(max(s.rowH-(s.rowDock-s.dotsDock), s.tuck), s.rowH)
}

// fold shows a tray opened from its dots as its dots again.
func (s *trayState) fold() {
	s.open, s.sel, s.seen = false, "", ""
}

// trayEscape takes an Escape in the pane for the tray, as Tern does,
// and reports whether it did: the first closes a card's peek, the next
// shows a row opened from the dots as its dots again. Else the Escape is
// the program's, as one that stops a turn.
func (a *App) trayEscape(p *Pane, sf *rex.Surface) bool {
	st := p.tray[sf.Image]
	switch {
	case st == nil || sf.Closed:
		return false
	case st.peek != "":
		a.tspSend(p, map[string]any{"ev": "action", "sf": sf.ID, "id": st.id, "act": "tray.peek="})
		st.peek = ""
		return true
	case st.open:
		st.fold()
		a.changed()
		return true
	}
	return false
}

// trayComposerPressed takes a press in the composer for the tray, as the
// user goes to type: a card's peek closes, and a row shown from the dots
// folds back to them. A row of running work stays, as Tern's.
func (a *App) trayComposerPressed(p *Pane, sf *rex.Surface) {
	st := p.tray[sf.Image]
	if st == nil || sf.Closed {
		return
	}
	if st.peek != "" {
		a.tspSend(p, map[string]any{"ev": "action", "sf": sf.ID, "id": st.id, "act": "tray.peek="})
		st.peek = ""
	}
	if st.open {
		st.fold()
	}
	a.changed()
}

// trayOf is the pane's state of a surface's tray.
func (p *Pane) trayOf(img uint32) *trayState {
	if p.tray == nil {
		p.tray = map[uint32]*trayState{}
	}
	s := p.tray[img]
	if s == nil {
		s = &trayState{}
		p.tray[img] = s
	}
	return s
}

// trayDock is what a dock drawn has of the tray: the tray, whether the
// dock has a composer, which the tray's row of cards sits on top of, the
// subagent the composer views ("" for the main session), the composer's
// way back to the main session, and whether the quiet tray's dots were
// drawn yet.
type trayDock struct {
	tray     *rex.TSPNode
	composer bool
	viewing  string
	exit     *rex.TSPNode
	dotted   bool
}

// findTray finds the tray of a dock, its composer, and what the composer
// views.
func findTray(dock *rex.TSPNode) trayDock {
	var d trayDock
	var walk func(n *rex.TSPNode)
	walk = func(n *rex.TSPNode) {
		if hidden, _ := n.Props["hidden"].(bool); hidden {
			return
		}
		role, _ := n.Props["role"].(string)
		switch {
		case role == "omp.tray":
			d.tray = n
			return
		case strings.HasPrefix(role, "omp.editor"):
			d.composer = true
		case role == "omp.composer.agent":
			d.viewing, _ = n.Props["text"].(string)
		case role == "omp.composer.exit":
			d.exit = n
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(dock)
	return d
}

// trayOnComposer reports whether tray n of the dock being drawn shows as
// its row of cards on top of the composer, which draws it (tspComposer).
func (a *App) trayOnComposer(p *Pane, sf *rex.Surface, n *rex.TSPNode) bool {
	return a.trayAt.tray == n && a.trayAt.composer && !trayQuiet(p, sf, n)
}

// trayQuiet reports whether tray n shows as its dots: nothing runs, no
// card is peeked at, and the user did not show its cards; a row that
// showed stays while it shrinks away (trayRowMove) before its dots show.
func trayQuiet(p *Pane, sf *rex.Surface, n *rex.TSPNode) bool {
	st := p.trayOf(sf.Image)
	if !trayFolded(st, n) {
		return false
	}
	if st.rowShown {
		if st.rowUntil.IsZero() {
			st.rowUntil = time.Now().Add(trayRowMove)
		}
		if time.Now().Before(st.rowUntil) {
			return false
		}
		st.rowShown, st.rowUntil = false, time.Time{}
	}
	return true
}

// trayFolded reports whether tray n is to show as its dots, as omp and
// the user have it now: nothing runs, no card is peeked at, and the user
// did not show its cards.
func trayFolded(st *trayState, n *rex.TSPNode) bool {
	quiet, _ := n.Props["quiet"].(bool)
	peek, _ := n.Props["peek"].(string)
	return quiet && peek == "" && !st.open
}

// trayDotsHere reports whether the status row of the dock being drawn
// takes the quiet tray's dots.
func (a *App) trayDotsHere(p *Pane, sf *rex.Surface) bool {
	d := &a.trayAt
	return d.tray != nil && !d.dotted && trayQuiet(p, sf, d.tray)
}

// tspTray draws omp's tray, n: its row of cards, or its dots where the
// status row did not take them. On a composer (tab), the row is a tab on
// top of the composer's card, as wide, its foot under the card, as
// Tern's; else a card of its own.
func (a *App) tspTray(c *ui.Context, key *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, tab bool) ui.Element {
	var cards []*rex.TSPNode
	var peek *rex.TSPNode
	for _, ch := range n.Children {
		if role, _ := ch.Props["role"].(string); role == "omp.tray.peek" {
			peek = ch
		} else {
			cards = append(cards, ch)
		}
	}
	st := p.trayOf(sf.Image)
	quiet, _ := n.Props["quiet"].(bool)
	if trayQuiet(p, sf, n) {
		// Where no status row took its dots.
		if a.trayAt.tray == n {
			a.trayAt.dotted = true
		}
		st.peek = ""
		return a.trayDots(c, key, k, p, sf, cards)
	}
	peekRef, _ := n.Props["peek"].(string)
	if st.peek != "" && peekRef == "" {
		// A peek closed, by any route: its card is no longer marked.
		st.sel = ""
	}
	st.peek, st.id = peekRef, n.ID
	main, _ := n.Props["main"].(string)
	focus, _ := n.Props["focus"].(float64)
	takeFocus := focus > 0 && st.focus != focus
	st.focus = max(st.focus, focus)
	viewing := a.trayAt.viewing
	if st.unpeek != "" && viewing == st.unpeekName {
		// A card opened (trayCard): once its subagent is viewed, its peek
		// closes, as Tern's does.
		if peekRef == st.unpeek && !sf.Closed {
			a.tspSend(p, map[string]any{"ev": "action", "sf": sf.ID, "id": n.ID, "act": "tray.peek="})
		}
		st.unpeek, st.unpeekName = "", ""
	}
	face := trayFace(k)
	tray := ui.Column(key).Clip().Background(face).Border(1, trayRing(k))
	st.tuck = 0
	if tab {
		// Stretched by the composer's column between its margins, 10
		// DIPs in from the card's sides: a full width would overflow by
		// them.
		tray.Radius(16, 16, 0, 0).Margin(0, 10, -trayTuck, 10).Padding(0, 0, trayTuck, 0)
		st.tuck = trayTuck
	} else {
		tray.FillWidth().Radius(16)
	}
	// The row grows from the height its dots took as it shows, and
	// shrinks back to it before its dots show, as Tern's, the tray cut to
	// the height it has reached: the dock, and the output above it, move
	// smoothly from one to the other.
	var rowTarget float32 = 1
	if trayFolded(st, n) {
		rowTarget = 0
		c.After(trayRowMove)
	} else {
		st.rowUntil = time.Time{}
	}
	if !st.rowShown {
		tray.Jump("row", 0)
		st.rowShown = true
	}
	settled := !st.rowMoving
	grown := tray.AnimateTo("row", rowTarget, ui.Animation{Duration: trayRowMove})
	st.rowMoving = grown != rowTarget
	if full := tray.Bounds().H; grown >= 1 && settled && full > 0 {
		if st.shown == nil && !st.moving {
			st.rowH, st.rowDock = full, p.dockH[sf.Image]
		}
	} else if grown < 1 {
		if st.rowH == 0 {
			st.rowH = trayRowH + trayScrub + trayTuck
		}
		tray.Height(st.rowLow() + grown*(st.rowH-st.rowLow()))
	}
	if st.open && (!quiet || viewing == "" && tray.PressedOutside()) {
		// Shown from its dots: hidden again by a press elsewhere on the
		// main session, or once something runs and it shows by itself.
		st.fold()
		a.changed()
	}
	tray.HandleInput(func(ev ui.InputEvent) bool {
		return ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape && ev.Mods == 0 && a.trayEscape(p, sf)
	})
	tray.Children(func() {
		// The peek grows from nothing and shrinks back, as Tern's, over
		// trayPeekMove; the last one stays drawn while it shrinks.
		var target float32
		if peek != nil {
			st.shown, target = peek, trayPeekH
		}
		h := tray.AnimateTo("peek", target, ui.Animation{Duration: trayPeekMove})
		st.moving = h != target
		if shown := st.shown; shown != nil && h > 0.5 {
			panel := ui.Column(c.Key("peek")).FillWidth().Height(h).Clip().
				Background(k.busy.Alpha(0.06)).BorderWidth(0, 0, 1, 0).BorderColor(trayRing(k))
			panel.Children(func() {
				// Laid out at its full height, the panel cutting it.
				ui.Column(c.Key("peek-body")).FillWidth().Height(trayPeekH).Shrink(0).Gap(3).Padding(6, 8, 10, 13).Children(func() {
					a.tspHead = true
					a.trayPeekBody(c, k, t, p, sf, shown)
					a.tspHead = false
				})
			})
		} else if peek == nil {
			st.shown = nil
		}
		ui.Row(c.Key("row")).FillWidth().AlignItems(ui.Stretch).Children(func() {
			a.trayMain(c, k, t, p, sf, main, viewing)
			a.trayStrip(c, k, t, p, sf, trayRowOrder(cards), peekRef, viewing, takeFocus, face)
		})
	})
	return tray
}

// trayPeekBody draws what a peek holds, as Tern lays it out: a job's
// widget (omp.job.widget) and its last lines of output (omp.tray.out)
// side by side under the head, the output a little wider; anything else
// in order, a finished list (omp.tray.done) taking the height left.
func (a *App) trayPeekBody(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, peek *rex.TSPNode) {
	var widget, out *rex.TSPNode
	for _, ch := range peek.Children {
		switch role, _ := ch.Props["role"].(string); role {
		case "omp.job.widget":
			widget = ch
		case "omp.tray.out":
			out = ch
		}
	}
	if widget == nil {
		a.tspChildren(c, k, t, p, sf, peek)
		return
	}
	for _, ch := range peek.Children {
		if ch != widget && ch != out {
			a.tspNode(c, k, t, p, sf, ch)
		}
	}
	ui.Row(c.Key("peek-job")).FillWidth().Grow(1).MinHeight(0).Gap(20).AlignItems(ui.Start).Children(func() {
		ui.Column(c.Key("peek-widget")).Grow(1).Basis(0).MinWidth(0).Padding(2, 0, 0, 0).Clip().Children(func() {
			a.tspNode(c, k, t, p, sf, widget)
		})
		if out != nil {
			ui.Column(c.Key("peek-out")).Grow(1.15).Basis(0).MinWidth(0).Padding(3, 0, 0, 0).Clip().Children(func() {
				a.tspNode(c, k, t, p, sf, out)
			})
		}
	})
}

// trayTuck is how far the tray's tab goes under the composer's card.
const trayTuck = 18

// trayPeekMove is how long the peek takes to open or close, as Tern's.
const trayPeekMove = 220 * time.Millisecond

// trayFace and trayRing are the colors of the tray's surface and of its
// outline: a touch toward the text's color from the output's, quieter
// than the composer's card.
func trayFace(k *colors) ui.Color { return k.bgBottom.Mix(k.text, 0.035) }
func trayRing(k *colors) ui.Color { return k.bgBottom.Mix(k.text, 0.1) }

// trayMain draws the tray's strip of the main session, MAIN on its side:
// a quiet label on the main session, and while a subagent's conversation
// shows, a button back to the main session, as the composer's is.
func (a *App) trayMain(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, state, viewing string) {
	exit := a.trayAt.exit
	back := viewing != "" && exit != nil && !sf.Closed
	label := "Main session"
	if state != "" {
		label += ": " + state
	}
	strip := ui.Box(c.Key("main")).Width(trayMainW).Height(trayRowH).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
		BorderWidth(0, 1, 0, 0).BorderColor(trayRing(k))
	col := k.textMuted.Alpha(0.55)
	if back {
		col = k.text
		strip.Background(k.text.Alpha(0.06)).Cursor(ui.CursorPointer).Label("Back to the main session").Tooltip("Back to the main session")
		if strip.Hovered() {
			strip.Background(k.text.Alpha(0.12))
		}
		if strip.Clicked() {
			act, _ := exit.Props["actions"].(map[string]any)["click"].(string)
			a.surfacePressed(t, p)
			a.tspSend(p, map[string]any{"ev": "action", "sf": sf.ID, "id": exit.ID, "act": act})
		}
	} else {
		strip.Label(label)
	}
	strip.Children(func() {
		// Laid out across, the strip's height wide, then turned on its
		// side about its center, which is the strip's.
		label := ui.Box(c.Key("main-label")).Absolute().Width(trayRowH).Height(trayMainW).
			Left((trayMainW - trayRowH) / 2).Top((trayRowH - trayMainW) / 2).
			AlignItems(ui.Center).Justify(ui.Center).Rotate(-90).PassThrough()
		label.Children(func() {
			ui.Text(c, "MAIN").FontSize(a.tspText(9)).Bold().LetterSpacing(1.3).TextColor(col).SingleLine()
		})
	})
}

// trayStrip draws the row of cards, which scrolls sideways, the wheel's
// turns too, fades at an end that has more with an arrow there under the
// pointer, and has a bar of its cards under it when it has more than
// shows.
func (a *App) trayStrip(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, cards []*rex.TSPNode, peekRef, viewing string, takeFocus bool, face ui.Color) {
	st := p.trayOf(sf.Image)
	sc := &st.scroll
	box := ui.Column(c.Key("strip-box")).Grow(1).MinWidth(0)
	over := sc.MaxX > 0
	viewW := box.Bounds().W
	// Few cards share the row's width, as Tern's; more are trayCardW
	// wide, and the row scrolls.
	cardW := float32(trayCardW)
	if n := len(cards); n > 0 {
		cardW = max(cardW, float32(math.Floor(float64(viewW/float32(n)))))
	}
	to := func(x float32) {
		sc.ScrollTo(min(max(x, 0), sc.MaxX), sc.Y, ui.Smooth)
		a.changed()
	}
	if st.open && st.sel != "" && st.seen != st.sel && sc.MaxX > 0 {
		// Chosen from the dots: brought into sight once.
		for i, card := range cards {
			if ref, _ := card.Props["ref"].(string); ref == st.sel {
				to(float32(i-1) * trayCardW)
			}
		}
		st.seen = st.sel
	}
	hovered := box.Hovered()
	box.Children(func() {
		strip := ui.ScrollHorizontal(c.Key("strip")).FillWidth().Height(trayRowH).TrackScroll(sc).Scrollbars(false)
		strip.HandleInput(func(ev ui.InputEvent) bool {
			// The wheel's turn goes sideways along the row.
			if ev.Kind != ui.InputScroll || ev.DX != 0 || sc.MaxX <= 0 {
				return false
			}
			sc.ScrollTo(min(max(sc.X+ev.DY, 0), sc.MaxX), sc.Y, ui.Animation{})
			a.changed()
			return true
		})
		strip.Children(func() {
			// A card lit (chosen, or viewed) shows its rounded box with no
			// divider on either side.
			lit := make([]bool, len(cards))
			chosen := make([]bool, len(cards))
			for i, card := range cards {
				ref, _ := card.Props["ref"].(string)
				chosen[i] = ref != "" && (ref == peekRef || ref == st.sel) || i == 0 && peekRef == "" && st.sel == ""
				lit[i] = chosen[i] || viewing != "" && trayName(card) == viewing
			}
			for i, card := range cards {
				rule := i > 0 && !lit[i] && !lit[i-1]
				el := a.trayCard(c, k, t, p, sf, card, peekRef, viewing, rule, chosen[i], cardW)
				if takeFocus && i == 0 {
					el.Focus()
				}
			}
		})
		fade := func(name string, left bool) {
			from, to := face, face.Alpha(0)
			e := ui.Box(c.Key(name)).Absolute().Top(0).Height(trayRowH).Width(trayFade).PassThrough()
			if left {
				e.Left(0)
			} else {
				e.Right(0)
				from, to = to, from
			}
			e.LinearGradient(ui.LinearGradient{From: from, To: to, Angle: 90})
		}
		arrow := func(name string, left bool) {
			b := ui.Box(c.Key(name)).Absolute().Top(20).Size(24, 24).Radius(12).Background(k.panel).Border(1, k.panelBorder).
				AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
			deg := float32(-90)
			if left {
				b.Left(6).Label("Previous cards")
				deg = 90
			} else {
				b.Right(6).Label("Next cards")
			}
			b.Children(func() {
				ui.Icon(c, icon("chevron-down")).Size(13, 13).TextColor(k.text).Rotate(deg)
			})
			if b.Clicked() {
				if left {
					to(sc.X - viewW*0.8)
				} else {
					to(sc.X + viewW*0.8)
				}
			}
		}
		if sc.X > 1 {
			fade("fade-l", true)
			if hovered {
				arrow("prev", true)
			}
		}
		if sc.X < sc.MaxX-1 {
			fade("fade-r", false)
			if hovered {
				arrow("next", false)
			}
		}
		if over {
			sel := st.sel
			if sel == "" && peekRef == "" && len(cards) > 0 {
				sel, _ = cards[0].Props["ref"].(string)
			}
			a.trayScrub(c, k, sc, cards, sel, viewW, to)
		}
	})
}

// trayScrub draws the bar under a row with more cards than show, as
// Tern's: a segment of each card in its status's color, full for the
// cards in sight and faint for the others, a dot over the chosen card's
// (sel), and a line under those in sight. A click on a segment brings its
// card into sight.
func (a *App) trayScrub(c *ui.Context, k *colors, sc *ui.ScrollState, cards []*rex.TSPNode, sel string, viewW float32, to func(float32)) {
	total := sc.MaxX + viewW
	ui.Box(c.Key("scrub")).FillWidth().Height(trayScrub).Children(func() {
		ui.Row(c.Key("segs")).Absolute().Left(12).Right(12).Top(4).Height(3).Gap(1).Children(func() {
			for i, card := range cards {
				x := float32(i) * trayCardW
				col := a.trayColor(k, card)
				if x+trayCardW <= sc.X || x >= sc.X+viewW {
					col = col.Alpha(0.45)
				}
				seg := ui.Box(c.Key("seg-"+card.ID)).Grow(1).Basis(0).MinWidth(0).Height(3).Radius(1).Background(col).Cursor(ui.CursorPointer)
				if ref, _ := card.Props["ref"].(string); ref != "" && ref == sel {
					seg.Children(func() {
						ui.Box(c.Key("sel")).Absolute().Top(-5).Size(3, 3).Radius(1.5).Background(k.busy).
							LeftPercent(50).Margin(0, 0, 0, -1.5)
					})
				}
				if seg.Clicked() {
					to(x)
				}
			}
		})
		if total > 0 {
			ui.Box(c.Key("win")).Absolute().Top(9).Height(1).Radius(1).Background(k.textMuted).
				LeftPercent(sc.X / total * 100).WidthPercent(viewW / total * 100)
		}
	})
}

// statusDots draws the quiet tray of the dock being drawn as its dots,
// in the status row.
func (a *App) statusDots(c *ui.Context, k *colors, p *Pane, sf *rex.Surface) {
	var cards []*rex.TSPNode
	for _, ch := range a.trayAt.tray.Children {
		if role, _ := ch.Props["role"].(string); role != "omp.tray.peek" {
			cards = append(cards, ch)
		}
	}
	a.trayAt.dotted = true
	a.trayDots(c, c.Key("tray-dots"), k, p, sf, cards)
}

// trayDotsMost is how many dots the quiet tray shows, as Tern's.
const trayDotsMost = 8

// trayDots draws the quiet tray as Tern does: a dot of each of the 8
// newest cards, newest first, grey for what finished, and a pill of how
// many more there are. The pointer over a dot shows its card above it; a
// click on a dot, or on the pill, shows the cards, that card chosen.
func (a *App) trayDots(c *ui.Context, key *ui.Context, k *colors, p *Pane, sf *rex.Surface, cards []*rex.TSPNode) ui.Element {
	st := p.trayOf(sf.Image)
	show := func(ref string) {
		if sf.Closed {
			return
		}
		st.open, st.sel = true, ref
		a.changed()
	}
	if h := p.dockH[sf.Image]; h > 0 {
		// The dock with the dots, as the last frame drew it: the row
		// grows from as much (rowLow).
		st.dotsDock = h
	}
	dots, more := trayDotCards(cards)
	row := ui.Row(key).Gap(3).AlignItems(ui.Center).Height(20).Shrink(0).Label("Agents and jobs")
	row.Children(func() {
		for _, card := range dots {
			ref, _ := card.Props["ref"].(string)
			pip := ui.Box(c.Key("pip-"+card.ID)).Size(11, 11).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Label(trayName(card))
			pip.Children(func() {
				dot := ui.Box(c).Size(7, 7).Radius(3.5)
				if status, _ := card.Props["status"].(string); status == "queued" {
					dot.Border(1.5, k.textMuted)
				} else {
					dot.Background(a.trayColor(k, card))
				}
			})
			if pip.Hovered() {
				a.trayHover(c, k, p, sf, card, pip)
			}
			if pip.Clicked() {
				show(ref)
			}
		}
		if more <= 0 {
			return
		}
		pill := ui.Box(c.Key("more")).Padding(0, 5).Height(16).Radius(8).Border(1, k.cardBorder).
			AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Margin(0, 0, 0, 3).
			Label("Show the cards").Tooltip(fmt.Sprintf("%d more: show the cards", more))
		if pill.Hovered() {
			pill.Background(k.hover)
		}
		pill.Children(func() {
			ui.Text(c, "+"+strconv.Itoa(more)).FontSize(a.tspText(10.5)).Font(a.tspLook().mono).TextColor(k.textMuted).SingleLine()
		})
		if pill.Clicked() {
			show("")
		}
	})
	return row
}

// trayDotCards are the cards the quiet tray has dots of: the newest
// trayDotsMost, newest first (omp sends them oldest first), those that
// run or wait taking the places of the oldest finished if they are
// older; and how many more there are, what omp's card of what finished
// counts among them.
func trayDotCards(cards []*rex.TSPNode) (dots []*rex.TSPNode, more int) {
	var rest []*rex.TSPNode
	for i := len(cards) - 1; i >= 0; i-- {
		card := cards[i]
		if item, _ := card.Props["item"].(string); item == "done" {
			more += trayDoneCount(card)
			continue
		}
		if len(dots) < trayDotsMost {
			dots = append(dots, card)
		} else {
			rest = append(rest, card)
		}
	}
	for _, card := range rest {
		status, _ := card.Props["status"].(string)
		if status != "running" && status != "queued" {
			continue
		}
		// A finished dot, the oldest, makes room for it.
		for j := len(dots) - 1; j >= 0; j-- {
			if s, _ := dots[j].Props["status"].(string); s != "running" && s != "queued" {
				dots = append(dots[:j], dots[j+1:]...)
				dots = append(dots, card)
				break
			}
		}
	}
	return dots, more + len(cards) - len(dots) - trayDoneCards(cards)
}

// trayDoneCards is how many of cards are omp's card of what finished.
func trayDoneCards(cards []*rex.TSPNode) int {
	n := 0
	for _, card := range cards {
		if item, _ := card.Props["item"].(string); item == "done" {
			n++
		}
	}
	return n
}

// trayHover draws card above its dot, pip, as the pointer is over it.
func (a *App) trayHover(c *ui.Context, k *colors, p *Pane, sf *rex.Surface, card *rex.TSPNode, pip ui.Element) {
	ui.Overlay(c, func() {
		box := ui.Column(c.Key("tray-hover-"+card.ID)).Width(trayCardW+80).Gap(2).Padding(9, 12, 8, 12).Radius(10).
			Background(k.panel).Border(1, k.panelBorder).Shadow(0, 6, 18, -4, k.shadow).
			AttachTo(pip, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(0, 0, 8, 0).PassThrough()
		box.Children(func() {
			a.tspHead = true
			a.tspChildren(c, k, a.tab(), p, sf, card)
			a.tspHead = false
			ui.Text(c, "Click to show the cards").FontSize(a.tspText(10.5)).TextColor(k.textMuted.Alpha(0.7)).Margin(4, 0, 0, 0)
		})
	})
}

// trayColor is the color of a card's status: its tone while it runs,
// waits or failed; grey once it finished or stopped.
func (a *App) trayColor(k *colors, card *rex.TSPNode) ui.Color {
	switch status, _ := card.Props["status"].(string); status {
	case "done", "stopped", "cancelled":
		return k.textMuted.Alpha(0.45)
	}
	tone, _ := card.Props["tone"].(string)
	if col, ok := a.spanColor(k, tone); ok {
		return col
	}
	return k.textMuted
}

// trayName is the name a card shows (omp.tray.name), as the composer
// names the subagent it views.
func trayName(card *rex.TSPNode) string {
	var name string
	var find func(n *rex.TSPNode)
	find = func(n *rex.TSPNode) {
		if role, _ := n.Props["role"].(string); role == "omp.tray.name" && name == "" {
			name, _ = n.Props["text"].(string)
		}
		for _, ch := range n.Children {
			find(ch)
		}
	}
	find(card)
	return name
}

// trayDoneCount is how many finished, by the name of the card of what
// finished ("33 done", "4 more done"); 0 when it does not tell.
func trayDoneCount(card *rex.TSPNode) int {
	n, _ := strconv.Atoi(strings.Fields(trayName(card) + " x")[0])
	return n
}

// trayCard draws a card of the tray, 180 by 64 DIPs, apart from the card
// before it by a line (rule): its head (a dot of its status, its name, id
// and time) and what it does, as omp sends them. As Tern's, a click peeks
// at it (omp's detail panel above the row, with Open, Stop and Dismiss),
// or closes its peek; a double click or Enter opens it, and Space peeks.
// The card the composer views has an eye, and the card chosen (the
// first, unless one was chosen) is lit, with the mark. It is w wide.
func (a *App) trayCard(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, peekRef, viewing string, rule, chosen bool, w float32) ui.Element {
	ref, _ := n.Props["ref"].(string)
	item, _ := n.Props["item"].(string)
	status, _ := n.Props["status"].(string)
	look := true
	if l, ok := n.Props["look"].(bool); ok {
		look = l
	}
	st := p.trayOf(sf.Image)
	peekAct, openAct := trayActs(ref, peekRef, item, look)
	aria, _ := n.Props["aria"].(string)
	viewed := viewing != "" && trayName(n) == viewing
	card := ui.Column(c.Key("card-"+n.ID)).Width(w).Height(trayRowH).Shrink(0).Gap(2).Padding(9, 12, 0, 13).Clip().
		Focusable().Cursor(ui.CursorPointer).Label(aria).SnapAlignX(ui.Start)
	if rule {
		card.BorderWidth(0, 0, 0, 1).BorderColor(trayRing(k))
	}
	// Its light, and its ring, are a box rounded as the tray, set in from
	// the card's edges (trayLitInset).
	var fill, ring ui.Color
	switch {
	case viewed:
		fill, ring = k.attention.Alpha(0.07), k.attention.Alpha(0.34)
	case chosen:
		fill = k.text.Alpha(0.06)
		if ref == st.sel || ref == peekRef {
			ring = k.busy.Alpha(0.5)
		}
	case card.Hovered() || card.Focused():
		fill = k.text.Alpha(0.04)
	}
	send := func(act string) {
		if act == "" {
			return
		}
		a.surfacePressed(t, p)
		a.tspSend(p, map[string]any{"ev": "action", "sf": sf.ID, "id": n.ID, "act": act})
	}
	// Opening a card closes its peek, as Tern does, once the subagent
	// shows (tspTray): omp keeps the peek open through the switch.
	open := func() {
		if openAct != "" {
			st.unpeek, st.unpeekName = ref, trayName(n)
			send(openAct)
		}
	}
	if !sf.Closed {
		if card.DoubleClicked() {
			open()
		} else if card.Clicked() {
			st.sel = ref
			send(peekAct)
		}
		card.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown || ev.Mods != 0 {
				return false
			}
			switch ev.Key {
			case ui.KeyEnter:
				open()
			case ui.KeySpace:
				send(peekAct)
			default:
				return false
			}
			return true
		})
	}
	card.Children(func() {
		if fill != (ui.Color{}) {
			lit := ui.Box(c.Key("lit")).Absolute().Top(trayLitInset).Bottom(trayLitInset).Left(trayLitInset-1).Right(trayLitInset-1).
				Radius(trayLitR).Background(fill).PassThrough()
			if ring != (ui.Color{}) {
				lit.Border(1, ring)
			}
		}
		a.tspHead = true
		a.trayEye, a.trayRunning = viewed, status == "running" || status == "queued"
		a.tspChildren(c, k, t, p, sf, n)
		a.trayEye, a.trayRunning = false, false
		a.tspHead = false
		if look && peekAct != "" && !sf.Closed && (card.Hovered() || chosen) {
			cue := ui.Box(c.Key("cue")).Absolute().Top(trayLitInset+3).Right(trayLitInset+2).Size(14, 14).Radius(4).
				AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Label("Peek").Tooltip("Peek  Space")
			if cue.Hovered() {
				cue.Background(k.text.Alpha(0.08))
			}
			cue.Children(func() {
				ui.Icon(c, icon("chevron-down")).Size(10, 10).TextColor(k.textMuted).Rotate(180)
			})
			if cue.Clicked() {
				send(peekAct)
			}
		}
	})
	return card
}

// trayRowOrder is the cards in the order the row shows them, as Tern's:
// the newest first (omp sends them oldest first), as the dots are, and
// omp's card of what finished last.
func trayRowOrder(cards []*rex.TSPNode) []*rex.TSPNode {
	out := make([]*rex.TSPNode, 0, len(cards))
	var done []*rex.TSPNode
	for i := len(cards) - 1; i >= 0; i-- {
		if item, _ := cards[i].Props["item"].(string); item == "done" {
			done = append(done, cards[i])
		} else {
			out = append(out, cards[i])
		}
	}
	return append(out, done...)
}

// trayActs are the actions a card of the tray sends omp: peek, which
// peeks at it, or closes its peek when it shows; open, which opens it.
// "" is none: a card that has nothing to peek at (look false) and the
// card of what finished, which opens nothing.
func trayActs(ref, peekRef, item string, look bool) (peek, open string) {
	if ref == "" {
		return "", ""
	}
	if look {
		peek = "tray.peek=" + ref
		if ref == peekRef {
			peek = "tray.peek="
		}
	}
	if item != "done" {
		open = "tray.open=" + ref
	}
	return peek, open
}

// trayTextSize is the size of a text of the tray, or of a job's widget,
// by its role, as Tern's, at the scale of 13 (tspText); 0 for another.
// Rich text takes it for its spans, which carry their size.
func trayTextSize(role string) float32 {
	switch role {
	case "omp.tray.name", "omp.job.widget.title":
		return 12.5
	case "omp.tray.id", "omp.tray.stats", "omp.tray.ev.tool", "omp.tray.model", "omp.tray.meta",
		"omp.tray.stage", "omp.tray.peek.meta", "omp.job.widget.caption":
		return 11
	case "omp.tray.done.more", "omp.tray.out", "omp.job.widget.stat", "omp.job.widget.text":
		return 11.5
	case "omp.tray.doing", "omp.tray.line", "omp.tray.ev.text", "omp.tray.peek.task",
		"omp.job.widget.stage", "omp.job.widget.headline":
		return 12
	}
	return 0
}

// trayText styles a text of the tray by its role, as Tern does: the name
// semibold, what it does in the window's font, the rest small in the
// terminal's font, quiet. A job's widget, in its peek and in the jobs
// sheet, the same way: its title semibold, its figures small in the
// terminal's font. Its size is trayTextSize's.
func (a *App) trayText(el ui.Element, k *colors, role string) bool {
	mono := a.tspLook().mono
	switch role {
	case "omp.tray.name":
		el.Bold()
	case "omp.tray.id", "omp.tray.stats", "omp.tray.ev.tool":
		el.Font(mono).TextColor(k.textMuted.Alpha(0.8))
	case "omp.tray.done.more":
		el.TextColor(k.textFaint).Margin(4, 6, 0, 6)
	case "omp.tray.model", "omp.tray.meta", "omp.tray.stage", "omp.tray.peek.meta":
		el.Font(mono).TextColor(k.textMuted)
	case "omp.tray.out":
		el.Font(mono).LineHeight(17.0 / 11.5).TextColor(k.textMuted)
	case "omp.tray.doing", "omp.tray.line", "omp.tray.ev.text", "omp.tray.peek.task":
	case "omp.job.widget.stat", "omp.job.widget.text":
		el.Font(mono)
	case "omp.tray.caption":
		el.TextColor(k.textMuted)
	case "omp.job.widget.title":
		el.FontWeight(600)
	case "omp.job.widget.stage":
		el.TextColor(k.textMuted)
	case "omp.job.widget.headline":
		el.Font(mono)
	case "omp.job.widget.caption":
		el.TextColor(k.textFaint)
	default:
		return false
	}
	if size := trayTextSize(role); size > 0 {
		el.FontSize(a.tspText(size))
	}
	return true
}

// trayDot draws a status dot of the tray: the color of its tone, or the
// busy color while what it stands for runs, breathing then; on the card
// the composer views, with an eye after it.
func (a *App) trayDot(c *ui.Context, key *ui.Context, k *colors, p *Pane, sf *rex.Surface, n *rex.TSPNode) ui.Element {
	col := k.textMuted
	pulse := false
	if spans, ok := n.Props["spans"].([]any); ok && len(spans) > 0 {
		m, _ := spans[0].(map[string]any)
		style, _ := m["s"].(string)
		if cl, ok := a.spanColor(k, style); ok {
			col = cl
		}
		fx, _ := m["fx"].(string)
		pulse = fx == "pulse"
	}
	if a.trayRunning {
		col = k.busy
	}
	if pulse && !sf.Closed {
		// Breathing once in two and a half seconds, redrawn as it does.
		phase := float64(time.Now().UnixMilli()%2400) / 2400
		col = col.Alpha(float32(0.55 + 0.45*math.Cos(phase*2*math.Pi)))
		c.After(60 * time.Millisecond)
	}
	if !a.trayEye {
		return ui.Box(key).Size(7, 7).Radius(3.5).Background(col).Shrink(0)
	}
	row := ui.Row(key).Gap(5).AlignItems(ui.Center).Shrink(0)
	row.Children(func() {
		ui.Box(c).Size(7, 7).Radius(3.5).Background(col).Shrink(0)
		ui.Icon(c, icon("eye")).Size(12, 12).TextColor(k.attention).Label("Viewing")
	})
	return row
}
