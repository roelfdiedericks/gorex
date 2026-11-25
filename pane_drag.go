package main

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
)

// A pane drags by its header onto another pane: onto one of its edges to
// take that half of it, onto its middle to swap places with it, onto one
// of its own edges to split it, or onto the edge of the tab's tree, to
// take a strip along the whole side and push the other panes over. While
// it drags, the tree shows as the drop would make it, the panes moved
// aside. Held over a tab, the drag opens that tab, to drop the pane there.

// dropZone is where a dragged pane would go on the pane under it.
type dropZone int

const (
	zoneNone dropZone = iota
	zoneLeft
	zoneRight
	zoneTop
	zoneBottom
	zoneCenter
)

const (
	// zoneEdge is how far into a pane, as a share of its size, its edges
	// take a dragged pane.
	zoneEdge = 0.35
	// treeEdge is the band along the edge of the tab's tree, in DIPs, in
	// which a dragged pane takes the whole side.
	treeEdge = 32
	// treeShare is the share of the tree a pane dropped on its edge takes.
	treeShare = 1.0 / 3
	// tabSwitchDelay is how long a pane held over a tab takes to open it.
	tabSwitchDelay = 700 * time.Millisecond
)

// paneDrop is where the pane being dragged would go, as the frame that
// set it saw: a zone of the pane target, or, when outer, the side of
// target's tree.
type paneDrop struct {
	target *Pane
	zone   dropZone
	outer  bool
	frame  uint64
	// layout is the tree as the drop makes it, which the tree shows while
	// the pane drags; rect is where the pane lands in it, in the window.
	// plan is where movePane fits the tab's panes, nil but on a pane's
	// edge.
	layout *layoutNode
	rect   ui.Rect
	plan   *movePlan
}

// tabHover is the tab a dragged pane is held over, and since when.
type tabHover struct {
	tab   *Tab
	since time.Time
}

// paneDragging is the pane drag state of the window.
type paneDragging struct {
	// at is where the pane being dragged would go; dragged is that pane,
	// faded while it drags.
	at      paneDrop
	dragged *Pane
	// tree is where the active tab's tree of splits was in the last
	// frame.
	tree  ui.Rect
	hover tabHover
	// grab is where the pointer took held's header, size the pane's size
	// then, and title its name, for the copy of it held.
	held  *Pane
	grab  ui.Point
	size  ui.Rect
	title string
	// ended is when a pane's drag ended: the presses of drags soon after
	// one another are no double click on a header, which zooms.
	ended time.Time
	// seen is the last view pass that saw the drag: its pane's header
	// dragging, or the pane over the tree or a tab.
	seen uint64
}

// dragGone ends a drag no view pass saw since the last one: one let go
// or given up while its pane was out of sight, as in another tab, whose
// header never saw it end.
func (a *App) dragGone(c *ui.Context) {
	if a.drag.dragged != nil && a.drag.seen != a.frame {
		a.drag.dragged, a.drag.held, a.drag.ended = nil, nil, c.Now()
	}
}

// dragSettle is how long after a drag a header takes no double click.
const dragSettle = 600 * time.Millisecond

// justDragged reports whether a pane was dragged a moment ago.
func (a *App) justDragged(c *ui.Context) bool {
	return a.drag.dragged != nil || c.Now().Sub(a.drag.ended) < dragSettle
}

// canMoveTo reports whether pane p may go into tab t: any pane but a file
// browser.
func canMoveTo(p *Pane, t *Tab) bool {
	return p != nil && t != nil && p.Kind != paneBrowser
}

// dragHandle makes e, a pane's header, drag the pane.
func (a *App) dragHandle(c *ui.Context, e ui.Element, p *Pane) {
	if a.bind.serial != "" || p.Kind == paneBrowser {
		return
	}
	e.Drag(p)
	if e.Dragging() {
		if a.drag.held != p {
			x, y, _ := e.PointerPosition()
			x, y = min(max(x, 0), p.bounds.W), min(max(y, 0), headerH)
			a.drag.held, a.drag.grab, a.drag.size = p, ui.Point{X: x, Y: y}, p.bounds
			a.drag.title, _ = p.label()
		}
		a.drag.dragged, a.drag.seen = p, a.frame
		// MyGo's copy of the header, its buttons fading with it, shows
		// nothing: drawHeld draws the pane held instead.
		e.Invisible()
	} else if a.drag.dragged == p {
		a.drag.dragged, a.drag.held, a.drag.ended = nil, nil, c.Now()
	}
}

// paneDragLook fades card, pane p's, while it drags, and tints it, as it
// shows where it lands.
func (a *App) paneDragLook(card ui.Element, p *Pane) {
	if a.drag.dragged != p {
		return
	}
	card.Opacity(0.6)
	card.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		pt.Fill(r, a.k.busy.Alpha(0.16), cardR)
		pt.Stroke(r, a.k.busy.Alpha(0.9), cardR, 2)
	})
}

// newPaneSlot is where the pane a pane dragged onto its own edge splits
// off would go.
func (a *App) newPaneSlot(c *ui.Context, k *colors, t *Tab) ui.Element {
	name := "New terminal"
	if t.Kind == paneEditor {
		name = "New editor"
	}
	return ui.Box(c.Key("new-pane")).Draw(func(p *ui.Painter, r ui.Rect) {
		drawSpot(p, r, k.text.Alpha(0.04), k.text.Alpha(0.35), k.text.Alpha(0.7), name)
	})
}

// treeDrop makes tree, tab t's tree laid out in r, take panes dragged
// onto it, and returns the tree to show: as the drop under the pointer
// would make it while one drags over it, else as it is.
func (a *App) treeDrop(c *ui.Context, tree ui.Element, t *Tab, r ui.Rect) *layoutNode {
	a.drag.tree = r
	real := copyLayout(t.Root)
	if a.bind.serial != "" {
		return real
	}
	// Where the drop goes follows the panes where they are, not where the
	// preview shows them.
	at := map[*Pane]ui.Rect{}
	real.each(r, func(p *Pane, b ui.Rect) { at[p] = b })
	x, y, in := tree.PointerPosition()
	x, y = r.X+x, r.Y+y
	if v, ok := ui.Drop[*Pane](tree); ok {
		// Where it was let go: the last move and the release may come in
		// one frame, past where the frame before saw it.
		d := a.drag.at
		if in {
			if nd := a.dropOver(v, t, r, at, x, y); nd.target != nil {
				d = nd
			}
		}
		a.drag.at = paneDrop{}
		if d.target == nil || d.target.Tab != t || d.zone == zoneNone {
			return real
		}
		a.later(c, func() { a.movePane(v, d.target, d.zone, d.outer, d.plan) })
		return d.layout
	}
	v, ok := ui.DragOver[*Pane](tree)
	if ok {
		// Back over the tree after dragGone gave it up out of sight.
		a.drag.dragged, a.drag.seen = v, a.frame
	}
	if !ok || !in {
		a.drag.at = paneDrop{}
		return real
	}
	// Over the gap between two panes, the drop stays as it was.
	if d := a.dropOver(v, t, r, at, x, y); d.target != nil {
		a.drag.at = d
	} else if prev := a.drag.at.target; prev == nil || prev.Tab != t {
		a.drag.at = paneDrop{}
	}
	a.drag.at.frame = a.frame
	if a.drag.at.zone == zoneNone {
		return real
	}
	return a.drag.at.layout
}

// dropOver is where pane v would go dropped at (x, y), in the window, on
// tab t's tree laid out in tree, its panes where at has them: on none,
// over a gap.
func (a *App) dropOver(v *Pane, t *Tab, tree ui.Rect, at map[*Pane]ui.Rect, x, y float32) paneDrop {
	d := paneDrop{frame: a.frame}
	var b ui.Rect
	for p, pb := range at {
		if x >= pb.X && x < pb.X+pb.W && y >= pb.Y && y < pb.Y+pb.H {
			d.target, b = p, pb
		}
	}
	target := d.target
	if target == nil || !canMoveTo(v, t) {
		return d
	}
	if side, ok := treeSide(tree, x, y); ok && t.Root.Pane == nil {
		d.zone, d.outer = side, true
	} else {
		d.zone = zoneFor(x-b.X, y-b.Y, b.W, b.H)
		if d.zone == zoneCenter && (v == target || !canMoveTo(target, v.Tab)) {
			d.zone = zoneNone
		}
	}
	if d.zone == zoneNone {
		return d
	}
	real := copyLayout(t.Root)
	switch {
	case v == target && !d.outer:
		// On its own edge, the pane takes that half of its place, a new
		// one the other.
		me, fresh := &layoutNode{pane: v}, &layoutNode{}
		s := &layoutNode{vertical: d.zone == zoneTop || d.zone == zoneBottom, ratio: 0.5, a: me, b: fresh}
		if d.zone == zoneRight || d.zone == zoneBottom {
			s.a, s.b = fresh, me
		}
		d.layout = real.replace(v, s)
	case d.zone == zoneCenter:
		d.layout = real.swap(v, target)
	default:
		d.layout, d.plan = landing(v, target, d.zone, d.outer, b, tree, at)
	}
	if d.layout == nil {
		d.zone = zoneNone
		return d
	}
	d.rect, _ = d.layout.find(v, tree)
	return d
}

// layoutNode is a node of a copy of a tree of splits, to lay out the tree
// a move would make without making it: a pane, a new pane when a leaf
// with none, or a split; node is the tree's own split it copies.
type layoutNode struct {
	pane     *Pane
	node     *Node
	vertical bool
	ratio    float32
	a, b     *layoutNode
}

func copyLayout(n *Node) *layoutNode {
	if n.Pane != nil {
		return &layoutNode{pane: n.Pane}
	}
	return &layoutNode{node: n, vertical: n.Vertical, ratio: n.Ratio, a: copyLayout(n.A), b: copyLayout(n.B)}
}

// leaf reports whether n is a pane, not a split.
func (n *layoutNode) leaf() bool { return n.a == nil }

// without is the tree n without pane p, the other side of p's split
// taking its place, as unlinkPane does; nil when p was all of it.
func (n *layoutNode) without(p *Pane) *layoutNode {
	if n.leaf() {
		if n.pane == p {
			return nil
		}
		return n
	}
	a, b := n.a.without(p), n.b.without(p)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	return &layoutNode{vertical: n.vertical, ratio: n.ratio, a: a, b: b}
}

// beside is the tree n with pane p put on a side of the node of pane
// target, or of the whole tree when target is nil, as insertBeside does.
func (n *layoutNode) beside(target, p *Pane, zone dropZone, share float32) *layoutNode {
	if target != nil && n.pane != target {
		if n.leaf() {
			return n
		}
		return &layoutNode{vertical: n.vertical, ratio: n.ratio, a: n.a.beside(target, p, zone, share), b: n.b.beside(target, p, zone, share)}
	}
	pn := &layoutNode{pane: p}
	s := &layoutNode{vertical: zone == zoneTop || zone == zoneBottom}
	if zone == zoneLeft || zone == zoneTop {
		s.a, s.b, s.ratio = pn, n, share
	} else {
		s.a, s.b, s.ratio = n, pn, 1-share
	}
	return s
}

// replace is the tree n with with in place of pane p.
func (n *layoutNode) replace(p *Pane, with *layoutNode) *layoutNode {
	if n.leaf() {
		if n.pane == p {
			return with
		}
		return n
	}
	c := *n
	c.a, c.b = n.a.replace(p, with), n.b.replace(p, with)
	return &c
}

// swap is the tree n with panes p and q in each other's places; with q
// in it alone, as of another tab, p in its place.
func (n *layoutNode) swap(p, q *Pane) *layoutNode {
	if n.leaf() {
		switch n.pane {
		case p:
			return &layoutNode{pane: q}
		case q:
			return &layoutNode{pane: p}
		}
		return n
	}
	c := *n
	c.a, c.b = n.a.swap(p, q), n.b.swap(p, q)
	return &c
}

// find is the box of pane p in the tree n laid out in r: each split's
// two sides share its room less the gap.
func (n *layoutNode) find(p *Pane, r ui.Rect) (ui.Rect, bool) {
	if n.leaf() {
		return r, n.pane == p && p != nil
	}
	ra, rb := n.split(r)
	if got, ok := n.a.find(p, ra); ok {
		return got, true
	}
	return n.b.find(p, rb)
}

// each calls fn with each pane of n laid out in r and its box.
func (n *layoutNode) each(r ui.Rect, fn func(*Pane, ui.Rect)) {
	if n.leaf() {
		if n.pane != nil {
			fn(n.pane, r)
		}
		return
	}
	ra, rb := n.split(r)
	n.a.each(ra, fn)
	n.b.each(rb, fn)
}

// placed is a pane of a tree laid out and its box; a new pane's when
// pane is nil.
type placed struct {
	pane *Pane
	rect ui.Rect
}

// divider is the bar between the sides of the tree's split node, laid
// out in rect, the split in box; node is nil for a split a move makes.
type divider struct {
	node      *Node
	rect, box ui.Rect
}

// place lays out the tree n in r: its panes and dividers.
func (n *layoutNode) place(r ui.Rect, panes *[]placed, divs *[]divider) {
	r.W, r.H = max(r.W, 0), max(r.H, 0)
	if n.leaf() {
		*panes = append(*panes, placed{n.pane, r})
		return
	}
	ra, rb := n.split(r)
	d := divider{node: n.node, box: r, rect: ui.Rect{X: ra.X + ra.W, Y: r.Y, W: gap, H: r.H}}
	if n.vertical {
		d.rect = ui.Rect{X: r.X, Y: ra.Y + ra.H, W: r.W, H: gap}
	}
	*divs = append(*divs, d)
	n.a.place(ra, panes, divs)
	n.b.place(rb, panes, divs)
}

// movePlan is where the panes of a tab should be after a move, in the
// window: the splits' ratios are fitted to it (fit).
type movePlan struct {
	tree ui.Rect
	want map[*Pane]ui.Rect
}

// landing is the tree pane v moved to a zone of target, whose box is b,
// makes of target's tree laid out in tree, its panes where at has them,
// and the plan movePane fits the tree to. On a pane, v takes the half of
// target under the pointer and target the other; every other pane keeps
// its box, the room v leaves going to those beside it. On the tree's
// edge, v takes a strip of it.
func landing(v, target *Pane, zone dropZone, outer bool, b, tree ui.Rect, at map[*Pane]ui.Rect) (*layoutNode, *movePlan) {
	t := target.Tab
	root := copyLayout(t.Root)
	if v.Tab == t {
		root = root.without(v)
	}
	if root == nil {
		return nil, nil
	}
	if outer {
		return root.beside(nil, v, zone, treeShare), nil
	}
	plan := &movePlan{tree: tree, want: map[*Pane]ui.Rect{}}
	for p, r := range at {
		if p != v {
			plan.want[p] = r
		}
	}
	plan.want[target], plan.want[v] = halves(b, zone)
	root = root.beside(target, v, zone, 0.5)
	root.fit(tree, plan.want)
	return root, plan
}

// halves splits box b, less the gap, into what stays and what a zone
// takes.
func halves(b ui.Rect, zone dropZone) (stay, take ui.Rect) {
	stay, take = b, b
	switch zone {
	case zoneLeft, zoneRight:
		w := (b.W - gap) / 2
		stay.W, take.W = w, w
		if zone == zoneLeft {
			stay.X = b.X + w + gap
		} else {
			take.X = b.X + w + gap
		}
	case zoneTop, zoneBottom:
		h := (b.H - gap) / 2
		stay.H, take.H = h, h
		if zone == zoneTop {
			stay.Y = b.Y + h + gap
		} else {
			take.Y = b.Y + h + gap
		}
	}
	return stay, take
}

// fit sets the ratios of the splits of n, laid out in r, for its panes
// to fall where want has them: each split falls halfway between the
// boxes its two sides want, so that room no pane wants is shared by the
// panes either side of it.
func (n *layoutNode) fit(r ui.Rect, want map[*Pane]ui.Rect) {
	if n.leaf() {
		return
	}
	a, okA := n.a.wanted(want)
	b, okB := n.b.wanted(want)
	if okA && okB {
		start, length, end, next := r.X, r.W, a.X+a.W, b.X
		if n.vertical {
			start, length, end, next = r.Y, r.H, a.Y+a.H, b.Y
		}
		if length > gap {
			n.ratio = min(max(((end+next)/2-gap/2-start)/(length-gap), 0.05), 0.95)
		}
	}
	ra, rb := n.split(r)
	n.a.fit(ra, want)
	n.b.fit(rb, want)
}

// wanted is the box around what want has for the panes of n.
func (n *layoutNode) wanted(want map[*Pane]ui.Rect) (ui.Rect, bool) {
	if n.leaf() {
		r, ok := want[n.pane]
		return r, ok
	}
	a, okA := n.a.wanted(want)
	b, okB := n.b.wanted(want)
	switch {
	case !okA:
		return b, okB
	case !okB:
		return a, true
	}
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

// split is the boxes of n's two sides laid out in r.
func (n *layoutNode) split(r ui.Rect) (ui.Rect, ui.Rect) {
	ra, rb := r, r
	if n.vertical {
		h := (r.H - gap) * n.ratio
		ra.H, rb.Y, rb.H = h, r.Y+h+gap, r.H-h-gap
	} else {
		w := (r.W - gap) * n.ratio
		ra.W, rb.X, rb.W = w, r.X+w+gap, r.W-w-gap
	}
	return ra, rb
}

// applyRatios gives the splits of the tree n the ratios of its copy l,
// of the same shape.
func applyRatios(n *Node, l *layoutNode) {
	if n.Pane != nil || l.leaf() {
		return
	}
	n.Ratio = l.ratio
	applyRatios(n.A, l.a)
	applyRatios(n.B, l.b)
}

// zoneFor is the zone of a box of w by h that (x, y) in it is in.
func zoneFor(x, y, w, h float32) dropZone {
	if w <= 0 || h <= 0 {
		return zoneNone
	}
	fx, fy := x/w, y/h
	zone, near := zoneCenter, float32(zoneEdge)
	for _, s := range []struct {
		zone dropZone
		d    float32
	}{{zoneLeft, fx}, {zoneRight, 1 - fx}, {zoneTop, fy}, {zoneBottom, 1 - fy}} {
		if s.d < near {
			zone, near = s.zone, s.d
		}
	}
	return zone
}

// treeSide is the side of the tree, whose box is r, that (x, y) is
// within treeEdge of.
func treeSide(r ui.Rect, x, y float32) (dropZone, bool) {
	if r.W <= 4*treeEdge || r.H <= 4*treeEdge {
		return zoneNone, false
	}
	switch {
	case x-r.X < treeEdge:
		return zoneLeft, true
	case r.X+r.W-x < treeEdge:
		return zoneRight, true
	case y-r.Y < treeEdge:
		return zoneTop, true
	case r.Y+r.H-y < treeEdge:
		return zoneBottom, true
	}
	return zoneNone, false
}

// drawSpot draws a box a pane goes to in a drop, its name in the middle.
func drawSpot(p *ui.Painter, r ui.Rect, fill, stroke, text ui.Color, name string) {
	const inset = 4
	r = ui.Rect{X: r.X + inset, Y: r.Y + inset, W: r.W - 2*inset, H: r.H - 2*inset}
	if r.W <= 0 || r.H <= 0 {
		return
	}
	p.Fill(r, fill, cardR-2)
	p.Stroke(r, stroke, cardR-2, 2)
	if name == "" {
		return
	}
	label := ui.Span{Text: name, Size: 15, Weight: 600, Color: text}
	w, h := p.MeasureText(0, label)
	p.Clip(r, cardR-2, func() {
		p.RichText(r.X+max((r.W-w)/2, 8), r.Y+(r.H-h)/2, 0, label)
	})
}

// heldMax is the most a pane held by its header shows of its size.
var heldMax = ui.Rect{W: 420, H: 280}

// drawHeld draws a light copy of the pane being dragged where the
// pointer, at (x, y) in the window, holds it by its header.
func (a *App) drawHeld(p *ui.Painter, x, y float32) {
	if a.drag.dragged == nil || a.drag.held != a.drag.dragged {
		return
	}
	k, sz := a.k, a.drag.size
	s := min(1, heldMax.W/max(sz.W, 1), heldMax.H/max(sz.H, 1))
	r := ui.Rect{X: x - a.drag.grab.X*s, Y: y - a.drag.grab.Y, W: sz.W * s, H: sz.H * s}
	if r.W <= 0 || r.H <= 0 {
		return
	}
	p.Fill(r, k.cardFocused.Alpha(0.55), cardR)
	p.Stroke(r, k.busy.Alpha(0.6), cardR, 1.5)
	p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + headerH, W: r.W - 2, H: 1}, k.cardBorder, 0)
	label := ui.Span{Text: a.drag.title, Size: 12.5, Weight: 600, Color: k.text.Alpha(0.8)}
	_, h := p.MeasureText(0, label)
	p.Clip(ui.Rect{X: r.X, Y: r.Y, W: r.W - 8, H: headerH}, 0, func() {
		p.RichText(r.X+12, r.Y+(headerH-h)/2, 0, label)
	})
}

// hoverTab opens tab t, the i-th, once a pane has been held over it for
// tabSwitchDelay.
func (a *App) hoverTab(c *ui.Context, i int, t *Tab) {
	if i == a.active {
		return
	}
	now := c.Now()
	if a.drag.hover.tab != t {
		a.drag.hover = tabHover{t, now}
	}
	if wait := tabSwitchDelay - now.Sub(a.drag.hover.since); wait > 0 {
		c.After(wait)
		return
	}
	a.drag.hover = tabHover{}
	a.selectTab(i)
}

// dropOnTab moves pane p into tab t, beside the pane focused there.
func (a *App) dropOnTab(p *Pane, t *Tab) {
	if p.Tab == t || !canMoveTo(p, t) {
		return
	}
	if target := t.treeFocus(); target != nil {
		a.movePane(p, target, zoneRight, false, nil)
	}
}

// canMoveOut reports whether pane p may go into a tab of its own: not a
// file browser, one alone in a tab of its kind already, nor one of a
// serial port's window, which has one pane.
func (a *App) canMoveOut(p *Pane) bool {
	if p == nil || p.closed || p.Kind == paneBrowser || a.bind.serial != "" {
		return false
	}
	return p.Tab.Root.Pane != p || p.Tab.Kind != p.Kind
}

// movePaneToNewTab moves pane p, its session still running, into a tab
// of its own after its tab: a terminal into a terminal tab, and a file
// into an editor tab whose tree opens at its folder. The tab it leaves
// closes when it is left empty, but for an editor tab, which keeps its
// tree and takes an empty editor in the terminal's place.
func (a *App) movePaneToNewTab(p *Pane) {
	if !a.canMoveOut(p) {
		return
	}
	if p.Kind == paneEditor {
		dir := ""
		if p.edit != nil && p.edit.path != "" {
			dir = dirName(p.edit.path)
		}
		t := a.newEditorTab(dir)
		if t == nil {
			return
		}
		a.unlinkPane(p)
		a.putInTab(t, p)
		a.showPane(p)
		return
	}
	from := p.Tab
	a.leaveTab(p)
	t := &Tab{ID: a.id()}
	t.Root = &Node{ID: a.id(), Pane: p}
	p.Tab, p.Node = t, t.Root
	at := len(a.tabs)
	if i := slices.Index(a.tabs, from); i >= 0 {
		at = i + 1
	}
	a.tabs = slices.Insert(a.tabs, at, t)
	a.active = at
	a.showPane(p)
}

// leaveTab takes pane p out of its tab, which closes when it is left
// empty; but a terminal alone in an editor tab leaves an empty editor in
// its place, for the tab to keep its tree.
func (a *App) leaveTab(p *Pane) {
	if t := p.Tab; p.Kind == paneTerminal && t.Kind == paneEditor && t.Root.Pane == p {
		a.replacePane(p, a.newEditorPane(""))
		return
	}
	a.unlinkPane(p)
}

// replacePane puts pane q in pane p's place in its tab, focused there,
// and takes p out of the tab.
func (a *App) replacePane(p, q *Pane) {
	t, n := p.Tab, p.Node
	n.Pane, q.Node, q.Tab = q, n, t
	if t.Zoom == p {
		t.Zoom = nil
	}
	t.recent = slices.DeleteFunc(t.recent, func(x *Pane) bool { return x == p })
	if t.Focus == p {
		t.Focus = nil
		t.setFocus(q)
	}
}

// movePane moves pane p, its session still running, to a zone of pane
// target, a half of it, or to its place, swapping the two; when outer, to
// a strip along that side of target's tree. The tree's splits then fit
// plan, when there is one (landing).
func (a *App) movePane(p, target *Pane, zone dropZone, outer bool, plan *movePlan) {
	t := target.Tab
	if zone == zoneNone || p.closed || target.closed || !canMoveTo(p, t) {
		return
	}
	if zone == zoneCenter {
		if p != target && !outer {
			a.swapPanes(p, target)
		}
		return
	}
	if outer && t.Root.Pane == p {
		return
	}
	if p == target && !outer {
		a.splitOff(p, zone)
		return
	}
	a.leaveTab(p)
	n, share := target.Node, float32(0.5)
	if outer {
		n, share = t.Root, treeShare
	}
	a.insertBeside(n, p, zone, share)
	if plan != nil {
		l := copyLayout(t.Root)
		l.fit(plan.tree, plan.want)
		applyRatios(t.Root, l)
	}
	a.showPane(p)
}

// splitOff splits pane p, p going to a zone of its place and a new pane,
// as Split Right or Split Down makes, to the other half.
func (a *App) splitOff(p *Pane, zone dropZone) {
	vertical := zone == zoneTop || zone == zoneBottom
	if a.splitPane(p, vertical) == nil {
		return
	}
	if n := p.Node.Parent; zone == zoneRight || zone == zoneBottom {
		n.A, n.B = n.B, n.A
	}
	a.showPane(p)
}

// insertBeside puts pane p on a side of node n, taking share of it: n's
// content moves into a node of its own beside p's.
func (a *App) insertBeside(n *Node, p *Pane, zone dropZone, share float32) {
	inner := &Node{ID: a.id(), Pane: n.Pane, Parent: n, Vertical: n.Vertical, Ratio: n.Ratio, A: n.A, B: n.B}
	if inner.Pane != nil {
		inner.Pane.Node = inner
	} else {
		inner.A.Parent, inner.B.Parent = inner, inner
	}
	pn := &Node{ID: a.id(), Pane: p, Parent: n}
	p.Node, p.Tab = pn, inner.tab()
	n.Pane, n.Vertical = nil, zone == zoneTop || zone == zoneBottom
	if zone == zoneLeft || zone == zoneTop {
		n.A, n.B, n.Ratio = pn, inner, share
	} else {
		n.A, n.B, n.Ratio = inner, pn, 1-share
	}
}

// tab is the tab of a node's panes.
func (n *Node) tab() *Tab {
	if ps := n.panes(); len(ps) > 0 {
		return ps[0].Tab
	}
	return nil
}

// swapPanes swaps the places of two panes, of one tab or two.
func (a *App) swapPanes(p, q *Pane) {
	pt, qt := p.Tab, q.Tab
	if !canMoveTo(p, qt) || !canMoveTo(q, pt) {
		return
	}
	pn, qn := p.Node, q.Node
	pn.Pane, qn.Pane = q, p
	p.Node, q.Node = qn, pn
	p.Tab, q.Tab = qt, pt
	if pt != qt {
		for _, s := range []struct {
			t       *Tab
			out, in *Pane
		}{{pt, p, q}, {qt, q, p}} {
			if s.t.Zoom == s.out {
				s.t.Zoom = nil
			}
			s.t.recent = slices.DeleteFunc(s.t.recent, func(x *Pane) bool { return x == s.out })
			if s.t.Focus == s.out {
				s.t.Focus = nil
				s.t.setFocus(s.in)
			}
		}
	}
	a.showPane(p)
}

// showPane shows the tab of a pane just moved, the pane focused.
func (a *App) showPane(p *Pane) {
	t := p.Tab
	t.Zoom = nil
	if i := slices.Index(a.tabs, t); i >= 0 && i != a.active {
		a.selectTab(i)
	}
	t.setFocus(p)
	a.focusReq = p
	a.drag.hover = tabHover{}
	a.changed()
}
