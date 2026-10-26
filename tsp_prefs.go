package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A program's settings (kind prefs) draw from its data: its pages in a
// nav, the current page's sections of rows, each row a typed control
// (switch, choice, number, text, keys, multi, action). The program owns
// every value and every key: keys go to it through the terminal, and
// its search text, the row it has the focus on and the editor it has
// open come back as props; each pointer action goes to it as an event
// (change with a typed value, activate, select, action), which it
// answers with new props. In the layer region (omp puts its /settings
// there over a live session, as the hello's aside feature lets it) it is
// a sheet docked at the pane's right edge, and the inline surface beside
// it is narrowed to leave it room, where the pane has room for both
// (tspAside); without the room the sheet lies over the transcript's
// right part. Anywhere else it is a page in place.

const (
	// tspPrefsSheetW is a sheet's width; tspPrefsRoom the least width
	// the transcript keeps beside it, else the sheet lies over it.
	tspPrefsSheetW = 440
	tspPrefsRoom   = 360
	// The roles of a prefs node's children: the status-line preview,
	// after the status-line section, and an editor without a native
	// control, in a card over the page.
	tspPrefsStatusRole = "omp.prefs.preview.status"
	tspPrefsEditorRole = "omp.prefs.editor"
)

// tspPrefsView is a prefs node as one frame draws it.
type tspPrefsView struct {
	a      *App
	k      *colors
	t      *Tab
	p      *Pane
	sf     *rex.Surface
	n      *rex.TSPNode
	sheet  bool
	pages  []map[string]any
	page   string
	query  string // trimmed: non-empty shows search results
	focus  string
	edit   map[string]any // the program's open editor, nil for none
	status []*rex.TSPNode // the status-line preview
	editor []*rex.TSPNode // editors in a card over the page
	extra  []*rex.TSPNode // the rest, after the sections
	picker []*rex.TSPNode // pickers, which are sheets of their own
	// handled tells that a row's control took the click this frame,
	// which then does not select the row.
	handled bool
}

// tspLayer is a surface's layer region, nil for none.
func tspLayer(sf *rex.Surface) *rex.TSPNode {
	if sf.Root == nil {
		return nil
	}
	for _, r := range sf.Root.Children {
		if r.ID == "layer" {
			return r
		}
	}
	return nil
}

// tspPrefsQueued takes a prefs node of the layer region as a sheet, which
// surfaceOverlays draws over the surface after it (a live surface's
// only); it reports whether n was one.
func (a *App) tspPrefsQueued(sf *rex.Surface, n *rex.TSPNode) bool {
	layer := tspLayer(sf)
	if layer == nil || !slices.Contains(layer.Children, n) {
		return false
	}
	if !sf.Closed {
		a.tspOverlays = append(a.tspOverlays, n)
	}
	return true
}

// tspAside is how far an inline surface's right edge, placed at pl,
// keeps from the pane's: the strip of the terminal's scroll bar, which
// stays in sight and in reach beside every surface, and for a live one
// the prefs sheet docked beside it, with its margins, where the
// transcript keeps tspPrefsRoom beside it; else the sheet lies over the
// transcript. The pane is as wide as the terminal's columns from the
// view's left: the anchor's placement is one cell wide, which gives a
// cell's width.
func tspAside(p *Pane, sf *rex.Surface, pl terminal.AppPlacement) float32 {
	strip := p.term.ScrollbarStrip()
	cols, _ := p.term.Size()
	cell := pl.Rect.W / float32(max(pl.Cols, 1))
	width := pl.Rect.X + float32(cols-pl.Col)*cell
	if sf.Closed || width < tspPrefsSheetW+16+tspPrefsRoom+strip {
		return strip
	}
	layer := tspLayer(sf)
	if layer == nil {
		return strip
	}
	for _, n := range layer.Children {
		if hidden, _ := n.Props["hidden"].(bool); n.Kind == "prefs" && !hidden {
			return strip + tspPrefsSheetW + 16
		}
	}
	return strip
}

// newPrefsView reads a prefs node's props and sorts its children by role.
func (a *App) newPrefsView(k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, sheet bool) *tspPrefsView {
	pr := n.Props
	v := &tspPrefsView{a: a, k: k, t: t, p: p, sf: sf, n: n, sheet: sheet}
	pages, _ := pr["pages"].([]any)
	for _, pg := range pages {
		if m, _ := pg.(map[string]any); m != nil {
			v.pages = append(v.pages, m)
		}
	}
	v.page, _ = pr["page"].(string)
	q, _ := pr["query"].(string)
	v.query = strings.TrimSpace(q)
	v.focus, _ = pr["focus"].(string)
	v.edit, _ = pr["editing"].(map[string]any)
	for _, ch := range n.Children {
		role, _ := ch.Props["role"].(string)
		switch {
		case ch.Kind == "picker":
			v.picker = append(v.picker, ch)
		case role == tspPrefsEditorRole:
			v.editor = append(v.editor, ch)
		case role == tspPrefsStatusRole:
			v.status = append(v.status, ch)
		default:
			v.extra = append(v.extra, ch)
		}
	}
	return v
}

// send sends the program an event of the prefs node; the keys go back
// to the terminal, as after any click on a surface.
func (v *tspPrefsView) send(ev map[string]any) {
	v.handled = true
	if v.sf.Closed {
		return
	}
	v.a.surfacePressed(v.t, v.p)
	ev["sf"], ev["id"] = v.sf.ID, v.n.ID
	v.a.tspSend(v.p, ev)
}

// act sends an action, with its value when it has one.
func (v *tspPrefsView) act(act, value string) {
	ev := map[string]any{"ev": "action", "act": act}
	if value != "" {
		ev["value"] = value
	}
	v.send(ev)
}

// change sends a row's new value; nil resets it to its default.
func (v *tspPrefsView) change(row string, value any) {
	v.send(map[string]any{"ev": "change", "item": row, "value": value})
}

// editing reports whether the program has row's editor open.
func (v *tspPrefsView) editing(row string) bool {
	r, _ := v.edit["row"].(string)
	return v.edit != nil && r == row
}

// tspReveal scrolls el into view on the frame it becomes the target
// (the page, focus or option the program points at), not on every
// frame it stays one, which would pin the scroll on it.
func tspReveal(el ui.Element, now bool) {
	was := ui.Local(el, "tspReveal", func() bool { return false })
	if was == nil {
		return
	}
	if now && !*was {
		el.ScrollIntoView()
	}
	*was = now
}

// tspPrefsSheet draws a prefs node of the layer region: a sheet docked at
// the pane's right edge, from its top to the dock pinned at its bottom,
// with the title and a close button, the search, the pages as a rail
// and the document; pickers among its children draw as their sheets.
func (a *App) tspPrefsSheet(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	v := a.newPrefsView(k, t, p, sf, n, true)
	bottom := float32(8)
	if sf.Mode != "screen" && tspDocked(sf) {
		bottom += p.dockH[sf.Image] + 8
	}
	sheet := ui.Column(c.Key("tsp-prefs-"+n.ID)).Absolute().Top(8).Right(p.term.ScrollbarStrip()+8).Bottom(bottom).Width(tspPrefsSheetW).MaxWidthPercent(92).
		Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Padding(10, 12).Gap(8).Font(tspFont).Clip()
	sheet.Children(func() {
		head := ui.Row(c.Key("brand")).Gap(8).AlignItems(ui.Center).FillWidth()
		head.Children(func() {
			ui.Icon(c, icon("settings-2")).Size(14, 14).TextColor(k.textMuted)
			v.title(c).Grow(1)
			if iconButton(c, k, "x", "Close", 22, 13).Clicked() {
				v.act("close", "")
			}
		})
		v.search(c).FillWidth()
		body := ui.Row(c.Key("body")).Gap(8).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch)
		body.Children(func() {
			v.rail(c, true)
			doc := ui.Scroll(c.Key("doc-" + v.page + "|" + v.query)).Grow(1).MinWidth(0)
			doc.Children(func() { v.doc(c) })
		})
		v.editorCard(c)
	})
	if sheet.Pressed() {
		a.surfacePressed(t, p)
	}
	for _, pk := range v.picker {
		a.tspPicker(c, k, t, p, sf, pk)
	}
}

// tspPrefs draws a prefs node anywhere but the layer region: a page in
// place, the title, search and Close over the pages' nav and the
// document; the nav is a rail of icons where the page is narrow.
func (a *App) tspPrefs(c *ui.Context, key *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) ui.Element {
	v := a.newPrefsView(k, t, p, sf, n, false)
	page := ui.Column(key).Gap(10).FillWidth()
	w := page.Bounds().W
	page.Children(func() {
		head := ui.Row(c.Key("top")).Gap(10).AlignItems(ui.Center).FillWidth()
		head.Children(func() {
			ui.Icon(c, icon("settings-2")).Size(15, 15).TextColor(k.textMuted)
			v.title(c)
			v.search(c).Grow(1).MaxWidth(360)
			ui.Box(c).Grow(1)
			if !sf.Closed {
				if textButton(c.Key("close"), k, "Close").Clicked() {
					v.act("close", "")
				}
			}
		})
		body := ui.Row(c.Key("body")).Gap(14).FillWidth().AlignItems(ui.Start)
		body.Children(func() {
			v.rail(c, w > 0 && w < 760)
			doc := ui.Column(c.Key("doc")).Grow(1).MinWidth(0)
			doc.Children(func() { v.doc(c) })
		})
		v.editorCard(c)
	})
	// A picker over the page is a sheet of the surface's, drawn after it.
	for _, pk := range v.picker {
		a.tspNode(c, k, t, p, sf, pk)
	}
	return page
}

// title draws the settings' title.
func (v *tspPrefsView) title(c *ui.Context) ui.Element {
	title, _ := v.n.Props["title"].(string)
	if title == "" {
		title = "Settings"
	}
	return ui.Text(c, title).FontSize(14).Bold().TextColor(v.k.text).SingleLine()
}

// search draws the program's search field: its text, with the caret
// where the program has it while its field has the keys. The keys are
// the program's: they reach it through the terminal, which a click on
// the field gives them to.
func (v *tspPrefsView) search(c *ui.Context) ui.Element {
	k := v.k
	q, _ := v.n.Props["query"].(string)
	at, caret := v.n.Props["cursor"].(float64)
	box := ui.Row(c.Key("search")).Gap(6).AlignItems(ui.Center).Padding(4, 8).Radius(6).Background(k.bgLow).Border(1, k.cardBorder).Unselectable()
	if caret {
		box.Border(1, k.busy)
	}
	box.Children(func() {
		ui.Icon(c, icon("search")).Size(12, 12).TextColor(k.textMuted)
		switch {
		case caret:
			pre, post := tspSplitAt(q, int(at))
			v.caretText(c, pre, post, "Search settings", "")
		case v.query != "":
			ui.Text(c, v.query).FontSize(12).TextColor(k.text).SingleLine()
		default:
			ui.Text(c, "Search settings").FontSize(12).TextColor(k.textMuted).SingleLine()
		}
	})
	if box.Clicked() {
		v.a.surfacePressed(v.t, v.p)
	}
	return box
}

// caretText draws a text with a caret between pre and post, the
// placeholder after it when both are empty, in font (the system's for
// "").
func (v *tspPrefsView) caretText(c *ui.Context, pre, post, placeholder, font string) {
	k := v.k
	row := ui.Row(c.Key("caret")).AlignItems(ui.Center).MinWidth(0)
	row.Children(func() {
		text := func(s string, color ui.Color) {
			t := ui.Text(c, s).FontSize(12).TextColor(color).NoWrap()
			if font != "" {
				t.Font(font)
			}
		}
		if pre != "" {
			text(pre, k.text)
		}
		ui.Box(c).Width(1.5).Height(14).Background(k.busy).Shrink(0)
		if post != "" {
			text(post, k.text)
		} else if pre == "" && placeholder != "" {
			text(placeholder, k.textMuted)
		}
	})
}

// rail draws the pages' nav: a row for each page under its group's head,
// the current one highlighted, its count of settings changed from their
// defaults; a disabled one dimmed, its reason its tooltip, taking no
// clicks. A compact rail shows each page's icon over its short label.
func (v *tspPrefsView) rail(c *ui.Context, compact bool) {
	k := v.k
	width := float32(190)
	if compact {
		width = 64
	}
	var nav ui.Element
	if v.sheet {
		nav = ui.Scroll(c.Key("nav")).Width(width).Shrink(0)
	} else {
		nav = ui.Column(c.Key("nav")).Width(width).Shrink(0)
	}
	nav.Children(func() {
		inner := ui.Column(c).Gap(1).FillWidth()
		inner.Children(func() {
			group := ""
			for i, pg := range v.pages {
				id, _ := pg["id"].(string)
				label, _ := pg["label"].(string)
				if label == "" {
					label = id
				}
				if g, _ := pg["group"].(string); g != group {
					group = g
					if compact {
						ui.Box(c.Key(fmt.Sprintf("gs%d", i))).Height(1).FillWidth().Background(k.panelBorder).Margin(4, 6)
					} else if g != "" {
						ui.Text(c.Key(fmt.Sprintf("g%d", i)), g).FontSize(11).Bold().TextColor(k.textMuted).Padding(8, 8, 2, 8)
					}
				}
				changed, _ := pg["changed"].(float64)
				why, _ := pg["disabled"].(string)
				name, _ := pg["icon"].(string)
				if name == "" {
					name = "settings"
				}
				var row ui.Element
				if compact {
					row = ui.Column(c.Key("p-"+id)).Gap(2).AlignItems(ui.Center).Padding(5, 2).Radius(6).Unselectable()
				} else {
					row = ui.Row(c.Key("p-"+id)).Gap(7).AlignItems(ui.Center).Padding(4, 8).Radius(6).Unselectable()
				}
				if id == v.page {
					row.Background(k.hover)
				}
				if !v.sheet {
					tspReveal(row, id == v.page)
				}
				tip := label
				if changed > 0 {
					tip += fmt.Sprintf(" · %d changed from default", int(changed))
				}
				if why != "" {
					row.Opacity(0.45)
					tip = why
				}
				row.Tooltip(tip)
				row.Children(func() {
					if compact {
						ui.Box(c).Children(func() {
							v.a.tspIcon(c, k, name)
							if changed > 0 {
								ui.Box(c).Absolute().Top(-2).Right(-5).Size(6, 6).Radius(3).Background(k.busy)
							}
						})
						ui.Text(c, label).FontSize(10).TextColor(k.textMuted).SingleLine().MaxWidth(width - 6)
						return
					}
					v.a.tspIcon(c, k, name)
					ui.Text(c, label).FontSize(12.5).TextColor(k.text).SingleLine().Grow(1).MinWidth(0)
					if changed > 0 {
						ui.Text(c, strconv.Itoa(int(changed))).FontSize(11).TextColor(k.busy)
					}
				})
				if row.Clicked() && why == "" {
					v.act("page", id)
				}
			}
		})
	})
}

// pageOf is the page id names, nil for none.
func (v *tspPrefsView) pageOf(id string) map[string]any {
	for _, pg := range v.pages {
		if pid, _ := pg["id"].(string); pid == id {
			return pg
		}
	}
	return nil
}

// doc draws the document: the page's head (its number and label, its
// lead) and its sections, or with a search the results, grouped under
// the heads of their pages; then the children after the sections.
func (v *tspPrefsView) doc(c *ui.Context) {
	k := v.k
	sections, _ := v.n.Props["sections"].([]any)
	col := ui.Column(c.Key("sections")).Gap(14).FillWidth().Padding(2, 4, 8, 2)
	col.Children(func() {
		head := ui.Column(c.Key("head")).Gap(3).FillWidth()
		head.Children(func() {
			if v.query != "" {
				ui.Text(c, "Search").FontSize(11).TextColor(k.textMuted)
				ui.Text(c, "Results for “"+v.query+"”").FontSize(17).Bold().TextColor(k.text)
				return
			}
			label, num := v.page, 0
			for i, pg := range v.pages {
				if id, _ := pg["id"].(string); id == v.page {
					num = i + 1
					if l, _ := pg["label"].(string); l != "" {
						label = l
					}
				}
			}
			if num > 0 {
				ui.Text(c, fmt.Sprintf("%02d · %s", num, label)).FontSize(11).TextColor(k.textMuted).Font(v.a.editorFont().Family)
			}
			ui.Text(c, label).FontSize(17).Bold().TextColor(k.text)
			if lead, _ := v.n.Props["lead"].(string); lead != "" {
				ui.Text(c, lead).FontSize(12.5).TextColor(k.textMuted)
			}
		})
		shown, page := 0, ""
		for i, sec := range sections {
			sec, _ := sec.(map[string]any)
			rows, _ := sec["rows"].([]any)
			id, _ := sec["id"].(string)
			if len(rows) == 0 && id != "status-line" {
				continue
			}
			if pid, _ := sec["page"].(string); v.query != "" && pid != page {
				page = pid
				v.pageHead(c.Key(fmt.Sprintf("ph%d", i)), pid)
			}
			v.section(c.Key(fmt.Sprintf("s%d-%s", i, id)), sec, rows)
			shown += len(rows)
			if id == "status-line" {
				for _, ch := range v.status {
					v.a.tspNode(c, k, v.t, v.p, v.sf, ch)
				}
			}
		}
		if shown == 0 {
			text := "Nothing to set here"
			if v.query != "" {
				text = "No settings match “" + v.query + "”"
			}
			ui.Text(c.Key("none"), text).FontSize(12.5).TextColor(k.textMuted).Padding(10, 2)
		}
		for _, ch := range v.extra {
			v.a.tspNode(c, k, v.t, v.p, v.sf, ch)
		}
	})
}

// pageHead draws the head of a page's search results: a click opens the
// page, unless it is disabled.
func (v *tspPrefsView) pageHead(c *ui.Context, id string) {
	k := v.k
	pg := v.pageOf(id)
	label, _ := pg["label"].(string)
	if label == "" {
		label = id
	}
	why, _ := pg["disabled"].(string)
	h := ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(2, 2).Unselectable()
	if why != "" {
		h.Opacity(0.45).Tooltip(why)
	} else {
		h.Cursor(ui.CursorPointer)
	}
	h.Children(func() {
		name, _ := pg["icon"].(string)
		if name == "" {
			name = "settings"
		}
		v.a.tspIcon(c, k, name)
		ui.Text(c, label).FontSize(13).Bold().TextColor(k.text)
		ui.Text(c, "›").FontSize(13).TextColor(k.textMuted)
	})
	if h.Clicked() && why == "" {
		v.act("page", id)
	}
}

// section draws a section: its title over a card of its rows, ringed
// while the program has the focus on it.
func (v *tspPrefsView) section(c *ui.Context, sec map[string]any, rows []any) {
	k := v.k
	id, _ := sec["id"].(string)
	col := ui.Column(c).Gap(5).FillWidth()
	col.Children(func() {
		if title, _ := sec["title"].(string); title != "" {
			ui.Text(c, title).FontSize(11.5).Bold().TextColor(k.textMuted).Padding(0, 2)
		}
		if len(rows) == 0 {
			return
		}
		card := ui.Column(c.Key("rows")).FillWidth().Radius(8).Background(k.card).Border(1, k.cardBorder)
		if id != "" && id == v.focus {
			card.Border(1, k.busy)
		}
		tspReveal(col, id != "" && id == v.focus)
		card.Children(func() {
			for i, r := range rows {
				r, _ := r.(map[string]any)
				rid, _ := r["id"].(string)
				v.row(c.Key(fmt.Sprintf("r%d-%s", i, rid)), r, i > 0)
			}
		})
	})
}

// row draws a settings row: the changed dot (the default its tooltip),
// the label with a warning's sign, the hint, the warning and a disabled
// row's reason under it, and the control, with a Reset before it while
// changed; a multi row has its control under the label. A click off
// the control selects the row.
func (v *tspPrefsView) row(c *ui.Context, r map[string]any, rule bool) {
	k := v.k
	id, _ := r["id"].(string)
	label, _ := r["label"].(string)
	if label == "" {
		label = id
	}
	hint, _ := r["hint"].(string)
	warning, _ := r["warning"].(string)
	why, _ := r["disabled"].(string)
	changed, _ := r["changed"].(bool)
	ctl, _ := r["control"].(map[string]any)
	kind, _ := ctl["k"].(string)
	stack := kind == "multi"
	off := why != ""
	row := ui.Column(c).Gap(6).FillWidth().Padding(8, 10).Unselectable()
	if rule {
		row.BorderWidth(1, 0, 0, 0).BorderColor(k.cardBorder)
	}
	if id == v.focus || v.editing(id) {
		row.Background(k.hover)
	}
	tspReveal(row, id == v.focus)
	if off {
		row.Opacity(0.5).Tooltip(why)
	}
	v.handled = false
	row.Children(func() {
		line := ui.Row(c.Key("line")).Gap(8).AlignItems(ui.Center).FillWidth()
		line.Children(func() {
			dot := ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Shrink(0)
			if changed {
				dot.Background(k.busy)
				tip := "Changed from default"
				if d, _ := r["defaultLabel"].(string); d != "" {
					tip += ": " + d
				}
				dot.Tooltip(tip)
			}
			text := ui.Column(c.Key("label")).Gap(2).Grow(1).MinWidth(0)
			text.Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Baseline).Children(func() {
					if warning != "" {
						ui.Text(c, "⚠").FontSize(12).TextColor(k.attention)
					}
					v.marked(c, label, 13, k.text)
				})
				if hint != "" {
					v.marked(c, hint, 11.5, k.textMuted)
				}
				if warning != "" {
					ui.Text(c, warning).FontSize(11.5).TextColor(k.attention)
				}
				if off {
					ui.Text(c, why).FontSize(11.5).Italic().TextColor(k.textMuted)
				}
			})
			if !stack {
				ui.Row(c.Key("ctl")).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					v.reset(c, id, kind, changed, off)
					v.control(c, id, label, ctl, off)
				})
			}
		})
		if stack {
			ui.Row(c.Key("ctl")).Gap(6).AlignItems(ui.Start).FillWidth().Padding(0, 0, 0, 14).Children(func() {
				ui.Box(c).Grow(1).MinWidth(0).Children(func() { v.control(c, id, label, ctl, off) })
				v.reset(c, id, kind, changed, off)
			})
		}
	})
	if row.Clicked() && !v.handled && !off {
		v.send(map[string]any{"ev": "select", "item": id})
	}
}

// reset draws a changed row's Reset, which sets it to its default: not
// for actions and keys, nor a disabled row.
func (v *tspPrefsView) reset(c *ui.Context, id, kind string, changed, off bool) {
	if !changed || off || kind == "action" || kind == "keys" {
		return
	}
	b := ui.Text(c.Key("reset"), "Reset").FontSize(11).TextColor(v.k.textMuted).Padding(2, 6).Radius(5).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(v.k.hover)
	}
	if b.Clicked() {
		v.change(id, nil)
	}
}

// control draws a row's control by its kind; a disabled row's takes no
// clicks.
func (v *tspPrefsView) control(c *ui.Context, id, label string, ctl map[string]any, off bool) {
	k := v.k
	switch kind, _ := ctl["k"].(string); kind {
	case "switch":
		on, _ := ctl["on"].(bool)
		sw := ui.Switch(c.Key("switch"), &on).Label(label).Disabled(off)
		if sw.Changed() {
			v.change(id, on)
		}
	case "choice":
		if tspPrefsSegmented(ctl) {
			v.segments(c, id, ctl, off)
		} else {
			v.menu(c, id, ctl, off)
		}
	case "number":
		v.stepper(c, id, ctl, off)
	case "text":
		v.textField(c, id, ctl, off)
	case "keys":
		chords, _ := ctl["keys"].([]any)
		ui.Row(c.Key("keys")).Gap(6).AlignItems(ui.Center).Children(func() {
			for i, ch := range chords {
				keys, _ := ch.([]any)
				if i > 0 {
					ui.Text(c.Key(fmt.Sprintf("or%d", i)), "/").FontSize(11).TextColor(k.textMuted)
				}
				v.a.tspKeys(c.Key(fmt.Sprintf("chord%d", i)), k, keys)
			}
		})
	case "multi":
		if ordered, _ := ctl["ordered"].(bool); ordered {
			v.ordered(c, id, ctl, off)
		} else {
			v.chips(c, id, ctl, off)
		}
	case "action":
		text, _ := ctl["label"].(string)
		if text == "" {
			text = "Edit…"
		}
		b := textButton(c.Key("action"), k, text).Border(1, k.cardBorder)
		if off {
			b.Opacity(0.6)
		}
		if b.Clicked() && !off {
			act, _ := ctl["act"].(string)
			if act == "" {
				act = "edit"
			}
			v.act(act, id)
		}
	}
}

// tspPrefsOptions are a choice's or multi's options: value, label (the
// value when none) and detail.
func tspPrefsOptions(ctl map[string]any) [][3]string {
	opts, _ := ctl["options"].([]any)
	out := make([][3]string, 0, len(opts))
	for _, o := range opts {
		m, _ := o.(map[string]any)
		value, _ := m["value"].(string)
		label, _ := m["label"].(string)
		detail, _ := m["detail"].(string)
		if label == "" {
			label = value
		}
		out = append(out, [3]string{value, label, detail})
	}
	return out
}

// tspPrefsSegmented reports whether a choice draws as segments: when it
// says so, or, left to the terminal (auto), with two to four options
// whose labels total 28 characters or fewer; else it is a popup menu.
func tspPrefsSegmented(ctl map[string]any) bool {
	switch style, _ := ctl["style"].(string); style {
	case "segmented":
		return true
	case "menu":
		return false
	}
	opts := tspPrefsOptions(ctl)
	if len(opts) < 2 || len(opts) > 4 {
		return false
	}
	total := 0
	for _, o := range opts {
		total += len([]rune(o[1]))
	}
	return total <= 28
}

// segments draws a choice as segments, the current one raised; a click
// on another chooses it.
func (v *tspPrefsView) segments(c *ui.Context, id string, ctl map[string]any, off bool) {
	k := v.k
	value, _ := ctl["value"].(string)
	mono, _ := ctl["mono"].(bool)
	track := ui.Row(c.Key("seg")).Gap(2).Padding(2).Radius(7).Background(k.track).AlignItems(ui.Center)
	track.Children(func() {
		for i, o := range tspPrefsOptions(ctl) {
			seg := ui.Box(c.Key(i)).Padding(2, 9).Radius(5).Cursor(ui.CursorPointer)
			if o[0] == value {
				seg.Background(k.card).Border(1, k.cardBorder)
			} else if seg.Hovered() && !off {
				seg.Background(k.hover)
			}
			if o[2] != "" {
				seg.Tooltip(o[2])
			}
			seg.Children(func() {
				t := ui.Text(c, o[1]).FontSize(12).TextColor(k.text).SingleLine()
				if mono {
					t.Font(v.a.editorFont().Family)
				}
			})
			if seg.Clicked() && !off && o[0] != value {
				v.change(id, o[0])
			}
		}
	})
}

// menu draws a choice as a popup button showing the current option. A
// click asks the program to open it (activate); while the program has
// it open (editing) its menu shows under it, the program's option
// highlighted: pointing at an option tells the program (select, for its
// live preview), a click on one chooses it, and a click on the button or
// anywhere off the menu closes it.
func (v *tspPrefsView) menu(c *ui.Context, id string, ctl map[string]any, off bool) {
	k := v.k
	value, _ := ctl["value"].(string)
	mono, _ := ctl["mono"].(bool)
	opts := tspPrefsOptions(ctl)
	current := value
	for _, o := range opts {
		if o[0] == value {
			current = o[1]
		}
	}
	font := ""
	if mono {
		font = v.a.editorFont().Family
	}
	open := v.editing(id)
	btn := ui.Row(c.Key("pick")).Gap(6).AlignItems(ui.Center).Padding(3, 6, 3, 9).Radius(6).Background(k.bgLow).Border(1, k.cardBorder).
		Cursor(ui.CursorPointer).MaxWidth(220)
	if open {
		btn.Border(1, k.busy)
	}
	btn.Children(func() {
		t := ui.Text(c, current).FontSize(12).TextColor(k.text).SingleLine().Shrink(1)
		if font != "" {
			t.Font(font)
		}
		ui.Icon(c, icon("chevron-down")).Size(11, 11).TextColor(k.textMuted)
	})
	if btn.Clicked() && !off {
		if open {
			v.act("close", id)
		} else {
			v.send(map[string]any{"ev": "activate", "item": id})
		}
	}
	if !open || off {
		return
	}
	highlight, _ := v.edit["option"].(string)
	if highlight == "" {
		highlight = value
	}
	pop := ui.PopoverBase(c.Key("menu"), btn, &open, func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).Radius(8).Background(k.panel).Border(1, k.panelBorder).Font(tspFont).
			MinWidth(160).MaxWidth(340).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.25))
		pointed := ui.Local(panel, "pointed", func() string { return "" })
		list := ui.Scroll(c.Key("options")).MaxHeight(300)
		list.Children(func() {
			col := ui.Column(c).Gap(1)
			col.Children(func() {
				for i, o := range opts {
					item := ui.Row(c.Key(i)).Gap(6).AlignItems(ui.Start).Padding(4, 8).Radius(5).Cursor(ui.CursorPointer).FillWidth()
					if o[0] == highlight {
						item.Background(k.hover)
					}
					tspReveal(item, o[0] == highlight)
					item.Children(func() {
						mark := " "
						if o[0] == value {
							mark = "✓"
						}
						ui.Text(c, mark).FontSize(12).TextColor(k.busy).Width(12)
						ui.Column(c).Gap(1).MinWidth(0).Children(func() {
							t := ui.Text(c, o[1]).FontSize(12.5).TextColor(k.text)
							if font != "" {
								t.Font(font)
							}
							if o[2] != "" {
								ui.Text(c, o[2]).FontSize(11).TextColor(k.textMuted)
							}
						})
					})
					if item.Hovered() && pointed != nil && *pointed != o[0] {
						*pointed = o[0]
						v.send(map[string]any{"ev": "select", "item": id + "=" + o[0]})
					}
					if item.Clicked() {
						v.change(id, o[0])
					}
				}
			})
		})
	})
	if pop.Dismissed() {
		v.act("close", id)
	}
}

// stepper draws a number as − value +: a step goes to the next of its
// labels, or by its step within its bounds, and a button with nowhere
// to go is dimmed. The value shows its label, else itself and its unit.
func (v *tspPrefsView) stepper(c *ui.Context, id string, ctl map[string]any, off bool) {
	k := v.k
	value, _ := ctl["value"].(float64)
	text, ok := tspPrefsNumberLabel(ctl, value)
	if !ok {
		text = strconv.FormatFloat(value, 'f', -1, 64)
		if unit, _ := ctl["unit"].(string); unit != "" {
			if !strings.HasPrefix(unit, "%") {
				text += " "
			}
			text += unit
		}
	}
	ui.Row(c.Key("step")).Gap(2).AlignItems(ui.Center).Padding(1).Radius(7).Background(k.bgLow).Border(1, k.cardBorder).Children(func() {
		button := func(name string, dir int) {
			next, ok := tspPrefsStep(ctl, dir)
			b := textButton(c.Key(name), k, name).Width(24)
			if !ok || off {
				b.Opacity(0.35)
			}
			if b.Clicked() && ok && !off {
				v.change(id, next)
			}
		}
		button("−", -1)
		ui.Text(c, text).FontSize(12).TextColor(k.text).MinWidth(44).TextAlign(ui.Center).SingleLine()
		button("+", 1)
	})
}

// tspPrefsNumberLabel is the label a number's labels give value.
func tspPrefsNumberLabel(ctl map[string]any, value float64) (string, bool) {
	labels, _ := ctl["labels"].(map[string]any)
	for key, l := range labels {
		if f, err := strconv.ParseFloat(key, 64); err == nil && f == value {
			s, _ := l.(string)
			return s, true
		}
	}
	return "", false
}

// tspPrefsStep is where a number's step in dir (1 up, -1 down) goes:
// with labels, to the nearest of their keys past the value; else by its
// step (1 when none), within min and max. It reports false where the
// value cannot go further.
func tspPrefsStep(ctl map[string]any, dir int) (float64, bool) {
	value, _ := ctl["value"].(float64)
	if labels, _ := ctl["labels"].(map[string]any); len(labels) > 0 {
		var keys []float64
		for key := range labels {
			if f, err := strconv.ParseFloat(key, 64); err == nil {
				keys = append(keys, f)
			}
		}
		slices.Sort(keys)
		if dir > 0 {
			for _, f := range keys {
				if f > value {
					return f, true
				}
			}
		} else {
			for i := len(keys) - 1; i >= 0; i-- {
				if keys[i] < value {
					return keys[i], true
				}
			}
		}
		return value, false
	}
	step, _ := ctl["step"].(float64)
	if step <= 0 {
		step = 1
	}
	next := value + float64(dir)*step
	// Steps of a fraction add up without the float's noise (0.1+0.2).
	next = math.Round(next*1e9) / 1e9
	if lo, ok := ctl["min"].(float64); ok && next < lo {
		next = lo
	}
	if hi, ok := ctl["max"].(float64); ok && next > hi {
		next = hi
	}
	return next, next != value
}

// textField draws a text setting as a field's look: its value (dots for
// a secret, "Not set" for none); while the program has it open, its
// draft with the caret where the program has it, the keys going to the
// program. A click asks the program to open it (activate).
func (v *tspPrefsView) textField(c *ui.Context, id string, ctl map[string]any, off bool) {
	k := v.k
	value, _ := ctl["value"].(string)
	secret, _ := ctl["secret"].(bool)
	mono, _ := ctl["mono"].(bool)
	placeholder, _ := ctl["placeholder"].(string)
	if placeholder == "" {
		placeholder = "Not set"
	}
	font := ""
	if mono && !secret {
		font = v.a.editorFont().Family
	}
	hide := func(s string) string {
		if secret {
			return strings.Repeat("•", len([]rune(s)))
		}
		return s
	}
	editing := v.editing(id)
	field := ui.Row(c.Key("text")).AlignItems(ui.Center).Width(200).Height(24).Padding(0, 8).Radius(6).Background(k.bgLow).
		Border(1, k.cardBorder).Clip().Cursor(ui.CursorText)
	if editing {
		field.Border(1, k.busy)
	}
	field.Children(func() {
		if editing {
			draft, ok := v.edit["draft"].(string)
			if !ok {
				draft = value
			}
			at := utf16Len(draft)
			if cur, ok := v.edit["cursor"].(float64); ok {
				at = int(cur)
			}
			pre, post := tspSplitAt(draft, at)
			v.caretText(c, hide(pre), hide(post), "", font)
			return
		}
		if value == "" {
			ui.Text(c, placeholder).FontSize(12).TextColor(k.textMuted).SingleLine()
			return
		}
		t := ui.Text(c, hide(value)).FontSize(12).TextColor(k.text).SingleLine()
		if font != "" {
			t.Font(font)
		}
	})
	if field.Clicked() && !off && !editing {
		v.send(map[string]any{"ev": "activate", "item": id})
	}
}

// chips draws a multi as toggle chips, those on filled; the program's
// option highlighted while it has the row open. A click on one turns it
// on or off, which the program gets as the values on, in order.
func (v *tspPrefsView) chips(c *ui.Context, id string, ctl map[string]any, off bool) {
	k := v.k
	values := tspPrefsValues(ctl)
	sel := ""
	if v.editing(id) {
		sel, _ = v.edit["option"].(string)
	}
	wrap := ui.Row(c.Key("chips")).Gap(5).Wrap().FillWidth()
	wrap.Children(func() {
		for i, o := range tspPrefsOptions(ctl) {
			on := slices.Contains(values, o[0])
			chip := ui.Row(c.Key(i)).Gap(4).AlignItems(ui.Center).Padding(2, 9).Radius(10).Border(1, k.cardBorder).Cursor(ui.CursorPointer)
			if on {
				chip.Background(k.busy.Alpha(0.22)).Border(1, k.busy)
			} else if chip.Hovered() && !off {
				chip.Background(k.hover)
			}
			if o[0] == sel {
				chip.Border(1.5, k.text)
			}
			if o[2] != "" {
				chip.Tooltip(o[2])
			}
			chip.Children(func() {
				if on {
					ui.Text(c, "✓").FontSize(11).TextColor(k.busy)
				}
				ui.Text(c, o[1]).FontSize(12).TextColor(k.text).SingleLine()
			})
			if chip.Clicked() && !off {
				v.change(id, tspMultiToggle(values, o[0]))
			}
		}
	})
}

// ordered draws an ordered multi as a list: those on first, in their
// order, numbered, each with buttons moving it up and down among them,
// then those off; each with a switch turning it on (last) or off. The
// program gets the values on, in order.
func (v *tspPrefsView) ordered(c *ui.Context, id string, ctl map[string]any, off bool) {
	k := v.k
	values := tspPrefsValues(ctl)
	opts := tspPrefsOptions(ctl)
	byValue := map[string][3]string{}
	for _, o := range opts {
		byValue[o[0]] = o
	}
	var order [][3]string
	for _, val := range values {
		if o, ok := byValue[val]; ok {
			order = append(order, o)
		}
	}
	for _, o := range opts {
		if !slices.Contains(values, o[0]) {
			order = append(order, o)
		}
	}
	sel := ""
	if v.editing(id) {
		sel, _ = v.edit["option"].(string)
	}
	list := ui.Column(c.Key("order")).Gap(2).FillWidth()
	list.Children(func() {
		pos := 0
		for i, o := range order {
			on := slices.Contains(values, o[0])
			item := ui.Row(c.Key(i)).Gap(6).AlignItems(ui.Center).Padding(3, 6).Radius(6).FillWidth()
			if o[0] == sel {
				item.Background(k.hover)
			}
			tspReveal(item, o[0] == sel)
			if o[2] != "" {
				item.Tooltip(o[2])
			}
			if on {
				pos++
			}
			at := pos
			item.Children(func() {
				n := ""
				if on {
					n = strconv.Itoa(at) + "."
				}
				ui.Text(c, n).FontSize(11).TextColor(k.textMuted).Width(18).TextAlign(ui.End)
				t := ui.Text(c, o[1]).FontSize(12.5).SingleLine().Grow(1).MinWidth(0)
				if on {
					t.TextColor(k.text)
				} else {
					t.TextColor(k.textMuted)
				}
				if on {
					move := func(name string, delta int, can bool) {
						b := iconButton(c.Key(name), k, "chevron-down", name, 20, 11)
						if delta < 0 {
							b.Rotate(180)
						}
						if !can || off {
							b.Opacity(0.3)
						}
						if b.Clicked() && can && !off {
							v.change(id, tspMultiMove(values, o[0], delta))
						}
					}
					move("Move up", -1, at > 1)
					move("Move down", 1, at < len(values))
				}
				sw := on
				if ui.Switch(c.Key("on"), &sw).Label(o[1]).Disabled(off).Changed() {
					v.change(id, tspMultiToggle(values, o[0]))
				}
			})
		}
	})
}

// tspPrefsValues are a multi's values on, in order.
func tspPrefsValues(ctl map[string]any) []string {
	vals, _ := ctl["values"].([]any)
	out := make([]string, 0, len(vals))
	for _, x := range vals {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// tspMultiToggle turns value off when on, else on, last; in a new slice.
func tspMultiToggle(values []string, value string) []string {
	if i := slices.Index(values, value); i >= 0 {
		return slices.Delete(slices.Clone(values), i, i+1)
	}
	return append(slices.Clone(values), value)
}

// tspMultiMove moves an on value by delta places among those on, within
// them; in a new slice.
func tspMultiMove(values []string, value string, delta int) []string {
	out := slices.Clone(values)
	i := slices.Index(out, value)
	if i < 0 {
		return out
	}
	j := max(0, min(len(out)-1, i+delta))
	out = slices.Delete(out, i, i+1)
	return slices.Insert(out, j, value)
}

// editorCard draws the editors the program has without a native control
// in a card over the page.
func (v *tspPrefsView) editorCard(c *ui.Context) {
	if len(v.editor) == 0 {
		return
	}
	k := v.k
	card := ui.Scroll(c.Key("editor")).Absolute().Left(16).Right(16).TopPercent(12).MaxHeightPercent(76).
		Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Padding(12, 14).Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.3))
	card.Children(func() {
		col := ui.Column(c).Gap(8).FillWidth()
		col.Children(func() {
			for _, ch := range v.editor {
				v.a.tspNode(c, k, v.t, v.p, v.sf, ch)
			}
		})
	})
}

// marked draws a label or hint, the words of the search marked in it.
func (v *tspPrefsView) marked(c *ui.Context, s string, size float32, color ui.Color) ui.Element {
	marks := tspMatches(s, v.query)
	if marks == nil {
		return ui.Text(c, s).FontSize(size).TextColor(color)
	}
	rs := []rune(s)
	var spans []ui.Span
	start := 0
	for i := 1; i <= len(rs); i++ {
		if i < len(rs) && marks[i] == marks[start] {
			continue
		}
		sp := ui.Span{Text: string(rs[start:i]), Size: size, Color: color}
		if marks[start] {
			sp.Background, sp.Color = v.k.busy.Alpha(0.3), v.k.text
		}
		spans = append(spans, sp)
		start = i
	}
	if len(spans) == 0 {
		return ui.Text(c, s).FontSize(size).TextColor(color)
	}
	return ui.RichText(c, spans...)
}

// tspMatches marks the runes of s that the words of query match,
// ignoring case; nil when nothing does.
func tspMatches(s, query string) []bool {
	words := strings.Fields(query)
	if len(words) == 0 || s == "" {
		return nil
	}
	rs := []rune(s)
	low := make([]rune, len(rs))
	for i, r := range rs {
		low[i] = unicode.ToLower(r)
	}
	marks := make([]bool, len(rs))
	hit := false
	for _, w := range words {
		wr := []rune(strings.ToLower(w))
		for i := 0; i+len(wr) <= len(low); i++ {
			if slices.Equal(low[i:i+len(wr)], wr) {
				for j := i; j < i+len(wr); j++ {
					marks[j] = true
				}
				hit = true
			}
		}
	}
	if !hit {
		return nil
	}
	return marks
}

// tspSplitAt splits s at a UTF-16 offset, as the protocol counts its
// carets, clamped to s.
func tspSplitAt(s string, at int) (string, string) {
	if at <= 0 {
		return "", s
	}
	runes := utf16ToRunes(s, at)
	if runes < 0 {
		return s, ""
	}
	b := runeByte(s, runes)
	return s[:b], s[b:]
}
