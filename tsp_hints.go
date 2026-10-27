package main

import (
	"math"

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
// brings it into view, and for a node of an inline surface's content the
// terminal's scrollback scrolls to it when it is outside the view, the
// dock pinned at the bottom left out of the view. A node of the dock or
// of an overlay (tspPinned) shows where it is pinned, which the
// scrollback does not move.
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
	_, rows := p.term.Size()
	if sf.Image == 0 || a.tspPinned || rows <= 0 {
		return
	}
	cell := view.H / float32(rows)
	view.H -= p.dockOver()
	var dy float32
	switch {
	case at == "start" || (at != "end" && b.Y < view.Y):
		dy = b.Y - view.Y
	case b.Y+b.H > view.Y+view.H || at == "end":
		dy = b.Y + b.H - (view.Y + view.H)
	}
	// Whole rows, as far as shows all of it.
	if dy < 0 {
		p.term.ScrollRows(int(math.Floor(float64(dy / cell))))
	} else if dy > 0 {
		p.term.ScrollRows(int(math.Ceil(float64(dy / cell))))
	}
}

// scrollTerm scrolls the pane's scrollback by a line, a page or to an
// end: a page is the view's rows less one, and less those the dock of a
// live surface pinned at the bottom covers.
func (a *App) scrollTerm(p *Pane, by string) {
	_, rows := p.term.Size()
	page := rows - 1
	if view := p.hostBounds; rows > 0 && view.H > 0 {
		page -= int(math.Ceil(float64(p.dockOver() / (view.H / float32(rows)))))
	}
	page = max(page, 1)
	switch by {
	case "line-up":
		p.term.ScrollRows(-1)
	case "line-down":
		p.term.ScrollRows(1)
	case "page-up":
		p.term.ScrollRows(-page)
	case "page-down":
		p.term.ScrollRows(page)
	case "start":
		p.term.ScrollToTop()
	case "end":
		p.term.ScrollToBottom()
	}
}

// dockOver is how tall the dock of the pane's live surface is where it
// is pinned over the bottom of the view, its padding included; 0
// without one.
func (p *Pane) dockOver() float32 {
	for i := range p.surfaces {
		if sf := &p.surfaces[i]; !sf.Closed && sf.Image != 0 && tspDocked(sf) {
			if h := p.dockH[sf.Image]; h > 0 {
				return h + 8
			}
		}
	}
	return 0
}

// tspDockLive reports whether the pane shows a live inline surface with
// a dock and nothing in its layer region over it (an overlay, a picker,
// a sheet, whose keys are the program's): its composer then takes the
// keys the terminal would, and the page keys scroll the transcript.
func (p *Pane) tspDockLive() bool {
	for i := range p.surfaces {
		sf := &p.surfaces[i]
		if sf.Closed || sf.Image == 0 || !tspDocked(sf) {
			continue
		}
		if layer := tspLayer(sf); layer != nil {
			for _, n := range layer.Children {
				if hidden, _ := n.Props["hidden"].(bool); !hidden {
					return false
				}
			}
		}
		return true
	}
	return false
}
