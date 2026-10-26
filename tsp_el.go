package main

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// An el node (elements/el.md) is one HTML element of an allowed tag: its
// text, then its children, laid out by the user agent's defaults of its
// tag and the program's style sheets (tsp_css.go), in MyGo's boxes: a
// block lays out its children in a column, its inline content in runs
// (one rich text where it is all text); flex, grid and table containers
// as MyGo's rows, columns and grids. An input of type checkbox or radio
// is a control the user flips at once, which the program is told of; a
// label's click flips its first control; actions on any el go to the
// program, with the values of the form around it.

// tspEls is a pane's state of the el nodes it draws: the user's flips of
// their controls, over the checked the program says, and what was
// reported; and, for the frame being drawn, the surfaces' documents
// indexed, their sheets and the elements' computed styles.
type tspEls struct {
	// frame is the app's frame drawn last; count counts the frames the
	// pane drew els in, the clock of what the state keeps.
	frame, count uint64
	// flips are the controls the user flipped, by key (tspEls.key);
	// shown the checked each control was last drawn with, from its
	// program.
	flips map[string]elFlip
	shown map[string]elShown
	// errs are the unknown tags reported, by anchor and node id;
	// sheetErrs the sheets whose errors were, by anchor and name, with
	// the CSS reported.
	errs      map[string]elErr
	sheetErrs map[string]string
	// sheets are the sheets parsed, by their CSS, with the count each
	// was last used at.
	sheets map[string]*elParsed
	doc    elDoc
	// dblHeld is the control whose double click's second press is
	// held: its release is no click of its own.
	dblHeld string
	// indexed are the surfaces' roots indexed in doc this frame; surf
	// the sheets of each surface this frame, the user agent's first.
	indexed []*rex.TSPNode
	surf    map[*rex.Surface]elSheets
	// styles are the elements' computed styles this frame, from pool;
	// base is the regions' style, which their nodes inherit.
	styles map[*rex.TSPNode]*elStyle
	pool   []*elStyle
	used   int
	base   elStyle
	match  []cssMatch
	// hoverWas and hoverNow are the els under the pointer, as the last
	// frame drew them and as this one does.
	hoverWas, hoverNow map[*rex.TSPNode]bool
	// env is what values resolve against, made for the colors k; ctl
	// the colors of the controls in them.
	env cssEnv
	k   *colors
	ctl struct{ ring, hover, fill, wash ui.Color }
	// sf is the surface being drawn.
	sf *rex.Surface
}

// elFlip is a user's flip of a control: what it shows, and the checked
// its program had said then; seen the count it was last drawn at.
type elFlip struct {
	on, prog bool
	seen     uint64
}

type elShown struct {
	prog bool
	seen uint64
}

type elErr struct {
	n    *rex.TSPNode
	msg  string
	seen uint64
}

type elParsed struct {
	sheet *cssSheet
	seen  uint64
}

type elSheets struct {
	list  []*cssSheet
	hover bool
}

// elTags are the tags an el draws (elements/el.md).
var elTags = map[string]bool{"div": true, "span": true, "p": true, "section": true, "header": true,
	"footer": true, "nav": true, "aside": true, "main": true, "article": true, "figure": true,
	"blockquote": true, "ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "pre": true, "code": true, "kbd": true,
	"strong": true, "b": true, "em": true, "i": true, "del": true, "mark": true, "hr": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true, "label": true,
	"button": true, "form": true, "input": true}

// elTag is an el's tag as written, lowercased, div when absent.
func elTag(n *rex.TSPNode) string {
	tag, _ := n.Props["tag"].(string)
	if tag == "" {
		return "div"
	}
	return strings.ToLower(tag)
}

// elControl is an el's control type, checkbox or radio, or "".
func elControl(n *rex.TSPNode) string {
	if n.Kind != "el" || elTag(n) != "input" {
		return ""
	}
	switch typ, _ := n.Props["type"].(string); typ {
	case "checkbox", "radio":
		return typ
	}
	return ""
}

// elTagName is the element a node is, as type selectors see it: a form
// is a div, a control a span, an unknown tag or another input a div,
// and the node of any other kind a div.
func elTagName(n *rex.TSPNode) string {
	if n.Kind != "el" {
		return "div"
	}
	switch tag := elTag(n); {
	case tag == "form":
		return "div"
	case tag == "input":
		if elControl(n) != "" {
			return "span"
		}
		return "div"
	case elTags[tag]:
		return tag
	}
	return "div"
}

// elHasClass tells whether a node's element has a class: sf on every
// one, sf-el or sf-<kind>, sf-act with a click action, and an el's own
// classes but Tern's (sf and sf-*).
func elHasClass(n *rex.TSPNode, cl string) bool {
	switch {
	case cl == "sf":
		return true
	case n.Kind == "el" && cl == "sf-el", n.Kind != "el" && strings.HasPrefix(cl, "sf-") && cl[3:] == n.Kind:
		return true
	case cl == "sf-act":
		actions, _ := n.Props["actions"].(map[string]any)
		return actions["click"] != nil || actions["dblclick"] != nil
	case cl == "sf-hidden":
		hidden, _ := n.Props["hidden"].(bool)
		return hidden
	case n.Kind != "el" || strings.HasPrefix(cl, "sf-"):
		return false
	}
	class, _ := n.Props["class"].(string)
	for len(class) > 0 {
		i := strings.IndexAny(class, " \t\n")
		word := class
		if i >= 0 {
			word, class = class[:i], class[i+1:]
		} else {
			class = ""
		}
		if word == cl {
			return true
		}
	}
	return false
}

// elTones are the tones that set data-tone.
var elTones = map[string]bool{"neutral": true, "accent": true, "info": true, "success": true,
	"warning": true, "error": true, "pending": true, "muted": true, "user": true}

// elAttr is an attribute of a node's element: those Tern sets from the
// common props (data-id, data-role, data-tone, data-mark, data-href,
// title, aria-label), a control's (role, aria-checked, aria-disabled,
// checked, disabled; checked tells its state as drawn), and an el's
// allowed attrs.
func elAttr(n *rex.TSPNode, name string, checked func(*rex.TSPNode) bool) (string, bool) {
	str := func(prop string) (string, bool) {
		s, ok := n.Props[prop].(string)
		return s, ok && s != ""
	}
	switch name {
	case "data-id":
		return n.ID, true
	case "data-role":
		return str("role")
	case "data-tone":
		tone, ok := str("tone")
		return tone, ok && elTones[tone]
	case "data-mark":
		return str("mark")
	case "data-href":
		return str("href")
	case "title":
		return str("title")
	case "aria-label":
		return str("aria")
	case "class":
		if n.Kind != "el" {
			return "sf sf-" + n.Kind, true
		}
		class, _ := n.Props["class"].(string)
		return strings.TrimSpace("sf sf-el " + class), true
	}
	if n.Kind != "el" {
		return "", false
	}
	if typ := elControl(n); typ != "" {
		disabled, _ := n.Props["disabled"].(bool)
		on := checked != nil && checked(n)
		switch name {
		case "role":
			return typ, true
		case "aria-checked":
			return strconv.FormatBool(on), true
		case "aria-disabled":
			return strconv.FormatBool(disabled), true
		case "checked":
			return "", on
		case "disabled":
			return "", disabled
		case "aria-labelledby":
			return "", false
		}
	}
	if !elAllowedAttr(name) {
		return "", false
	}
	attrs, _ := n.Props["attrs"].(map[string]any)
	switch v := attrs[name].(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", false
}

// elAllowedAttr tells the attribute names attrs may set: data-* and
// aria-* of lowercase letters, digits, - and _, but those Tern owns;
// role, aria-labelledby, colspan and rowspan.
func elAllowedAttr(name string) bool {
	switch name {
	case "data-id", "data-role", "data-tone", "data-mark", "data-href", "data-scope", "aria-label", "aria-checked":
		return false
	case "role", "aria-labelledby", "colspan", "rowspan":
		return true
	}
	rest, ok := strings.CutPrefix(name, "data-")
	if !ok {
		rest, ok = strings.CutPrefix(name, "aria-")
	}
	if !ok || rest == "" {
		return false
	}
	for i := range len(rest) {
		c := rest[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// elUA is Tern's user-agent sheet for els, a subset of Chrome's, with
// Tern's look of labels, Kit's reset of buttons and the controls'
// inline-block (elements/el.md, Tags).
const elUA = `
div, p, section, header, footer, nav, aside, main, article, figure, blockquote,
ul, ol, dl, dt, dd, h1, h2, h3, h4, pre, hr { display: block }
li { display: list-item }
table { display: table; gap: 2px }
thead { display: table-header-group }
tbody { display: table-row-group }
tr { display: table-row }
td, th { display: table-cell; padding: 1px }
th { font-weight: bold; text-align: center }
label { display: inline-flex; align-items: center; gap: 6px }
button { display: inline-block }
[role=checkbox], [role=radio] { display: inline-block }
p, dl, pre { margin: 1em 0 }
ul, ol { margin: 1em 0; padding-left: 40px }
ul { list-style-type: disc }
ol { list-style-type: decimal }
ul ul, ol ul { list-style-type: circle }
ul ul ul, ul ol ul, ol ul ul, ol ol ul { list-style-type: square }
ul ul, ul ol, ol ul, ol ol { margin-top: 0; margin-bottom: 0 }
figure, blockquote { margin: 1em 40px }
dd { margin-left: 40px }
h1 { font-size: 2em; font-weight: bold; margin: .67em 0 }
h2 { font-size: 1.5em; font-weight: bold; margin: .83em 0 }
h3 { font-size: 1.17em; font-weight: bold; margin: 1em 0 }
h4 { font-weight: bold; margin: 1.33em 0 }
pre { white-space: pre; font-family: monospace }
code, kbd { font-family: monospace }
strong, b { font-weight: bolder }
em, i { font-style: italic }
del { text-decoration: line-through }
mark { background-color: yellow; color: black }
hr { margin: .5em 0; border-top: 1px solid gray }
`

var elUASheet = parseCSS(elUA)

// elState is the pane's el state, made ready for the frame and for the
// surface being drawn: its document indexed and its sheets parsed.
func (a *App) elState(c *ui.Context, p *Pane, k *colors, sf *rex.Surface) *tspEls {
	e := p.els
	if e == nil {
		e = &tspEls{flips: map[string]elFlip{}, shown: map[string]elShown{}, errs: map[string]elErr{},
			sheetErrs: map[string]string{}, sheets: map[string]*elParsed{}, surf: map[*rex.Surface]elSheets{},
			styles: map[*rex.TSPNode]*elStyle{}, hoverWas: map[*rex.TSPNode]bool{}, hoverNow: map[*rex.TSPNode]bool{},
			doc: elDoc{parent: map[*rex.TSPNode]*rex.TSPNode{}, roots: map[*rex.TSPNode]*rex.TSPNode{}}}
		e.doc.checked = e.checked
		e.doc.hovered = func(n *rex.TSPNode) bool { return e.hoverWas[n] }
		p.els = e
	}
	if e.frame != a.frame {
		e.newFrame(a, c, k)
	}
	if e.sf != sf {
		e.sf = sf
		e.env.palette = nil
		if pal := sf.Palette; pal != nil {
			e.env.palette = pal.Light
			if a.dark && pal.Dark != nil || pal.Light == nil {
				e.env.palette = pal.Dark
			}
		}
	}
	if !slices.Contains(e.indexed, sf.Root) {
		e.doc.index(sf.Root)
		e.indexed = append(e.indexed, sf.Root)
	}
	if _, ok := e.surf[sf]; !ok {
		a.elSheetsOf(p, sf, e)
	}
	return e
}

// newFrame drops the last frame's documents and styles, and makes Tern's
// variables anew when the colors they come from changed.
func (e *tspEls) newFrame(a *App, c *ui.Context, k *colors) {
	e.frame, e.sf = a.frame, nil
	if e.count++; e.count%elKeep == 0 {
		e.prune()
	}
	clear(e.doc.parent)
	clear(e.doc.roots)
	e.indexed = e.indexed[:0]
	clear(e.surf)
	clear(e.styles)
	e.used = 0
	e.hoverWas, e.hoverNow = e.hoverNow, e.hoverWas
	clear(e.hoverNow)
	mono, ansi := a.editorFont().Family, a.ansiPalette()
	if e.k != k || e.env.dark != a.dark || e.env.mono != mono || e.env.vars == nil || e.env.vars["--ansi1"] != cssHex(ansi.Palette[1]) {
		e.k, e.env.dark, e.env.mono = k, a.dark, mono
		e.env.vars = elVars(k, a.dark, mono, &ansi.Palette)
		e.ctl.ring, e.ctl.hover, e.ctl.fill = k.text.Alpha(0.22), k.textFaint, elFill(k.busy)
		e.ctl.wash = ui.RGBA(255, 255, 255, 0.9)
		if a.dark {
			e.ctl.wash = ui.RGBA(255, 255, 255, 0.06)
		}
	}
	e.env.vw, e.env.vh = c.Size()
	prefs := c.Preferences()
	e.env.reduceMotion, e.env.highContrast = prefs.ReduceMotion, prefs.HighContrast
	e.base.inherit(&elStyle{color: k.text, fontSize: 13, weight: 400, whiteSpace: "normal", listStyle: "disc", tc: k.text})
	e.base.display, e.base.col = "flex", true
}

// elKeep is how many of its frames the pane keeps what it drew no more:
// a sheet parsed, an error reported, a control's checked shown; a flip,
// past elKeepFlips of them.
const (
	elKeep      = 600
	elKeepFlips = 256
)

// prune drops what was not drawn in the last elKeep frames, so that the
// state of nodes gone from the documents goes too.
func (e *tspEls) prune() {
	old := func(seen uint64) bool { return e.count-seen > elKeep }
	for css, ps := range e.sheets {
		if old(ps.seen) {
			delete(e.sheets, css)
		}
	}
	for key, css := range e.sheetErrs {
		if e.sheets[css] == nil {
			delete(e.sheetErrs, key)
		}
	}
	for key, er := range e.errs {
		if old(er.seen) {
			delete(e.errs, key)
		}
	}
	for key, s := range e.shown {
		if old(s.seen) {
			delete(e.shown, key)
		}
	}
	if len(e.flips) > elKeepFlips {
		for key, f := range e.flips {
			if old(f.seen) {
				delete(e.flips, key)
			}
		}
	}
}

// elSheetsOf takes a surface's sheets, parsing those it has not, after
// the user agent's; a live surface's program is told of a sheet's
// errors, once for each CSS it sends.
func (a *App) elSheetsOf(p *Pane, sf *rex.Surface, e *tspEls) {
	s := elSheets{list: []*cssSheet{elUASheet}}
	for _, sh := range sf.Sheets {
		ps := e.sheets[sh.CSS]
		if ps == nil {
			ps = &elParsed{sheet: parseCSS(sh.CSS)}
			e.sheets[sh.CSS] = ps
		}
		ps.seen = e.count
		s.list = append(s.list, ps.sheet)
		s.hover = s.hover || ps.sheet.hover
		if len(ps.sheet.errs) > 0 && !sf.Closed && !sf.NoListen {
			key := strconv.FormatUint(uint64(sf.Image), 10) + "/" + sh.Name
			if e.sheetErrs[key] != sh.CSS {
				e.sheetErrs[key] = sh.CSS
				a.tspSend(p, map[string]any{"ev": "error", "sf": sf.ID, "sheet": sh.Name, "msg": strings.Join(ps.sheet.errs, "; ")})
			}
		}
	}
	e.surf[sf] = s
}

// elVars are Tern's CSS variables (styles/variables.md) in GoRex's
// colors: its text steps, hairlines, accent (GoRex's busy green) and
// status colors, fills, radii, fonts and the grid metrics of a 13px
// surface; and the terminal's ANSI colors.
func elVars(k *colors, dark bool, mono string, ansi *[16]ui.Color) map[string]string {
	ink := func(a float32) string { return cssHex(k.text.Alpha(a)) }
	line := float32(0.09)
	if dark {
		line = 0.1
	}
	m := map[string]string{
		"--t2": cssHex(k.text.Mix(k.textMuted, 0.5)), "--ink2": ink(0.66), "--ink3": ink(0.44), "--ink4": ink(0.14),
		"--l1": ink(0.06), "--l2": ink(line), "--l3": ink(0.14), "--l4": ink(0.22),
		"--chip-bg": ink(0.055), "--raise": ink(0.04), "--sf-shade": ink(0.03),
		"--card": cssHex(k.card), "--sf-card-bg": cssHex(k.card), "--code-bg": cssHex(k.bgLow), "--tbl-head": cssHex(k.bgLow),
		"--page": cssHex(k.bgBottom), "--panel": cssHex(k.bgBottom), "--tv-bg": cssHex(k.bgBottom), "--fog": cssHex(k.bgBottom),
		"--pop-bg": cssHex(k.panel), "--well": cssHex(k.track), "--well-line": cssHex(k.cardBorder), "--dim": cssHex(k.backdrop),
		"--sf-sel": cssHex(k.panelSel), "--tv-sel": cssHex(k.panelSel), "--sf-mark-bg": cssHex(k.busy.Alpha(0.16)),
		"--tv-m1": cssHex(k.attention.Alpha(0.34)), "--tv-m2": cssHex(k.busy.Alpha(0.3)), "--tv-cur": cssHex(k.text),
		"--sf-c-code": "currentColor", "--grad-ink": "#05070b",
		"--r-chip": "6px", "--r-ctl": "8px", "--r-card": "12px", "--r-panel": "16px",
		"--sans": "system-ui", "--mono": mono, "--tv-font": mono, "--hand": "cursive",
		"--sf-fs": "13px", "--sf-lh": "17px", "--sf-cw": "8px", "--sf-zoom": "1",
		"--sf-block-gap": "16px", "--sf-inner-gap": "8px",
	}
	for _, group := range [...]struct {
		c     ui.Color
		names string
	}{
		{k.text, "--t1 --ink --tv-fg --sf-neutral --sf-c-path --sf-c-num"},
		{k.textMuted, "--t3 --sf-muted --sf-c-muted --sf-c-del --sf-c-key"},
		{k.textFaint, "--t4 --sf-c-dim"},
		{k.busy, "--accent --accent-ink --sf-c-accent --sf-c-link --a1 --a2 --ok --sf-ok --sf-c-success --sf-c-ins --live --sf-info --sf-c-info"},
		{elFill(k.busy), "--accent-fill --ok-fill --live-fill"},
		{k.attention, "--warn --sf-warn --sf-c-warning --bad --sf-bad --sf-c-error --ink-pen --sf-user"},
		{elFill(k.attention), "--bad-fill --ink-pen-fill"},
	} {
		for _, name := range strings.Fields(group.names) {
			m[name] = cssHex(group.c)
		}
	}
	for i, c := range ansi {
		m["--ansi"+strconv.Itoa(i)] = cssHex(c)
	}
	return m
}

// elFill is a color darkened to carry white text, as Tern's *-fill.
func elFill(c ui.Color) ui.Color { return c.Mix(ui.RGB(0, 0, 0), 0.3) }

// elTone is a tone's color, --tc.
func elTone(k *colors, tone string) ui.Color {
	switch tone {
	case "accent", "info", "success", "pending":
		return k.busy
	case "warning", "error", "user":
		return k.attention
	case "muted":
		return k.textMuted
	}
	return k.text
}

// style is a node's computed style this frame: an el's from the sheets
// over what it inherits; another kind's inherited, flex for col and row.
// The children of a flex or grid container are blockified.
func (e *tspEls) style(n *rex.TSPNode) *elStyle {
	if st, ok := e.styles[n]; ok {
		return st
	}
	ps := &e.base
	if parent := e.doc.parent[n]; parent != nil && e.doc.roots[parent] == nil {
		ps = e.style(parent)
	}
	if e.used == len(e.pool) {
		e.pool = append(e.pool, new(elStyle))
	}
	st := e.pool[e.used]
	e.used++
	st.inherit(ps)
	if tone, _ := n.Props["tone"].(string); elTones[tone] {
		st.tc = elTone(e.k, tone)
	}
	if n.Kind != "el" {
		st.display = "block"
		if n.Kind == "col" || n.Kind == "row" {
			st.display, st.col = "flex", n.Kind == "col"
		}
	} else {
		e.match = e.doc.cascade(n, e.surf[e.sf].list, &e.env, e.match)
		st.compute(ps, e.match, &e.env)
		switch ps.display {
		case "flex", "inline-flex", "grid", "inline-grid":
			switch st.display {
			case "inline", "inline-block":
				st.display = "block"
			case "inline-flex":
				st.display = "flex"
			case "inline-grid":
				st.display = "grid"
			case "inline-table":
				st.display = "table"
			}
		}
	}
	e.styles[n] = st
	return st
}

// key names a control's local state: by its key among its siblings,
// else by its id, in the surface's anchor.
func (e *tspEls) key(n *rex.TSPNode) string {
	img := strconv.FormatUint(uint64(e.sf.Image), 10)
	var key string
	switch v := n.Props["key"].(type) {
	case string:
		key = v
	case float64:
		key = strconv.FormatFloat(v, 'f', -1, 64)
	}
	if key == "" {
		return img + "/" + n.ID
	}
	parent := ""
	if p := e.doc.parent[n]; p != nil {
		parent = p.ID
	}
	return img + "/" + parent + "/key:" + key
}

// checked is a control's state as drawn: the user's flip, while the
// program's checked is the one it was flipped over; a change of checked
// drops the flip.
func (e *tspEls) checked(n *rex.TSPNode) bool {
	prog, _ := n.Props["checked"].(bool)
	if len(e.flips) == 0 {
		return prog
	}
	k := e.key(n)
	if f, ok := e.flips[k]; ok {
		if f.prog == prog {
			if f.seen != e.count {
				f.seen = e.count
				e.flips[k] = f
			}
			return f.on
		}
		delete(e.flips, k)
	}
	return prog
}

// set shows a control checked or not, over its program's checked.
func (e *tspEls) set(n *rex.TSPNode, on bool) {
	prog, _ := n.Props["checked"].(bool)
	if k := e.key(n); on == prog {
		delete(e.flips, k)
	} else {
		e.flips[k] = elFlip{on, prog, e.count}
	}
}

// flip flips a control as a click does: a checkbox toggles; a radio is
// checked and the others of its group unchecked, and a checked radio
// stays as it is (false: nothing changed).
func (e *tspEls) flip(n *rex.TSPNode) bool {
	on := e.checked(n)
	if elControl(n) != "radio" {
		e.set(n, !on)
		return true
	}
	if on {
		return false
	}
	for _, o := range e.doc.group(n) {
		if o != n && e.checked(o) {
			e.set(o, false)
		}
	}
	e.set(n, true)
	return true
}

// elName and elValue are a control's name ("" for none) and value ("on"
// by default).
func elName(n *rex.TSPNode) string {
	name, _ := n.Props["name"].(string)
	return name
}

func elValue(n *rex.TSPNode) string {
	if v, ok := n.Props["value"].(string); ok {
		return v
	}
	return "on"
}

// form is the nearest el form around a node, the node itself when it is
// one.
func (d *elDoc) form(n *rex.TSPNode) *rex.TSPNode {
	for x := n; x != nil; x = d.parent[x] {
		if x.Kind == "el" && elTag(x) == "form" {
			return x
		}
	}
	return nil
}

// walk visits the nodes under root in document order, but the forms in
// it and what they hold.
func walkOutsideForms(root *rex.TSPNode, visit func(*rex.TSPNode)) {
	for _, ch := range root.Children {
		if ch.Kind == "el" && elTag(ch) == "form" {
			continue
		}
		visit(ch)
		walkOutsideForms(ch, visit)
	}
}

// group is a radio's group: the radios of its name in its nearest form,
// not in a form nested in it; outside every form, those outside every
// form in the surface's document. A radio without a name is alone.
func (d *elDoc) group(n *rex.TSPNode) []*rex.TSPNode {
	name := elName(n)
	if name == "" {
		return []*rex.TSPNode{n}
	}
	var out []*rex.TSPNode
	visit := func(x *rex.TSPNode) {
		if elControl(x) == "radio" && elName(x) == name {
			out = append(out, x)
		}
	}
	if f := d.form(n); f != nil {
		walkOutsideForms(f, visit)
		return out
	}
	region := n
	for d.parent[region] != nil {
		region = d.parent[region]
	}
	if root := d.roots[region]; root != nil {
		for _, r := range root.Children {
			walkOutsideForms(r, visit)
		}
	}
	return out
}

// values are a form's values (elements/el.md, Form values): its enabled,
// named controls as drawn, not those of nested forms, by name in the
// order first seen: a radio group's checked value (nil for none), a lone
// checkbox's state, several checkboxes' checked values.
func (d *elDoc) values(form *rex.TSPNode) map[string]any {
	type group struct {
		name  string
		radio bool
		pick  any
		boxes []*rex.TSPNode
	}
	var groups []*group
	walkOutsideForms(form, func(x *rex.TSPNode) {
		typ := elControl(x)
		name := elName(x)
		if disabled, _ := x.Props["disabled"].(bool); typ == "" || name == "" || disabled {
			return
		}
		i := slices.IndexFunc(groups, func(g *group) bool { return g.name == name })
		if i < 0 {
			i = len(groups)
			groups = append(groups, &group{name: name})
		}
		g := groups[i]
		if typ == "radio" {
			if !g.radio || g.pick == nil {
				if d.checked(x) {
					g.pick = elValue(x)
				}
			}
			g.radio = true
		} else {
			g.boxes = append(g.boxes, x)
		}
	})
	out := make(map[string]any, len(groups))
	for _, g := range groups {
		switch {
		case g.radio:
			out[g.name] = g.pick
		case len(g.boxes) == 1:
			out[g.name] = d.checked(g.boxes[0])
		default:
			picked := []string{}
			for _, b := range g.boxes {
				if d.checked(b) {
					picked = append(picked, elValue(b))
				}
			}
			out[g.name] = picked
		}
	}
	return out
}

// firstControl is the first control under a node, in document order.
func firstControl(n *rex.TSPNode) *rex.TSPNode {
	for _, ch := range n.Children {
		if elControl(ch) != "" {
			return ch
		}
		if c := firstControl(ch); c != nil {
			return c
		}
	}
	return nil
}

// tspEl draws an el node: nothing for display none; a control, or a box
// of its tag's layout with its text and children. A closed surface's,
// or one whose program does not listen, takes no clicks.
func (a *App) tspEl(c *ui.Context, key *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	e := a.elState(c, p, k, sf)
	st := e.style(n)
	inert := sf.Closed || sf.NoListen
	a.elReport(p, sf, e, n, inert)
	if st.display == "none" {
		return
	}
	var el ui.Element
	if elControl(n) != "" {
		el = a.elControlBox(c, key, t, p, sf, e, n, st, inert)
	} else {
		el = a.elBox(c, key, k, t, p, sf, e, n, st, inert)
	}
	// The pointer over the el, which :hover rules match from the next
	// frame on.
	if e.surf[sf].hover {
		h := el.Hovered()
		if h {
			e.hoverNow[n] = true
		}
		if h != e.hoverWas[n] {
			c.Invalidate()
		}
	}
	a.revealed(p, sf, n, el)
	if grow, _ := n.Props["grow"].(float64); grow > 0 {
		el.Grow(float32(grow))
	}
	if title, _ := n.Props["title"].(string); title != "" {
		el.Tooltip(title)
	}
}

// elReport tells a live surface's program of an el of an unknown tag (or
// an input of no or another type), once for each version of the node.
func (a *App) elReport(p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode, inert bool) {
	var msg string
	switch tag := elTag(n); {
	case tag == "input":
		typ, _ := n.Props["type"].(string)
		switch {
		case elControl(n) != "":
			return
		case typ == "":
			msg = "unknown el tag input"
		default:
			msg = "unknown el tag input type " + typ
		}
	case !elTags[tag]:
		msg = "unknown el tag " + tag
	default:
		return
	}
	if inert {
		return
	}
	key := strconv.FormatUint(uint64(sf.Image), 10) + "/" + n.ID
	was, ok := e.errs[key]
	if ok && was.n == n && was.msg == msg {
		was.seen = e.count
		e.errs[key] = was
		return
	}
	e.errs[key] = elErr{n, msg, e.count}
	a.tspSend(p, map[string]any{"ev": "error", "sf": sf.ID, "id": n.ID, "msg": msg})
}

// elBox draws an el as a box laid out by its display: flex as a row or a
// column, grid as a grid of its tracks, a table as a grid of its cells;
// any other in block flow (elFlow). A label's click flips its first
// control, and actions go to the program.
func (a *App) elBox(c *ui.Context, key *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode, st *elStyle, inert bool) ui.Element {
	tag := elTag(n)
	box := ui.Box(key)
	var cells []elCell
	switch st.display {
	case "flex", "inline-flex":
		if !st.col {
			box.Row()
		}
		if st.reverse {
			box.Reverse()
		}
		if st.wrap {
			box.Wrap()
		}
		align := ui.Stretch
		if st.hasAlignItems {
			align = st.alignItems
		}
		box.AlignItems(align)
		if st.hasJustify {
			box.Justify(st.justify)
		}
	case "grid", "inline-grid":
		box.Grid()
		if len(st.cols) > 0 {
			box.ColumnTracks(st.cols...)
		} else {
			box.Columns(1)
		}
	case "table", "inline-table":
		box.Grid()
		var ncols int
		cells, ncols = e.tableCells(n)
		if ncols > 0 {
			tracks := make([]ui.Track, ncols)
			for i := range tracks {
				tracks[i] = ui.FitContent()
			}
			box.ColumnTracks(tracks...)
		}
	case "table-row":
		box.Row().AlignItems(ui.Stretch)
	default:
		box.AlignItems(ui.Stretch)
	}
	if st.rowGap > 0 {
		box.GapY(st.rowGap)
	}
	if st.colGap > 0 {
		box.GapX(st.colGap)
	}
	elBoxStyle(box, st)
	elTextStyle(box, st)
	label := tag == "label"
	head := a.tspHead
	if label {
		// Its text is not selectable: a click on it flips its control.
		a.tspHead = true
	}
	box.Children(func() {
		if tag == "hr" || tag == "input" {
			// No children; an input that is no control draws empty.
			return
		}
		if st.display == "list-item" {
			a.elMarker(c, e, n, st)
		}
		switch st.display {
		case "flex", "inline-flex", "grid", "inline-grid":
			a.elItems(c, k, t, p, sf, n, st)
		case "table", "inline-table":
			for i, cell := range cells {
				b := ui.Box(c.Key(i)).RowStart(cell.row + 1).ColumnStart(cell.col + 1)
				if cell.cols > 1 {
					b.ColumnSpan(cell.cols)
				}
				if cell.rows > 1 {
					b.RowSpan(cell.rows)
				}
				b.Children(func() { a.tspNode(c, k, t, p, sf, cell.n) })
			}
		default:
			a.elFlow(c, k, t, p, sf, e, n, st)
		}
	})
	a.tspHead = head
	if label && !inert && box.Clicked() {
		if ctl := firstControl(n); ctl != nil {
			if disabled, _ := ctl.Props["disabled"].(bool); !disabled {
				a.elFlip(c, t, p, sf, e, ctl)
			}
		}
	}
	a.elAction(t, p, sf, e, n, box, inert)
	return box
}

// elCell is a table's cell placed in its grid: from row and col, over
// rows and cols.
type elCell struct {
	n                    *rex.TSPNode
	row, col, rows, cols int
}

// elTableRows and elTableCols bound a table's grid, and elTableArea the
// slots its cells' spans take in all: the cells past them are not drawn,
// and spans past the area are one slot.
const (
	elTableRows = 1024
	elTableCols = 256
	elTableArea = 1 << 16
)

// tableCells places a table's cells, as a table's automatic layout
// does: its rows (in thead and tbody too) top down, each one's cells
// left to right past those spanning into it from above; within
// elTableRows, elTableCols and elTableArea.
func (e *tspEls) tableCells(n *rex.TSPNode) (cells []elCell, ncols int) {
	var rows []*rex.TSPNode
	for _, ch := range n.Children {
		if ch.Kind == "el" && (elTag(ch) == "thead" || elTag(ch) == "tbody") {
			rows = append(rows, ch.Children...)
		} else {
			rows = append(rows, ch)
		}
	}
	rows = rows[:min(len(rows), elTableRows)]
	taken := map[[2]int]bool{}
	area := 0
	for r, row := range rows {
		cs := []*rex.TSPNode{row}
		if row.Kind == "el" && elTag(row) == "tr" {
			cs = row.Children
		}
		col := 0
		for _, cell := range cs {
			for taken[[2]int{r, col}] {
				col++
			}
			if col >= elTableCols || area >= elTableArea {
				break
			}
			if hidden, _ := cell.Props["hidden"].(bool); hidden || cell.Kind == "el" && e.style(cell).display == "none" {
				continue
			}
			span := min(elSpanAttr(cell, "colspan"), elTableCols-col)
			rspan := min(elSpanAttr(cell, "rowspan"), len(rows)-r)
			if area+span*rspan > elTableArea {
				span, rspan = 1, 1
			}
			area += span * rspan
			for i := range span {
				for j := range rspan {
					taken[[2]int{r + j, col + i}] = true
				}
			}
			cells = append(cells, elCell{cell, r, col, rspan, span})
			col += span
			ncols = max(ncols, col)
		}
	}
	return cells, ncols
}

// elSpanAttr is a cell's colspan or rowspan, 1 to 64.
func elSpanAttr(n *rex.TSPNode, name string) int {
	v, ok := elAttr(n, name, nil)
	if !ok {
		return 1
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 1
	}
	return int(max(1, min(64, f)))
}

// elItems draws a flex or grid container's items: its text, then its
// children.
func (a *App) elItems(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, st *elStyle) {
	if text, _ := n.Props["text"].(string); text != "" {
		a.elRich(c, st, []ui.Span{elSpan(st, text)})
	}
	for _, ch := range n.Children {
		a.tspNode(c, k, t, p, sf, ch)
	}
}

// elFlow draws a block's content: block children down its column, their
// vertical margins collapsing between them; runs of inline content
// (its text, inline els) between them in lines (elRun).
func (a *App) elFlow(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode, st *elStyle) {
	text, _ := n.Props["text"].(string)
	items := len(n.Children) + 1 // the text, then the children
	inline := func(i int) bool {
		if i == 0 {
			return true
		}
		ch := n.Children[i-1]
		if ch.Kind != "el" {
			return false
		}
		if hidden, _ := ch.Props["hidden"].(bool); hidden {
			return true
		}
		d := e.style(ch).display
		return d == "none" || strings.HasPrefix(d, "inline")
	}
	prevBottom, prevBlock := float32(0), false
	for i := 0; i < items; {
		if inline(i) {
			j := i + 1
			for j < items && inline(j) {
				j++
			}
			a.elRun(c, k, t, p, sf, e, n, st, text, i, j)
			i, prevBlock = j, false
			continue
		}
		ch := n.Children[i-1]
		i++
		if ch.Kind != "el" {
			prevBlock = false
			a.tspNode(c, k, t, p, sf, ch)
			continue
		}
		cs := e.style(ch)
		if top := cs.margin[0]; prevBlock && !top.auto && !top.pct && top.v >= 0 && prevBottom >= 0 {
			cs.collapsedTop, cs.hasCollapsedTop = max(top.v, prevBottom)-prevBottom, true
		}
		prevBottom, prevBlock = 0, true
		if b := cs.margin[2]; !b.auto && !b.pct {
			prevBottom = b.v
		}
		a.tspNode(c, k, t, p, sf, ch)
	}
}

// elRun draws a run of a block's inline content, items i to j (0 its
// text, then its children): one rich text where it is all text, the
// inline els' styles on their spans; else a wrapping row of its items.
func (a *App) elRun(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode, st *elStyle, text string, i, j int) {
	flat := true
	for x := max(i, 1); x < j && flat; x++ {
		flat = e.flat(n.Children[x-1])
	}
	if flat {
		var spans []ui.Span
		if i == 0 && text != "" {
			spans = append(spans, elSpan(st, text))
		}
		for x := max(i, 1); x < j; x++ {
			spans = e.spans(spans, n.Children[x-1])
		}
		if len(spans) > 0 {
			a.elRich(c, st, spans)
		}
		return
	}
	row := ui.Row(c).Wrap().AlignItems(ui.Baseline)
	if st.hasTextAlign {
		row.Justify(st.textAlign)
	}
	row.Children(func() {
		for x := i; x < j; x++ {
			if x > 0 {
				a.tspNode(c, k, t, p, sf, n.Children[x-1])
			} else if text != "" {
				a.elRich(c, st, []ui.Span{elSpan(st, text)})
			}
		}
	})
}

// elRich draws spans as a paragraph of a style: aligned, spaced and
// wrapped by it, selectable but in a control.
func (a *App) elRich(c *ui.Context, st *elStyle, spans []ui.Span) {
	rt := ui.RichText(c, spans...)
	elTextStyle(rt, st)
	switch st.whiteSpace {
	case "nowrap", "pre":
		rt.NoWrap()
	}
	if !a.tspHead {
		rt.Selectable()
	}
}

// flat tells whether an inline el draws as spans of its paragraph: an
// inline el of text and such els only, without a box of its own (no
// padding, margin, border, corners, shadow, size or opacity), nor
// actions or a tooltip. Hidden ones draw nothing.
func (e *tspEls) flat(n *rex.TSPNode) bool {
	if n.Kind != "el" {
		return false
	}
	if hidden, _ := n.Props["hidden"].(bool); hidden {
		return true
	}
	st := e.style(n)
	switch {
	case st.display == "none":
		return true
	case st.display != "inline", elTag(n) == "input", elTag(n) == "hr", n.Props["actions"] != nil, n.Props["title"] != nil:
		return false
	case st.nShadow > 0, st.opacity < 1, st.invisible, st.width.set, st.height.set:
		return false
	}
	for i := range 4 {
		if st.pad[i].v != 0 || st.margin[i].v != 0 || st.margin[i].auto || st.borderOn[i] && st.borderW[i] > 0 || st.radius[i] != 0 {
			return false
		}
	}
	for _, ch := range n.Children {
		if !e.flat(ch) {
			return false
		}
	}
	return true
}

// spans appends a flat inline el's spans: its text, then its children's.
func (e *tspEls) spans(spans []ui.Span, n *rex.TSPNode) []ui.Span {
	if hidden, _ := n.Props["hidden"].(bool); hidden {
		return spans
	}
	st := e.style(n)
	if st.display == "none" {
		return spans
	}
	if text, _ := n.Props["text"].(string); text != "" {
		sp := elSpan(st, text)
		sp.Background = st.bg
		spans = append(spans, sp)
	}
	for _, ch := range n.Children {
		spans = e.spans(spans, ch)
	}
	return spans
}

// elMarker draws a list item's marker in the list's padding, left of
// it: a bullet of the list's style, or the item's number.
func (a *App) elMarker(c *ui.Context, e *tspEls, n *rex.TSPNode, st *elStyle) {
	var marker string
	switch ls := strings.ToLower(st.listStyle); ls {
	case "none":
		return
	case "disc":
		marker = "•"
	case "circle":
		marker = "◦"
	case "square":
		marker = "▪"
	case "decimal":
		i := 1
		for _, s := range e.doc.siblings(n) {
			if s == n {
				break
			}
			if s.Kind == "el" && elTag(s) == "li" {
				i++
			}
		}
		marker = strconv.Itoa(i) + "."
	default:
		marker = cssUnquote(st.listStyle)
	}
	m := ui.Text(c, marker).Absolute().Left(-40).Top(0).Width(34).TextAlign(ui.End)
	m.FontSize(st.fontSize).TextColor(st.color)
}

// elControlBox draws a checkbox or radio: a 15px square (round for a
// radio) with a hairline ring, which the pointer darkens; checked, the
// accent fill with a white check or dot; disabled, faded. A click flips
// it. When its program checks a radio anew, the flips of its group give
// way to what the program says.
func (a *App) elControlBox(c *ui.Context, key *ui.Context, t *Tab, p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode, st *elStyle, inert bool) ui.Element {
	radio := elControl(n) == "radio"
	disabled, _ := n.Props["disabled"].(bool)
	prog, _ := n.Props["checked"].(bool)
	ck := e.key(n)
	if was, ok := e.shown[ck]; ok && was.prog != prog && prog && radio {
		for _, o := range e.doc.group(n) {
			delete(e.flips, e.key(o))
		}
		c.Invalidate()
	}
	e.shown[ck] = elShown{prog, e.count}
	on := e.checked(n)
	r := float32(4)
	if radio {
		r = 7.5
	}
	ctl := ui.Box(key).Size(15, 15).Radius(r).Center().Shrink(0)
	ring := e.ctl.ring
	if !inert && !disabled && ctl.Hovered() {
		ring = e.ctl.hover
	}
	if on {
		ctl.Background(e.ctl.fill)
	} else {
		ctl.Background(e.ctl.wash).InsetShadow(0, 0, 0, 1, ring)
	}
	white := ui.RGB(255, 255, 255)
	ctl.Children(func() {
		switch {
		case !on:
		case radio:
			ui.Box(c).Size(6, 6).Radius(3).Background(white)
		default:
			ui.Icon(c, icon("check")).Size(11, 11).TextColor(white)
		}
	})
	opacity := st.opacity
	if disabled {
		opacity *= 0.45
	}
	if opacity < 1 {
		ctl.Opacity(opacity)
	}
	if st.invisible {
		ctl.Invisible()
	}
	elMargins(ctl, st)
	elPlace(ctl, st)
	if !inert && !disabled && ctl.Clicked() {
		a.elFlip(c, t, p, sf, e, n)
	}
	return ctl
}

// elFlip flips a control as the user clicked it and tells the program:
// a change event with its new state and, in a form, the form's values.
// Keys go to the program after, as after any click on its controls.
func (a *App) elFlip(c *ui.Context, t *Tab, p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode) {
	if !e.flip(n) {
		return
	}
	a.surfacePressed(t, p)
	ev := map[string]any{"ev": "change", "sf": sf.ID, "id": n.ID, "value": elValue(n), "checked": e.checked(n)}
	if name := elName(n); name != "" {
		ev["name"] = name
	}
	if form := e.doc.form(n); form != nil {
		ev["values"] = e.doc.values(form)
	}
	a.tspSend(p, ev)
	c.Invalidate()
}

// elAction sends the program an el's click or double click action: an
// action event (a name with = splits into act and value, with the
// modifiers held), or select and activate as their own events; in a form
// with its values. Tern's own actions (toggle, copy, open, zoom) are not
// the program's. With a double click action, a double click's second
// click is that action's and not a click of its own: its press, which
// makes the double click, may come a frame before its release.
func (a *App) elAction(t *Tab, p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode, el ui.Element, inert bool) {
	actions, _ := n.Props["actions"].(map[string]any)
	if inert || actions["click"] == nil && actions["dblclick"] == nil {
		return
	}
	clicks := el.Clicks()
	dbl, _ := actions["dblclick"].(string)
	double := false
	if dbl != "" {
		k := e.key(n)
		if e.dblHeld == k {
			if clicks > 0 {
				clicks--
				e.dblHeld = ""
			} else if !el.Pressed() {
				e.dblHeld = "" // released outside: no click
			}
		}
		if double = el.DoubleClicked(); double {
			if el.Pressed() {
				e.dblHeld = k
			} else if clicks > 0 {
				clicks--
			}
		}
	}
	if clicks > 0 {
		act, _ := actions["click"].(string)
		a.elSend(t, p, sf, e, n, el, act)
	}
	if double {
		a.elSend(t, p, sf, e, n, el, dbl)
	}
}

// elSend sends an el's action act, as elAction tells.
func (a *App) elSend(t *Tab, p *Pane, sf *rex.Surface, e *tspEls, n *rex.TSPNode, el ui.Element, act string) {
	ev := map[string]any{"sf": sf.ID, "id": n.ID}
	switch act {
	case "", "toggle", "copy", "open", "zoom":
		return
	case "select", "activate":
		ev["ev"], ev["item"] = act, n.ID
	default:
		ev["ev"] = "action"
		if name, value, ok := strings.Cut(act, "="); ok {
			ev["act"], ev["value"] = name, value
		} else {
			ev["act"] = act
		}
		if mods := elMods(el.ClickModifiers()); mods != nil {
			ev["mods"] = mods
		}
	}
	if form := e.doc.form(n); form != nil {
		ev["values"] = e.doc.values(form)
	}
	a.surfacePressed(t, p)
	a.tspSend(p, ev)
}

// elMods names the modifiers held, in the protocol's order.
func elMods(m ui.Modifiers) []string {
	var out []string
	for _, mod := range [...]struct {
		m    ui.Modifiers
		name string
	}{{ui.Shift, "shift"}, {ui.Ctrl, "ctrl"}, {ui.Alt, "alt"}, {ui.Super, "meta"}} {
		if m&mod.m != 0 {
			out = append(out, mod.name)
		}
	}
	return out
}

// elBoxStyle sets an element's box from its computed style: margins and
// padding, size, corners, fill, border, shadows, opacity, clipping and
// its place in a flex or grid container.
func elBoxStyle(e ui.Element, st *elStyle) {
	var pad [4]float32
	for i, l := range st.pad {
		if !l.pct && !l.auto {
			pad[i] = l.v
		}
	}
	if pad != [4]float32{} {
		e.Padding(pad[0], pad[1], pad[2], pad[3])
	}
	elMargins(e, st)
	if l := st.width; l.set && !l.auto {
		if l.pct {
			e.WidthPercent(l.v)
		} else {
			e.Width(l.v)
		}
	}
	if l := st.height; l.set && !l.auto {
		if l.pct {
			e.HeightPercent(l.v)
		} else {
			e.Height(l.v)
		}
	}
	if l := st.minW; l.set && !l.auto {
		if l.pct {
			e.MinWidthPercent(l.v)
		} else {
			e.MinWidth(l.v)
		}
	}
	if l := st.minH; l.set && !l.auto {
		if l.pct {
			e.MinHeightPercent(l.v)
		} else {
			e.MinHeight(l.v)
		}
	}
	if l := st.maxW; l.set && !l.auto {
		if l.pct {
			e.MaxWidthPercent(l.v)
		} else {
			e.MaxWidth(l.v)
		}
	}
	if l := st.maxH; l.set && !l.auto {
		if l.pct {
			e.MaxHeightPercent(l.v)
		} else {
			e.MaxHeight(l.v)
		}
	}
	if st.radius != [4]float32{} {
		e.Radius(st.radius[0], st.radius[1], st.radius[2], st.radius[3])
	}
	if st.bg.A > 0 {
		e.Background(st.bg)
	}
	var bw [4]float32
	var bc ui.Color
	for i := range 4 {
		if st.borderOn[i] && st.borderW[i] > 0 {
			if bw == [4]float32{} {
				bc = st.borderC[i]
			}
			bw[i] = st.borderW[i]
		}
	}
	if bw != [4]float32{} {
		e.BorderWidth(bw[0], bw[1], bw[2], bw[3]).BorderColor(bc)
	}
	for _, sh := range st.shadows[:st.nShadow] {
		if sh.inset {
			e.InsetShadow(sh.x, sh.y, sh.blur, sh.spread, sh.c)
		} else {
			e.Shadow(sh.x, sh.y, sh.blur, sh.spread, sh.c)
		}
	}
	if st.opacity < 1 {
		e.Opacity(st.opacity)
	}
	if st.invisible {
		e.Invisible()
	}
	if st.clip {
		e.Clip()
	}
	if st.pointer {
		e.Cursor(ui.CursorPointer)
	}
	elPlace(e, st)
}

// elMargins sets an element's margins, auto ones taking the free room,
// the top one as collapsed with the block before it.
func elMargins(e ui.Element, st *elStyle) {
	var m [4]float32
	for i, l := range st.margin {
		switch {
		case l.auto:
			m[i] = ui.Auto
		case !l.pct:
			m[i] = l.v
		}
	}
	if st.hasCollapsedTop {
		m[0] = st.collapsedTop
	}
	if m != [4]float32{} {
		e.Margin(m[0], m[1], m[2], m[3])
	}
}

// elPlace sets an element's place in its flex or grid container.
func elPlace(e ui.Element, st *elStyle) {
	if st.grow > 0 {
		e.Grow(st.grow)
	}
	if st.hasShrink {
		e.Shrink(st.shrink)
	}
	if l := st.basis; l.set && !l.auto {
		if l.pct {
			e.BasisPercent(l.v)
		} else {
			e.Basis(l.v)
		}
	}
	if st.hasAlignSel {
		e.AlignSelf(st.alignSelf)
	}
	if st.colStart > 0 {
		e.ColumnStart(st.colStart)
	}
	if st.colSpan > 1 {
		e.ColumnSpan(st.colSpan)
	}
	if st.rowStart > 0 {
		e.RowStart(st.rowStart)
	}
	if st.rowSpan > 1 {
		e.RowSpan(st.rowSpan)
	}
}

// elTextStyle sets the text style an element's text takes, and its
// descendants'.
func elTextStyle(e ui.Element, st *elStyle) {
	e.TextColor(st.color).FontSize(st.fontSize).FontWeight(st.weight)
	if st.italic {
		e.Italic()
	}
	if st.family != "" {
		e.Font(st.family)
	}
	if st.hasTextAlign {
		e.TextAlign(st.textAlign)
	}
	if st.lineHeight > 0 {
		if st.lineFixed {
			e.FixedLineHeight(st.lineHeight)
		} else {
			e.LineHeight(st.lineHeight)
		}
	}
	if st.spacing != 0 {
		e.LetterSpacing(st.spacing)
	}
	if st.underline {
		e.Underline()
	}
	if st.strike {
		e.Strikethrough()
	}
}

// elSpan is a span of text in a style.
func elSpan(st *elStyle, text string) ui.Span {
	return ui.Span{Text: elText(st, text), Font: st.family, Size: st.fontSize, Weight: st.weight, Italic: st.italic,
		Color: st.color, Underline: st.underline, Strikethrough: st.strike, LetterSpacing: st.spacing}
}

// elText is text as its white-space and text-transform draw it: runs of
// white space collapsed but where kept (pre, pre-wrap; pre-line keeps
// the newlines), and cased.
func elText(st *elStyle, s string) string {
	switch st.whiteSpace {
	case "pre", "pre-wrap", "break-spaces":
	default:
		s = elCollapse(s, st.whiteSpace == "pre-line")
	}
	switch st.transform {
	case "uppercase":
		s = strings.ToUpper(s)
	case "lowercase":
		s = strings.ToLower(s)
	case "capitalize":
		rs := []rune(s)
		for i, r := range rs {
			if i == 0 || unicode.IsSpace(rs[i-1]) {
				rs[i] = unicode.ToUpper(r)
			}
		}
		s = string(rs)
	}
	return s
}

// elCollapse collapses runs of white space to one space; with
// newlines, a newline stays.
func elCollapse(s string, newlines bool) string {
	if !strings.ContainsAny(s, "\t\r\n") && !strings.Contains(s, "  ") {
		return s
	}
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == '\n' && newlines:
			b.WriteRune(r)
			space = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if !space {
				b.WriteByte(' ')
			}
			space = true
		default:
			b.WriteRune(r)
			space = false
		}
	}
	return b.String()
}
