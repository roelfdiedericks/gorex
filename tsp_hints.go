package main

import (
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// The ops that keep nothing, which a window acts on as it draws: reveal
// brings a node into view (in its scroll container, or the terminal's
// scrollback for one drawn among its rows), scroll moves the scrollback
// by a line, a page or to an end, focus gives a field the keys (none:
// the terminal). A reveal of a node not drawn yet waits a few frames.

// tspHintFrames is how many frames a reveal waits for its node.
const tspHintFrames = 3

// tspPending is a hint the pane has to act on, and the frames it has
// waited.
type tspPending struct {
	rex.SurfaceHint
	frames int
}

// takeHints moves the hints the pane's mirror gathered into what this
// frame acts on.
func (a *App) takeHints(c *ui.Context, p *Pane) {
	for _, h := range p.tspHints {
		p.tspPending = append(p.tspPending, tspPending{SurfaceHint: h})
	}
	p.tspHints = p.tspHints[:0]
	a.tspReveal, a.tspFocus = map[string]string{}, ""
	keep := p.tspPending[:0]
	for _, h := range p.tspPending {
		switch h.Op {
		case "reveal":
			if h.frames >= tspHintFrames {
				// Done, or waited enough.
				continue
			}
			a.tspReveal[h.ID] = h.Arg
			h.frames++
			keep = append(keep, h)
		case "focus":
			a.tspFocus = h.ID
			if h.ID == "" {
				// No field: the keys go to the terminal.
				a.focusReq = p
			}
		case "scroll":
			a.scrollTerm(p, h.Arg)
		}
	}
	p.tspPending = keep
	if len(keep) > 0 {
		c.Invalidate()
	}
}

// revealed acts on a reveal of node n, drawn as el: its scroll container
// brings it into view, at the start or end of the view when the reveal
// says so; for an inline surface, whose box scrolls its content, the
// terminal's scrollback then scrolls to the box when it is outside the
// view.
func (a *App) revealed(p *Pane, sf *rex.Surface, n *rex.TSPNode, el ui.Element) {
	at, ok := a.tspReveal[n.ID]
	if !ok {
		return
	}
	var st *tspScroller
	if sf.Image != 0 {
		st = p.tspScroll[sf.Image]
	}
	inline := st != nil && st.box.H > 0
	if !inline || at != "start" && at != "end" {
		el.ScrollIntoView()
	}
	b, view := el.Bounds(), p.hostBounds
	if b.H == 0 || view.H == 0 {
		return
	}
	// Seen: the reveal is done.
	delete(a.tspReveal, n.ID)
	for i := range p.tspPending {
		if p.tspPending[i].Op == "reveal" && p.tspPending[i].ID == n.ID {
			p.tspPending[i].frames = tspHintFrames
		}
	}
	if !inline {
		return
	}
	// Where the node is in the content, as the last frame laid it out,
	// under the box's padding.
	y := b.Y - st.box.Y - 4 + min(st.Y, st.MaxY)
	switch at {
	case "start":
		st.scrollBy(y - min(st.Y, st.MaxY))
	case "end":
		st.scrollBy(y + b.H - st.view - min(st.Y, st.MaxY))
	}
	box := st.box
	var dy float32
	switch {
	case box.Y < view.Y:
		dy = box.Y - view.Y
	case box.Y+box.H > view.Y+view.H:
		dy = box.Y + box.H - (view.Y + view.H)
	}
	if dy != 0 {
		p.term.Scroll(ui.InputEvent{Kind: ui.InputScroll, X: view.W / 2, Y: view.H / 2, DY: dy, Precise: true})
	}
}

// scrollTerm scrolls by a line, a page or to an end the content of the
// pane's live inline surface, the last opened, or without one the
// pane's terminal.
func (a *App) scrollTerm(p *Pane, by string) {
	_, rows := p.term.Size()
	view := p.hostBounds
	cell := float32(16)
	if rows > 0 && view.H > 0 {
		cell = view.H / float32(rows)
	}
	page := view.H - cell
	var st *tspScroller
	for i := len(p.surfaces) - 1; i >= 0 && st == nil; i-- {
		if sf := &p.surfaces[i]; !sf.Closed && sf.Image != 0 {
			st = p.tspScroll[sf.Image]
		}
	}
	if st != nil {
		page = st.view - cell
	}
	dy := map[string]float32{
		"line-up": -cell, "line-down": cell,
		"page-up": -page, "page-down": page,
		"start": -1e7, "end": 1e7,
	}[by]
	switch {
	case dy == 0:
	case st != nil:
		st.scrollBy(dy)
	default:
		p.term.Scroll(ui.InputEvent{Kind: ui.InputScroll, X: view.W / 2, Y: view.H / 2, DY: dy, Precise: true})
	}
}
