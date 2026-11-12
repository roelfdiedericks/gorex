package main

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"gorex/internal/rex"
)

// A surface of the Tern Surface Protocol (TSP.md) draws over the pane's
// terminal: the program's tree of nodes, laid out in MyGo's UI. A
// screen surface fills the pane while it is open. An inline surface
// draws at the rows its anchor takes in the terminal (internal/rex
// tsp.go), which scroll with the output: as tall as its content, which
// the window measures and tells the server, whose anchor then takes
// that many rows; closed with keep, it stays drawn there, in the rows it
// had. The kinds drawn are those the server's hello reply names
// (internal/rex tspKinds); a node of another kind draws nothing, as the
// program was told, and falls back itself. A click on a node that has a
// click action goes to the program as an action event.

// surfaces draws the pane's surfaces over its terminal, in the box the
// terminal's view fills.
func (a *App) surfaces(c *ui.Context, k *colors, t *Tab, p *Pane) {
	a.tspPane, a.tspLinkClicked = p, false
	defer func() { a.tspPane = nil }()
	a.takeHints(c, p)
	// The nodes the overlays were anchored at in the last frame record
	// where they draw in this one (overlayAnchor).
	p.tspAnchorWant, p.tspAnchorNext, p.tspAnchorAt = p.tspAnchorNext, nil, nil
	if len(p.tspAnchorWant) > 0 {
		p.tspAnchorAt = make(map[string]tspAnchorBox, len(p.tspAnchorWant))
	}
	var placements []terminal.AppPlacement
	for i := range p.surfaces {
		sf := &p.surfaces[i]
		// The surface's colors: its program's palette over GoRex's.
		k := a.surfaceColors(k, sf)
		switch {
		case sf.Mode == "screen":
			a.screenSurface(c, k, t, p, sf)
		case sf.Image != 0:
			if placements == nil {
				placements = p.term.AppPlacements()
			}
			// Its anchor (one cell) and its end marker (two): from the
			// anchor, or when the scrollback dropped it, up from the end,
			// its head cut.
			var start, end *terminal.AppPlacement
			for i := range placements {
				switch pl := &placements[i]; {
				case pl.Image != sf.Image:
				case pl.Cols == 2:
					end = pl
				default:
					start = pl
				}
			}
			switch {
			case start != nil:
				a.inlineSurface(c, k, t, p, sf, *start, false)
			case end != nil:
				pl, up := *end, max(sf.Rows, 1)-1
				pl.Row -= up
				pl.Rect.Y -= pl.Rect.H / float32(max(pl.Rows, 1)) * float32(up)
				a.inlineSurface(c, k, t, p, sf, pl, true)
			}
		}
	}
	a.tspPal = nil
	a.tspJump(c, k, p)
	a.surfaceZoom(c, k, t, p)
}

// tspJump draws, while a live omp transcript is scrolled back, a round ↓
// button at the pane's right edge above the composer, as Tern does: it
// widens to tell how many lines are below as the pointer comes over it,
// and a click brings the view to the end. It takes no focus, so the keys
// stay with the composer, and hands the wheel to the terminal. Its width
// is animated here, its right edge where it is: a transition of its size
// put it at its new left edge at once, away from the pointer, which then
// left it, and it went back and forth.
func (a *App) tspJump(c *ui.Context, k *colors, p *Pane) {
	dock := p.dockOver()
	below := p.term.LinesBelow()
	if dock <= 0 || below <= 0 {
		return
	}
	const round = 28
	pill := ui.Row(c.Key("tsp-jump")).Absolute().Right(p.term.ScrollbarStrip()+6).Bottom(dock+tspDockPadBottom+8).
		Height(round).AlignItems(ui.Center).ClipX().
		Radius(round/2).Background(k.panel).Border(1, k.panelBorder).Cursor(ui.CursorPointer).
		Font(a.tspLook().font).
		Transition(ui.ElementTransition{Duration: 150 * time.Millisecond, Colors: true, Enter: &ui.Motion{}, Exit: &ui.Motion{}})
	hovered := pill.Hovered()
	shown := float32(0)
	if hovered {
		shown = 1
	}
	text := fmt.Sprintf("%d lines below", below)
	if below == 1 {
		text = "1 line below"
	}
	// The arrow and the count, at their own width: the pill as wide as
	// the arrow shows the arrow alone, and widening shows the count.
	full := float32(round)
	pill.Children(func() {
		inner := ui.Row(c.Key("inner")).Shrink(0).Padding(0, 10).Gap(6).AlignItems(ui.Center)
		full = max(inner.Bounds().W, round)
		inner.Children(func() {
			ui.Text(c, "↓").FontSize(a.tspText(13)).TextColor(k.textMuted)
			ui.Text(c.Key("label"), text).FontSize(a.tspText(12)).TextColor(k.textMuted).SingleLine().
				Opacity(pill.Animate("label", shown, 150*time.Millisecond))
		})
	})
	target := float32(round)
	if hovered {
		target = full
	}
	pill.Width(pill.Animate("width", target, 150*time.Millisecond))
	term, view := p.term, p.hostBounds
	pill.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		b := pill.Bounds()
		ev.X, ev.Y = ev.X+b.X-view.X, ev.Y+b.Y-view.Y
		return term.Scroll(ev)
	})
	if pill.Clicked() {
		term.ScrollToBottom()
		a.services.Invalidate()
	}
}

// screenSurface draws a screen surface over a pane's terminal, filling
// the box it is in.
func (a *App) screenSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface) {
	a.tspOverlays = a.tspOverlays[:0]
	s := ui.Scroll(c.Key("tsp-"+sf.ID)).Absolute().Fill().Background(k.bgBottom).Padding(12, 16).Font(a.tspLook().font)
	s.Children(func() { a.surfaceContent(c, k, t, p, sf, false) })
	if s.Pressed() {
		a.surfacePressed(t, p)
	}
	a.surfaceOverlays(c, k, t, p, sf)
}

// inlineSurface draws an inline surface from its anchor's row, over the
// terminal's width. Live, it is as tall as its content, and its height
// in rows, as the last frame laid it out, goes to the server when it
// differs from the rows the surface has; kept, it is cut to those rows.
// Live with a dock, it reaches the pane's bottom, where the dock is
// pinned, over the rows the server pads it with (tspTracker.padding):
// no terminal shows between them; its content is laid out up from its
// end, above the dock, when it needs more rows than it has. A surface
// takes no more rows than the scrollback keeps (rex.Surface.Most):
// content taller shows its end, its head cut, as a terminal's oldest
// lines go first; so does a surface whose anchor the scrollback dropped
// (headless), placed up from its end marker. Scrolled out of the
// viewport it draws nothing.
func (a *App) inlineSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, pl terminal.AppPlacement, headless bool) {
	_, screenRows := p.term.Size()
	docked := !sf.Closed && tspDocked(sf)
	// A live surface with a dock reaches the pane's bottom whatever rows
	// it has: it shows while its anchor is above the view's end.
	rows := max(sf.Rows+sf.Dock, p.surfaceRows[sf.ID].rows+p.surfaceRows[sf.ID].dock, 1) + sf.Spare
	if pl.Row >= screenRows || !docked && pl.Row+rows <= 0 {
		// Scrolled above a live surface's start, its dock stays pinned
		// at the pane's bottom, as Tern's composer does: the user types,
		// and the focus in its field stays, whatever part of the
		// scrollback is in view.
		if docked {
			a.surfaceDock(c, k, t, p, sf, tspDockRegion(sf), pl.Rect.X)
		}
		return
	}
	cell := pl.Rect.H / float32(max(pl.Rows, 1))
	// The most rows the content takes, and whether the last frame
	// measured it taller.
	most := 0
	if sf.Most > 0 {
		most = max(sf.Most-sf.Dock, 1)
		if sf.Closed {
			most = sf.Most
		}
	}
	// A surface settling after a change of the pane's width is laid out
	// from its end, as one cut, while what is far above the view keeps
	// the heights measured before (tsp_cull.go).
	settle := a.settling(p, sf)
	if !docked {
		settle.synced = true
	}
	cut := headless || most > 0 && p.tspNeed[sf.Image] > most || docked && settle.on
	// A live surface with a dock ends above its dock, as Tern's transcript
	// ends above its composer: it is laid out up from its end. With the
	// view at the terminal's end, that is the pane's bottom, whatever rows
	// the server has given it yet: a line of output it has no row for
	// (TSPSize) shows above the dock at once, the content's head cut at
	// the anchor, and the rows the server then adds (padding, which
	// scrolls the terminal, or fewer rows spare) show more of the head,
	// the end staying where it is. Placed by those rows instead, it moved
	// as they came, and again as the terminal scrolled, which the window
	// gets apart from them. Scrolled back (held), the node at the top of
	// the view keeps its place, and moves only as the terminal scrolls:
	// what changes above it, as a section omp folds when it is done,
	// grows or shrinks upward, out of sight, and output that comes goes
	// under it (tspHolding).
	// Content that needs fewer rows than the screen has under its anchor
	// starts at the anchor.
	fromEnd := cut || docked
	atEnd := docked && p.term.AtBottom()
	top, held := p.tspTop[sf.Image]
	held = held && docked && !atEnd
	box := ui.Box(c.Key(fmt.Sprintf("tsp-%d", sf.Image))).Absolute().Left(pl.Rect.X).Top(pl.Rect.Y).Right(p.term.ScrollbarStrip()).Background(k.bgBottom).Font(a.tspLook().font)
	// A live surface with a dock reaches the screen's last row, under its
	// rows, where the cursor stays (rex.tspTracker.padding).
	below := float32(0)
	if docked {
		below = cell
	}
	// The least height of the content laid out from the end: what starts
	// it at the anchor when it fits, the anchor on the screen.
	fill := float32(0)
	// The box's height, but for one that reaches the pane's bottom.
	boxH := float32(-1)
	switch {
	case sf.Closed:
		boxH = cell * float32(sf.Rows)
		box.Height(boxH)
	case atEnd:
		box.Bottom(0)
		if !cut && pl.Row >= 0 {
			fill = max(cell*float32(screenRows-pl.Row)-8-below, 0)
		}
	case held:
		box.Bottom(0)
	case cut:
		// Its rows, and its dock's, which reach the pane's bottom
		// when it is scrolled to the end: a surface cut takes the
		// screen.
		boxH = cell*float32(sf.Rows+sf.Dock+sf.Spare) + below
		box.Height(boxH)
	case docked:
		// Scrolled back: its rows, and its dock's, or as many as the
		// screen has under its anchor, which the server pads it with
		// while its content needs fewer (rex.Surface.under).
		span := max(sf.Rows+sf.Dock+sf.Spare, screenRows-1-pl.Row)
		boxH = cell*float32(span) + below
		box.Height(boxH)
		if pl.Row >= 0 {
			fill = max(cell*float32(span-sf.Spare)-8, 0)
		}
	}
	if fromEnd || sf.Closed {
		box.Clip()
	}
	if !fromEnd {
		// Under the rows the surface has spare above its content, with
		// its anchor in the scrollback (rex.Surface.Spare).
		box.Padding(4+cell*float32(sf.Spare), 8, 4, 8)
	}
	// The wheel over the surface scrolls the terminal, as over its rows.
	term, at := p.term, pl.Rect
	box.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		ev.X, ev.Y = ev.X+at.X, ev.Y+at.Y
		return term.Scroll(ev)
	})
	a.tspOverlays = a.tspOverlays[:0]
	a.tspDock = nil
	var content ui.Element
	box.Children(func() {
		if !fromEnd {
			// The content hangs from the box's top, where its nodes
			// are measured from.
			a.startCulling(p, sf, settle, box.Bounds().Y, func(view ui.Rect) float32 { return view.Y + pl.Rect.Y })
			content = a.surfaceContent(c, k, t, p, sf, !sf.Closed)
			return
		}
		// Laid out from the box's bottom, the content's head goes out
		// of the top, where the box cuts it; at least as tall as fill,
		// content that fits starts at the top. Held, it is placed by
		// the node at the top of the view, or without one in view, at
		// the top it had in the last frame.
		end := ui.Box(c.Key("end")).Absolute().Left(8).Right(8)
		if !held {
			end.Bottom(4 + below)
		}
		if fill > 0 {
			end.MinHeight(fill)
		}
		// Its nodes are measured from its top, which is where its
		// bottom is less its height: the box's top moves with the
		// anchor and the rows the server gives, which leave the
		// content's end where it is, so offsets from the box's top go
		// stale, and the nodes culled by them stayed culled, the view
		// blank, as when a resumed session took its rows at once.
		last := end.Bounds()
		if b := box.Bounds(); docked && last.H > 0 && b.H > 0 {
			top = last.Y - b.Y
			if p.tspTop == nil {
				p.tspTop = map[uint32]float32{}
			}
			p.tspTop[sf.Image] = top
			if held {
				a.tspHeld = tspHolding{on: true, line: max(p.hostBounds.Y, b.Y), box: b.Y}
			}
		}
		a.startCulling(p, sf, settle, last.Y, func(view ui.Rect) float32 {
			if held {
				return view.Y + pl.Rect.Y + top
			}
			bottom := view.Y + view.H
			if boxH >= 0 {
				bottom = view.Y + pl.Rect.Y + boxH
			}
			return bottom - 4 - below - last.H
		})
		end.Children(func() { content = a.surfaceContent(c, k, t, p, sf, !sf.Closed) })
		if held {
			end.Top(top)
			if a.tspHeld.found {
				end.HoldTop(a.tspHeld.at)
			}
		}
		a.tspHeld = tspHolding{}
	})
	a.endCulling(p, settle)
	if box.Pressed() {
		// A press in the transcript, between its blocks, leaves the
		// composer: the keys go to the terminal, and the page keys
		// scroll (view.go).
		a.surfacePressed(t, p)
		p.termHeld = true
	}
	if dock := a.tspDock; dock != nil {
		a.surfaceDock(c, k, t, p, sf, dock, pl.Rect.X)
	}
	a.surfaceOverlays(c, k, t, p, sf)
	if cell <= 0 {
		return
	}
	// The rows the content takes, the box's padding included, and those
	// of the dock's space under it, which the server pads while the
	// surface is live and leaves when it is kept. A kept surface's box
	// is cut to its rows: its content measured taller is told once, for
	// the rows it lacks (rex.tspTracker.resize), from its second kept
	// frame on, the bounds being the last frame's, the live layout's
	// with the dock's space on the first.
	if sf.Closed {
		if p.keptFrames == nil {
			p.keptFrames = map[uint32]int{}
		}
		p.keptFrames[sf.Image]++
		if p.keptFrames[sf.Image] < 2 {
			return
		}
	}
	dockH := float32(0)
	if docked {
		dockH = p.dockH[sf.Image]
	}
	// Measured once laid out, with its dock: a frame that built the
	// surface first has no bounds yet, which would tell it a row.
	if ch := content.Bounds().H; ch == 0 || docked && dockH == 0 {
		return
	}
	if docked && tspLayoutLog != nil {
		a.logSurfaceLayout(p, sf, pl, screenRows, cut, dockH, content.Bounds().H, box.Bounds())
	}
	if h := content.Bounds().H + 8 - dockH; h > 0 {
		size := tspSize{int(math.Ceil(float64(h/cell - 0.01))), int(math.Ceil(float64(dockH/cell - 0.01)))}
		if p.tspNeed == nil {
			p.tspNeed = map[uint32]int{}
		}
		p.tspNeed[sf.Image] = size.rows
		if most > 0 {
			size.rows = min(size.rows, most)
		}
		if docked {
			settle.synced = size.rows == sf.Rows && size.dock == sf.Dock
		}
		if sf.Closed && size.rows <= sf.Rows {
			return
		}
		if size.rows > 0 && (size.rows != sf.Rows || size.dock != sf.Dock) && size != p.surfaceRows[sf.ID] {
			if p.surfaceRows == nil {
				p.surfaceRows = map[string]tspSize{}
			}
			p.surfaceRows[sf.ID] = size
			stream, id := p.stream, sf.ID
			go stream.TSPSize(id, size.rows, size.dock)
			if docked && tspLayoutLog != nil {
				tspLayoutLog.Printf("f=%d %s sent TSPSize rows=%d dock=%d (had rows=%d dock=%d)", a.drawn, sf.ID, size.rows, size.dock, sf.Rows, sf.Dock)
			}
		}
	}
}

// tspHolding finds, as a held surface's content is built, the node it is
// placed by: the node at the top of the view in the last frame's layout,
// the first one drawn whose box then reached below line (the view's top,
// or the box's where the surface starts lower), inner nodes before the
// node around them, as each is drawn after what is inside it. The content
// goes where that node is at below the box's top (ui.Element.HoldTop),
// as it was in the last frame: as the terminal scrolls, the box and the
// node move with it, and what changes above the node moves what is above.
type tspHolding struct {
	on, found bool
	line, box float32
	at        float32
}

// tspHold takes el, the element of a node just drawn, as the node the
// held content is placed by, when none is yet and its box in the last
// frame reached below the line.
func (a *App) tspHold(el ui.Element) {
	h := &a.tspHeld
	if !h.on || h.found || a.tspPinned {
		return
	}
	if b := el.Bounds(); b.H > 0 && b.Y+b.H > h.line {
		h.found, h.at = true, b.Y-h.box
		el.HoldHere()
	}
}

// tspSize is what a window measures a live inline surface to need: the
// rows of its content, and of the dock pinned under it.
type tspSize struct{ rows, dock int }

// tspDocked reports whether a surface has a dock region, as the server
// pads it for (rex.Surface.docked).
func tspDocked(sf *rex.Surface) bool { return tspDockRegion(sf) != nil }

// tspDockRegion is a surface's dock region, nil for none.
func tspDockRegion(sf *rex.Surface) *rex.TSPNode {
	if sf.Root == nil {
		return nil
	}
	for _, n := range sf.Root.Children {
		if n.ID == "dock" {
			return n
		}
	}
	return nil
}

// surfaceDock draws a live surface's dock region (a program's composer
// and status bar) pinned to the bottom of the pane, where the user
// types whatever part of the output they look at; the surface keeps a
// space as tall in its place, so that at the bottom the dock is where
// it would be. As Tern draws it, it floats over the output: centered,
// no wider than the OMP settings' composer width, with no band of its
// own behind it.
func (a *App) surfaceDock(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, dock *rex.TSPNode, x float32) {
	// The pinned strip passes the pointer through beside the dock, to the
	// output under it.
	pinned := ui.Column(c.Key(fmt.Sprintf("tsp-dock-%d", sf.Image))).Absolute().Left(x).Right(p.term.ScrollbarStrip()).Bottom(0).
		Padding(tspDockPadTop, 16, tspDockPadBottom, 16).AlignItems(ui.Center).Font(a.tspLook().font).PassThrough()
	term := p.term
	var inner ui.Element
	pinned.Children(func() {
		inner = ui.Column(c).Gap(a.tspGap(8)).FillWidth().MaxWidth(a.tspLook().dockW).Children(func() {
			a.tspPinned = true
			for _, ch := range dock.Children {
				a.tspNode(c, k, t, p, sf, ch)
			}
			a.tspPinned = false
		})
	})
	inner.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		return term.Scroll(ev)
	})
	if inner.Pressed() {
		a.surfacePressed(t, p)
	}
	if h := pinned.Bounds().H - tspDockPadTop - tspDockPadBottom; h > 0 && h != p.dockH[sf.Image] {
		if p.dockH == nil {
			p.dockH = map[uint32]float32{}
		}
		p.dockH[sf.Image] = h
	}
}

// The room around the dock, in DIPs.
const (
	tspDockPadTop    = 6
	tspDockPadBottom = 10
)

// tspComposer draws a program's composer in the dock (omp's role
// omp.editor) as Tern does: a card around the input and the bar under
// it, its context meter a hairline along the card's top edge, filled to
// what is used, with the share used over the fill's end and the window's
// size over the line's (tspEdgeLabels, tspEdgeLine). What the dock has above the
// composer (omp's activity row) floats over the output, outside the card.
func (a *App) tspComposer(c *ui.Context, key *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) ui.Element {
	var meter *rex.TSPNode
	var rest []*rex.TSPNode
	for _, ch := range n.Children {
		if hidden, _ := ch.Props["hidden"].(bool); ch.Kind == "meter" && meter == nil && !hidden {
			meter = ch
		} else {
			rest = append(rest, ch)
		}
	}
	col := ui.Column(key).FillWidth()
	// Its fields' popups open above it (tspOverlayAt).
	p.tspCard = col.Bounds()
	col.Children(func() {
		var labels ui.Element
		if meter != nil {
			labels = a.tspEdgeLabels(c, k, meter)
		}
		// Raised as Tern's: a touch toward the text's color from the
		// output's, opaque, so that the output under the card does not
		// show through, with a border a touch further (lighter in a dark
		// theme, darker in a light one).
		raised := k.bgBottom.Mix(k.text, 0.06)
		ui.Column(c).FillWidth().Radius(tspComposerR).Background(raised).Shadow(0, 10, 28, -8, ui.RGBA(0, 0, 0, 0.22)).Children(func() {
			ui.Column(c).Gap(10).FillWidth().Radius(tspComposerR).Border(1, k.bgBottom.Mix(k.text, 0.1)).Padding(16, 16, 14, 16).Children(func() {
				a.tspFieldBare = true
				for _, ch := range rest {
					a.tspNode(c, k, t, p, sf, ch)
				}
				a.tspFieldBare = false
			})
		})
		if meter != nil {
			// Over the card's top border, inside its corners; omp's tone
			// pending says a turn runs.
			tone, _ := n.Props["tone"].(string)
			line := a.tspEdgeLine(c, k, meter, tone == "pending").Absolute().Top(tspEdgeLabelsH).Left(tspComposerR).Right(tspComposerR)
			a.tspClick(t, p, sf, meter, labels)
			a.tspClick(t, p, sf, meter, line)
			if title, _ := meter.Props["title"].(string); title != "" {
				labels.Tooltip(title)
				line.Tooltip(title)
			}
		}
	})
	return col
}

// tspEdgeLabelsH is the height of the labels over a composer's edge meter,
// and tspComposerR the radius of the composer's card, whose corners the
// meter keeps inside.
const (
	tspEdgeLabelsH = 15
	tspComposerR   = 22
)

// tspEdgeLabels draws the labels of a composer's edge meter, over the
// card: the share used ending where the fill ends, and the total at the
// right end.
func (a *App) tspEdgeLabels(c *ui.Context, k *colors, n *rex.TSPNode) ui.Element {
	value, _ := n.Props["value"].(float64)
	value = max(0, min(1, value))
	label, _ := n.Props["label"].(string)
	total, _ := n.Props["total"].(string)
	row := ui.Row(c.Key("edge-labels")).FillWidth().Height(tspEdgeLabelsH).Padding(0, tspComposerR).AlignItems(ui.End)
	row.Children(func() {
		ui.Row(c).Grow(float32(value)).Basis(0).MinWidth(0).Justify(ui.End).Children(func() {
			if label != "" {
				ui.Text(c, label).FontSize(a.tspText(10.5)).TextColor(k.textFaint).SingleLine()
			}
		})
		ui.Row(c).Grow(float32(1 - value)).Basis(0).MinWidth(0).Justify(ui.End).Children(func() {
			if total != "" {
				ui.Text(c, total).FontSize(a.tspText(10.5)).TextColor(k.textFaint).SingleLine()
			}
		})
	})
	return row
}

// tspEdgeSweep is how long the glow of a running turn takes to cross a
// composer's edge meter.
const tspEdgeSweep = 1600 * time.Millisecond

// tspEdgeLine draws a composer's edge meter: a hairline, filled to the
// share used, with its marks (omp's auto-compact threshold). While a turn
// runs, a glow sweeps the fill from left to right, as in Tern.
func (a *App) tspEdgeLine(c *ui.Context, k *colors, n *rex.TSPNode, running bool) ui.Element {
	value, has := n.Props["value"].(float64)
	value = max(0, min(1, value))
	marks, _ := n.Props["marks"].([]any)
	fill := tspMeterFill(k, n)
	line := ui.Box(c.Key("edge-line")).Height(2)
	var sweep float32 = -1
	if running && has && value > 0 && a.tspMotion() {
		sweep = line.Loop("sweep", tspEdgeSweep, ui.EaseInOut)
	}
	line.Draw(func(pt *ui.Painter, r ui.Rect) {
		pt.Fill(r, k.track, 1)
		if !has {
			return
		}
		used := ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(value), H: r.H}
		pt.Fill(used, fill, 1)
		if sweep >= 0 {
			// A white glow, a quarter of the line wide, from before the
			// fill's start to past its end, seen within the fill.
			w := max(48, r.W/4)
			x := used.X - w + sweep*(used.W+w)
			pt.Clip(used, 1, func() {
				pt.FillGradient(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
					{Color: ui.RGBA(255, 255, 255, 0), At: 0},
					{Color: ui.RGBA(255, 255, 255, 0.95), At: 0.8},
					{Color: ui.RGBA(255, 255, 255, 0), At: 1},
				}}, 1)
			})
		}
		for _, m := range marks {
			m, _ := m.(map[string]any)
			at, _ := m["at"].(float64)
			pt.Fill(ui.Rect{X: r.X + r.W*float32(max(0, min(1, at))), Y: r.Y, W: 1, H: r.H}, k.textMuted, 0)
		}
	})
	return line
}

// tspSubmit draws omp's send and stop controls in the composer
// (omp.composer.send, a keycap of Enter, and omp.composer.stop, the
// word) as Tern does: a round button, a return arrow on it, or a square
// while a turn runs. Send is faint while there is nothing to send (idle:
// the composer empty), as is a muted one (a draft held by dictation).
func (a *App) tspSubmit(c *ui.Context, key *ui.Context, k *colors, n *rex.TSPNode, stop, idle bool) ui.Element {
	bg, ink := k.text, k.bgBottom
	if tone, _ := n.Props["tone"].(string); tone == "muted" {
		bg = bg.Alpha(0.3)
	}
	if idle && !stop {
		bg, ink = k.text.Alpha(0.14), k.textMuted
	}
	b := ui.Box(key).Size(34, 34).Radius(17).Center().Background(bg).Shrink(0).Cursor(ui.CursorPointer).Role(ui.RoleButton)
	if b.Hovered() {
		b.Background(bg.Alpha(0.8))
	}
	b.Children(func() {
		if stop {
			ui.Box(c).Size(11, 11).Radius(2.5).Background(ink)
		} else {
			ui.Icon(c, icon("corner-down-left")).Size(18, 18).TextColor(ink)
		}
	})
	return b
}

// tspSent brings the terminal to its end when el, omp's send or stop
// control, is clicked with something in the composer, which sends or
// queues it: the message shows at the transcript's end, above the
// composer, as what is typed in a terminal shows at its end. Scrolled
// back, the transcript keeps what is read in place, and the message went
// under the composer. A stop with the composer empty leaves the view.
func (a *App) tspSent(p *Pane, el ui.Element) {
	if el.Clicked() && !p.composerEmpty {
		p.term.ScrollToBottom()
	}
}

// tspStarburst are the frames of omp's working glyph (spinner style
// starburst); text stars only, as ✳ and ✴ draw as emoji.
var tspStarburst = []string{"✢", "✣", "✶", "✻", "✽", "✻", "✶", "✣"}

// tspComposerBar draws the composer's bar (omp.composer.bar) with, as
// Tern has, the pane's directory after the model, effort and rate: omp
// leaves the path to the terminal.
func (a *App) tspComposerBar(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	dir := shortDir(p.info.Dir)
	if plainHost != "" {
		// The folder of the ssh on this machine, not of the host's shell.
		dir = ""
	}
	for _, ch := range n.Children {
		role, _ := ch.Props["role"].(string)
		if dir != "" && role != "omp.composer.model" && role != "omp.composer.effort" && role != "omp.composer.rate" {
			ui.Row(c.Key("cwd")).Gap(5).AlignItems(ui.Center).Shrink(0).Tooltip(p.info.Dir).Children(func() {
				ui.Icon(c, icon("folder")).Size(13, 13).TextColor(k.textMuted)
				ui.Text(c, dir).Font(a.tspLook().mono).FontFeatures(a.tspLook().monoFeatures...).FontSize(a.tspCode(12)).TextColor(k.textMuted).SingleLine()
			})
			dir = ""
		}
		a.tspNode(c, k, t, p, sf, ch)
	}
}

// tspChecklist draws a checklist whole: its phases, each item with its
// status and note.
func (a *App) tspChecklist(c *ui.Context, key *ui.Context, k *colors, n *rex.TSPNode, phases []any) ui.Element {
	cl := ui.Column(key).Gap(3).FillWidth()
	cl.Children(func() {
		for pi, ph := range phases {
			ph, _ := ph.(map[string]any)
			if title := ph["title"]; title != nil {
				a.tspRich(c.Key(pi), k, title, 12, true)
			}
			items, _ := ph["items"].([]any)
			for i, it := range items {
				it, _ := it.(map[string]any)
				status, _ := it["status"].(string)
				row := ui.Row(c.Key(fmt.Sprintf("%d.%d", pi, i))).Gap(6).AlignItems(ui.Baseline).FillWidth()
				row.Children(func() {
					glyph, color := "☐", k.textMuted
					switch status {
					case "active":
						glyph, color = "◉", k.busy
					case "done":
						glyph, color = "☑", k.busy
					case "dropped":
						glyph = "⊘"
					case "blocked":
						glyph, color = "⊗", k.attention
					}
					ui.Text(c, glyph).FontSize(a.tspText(13)).TextColor(color)
					t := a.tspRich(c, k, it["text"], 13, false)
					if status == "done" || status == "dropped" {
						t.TextColor(k.textMuted)
					}
				})
				if note := it["note"]; note != nil {
					a.tspRich(c, k, note, 12, false).TextColor(k.textMuted).Padding(0, 0, 0, 20)
				}
			}
		}
		if note := n.Props["note"]; note != nil {
			a.tspRich(c, k, note, 12, false).TextColor(k.textMuted)
		}
	})
	return cl
}

// tspChecklistPill draws a checklist HUD (mode hud) as Tern does: a pill
// of a ring filled to the share done, the item at work (or the next) and
// the count, whose click shows the whole plan in a popover.
func (a *App) tspChecklistPill(c *ui.Context, key *ui.Context, k *colors, p *Pane, sf *rex.Surface, n *rex.TSPNode, phases []any) ui.Element {
	done, total := 0, 0
	var current, next any
	for _, ph := range phases {
		ph, _ := ph.(map[string]any)
		items, _ := ph["items"].([]any)
		for _, it := range items {
			it, _ := it.(map[string]any)
			total++
			switch status, _ := it["status"].(string); status {
			case "done", "dropped":
				done++
			case "active":
				if current == nil {
					current = it["text"]
				}
			default:
				if next == nil {
					next = it["text"]
				}
			}
		}
	}
	if current == nil {
		current = next
	}
	if p.hudOpen == nil {
		p.hudOpen = map[string]*bool{}
	}
	openKey := strconvImage(sf.Image) + ":" + n.ID
	open := p.hudOpen[openKey]
	if open == nil {
		open = new(bool)
		p.hudOpen[openKey] = open
	}
	pill := ui.Row(key).Gap(7).AlignItems(ui.Center).Padding(3, 8).Radius(12).MinWidth(0).MaxWidth(360).Cursor(ui.CursorPointer)
	if pill.Hovered() || *open {
		pill.Background(k.hover)
	}
	share := float32(0)
	if total > 0 {
		share = float32(done) / float32(total)
	}
	pill.Children(func() {
		ui.Box(c).Size(14, 14).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
			pt.FillConicGradient(r, ui.ConicGradient{Stops: []ui.GradientStop{
				{Color: k.busy, At: 0}, {Color: k.busy, At: share}, {Color: k.track, At: share}, {Color: k.track, At: 1},
			}}, r.W/2)
			hole := ui.Rect{X: r.X + 2.5, Y: r.Y + 2.5, W: r.W - 5, H: r.H - 5}
			pt.Fill(hole, k.bgBottom, hole.W/2)
		})
		if current != nil {
			a.tspRich(c, k, current, 12.5, false).TextColor(k.text).SingleLine().MinWidth(0)
		}
		ui.Text(c, fmt.Sprintf("%d/%d", done, total)).FontSize(a.tspText(12)).TextColor(k.textMuted).SingleLine().Shrink(0)
	})
	if pill.Clicked() {
		*open = !*open
	}
	ui.Popover(c, pill, open, func() {
		ui.Scroll(c).MaxHeight(420).Width(420).Padding(10, 12).Children(func() {
			a.tspChecklist(c, c.Key("plan"), k, n, phases)
		})
	})
	return pill
}

// tspBlank reports whether n is a container that draws nothing: a col or
// row whose children are all hidden or blank themselves, as the empty
// placeholders a program keeps (omp's dock has one per HUD). It takes no
// room, nor the gap beside it.
func tspBlank(n *rex.TSPNode) bool {
	if n.Kind != "col" && n.Kind != "row" {
		return false
	}
	if _, ok := n.Props["actions"]; ok {
		return false
	}
	for _, ch := range n.Children {
		if hidden, _ := ch.Props["hidden"].(bool); !hidden && !tspBlank(ch) {
			return false
		}
	}
	return true
}

// surfaceContent draws a surface's document. Its paragraphs are
// selectable, each on its own (Markdown within itself); a press on one
// takes the focus, as selecting text does anywhere, and a press
// anywhere else on the surface, or on a control of the program's, gives
// it back to the terminal (surfacePressed), as Escape does, which the
// program gets too: the key most programs end a surface with. With
// pin, the dock region is left as a space for surfaceDock to draw. With
// the focus in an inline surface's text, Page Up and Page Down, Home and
// End scroll the terminal it is drawn among, as over the terminal.
func (a *App) surfaceContent(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, pin bool) ui.Element {
	content := ui.Column(c).FillWidth().Gap(a.tspGap(8))
	if w := a.tspLook().msgW; w > 0 {
		// The transcript no wider than the setting, centered in the
		// pane as the dock is.
		content.MaxWidth(w).AlignSelf(ui.Center)
	}
	content.Children(func() {
		if sf.Root == nil {
			return
		}
		for _, region := range sf.Root.Children {
			if region.ID == "dock" {
				if sf.Closed {
					// A program's composer and controls go with it.
					continue
				}
				if pin {
					a.tspDock = region
					ui.Box(c.Key("dock-space")).Height(p.dockH[sf.Image])
					continue
				}
			}
			a.tspNode(c, k, t, p, sf, region)
		}
	})
	if content.Shortcut(0, ui.KeyEscape) {
		a.surfacePressed(t, p)
		if !sf.Closed {
			p.term.Send([]byte{0x1b})
		}
	}
	if sf.Image != 0 {
		for key, by := range tspPageKeys {
			if content.Shortcut(0, key) {
				a.scrollTerm(p, by)
			}
		}
	}
	return content
}

// tspPageKeys are the keys that scroll the terminal with the focus in an
// inline surface's text, and how.
var tspPageKeys = map[ui.Key]string{ui.KeyPageUp: "page-up", ui.KeyPageDown: "page-down", ui.KeyHome: "start", ui.KeyEnd: "end"}

// surfacePressed takes a press on a surface's edge, or on a control of
// the program's: the pane and its terminal get the focus, so that keys
// go to the program (to its composer, with a live dock: view.go).
func (a *App) surfacePressed(t *Tab, p *Pane) {
	if t.Focus != p {
		t.setFocus(p)
	}
	a.focusReq = p
	p.termHeld = false
	a.changed()
}

// tspNode draws a node and its children, and returns its element: none
// for a node drawn elsewhere (an overlay) or not at all.
func (a *App) tspNode(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) (el ui.Element) {
	if hidden, _ := n.Props["hidden"].(bool); hidden || tspBlank(n) {
		return
	}
	role, _ := n.Props["role"].(string)
	if a.tspPinned && role == "omp.working.stop" {
		// Tern leaves the working row's Esc Stop out: the composer's
		// round button stops the turn.
		return
	}
	key := c.Key("n-" + n.ID)
	text, _ := n.Props["text"].(string)
	children := func() { a.tspChildren(c, k, t, p, sf, n) }
	// A node with a click action is a control: its text is not
	// selectable, so that a click on it reaches the node.
	if actions, ok := n.Props["actions"].(map[string]any); ok && actions["click"] != nil && !a.tspHead {
		a.tspHead = true
		defer func() { a.tspHead = false }()
	}
	switch n.Kind {
	case "col":
		if a.tspPinned && strings.HasPrefix(role, "omp.editor") {
			el = a.tspComposer(c, key, k, t, p, sf, n)
			break
		}
		sheet := a.tspSheet
		if sheet != nil && sheet.panes[n.ID] {
			// A pane of an overlay's sheet: it scrolls on its own, as
			// tall as the row of panes (tspPaneSheet). The jobs sheet's
			// list is a third of it, apart from the detail by a line, as
			// Tern draws it.
			pane := ui.Scroll(key).TrackScroll(p.overlayScroll(n.ID))
			a.scrolled(p, n.ID, pane)
			// Clear of its scroll bar, which draws over its right edge.
			pt, pr, pb, pl := float32(0), float32(12), float32(0), float32(0)
			switch {
			case role == "omp.jobs.master":
				pane.WidthPercent(30).MinWidth(200).MaxWidth(300).Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(k.panelBorder)
				pt, pr, pb, pl = 10, 10, 10, 8
			case role == "omp.jobs.detail":
				pane.Grow(1).MinWidth(0)
				pt, pr, pb, pl = 18, 24, 18, 24
			case !tspFlexed(n):
				pane.Grow(1)
			}
			el = pane.Children(func() {
				ui.Column(c).Gap(a.tspGap(8)).FillWidth().Padding(pt, pr, pb, pl).Children(children)
			})
			break
		}
		col := ui.Column(key).Gap(a.tspGap(8))
		switch {
		case a.tspPinned && role == "omp.hud.status":
			// The working rows, beside the todo HUD on their line.
			col.Grow(1).MinWidth(0)
		case role == "omp.jobs.master":
			// The jobs sheet's list, beside the selected job's detail:
			// about a third of the sheet.
			col.WidthPercent(30).MinWidth(200).MaxWidth(300).Shrink(0)
		case role == "omp.jobs.detail":
			// The rest, its long lines wrapped or scrolled in it; under
			// the list where there is no room beside it.
			col.Grow(1).MinWidth(360)
		case role == "omp.job.widget.side":
			// A job widget's figure beside its chart, which takes the
			// rest of the row.
			col.Shrink(0)
		case role == "omp.jobs.widget":
			// A job's widget in the jobs sheet: a card of its own.
			col.FillWidth().Padding(10, 14).Radius(8).Border(1, k.panelBorder).Background(k.text.Alpha(0.03))
		case tspFlexed(n):
			// Sized by its row (tspFlex), as a pane beside another.
		default:
			col.FillWidth()
		}
		if sheet != nil && sheet.path[n.ID] {
			// Between a sheet and its row of panes: as tall as the sheet
			// leaves it.
			col.Grow(1).MinHeight(0)
		}
		el = col.Children(children)
	case "row":
		if role == "omp.btn" {
			// omp's button, drawn as a row of its label and key: MyGo's
			// button, the key in its tooltip, as Tern's.
			el = a.tspButton(key, k, n)
			break
		}
		if role == "omp.recap" {
			var ok bool
			if el, ok = a.tspRecap(c, key, k, n); ok {
				break
			}
		}
		row := ui.Row(key).Gap(8).AlignItems(tspRowAlign(n))
		if s := a.tspSheet; s != nil && s.row == n.ID {
			// A sheet's row of panes fills it, each pane as tall.
			row.Grow(1).MinHeight(0).AlignItems(ui.Stretch)
		}
		switch {
		case !a.tspPinned:
			row.FillWidth()
		case role == "omp.composer.model":
			// Tern's model chip: an outlined pill.
			row.Gap(6).Padding(5, 12, 5, 10).Radius(16).Border(1, k.bgBottom.Mix(k.text, 0.22)).Shrink(0).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(k.hover)
			}
		case role == "omp.composer.mode":
			// The shell-mode chip before the input.
			row.Gap(4).Padding(1, 8).Radius(10).Background(k.hover).Shrink(0)
		case role == "omp.hud.pill":
			// omp's running-work counter, as Tern's: an outlined pill
			// at the right end, as wide as its segments.
			row.Gap(2).Padding(2).Radius(16).Border(1, k.bgBottom.Mix(k.text, 0.22)).Shrink(0)
		case role == "omp.hud.seg":
			// A segment of it: a button of an icon, a count, and a
			// job's glance and spark.
			row.Gap(5).Padding(4, 8).Radius(14).Shrink(0).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(k.hover)
			}
		case role == "omp.actions" && a.tspSheet != nil:
			// A sheet's actions, under a line across it.
			row.FillWidth().Padding(10, 18).BorderWidth(1, 0, 0, 0).BorderColor(k.panelBorder)
		case role == "omp.jobs.split":
			// The jobs sheet: its list and the selected job side by
			// side; outside a sheet of panes, from the top, or stacked
			// in a narrow pane.
			row.FillWidth()
			if s := a.tspSheet; s != nil && s.row == n.ID {
				row.Gap(0)
			} else {
				row.AlignItems(ui.Start).Gap(14).Wrap()
			}
		case strings.HasPrefix(role, "omp.composer.") && role != "omp.composer.bar" && role != "omp.composer.line":
			// The composer's other controls take their content's width.
			row.Shrink(0)
		default:
			row.FillWidth()
		}
		if wrap, _ := n.Props["wrap"].(bool); wrap {
			row.Wrap()
		}
		switch justify, _ := n.Props["justify"].(string); justify {
		case "end":
			row.Justify(ui.End)
		case "between":
			row.Justify(ui.SpaceBetween)
		case "center":
			row.Justify(ui.Center)
		}
		if a.tspPinned && role == "omp.hud.activity" {
			// The todo pill keeps the right end, with the working row
			// beside it during a turn or without it after.
			row.Justify(ui.End)
		}
		if a.tspPinned && role == "omp.composer.bar" {
			el = row.Children(func() { a.tspComposerBar(c, k, t, p, sf, n) })
			break
		}
		el = row.Children(children)
	case "card":
		status, _ := n.Props["status"].(string)
		variant, _ := n.Props["variant"].(string)
		head, body, collapsed := a.tspFold(p, sf, n)
		card := ui.Column(key).Gap(a.tspGap(10)).FillWidth()
		if variant != "bare" && a.tspFlat == 0 {
			card.Radius(cardR).Background(k.card).Border(1, k.cardBorder).Padding(a.tspGap(12), a.tspGap(14))
			if tone, _ := n.Props["tone"].(string); tone != "" {
				if bg, ok := a.toneBg(k, tone); ok {
					card.Background(bg)
				}
			}
			if selected, _ := n.Props["selected"].(bool); selected {
				card.Border(1, k.busy)
			}
		}
		card.Children(func() {
			h := ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth()
			if collapsible, _ := n.Props["collapsible"].(bool); collapsible {
				h.Unselectable()
			}
			h.Children(func() {
				a.tspChevron(c, k, n, collapsed)
				if head != nil {
					a.tspHead = true
					ui.Box(c).Grow(1).Children(func() { a.tspNode(c, k, t, p, sf, head) })
					a.tspHead = false
				} else {
					a.tspRich(c, k, n.Props["head"], 14, true).Grow(1)
				}
				if status != "" {
					a.tspStatus(c, k, status)
				}
			})
			a.tspFoldHead(h, p, sf, n, collapsed)
			if !collapsed {
				for _, ch := range body {
					a.tspNode(c, k, t, p, sf, ch)
				}
			}
		})
		el = card
	case "section":
		head, body, collapsed := a.tspFold(p, sf, n)
		sec := ui.Column(key).Gap(a.tspGap(8)).FillWidth()
		sec.Children(func() {
			h := ui.Row(c).Gap(6).AlignItems(ui.Center).FillWidth()
			if collapsible, _ := n.Props["collapsible"].(bool); collapsible {
				h.Unselectable()
			}
			h.Children(func() {
				a.tspChevron(c, k, n, collapsed)
				if head != nil {
					a.tspHead = true
					a.tspNode(c, k, t, p, sf, head)
					a.tspHead = false
				} else if hd := n.Props["head"]; hd != nil {
					a.tspRich(c, k, hd, 13, true)
				}
			})
			a.tspFoldHead(h, p, sf, n, collapsed)
			if !collapsed {
				for _, ch := range body {
					a.tspNode(c, k, t, p, sf, ch)
				}
			}
		})
		el = sec
	case "text":
		if a.tspPinned && role == "omp.composer.stop" {
			el = a.tspSubmit(c, key, k, n, true, false)
			a.tspSent(p, el)
			break
		}
		if a.tspPinned && role == "omp.working.sep" {
			// Tern divides the time from the label with a rule.
			el = ui.Box(key).Size(1, 14).Background(k.cardBorder).Margin(0, 2)
			break
		}
		truncate, _ := n.Props["truncate"].(string)
		spans, isSpans := n.Props["spans"].([]any)
		if isSpans && (truncate == "middle" || truncate == "start") {
			// Cut where the program says: the ends a link needs stay
			// in sight. Text drawn so can't be selected; its link's
			// context menu copies it.
			sp, hrefs := a.tspSpans(k, spans, 13, false)
			el = a.tspCutText(c, sp, hrefs, truncate)
			break
		}
		// A tone colors the text, as Tern's data-tone: the spans of no
		// color of their own too.
		tone, _ := n.Props["tone"].(string)
		toneCol, toned := a.spanColor(k, tone)
		if !toned && tone == "pending" {
			// Work under way, in amber, as Tern's.
			toneCol, toned = k.attention, true
		}
		switch {
		case role == "omp.jobs.state" && toned:
			// The selected job's status: MyGo's badge in its tone.
			el = ui.Badge(c, tspPlain(n.Props["spans"], text)).Background(toneCol.Alpha(0.15)).TextColor(toneCol)
		case isSpans && toned:
			sp, _ := a.tspSpans(k, spans, 13, false)
			for i := range sp {
				if sp[i].Color == k.text {
					sp[i].Color = toneCol
				}
			}
			el = ui.RichText(c, sp...).TextColor(toneCol)
		case isSpans:
			el = a.tspRich(c, k, spans, 13, false)
		case toned:
			el = ui.Text(c, text).FontSize(a.tspText(13)).TextColor(toneCol)
		default:
			el = ui.Text(c, text).FontSize(a.tspText(13)).TextColor(k.text)
		}
		if a.tspPinned && role == "omp.composer.usage" {
			// The session's cost, quiet beside the send button, as Tern's.
			el.TextColor(k.textMuted)
		}
		// Kept on one line: whole, or cut with an ellipsis.
		if truncate != "" {
			el.SingleLine().MinWidth(0)
		} else if wrap, _ := n.Props["wrap"].(string); wrap == "none" {
			el.SingleLine().Shrink(0)
		}
		if !a.tspHead {
			el.Selectable()
		}
	case "md":
		look := a.tspLook()
		box := ui.Box(key).FillWidth()
		box.Children(func() {
			markdown.View(c, text, &markdown.Options{
				Theme: &markdown.Theme{
					Text: k.text, Muted: k.textMuted, Link: k.busy,
					// The surfaces' messages and code, by the OMP settings:
					// the body at the message size, code in ems of it.
					Font: look.font, FontSize: a.tspMsg(14), LineHeight: look.lineHeight,
					CodeFont: look.mono, CodeSize: a.tspCode(12) / a.tspMsg(14), BlockSpacing: 0.75 * look.gap,
				},
				// Charts come as SVG: drawn in the theme's colors with
				// their labels, once the block is whole. Mermaid diagrams
				// are drawn as pictures (tsp_mermaid.go).
				Code: map[string]func(*ui.Context, *markdown.CodeBlockView){"svg": func(c *ui.Context, b *markdown.CodeBlockView) {
					if !b.Open {
						if s := a.tspSVGFor(k, b.Code()); s != nil {
							s.draw(c)
							return
						}
					}
					markdown.DefaultCodeBlock(c, b)
				}, "mermaid": func(c *ui.Context, b *markdown.CodeBlockView) {
					a.tspMermaidBlock(c, k, p, b)
				}},
				Extensions: tspMDExtensions,
				Math:       a.tspMDMath(k),
				InlineMath: tspInlineMath(k),
				// Files the message names, as `FOO.md`, link to them,
				// when they are (tsp_files.go); a click on one opens it
				// as the settings say, on a web address in the browser.
				CodeLink: a.tspCodeLinker(p),
				OnLink:   func(dest string) { a.tspOpenLink(c, p, dest) },
				LinkMenu: func(m *ui.Menu, dest string) { a.tspLinkItems(m, p, dest) },
			})
		})
		el = box
	case "badge":
		b := ui.Box(key).Radius(6).Background(k.hover).Padding(3, 10)
		b.Children(func() {
			ui.Text(c, text).FontSize(a.tspText(12)).TextColor(k.text)
		})
		el = b
	case "kv":
		items, _ := n.Props["items"].([]any)
		inline, _ := n.Props["layout"].(string)
		kv := ui.Column(key).Gap(4).FillWidth()
		if inline == "inline" {
			kv = ui.Row(key).Gap(14).Wrap().FillWidth()
		}
		kv.Children(func() {
			for i, it := range items {
				pair, _ := it.(map[string]any)
				r := ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Baseline)
				r.Children(func() {
					a.tspRich(c, k, pair["k"], 13, false).TextColor(k.textMuted).MinWidth(90)
					a.tspRich(c, k, pair["v"], 13, false)
				})
			}
		})
		el = kv
	case "list":
		selected, _ := n.Props["selected"].(string)
		wasSel, wasList := a.tspSelected, a.tspList
		a.tspSelected, a.tspList = selected, n.ID
		el = ui.Column(key).Gap(2).FillWidth().Children(children)
		a.tspSelected, a.tspList = wasSel, wasList
	case "item":
		item := ui.Row(key).Gap(8).AlignItems(ui.Center).FillWidth().Padding(3, 6).Radius(4)
		if disabled, _ := n.Props["disabled"].(bool); disabled {
			item.Opacity(0.5)
		}
		selected := a.tspSelected != "" && a.tspSelected == n.ID
		if selected {
			// Tinted, with a bar of the accent at its left, as Tern's.
			item.Background(k.hover).BorderWidth(0, 0, 0, 2).BorderColor(k.busy)
		}
		// Into view as it is selected, not every frame after, which would
		// pin the scroll on it.
		tspReveal(item, selected)
		item.Children(func() {
			if icon, _ := n.Props["icon"].(string); icon != "" {
				ic := a.tspIcon(c, k, icon)
				if tone, _ := n.Props["tone"].(string); tone != "" {
					if col, ok := a.spanColor(k, tone); ok {
						ic.TextColor(col)
					}
				}
			}
			// The label and its detail each on one line, cut to the
			// room; the value whole.
			col := ui.Column(c).Gap(1).Grow(1).MinWidth(0)
			col.Children(func() {
				a.tspRich(c, k, n.Props["label"], 13, false).SingleLine().MinWidth(0)
				if d := n.Props["detail"]; d != nil {
					a.tspRich(c, k, d, 12, false).TextColor(k.textMuted).SingleLine().MinWidth(0)
				}
			})
			if v := n.Props["value"]; v != nil {
				a.tspRich(c, k, v, 12, false).TextColor(k.textMuted).SingleLine().Shrink(0)
			}
			if hint, _ := n.Props["hint"].([]any); len(hint) > 0 {
				a.tspKeys(c, k, hint)
			}
		})
		// In a list, a click selects the item and a double click
		// activates it, as the keys do.
		if a.tspList != "" && !sf.Closed {
			if clicks := item.Clicks(); clicks > 0 {
				ev := "select"
				if clicks > 1 {
					ev = "activate"
				}
				a.surfacePressed(t, p)
				stream, list, id := p.stream, a.tspList, n.ID
				go stream.TSPEvent(map[string]any{"ev": ev, "sf": sf.ID, "id": list, "item": id})
			}
		}
		el = item
	case "input":
		el = a.tspField(c, key, k, t, p, sf, n, false)
	case "overlay":
		// Drawn over the surface, after it (surfaceOverlays).
		if !sf.Closed {
			a.tspOverlays = append(a.tspOverlays, n)
		}
		return
	case "code":
		el = a.tspCodeBlock(c, key, k, p, sf, n, text)
	case "diff":
		// A unified diff, as text or hunks: added lines green, removed
		// red, hunk heads muted.
		var lines []string
		if hunks, ok := n.Props["hunks"].([]any); ok && len(hunks) > 0 {
			for _, h := range hunks {
				h, _ := h.(map[string]any)
				oldStart, _ := h["oldStart"].(float64)
				newStart, _ := h["newStart"].(float64)
				lines = append(lines, fmt.Sprintf("@@ -%d +%d @@", int(oldStart), int(newStart)))
				hl, _ := h["lines"].([]any)
				for _, l := range hl {
					s, _ := l.(string)
					lines = append(lines, s)
				}
			}
		} else {
			lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
		}
		path, _ := n.Props["path"].(string)
		block := ui.Column(key).Radius(6).Background(k.bgLow).Border(1, k.cardBorder).FillWidth().Clip()
		block.Children(func() {
			if path != "" {
				ui.Text(c, path).FontSize(a.tspText(11)).TextColor(k.textMuted).Padding(4, 10, 0, 10)
			}
			body := ui.ScrollHorizontal(c).FillWidth().Padding(6, 0)
			body.Children(func() {
				col := ui.Column(c).Gap(0)
				col.Children(func() {
					for i, l := range lines {
						row := ui.Box(c.Key(i)).Padding(0, 10)
						color := k.text
						switch {
						case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "@@"):
							color = k.textMuted
						case strings.HasPrefix(l, "+"):
							color = k.busy
							row.Background(k.busy.Alpha(0.1))
						case strings.HasPrefix(l, "-"):
							color = k.attention
							row.Background(k.attention.Alpha(0.1))
						}
						row.Children(func() {
							ui.Text(c, l).Font(a.tspLook().mono).FontFeatures(a.tspLook().monoFeatures...).FontSize(a.tspCode(12)).TextColor(color).NoWrap().Selectable()
						})
					}
				})
			})
		})
		el = block
	case "agent":
		// A subagent: its name and kind, state, model and what it cost,
		// its task, and what it did, folding as a card does.
		name, _ := n.Props["name"].(string)
		kind, _ := n.Props["agent"].(string)
		status, _ := n.Props["status"].(string)
		model, _ := n.Props["model"].(string)
		_, body, collapsed := a.tspFold(p, sf, n)
		ag := ui.Column(key).Gap(6).FillWidth().Radius(cardR).Background(k.card).Border(1, k.cardBorder).Padding(8, 12)
		ag.Children(func() {
			h := ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Unselectable()
			h.Children(func() {
				a.tspChevron(c, k, n, collapsed)
				ui.Text(c, name).FontSize(a.tspText(13)).Bold().TextColor(k.text)
				if kind != "" {
					ui.Text(c, kind).FontSize(a.tspText(12)).TextColor(k.textMuted)
				}
				if badges, _ := n.Props["badges"].([]any); len(badges) > 0 {
					for i, b := range badges {
						b, _ := b.(map[string]any)
						t, _ := b["text"].(string)
						ui.Text(c.Key(i), t).FontSize(a.tspText(11)).TextColor(k.textMuted).Padding(1, 6).Radius(4).Background(k.hover)
					}
				}
				ui.Box(c).Grow(1)
				if model != "" {
					ui.Text(c, model).FontSize(a.tspText(11)).TextColor(k.textMuted)
				}
				if st, ok := n.Props["stats"].(map[string]any); ok {
					var parts []string
					if l, _ := st["contextLabel"].(string); l != "" {
						parts = append(parts, l)
					}
					if cost, _ := st["cost"].(float64); cost > 0 {
						parts = append(parts, fmt.Sprintf("$%.2f", cost))
					}
					if len(parts) > 0 {
						ui.Text(c, strings.Join(parts, " · ")).FontSize(a.tspText(11)).TextColor(k.textMuted)
					}
				}
				if status != "" {
					a.tspStatus(c, k, status)
				}
			})
			a.tspFoldHead(h, p, sf, n, collapsed)
			if task := n.Props["task"]; task != nil {
				a.tspRich(c, k, task, 12, false).TextColor(k.textMuted)
			}
			if !collapsed {
				for _, ch := range body {
					a.tspNode(c, k, t, p, sf, ch)
				}
			}
		})
		el = ag
	case "toast":
		sub, _ := n.Props["sub"].(string)
		toast := ui.Column(key).Gap(2).Padding(6, 10).Radius(6).Background(k.panel).Border(1, k.panelBorder)
		toast.Children(func() {
			ui.Text(c, text).FontSize(a.tspText(12)).TextColor(k.text)
			if sub != "" {
				ui.Text(c, sub).FontSize(a.tspText(11)).TextColor(k.textMuted)
			}
		})
		el = toast
	case "chart":
		el = a.tspChart(c, k, key, n)
	case "table":
		el = a.tspTable(c, k, key, n)
	case "progress":
		value, has := n.Props["value"].(float64)
		bar := ui.Column(key).Gap(4).FillWidth()
		bar.Children(func() {
			if l := n.Props["label"]; l != nil {
				a.tspRich(c, k, l, 12, false).TextColor(k.textMuted)
			}
			track := ui.Box(c).Height(6).Radius(3).Background(k.track).FillWidth().Clip()
			track.Children(func() {
				if has {
					ui.Box(c).WidthPercent(float32(max(0, min(1, value)) * 100)).Height(6).Background(k.busy)
				} else {
					activityDot(c, k.busy, 6)
				}
			})
		})
		el = bar
	case "spinner":
		sp := ui.Row(key).Gap(6).AlignItems(ui.Center)
		style, _ := n.Props["style"].(string)
		frame := -1
		if style == "starburst" {
			// omp's working glyph, as Tern draws it: a star that
			// turns through its points; still with less motion.
			frame = 3
			if a.tspMotion() && !sf.Closed {
				frame = int(sp.Loop("starburst", time.Duration(len(tspStarburst))*120*time.Millisecond, ui.Linear)*float32(len(tspStarburst))) % len(tspStarburst)
			}
		}
		sp.Children(func() {
			if frame >= 0 {
				ui.Text(c, tspStarburst[frame]).FontSize(a.tspText(15)).TextColor(k.busy).Width(a.tspText(15)).Shrink(0)
			} else {
				activityDot(c, k.busy, 8)
			}
			if l := n.Props["label"]; l != nil {
				a.tspRich(c, k, l, 12, false).TextColor(k.textMuted)
			}
		})
		el = sp
	case "status":
		st := ui.Row(key).Gap(8).AlignItems(ui.Center).FillWidth().Padding(4, 8).Radius(6)
		if transparent, _ := n.Props["transparent"].(bool); !transparent {
			st.Background(k.hover)
		}
		el = st.Children(children)
	case "rule":
		rule := ui.Row(key).Gap(8).AlignItems(ui.Center).FillWidth()
		rule.Children(func() {
			ui.Box(c).Height(1).Background(k.cardBorder).Grow(1)
			if l := n.Props["label"]; l != nil {
				a.tspRich(c, k, l, 11, false).TextColor(k.textMuted)
				ui.Box(c).Height(1).Background(k.cardBorder).Grow(1)
			}
		})
		el = rule
	case "spacer":
		size, _ := n.Props["size"].(string)
		el = ui.Box(key).Height(map[string]float32{"none": 0, "xs": 2, "sm": 4, "md": 8, "lg": 16}[size])
	case "kbd":
		if a.tspPinned && role == "omp.composer.send" {
			el = a.tspSubmit(c, key, k, n, false, p.composerEmpty)
			a.tspSent(p, el)
			break
		}
		keys, _ := n.Props["keys"].([]any)
		el = a.tspKeys(c.Key(n.ID), k, keys)
	case "icon":
		name, _ := n.Props["name"].(string)
		el = a.tspIcon(c.Key(n.ID), k, name)
		if tone, _ := n.Props["tone"].(string); tone != "" {
			if col, ok := a.spanColor(k, tone); ok {
				el.TextColor(col)
			}
		}
	case "editor":
		el = a.tspField(c, key, k, t, p, sf, n, true)
	case "meter":
		value, has := n.Props["value"].(float64)
		marks, _ := n.Props["marks"].([]any)
		label, _ := n.Props["label"].(string)
		fill := tspMeterFill(k, n)
		meter := ui.Row(key).Gap(6).AlignItems(ui.Center)
		meter.Children(func() {
			track := ui.Box(c).Height(6).Radius(3).Background(k.track).Width(120).Clip()
			track.Children(func() {
				if has {
					ui.Box(c).WidthPercent(float32(max(0, min(1, value)) * 100)).Height(6).Background(fill)
				}
				for i, m := range marks {
					m, _ := m.(map[string]any)
					at, _ := m["at"].(float64)
					ui.Box(c.Key(i)).Absolute().LeftPercent(float32(at * 100)).Top(0).Width(1).Height(6).Background(k.textMuted)
				}
			})
			if label != "" {
				ui.Text(c, label).FontSize(a.tspText(11)).TextColor(k.textMuted)
			}
		})
		el = meter
	case "seg":
		seg := ui.Row(key).Gap(4).AlignItems(ui.Center)
		seg.Children(func() {
			if icon, _ := n.Props["icon"].(string); icon != "" {
				a.tspIcon(c, k, icon)
			}
			a.tspRich(c, k, n.Props["spans"], 12, false).TextColor(k.textMuted).SingleLine()
		})
		el = seg
	case "image":
		alt, _ := n.Props["alt"].(string)
		builtin, _ := n.Props["builtin"].(string)
		if alt == "" {
			alt = builtin
		}
		if builtin == "omp" {
			// omp's mark, which GoRex draws itself, as Tern ships it.
			w, _ := n.Props["w"].(float64)
			el = a.tspLogo(key, float32(w), alt)
			break
		}
		blob, _ := n.Props["blob"].(string)
		if bm := a.tspImages[blob]; bm != nil {
			// The program's image as a thumbnail, its natural size
			// fitted in 160×96; a click shows it whole.
			bw, bh := bm.Size()
			w, h := float64(bw), float64(bh)
			if s := min(1, 160/w, 96/h); s < 1 {
				w, h = w*s, h*s
			}
			img := ui.Image(key, bm).Size(float32(w), float32(h)).Radius(4).Tooltip(alt)
			if img.Clicked() {
				// The file it came from, which the viewer opens.
				path := ""
				if href, _ := n.Props["href"].(string); href != "" {
					if u, err := url.Parse(href); err == nil && u.Scheme == "file" {
						path = hostPathOf(u)
					}
				}
				a.zoomPicture(p, bm, 1, alt, path, nil)
			}
			el = img
			break
		}
		img := ui.Box(key).Radius(6).Background(k.hover).Padding(6, 12)
		img.Children(func() {
			ui.Text(c, alt).FontSize(a.tspText(16)).Bold().TextColor(k.text)
		})
		el = img
	case "elapsed":
		// A live timer: the program says how long so far, in ms, and
		// the window keeps counting from when it heard, until stopped.
		age, _ := n.Props["age"].(float64)
		var stopped *float64
		if s, ok := n.Props["stopped"].(float64); ok {
			stopped = &s
		}
		el = ui.Text(c, elapsedText(a.liveMS(c, p, sf, n.ID, age, stopped)/1000)).FontSize(a.tspText(12)).TextColor(k.textMuted).SingleLine().Shrink(0)
	case "media":
		el = a.tspMedia(c, key, k, p, n)
	case "tabs":
		el = a.tspTabs(c, key, k, p, sf, n)
	case "effort":
		el = a.tspEffort(c, key, k, sf, n)
	case "shimmer":
		el = a.tspShimmer(c, key, k, sf, n)
		if a.tspPinned {
			// omp's working label: one line, cut to the room it has.
			el.SingleLine().MinWidth(0)
		}
	case "rows":
		el = a.tspRows(c, key, k, n)
	case "prefs":
		// In the layer region a sheet, drawn over the surface after it.
		if a.tspPrefsQueued(sf, n) {
			return
		}
		el = a.tspPrefs(c, key, k, t, p, sf, n)
	case "math":
		el = a.tspMath(c, key, k, n)
	case "el":
		// Drawn whole, its actions with its form's values (tsp_el.go).
		a.tspEl(c, key, k, t, p, sf, n)
		return
	case "tool":
		el = a.tspTool(c, key, k, t, p, sf, n)
	case "picker":
		// A modal sheet, drawn over the surface after it.
		if !sf.Closed {
			a.tspOverlays = append(a.tspOverlays, n)
		}
		return
	case "rate":
		value, _ := n.Props["value"].(float64)
		unit, _ := n.Props["unit"].(string)
		el = ui.Text(c, fmt.Sprintf("%.1f %s", value, unit)).FontSize(a.tspText(12)).TextColor(k.textMuted).SingleLine().Shrink(0)
	case "ansi":
		// A program's terminal output in its colors (the terminal's
		// palette), cut to the preview it asks for; with a height bound
		// (max h, tspFlex), scrolling in it, kept at its end with follow
		// while the user leaves it there, as Tern does.
		th := a.ansiPalette()
		font := a.tspLook().mono
		lines := ansiLines(ansiSpans(text, ui.Span{Font: font, Size: a.tspCode(12), Color: k.text}, &th.Palette, k.bgLow))
		for len(lines) > 1 && len(lines[len(lines)-1]) == 0 {
			lines = lines[:len(lines)-1]
		}
		more := 0
		pv, previewed := n.Props["preview"].(map[string]any)
		if previewed {
			if l, ok := pv["lines"].(float64); ok && int(l) < len(lines) {
				more, lines = len(lines)-int(l), lines[:int(l)]
			} else if t, ok := pv["tail"].(float64); ok && int(t) < len(lines) {
				more, lines = len(lines)-int(t), lines[len(lines)-int(t):]
			}
		}
		var spans []ui.Span
		for i, line := range lines {
			if i > 0 {
				spans = append(spans, ui.Span{Text: "\n", Font: font, Size: a.tspCode(12)})
			}
			spans = append(spans, line...)
		}
		body := func() {
			if len(spans) > 0 {
				ui.RichText(c, spans...).FontFeatures(a.tspLook().monoFeatures...).NoWrap().Selectable()
			}
			if more > 0 {
				ui.Text(c, fmt.Sprintf("… %d more lines", more)).FontSize(a.tspText(11)).TextColor(k.textMuted)
			}
		}
		// In a sheet, as Tern's: a faint fill under a hairline.
		fill, edge := k.bgLow, k.bgLow
		if a.tspInOverlay {
			fill, edge = k.text.Alpha(0.04), k.panelBorder
		}
		bound, _ := n.Props["max"].(map[string]any)
		if _, ok := bound["h"]; ok && !previewed {
			f := p.tspFollowOf(fmt.Sprintf("%d/%s", sf.Image, n.ID))
			if follow, _ := n.Props["follow"].(bool); follow && len(lines) != f.lines {
				// New output, or the first: to the end, unless the user
				// scrolled away from it.
				if f.lines == 0 || f.scroll.Y >= f.scroll.MaxY-1 {
					f.scroll.Y = math.MaxFloat32
				}
			}
			f.lines = len(lines)
			// The scroll inset in its box, so that the text keeps clear
			// of the edges and the scroll bar as it scrolls.
			block := ui.Column(key).Radius(6).Background(fill).Border(1, edge).FillWidth().Padding(10, 6, 10, 14).Clip()
			block.Children(func() {
				ui.Scroll(c).TrackScroll(&f.scroll).FillWidth().MinHeight(0).Shrink(1).Children(func() {
					ui.Column(c).Padding(0, 14, 0, 0).Children(body)
				})
			})
			el = block
			break
		}
		el = ui.Column(key).Radius(6).Background(fill).Border(1, edge).FillWidth().Padding(10, 14).Clip().Children(body)
	case "checklist":
		phases, _ := n.Props["phases"].([]any)
		if mode, _ := n.Props["mode"].(string); mode == "hud" {
			el = a.tspChecklistPill(c, key, k, p, sf, n, phases)
			break
		}
		el = a.tspChecklist(c, key, k, n, phases)
	case "tree":
		nodes, _ := n.Props["nodes"].([]any)
		tree := ui.Column(key).Gap(2).FillWidth()
		tree.Children(func() { a.tspTreeNodes(c, k, nodes, 0) })
		el = tree
	default:
		return
	}
	a.revealed(p, sf, n, el)
	if p.tspAnchorWant[n.ID] {
		// An overlay is anchored at it (overlayAnchor).
		p.tspAnchorAt[n.ID] = tspAnchorBox{el.Bounds(), n.Kind, a.tspPinned}
	}
	if grow, _ := n.Props["grow"].(float64); grow > 0 {
		el.Grow(float32(grow))
	}
	tspFlex(p, n, el)
	// A tool's title is its verb, printed in its head (TspToolProps), not
	// a tooltip. A hover card, while it shows, takes the tooltip's place.
	carded := a.tspHoverCardFor(c, k, t, p, sf, n, el)
	if title, _ := n.Props["title"].(string); title != "" && n.Kind != "tool" && !carded {
		el.Tooltip(title)
	}
	a.tspClick(t, p, sf, n, el)
	a.tspHold(el)
	return el
}

// tspRecap draws omp's recap (omp.recap), written while the user was
// away, as Tern does: the history icon beside the text, in italics and
// the muted color, at the labels' size, a little apart from what is
// above it. Not ok for a recap without plain text, which draws as a row.
func (a *App) tspRecap(c *ui.Context, key *ui.Context, k *colors, n *rex.TSPNode) (ui.Element, bool) {
	var recap string
	for _, ch := range n.Children {
		if ch.Kind == "text" {
			recap, _ = ch.Props["text"].(string)
		}
	}
	if recap == "" {
		return ui.Element{}, false
	}
	const lineHeight = 1.4
	size := a.tspText(12)
	row := ui.Row(key).FillWidth().Gap(8).AlignItems(ui.Start).Margin(a.tspGap(6), 0, 0, 0)
	row.Children(func() {
		// On the middle of the text's first line.
		ui.Box(c.Key("icon")).Height(size * lineHeight).Center().Shrink(0).Children(func() {
			ui.Icon(c, icon("history")).Size(size*0.9, size*0.9).TextColor(k.textMuted)
		})
		ui.Text(c.Key("text"), recap).FontSize(size).LineHeight(lineHeight).Italic().TextColor(k.textMuted).
			Grow(1).MinWidth(0).Selectable()
	})
	return row, true
}

// tspFlexed reports whether a node sizes itself in its row (grow, shrink
// or basis), which it then does rather than fill the row.
func tspFlexed(n *rex.TSPNode) bool {
	for _, k := range []string{"grow", "shrink", "basis"} {
		if _, ok := n.Props[k]; ok {
			return true
		}
	}
	return false
}

// tspRowAlign is how a row aligns its children across: as its align
// prop says; else centered, but at the top for a row of panes (a column
// sized in it, as a list beside a reader).
func tspRowAlign(n *rex.TSPNode) ui.Align {
	switch align, _ := n.Props["align"].(string); align {
	case "start":
		return ui.Start
	case "end":
		return ui.End
	case "stretch":
		return ui.Stretch
	case "center", "baseline":
		return ui.Center
	}
	for _, ch := range n.Children {
		if ch.Kind == "col" && tspFlexed(ch) {
			return ui.Start
		}
	}
	return ui.Center
}

// tspFlex applies a node's common sizing props to el: shrink, and the min
// and max bounds, in "<n>ch", "<n>lines" or a fraction of the parent (0
// for a min: as small as the row needs, under its content). A basis is
// the layout's own: one that grows starts from nothing (Grow), as omp's
// basis 0 beside a grow asks.
func tspFlex(p *Pane, n *rex.TSPNode, el ui.Element) {
	if s, ok := n.Props["shrink"].(float64); ok && s >= 0 {
		el.Shrink(float32(s))
	}
	cw, lh := mediaCell(p)
	bound := func(prop, axis string, px, pct func(float32) ui.Element) {
		m, _ := n.Props[prop].(map[string]any)
		v, ok := m[axis]
		if !ok {
			return
		}
		unit := cw
		if axis == "h" {
			unit = lh
		}
		if f, ok := v.(float64); ok && f == 0 && prop == "min" {
			px(0)
			return
		}
		switch d, frac := mediaExtent(v, unit, unit); {
		case d > 0:
			px(d)
		case frac > 0:
			pct(frac * 100)
		}
	}
	bound("min", "w", el.MinWidth, el.MinWidthPercent)
	bound("min", "h", el.MinHeight, el.MinHeightPercent)
	bound("max", "w", el.MaxWidth, el.MaxWidthPercent)
	bound("max", "h", el.MaxHeight, el.MaxHeightPercent)
}

// tspClick sends the program n's click action, where it has one and the
// surface is live, when el is clicked: the program gets it, and the keys
// after it. A copy or open action acts on n's href here first, as Tern
// does (tspLocal), and n's menu actions make el's context menu. Each
// program's surfaces have ids of its own, so the kept ones are keyed by
// their anchors.
func (a *App) tspClick(t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, el ui.Element) {
	actions, ok := n.Props["actions"].(map[string]any)
	if !ok || sf.Closed {
		return
	}
	send := func(act string) {
		a.surfacePressed(t, p)
		stream := p.stream
		ev := map[string]any{"ev": "action", "sf": sf.ID, "id": n.ID, "act": act}
		go func() {
			if err := stream.TSPEvent(ev); err != nil {
				a.post(func() {
					a.toast(ui.Toast{Title: "The program did not get the click", Description: err.Error(), Type: "error"})
				})
			}
		}()
	}
	a.tspMenu(el, n, send)
	act, ok := actions["click"].(string)
	if !ok || act == "" || !el.Clicked() {
		return
	}
	a.tspLocal(n, act)
	send(act)
}

// tspMeterFill is the color of a meter's fill: by its thresholds of what
// is left, warn and bad, the attention color.
func tspMeterFill(k *colors, n *rex.TSPNode) ui.Color {
	value, has := n.Props["value"].(float64)
	if th, ok := n.Props["thresholds"].(map[string]any); ok && has {
		if bad, ok := th["bad"].(float64); ok && 1-value < bad {
			return k.attention
		} else if warn, ok := th["warn"].(float64); ok && 1-value < warn {
			return k.attention.Alpha(0.7)
		}
	}
	return k.busy
}

func (a *App) tspChildren(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	if a.tspCull.on && !a.tspPinned && len(n.Children) >= tspCullMin {
		a.tspCulledChildren(c, k, t, p, sf, n)
		return
	}
	for _, ch := range n.Children {
		a.tspNode(c, k, t, p, sf, ch)
	}
}

// tspRich draws rich text of the protocol's: a string, or spans of
// text with style tokens (muted, strong, em, code, accent, success,
// warning, error, link, …). A span with a web address (href) is a link:
// a click opens it in the browser, and its context menu opens or copies
// it. A span with a file's link (file://) opens the file as a click on a
// file does, underlined while hovered.
func (a *App) tspRich(c *ui.Context, k *colors, v any, size float32, bold bool) ui.Element {
	switch v := v.(type) {
	case string:
		t := ui.Text(c, v).FontSize(a.tspText(size)).TextColor(k.text)
		if bold {
			t.Bold()
		}
		return t
	case []any:
		spans, hrefs := a.tspSpans(k, v, size, bold)
		if !slices.ContainsFunc(hrefs, a.tspOpenable) {
			return ui.RichText(c, spans...)
		}
		return ui.RichText(c).Children(func() {
			for i, sp := range spans {
				switch href := hrefs[i]; {
				case webURL(href):
					l := ui.Link(c.Key(i), "", href).Children(func() { ui.RichText(c, sp) })
					a.tspLinkMenu(l, href)
				case a.tspFileLink(href) != "":
					a.tspFileSpan(c.Key(i), sp, a.tspFileLink(href))
				default:
					ui.RichText(c.Key(i), sp)
				}
			}
		})
	}
	return ui.Text(c, "").FontSize(a.tspText(size))
}

// tspFileLink is the file a span's href links to when a click can open
// it: a file:// link in a pane's surface; "" for none.
func (a *App) tspFileLink(href string) string {
	if a.tspPane == nil {
		return ""
	}
	return tspFileHref(href)
}

// tspOpenable tells an href a click opens: a web address or a file.
func (a *App) tspOpenable(href string) bool { return webURL(href) || a.tspFileLink(href) != "" }

// tspFileSpan draws a span that links to a file as a tool card's file
// name is drawn: underlined while hovered, opening the file on a click as
// the settings say, with the file's menu.
func (a *App) tspFileSpan(c *ui.Context, sp ui.Span, file string) {
	w := ui.RichText(c)
	sp.Underline = w.Hovered()
	w.Children(func() { ui.RichText(c, sp) })
	if a.fileLink(w, a.tspPane, file) {
		a.tspLinkClicked = true
	}
}

// tspButton draws omp's button (an omp.btn row of its label and key) as
// MyGo's button, the key in its tooltip as Tern has it: the accent tone's
// as the primary button, the error tone's label in the error color, one
// without a click action disabled.
func (a *App) tspButton(key *ui.Context, k *colors, n *rex.TSPNode) ui.Element {
	var label strings.Builder
	for _, ch := range n.Children {
		if ch.Kind == "text" {
			text, _ := ch.Props["text"].(string)
			label.WriteString(tspPlain(ch.Props["spans"], text))
		}
	}
	tone, _ := n.Props["tone"].(string)
	var b ui.Element
	if tone == "accent" {
		b = ui.PrimaryButton(key, label.String())
	} else {
		b = ui.Button(key, label.String())
	}
	if tone == "error" {
		b.TextColor(k.attention)
	}
	if _, ok := n.Props["actions"]; !ok {
		b.Disabled(true)
	}
	return b.Shrink(0)
}

// tspPlain is the plain text of the protocol's rich text v (spans), else
// text.
func tspPlain(v any, text string) string {
	spans, ok := v.([]any)
	if !ok {
		return text
	}
	var s strings.Builder
	for _, sp := range spans {
		switch sp := sp.(type) {
		case string:
			s.WriteString(sp)
		case map[string]any:
			t, _ := sp["t"].(string)
			s.WriteString(t)
		}
	}
	return s.String()
}

// tspSpans makes the spans of the protocol's rich text, with each one's
// href ("" for none).
func (a *App) tspSpans(k *colors, v []any, size float32, bold bool) ([]ui.Span, []string) {
	spans := make([]ui.Span, 0, len(v))
	hrefs := make([]string, 0, len(v))
	for _, s := range v {
		m, _ := s.(map[string]any)
		text, _ := m["t"].(string)
		style, _ := m["s"].(string)
		sp := ui.Span{Text: text, Size: a.tspText(size), Color: k.text}
		if bold {
			sp.Weight = 600
		}
		for _, tok := range strings.Fields(style) {
			// A token's color: semantic, or one of the program's
			// palette by name (statusLineModel).
			if col, ok := a.spanColor(k, tok); ok {
				sp.Color = col
			}
			switch tok {
			case "strong":
				sp.Weight = 600
			case "em":
				sp.Italic = true
			case "icon":
				// Glyphs of the Nerd Fonts, which GoRex bundles.
				sp.Font = symbolsFont
			case "code", "mono", "path", "key", "num":
				sp.Font = a.tspLook().mono
			case "del":
				sp.Strikethrough = true
			case "ins":
				sp.Background = k.hover
			case "mark":
				sp.Background = k.hover
				if col, ok := a.tspPal["selectedBg"]; ok {
					sp.Background = tint(k.hover, col)
				}
			}
		}
		href, _ := m["href"].(string)
		if href != "" {
			// A file's link underlines only while hovered (tspFileSpan).
			sp.Underline, sp.Color = tspFileHref(href) == "", k.busy
		}
		spans = append(spans, sp)
		hrefs = append(hrefs, href)
	}
	return spans, hrefs
}

// tspKeys draws key caps, as a hint of what a key does.
func (a *App) tspKeys(c *ui.Context, k *colors, keys []any) ui.Element {
	row := ui.Row(c).Gap(3).AlignItems(ui.Center)
	row.Children(func() {
		for i, key := range keys {
			s, _ := key.(string)
			cap := ui.Box(c.Key(i)).Radius(4).Background(k.bgLow).Border(1, k.cardBorder).Padding(1, 5)
			cap.Children(func() {
				ui.Text(c, s).FontSize(a.tspText(11)).TextColor(k.textMuted)
			})
		}
	})
	return row
}

// tspIcons are GoRex's icons by the protocol's names.
var tspIcons = map[string]string{
	"check": "check", "x": "x", "copy": "copy", "folder": "folder", "folder-open": "folder-open",
	"file": "file", "file-code": "file-code", "branch": "git-branch", "diff": "diff", "trash": "trash-2",
	"search": "search", "terminal": "terminal", "model": "sparkles", "bot": "bot", "settings": "settings-2",
	"users": "users", "user": "users", "job": "play", "run": "play", "play": "play",
	"rewind": "rotate-ccw", "undo": "rotate-ccw", "refresh": "refresh-cw", "more": "ellipsis", "plus": "plus",
	"minus": "minus", "arrow-left": "arrow-left", "chev": "chevron-down", "chev-down": "chevron-down",
	"circle": "circle-dot", "eye": "eye", "pencil": "pencil", "pen": "pencil", "globe": "globe", "database": "database",
	"cpu": "cpu", "server": "server", "keyboard": "keyboard", "command": "command", "activity": "activity",
	"open": "external-link", "link": "link", "history": "history", "doc": "file", "sliders": "settings-2",
	"layers": "layers", "brain": "brain", "laptop": "laptop", "monitor": "monitor", "tree": "folder-tree",
	"list": "list-checks", "list-checks": "list-checks", "box": "puzzle", "puzzle": "puzzle", "plug": "plug",
	"sparkle": "sparkles", "wrench": "wrench", "palette": "palette", "grid": "layout-grid", "columns": "columns-2",
	// omp's settings pages.
	"appearance": "palette", "interaction": "mouse-pointer-click", "context": "layers", "memory": "brain",
	"files": "file", "shell": "terminal", "tools": "wrench", "tasks": "list-checks", "providers": "plug",
	"plugins": "puzzle",
}

// tspIconAliases are the names Tern takes for its icons, when no icon has
// the name (omp's symbol names and common words), as its book lists them.
var tspIconAliases = map[string]string{
	"success": "check", "done": "check", "error": "x", "failed": "x", "warning": "warn", "pending": "clock",
	"running": "activity", "aborted": "stop", "cancelled": "stop", "disabled": "minus", "model": "sparkle",
	"plan": "list", "goal": "pin", "loop": "redo", "git": "branch", "pr": "merge", "tokens": "hash",
	"speculation": "selection", "compaction": "wand", "context": "layers", "cost": "hash", "time": "clock",
	"agents": "user", "agent": "user", "job": "run", "cache": "database", "input": "download", "output": "send",
	"host": "laptop", "session": "history", "worktree": "tree", "bash": "terminal", "shell": "terminal",
	"edit": "pen", "write": "pen", "read": "doc", "web": "globe", "url": "link", "todo": "list", "task": "layers",
	"thinking": "sparkle", "settings": "sliders", "appearance": "ink", "interaction": "keyboard", "memory": "brain",
	"tools": "wand", "tasks": "list", "providers": "server", "plugins": "box", "advisor": "sparkle",
	"git-branch": "branch",
}

// tspIconSVG is GoRex's icon of the protocol's name: by its name, else by
// the icon Tern takes the name for; nil when GoRex has none.
func tspIconSVG(name string) *ui.SVG {
	svg, ok := tspIcons[name]
	if !ok {
		svg, ok = tspIcons[tspIconAliases[name]]
	}
	if !ok {
		return nil
	}
	return icon(svg)
}

// tspIcon draws an icon of the protocol's by name, those GoRex has: by
// its name, else by the icon Tern takes the name for, else a glyph.
func (a *App) tspIcon(c *ui.Context, k *colors, name string) ui.Element {
	if svg := tspIconSVG(name); svg != nil {
		return ui.Icon(c, svg).Size(13, 13).TextColor(k.textMuted).Shrink(0)
	}
	if alias, ok := tspIconAliases[name]; ok {
		name = alias
	}
	glyph := map[string]string{
		"close": "✕", "x-circle": "⊗", "warn": "⚠", "warning": "⚠", "error": "⊗", "info": "ⓘ", "help": "?",
		"arrow-up": "↑", "arrow-down": "↓", "arrow-right": "→", "back": "←", "forward": "→",
		"chev-r": "›", "chev-up": "︿", "dot": "•", "star": "★", "clock": "◷", "timer": "⏱", "lightbulb": "💡",
		"pause": "⏸", "stop": "■", "send": "➤", "lock": "🔒", "key": "⚿", "bell": "🔔",
		"context.speculation": "◔", "context.compaction": "◑", "commit": "●", "merge": "⑂",
	}[name]
	if glyph == "" {
		glyph = "•"
	}
	return ui.Text(c, glyph).FontSize(a.tspText(13)).TextColor(k.textMuted)
}

// tspTable draws a table: its columns' heads, then its rows' cells by
// column id, as rich text or a meter.
func (a *App) tspTable(c *ui.Context, k *colors, key *ui.Context, n *rex.TSPNode) ui.Element {
	cols, _ := n.Props["cols"].([]any)
	rows, _ := n.Props["rows"].([]any)
	table := ui.Column(key).FillWidth().Radius(6).Border(1, k.cardBorder).Clip()
	cell := func(c *ui.Context, col map[string]any, v any, head bool) {
		grow, _ := col["grow"].(float64)
		align, _ := col["align"].(string)
		box := ui.Box(c).Padding(4, 8).MinWidth(60)
		if grow > 0 {
			box.Grow(float32(grow))
		}
		if align == "right" || align == "end" {
			box.AlignItems(ui.End)
		}
		box.Children(func() {
			switch v := v.(type) {
			case map[string]any:
				// A meter cell.
				value, _ := v["value"].(float64)
				track := ui.Box(c).Height(6).Radius(3).Background(k.track).Width(80).Clip()
				track.Children(func() {
					ui.Box(c).WidthPercent(float32(max(0, min(1, value)) * 100)).Height(6).Background(k.busy)
				})
			default:
				t := a.tspRich(c, k, v, 12, head)
				if head {
					t.TextColor(k.textMuted)
				}
			}
		})
	}
	table.Children(func() {
		h := ui.Row(c).FillWidth().Background(k.bgLow)
		h.Children(func() {
			for i, col := range cols {
				col, _ := col.(map[string]any)
				cell(c.Key(i), col, col["head"], true)
			}
		})
		for ri, row := range rows {
			row, _ := row.(map[string]any)
			cells, _ := row["cells"].(map[string]any)
			r := ui.Row(c.Key(ri)).FillWidth()
			if ri%2 == 1 {
				r.Background(k.bgTint)
			}
			r.Children(func() {
				for i, col := range cols {
					col, _ := col.(map[string]any)
					id, _ := col["id"].(string)
					cell(c.Key(i), col, cells[id], false)
				}
			})
		}
	})
	return table
}

// tspStatus draws a card's status chip.
func (a *App) tspStatus(c *ui.Context, k *colors, status string) {
	color := k.textMuted
	switch status {
	case "running":
		color = k.busy
	case "done":
		color = k.busy
	case "error", "failed":
		color = k.attention
	}
	chip := ui.Row(c).Gap(5).AlignItems(ui.Center).Radius(6).Background(k.hover).Padding(2, 8)
	chip.Children(func() {
		if status == "running" {
			activityDot(c, color, 8)
		}
		ui.Text(c, fmt.Sprint(status)).FontSize(a.tspText(11)).TextColor(color)
	})
}

// tspTreeNodes draws a tree's nodes, indented by depth.
func (a *App) tspTreeNodes(c *ui.Context, k *colors, nodes []any, depth int) {
	for i, nd := range nodes {
		nd, _ := nd.(map[string]any)
		row := ui.Row(c.Key(i)).Gap(6).AlignItems(ui.Baseline).FillWidth().Padding(1, 0, 1, float32(depth*16))
		row.Children(func() {
			if icon, _ := nd["icon"].(string); icon != "" {
				a.tspIcon(c, k, icon)
			}
			a.tspRich(c, k, nd["label"], 13, false)
		})
		if children, _ := nd["children"].([]any); len(children) > 0 {
			ui.Column(c.Key(fmt.Sprintf("c%d", i))).Gap(2).FillWidth().Children(func() { a.tspTreeNodes(c, k, children, depth+1) })
		}
	}
}

// elapsedText formats seconds as a program shows how long something
// took: 36s, 1m 12s, 2h 05m.
func elapsedText(secs float64) string {
	s := int(secs)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh %02dm", s/3600, s%3600/60)
}

// elapsedStart is a live timer as the window first saw it: the age the
// program gave, and when.
type elapsedStart struct {
	age float64
	at  time.Time
}

// tspFold reads a card's or section's head and body: the head is the
// child the head prop names (a slot), when it names one, and the body
// the other children; collapsed tells that only the head shows, as the
// program said at first and as the user flipped it since.
func (a *App) tspFold(p *Pane, sf *rex.Surface, n *rex.TSPNode) (head *rex.TSPNode, body []*rex.TSPNode, collapsed bool) {
	slot, _ := n.Props["head"].(string)
	for _, ch := range n.Children {
		if slot != "" && ch.ID == slot && head == nil {
			head = ch
		} else {
			body = append(body, ch)
		}
	}
	if collapsible, _ := n.Props["collapsible"].(bool); collapsible {
		collapsed, _ = n.Props["collapsed"].(bool)
		if v, ok := p.folds[fmt.Sprintf("%d/%s", sf.Image, n.ID)]; ok {
			collapsed = v
		}
	}
	return head, body, collapsed
}

// tspFoldHead makes a collapsible node's head row flip its fold on a
// click, which the program is told of (a toggle event). The row shows it
// can, as MyGo's disclosures do: a pointer, a shade on hover, and a
// disclosure, open or not, to assistive technology.
func (a *App) tspFoldHead(h ui.Element, p *Pane, sf *rex.Surface, n *rex.TSPNode, collapsed bool) {
	if collapsible, _ := n.Props["collapsible"].(bool); !collapsible || sf.Closed {
		return
	}
	h.Cursor(ui.CursorPointer).Role(ui.RoleDisclosure).Expanded(!collapsed).Radius(6).
		Transition(ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond})
	if h.Hovered() {
		h.Background(a.k.hover)
	}
	// A click on a file link in the head opened the file, not the fold.
	if h.Clicked() && !a.tspLinkClicked {
		if p.folds == nil {
			p.folds = map[string]bool{}
		}
		p.folds[fmt.Sprintf("%d/%s", sf.Image, n.ID)] = !collapsed
		stream := p.stream
		ev := map[string]any{"ev": "toggle", "sf": sf.ID, "id": n.ID, "collapsed": !collapsed}
		go stream.TSPEvent(ev)
	}
}

// tspChevron draws a collapsible node's fold mark, first in its head as
// MyGo's disclosures have it: a chevron pointing down while open and
// right while folded, turning between them. A node that does not fold
// has none.
func (a *App) tspChevron(c *ui.Context, k *colors, n *rex.TSPNode, collapsed bool) {
	if collapsible, _ := n.Props["collapsible"].(bool); !collapsible {
		return
	}
	open := float32(1)
	if collapsed {
		open = 0
	}
	s := a.tspText(13)
	ch := ui.Icon(c.Key("fold"), icon("chevron-down")).Size(s, s).TextColor(k.textMuted).Shrink(0)
	ch.Rotate(-90 * (1 - ch.Animate("open", open, 160*time.Millisecond)))
}

// surfaceOverlays draws the overlays a surface's document has (pickers,
// dialogs) floating over the pane: anchored at the bottom, the top or
// the center, sized sm, md, lg or full, a modal one over a dim backdrop.
// Their keys go to the program through the terminal, as any.
func (a *App) surfaceOverlays(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface) {
	overlays := a.tspOverlays
	a.tspOverlays = nil
	a.tspPinned = true
	defer func() { a.tspPinned = false }()
	a.tspScrolled = map[string]bool{}
	defer func() {
		// A closed overlay opens again at its start, unless told
		// otherwise.
		if !sf.Closed {
			for id := range p.tspOverlayScroll {
				if !a.tspScrolled[id] {
					delete(p.tspOverlayScroll, id)
					delete(p.tspOverlayH, id)
				}
			}
		}
		a.tspScrolled = nil
	}()
	for _, ov := range overlays {
		if ov.Kind == "prefs" {
			a.tspPrefsSheet(c, k, t, p, sf, ov)
			continue
		}
		if ov.Kind == "picker" {
			a.tspPicker(c, k, t, p, sf, ov)
			continue
		}
		if modal, _ := ov.Props["modal"].(bool); modal {
			back := ui.Box(c.Key("tsp-back-" + ov.ID)).Absolute().Fill().Background(k.backdrop)
			if back.Pressed() {
				a.surfacePressed(t, p)
			}
		}
		size, _ := ov.Props["size"].(string)
		// Anchored at a node, or at a field's caret: placed beside it
		// (tspOverlayAt), hidden while it is not drawn. Any other
		// anchor centers it.
		at, _ := ov.Props["anchor"].(map[string]any)
		anchored := at["node"] != nil || at["caret"] != nil
		var near tspAnchorBox
		if anchored {
			var ok bool
			if near, ok = a.overlayAnchor(p, at); !ok {
				continue
			}
		}
		width := map[string]float32{"sm": 40, "md": 60, "lg": 80, "full": 100}[size]
		if width == 0 {
			width = 60
		}
		// An overlay of panes side by side (tspPaneSheet) is a sheet of
		// its own height, which the panes' row fills, each scrolling on
		// its own, what is above and under them staying in view; any
		// other scrolls whole.
		sheet := tspPaneSheet(ov)
		if anchored && size != "full" {
			sheet = nil
		}
		// A sheet with a head draws as Tern's: the head on a line of its
		// own over a hairline, what is under it scrolling (or its row of
		// panes, each scrolling); without one it scrolls whole.
		head := ov.Props["head"]
		var panel ui.Element
		switch {
		case sheet != nil:
			panel = ui.Column(c.Key("tsp-sheet-" + ov.ID)).Absolute()
			if !anchored {
				panel.HeightPercent(84)
			}
		case head != nil:
			panel = ui.Column(c.Key("tsp-ov-" + ov.ID)).Absolute().MaxHeightPercent(85)
		default:
			panel = ui.Scroll(c.Key("tsp-ov-" + ov.ID)).Absolute().MaxHeightPercent(85).TrackScroll(p.overlayScroll(ov.ID))
			a.scrolled(p, ov.ID, panel)
		}
		panel.Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Font(a.tspLook().font)
		if head == nil {
			panel.Padding(12, 14)
		} else {
			panel.Clip()
		}
		if anchored {
			a.tspOverlayAt(panel, p, at, near, size)
		} else {
			panel.LeftPercent((100 - width) / 2).WidthPercent(width)
			anchor, _ := ov.Props["anchor"].(string)
			switch anchor {
			case "top":
				panel.Top(8)
			case "bottom":
				panel.Bottom(8)
			default:
				panel.TopPercent(8)
			}
		}
		panel.Children(func() {
			wasOverlay := a.tspInOverlay
			a.tspInOverlay = true
			draw := func() {
				a.tspSheet = sheet
				for _, ch := range ov.Children {
					a.tspNode(c, k, t, p, sf, ch)
				}
				a.tspSheet = nil
			}
			switch {
			case head == nil:
				draw()
			case sheet != nil:
				a.tspSheetHead(c, k, ov, head)
				draw()
			default:
				a.tspSheetHead(c, k, ov, head)
				body := ui.Scroll(c.Key("tsp-ov-body-" + ov.ID)).TrackScroll(p.overlayScroll(ov.ID)).FillWidth().MinHeight(0).Shrink(1)
				a.scrolled(p, ov.ID, body)
				body.Children(func() { ui.Column(c).Gap(a.tspGap(8)).FillWidth().Padding(14, 18).Children(draw) })
			}
			a.tspInOverlay = wasOverlay
		})
		if panel.Pressed() {
			a.surfacePressed(t, p)
		}
		// Escape with the focus in it (its text) is the program's, as
		// with the focus in the terminal: it closes the sheet.
		if panel.Shortcut(0, ui.KeyEscape) {
			a.surfacePressed(t, p)
			p.term.Send([]byte{0x1b})
		}
	}
}

// tspSheetIcons are the icons Tern heads omp's sheets with, which omp
// does not send, by their roles.
var tspSheetIcons = map[string]string{"omp.overlay.jobs": "activity"}

// tspSheetHead draws an overlay's head as Tern does: its icon (its own,
// or its role's) and its title on a row, over a hairline across it.
func (a *App) tspSheetHead(c *ui.Context, k *colors, ov *rex.TSPNode, head any) {
	row := ui.Row(c.Key("head")).Gap(10).AlignItems(ui.Center).FillWidth().Shrink(0).Padding(14, 18).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder)
	row.Children(func() {
		name, _ := ov.Props["icon"].(string)
		if name == "" {
			role, _ := ov.Props["role"].(string)
			name = tspSheetIcons[role]
		}
		if name != "" {
			a.tspIcon(c, k, name).TextColor(k.text)
		}
		a.tspRich(c, k, head, 15, true)
	})
}

// tspSheetLayout is how an overlay of panes lays out (tspPaneSheet): the
// panes' row, the nodes between it and the overlay, which grow to the
// sheet's height, and the panes, which scroll each.
type tspSheetLayout struct {
	row   string
	path  map[string]bool
	panes map[string]bool
}

// tspPaneSheet finds in an overlay a row of panes, columns side by side
// one of which sizes itself in the row (grow or basis), as a list beside
// a reader: nil for none.
func tspPaneSheet(ov *rex.TSPNode) *tspSheetLayout {
	var path []string
	var find func(n *rex.TSPNode) *tspSheetLayout
	find = func(n *rex.TSPNode) *tspSheetLayout {
		if hidden, _ := n.Props["hidden"].(bool); hidden {
			return nil
		}
		if n.Kind == "row" {
			panes := map[string]bool{}
			sized := false
			for _, ch := range n.Children {
				if ch.Kind == "col" {
					panes[ch.ID] = true
					_, grow := ch.Props["grow"]
					_, basis := ch.Props["basis"]
					sized = sized || grow || basis
				}
			}
			// omp's jobs sheet: its list beside the selected job,
			// sized here (tspNode's col), as Tern sizes them.
			if role, _ := n.Props["role"].(string); role == "omp.jobs.split" {
				sized = true
			}
			if sized {
				s := &tspSheetLayout{row: n.ID, path: map[string]bool{}, panes: panes}
				for _, id := range path {
					s.path[id] = true
				}
				return s
			}
		}
		if n.Kind != "col" && n.Kind != "row" {
			return nil
		}
		path = append(path, n.ID)
		for _, ch := range n.Children {
			if s := find(ch); s != nil {
				return s
			}
		}
		path = path[:len(path)-1]
		return nil
	}
	for _, ch := range ov.Children {
		if s := find(ch); s != nil {
			return s
		}
	}
	return nil
}

// scrolled notes that the scroller of node id (an overlay or a pane of
// one) was drawn as el, and how tall it was laid out, for a page of the
// scroll ops (scrollHint).
func (a *App) scrolled(p *Pane, id string, el ui.Element) {
	if p.tspOverlayH == nil {
		p.tspOverlayH = map[string]float32{}
	}
	p.tspOverlayH[id] = el.Bounds().H
	if a.tspScrolled != nil {
		a.tspScrolled[id] = true
	}
}

// tspAnchorBox is where a node an overlay is anchored at was drawn, and
// its kind: a caret anchor needs a field; dock tells one drawn in the
// pinned dock, as the composer's field.
type tspAnchorBox struct {
	r    ui.Rect
	kind string
	dock bool
}

// overlayAnchor returns where the node an anchored overlay names was
// drawn ({node} or {caret}), as the nodes drew this frame (tspNode
// records those the overlays named the frame before); false while it
// is not drawn, or names no field for a caret.
func (a *App) overlayAnchor(p *Pane, at map[string]any) (tspAnchorBox, bool) {
	caret, _ := at["caret"].(string)
	id := caret
	if id == "" {
		id, _ = at["node"].(string)
	}
	if id == "" {
		return tspAnchorBox{}, false
	}
	if p.tspAnchorNext == nil {
		p.tspAnchorNext = map[string]bool{}
	}
	p.tspAnchorNext[id] = true
	box, ok := p.tspAnchorAt[id]
	if !ok || box.r.W <= 0 || caret != "" && box.kind != "editor" && box.kind != "input" {
		return tspAnchorBox{}, false
	}
	return box, true
}

// tspOverlayAt places an anchored overlay beside what it is anchored at
// (near, in the window). One of a field in the dock, as omp's slash
// commands at the composer's caret, opens above the composer, as wide
// as it, as Tern's do. Any other goes under the node, or above it with
// side above (tspOverlayDown), left-aligned with it; under a field, 10
// DIPs left of it; kept 8 DIPs inside the pane, its height capped to
// the room it has. Its width is the size's: sm 320, md 480, lg 720;
// full fills the pane.
func (a *App) tspOverlayAt(panel ui.Element, p *Pane, at map[string]any, near tspAnchorBox, size string) {
	host := p.hostBounds
	// Clear of the terminal's scroll bar at the right edge.
	strip := p.term.ScrollbarStrip()
	if size == "full" {
		panel.Left(8).Right(8 + strip).Top(8).Bottom(8)
		return
	}
	if _, caret := at["caret"]; caret && near.dock && p.tspCard.W > 0 {
		card := p.tspCard
		panel.Left(card.X - host.X).Width(card.W).Bottom(host.Y + host.H - card.Y + 6).MaxHeight(max(card.Y-host.Y-14, 40))
		return
	}
	w := map[string]float32{"md": 480, "lg": 720}[size]
	if w == 0 {
		w = 320
	}
	w = max(min(w, host.W-16-strip), 0)
	r := near.r
	x := r.X - host.X
	if _, caret := at["caret"]; caret {
		x -= 10
	}
	x = max(8, min(x, host.W-w-8-strip))
	panel.Left(x).Width(w)
	below := host.Y + host.H - (r.Y + r.H) - 12
	above := r.Y - host.Y - 12
	side, _ := at["side"].(string)
	if tspOverlayDown(side == "above", below, above, panel.Bounds().H) {
		panel.Top(r.Y + r.H - host.Y + 4).MaxHeight(max(below, 40))
	} else {
		panel.Bottom(host.Y + host.H - r.Y + 4).MaxHeight(max(above, 40))
	}
}

// tspOverlayRoom is the room an anchored overlay needs on its side at
// least, below which it goes to the other side when that has more.
const tspOverlayRoom = 200

// tspOverlayDown reports whether an anchored overlay goes under what it
// is anchored at: the side asked for (under, unless above), but for the
// other when the asked side has less room than the overlay needs (its
// height last frame, and tspOverlayRoom at least, as the height a cap
// left it would never tell) and the other has more.
func tspOverlayDown(above bool, below, aboveRoom, h float32) bool {
	need := max(h, tspOverlayRoom)
	down := !above
	if down && below < need && aboveRoom > below || !down && aboveRoom < need && below > aboveRoom {
		down = !down
	}
	return down
}

// tspChart draws a chart: a bars chart as a bar for each point of its
// series, labeled, scaled to its domain; a spark as a row of thin bars
// (tspSpark); a heatmap as a grid of cells shaded by intensity; the
// summary under it.
func (a *App) tspChart(c *ui.Context, k *colors, key *ui.Context, n *rex.TSPNode) ui.Element {
	kind, _ := n.Props["kind"].(string)
	if kind == "spark" {
		return a.tspSpark(c, k, key, n)
	}
	chart := ui.Column(key).Gap(3).FillWidth()
	chart.Children(func() {
		if kind == "bars" {
			// As Tern draws it: one bar per point, side by side across
			// the row, at most 24 DIPs wide, 2, 4 or 8 lines tall by
			// size, scaled to the domain, in the token's color or the
			// point's tone; a point's title, else label, its tooltip.
			series, _ := n.Props["series"].([]any)
			lo, _ := n.Props["min"].(float64)
			hi, ok := n.Props["max"].(float64)
			if !ok {
				for _, pt := range series {
					pt, _ := pt.(map[string]any)
					if v, _ := pt["value"].(float64); v > hi {
						hi = v
					}
				}
			}
			fill := k.busy
			if tok, _ := n.Props["token"].(string); tok != "" {
				if col, ok := a.spanColor(k, tok); ok {
					fill = col
				}
			}
			size, _ := n.Props["size"].(string)
			lines := map[string]float32{"sm": 2, "lg": 8}[size]
			if lines == 0 {
				lines = 4
			}
			h := lines * a.tspText(13) * 1.4
			bars := ui.Row(c).Gap(2).AlignItems(ui.End).FillWidth().Height(h)
			bars.Children(func() {
				for i, pt := range series {
					pt, _ := pt.(map[string]any)
					v, _ := pt["value"].(float64)
					frac := float32(0)
					if hi > lo {
						frac = float32(max(0, min(1, (v-lo)/(hi-lo))))
					}
					col := fill
					if tone, _ := pt["tone"].(string); tone != "" {
						if tc, ok := a.spanColor(k, tone); ok {
							col = tc
						}
					}
					bar := ui.Box(c.Key(i)).Grow(1).MaxWidth(24).Height(max(1, frac*h)).Radius(2).Background(col)
					if title, _ := pt["title"].(string); title != "" {
						bar.Tooltip(title)
					} else if label, _ := pt["label"].(string); label != "" {
						bar.Tooltip(label)
					}
				}
			})
		} else {
			cells, _ := n.Props["cells"].([]any)
			labels, _ := n.Props["rows"].([]any)
			for i, r := range cells {
				r, _ := r.([]any)
				row := ui.Row(c.Key(i)).Gap(2).AlignItems(ui.Center)
				row.Children(func() {
					if i < len(labels) {
						l, _ := labels[i].(string)
						ui.Text(c, l).FontSize(a.tspText(11)).TextColor(k.textMuted).Width(60).NoWrap()
					}
					for j, v := range r {
						cell := ui.Box(c.Key(j)).Size(12, 12).Radius(2).Background(k.track)
						if f, ok := v.(float64); ok {
							cell.Background(k.busy.Alpha(float32(0.15 + 0.85*max(0, min(1, f)))))
						}
					}
				})
			}
		}
		if s := n.Props["summary"]; s != nil {
			a.tspRich(c, k, s, 12, false).TextColor(k.textMuted)
		}
	})
	return chart
}

// tspSpark draws a spark chart as Tern does: a row of thin bars, one line
// tall, scaled to its domain, in its token's color or each point's tone;
// its summary under it, except in omp's HUD, whose segment says it.
func (a *App) tspSpark(c *ui.Context, k *colors, key *ui.Context, n *rex.TSPNode) ui.Element {
	series, _ := n.Props["series"].([]any)
	lo, _ := n.Props["min"].(float64)
	hi, ok := n.Props["max"].(float64)
	if !ok {
		for _, pt := range series {
			pt, _ := pt.(map[string]any)
			if v, _ := pt["value"].(float64); v > hi {
				hi = v
			}
		}
	}
	fill := k.busy
	if tok, _ := n.Props["token"].(string); tok != "" {
		if col, ok := a.spanColor(k, tok); ok {
			fill = col
		}
	}
	h := a.tspText(13)
	chart := ui.Column(key).Gap(3).AlignItems(ui.Start).Shrink(0)
	chart.Children(func() {
		bars := ui.Row(c).Gap(1).AlignItems(ui.End).Height(h).Shrink(0)
		bars.Children(func() {
			for i, pt := range series {
				pt, _ := pt.(map[string]any)
				v, _ := pt["value"].(float64)
				frac := float32(0)
				if hi > lo {
					frac = float32(max(0, min(1, (v-lo)/(hi-lo))))
				}
				col := fill
				if tone, _ := pt["tone"].(string); tone != "" {
					if tc, ok := a.spanColor(k, tone); ok {
						col = tc
					}
				}
				bar := ui.Box(c.Key(i)).Width(3).Height(max(1, frac*h)).Radius(1).Background(col).Shrink(0)
				if title, _ := pt["title"].(string); title != "" {
					bar.Tooltip(title)
				} else if label, _ := pt["label"].(string); label != "" {
					bar.Tooltip(label)
				}
			}
		})
		if role, _ := n.Props["role"].(string); role != "omp.hud.spark" {
			if s := n.Props["summary"]; s != nil {
				a.tspRich(c, k, s, 12, false).TextColor(k.textMuted)
			}
		}
	})
	return chart
}

// tspCodeBlock draws a code block: its language or path over its text,
// which scrolls sideways, or wraps with wrap. A preview of {lines: N}
// shows its first N lines with a button for the rest, which shows them
// all (and Show less folds them again), as Tern's does.
func (a *App) tspCodeBlock(c *ui.Context, key *ui.Context, k *colors, p *Pane, sf *rex.Surface, n *rex.TSPNode, text string) ui.Element {
	lang, _ := n.Props["lang"].(string)
	path, _ := n.Props["path"].(string)
	wrap, _ := n.Props["wrap"].(bool)
	text = strings.TrimRight(text, "\n")
	more, shown := 0, text
	open := p.codeMore[fmt.Sprintf("%d/%s", sf.Image, n.ID)]
	if pv, ok := n.Props["preview"].(map[string]any); ok {
		if l, ok := pv["lines"].(float64); ok && l > 0 {
			if lines := strings.Split(text, "\n"); len(lines) > int(l) {
				more = len(lines) - int(l)
				if !open {
					shown = strings.Join(lines[:int(l)], "\n")
				}
			}
		}
	}
	// In a sheet, as Tern's: a faint fill under a hairline, roomier;
	// a job's command without its language.
	fill, edge, py, px := k.bgLow, k.cardBorder, float32(8), float32(10)
	if a.tspInOverlay {
		fill, edge, py, px = k.text.Alpha(0.04), k.panelBorder, 10, 14
	}
	if role, _ := n.Props["role"].(string); role == "omp.jobs.command" {
		lang, path = "", ""
	}
	block := ui.Column(key).Radius(6).Background(fill).Border(1, edge).FillWidth().MinWidth(0).Clip()
	block.Children(func() {
		if lang != "" || path != "" {
			label := path
			if label == "" {
				label = lang
			}
			ui.Text(c, label).FontSize(a.tspText(11)).TextColor(k.textMuted).Padding(4, px, 0, px)
		}
		body := ui.ScrollHorizontal(c).FillWidth().Padding(py, px)
		if wrap {
			body = ui.Box(c).FillWidth().MinWidth(0).Padding(py, px)
		}
		body.Children(func() {
			t := ui.Text(c, shown).Font(a.tspLook().mono).FontFeatures(a.tspLook().monoFeatures...).FontSize(a.tspCode(12)).TextColor(k.text).Selectable()
			if wrap {
				t.MinWidth(0)
			} else {
				t.NoWrap()
			}
		})
		if more > 0 {
			label := fmt.Sprintf("%d more lines", more)
			if more == 1 {
				label = "1 more line"
			}
			if open {
				label = "Show less"
			}
			btn := ui.Text(c, label).FontSize(a.tspText(11)).TextColor(k.textMuted).Padding(2, 10, 6, 10).Cursor(ui.CursorPointer).Unselectable()
			if btn.Hovered() {
				btn.TextColor(k.text)
			}
			if btn.Clicked() {
				if p.codeMore == nil {
					p.codeMore = map[string]bool{}
				}
				p.codeMore[fmt.Sprintf("%d/%s", sf.Image, n.ID)] = !open
			}
		}
	})
	return block
}
