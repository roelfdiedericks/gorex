package main

import (
	"fmt"
	"math"
	"strings"

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

// inlineSurface draws an inline surface at its anchor's rows. Live, it
// is as tall as its content, and its height in rows, as the last frame
// laid it out, goes to the server when it differs from the anchor's;
// kept, it is cut to the anchor's rows. Scrolled out of the viewport it
// draws nothing.
func (a *App) inlineSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, pl terminal.AppPlacement) {
	if !pl.Visible && pl.Row+pl.Rows <= 0 {
		return
	}
	box := ui.Box(c.Key("tsp-"+sf.ID)).Absolute().Left(pl.Rect.X).Top(pl.Rect.Y).Width(pl.Rect.W).Background(k.bgBottom).Padding(4, 8)
	if sf.Closed {
		box.Height(pl.Rect.H).Clip()
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
	if sf.Closed || pl.Rows <= 0 {
		return
	}
	cell := pl.Rect.H / float32(pl.Rows)
	if h := box.Bounds().H; h > 0 && cell > 0 {
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

// surfaceContent draws a surface's document. Its text is selectable,
// as one selection across the nodes, which copies as lines; a press on
// it takes the focus, as selecting text does anywhere, and a press on
// the surface's edge, or on a control of the program's, gives it back
// to the terminal (surfacePressed).
func (a *App) surfaceContent(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface) {
	content := ui.Box(c).FillWidth().Selectable()
	content.Children(func() {
		if sf.Root != nil {
			a.tspNode(c, k, t, p, sf, sf.Root)
		}
	})
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
		card := ui.Column(key).Radius(cardR).Background(k.card).Border(1, k.cardBorder).Padding(12, 14).Gap(10).FillWidth()
		card.Children(func() {
			h := ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth()
			h.Children(func() {
				a.tspRich(c, k, n.Props["head"], 14, true).Grow(1)
				if status != "" {
					a.tspStatus(c, k, status)
				}
			})
			children()
		})
		el = card
	case "section":
		sec := ui.Column(key).Gap(8).FillWidth()
		sec.Children(func() {
			if head := n.Props["head"]; head != nil {
				a.tspRich(c, k, head, 13, true)
			}
			children()
		})
		el = sec
	case "text":
		el = ui.Text(c, text).FontSize(13).TextColor(k.text)
	case "md":
		box := ui.Box(key).FillWidth()
		box.Children(func() {
			markdown.View(c, text, &markdown.Options{Theme: &markdown.Theme{Text: k.text, Muted: k.textMuted, Link: k.busy, CodeFont: a.editorFont().Family}, ShareSelection: true})
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
				ui.Text(c, text).Font(a.editorFont().Family).FontSize(12).TextColor(k.text).NoWrap()
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
					ui.Box(c).WidthPercent(float32(max(0, min(1, value))*100)).Height(6).Background(k.busy)
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
	default:
		return
	}
	if title, _ := n.Props["title"].(string); title != "" {
		el.Tooltip(title)
	}
	// A click action, where the node has one: the program gets it, and
	// the keys after it.
	if actions, ok := n.Props["actions"].(map[string]any); ok {
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
					ui.Box(c).WidthPercent(float32(max(0, min(1, value))*100)).Height(6).Background(k.busy)
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
