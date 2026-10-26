package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// el makes an el node.
func el(id, tag string, props map[string]any, children ...*rex.TSPNode) *rex.TSPNode {
	p := map[string]any{"tag": tag}
	for k, v := range props {
		p[k] = v
	}
	return &rex.TSPNode{ID: id, Kind: "el", Props: p, Children: children}
}

// elTestDoc indexes a surface whose main region holds nodes; controls
// are checked as their props say.
func elTestDoc(nodes ...*rex.TSPNode) *elDoc {
	d := &elDoc{parent: map[*rex.TSPNode]*rex.TSPNode{}, roots: map[*rex.TSPNode]*rex.TSPNode{}}
	d.checked = func(n *rex.TSPNode) bool { on, _ := n.Props["checked"].(bool); return on }
	main := &rex.TSPNode{ID: "main", Kind: "col", Children: nodes}
	d.index(&rex.TSPNode{ID: "s1", Children: []*rex.TSPNode{main}})
	return d
}

// computed is a node's style under sheets, its parent's computed first.
func computed(d *elDoc, n *rex.TSPNode, env *cssEnv, sheets ...*cssSheet) *elStyle {
	base := &elStyle{}
	base.inherit(&elStyle{color: ui.RGB(1, 2, 3), fontSize: 13, weight: 400, whiteSpace: "normal"})
	base.display = "block"
	ps := base
	if p := d.up(n); p != nil {
		ps = computed(d, p, env, sheets...)
	}
	st := &elStyle{}
	st.inherit(ps)
	st.compute(ps, d.cascade(n, append([]*cssSheet{elUASheet}, sheets...), env, nil), env)
	return st
}

// The parser keeps what it can: comments and selector lists parse; an
// unsupported pseudo-class drops its whole rule, an unknown property its
// declaration, an unsupported at-rule itself, a stray brace nothing
// after it; each is reported with its place.
func TestCSSParse(t *testing.T) {
	sh := parseCSS(`/* head */ .a, .b { color: red /* inline */ }
.c:enabled { color: blue }
@import "x.css";
.d { colr: red; width: 2px; background: url(x.png) }
}
.e { margin: 0 !important }
@media (min-width: 10px) { .f { color: red } }
@keyframes spin { from { opacity: 0 } }
.g::before { content: "x" }
.h { & .i { color: red } color: blue }`)
	var got []string
	for _, r := range sh.rules {
		var decls []string
		for _, d := range r.decls {
			s := d.prop + ":" + d.val
			if d.important {
				s += "!"
			}
			decls = append(decls, s)
		}
		got = append(got, strings.Join(decls, ";"))
	}
	want := []string{"color:red", "color:red", "width:2px", "margin:0!", "color:red", "color:blue"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rules %q, want %q", got, want)
	}
	if sh.rules[0].order != sh.rules[1].order || sh.rules[4].order >= sh.rules[5].order {
		t.Errorf("orders %d %d, nested %d after %d", sh.rules[0].order, sh.rules[1].order, sh.rules[4].order, sh.rules[5].order)
	}
	errs := strings.Join(sh.errs, "\n")
	for _, msg := range []string{"2:1: unsupported pseudo-class `:enabled`", "3:1: unsupported at-rule", "4:6: unknown property in `colr: red`",
		"4:29: url() dropped: background outside the surface", "5:1: unexpected `}`", "7:1: unsupported media query"} {
		if !strings.Contains(errs, msg) {
			t.Errorf("errors lack %q:\n%s", msg, errs)
		}
	}
	if len(sh.errs) != 6 {
		t.Errorf("%d errors:\n%s", len(sh.errs), errs)
	}
}

// Later wins at equal specificity, more specific wins whatever its
// place, !important wins over both, and the user agent's sheet loses to
// any author rule.
func TestCSSCascade(t *testing.T) {
	p := el("p", "p", map[string]any{"class": "x y"})
	d := elTestDoc(p)
	env := &cssEnv{}
	color := func(css string) ui.Color { return computed(d, p, env, parseCSS(css)).color }
	red, blue, green := ui.RGB(255, 0, 0), ui.RGB(0, 0, 255), ui.RGB(0, 128, 0)
	for _, c := range []struct {
		css  string
		want ui.Color
	}{
		{".x { color: red } .y { color: blue }", blue},
		{".x.y { color: red } .y { color: blue }", red},
		{"[data-id='p'] { color: green } p { color: red }", green},
		{".x { color: red !important } .x.y { color: blue }", red},
		{"p { color: red } p { color: inherit }", ui.RGB(1, 2, 3)},
	} {
		if got := color(c.css); got != c.want {
			t.Errorf("%s: %v, want %v", c.css, got, c.want)
		}
	}
	st := computed(d, p, env, parseCSS("p { margin-top: 3px }"))
	if st.margin[0].v != 3 || st.margin[2].v != 13 {
		t.Errorf("p margins %+v, want 3px over the user agent's 1em top, 13px bottom", st.margin)
	}
}

// Selectors match els by tag, class, attribute and state, through
// descendant, child and sibling combinators, never the region itself.
func TestCSSMatch(t *testing.T) {
	r1 := el("r1", "input", map[string]any{"type": "radio", "name": "size", "checked": true})
	s1 := el("s1", "span", map[string]any{"text": "Small"})
	r2 := el("r2", "input", map[string]any{"type": "radio", "name": "size", "disabled": true})
	s2 := el("s2", "span", map[string]any{"text": "Large", "tone": "warning", "attrs": map[string]any{"data-col": 2.0, "style": "x"}})
	l1 := el("l1", "label", nil, r1, s1)
	l2 := el("l2", "label", nil, r2, s2)
	form := el("f", "form", map[string]any{"class": "ask sf-x"}, l1, l2)
	d := elTestDoc(form)
	for _, c := range []struct {
		sel  string
		n    *rex.TSPNode
		want bool
	}{
		{".ask :checked+span", s1, true},
		{".ask :checked+span", s2, false},
		{":disabled + span", s2, true},
		{"[role='radio']:not(:checked) ~ span", s2, true},
		{"[aria-checked=true]", r1, true},
		{"div.ask", form, true},
		{"form", form, false},
		{".sf-x", form, false},
		{".sf-el.ask > label > span", s1, true},
		{".ask > span", s1, false},
		{".ask span", s2, true},
		{"label:first-child span", s1, true},
		{"label:last-child span", s1, false},
		{"label:nth-child(2n) span:last-child", s2, true},
		{"[data-tone='warning'][data-col='2']", s2, true},
		{"[style]", s2, false},
		{"[data-id='main'] .ask", form, false},
		{".sf-col .ask", form, false},
		{":is(.nope, .ask)", form, true},
		{":where(.ask) label", l1, true},
	} {
		sel, err := compileSelector(c.sel)
		if err != "" {
			t.Errorf("%s: %s", c.sel, err)
			continue
		}
		if got := d.matches(sel.sel, c.n); got != c.want {
			t.Errorf("%s on %s: %v, want %v", c.sel, c.n.ID, got, c.want)
		}
	}
	spec := func(s string) int { sel, _ := compileSelector(s); return sel.spec }
	if !(spec("p") < spec(".a") && spec(".a") == spec("[x]") && spec(".a") == spec(":hover") &&
		spec("p.a") < spec(".a.b") && spec(":where(.a.b) p") == spec("p") && spec(":not(.a, p)") == spec(".a")) {
		t.Error("specificity")
	}
}

// var() takes a custom property an ancestor declares, else Tern's, else
// the fallback, nested; one that resolves to nothing leaves the property
// unset.
func TestCSSVar(t *testing.T) {
	span := el("s", "span", nil)
	div := el("d", "div", map[string]any{"class": "box"}, span)
	d := elTestDoc(div)
	env := &cssEnv{vars: map[string]string{"--accent": "#00ff00"}}
	sh := parseCSS(`.box { --x: #0000ff; --gap: 3px }
span { color: var(--x); padding: var(--nope, var(--gap)); margin-left: calc(2 * var(--gap, 1px) + 1em) }
.box { border-color: var(--accent); background: var(--missing); color: light-dark(#ff0000, #0000ff) }`)
	st := computed(d, span, env, sh)
	if st.color != ui.RGB(0, 0, 255) {
		t.Errorf("color %v from an ancestor's --x", st.color)
	}
	if st.pad[0].v != 3 || st.pad[3].v != 3 {
		t.Errorf("padding %+v from the nested fallback", st.pad)
	}
	if st.margin[3].v != 19 {
		t.Errorf("margin-left %v, want 2*3px + 1em of 13px", st.margin[3].v)
	}
	box := computed(d, div, env, sh)
	if box.borderC[0] != ui.RGB(0, 255, 0) || box.bg.A != 0 || box.color != ui.RGB(255, 0, 0) {
		t.Errorf("box border %v, background %v, color %v", box.borderC[0], box.bg, box.color)
	}
	if c, ok := cssColor("rgb(from #ff8000 r g b / 10%)", ui.Color{}, false); !ok || c != ui.RGBA(255, 128, 0, 0.1) {
		t.Errorf("relative color %v", c)
	}
	if c, ok := cssColor("hsl(120 100% 25%)", ui.Color{}, false); !ok || c != ui.RGB(0, 128, 0) {
		t.Errorf("hsl %v", c)
	}
}

// A form's values: a radio group's pick (nil for none), a lone
// checkbox's state, several checkboxes' checked values; disabled and
// unnamed controls and nested forms left out. A radio's group is its
// name in its nearest form, or outside every form.
func TestElFormValues(t *testing.T) {
	ctl := func(id, typ, name, value string, props ...any) *rex.TSPNode {
		p := map[string]any{"type": typ}
		if name != "" {
			p["name"] = name
		}
		if value != "" {
			p["value"] = value
		}
		for i := 0; i+1 < len(props); i += 2 {
			p[props[i].(string)] = props[i+1]
		}
		return el(id, "input", p)
	}
	s, l := ctl("s", "radio", "size", "s"), ctl("l", "radio", "size", "l")
	a, b := ctl("a", "checkbox", "tags", "a", "checked", true), ctl("b", "checkbox", "tags", "b")
	inner := ctl("in", "radio", "size", "x", "checked", true)
	out1, out2 := ctl("o1", "radio", "size", "1", "checked", true), ctl("o2", "radio", "size", "2")
	form := el("f", "form", nil, s, el("w", "div", nil, l),
		ctl("n", "checkbox", "notify", "", "checked", true), a, b, ctl("c", "checkbox", "tags", "c", "checked", true),
		ctl("off", "checkbox", "off", "", "checked", true, "disabled", true), ctl("anon", "checkbox", "", "", "checked", true),
		el("nested", "form", nil, inner))
	e := &tspEls{flips: map[string]elFlip{}, sf: &rex.Surface{Image: 1}}
	e.doc = *elTestDoc(form, out1, el("x", "div", nil, out2))
	e.doc.checked = e.checked
	got := e.doc.values(form)
	want := map[string]any{"size": nil, "notify": true, "tags": []string{"a", "c"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("values %v, want %v", got, want)
	}
	if !e.flip(l) || !e.flip(b) {
		t.Fatal("no flip")
	}
	want = map[string]any{"size": "l", "notify": true, "tags": []string{"a", "b", "c"}}
	if got := e.doc.values(form); !reflect.DeepEqual(got, want) {
		t.Errorf("after flips %v, want %v", got, want)
	}
	if e.flip(l) {
		t.Error("a checked radio flipped")
	}
	if !e.checked(inner) || !e.checked(out1) {
		t.Error("another form's radio, or one outside, unchecked by the form's")
	}
	if g := e.doc.group(out2); len(g) != 2 || g[0] != out1 {
		t.Errorf("the group outside forms: %v", g)
	}
	e.flip(out2)
	if e.checked(out1) || !e.checked(out2) {
		t.Error("outside forms, the group did not move")
	}
	if e.doc.values(form)["size"] != "l" {
		t.Error("a radio outside the form changed its values")
	}
	// The program's checked, changed, wins over the flip.
	b.Props["checked"] = true
	if !e.checked(b) || len(e.flips) != 3 {
		t.Errorf("the program's checked: %v, flips %v", e.checked(b), e.flips)
	}
	b.Props["checked"] = false
	if e.checked(b) {
		t.Error("an old flip came back")
	}
}

// What a program sends can neither crash the window nor make it do
// unbounded work: a track list cut off, a color whose lowercase is
// longer, var() references fanning out, repeat()s and & nesting
// multiplying.
func TestCSSHostile(t *testing.T) {
	env := &cssEnv{}
	for _, v := range []string{"repeat(", "minmax(", "repeat(1,]x repeat()", "repeat(2,minmax()", "repeat(1"} {
		cssTracks(v, 13, env)
	}
	if got := len(cssTracks("repeat(64,repeat(64,repeat(64,repeat(64,repeat(64,1px)))))", 13, env)); got != cssMaxTracks {
		t.Errorf("nested repeat() made %d tracks, want %d", got, cssMaxTracks)
	}
	if got := cssTracks("repeat(64,repeat(64,repeat(64,repeat(64,repeat(64,minmax(1px))))))", 13, env); len(got) != 0 {
		t.Errorf("nested repeat() of nothing made %d tracks", len(got))
	}
	if got := cssTracks("repeat(2, 1fr 10px)", 13, env); len(got) != 4 {
		t.Errorf("repeat(2, 1fr 10px): %d tracks", len(got))
	}
	for _, s := range []string{"Ⱥ()", "ȺȺȺ(1)", "rgb(Ⱥ)"} {
		if _, ok := cssColor(s, ui.Color{}, false); ok {
			t.Errorf("%s read as a color", s)
		}
	}
	if c, ok := cssColor("RGB(255, 0, 0)", ui.Color{}, false); !ok || c != ui.RGB(255, 0, 0) {
		t.Errorf("RGB() in capitals: %v", c)
	}
	sh := parseCSS(`.a { grid-template-columns: repeat(`)
	if len(sh.errs) == 0 {
		t.Error("a sheet cut off reported nothing")
	}

	// --vN is var(--vN-1) twice: 2^30 references, resolving to 2^30
	// bytes, or to nothing.
	for _, v0 := range []string{"x", ""} {
		vars := map[string]string{"--v0": v0}
		for i := 1; i <= 30; i++ {
			vars[fmt.Sprintf("--v%d", i)] = fmt.Sprintf("var(--v%d)var(--v%d)", i-1, i-1)
		}
		st := &elStyle{}
		if _, ok := st.resolve("var(--v30)", &cssEnv{vars: vars}); ok {
			t.Errorf("var() fanning out from %q resolved", v0)
		}
		if got, ok := st.resolve("var(--v3)", &cssEnv{vars: vars}); !ok || got != strings.Repeat(v0, 8) {
			t.Errorf("var(--v3) from %q: %q, %v", v0, got, ok)
		}
	}

	// Eight & a level, twelve levels: 8^12 selectors written out.
	css := ".a {" + strings.Repeat("& & & & & & & & {", 12) + "color: red" + strings.Repeat("}", 13)
	sh = parseCSS(css)
	if !strings.Contains(strings.Join(sh.errs, "\n"), "nesting too large") {
		t.Errorf("deep nesting: %q", sh.errs)
	}
	sh = parseCSS(".a { .b { color: red } & + .c { color: blue } }")
	if len(sh.errs) != 0 || len(sh.rules) != 2 {
		t.Errorf("shallow nesting: %d rules, %q", len(sh.rules), sh.errs)
	}

	// A selector list of 2000 rules, each declaration list flushed
	// over and over by an empty @media.
	sels := make([]string, 2000)
	for i := range sels {
		sels[i] = fmt.Sprintf(".s%d", i)
	}
	sh = parseCSS(strings.Join(sels, ",") + "{" + strings.Repeat("color:red;@media all{}", 2000) + "}")
	if len(sh.rules) > cssMaxRules || len(sh.errs) > 64 {
		t.Errorf("%d rules, %d errors", len(sh.rules), len(sh.errs))
	}
}

// A table's cells are placed within its row, column and area caps,
// however many cells span however far.
func TestElTableCaps(t *testing.T) {
	td := func(id string) *rex.TSPNode {
		return el(id, "td", map[string]any{"attrs": map[string]any{"colspan": "64", "rowspan": "64"}})
	}
	var rows []*rex.TSPNode
	for r := range 2000 {
		var cells []*rex.TSPNode
		for c := range 5 {
			cells = append(cells, td(fmt.Sprintf("c%d-%d", r, c)))
		}
		rows = append(rows, el(fmt.Sprintf("r%d", r), "tr", nil, cells...))
	}
	table := el("t", "table", nil, rows...)
	sf := &rex.Surface{Image: 1}
	e := &tspEls{sf: sf, styles: map[*rex.TSPNode]*elStyle{}, surf: map[*rex.Surface]elSheets{sf: {list: []*cssSheet{elUASheet}}}}
	e.doc = *elTestDoc(table)
	cells, ncols := e.tableCells(table)
	if ncols > elTableCols {
		t.Errorf("%d columns", ncols)
	}
	area := 0
	for _, c := range cells {
		if c.row >= elTableRows || c.col+c.cols > elTableCols {
			t.Fatalf("a cell placed past the caps: %+v", c)
		}
		area += c.rows * c.cols
	}
	if area > elTableArea {
		t.Errorf("cells over %d slots", area)
	}
	// An ordinary table is placed as before: a cell spanning two rows
	// pushes the next row's first cell right.
	a := el("a", "td", map[string]any{"attrs": map[string]any{"rowspan": "2"}})
	small := el("s", "table", nil, el("r1", "tr", nil, a, el("b", "td", nil)), el("r2", "tr", nil, el("c", "td", nil)))
	e.doc = *elTestDoc(small)
	cells, ncols = e.tableCells(small)
	if ncols != 2 || len(cells) != 3 || cells[2].row != 1 || cells[2].col != 1 || cells[0].rows != 2 {
		t.Errorf("small table: %d columns, %+v", ncols, cells)
	}
}

// The pane's el state drops what its frames have not drawn for elKeep
// of them: sheets, errors, checked shown; flips past elKeepFlips.
func TestElStatePrune(t *testing.T) {
	e := &tspEls{flips: map[string]elFlip{}, shown: map[string]elShown{}, errs: map[string]elErr{},
		sheetErrs: map[string]string{}, sheets: map[string]*elParsed{}}
	e.count = 10 * elKeep
	fresh, stale := e.count-1, e.count-elKeep-1
	e.sheets["old"], e.sheets["new"] = &elParsed{seen: stale}, &elParsed{seen: fresh}
	e.sheetErrs["1/old"], e.sheetErrs["1/new"] = "old", "new"
	e.errs["1/old"], e.errs["1/new"] = elErr{seen: stale}, elErr{seen: fresh}
	e.shown["1/old"], e.shown["1/new"] = elShown{seen: stale}, elShown{seen: fresh}
	for i := range elKeepFlips {
		e.flips[fmt.Sprintf("1/f%d", i)] = elFlip{seen: stale}
	}
	e.prune()
	if len(e.sheets) != 1 || e.sheets["new"] == nil || len(e.sheetErrs) != 1 || e.sheetErrs["1/new"] == "" {
		t.Errorf("sheets %v, sheet errors %v", e.sheets, e.sheetErrs)
	}
	if _, ok := e.errs["1/new"]; !ok || len(e.errs) != 1 {
		t.Errorf("errors %v", e.errs)
	}
	if _, ok := e.shown["1/new"]; !ok || len(e.shown) != 1 {
		t.Errorf("shown %v", e.shown)
	}
	if len(e.flips) != elKeepFlips {
		t.Errorf("%d flips, within elKeepFlips, pruned", len(e.flips))
	}
	e.flips["1/new"] = elFlip{seen: fresh}
	e.prune()
	if _, ok := e.flips["1/new"]; !ok || len(e.flips) != 1 {
		t.Errorf("%d flips past elKeepFlips", len(e.flips))
	}
}
