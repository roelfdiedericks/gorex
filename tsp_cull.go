package main

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A long transcript, as omp's of a day's work, is a list of hundreds of
// nodes under one of its surface's regions, most of them far out of the
// pane's view. Built and laid out every frame, they made each frame cost
// tens of milliseconds, whatever moved: a scroll, a line of a command's
// output, a working indicator. So the nodes of such a list that a frame
// measured, and that have not changed since, are drawn as space of their
// height while they are farther from the view than its height; those
// nearer, those never measured and those that changed are drawn whole.
//
// A pane made narrower or wider changes the height of every node, which
// laid out all at once cost half a second a frame while the window was
// dragged. So the surface settles instead: those far from the view keep
// the height measured at the old width while the width changes, the live
// surface's content laid out from its end (as when its head is cut), so
// that the view shows its end right whatever is above; once the width
// stands, a few of them a frame are drawn again and measured, nearest
// first, until all are, and the program has the rows they take.

// tspCullMin is how many children a node has before they are culled.
const tspCullMin = 32

// tspRemeasure is how many nodes measured at another width or style a
// settling surface draws again a frame, to measure them.
const tspRemeasure = 24

// tspSettleFrames is how many frames a surface settles at most after the
// last change of the width, whether or not the program took its rows.
const tspSettleFrames = 600

// tspCullOff turns culling off, for the debug verb cull.
var tspCullOff bool

// tspCullKey names a node of a pane's surfaces: by its surface's anchor,
// as each program's surfaces have ids of their own.
type tspCullKey struct {
	img uint32
	id  string
}

// tspSizedNode is what the frames drew and measured of a child of a long
// list. A pass's Bounds are of the last frame's layout: they measure the
// node as it was drawn then (prev), when it has not changed since.
type tspSizedNode struct {
	node, pnode   *rex.TSPNode
	rev, prev     uint64
	drawn, pdrawn uint64
	// mnode at mrev was measured off below the surface's box, h tall, in
	// a pane w wide with the style gen.
	mnode  *rex.TSPNode
	mrev   uint64
	off, h float32
	w      float32
	gen    int
	// focus tells that the keyboard focus was in it as it was last drawn,
	// as in text the user selected: it stays drawn until the focus goes.
	focus bool
	// seen is the last frame that drew its list.
	seen uint64
}

// tspSettle is how a surface settles after its pane's width or the style
// changed: on from the change until no node is left measured as it was
// before (left) and the program took the rows the content needs (synced),
// at the latest tspSettleFrames frames after the change (at).
type tspSettle struct {
	w      float32
	gen    int
	on     bool
	at     uint64
	left   int
	synced bool
}

// settling updates and reports whether the inline surface sf settles in
// this frame.
func (a *App) settling(p *Pane, sf *rex.Surface) *tspSettle {
	if p.tspSettles == nil {
		p.tspSettles = map[uint32]*tspSettle{}
	}
	s := p.tspSettles[sf.Image]
	if s == nil {
		s = &tspSettle{}
		p.tspSettles[sf.Image] = s
	}
	if w := p.hostBounds.W; w > 0 {
		if s.w != 0 && (w != s.w || a.tspGen != s.gen) {
			s.on, s.at, s.synced = true, a.drawn, false
		}
		s.w, s.gen = w, a.tspGen
	}
	if s.on && a.drawn > s.at+2 && (s.left == 0 && s.synced || a.drawn > s.at+tspSettleFrames) {
		s.on = false
	}
	return s
}

// tspCulling is how the inline surface being drawn culls its lists: its
// box's top in the last frame's layout (origin) and in this frame's
// (originNow), the band of the window near the view, the pane's width,
// and the nodes drawn whatever their place: those a hint waits for (a
// reveal, a focus, an overlay's anchor) and the nodes over them. While
// the surface settles, nodes measured at another width or style are
// culled too, and once the width stands (stable) those picked are drawn
// to be measured; stale counts those left as they were.
type tspCulling struct {
	on                bool
	origin, originNow float32
	top, bottom       float32
	w                 float32
	keep              map[string]bool
	settle, stable    bool
	pick              map[*rex.TSPNode]bool
	stale             int
}

// startCulling sets how the inline surface sf, whose box was last laid
// out at box and is placed top below the view's top in this frame, culls
// its lists. A pane shown again, as its tab is, has no layout of the
// last frame: what was measured before stands, in the view it had.
func (a *App) startCulling(p *Pane, sf *rex.Surface, s *tspSettle, box ui.Rect, top float32) {
	view := p.hostBounds
	if view.H > 0 {
		p.tspView = view
	} else {
		// Placed as the terminal last painted, which may be long ago:
		// the next frame draws what this one placed.
		view = p.tspView
		a.services.Invalidate()
	}
	a.tspCull = tspCulling{}
	if view.H <= 0 || tspCullOff {
		return
	}
	a.tspCull = tspCulling{
		on:        true,
		origin:    box.Y,
		originNow: view.Y + top,
		top:       view.Y - view.H,
		bottom:    view.Y + 2*view.H,
		w:         view.W,
		settle:    s.on,
		stable:    s.on && a.drawn > s.at+2,
	}
	keep := func(id string) {
		if a.tspCull.keep == nil {
			a.tspCull.keep = map[string]bool{}
		}
		p.mirror.Ancestors(sf, id, func(n *rex.TSPNode) { a.tspCull.keep[n.ID] = true })
	}
	for id := range a.tspReveal {
		keep(id)
	}
	if a.tspFocus != "" {
		keep(a.tspFocus)
	}
	for id := range p.tspAnchorWant {
		keep(id)
	}
}

// endCulling ends a surface's culling, telling s how many nodes are left
// to measure again, and forgets now and then what was measured of nodes
// no frame drew of late.
func (a *App) endCulling(p *Pane, s *tspSettle) {
	if a.tspCull.on {
		s.left = a.tspCull.stale
		if s.on && s.left > 0 && a.tspCull.stable {
			a.services.Invalidate()
		}
	}
	a.tspCull = tspCulling{}
	if a.drawn%256 != 0 {
		return
	}
	for key, e := range p.tspSized {
		if e.seen+256 < a.drawn {
			delete(p.tspSized, key)
		}
	}
}

// far reports how far a node measured as e is from the band near the
// view, 0 for one in it.
func (cu *tspCulling) far(e *tspSizedNode) float32 {
	y := cu.originNow + e.off
	return max(cu.top-(y+e.h), y-cu.bottom, 0)
}

// tspCulledChildren draws a long list's children: those measured as they
// are, far from the view, as space of their height.
func (a *App) tspCulledChildren(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	cu := &a.tspCull
	if p.tspSized == nil {
		p.tspSized = map[tspCullKey]*tspSizedNode{}
	}
	entry := func(ch *rex.TSPNode) *tspSizedNode {
		key := tspCullKey{sf.Image, ch.ID}
		e := p.tspSized[key]
		if e == nil {
			e = &tspSizedNode{}
			p.tspSized[key] = e
		}
		return e
	}
	// measured tells that e is ch as it was measured, and fresh that the
	// measure is good for this width and style.
	measured := func(e *tspSizedNode, ch *rex.TSPNode) bool {
		return e.mnode == ch && e.mrev == ch.Rev && !e.focus && !cu.keep[ch.ID]
	}
	fresh := func(e *tspSizedNode) bool { return e.gen == a.tspGen && e.w == cu.w }
	if cu.stable {
		// The nearest of those measured as they were, drawn to measure.
		type stale struct {
			ch   *rex.TSPNode
			dist float32
		}
		var all []stale
		for _, ch := range n.Children {
			if e := entry(ch); measured(e, ch) && !fresh(e) {
				all = append(all, stale{ch, cu.far(e)})
			}
		}
		slices.SortFunc(all, func(x, y stale) int {
			switch {
			case x.dist < y.dist:
				return -1
			case x.dist > y.dist:
				return 1
			}
			return 0
		})
		cu.pick = map[*rex.TSPNode]bool{}
		for _, s := range all[:min(len(all), tspRemeasure)] {
			cu.pick[s.ch] = true
		}
	}
	for _, ch := range n.Children {
		e := entry(ch)
		e.seen = a.drawn
		if measured(e, ch) && cu.far(e) > 0 {
			switch {
			case fresh(e):
				ui.Box(c.Key("cull-" + ch.ID)).Height(e.h).FillWidth()
				continue
			case cu.settle && !cu.pick[ch]:
				cu.stale++
				ui.Box(c.Key("cull-" + ch.ID)).Height(e.h).FillWidth()
				continue
			}
		}
		if cu.settle && measured(e, ch) && !fresh(e) && cu.pick[ch] {
			// Drawn to be measured: left until it is.
			cu.stale++
		}
		if e.drawn != a.drawn {
			e.pnode, e.prev, e.pdrawn = e.node, e.rev, e.drawn
			e.node, e.rev, e.drawn = ch, ch.Rev, a.drawn
		}
		el := a.tspNode(c, k, t, p, sf, ch)
		if !el.Valid() {
			// Nothing drawn (hidden, blank, or drawn elsewhere, as an
			// overlay): nothing to stand in for.
			e.mnode = nil
			continue
		}
		e.focus = el.FocusWithin()
		// Its bounds are the last frame's, which drew it as it is.
		if b := el.Bounds(); b.H > 0 && e.pdrawn+1 == a.drawn && e.pnode == ch && e.prev == ch.Rev {
			e.mnode, e.mrev, e.off, e.h, e.w, e.gen = ch, ch.Rev, b.Y-cu.origin, b.H, cu.w, a.tspGen
		}
	}
}
