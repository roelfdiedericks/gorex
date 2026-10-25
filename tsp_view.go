package main

import (
	"fmt"
	"math"
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
	var placements []terminal.AppPlacement
	for i := range p.surfaces {
		sf := &p.surfaces[i]
		switch {
		case sf.Mode == "screen":
			a.screenSurface(c, k, t, p, sf)
		case sf.Image != 0:
			if placements == nil {
				placements = p.term.AppPlacements()
			}
			for _, pl := range placements {
				if pl.Image == sf.Image {
					a.inlineSurface(c, k, t, p, sf, pl)
					break
				}
			}
		}
	}
}

// screenSurface draws a screen surface over a pane's terminal, filling
// the box it is in.
func (a *App) screenSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface) {
	s := ui.Scroll(c.Key("tsp-"+sf.ID)).Absolute().Fill().Background(k.bgBottom).Padding(12, 16)
	s.Children(func() { a.surfaceContent(c, k, t, p, sf) })
	if s.Pressed() {
		a.surfacePressed(t, p)
	}
}

// inlineSurface draws an inline surface from its anchor's row, over the
// terminal's width. Live, it is as tall as its content, and its height
// in rows, as the last frame laid it out, goes to the server when it
// differs from the rows the surface has; kept, it is cut to those rows.
// Scrolled out of the viewport it draws nothing.
func (a *App) inlineSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, pl terminal.AppPlacement) {
	_, screenRows := p.term.Size()
	rows := max(sf.Rows, p.surfaceRows[sf.ID], 1)
	if pl.Row >= screenRows || pl.Row+rows <= 0 {
		return
	}
	cell := pl.Rect.H / float32(max(pl.Rows, 1))
	box := ui.Box(c.Key(fmt.Sprintf("tsp-%d", sf.Image))).Absolute().Left(pl.Rect.X).Top(pl.Rect.Y).Right(0).Background(k.bgBottom).Padding(4, 8)
	if sf.Closed {
		box.Height(cell * float32(sf.Rows)).Clip()
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
	box.Children(func() { a.surfaceContent(c, k, t, p, sf) })
	if box.Pressed() {
		a.surfacePressed(t, p)
	}
	if sf.Closed || cell <= 0 {
		return
	}
	if h := box.Bounds().H; h > 0 {
		rows := int(math.Ceil(float64(h/cell - 0.01)))
		if rows > 0 && rows != sf.Rows && rows != p.surfaceRows[sf.ID] {
			if p.surfaceRows == nil {
				p.surfaceRows = map[string]int{}
			}
			p.surfaceRows[sf.ID] = rows
			stream, id := p.stream, sf.ID
			go stream.TSPSize(id, rows)
		}
	}
}

// surfaceContent draws a surface's document. Its paragraphs are
// selectable, each on its own (Markdown within itself); a press on one
// takes the focus, as selecting text does anywhere, and a press
// anywhere else on the surface, or on a control of the program's, gives
// it back to the terminal (surfacePressed), as Escape does, which the
// program gets too: the key most programs end a surface with.
func (a *App) surfaceContent(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface) {
	content := ui.Box(c).FillWidth()
	content.Children(func() {
		if sf.Root != nil {
			a.tspNode(c, k, t, p, sf, sf.Root)
		}
	})
	if content.Shortcut(0, ui.KeyEscape) {
		a.surfacePressed(t, p)
		if !sf.Closed {
			p.term.Send([]byte{0x1b})
		}
	}
}

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
	var el ui.Element
	switch n.Kind {
	case "col":
		el = ui.Column(key).Gap(8).FillWidth().Children(children)
	case "row":
		el = ui.Row(key).Gap(8).AlignItems(ui.Center).FillWidth().Children(children)
	case "card":
		status, _ := n.Props["status"].(string)
		variant, _ := n.Props["variant"].(string)
		head, body, collapsed := a.tspFold(p, sf, n)
		card := ui.Column(key).Gap(10).FillWidth()
		if variant != "bare" {
			card.Radius(cardR).Background(k.card).Border(1, k.cardBorder).Padding(12, 14)
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
			markdown.View(c, text, &markdown.Options{Theme: &markdown.Theme{Text: k.text, Muted: k.textMuted, Link: k.busy, CodeFont: a.editorFont().Family}})
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
		el = ui.Column(key).Gap(2).FillWidth().Children(children)
	case "item":
		item := ui.Row(key).Gap(8).AlignItems(ui.Center).FillWidth().Padding(3, 6).Radius(4)
		if disabled, _ := n.Props["disabled"].(bool); disabled {
			item.Opacity(0.5)
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
		el = item
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
		// A program's text field, which it edits itself from the keys the
		// terminal gives it: drawn as it says, with its caret.
		text, _ := n.Props["text"].(string)
		placeholder, _ := n.Props["placeholder"].(string)
		cursor, _ := n.Props["cursor"].(float64)
		ed := ui.Row(key).FillWidth().AlignItems(ui.Center).Gap(0).Padding(2, 0)
		ed.Children(func() {
			if text == "" {
				ui.Box(c).Width(2).Height(16).Background(k.text)
				ui.Text(c, placeholder).FontSize(13).TextColor(k.textFaint)
				return
			}
			at := utf16Offset(text, int(cursor))
			ui.Text(c, text[:at]).FontSize(13).TextColor(k.text)
			ui.Box(c).Width(2).Height(16).Background(k.text)
			ui.Text(c, text[at:]).FontSize(13).TextColor(k.text)
		})
		el = ed
	case "meter":
		value, has := n.Props["value"].(float64)
		marks, _ := n.Props["marks"].([]any)
		track := ui.Box(key).Height(6).Radius(3).Background(k.track).Width(120).Clip()
		track.Children(func() {
			if has {
				ui.Box(c).WidthPercent(float32(max(0, min(1, value)) * 100)).Height(6).Background(k.busy)
			}
			for i, m := range marks {
				m, _ := m.(map[string]any)
				at, _ := m["at"].(float64)
				ui.Box(c.Key(i)).Absolute().LeftPercent(float32(at * 100)).Top(0).Width(1).Height(6).Background(k.textMuted)
			}
		})
		el = track
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
		img := ui.Box(key).Radius(6).Background(k.hover).Padding(6, 12)
		img.Children(func() {
			ui.Text(c, alt).FontSize(16).Bold().TextColor(k.text)
		})
		el = img
	case "elapsed":
		// A live timer: the program says how long so far, in ms, and
		// the window keeps counting from when it heard, until stopped.
		age, _ := n.Props["age"].(float64)
		ms := age
		id := fmt.Sprintf("%d/%s", sf.Image, n.ID)
		if stopped, ok := n.Props["stopped"].(float64); ok {
			ms = stopped
			delete(p.elapsed, id)
		} else if !sf.Closed {
			st, ok := p.elapsed[id]
			if !ok || st.age != age {
				st = elapsedStart{age: age, at: time.Now()}
				if p.elapsed == nil {
					p.elapsed = map[string]elapsedStart{}
				}
				p.elapsed[id] = st
			}
			ms = age + float64(time.Since(st.at)/time.Millisecond)
			if !a.elapsedDue {
				a.elapsedDue = true
				time.AfterFunc(time.Second, func() {
					a.elapsedDue = false
					c.Invalidate()
				})
			}
		}
		el = ui.Text(c, elapsedText(ms/1000)).FontSize(12).TextColor(k.textMuted)
	case "rate":
		value, _ := n.Props["value"].(float64)
		unit, _ := n.Props["unit"].(string)
		el = ui.Text(c, fmt.Sprintf("%.0f %s", value, unit)).FontSize(12).TextColor(k.textMuted)
	case "ansi":
		// A program's terminal output, its colors dropped for now, cut
		// to the preview it asks for.
		lines := strings.Split(stripSGR(text), "\n")
		more := 0
		if pv, ok := n.Props["preview"].(map[string]any); ok {
			if l, ok := pv["lines"].(float64); ok && int(l) < len(lines) {
				more, lines = len(lines)-int(l), lines[:int(l)]
			} else if t, ok := pv["tail"].(float64); ok && int(t) < len(lines) {
				more, lines = len(lines)-int(t), lines[len(lines)-int(t):]
			}
		}
		block := ui.Column(key).Radius(6).Background(k.bgLow).FillWidth().Padding(6, 10).Clip()
		block.Children(func() {
			ui.Text(c, strings.Join(lines, "\n")).Font(a.editorFont().Family).FontSize(12).TextColor(k.text).NoWrap().Selectable()
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
				switch tok {
				case "muted", "dim":
					sp.Color = k.textMuted
				case "strong":
					sp.Weight = 600
				case "em":
					sp.Italic = true
				case "code", "mono", "path", "key", "num":
					sp.Font = a.editorFont().Family
				case "accent", "info", "link":
					sp.Color = k.busy
				case "success":
					sp.Color = k.busy
				case "warning", "error":
					sp.Color = k.attention
				case "del":
					sp.Strikethrough = true
				case "ins", "mark":
					sp.Background = k.hover
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
	glyph := map[string]string{
		"check": "✓", "x": "✕", "close": "✕", "warning": "⚠", "error": "⊗", "info": "ⓘ",
		"arrow-up": "↑", "arrow-down": "↓", "arrow-left": "←", "arrow-right": "→", "back": "←",
		"folder": "▸", "file": "▫", "dot": "•", "star": "★", "clock": "◷", "search": "⌕",
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

// utf16Offset returns the byte offset in s of the UTF-16 unit at, the
// end past them.
func utf16Offset(s string, at int) int {
	units := 0
	for i, r := range s {
		if units >= at {
			return i
		}
		units++
		if r > 0xffff {
			units++
		}
	}
	return len(s)
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

// stripSGR drops the escape sequences of styled terminal output,
// leaving its text.
func stripSGR(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
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
