package main

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
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

// tspFont is the font of surfaces: the system's, and the icons of the
// Nerd Fonts, which programs put in their text, where it lacks them.
const tspFont = "system-ui, " + symbolsFont

// surfaces draws the pane's surfaces over its terminal, in the box the
// terminal's view fills.
func (a *App) surfaces(c *ui.Context, k *colors, t *Tab, p *Pane) {
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
	a.surfaceZoom(c, k, t, p)
}

// screenSurface draws a screen surface over a pane's terminal, filling
// the box it is in.
func (a *App) screenSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface) {
	a.tspOverlays = a.tspOverlays[:0]
	s := ui.Scroll(c.Key("tsp-"+sf.ID)).Absolute().Fill().Background(k.bgBottom).Padding(12, 16).Font(tspFont)
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
// no terminal shows between them. A surface takes no more rows than the
// scrollback keeps (rex.Surface.Most): content taller shows its end, its
// head cut, as a terminal's oldest lines go first; so does a surface
// whose anchor the scrollback dropped (headless), placed up from its end
// marker. Scrolled out of the viewport it draws nothing.
func (a *App) inlineSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, pl terminal.AppPlacement, headless bool) {
	_, screenRows := p.term.Size()
	docked := !sf.Closed && tspDocked(sf)
	// A live surface with a dock reaches the pane's bottom whatever rows
	// it has: it shows while its anchor is above the view's end.
	rows := max(sf.Rows+sf.Dock, p.surfaceRows[sf.ID].rows+p.surfaceRows[sf.ID].dock, 1)
	if pl.Row >= screenRows || !docked && pl.Row+rows <= 0 {
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
	cut := headless || most > 0 && p.tspNeed[sf.Image] > most
	box := ui.Box(c.Key(fmt.Sprintf("tsp-%d", sf.Image))).Absolute().Left(pl.Rect.X).Top(pl.Rect.Y).Right(tspAside(p, sf, pl)).Background(k.bgBottom).Font(tspFont)
	switch {
	case sf.Closed:
		box.Height(cell * float32(sf.Rows))
	case cut:
		// Its rows, and its dock's, which reach the pane's bottom
		// when it is scrolled to the end: a surface cut takes the
		// screen.
		box.Height(cell * float32(sf.Rows+sf.Dock))
	case docked:
		box.Bottom(0)
	}
	if cut || sf.Closed {
		box.Clip()
	}
	if !cut {
		box.Padding(4, 8)
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
		if !cut {
			content = a.surfaceContent(c, k, t, p, sf, !sf.Closed)
			return
		}
		// Laid out from the box's bottom, the content's head goes out
		// of the top, where the box cuts it.
		end := ui.Box(c.Key("end")).Absolute().Left(8).Right(8).Bottom(4)
		end.Children(func() { content = a.surfaceContent(c, k, t, p, sf, !sf.Closed) })
	})
	if box.Pressed() {
		a.surfacePressed(t, p)
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
	if h := content.Bounds().H + 8 - dockH; h > 0 {
		size := tspSize{int(math.Ceil(float64(h/cell - 0.01))), int(math.Ceil(float64(dockH/cell - 0.01)))}
		if p.tspNeed == nil {
			p.tspNeed = map[uint32]int{}
		}
		p.tspNeed[sf.Image] = size.rows
		if most > 0 {
			size.rows = min(size.rows, most)
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
		}
	}
}

// tspSize is what a window measures a live inline surface to need: the
// rows of its content, and of the dock pinned under it.
type tspSize struct{ rows, dock int }

// tspDocked reports whether a surface has a dock region, as the server
// pads it for (rex.Surface.docked).
func tspDocked(sf *rex.Surface) bool {
	if sf.Root == nil {
		return false
	}
	for _, n := range sf.Root.Children {
		if n.ID == "dock" {
			return true
		}
	}
	return false
}

// surfaceDock draws a live surface's dock region (a program's composer
// and status bar) pinned to the bottom of the pane, where the user
// types whatever part of the output they look at; the surface keeps a
// space as tall in its place, so that at the bottom the dock is where
// it would be.
func (a *App) surfaceDock(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, dock *rex.TSPNode, x float32) {
	pinned := ui.Column(c.Key(fmt.Sprintf("tsp-dock-%d", sf.Image))).Absolute().Left(x).Right(p.term.ScrollbarStrip()).Bottom(0).
		Background(k.bgBottom).Padding(4, 8).Gap(8).Font(tspFont)
	term := p.term
	pinned.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		return term.Scroll(ev)
	})
	pinned.Children(func() {
		a.tspPinned = true
		for _, ch := range dock.Children {
			a.tspNode(c, k, t, p, sf, ch)
		}
		a.tspPinned = false
	})
	if pinned.Pressed() {
		a.surfacePressed(t, p)
	}
	if h := pinned.Bounds().H - 8; h > 0 && h != p.dockH[sf.Image] {
		if p.dockH == nil {
			p.dockH = map[uint32]float32{}
		}
		p.dockH[sf.Image] = h
	}
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
	content := ui.Column(c).FillWidth().Gap(8)
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
// go to the program.
func (a *App) surfacePressed(t *Tab, p *Pane) {
	if t.Focus != p {
		t.setFocus(p)
	}
	a.focusReq = p
	a.changed()
}

// tspNode draws a node and its children.
func (a *App) tspNode(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	if hidden, _ := n.Props["hidden"].(bool); hidden {
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
	var el ui.Element
	switch n.Kind {
	case "col":
		el = ui.Column(key).Gap(8).FillWidth().Children(children)
	case "row":
		row := ui.Row(key).Gap(8).AlignItems(ui.Center).FillWidth()
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
		el = row.Children(children)
	case "card":
		status, _ := n.Props["status"].(string)
		variant, _ := n.Props["variant"].(string)
		head, body, collapsed := a.tspFold(p, sf, n)
		card := ui.Column(key).Gap(10).FillWidth()
		if variant != "bare" && a.tspFlat == 0 {
			card.Radius(cardR).Background(k.card).Border(1, k.cardBorder).Padding(12, 14)
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
		sec := ui.Column(key).Gap(8).FillWidth()
		sec.Children(func() {
			h := ui.Row(c).Gap(6).AlignItems(ui.Center).FillWidth()
			if collapsible, _ := n.Props["collapsible"].(bool); collapsible {
				h.Unselectable()
			}
			h.Children(func() {
				if collapsible, _ := n.Props["collapsible"].(bool); collapsible {
					glyph := "▾"
					if collapsed {
						glyph = "▸"
					}
					ui.Text(c, glyph).FontSize(12).TextColor(k.textMuted)
				}
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
		if spans, ok := n.Props["spans"]; ok {
			el = a.tspRich(c, k, spans, 13, false)
		} else {
			el = ui.Text(c, text).FontSize(13).TextColor(k.text)
		}
		if !a.tspHead {
			el.Selectable()
		}
	case "md":
		box := ui.Box(key).FillWidth()
		box.Children(func() {
			markdown.View(c, text, &markdown.Options{
				Theme: &markdown.Theme{Text: k.text, Muted: k.textMuted, Link: k.busy, CodeFont: a.editorFont().Family},
				// Charts come as SVG: drawn in the theme's colors with
				// their labels, once the block is whole.
				Code: map[string]func(*ui.Context, *markdown.CodeBlockView){"svg": func(c *ui.Context, b *markdown.CodeBlockView) {
					if !b.Open {
						if s := a.tspSVGFor(k, b.Code()); s != nil {
							s.draw(c)
							return
						}
					}
					markdown.DefaultCodeBlock(c, b)
				}},
				Extensions: tspMDExtensions,
				Math:       a.tspMDMath(k),
				InlineMath: tspInlineMath(k),
			})
		})
		el = box
	case "badge":
		b := ui.Box(key).Radius(6).Background(k.hover).Padding(3, 10)
		b.Children(func() {
			ui.Text(c, text).FontSize(12).TextColor(k.text)
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
		if a.tspSelected != "" && a.tspSelected == n.ID {
			item.Background(k.hover).ScrollIntoView()
		}
		item.Children(func() {
			if icon, _ := n.Props["icon"].(string); icon != "" {
				a.tspIcon(c, k, icon)
			}
			col := ui.Column(c).Gap(1).Grow(1)
			col.Children(func() {
				a.tspRich(c, k, n.Props["label"], 13, false)
				if d := n.Props["detail"]; d != nil {
					a.tspRich(c, k, d, 12, false).TextColor(k.textMuted)
				}
			})
			if v := n.Props["value"]; v != nil {
				a.tspRich(c, k, v, 12, false).TextColor(k.textMuted)
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
		lang, _ := n.Props["lang"].(string)
		path, _ := n.Props["path"].(string)
		block := ui.Column(key).Radius(6).Background(k.bgLow).Border(1, k.cardBorder).FillWidth().Clip()
		block.Children(func() {
			if lang != "" || path != "" {
				label := path
				if label == "" {
					label = lang
				}
				ui.Text(c, label).FontSize(11).TextColor(k.textMuted).Padding(4, 10, 0, 10)
			}
			body := ui.ScrollHorizontal(c).FillWidth().Padding(8, 10)
			if wrap, _ := n.Props["wrap"].(bool); wrap {
				body = ui.Box(c).FillWidth().Padding(8, 10)
			}
			body.Children(func() {
				ui.Text(c, text).Font(a.editorFont().Family).FontSize(12).TextColor(k.text).NoWrap().Selectable()
			})
		})
		el = block
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
				ui.Text(c, path).FontSize(11).TextColor(k.textMuted).Padding(4, 10, 0, 10)
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
							ui.Text(c, l).Font(a.editorFont().Family).FontSize(12).TextColor(color).NoWrap().Selectable()
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
				ui.Text(c, name).FontSize(13).Bold().TextColor(k.text)
				if kind != "" {
					ui.Text(c, kind).FontSize(12).TextColor(k.textMuted)
				}
				if badges, _ := n.Props["badges"].([]any); len(badges) > 0 {
					for i, b := range badges {
						b, _ := b.(map[string]any)
						t, _ := b["text"].(string)
						ui.Text(c.Key(i), t).FontSize(11).TextColor(k.textMuted).Padding(1, 6).Radius(4).Background(k.hover)
					}
				}
				ui.Box(c).Grow(1)
				if model != "" {
					ui.Text(c, model).FontSize(11).TextColor(k.textMuted)
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
						ui.Text(c, strings.Join(parts, " · ")).FontSize(11).TextColor(k.textMuted)
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
			ui.Text(c, text).FontSize(12).TextColor(k.text)
			if sub != "" {
				ui.Text(c, sub).FontSize(11).TextColor(k.textMuted)
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
		sp.Children(func() {
			activityDot(c, k.busy, 8)
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
		keys, _ := n.Props["keys"].([]any)
		el = a.tspKeys(c.Key(n.ID), k, keys)
	case "icon":
		name, _ := n.Props["name"].(string)
		el = a.tspIcon(c.Key(n.ID), k, name)
	case "editor":
		el = a.tspField(c, key, k, t, p, sf, n, true)
	case "meter":
		value, has := n.Props["value"].(float64)
		marks, _ := n.Props["marks"].([]any)
		label, _ := n.Props["label"].(string)
		fill := k.busy
		if th, ok := n.Props["thresholds"].(map[string]any); ok && has {
			// Thresholds of what is left: below warn, then below bad.
			if bad, ok := th["bad"].(float64); ok && 1-value < bad {
				fill = k.attention
			} else if warn, ok := th["warn"].(float64); ok && 1-value < warn {
				fill = k.attention.Alpha(0.7)
			}
		}
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
				ui.Text(c, label).FontSize(11).TextColor(k.textMuted)
			}
		})
		el = meter
	case "seg":
		seg := ui.Row(key).Gap(4).AlignItems(ui.Center)
		seg.Children(func() {
			if icon, _ := n.Props["icon"].(string); icon != "" {
				a.tspIcon(c, k, icon)
			}
			a.tspRich(c, k, n.Props["spans"], 12, false).TextColor(k.textMuted)
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
				a.tspZoom, a.tspZoomAlt, a.tspZoomPane = bm, alt, p
				// The file it came from, which the viewer opens.
				a.tspZoomPath = ""
				if href, _ := n.Props["href"].(string); href != "" {
					if u, err := url.Parse(href); err == nil && u.Scheme == "file" {
						a.tspZoomPath = hostPathOf(u)
					}
				}
			}
			el = img
			break
		}
		img := ui.Box(key).Radius(6).Background(k.hover).Padding(6, 12)
		img.Children(func() {
			ui.Text(c, alt).FontSize(16).Bold().TextColor(k.text)
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
		el = ui.Text(c, elapsedText(a.liveMS(c, p, sf, n.ID, age, stopped)/1000)).FontSize(12).TextColor(k.textMuted)
	case "media":
		el = a.tspMedia(c, key, k, p, n)
	case "tabs":
		el = a.tspTabs(c, key, k, p, sf, n)
	case "effort":
		el = a.tspEffort(c, key, k, sf, n)
	case "shimmer":
		el = a.tspShimmer(c, key, k, sf, n)
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
		el = ui.Text(c, fmt.Sprintf("%.0f %s", value, unit)).FontSize(12).TextColor(k.textMuted)
	case "ansi":
		// A program's terminal output in its colors (the terminal's
		// palette), cut to the preview it asks for.
		th := a.ansiPalette()
		font := a.editorFont().Family
		lines := ansiLines(ansiSpans(text, ui.Span{Font: font, Size: 12, Color: k.text}, &th.Palette, k.bgLow))
		for len(lines) > 1 && len(lines[len(lines)-1]) == 0 {
			lines = lines[:len(lines)-1]
		}
		more := 0
		if pv, ok := n.Props["preview"].(map[string]any); ok {
			if l, ok := pv["lines"].(float64); ok && int(l) < len(lines) {
				more, lines = len(lines)-int(l), lines[:int(l)]
			} else if t, ok := pv["tail"].(float64); ok && int(t) < len(lines) {
				more, lines = len(lines)-int(t), lines[len(lines)-int(t):]
			}
		}
		var spans []ui.Span
		for i, line := range lines {
			if i > 0 {
				spans = append(spans, ui.Span{Text: "\n", Font: font, Size: 12})
			}
			spans = append(spans, line...)
		}
		block := ui.Column(key).Radius(6).Background(k.bgLow).FillWidth().Padding(6, 10).Clip()
		block.Children(func() {
			if len(spans) > 0 {
				ui.RichText(c, spans...).NoWrap().Selectable()
			}
			if more > 0 {
				ui.Text(c, fmt.Sprintf("… %d more lines", more)).FontSize(11).TextColor(k.textMuted)
			}
		})
		el = block
	case "checklist":
		phases, _ := n.Props["phases"].([]any)
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
						ui.Text(c, glyph).FontSize(13).TextColor(color)
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
		el = cl
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
		p.tspAnchorAt[n.ID] = tspAnchorBox{el.Bounds(), n.Kind}
	}
	if grow, _ := n.Props["grow"].(float64); grow > 0 {
		el.Grow(float32(grow))
	}
	if title, _ := n.Props["title"].(string); title != "" {
		el.Tooltip(title)
	}
	// A click action, where the node has one, of a live surface: the
	// program gets it, and the keys after it. Each program's surfaces
	// have ids of its own, so the kept ones are keyed by their anchors.
	if actions, ok := n.Props["actions"].(map[string]any); ok && !sf.Closed {
		if act, ok := actions["click"].(string); ok && act != "" {
			if el.Clicked() {
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
		}
	}
}

func (a *App) tspChildren(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	for _, ch := range n.Children {
		a.tspNode(c, k, t, p, sf, ch)
	}
}

// tspRich draws rich text of the protocol's: a string, or spans of
// text with style tokens (muted, strong, em, code, accent, success,
// warning, error, link, …).
func (a *App) tspRich(c *ui.Context, k *colors, v any, size float32, bold bool) ui.Element {
	switch v := v.(type) {
	case string:
		t := ui.Text(c, v).FontSize(size).TextColor(k.text)
		if bold {
			t.Bold()
		}
		return t
	case []any:
		spans := make([]ui.Span, 0, len(v))
		for _, s := range v {
			m, _ := s.(map[string]any)
			text, _ := m["t"].(string)
			style, _ := m["s"].(string)
			sp := ui.Span{Text: text, Size: size, Color: k.text}
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
					sp.Font = a.editorFont().Family
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
			if href, _ := m["href"].(string); href != "" {
				sp.Underline, sp.Color = true, k.busy
			}
			spans = append(spans, sp)
		}
		return ui.RichText(c, spans...)
	}
	return ui.Text(c, "").FontSize(size)
}

// tspKeys draws key caps, as a hint of what a key does.
func (a *App) tspKeys(c *ui.Context, k *colors, keys []any) ui.Element {
	row := ui.Row(c).Gap(3).AlignItems(ui.Center)
	row.Children(func() {
		for i, key := range keys {
			s, _ := key.(string)
			cap := ui.Box(c.Key(i)).Radius(4).Background(k.bgLow).Border(1, k.cardBorder).Padding(1, 5)
			cap.Children(func() {
				ui.Text(c, s).FontSize(11).TextColor(k.textMuted)
			})
		}
	})
	return row
}

// tspIcon draws an icon of the protocol's by name, those GoRex has.
func (a *App) tspIcon(c *ui.Context, k *colors, name string) ui.Element {
	// GoRex's icons, by the protocol's names.
	if svg, ok := map[string]string{
		"check": "check", "x": "x", "copy": "copy", "folder": "folder", "folder-open": "folder-open",
		"file": "file", "file-code": "file-code", "branch": "git-branch", "diff": "diff", "trash": "trash-2",
		"search": "search", "terminal": "terminal", "model": "sparkles", "bot": "bot", "settings": "settings-2",
		"rewind": "rotate-ccw", "undo": "rotate-ccw", "refresh": "refresh-cw", "more": "ellipsis", "plus": "plus",
		"minus": "minus", "arrow-left": "arrow-left", "chev": "chevron-down", "chev-down": "chevron-down",
		"circle": "circle-dot", "eye": "eye", "pencil": "pencil", "globe": "globe", "database": "database",
		"cpu": "cpu", "server": "server", "keyboard": "keyboard", "command": "command", "activity": "activity",
	}[name]; ok {
		return ui.Icon(c, icon(svg)).Size(13, 13).TextColor(k.textMuted).Shrink(0)
	}
	glyph := map[string]string{
		"close": "✕", "x-circle": "⊗", "warn": "⚠", "warning": "⚠", "error": "⊗", "info": "ⓘ", "help": "?",
		"arrow-up": "↑", "arrow-down": "↓", "arrow-right": "→", "back": "←", "forward": "→",
		"chev-r": "›", "chev-up": "︿", "dot": "•", "star": "★", "clock": "◷", "timer": "⏱", "lightbulb": "💡",
		"play": "▶", "pause": "⏸", "stop": "■", "send": "➤", "lock": "🔒", "key": "⚿", "bell": "🔔",
		"context.speculation": "◔", "context.compaction": "◑", "commit": "●", "merge": "⑂",
	}[name]
	if glyph == "" {
		glyph = "•"
	}
	return ui.Text(c, glyph).FontSize(13).TextColor(k.textMuted)
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
		ui.Text(c, fmt.Sprint(status)).FontSize(11).TextColor(color)
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
// click, which the program is told of (a toggle event).
func (a *App) tspFoldHead(h ui.Element, p *Pane, sf *rex.Surface, n *rex.TSPNode, collapsed bool) {
	if collapsible, _ := n.Props["collapsible"].(bool); !collapsible || sf.Closed {
		return
	}
	if h.Clicked() {
		if p.folds == nil {
			p.folds = map[string]bool{}
		}
		p.folds[fmt.Sprintf("%d/%s", sf.Image, n.ID)] = !collapsed
		stream := p.stream
		ev := map[string]any{"ev": "toggle", "sf": sf.ID, "id": n.ID, "collapsed": !collapsed}
		go stream.TSPEvent(ev)
	}
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
		var near ui.Rect
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
		panel := ui.Scroll(c.Key("tsp-ov-"+ov.ID)).Absolute().MaxHeightPercent(85).
			Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Padding(12, 14).Font(tspFont)
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
			if head := ov.Props["head"]; head != nil {
				a.tspRich(c, k, head, 14, true)
			}
			for _, ch := range ov.Children {
				a.tspNode(c, k, t, p, sf, ch)
			}
		})
		if panel.Pressed() {
			a.surfacePressed(t, p)
		}
	}
}

// tspAnchorBox is where a node an overlay is anchored at was drawn, and
// its kind: a caret anchor needs a field.
type tspAnchorBox struct {
	r    ui.Rect
	kind string
}

// overlayAnchor returns where the node an anchored overlay names was
// drawn ({node} or {caret}), as the nodes drew this frame (tspNode
// records those the overlays named the frame before); false while it
// is not drawn, or names no field for a caret.
func (a *App) overlayAnchor(p *Pane, at map[string]any) (ui.Rect, bool) {
	caret, _ := at["caret"].(string)
	id := caret
	if id == "" {
		id, _ = at["node"].(string)
	}
	if id == "" {
		return ui.Rect{}, false
	}
	if p.tspAnchorNext == nil {
		p.tspAnchorNext = map[string]bool{}
	}
	p.tspAnchorNext[id] = true
	box, ok := p.tspAnchorAt[id]
	if !ok || box.r.W <= 0 || caret != "" && box.kind != "editor" && box.kind != "input" {
		return ui.Rect{}, false
	}
	return box.r, true
}

// tspOverlayAt places an anchored overlay beside what it is anchored at
// (near, in the window): under a node, or above it with side above,
// left-aligned with it; under a field, 10 DIPs left of it. It goes to
// the other side when it does not fit and that side has more room, and
// is kept 8 DIPs inside the pane, its height capped to the room it has.
// Its width is the size's: sm 320, md 480, lg 720; full fills the pane.
func (a *App) tspOverlayAt(panel ui.Element, p *Pane, at map[string]any, near ui.Rect, size string) {
	host := p.hostBounds
	// Clear of the terminal's scroll bar at the right edge.
	strip := p.term.ScrollbarStrip()
	if size == "full" {
		panel.Left(8).Right(8 + strip).Top(8).Bottom(8)
		return
	}
	w := map[string]float32{"md": 480, "lg": 720}[size]
	if w == 0 {
		w = 320
	}
	w = max(min(w, host.W-16-strip), 0)
	x := near.X - host.X
	if _, caret := at["caret"]; caret {
		x -= 10
	}
	x = max(8, min(x, host.W-w-8-strip))
	panel.Left(x).Width(w)
	below := host.Y + host.H - (near.Y + near.H) - 12
	above := near.Y - host.Y - 12
	h := panel.Bounds().H
	side, _ := at["side"].(string)
	down := side != "above"
	if down && h > below && above > below || !down && h > above && below > above {
		down = !down
	}
	if down {
		panel.Top(near.Y + near.H - host.Y + 4).MaxHeight(max(below, 40))
	} else {
		panel.Bottom(host.Y + host.H - near.Y + 4).MaxHeight(max(above, 40))
	}
}

// tspChart draws a chart: a bars or spark chart as a bar for each point
// of its series, labeled, scaled to its domain; a heatmap as a grid of
// cells shaded by intensity; the summary under it.
func (a *App) tspChart(c *ui.Context, k *colors, key *ui.Context, n *rex.TSPNode) ui.Element {
	kind, _ := n.Props["kind"].(string)
	chart := ui.Column(key).Gap(3).FillWidth()
	chart.Children(func() {
		if kind == "bars" || kind == "spark" {
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
			for i, pt := range series {
				pt, _ := pt.(map[string]any)
				v, _ := pt["value"].(float64)
				label, _ := pt["label"].(string)
				frac := float32(0)
				if hi > lo {
					frac = float32(max(0, min(1, (v-lo)/(hi-lo))))
				}
				row := ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Center).FillWidth()
				row.Children(func() {
					ui.Text(c, label).FontSize(12).TextColor(k.textMuted).Width(140).NoWrap()
					track := ui.Box(c).Height(10).Grow(1).Radius(2).Clip()
					track.Children(func() {
						ui.Box(c).WidthPercent(frac * 100).Height(10).Background(k.busy)
					})
					ui.Text(c, strconv.FormatFloat(v, 'g', 6, 64)).FontSize(12).TextColor(k.textMuted).Width(70)
				})
				if title, _ := pt["title"].(string); title != "" {
					row.Tooltip(title)
				}
			}
		} else {
			cells, _ := n.Props["cells"].([]any)
			labels, _ := n.Props["rows"].([]any)
			for i, r := range cells {
				r, _ := r.([]any)
				row := ui.Row(c.Key(i)).Gap(2).AlignItems(ui.Center)
				row.Children(func() {
					if i < len(labels) {
						l, _ := labels[i].(string)
						ui.Text(c, l).FontSize(11).TextColor(k.textMuted).Width(60).NoWrap()
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

// surfaceZoom shows a surface's image whole over the pane, fitted to
// it, with its name and size, and an Open file button when it came
// from a file of the host's: a click or Escape closes it.
func (a *App) surfaceZoom(c *ui.Context, k *colors, t *Tab, p *Pane) {
	bm := a.tspZoom
	if bm == nil || a.tspZoomPane != p {
		return
	}
	w, h := bm.Size()
	open := false
	view := ui.Column(c.Key("tsp-zoom")).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.75)).
		Center().Gap(8).Padding(16).Focusable().FocusRing(false).Focus().Font(tspFont)
	view.Children(func() {
		ui.Image(c, bm).MaxWidthPercent(100).MaxHeightPercent(90)
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			caption := fmt.Sprintf("%d×%d", w, h)
			if a.tspZoomAlt != "" {
				caption = a.tspZoomAlt + "  " + caption
			}
			ui.Text(c, caption).FontSize(12).TextColor(ui.RGB(230, 230, 230))
			if a.tspZoomPath != "" {
				open = textButton(c, k, "Open file").Tooltip(a.tspZoomPath).Clicked()
			}
		})
	})
	if open {
		path := a.tspZoomPath
		a.tspZoom, a.tspZoomPane = nil, nil
		a.update(func() { a.openPaneFile(p, path) })
		return
	}
	if view.Clicked() || view.Shortcut(0, ui.KeyEscape) {
		a.tspZoom, a.tspZoomPane = nil, nil
		a.surfacePressed(t, p)
	}
}
