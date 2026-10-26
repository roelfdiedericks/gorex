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
// brings it into view, and the terminal's scrollback scrolls to it when
// it is drawn among the terminal's rows outside the view.
func (a *App) revealed(p *Pane, sf *rex.Surface, n *rex.TSPNode, el ui.Element) {
	at, ok := a.tspReveal[n.ID]
	if !ok {
		return
	}
	el.ScrollIntoView()
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
	if sf.Image == 0 {
		return
	}
	var dy float32
	switch {
	case at == "start" || (at != "end" && b.Y < view.Y):
		dy = b.Y - view.Y
	case b.Y+b.H > view.Y+view.H || at == "end":
		dy = b.Y + b.H - (view.Y + view.H)
	}
	if dy != 0 {
		p.term.Scroll(ui.InputEvent{Kind: ui.InputScroll, X: view.W / 2, Y: view.H / 2, DY: dy, Precise: true})
	}
}

// scrollTerm scrolls the pane's terminal by a line, a page or to an end.
func (a *App) scrollTerm(p *Pane, by string) {
	_, rows := p.term.Size()
	view := p.hostBounds
	cell := float32(16)
	if rows > 0 && view.H > 0 {
		cell = view.H / float32(rows)
	}
	dy := map[string]float32{
		"line-up": -cell, "line-down": cell,
		"page-up": -(view.H - cell), "page-down": view.H - cell,
		"start": -1e7, "end": 1e7,
	}[by]
	if dy != 0 {
		p.term.Scroll(ui.InputEvent{Kind: ui.InputScroll, X: view.W / 2, Y: view.H / 2, DY: dy, Precise: true})
	}
}
