package main

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// tspPicker draws a picker (kind picker), a modal sheet over the pane: a
// head of icon, title and subtitle with the count, the program's search
// field (its keys stay the program's), tabs, a scope column, the rows in
// the program's order with their group headers, facts in columns, the
// selected row's preview below or beside, a confirm strip, a chip strip
// and the action bar. A click on a row selects it, a second activates
// it; tabs, scopes, chips, the confirm and the actions send actions; the
// backdrop sends close.
func (a *App) tspPicker(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	pr := n.Props
	send := func(ev map[string]any) {
		if sf.Closed {
			return
		}
		ev["sf"], ev["id"] = sf.ID, n.ID
		a.tspSend(p, ev)
	}
	act := func(act, value string) {
		ev := map[string]any{"ev": "action", "act": act}
		if value != "" {
			ev["value"] = value
		}
		send(ev)
	}
	back := ui.Box(c.Key("tsp-back-" + n.ID)).Absolute().Fill().Background(k.backdrop)
	if back.Clicked() {
		a.surfacePressed(t, p)
		act("close", "")
	}
	size, _ := pr["size"].(string)
	width := map[string]float32{"md": 64, "lg": 82, "screen": 98}[size]
	if width == 0 {
		width = 82
	}
	sheet := ui.Column(c.Key("tsp-picker-"+n.ID)).Absolute().LeftPercent((100-width)/2).WidthPercent(width).TopPercent(4).MaxHeightPercent(92).
		Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Padding(12, 14).Gap(8).Font(tspFont).Clip()
	sheet.Children(func() {
		a.tspPickerHead(c, k, t, p, sf, n)
		if tabs, _ := pr["tabs"].([]any); len(tabs) > 0 {
			active, _ := pr["tab"].(string)
			row := ui.Row(c.Key("tabs")).Gap(4).FillWidth()
			row.Children(func() {
				for i, tb := range tabs {
					tb, _ := tb.(map[string]any)
					id, _ := tb["id"].(string)
					chip := ui.Row(c.Key(fmt.Sprintf("tab%d", i))).Gap(4).AlignItems(ui.Center).Padding(3, 10).Radius(5).Unselectable()
					if id == active {
						chip.Background(k.hover)
					}
					chip.Children(func() {
						a.tspRich(c, k, tb["label"], 12, id == active)
						if count, ok := tb["count"].(float64); ok {
							ui.Text(c, fmt.Sprint(int(count))).FontSize(11).TextColor(k.textMuted)
						}
					})
					if chip.Clicked() {
						act("tab", id)
					}
				}
			})
		}
		mid := ui.Row(c.Key("mid")).Gap(10).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch)
		mid.Children(func() {
			if scopes, _ := pr["scopes"].([]any); len(scopes) > 0 {
				a.tspPickerScopes(c, k, pr, scopes, act)
			}
			main := ui.Column(c.Key("main")).Gap(6).Grow(1).MinHeight(0).MinWidth(0)
			main.Children(func() {
				a.tspPickerList(c, k, t, p, sf, n, send, act)
				if preview, _ := pr["preview"].(string); preview == "below" && len(n.Children) > 0 {
					a.tspPickerPreview(c, k, t, p, sf, n)
				}
			})
			if preview, _ := pr["preview"].(string); (preview == "side" || preview == "") && len(n.Children) > 0 {
				side := ui.Scroll(c.Key("side")).WidthPercent(40).Padding(0, 0, 0, 10).Border(0, k.panelBorder)
				side.Children(func() {
					for _, ch := range n.Children {
						a.tspNode(c, k, t, p, sf, ch)
					}
				})
			}
		})
		if cf, ok := pr["confirm"].(map[string]any); ok {
			strip := ui.Row(c.Key("confirm")).Gap(8).AlignItems(ui.Center).FillWidth().Padding(6, 10).Radius(6).Background(k.attention.Alpha(0.15))
			strip.Children(func() {
				a.tspRich(c, k, cf["text"], 12, false).Grow(1)
				label, _ := cf["label"].(string)
				if label == "" {
					label = "Confirm"
				}
				btn := ui.Text(c, label).FontSize(12).TextColor(k.text).Padding(2, 10).Radius(5).Background(k.attention.Alpha(0.3)).Unselectable()
				if btn.Clicked() {
					what, _ := cf["act"].(string)
					act(what, "")
				}
			})
		}
		if st, ok := pr["strip"].(map[string]any); ok {
			items, _ := st["items"].([]any)
			selected, _ := st["selected"].(string)
			row := ui.Row(c.Key("strip")).Gap(6).AlignItems(ui.Center).FillWidth().Wrap()
			row.Children(func() {
				if l := st["label"]; l != nil {
					a.tspRich(c, k, l, 12, false).TextColor(k.textMuted)
				}
				for i, it := range items {
					it, _ := it.(map[string]any)
					id, _ := it["id"].(string)
					on, _ := it["on"].(bool)
					chip := ui.Row(c.Key(fmt.Sprintf("strip%d", i))).Gap(4).Padding(2, 8).Radius(10).Unselectable()
					chip.Background(k.hover)
					if on || id == selected {
						chip.Border(1, k.busy)
					}
					chip.Children(func() { a.tspRich(c, k, it["label"], 12, on) })
					if chip.Clicked() {
						act("strip", fmt.Sprint(i))
					}
				}
			})
		}
		if actions, _ := pr["actions"].([]any); len(actions) > 0 {
			var start, end []any
			for _, ac := range actions {
				if m, _ := ac.(map[string]any); m["end"] == true {
					end = append(end, ac)
				} else {
					start = append(start, ac)
				}
			}
			bar := ui.Row(c.Key("bar")).Gap(6).AlignItems(ui.Center).FillWidth()
			bar.Children(func() {
				a.tspActions(c.Key("start"), k, p, sf, n, start)
				ui.Box(c).Grow(1)
				a.tspActions(c.Key("end"), k, p, sf, n, end)
			})
		}
	})
	if sheet.Pressed() {
		a.surfacePressed(t, p)
	}
}

// tspPickerHead draws a picker's head: icon, title and subtitle, the
// count of rows, and the search field.
func (a *App) tspPickerHead(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	pr := n.Props
	head := ui.Row(c.Key("head")).Gap(8).AlignItems(ui.Center).FillWidth()
	head.Children(func() {
		if icon, _ := pr["icon"].(string); icon != "" {
			a.tspIcon(c, k, icon)
		}
		if title, _ := pr["title"].(string); title != "" {
			ui.Text(c, title).FontSize(14).Bold().TextColor(k.text)
		}
		if sub := pr["subtitle"]; sub != nil {
			a.tspRich(c, k, sub, 12, false).TextColor(k.textMuted).SingleLine().Shrink(1)
		}
		ui.Box(c).Grow(1)
		order := tspPickerOrder(pr)
		shown := 0
		for _, o := range order {
			if _, ok := o.(string); ok {
				shown++
			}
		}
		noun, _ := pr["noun"].(string)
		if total, ok := pr["total"].(float64); ok && int(total) != shown {
			ui.Text(c, fmt.Sprintf("%d of %d %s", shown, int(total), noun)).FontSize(11).TextColor(k.textMuted)
		} else if shown > 0 {
			ui.Text(c, strings.TrimSpace(fmt.Sprintf("%d %s", shown, noun))).FontSize(11).TextColor(k.textMuted)
		}
	})
	if q, ok := pr["query"]; ok && q != nil {
		query, _ := q.(string)
		props := map[string]any{"text": query}
		if cur, ok := pr["cursor"]; ok {
			props["cursor"] = cur
		}
		if ph, _ := pr["placeholder"].(string); ph != "" {
			props["placeholder"] = ph
		}
		field := &rex.TSPNode{ID: n.ID + "#query", Kind: "input", Props: props}
		row := ui.Row(c.Key("query")).Gap(6).AlignItems(ui.Center).FillWidth()
		row.Children(func() {
			ui.Icon(c, icon("search")).Size(13, 13).TextColor(k.textMuted)
			a.tspField(c, c.Key("query-field"), k, t, p, sf, field, false).AutoFocus()
		})
	}
}

// tspPickerOrder is what a picker shows, in order: item ids and group
// headers; its items in their order when it says none.
func tspPickerOrder(pr map[string]any) []any {
	if order, ok := pr["order"].([]any); ok {
		return order
	}
	items, _ := pr["items"].([]any)
	out := make([]any, 0, len(items))
	for _, it := range items {
		if m, _ := it.(map[string]any); m != nil {
			if id, _ := m["id"].(string); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// tspPickerList draws a picker's rows: group headers, then rows with
// their lead, label (hits marked), detail, badges and chips, and their
// facts by column; the selected row highlighted and kept in view.
func (a *App) tspPickerList(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, send func(map[string]any), act func(string, string)) {
	pr := n.Props
	items := map[string]map[string]any{}
	all, _ := pr["items"].([]any)
	for _, it := range all {
		if m, _ := it.(map[string]any); m != nil {
			if id, _ := m["id"].(string); id != "" {
				items[id] = m
			}
		}
	}
	order := tspPickerOrder(pr)
	selected, _ := pr["selected"].(string)
	current := map[string]bool{}
	if cur, _ := pr["current"].([]any); cur != nil {
		for _, v := range cur {
			if s, ok := v.(string); ok {
				current[s] = true
			}
		}
	}
	hits, _ := pr["hits"].(map[string]any)
	columns, _ := pr["columns"].([]any)
	layout, _ := pr["layout"].(string)
	state, _ := pr["state"].(string)
	// Column heads over the facts, when the columns have them.
	headed := false
	for _, col := range columns {
		if m, _ := col.(map[string]any); m["head"] != nil {
			headed = true
		}
	}
	if headed {
		hr := ui.Row(c.Key("heads")).Gap(8).FillWidth().Padding(0, 6)
		hr.Children(func() {
			ui.Box(c).Grow(1)
			for i, col := range columns {
				col, _ := col.(map[string]any)
				head, _ := col["head"].(string)
				minCh, _ := col["min"].(float64)
				ui.Text(c.Key(fmt.Sprintf("h%d", i)), head).FontSize(10).TextColor(k.textMuted).MinWidth(float32(max(minCh, 4)) * 7).TextAlign(ui.End).Shrink(0)
			}
		})
	}
	list := ui.Scroll(c.Key("list")).FillWidth().Grow(1).MinHeight(60)
	list.Children(func() {
		col := ui.Column(c).Gap(1).FillWidth()
		col.Children(func() {
			switch state {
			case "loading":
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					activityDot(c, k.busy, 8)
					ui.Text(c, "Loading…").FontSize(12).TextColor(k.textMuted)
				})
			case "error":
				a.tspRich(c, k, pr["message"], 12, false).TextColor(k.attention)
			}
			rows := 0
			for i, o := range order {
				if g, ok := o.(map[string]any); ok {
					h := ui.Row(c.Key(fmt.Sprintf("g%d", i))).Gap(6).Padding(6, 6, 2, 6).AlignItems(ui.Baseline)
					h.Children(func() {
						a.tspRich(c, k, g["label"], 11, true).TextColor(k.textMuted)
						if count, ok := g["count"].(float64); ok {
							ui.Text(c, fmt.Sprint(int(count))).FontSize(11).TextColor(k.textMuted)
						}
					})
					continue
				}
				id, _ := o.(string)
				it := items[id]
				if it == nil {
					continue
				}
				rows++
				var itemHits []any
				if hits != nil {
					itemHits, _ = hits[id].([]any)
				}
				if h, ok := it["hits"].([]any); ok && itemHits == nil {
					itemHits = h
				}
				row := a.tspPickerRow(c.Key("r-"+id), k, it, layout, columns, itemHits, id == selected, current[id])
				if clicks := row.Clicks(); clicks > 0 {
					if disabled := it["disabled"]; disabled == nil || disabled == false {
						ev := "select"
						if clicks > 1 || id == selected {
							ev = "activate"
						}
						a.surfacePressed(t, p)
						send(map[string]any{"ev": ev, "item": id})
					}
				}
			}
			if rows == 0 && state != "loading" {
				empty := pr["empty"]
				if empty == nil {
					empty = "Nothing here"
				}
				a.tspRich(c, k, empty, 12, false).TextColor(k.textMuted).Padding(8, 6)
			}
		})
	})
}

// tspPickerRow draws one row of a picker.
func (a *App) tspPickerRow(c *ui.Context, k *colors, it map[string]any, layout string, columns []any, hits []any, selected, current bool) ui.Element {
	depth, _ := it["depth"].(float64)
	row := ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Padding(3, 6, 3, 6+float32(depth)*14).Radius(5).Unselectable()
	if selected {
		row.Background(k.hover).ScrollIntoView()
	}
	if d := it["disabled"]; d != nil && d != false {
		row.Opacity(0.5)
		if why, ok := d.(string); ok {
			row.Tooltip(why)
		}
	} else if title, _ := it["title"].(string); title != "" {
		row.Tooltip(title)
	}
	row.Children(func() {
		if open, ok := it["open"].(bool); ok {
			glyph := "▸"
			if open {
				glyph = "▾"
			}
			ui.Text(c, glyph).FontSize(11).TextColor(k.textMuted)
		}
		switch {
		case it["dot"] != nil:
			tone, _ := it["dot"].(string)
			color := k.textMuted
			switch tone {
			case "success", "accent", "info":
				color = k.busy
			case "warning", "error":
				color = k.attention
			}
			ui.Box(c).Width(8).Height(8).Radius(4).Background(color).Shrink(0)
		case it["icon"] != nil:
			name, _ := it["icon"].(string)
			a.tspIcon(c, k, name)
		case it["mark"] != nil:
			m, _ := it["mark"].(map[string]any)
			text, _ := m["text"].(string)
			initials := strings.ToUpper(text)
			if r := []rune(initials); len(r) > 2 {
				initials = string(r[:2])
			}
			ui.Text(c, initials).FontSize(10).Bold().TextColor(k.text).Width(20).Height(20).Radius(10).Background(k.busy.Alpha(0.35)).TextAlign(ui.Center).Shrink(0)
		}
		text := ui.Column(c).Gap(1).Grow(1).MinWidth(0)
		text.Children(func() {
			line := ui.Row(c).Gap(6).AlignItems(ui.Baseline).MinWidth(0)
			line.Children(func() {
				mono, _ := it["mono"].(bool)
				a.tspPickerLabel(c, k, it["label"], mono, hits).SingleLine().Shrink(1)
				if current {
					ui.Text(c, "●").FontSize(10).TextColor(k.busy)
				}
				if d := it["detail"]; d != nil && layout != "cards" {
					a.tspRich(c, k, d, 12, false).TextColor(k.textMuted).SingleLine().Shrink(1)
				}
				a.tspChips(c, k, it["badges"])
				if chips, _ := it["chips"].([]any); len(chips) > 0 {
					for i, ch := range chips {
						ch, _ := ch.(map[string]any)
						t, _ := ch["text"].(string)
						on, _ := ch["on"].(bool)
						chip := ui.Text(c.Key(fmt.Sprintf("rc%d", i)), t).FontSize(11).Padding(1, 6).Radius(8).Background(k.hover)
						if on {
							chip.TextColor(k.busy)
						} else {
							chip.TextColor(k.textMuted)
						}
					}
				}
			})
			if d := it["detail"]; d != nil && layout == "cards" {
				a.tspRich(c, k, d, 12, false).TextColor(k.textMuted)
			}
		})
		facts, _ := it["facts"].(map[string]any)
		for i, col := range columns {
			col, _ := col.(map[string]any)
			id, _ := col["id"].(string)
			format, _ := col["format"].(string)
			minCh, _ := col["min"].(float64)
			cell := ui.Box(c.Key(fmt.Sprintf("f%d", i))).MinWidth(float32(max(minCh, 4)) * 7).AlignItems(ui.End).Shrink(0)
			cell.Children(func() {
				v := facts[id]
				switch v := v.(type) {
				case float64:
					if format == "bar" {
						track := ui.Box(c).Height(5).Radius(3).Background(k.track).Width(48).Clip()
						track.Children(func() {
							ui.Box(c).WidthPercent(float32(max(0, min(1, v)) * 100)).Height(5).Background(k.busy)
						})
						return
					}
					s := compactNum(v)
					if format == "price" {
						s = fmt.Sprintf("$%.2f", v)
					} else if format == "elapsed" || format == "time" {
						s = elapsedText(v / 1000)
					}
					ui.Text(c, s).FontSize(11).TextColor(k.textMuted)
				case nil:
				default:
					t := a.tspRich(c, k, v, 11, false)
					if format != "text" {
						t.TextColor(k.textMuted)
					}
				}
			})
		}
	})
	return row
}

// tspPickerLabel draws a row's label with its search hits marked (UTF-16
// ranges); a mono label has its prefix up to the last / dim and its
// tail strong.
func (a *App) tspPickerLabel(c *ui.Context, k *colors, v any, mono bool, hits []any) ui.Element {
	s, ok := v.(string)
	if !ok {
		return a.tspRich(c, k, v, 13, false)
	}
	font := ""
	if mono {
		font = a.editorFont().Family
	}
	cut := 0
	if mono {
		if i := strings.LastIndex(s, "/"); i >= 0 {
			cut = utf16Len(s[:i+1])
		}
	}
	marked := make([]bool, utf16Len(s))
	for _, h := range hits {
		r, _ := h.([]any)
		if len(r) != 2 {
			continue
		}
		from, _ := r[0].(float64)
		to, _ := r[1].(float64)
		for i := max(0, int(from)); i < min(len(marked), int(to)); i++ {
			marked[i] = true
		}
	}
	var spans []ui.Span
	units := utf16.Encode([]rune(s))
	start := 0
	style := func(i int) (bool, bool) { return marked[i], i < cut }
	for i := 1; i <= len(units); i++ {
		if i < len(units) {
			m0, d0 := style(start)
			m1, d1 := style(i)
			if m0 == m1 && d0 == d1 {
				continue
			}
		}
		m, dim := style(start)
		sp := ui.Span{Text: string(utf16.Decode(units[start:i])), Size: 13, Font: font, Color: k.text}
		if dim {
			sp.Color = k.textMuted
		} else if mono {
			sp.Weight = 600
		}
		if m {
			sp.Background, sp.Color = k.busy.Alpha(0.3), k.text
		}
		spans = append(spans, sp)
		start = i
	}
	if len(spans) == 0 {
		return ui.Text(c, "").FontSize(13)
	}
	return ui.RichText(c, spans...)
}

// tspPickerScopes draws a picker's scope column: entries under their
// group heads, the active one highlighted; a click sends scope.
func (a *App) tspPickerScopes(c *ui.Context, k *colors, pr map[string]any, scopes []any, act func(string, string)) {
	active, _ := pr["scope"].(string)
	col := ui.Scroll(c.Key("scopes")).Width(180).Shrink(0)
	col.Children(func() {
		inner := ui.Column(c).Gap(1)
		inner.Children(func() {
			group := ""
			for i, sc := range scopes {
				sc, _ := sc.(map[string]any)
				id, _ := sc["id"].(string)
				if g, _ := sc["group"].(string); g != "" && g != group {
					group = g
					ui.Text(c.Key(fmt.Sprintf("sg%d", i)), g).FontSize(11).Bold().TextColor(k.textMuted).Padding(6, 6, 2, 6)
				}
				row := ui.Row(c.Key(fmt.Sprintf("sc%d", i))).Gap(6).AlignItems(ui.Center).Padding(3, 6).Radius(5).Unselectable()
				if id == active {
					row.Background(k.hover)
				}
				row.Children(func() {
					if name, _ := sc["icon"].(string); name != "" {
						a.tspIcon(c, k, name)
					}
					a.tspRich(c, k, sc["label"], 12, id == active).Grow(1).SingleLine()
					if count, ok := sc["count"].(float64); ok {
						ui.Text(c, fmt.Sprint(int(count))).FontSize(11).TextColor(k.textMuted)
					}
				})
				if row.Clicked() {
					act("scope", id)
				}
			}
		})
	})
}

// tspPickerPreview draws the selected row's preview under the list.
func (a *App) tspPickerPreview(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	pv := ui.Column(c.Key("preview")).Gap(6).FillWidth().Padding(8, 2, 0, 2)
	pv.Children(func() {
		ui.Box(c).Height(1).FillWidth().Background(k.panelBorder)
		for _, ch := range n.Children {
			a.tspNode(c, k, t, p, sf, ch)
		}
	})
}

// compactNum writes a number short: 272K, 1M, 1.5M, 42, 0.5.
func compactNum(v float64) string {
	trim := func(f float64, unit string) string {
		s := strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", f), "0"), ".")
		return s + unit
	}
	switch a := max(v, -v); {
	case a >= 1e9:
		return trim(v/1e9, "B")
	case a >= 1e6:
		return trim(v/1e6, "M")
	case a >= 1e4:
		return trim(v/1e3, "K")
	}
	return trim(v, "")
}
