package main

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A program's style sheets (verb s, feature styles) restyle the el nodes
// of its surface: the subset of CSS of Tern's engine (styles/css.md),
// parsed once per sheet text into flat rules, matched against the
// surface's document and folded into a computed style per element
// (elStyle), which tsp_el.go draws in MyGo's boxes. Every selector is
// confined to the surface: it matches inside its regions, never a region
// itself.

// cssSheet is a style sheet parsed: its style rules, nesting and @media
// flattened into rules of their own in sheet order, and the problems
// found, as the protocol reports them ("line:col: message in `snippet`").
type cssSheet struct {
	rules []cssRule
	errs  []string
	// hover tells that a rule tests :hover, for which the window follows
	// the pointer over the surface's els.
	hover bool
}

// cssRule is one selector of a style rule with the rule's declarations;
// a selector list makes a rule per selector, of the same order.
type cssRule struct {
	sel   []cssCompound
	spec  int
	order int
	decls []cssDecl
	media []cssMedia
}

type cssDecl struct {
	prop, val string
	important bool
}

// cssMedia is one condition of an @media block: max-width or max-height
// in px, or a preference with its value. All of a rule's hold for it.
type cssMedia struct {
	feature, value string
	px             float32
}

// cssCompound is a compound selector, with the combinator that relates it
// to the one before it (' ', '>', '+' or '~'; 0 for the first).
type cssCompound struct {
	comb    byte
	tag     string
	ids     []string
	classes []string
	attrs   []cssAttrSel
	pseudos []cssPseudo
}

type cssAttrSel struct {
	name, op, val string
	fold          bool
}

// cssPseudo is a pseudo-class: its name, the compound selectors of
// :not(), :is(), :where() and nth's "of S", and nth's An+B.
type cssPseudo struct {
	name string
	args []cssCompound
	a, b int
}

// cssSel is a selector as written (for nesting) and compiled. A selector
// ending in a pseudo-element is valid but dead: the window draws no
// generated boxes.
type cssSel struct {
	text  string
	sel   []cssCompound
	spec  int
	dead  bool
	hover bool
}

// parseCSS parses a sheet. It never fails: what it cannot use it drops,
// with an error, and keeps the rest.
func parseCSS(src string) *cssSheet {
	p := &cssParser{src: cssStripComments(src), sh: &cssSheet{}}
	p.rules(nil, false)
	return p.sh
}

// cssMaxRules bounds a sheet's rules, and cssNestBytes the selectors its
// nested rules write out, in all.
const (
	cssMaxRules  = 4096
	cssNestBytes = 256 << 10
)

type cssParser struct {
	src   string
	pos   int
	order int
	sh    *cssSheet
	// nest is what nesting wrote out so far; full tells the rules came
	// to cssMaxRules.
	nest int
	full bool
}

// errAt records an error at a byte offset of the sheet, up to 64 of
// them: past those it records no more, each costing a scan of the sheet.
func (p *cssParser) errAt(at int, msg, snippet string) {
	if len(p.sh.errs) >= 64 {
		return
	}
	line, col := 1, 1
	for i := 0; i < at && i < len(p.src); i++ {
		if p.src[i] == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	s := strconv.Itoa(line) + ":" + strconv.Itoa(col) + ": " + msg
	if snippet != "" {
		s += " in `" + snippet + "`"
	}
	p.sh.errs = append(p.sh.errs, s)
}

func (p *cssParser) ws() {
	for p.pos < len(p.src) && cssSpace(p.src[p.pos]) {
		p.pos++
	}
}

// scan finds the end of a prelude or a declaration from pos: the first
// ';', '{' or '}' outside parentheses, brackets and strings, or the end
// (stop 0).
func (p *cssParser) scan() (end int, stop byte) {
	depth := 0
	for i := p.pos; i < len(p.src); i++ {
		switch ch := p.src[i]; ch {
		case '"', '\'':
			i = cssStringEnd(p.src, i)
		case '\\':
			i++
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ';', '{', '}':
			if depth == 0 {
				return i, ch
			}
		}
	}
	return len(p.src), 0
}

// skipBlock skips a block whose '{' was read, through its '}'.
func (p *cssParser) skipBlock() {
	depth := 1
	for ; p.pos < len(p.src); p.pos++ {
		switch p.src[p.pos] {
		case '"', '\'':
			p.pos = cssStringEnd(p.src, p.pos)
		case '\\':
			p.pos++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				p.pos++
				return
			}
		}
	}
}

// rules parses rules and at-rules: the sheet's, or an @media block's
// (inBlock, ended by its '}').
func (p *cssParser) rules(media []cssMedia, inBlock bool) {
	for {
		p.ws()
		if p.pos >= len(p.src) {
			if inBlock {
				p.errAt(len(p.src), "unclosed block", "")
			}
			return
		}
		switch p.src[p.pos] {
		case '}':
			if inBlock {
				p.pos++
				return
			}
			p.errAt(p.pos, "unexpected `}`", "")
			p.pos++
			continue
		case '@':
			p.atRule(nil, media, false)
			continue
		}
		start := p.pos
		end, stop := p.scan()
		text := strings.TrimSpace(p.src[start:end])
		switch stop {
		case '{':
			p.pos = end + 1
			p.ruleOf(cssSplit(text, ','), text, start, media)
		case '}':
			p.errAt(start, "expected a rule", text)
			p.pos = end
		default:
			p.errAt(start, "expected a rule", text)
			p.pos = min(end+1, len(p.src))
		}
	}
}

// atRule parses an at-rule at pos: @media, in a style rule (nested,
// with its selectors) too; @keyframes and @font-face are skipped, as the
// window runs no animations and loads no fonts; any other is an error.
func (p *cssParser) atRule(sels []cssSel, media []cssMedia, nested bool) {
	start := p.pos
	p.pos++
	nameEnd := p.pos
	for nameEnd < len(p.src) && cssIdentByte(p.src[nameEnd]) {
		nameEnd++
	}
	name := strings.ToLower(p.src[p.pos:nameEnd])
	p.pos = nameEnd
	end, stop := p.scan()
	prelude := strings.TrimSpace(p.src[nameEnd:end])
	if stop != '{' {
		p.errAt(start, "unsupported at-rule", "@"+name)
		if stop == ';' {
			end++
		}
		p.pos = end
		return
	}
	p.pos = end + 1
	switch {
	case name == "media":
		cond, ok := parseMedia(prelude)
		if !ok {
			p.errAt(start, "unsupported media query", "@media "+prelude)
			p.skipBlock()
			return
		}
		all := append(slices.Clip(media), cond...)
		if nested {
			p.body(sels, all)
		} else {
			p.rules(all, true)
		}
	case !nested && (name == "keyframes" || name == "-webkit-keyframes" || name == "font-face"):
		p.skipBlock()
	default:
		p.errAt(start, "unsupported at-rule", "@"+name)
		p.skipBlock()
	}
}

// ruleOf compiles a rule's selectors, written out, and parses its body;
// one invalid selector drops the whole rule.
func (p *cssParser) ruleOf(texts []string, prelude string, at int, media []cssMedia) {
	sels := make([]cssSel, 0, len(texts))
	for _, t := range texts {
		s, err := compileSelector(t)
		if err != "" {
			p.errAt(at, err, prelude+" {")
			p.skipBlock()
			return
		}
		p.sh.hover = p.sh.hover || s.hover && !s.dead
		sels = append(sels, s)
	}
	p.body(sels, media)
}

// body parses a style rule's body, whose '{' was read: declarations,
// which make rules of its selectors, and nested rules and @media, which
// flatten into rules after them. Declarations after a nested rule make a
// rule of their own, later, as CSS Nesting has it.
func (p *cssParser) body(sels []cssSel, media []cssMedia) {
	var decls []cssDecl
	flush := func() {
		if len(decls) == 0 {
			return
		}
		if len(p.sh.rules)+len(sels) > cssMaxRules {
			if !p.full {
				p.full = true
				p.errAt(p.pos, "too many rules: the rest dropped", "")
			}
			decls = nil
			return
		}
		for _, s := range sels {
			if !s.dead {
				p.sh.rules = append(p.sh.rules, cssRule{sel: s.sel, spec: s.spec, order: p.order, decls: decls, media: media})
			}
		}
		p.order++
		decls = nil
	}
	for {
		p.ws()
		if p.pos >= len(p.src) {
			p.errAt(len(p.src), "unclosed block", "")
			flush()
			return
		}
		switch p.src[p.pos] {
		case '}':
			p.pos++
			flush()
			return
		case ';':
			p.pos++
			continue
		case '@':
			flush()
			p.atRule(sels, media, true)
			continue
		}
		start := p.pos
		end, stop := p.scan()
		text := strings.TrimSpace(p.src[start:end])
		if stop == '{' {
			flush()
			p.pos = end + 1
			p.nested(text, start, sels, media)
			continue
		}
		p.pos = end
		if stop == ';' {
			p.pos++
		}
		if d, ok := p.decl(text, start); ok {
			decls = append(decls, d)
		}
	}
}

// nested writes out a nested rule's selectors over its parent's: & is
// the parent selector, and a selector without one is its descendant (or
// joins it by its leading combinator). What nesting writes out, which
// multiplies at each level, comes to cssNestBytes for the sheet.
func (p *cssParser) nested(prelude string, at int, parents []cssSel, media []cssMedia) {
	var texts []string
	for _, part := range cssSplit(prelude, ',') {
		amps := strings.Count(part, "&")
		for _, ps := range parents {
			size := len(part) + 1 + len(ps.text)
			if amps > 0 {
				size = len(part) + amps*len(ps.text)
			}
			if p.nest += size; p.nest > cssNestBytes {
				p.errAt(at, "nesting too large", prelude+" {")
				p.skipBlock()
				return
			}
			if amps > 0 {
				texts = append(texts, strings.ReplaceAll(part, "&", ps.text))
			} else {
				texts = append(texts, ps.text+" "+part)
			}
		}
	}
	if len(texts) == 0 {
		p.skipBlock()
		return
	}
	p.ruleOf(texts, prelude, at, media)
}

// decl parses a declaration. Unknown properties are errors; properties
// kept for web sheets' sake are dropped silently; what could reach out
// of the surface (url(), position: fixed) is dropped with an error.
func (p *cssParser) decl(text string, at int) (cssDecl, bool) {
	colon := strings.IndexByte(text, ':')
	if colon <= 0 {
		p.errAt(at, "invalid declaration", text)
		return cssDecl{}, false
	}
	prop := strings.TrimSpace(text[:colon])
	val := strings.TrimSpace(text[colon+1:])
	important := false
	if i := strings.LastIndexByte(val, '!'); i >= 0 && strings.EqualFold(strings.TrimSpace(val[i+1:]), "important") {
		important, val = true, strings.TrimSpace(val[:i])
	}
	low := strings.ToLower(val)
	if !strings.HasPrefix(prop, "--") {
		prop = strings.ToLower(prop)
		if alias, ok := cssAliases[prop]; ok {
			prop = alias
		}
		if cssIgnored[prop] {
			return cssDecl{}, false
		}
		if !cssKnown[prop] {
			p.errAt(at, "unknown property", text)
			return cssDecl{}, false
		}
		if val == "" {
			p.errAt(at, "missing value", text)
			return cssDecl{}, false
		}
		if prop == "position" && (low == "fixed" || strings.Contains(low, "var(")) {
			p.errAt(at, "position: fixed dropped: position outside the surface", text)
			return cssDecl{}, false
		}
	}
	if strings.Contains(low, "url(") {
		p.errAt(at, "url() dropped: "+prop+" outside the surface", text)
		return cssDecl{}, false
	}
	return cssDecl{prop, val, important}, true
}

// parseMedia reads an @media prelude: features joined with and, of
// max-width and max-height in px and the three preferences; screen,
// all and only are ignored. Anything else fails the block.
func parseMedia(q string) ([]cssMedia, bool) {
	var out []cssMedia
	for _, tok := range cssFields(strings.ToLower(q)) {
		switch tok {
		case "screen", "all", "only", "and":
			continue
		}
		if len(tok) < 2 || tok[0] != '(' || tok[len(tok)-1] != ')' {
			return nil, false
		}
		feature, value, ok := strings.Cut(tok[1:len(tok)-1], ":")
		if !ok {
			return nil, false
		}
		feature, value = strings.TrimSpace(feature), strings.TrimSpace(value)
		switch feature {
		case "max-width", "max-height":
			n, ok := strings.CutSuffix(value, "px")
			f, err := strconv.ParseFloat(n, 32)
			if !ok || err != nil {
				return nil, false
			}
			out = append(out, cssMedia{feature: feature, px: float32(f)})
		case "prefers-reduced-motion":
			if value != "reduce" {
				return nil, false
			}
			out = append(out, cssMedia{feature: feature, value: value})
		case "prefers-reduced-transparency", "prefers-contrast":
			if value != "reduce" && value != "more" && value != "no-preference" {
				return nil, false
			}
			out = append(out, cssMedia{feature: feature, value: value})
		default:
			return nil, false
		}
	}
	return out, true
}

// mediaOK tells whether a rule's @media conditions hold in the window.
func mediaOK(media []cssMedia, env *cssEnv) bool {
	for _, m := range media {
		var ok bool
		switch m.feature {
		case "max-width":
			ok = env.vw <= m.px
		case "max-height":
			ok = env.vh <= m.px
		case "prefers-reduced-motion":
			ok = env.reduceMotion
		case "prefers-reduced-transparency":
			ok = m.value == "no-preference"
		case "prefers-contrast":
			ok = env.highContrast == (m.value == "more")
		}
		if !ok {
			return false
		}
	}
	return true
}

// compileSelector compiles a complex selector: compounds joined by
// descendant, child, next- and subsequent-sibling combinators.
func compileSelector(text string) (cssSel, string) {
	q := &cssSelParser{s: strings.TrimSpace(text)}
	out := cssSel{text: q.s}
	var comb byte
	for {
		c, spec, pe, err := q.compound(false)
		if err != "" {
			return out, err
		}
		c.comb = comb
		out.sel = append(out.sel, c)
		out.spec += spec
		if pe {
			out.dead = true
			q.skipWS()
			if q.i < len(q.s) {
				return out, "a pseudo-element must end the selector"
			}
			break
		}
		ws := q.skipWS()
		if q.i >= len(q.s) {
			break
		}
		switch q.s[q.i] {
		case '>', '+', '~':
			comb = q.s[q.i]
			q.i++
			q.skipWS()
		default:
			if !ws {
				return out, "unsupported selector"
			}
			comb = ' '
		}
		if q.i >= len(q.s) {
			return out, "the selector ends in a combinator"
		}
	}
	out.hover = q.hover
	return out, ""
}

type cssSelParser struct {
	s     string
	i     int
	hover bool
}

func (q *cssSelParser) skipWS() bool {
	at := q.i
	for q.i < len(q.s) && cssSpace(q.s[q.i]) {
		q.i++
	}
	return q.i > at
}

// ident reads an identifier, its escapes taken literally.
func (q *cssSelParser) ident() string {
	start := q.i
	escaped := false
	for q.i < len(q.s) {
		ch := q.s[q.i]
		if ch == '\\' && q.i+1 < len(q.s) {
			escaped = true
			q.i += 2
			continue
		}
		if !cssIdentByte(ch) {
			break
		}
		q.i++
	}
	s := q.s[start:q.i]
	if escaped {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
			}
			b.WriteByte(s[i])
		}
		s = b.String()
	}
	return s
}

// compound reads a compound selector; pe tells that it ended in a
// pseudo-element. In an argument (inArg) pseudo-elements are errors.
func (q *cssSelParser) compound(inArg bool) (c cssCompound, spec int, pe bool, err string) {
	start := q.i
	if q.i < len(q.s) && q.s[q.i] == '*' {
		q.i++
	} else if id := q.ident(); id != "" {
		c.tag = strings.ToLower(id)
		spec++
	}
loop:
	for q.i < len(q.s) {
		switch q.s[q.i] {
		case '#':
			q.i++
			id := q.ident()
			if id == "" {
				return c, 0, false, "expected an id"
			}
			c.ids = append(c.ids, id)
			spec += 1 << 16
		case '.':
			q.i++
			cl := q.ident()
			if cl == "" {
				return c, 0, false, "expected a class"
			}
			c.classes = append(c.classes, cl)
			spec += 1 << 8
		case '[':
			a, e := q.attr()
			if e != "" {
				return c, 0, false, e
			}
			c.attrs = append(c.attrs, a)
			spec += 1 << 8
		case ':':
			q.i++
			if q.i < len(q.s) && q.s[q.i] == ':' {
				q.i++
				name := strings.ToLower(q.ident())
				if !cssPseudoElements[name] {
					return c, 0, false, "unsupported pseudo-element `::" + name + "`"
				}
				if inArg {
					return c, 0, false, "a pseudo-element in a selector argument"
				}
				return c, spec + 1, true, ""
			}
			name := strings.ToLower(q.ident())
			if name == "before" || name == "after" {
				if inArg {
					return c, 0, false, "a pseudo-element in a selector argument"
				}
				return c, spec + 1, true, ""
			}
			ps, s, e := q.pseudo(name)
			if e != "" {
				return c, 0, false, e
			}
			c.pseudos = append(c.pseudos, ps)
			spec += s
		default:
			break loop
		}
	}
	if q.i == start {
		return c, 0, false, "expected a selector"
	}
	return c, spec, false, ""
}

// attr reads an attribute selector: [name], or [name op value flag].
func (q *cssSelParser) attr() (cssAttrSel, string) {
	q.i++
	q.skipWS()
	a := cssAttrSel{name: strings.ToLower(q.ident())}
	if a.name == "" {
		return a, "expected an attribute name"
	}
	q.skipWS()
	if q.i < len(q.s) && q.s[q.i] == ']' {
		q.i++
		return a, ""
	}
	if q.i < len(q.s) && q.s[q.i] == '=' {
		a.op = "="
		q.i++
	} else if q.i+1 < len(q.s) && strings.IndexByte("~|^$*", q.s[q.i]) >= 0 && q.s[q.i+1] == '=' {
		a.op = q.s[q.i : q.i+2]
		q.i += 2
	} else {
		return a, "unsupported attribute selector"
	}
	q.skipWS()
	if q.i < len(q.s) && (q.s[q.i] == '"' || q.s[q.i] == '\'') {
		end := cssStringEnd(q.s, q.i)
		a.val = cssUnquote(q.s[q.i : end+1])
		q.i = end + 1
	} else {
		a.val = q.ident()
	}
	q.skipWS()
	if q.i < len(q.s) && (q.s[q.i] == 'i' || q.s[q.i] == 'I' || q.s[q.i] == 's' || q.s[q.i] == 'S') {
		a.fold = q.s[q.i] == 'i' || q.s[q.i] == 'I'
		q.i++
		q.skipWS()
	}
	if q.i >= len(q.s) || q.s[q.i] != ']' {
		return a, "unclosed attribute selector"
	}
	q.i++
	return a, ""
}

// pseudo reads a pseudo-class after its name, with its arguments, and
// returns its specificity.
func (q *cssSelParser) pseudo(name string) (cssPseudo, int, string) {
	ps := cssPseudo{name: name}
	switch name {
	case "hover":
		q.hover = true
		return ps, 1 << 8, ""
	case "checked", "disabled", "active", "focus", "focus-visible", "focus-within",
		"first-child", "last-child", "only-child", "first-of-type", "last-of-type", "root", "empty":
		return ps, 1 << 8, ""
	case "not", "is", "where":
		inner, ok := q.paren()
		if !ok {
			return ps, 0, "expected `(` after `:" + name + "`"
		}
		args, spec, err := compileCompounds(inner)
		if err != "" {
			return ps, 0, err
		}
		ps.args = args
		if name == "where" {
			spec = 0
		}
		q.hover = q.hover || strings.Contains(inner, ":hover")
		return ps, spec, ""
	case "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		inner, ok := q.paren()
		if !ok {
			return ps, 0, "expected `(` after `:" + name + "`"
		}
		expr, of, hasOf := strings.Cut(inner, " of ")
		var ok2 bool
		if ps.a, ps.b, ok2 = parseNth(expr); !ok2 {
			return ps, 0, "invalid `:" + name + "(" + inner + ")`"
		}
		spec := 1 << 8
		if hasOf {
			if strings.HasSuffix(name, "of-type") {
				return ps, 0, "`of S` in `:" + name + "`"
			}
			args, s, err := compileCompounds(of)
			if err != "" {
				return ps, 0, err
			}
			ps.args, spec = args, spec+s
		}
		return ps, spec, ""
	}
	return ps, 0, "unsupported pseudo-class `:" + name + "`"
}

// paren reads a parenthesized argument at i, returning what is inside.
func (q *cssSelParser) paren() (string, bool) {
	if q.i >= len(q.s) || q.s[q.i] != '(' {
		return "", false
	}
	end := cssCloseParen(q.s, q.i)
	if end < 0 {
		return "", false
	}
	inner := q.s[q.i+1 : end]
	q.i = end + 1
	return inner, true
}

// compileCompounds compiles a list of compound selectors, the arguments
// of :not(), :is() and :where(), returning the largest specificity.
func compileCompounds(list string) ([]cssCompound, int, string) {
	var out []cssCompound
	best := 0
	for _, part := range cssSplit(list, ',') {
		q := &cssSelParser{s: part}
		c, spec, _, err := q.compound(true)
		if err != "" {
			return nil, 0, err
		}
		if q.skipWS(); q.i < len(q.s) {
			return nil, 0, "selector arguments are compound selectors only"
		}
		out = append(out, c)
		best = max(best, spec)
	}
	if len(out) == 0 {
		return nil, 0, "empty selector list"
	}
	return out, best, ""
}

// parseNth reads An+B: odd, even, 3, n+1, -n+3, 2n-1.
func parseNth(s string) (a, b int, ok bool) {
	s = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "")
	switch s {
	case "odd":
		return 2, 1, true
	case "even":
		return 2, 0, true
	}
	i := strings.IndexByte(s, 'n')
	if i < 0 {
		b, err := strconv.Atoi(s)
		return 0, b, err == nil
	}
	switch as := s[:i]; as {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		v, err := strconv.Atoi(as)
		if err != nil {
			return 0, 0, false
		}
		a = v
	}
	if rest := strings.TrimPrefix(s[i+1:], "+"); rest != "" {
		v, err := strconv.Atoi(rest)
		if err != nil {
			return 0, 0, false
		}
		b = v
	}
	return a, b, true
}

// elDoc is a surface's document as selectors see it: each node's parent,
// the regions (which no selector matches and above which none looks)
// with their surface's root, and the state of the controls and the
// pointer.
type elDoc struct {
	parent  map[*rex.TSPNode]*rex.TSPNode
	roots   map[*rex.TSPNode]*rex.TSPNode
	checked func(*rex.TSPNode) bool
	hovered func(*rex.TSPNode) bool
}

// index adds a surface's document: the root's children are its regions.
func (d *elDoc) index(root *rex.TSPNode) {
	if root == nil {
		return
	}
	var walk func(n, parent *rex.TSPNode)
	walk = func(n, parent *rex.TSPNode) {
		d.parent[n] = parent
		for _, ch := range n.Children {
			walk(ch, n)
		}
	}
	for _, region := range root.Children {
		d.roots[region] = root
		for _, ch := range region.Children {
			walk(ch, region)
		}
	}
}

// up is a node's parent element: none above the region.
func (d *elDoc) up(n *rex.TSPNode) *rex.TSPNode {
	p := d.parent[n]
	if p == nil || d.roots[p] != nil {
		return nil
	}
	return p
}

// siblings are the children of a node's parent, the node among them.
func (d *elDoc) siblings(n *rex.TSPNode) []*rex.TSPNode {
	if p := d.parent[n]; p != nil {
		return p.Children
	}
	return []*rex.TSPNode{n}
}

// prev is the sibling before a node.
func (d *elDoc) prev(n *rex.TSPNode) *rex.TSPNode {
	sib := d.siblings(n)
	if i := slices.Index(sib, n); i > 0 {
		return sib[i-1]
	}
	return nil
}

// matches tells whether a complex selector matches a node.
func (d *elDoc) matches(sel []cssCompound, n *rex.TSPNode) bool {
	return d.matchFrom(sel, len(sel)-1, n)
}

func (d *elDoc) matchFrom(sel []cssCompound, i int, n *rex.TSPNode) bool {
	if !d.matchCompound(&sel[i], n) {
		return false
	}
	if i == 0 {
		return true
	}
	switch sel[i].comb {
	case '>':
		p := d.up(n)
		return p != nil && d.matchFrom(sel, i-1, p)
	case '+':
		s := d.prev(n)
		return s != nil && d.matchFrom(sel, i-1, s)
	case '~':
		for s := d.prev(n); s != nil; s = d.prev(s) {
			if d.matchFrom(sel, i-1, s) {
				return true
			}
		}
		return false
	}
	for p := d.up(n); p != nil; p = d.up(p) {
		if d.matchFrom(sel, i-1, p) {
			return true
		}
	}
	return false
}

func (d *elDoc) matchCompound(c *cssCompound, n *rex.TSPNode) bool {
	if c.tag != "" && c.tag != elTagName(n) {
		return false
	}
	if len(c.ids) > 0 {
		// No element has an id selectors can rely on.
		return false
	}
	for _, cl := range c.classes {
		if !elHasClass(n, cl) {
			return false
		}
	}
	for i := range c.attrs {
		a := &c.attrs[i]
		v, ok := elAttr(n, a.name, d.checked)
		if !ok || !a.match(v) {
			return false
		}
	}
	for i := range c.pseudos {
		if !d.matchPseudo(&c.pseudos[i], n) {
			return false
		}
	}
	return true
}

func (a *cssAttrSel) match(v string) bool {
	want := a.val
	if a.fold {
		v, want = strings.ToLower(v), strings.ToLower(want)
	}
	switch a.op {
	case "":
		return true
	case "=":
		return v == want
	case "~=":
		return want != "" && slices.Contains(strings.Fields(v), want)
	case "|=":
		return v == want || strings.HasPrefix(v, want+"-")
	case "^=":
		return want != "" && strings.HasPrefix(v, want)
	case "$=":
		return want != "" && strings.HasSuffix(v, want)
	case "*=":
		return want != "" && strings.Contains(v, want)
	}
	return false
}

func (d *elDoc) matchPseudo(ps *cssPseudo, n *rex.TSPNode) bool {
	switch ps.name {
	case "checked":
		return elControl(n) != "" && d.checked != nil && d.checked(n)
	case "disabled":
		disabled, _ := n.Props["disabled"].(bool)
		return elControl(n) != "" && disabled
	case "hover":
		return d.hovered != nil && d.hovered(n)
	case "empty":
		text, _ := n.Props["text"].(string)
		return len(n.Children) == 0 && text == ""
	case "not":
		return !d.anyCompound(ps.args, n)
	case "is", "where":
		return d.anyCompound(ps.args, n)
	case "first-child", "last-child", "only-child", "first-of-type", "last-of-type",
		"nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		return d.matchPosition(ps, n)
	}
	// :active, :focus and the like: controls never take the focus, and
	// a press is not followed.
	return false
}

func (d *elDoc) anyCompound(args []cssCompound, n *rex.TSPNode) bool {
	for i := range args {
		if d.matchCompound(&args[i], n) {
			return true
		}
	}
	return false
}

// matchPosition matches the pseudo-classes of a node's place among its
// siblings: counted from the first or the last, among all of them, those
// of its tag (of-type) or those an "of S" list matches.
func (d *elDoc) matchPosition(ps *cssPseudo, n *rex.TSPNode) bool {
	sib := d.siblings(n)
	ofType := strings.HasSuffix(ps.name, "of-type")
	tag := elTagName(n)
	pos, count := 0, 0
	for _, s := range sib {
		if ofType && elTagName(s) != tag || len(ps.args) > 0 && !d.anyCompound(ps.args, s) {
			continue
		}
		count++
		if s == n {
			pos = count
		}
	}
	if pos == 0 {
		return false
	}
	fromEnd := count - pos + 1
	switch ps.name {
	case "first-child", "first-of-type":
		return pos == 1
	case "last-child", "last-of-type":
		return fromEnd == 1
	case "only-child":
		return count == 1
	case "nth-last-child", "nth-last-of-type":
		pos = fromEnd
	}
	if ps.a == 0 {
		return pos == ps.b
	}
	k := pos - ps.b
	return k%ps.a == 0 && k/ps.a >= 0
}

// cssMatch is a rule an element matched and where it stands in the
// cascade: its origin (0 the user agent's sheet, 1 the program's) and its
// sheet's place.
type cssMatch struct {
	r             *cssRule
	origin, sheet int
}

// cascade finds the rules of sheets that match a node, in cascade order
// (origin, specificity, sheet, place in the sheet), into buf. The first
// sheet is the user agent's.
func (d *elDoc) cascade(n *rex.TSPNode, sheets []*cssSheet, env *cssEnv, buf []cssMatch) []cssMatch {
	buf = buf[:0]
	for si, sh := range sheets {
		origin := 1
		if si == 0 {
			origin = 0
		}
		for ri := range sh.rules {
			r := &sh.rules[ri]
			if mediaOK(r.media, env) && d.matches(r.sel, n) {
				buf = append(buf, cssMatch{r, origin, si})
			}
		}
	}
	slices.SortStableFunc(buf, func(a, b cssMatch) int {
		if a.origin != b.origin {
			return a.origin - b.origin
		}
		if a.r.spec != b.r.spec {
			return a.r.spec - b.r.spec
		}
		if a.sheet != b.sheet {
			return a.sheet - b.sheet
		}
		return a.r.order - b.r.order
	})
	return buf
}

// cssEnv is what values resolve against: the window's size, its
// appearance and preferences, Tern's variables in GoRex's colors, the
// surface's program palette (--sf-p-*), and the monospaced family.
type cssEnv struct {
	vw, vh       float32
	dark         bool
	reduceMotion bool
	highContrast bool
	vars         map[string]string
	palette      map[string]string
	mono         string
}

// cssLen is a length computed: px, or a percentage, or auto.
type cssLen struct {
	v              float32
	pct, auto, set bool
}

// cssShadow is one box shadow; cur takes the element's color.
type cssShadow struct {
	inset              bool
	x, y, blur, spread float32
	c                  ui.Color
	cur                bool
}

// cssVars are the custom properties an element declares, over those it
// inherits.
type cssVars struct {
	parent *cssVars
	m      map[string]string
}

// elStyle is an element's computed style, what the window draws of it.
type elStyle struct {
	display string
	// Flex and grid.
	col, reverse, wrap                     bool
	justify, alignItems, alignSelf         ui.Align
	hasJustify, hasAlignItems, hasAlignSel bool
	grow, shrink                           float32
	hasShrink                              bool
	basis                                  cssLen
	rowGap, colGap                         float32
	cols                                   []ui.Track
	colStart, colSpan, rowStart, rowSpan   int
	// The box.
	pad, margin                           [4]cssLen
	width, height, minW, minH, maxW, maxH cssLen
	radius                                [4]float32
	bg                                    ui.Color
	borderW                               [4]float32
	borderOn                              [4]bool
	borderC                               [4]ui.Color
	borderCur                             [4]bool
	shadows                               [4]cssShadow
	nShadow                               int
	opacity                               float32
	clip, pointer                         bool
	// Inherited.
	color        ui.Color
	fontSize     float32
	weight       int
	italic       bool
	family       string
	textAlign    ui.Align
	hasTextAlign bool
	whiteSpace   string
	lineHeight   float32
	lineFixed    bool
	spacing      float32
	transform    string
	listStyle    string
	invisible    bool
	underline    bool
	strike       bool
	tc           ui.Color
	vars         *cssVars
	// collapsedTop, when set, is the top margin left after collapsing
	// with the block before it (elFlow).
	collapsedTop    float32
	hasCollapsedTop bool
}

// cssInitial is every property's initial value, for initial and unset.
var cssInitial = elStyle{display: "inline", opacity: 1, shrink: 1, fontSize: 16, weight: 400,
	color: ui.RGB(0, 0, 0), whiteSpace: "normal", listStyle: "disc", borderW: [4]float32{3, 3, 3, 3}, borderCur: [4]bool{true, true, true, true}}

// inherit starts a style from its parent's: the inherited properties
// taken, the others initial.
func (st *elStyle) inherit(ps *elStyle) {
	*st = elStyle{display: "inline", opacity: 1, shrink: 1, borderW: [4]float32{3, 3, 3, 3}, borderCur: [4]bool{true, true, true, true},
		color: ps.color, fontSize: ps.fontSize, weight: ps.weight, italic: ps.italic, family: ps.family,
		textAlign: ps.textAlign, hasTextAlign: ps.hasTextAlign, whiteSpace: ps.whiteSpace,
		lineHeight: ps.lineHeight, lineFixed: ps.lineFixed, spacing: ps.spacing, transform: ps.transform,
		listStyle: ps.listStyle, invisible: ps.invisible, underline: ps.underline, strike: ps.strike,
		tc: ps.tc, vars: ps.vars, pointer: ps.pointer}
}

// compute applies the declarations of the matched rules, in cascade
// order: custom properties first, then font-size (which em lengths
// read), then the rest; normal declarations before important ones.
func (st *elStyle) compute(ps *elStyle, matches []cssMatch, env *cssEnv) {
	for pass := range 2 {
		for _, m := range matches {
			for _, d := range m.r.decls {
				if d.important == (pass == 1) && strings.HasPrefix(d.prop, "--") {
					if st.vars == ps.vars {
						st.vars = &cssVars{parent: ps.vars, m: map[string]string{}}
					}
					st.vars.m[d.prop] = d.val
				}
			}
		}
	}
	for stage := range 2 {
		for pass := range 2 {
			for _, m := range matches {
				for _, d := range m.r.decls {
					if d.important != (pass == 1) || strings.HasPrefix(d.prop, "--") {
						continue
					}
					if font := d.prop == "font-size" || d.prop == "font"; font == (stage == 0) {
						st.apply(d.prop, d.val, ps, env)
					}
				}
			}
		}
	}
	for i := range 4 {
		if st.borderCur[i] {
			st.borderC[i] = st.color
		}
	}
	for i := range st.nShadow {
		if st.shadows[i].cur {
			st.shadows[i].c = st.color
		}
	}
}

// lookup finds a custom property: declared on the element or an
// ancestor, the tone color, the program's palette, or Tern's.
func (st *elStyle) lookup(name string, env *cssEnv) (string, bool) {
	for v := st.vars; v != nil; v = v.parent {
		if s, ok := v.m[name]; ok {
			return s, true
		}
	}
	if name == "--tc" {
		return cssHex(st.tc), true
	}
	if tok, ok := strings.CutPrefix(name, "--sf-p-"); ok {
		if s, ok := env.palette[tok]; ok {
			return s, true
		}
		return "", false
	}
	s, ok := env.vars[name]
	return s, ok
}

// cssVarRefs bounds the var() references one value's expansion takes,
// and cssVarLen the length it may come to, however the references fan
// out: each is resolved anew, so these bound the work of a declaration.
const (
	cssVarRefs = 256
	cssVarLen  = 16 << 10
)

// resolve substitutes the var() references of a value, with their
// fallbacks; false when one resolves to nothing, the chain runs past 32
// references, or the expansion past cssVarRefs or cssVarLen.
func (st *elStyle) resolve(val string, env *cssEnv) (string, bool) {
	refs := cssVarRefs
	return st.expand(val, env, 0, &refs)
}

func (st *elStyle) expand(val string, env *cssEnv, depth int, refs *int) (string, bool) {
	if !strings.Contains(val, "var(") {
		return val, len(val) <= cssVarLen || depth == 0
	}
	if depth >= 32 {
		return "", false
	}
	var b strings.Builder
	rest := val
	for {
		i := strings.Index(rest, "var(")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		end := cssCloseParen(rest, i+3)
		if *refs--; end < 0 || *refs < 0 {
			return "", false
		}
		name, fallback, hasFallback := strings.Cut(rest[i+4:end], ",")
		v, ok := st.lookup(strings.TrimSpace(name), env)
		if !ok {
			if !hasFallback {
				return "", false
			}
			v = strings.TrimSpace(fallback)
		}
		v, ok = st.expand(v, env, depth+1, refs)
		if !ok || b.Len()+len(v) > cssVarLen {
			return "", false
		}
		b.WriteString(v)
		rest = rest[end+1:]
	}
	if b.Len() > cssVarLen {
		return "", false
	}
	return b.String(), true
}

// cssInherited are the inherited properties the window draws.
var cssInherited = map[string]bool{"color": true, "font-size": true, "font-weight": true, "font-style": true,
	"font-family": true, "font": true, "text-align": true, "white-space": true, "line-height": true,
	"letter-spacing": true, "text-transform": true, "list-style-type": true, "list-style": true,
	"visibility": true, "cursor": true}

// apply applies one declaration.
func (st *elStyle) apply(prop, val string, ps *elStyle, env *cssEnv) {
	v, ok := st.resolve(val, env)
	if !ok {
		v = "unset"
	}
	v = strings.TrimSpace(v)
	kw := strings.ToLower(v)
	switch kw {
	case "inherit":
		st.copyProp(prop, ps)
		return
	case "initial":
		st.copyProp(prop, &cssInitial)
		return
	case "unset":
		if cssInherited[prop] {
			st.copyProp(prop, ps)
		} else {
			st.copyProp(prop, &cssInitial)
		}
		return
	}
	size := st.fontSize
	length := func(s string) (cssLen, bool) { return cssLength(s, size, env) }
	switch prop {
	case "display":
		if cssDisplays[kw] {
			st.display = kw
		}
	case "flex-direction":
		st.col, st.reverse = strings.HasPrefix(kw, "column"), strings.HasSuffix(kw, "-reverse")
	case "flex-wrap":
		st.wrap = kw == "wrap" || kw == "wrap-reverse"
	case "flex-flow":
		for _, f := range strings.Fields(kw) {
			switch f {
			case "row", "row-reverse", "column", "column-reverse":
				st.col, st.reverse = strings.HasPrefix(f, "column"), strings.HasSuffix(f, "-reverse")
			case "wrap", "wrap-reverse":
				st.wrap = true
			case "nowrap":
				st.wrap = false
			}
		}
	case "flex-grow":
		if f, ok := cssNumber(kw); ok && f >= 0 {
			st.grow = f
		}
	case "flex-shrink":
		if f, ok := cssNumber(kw); ok && f >= 0 {
			st.shrink, st.hasShrink = f, true
		}
	case "flex-basis":
		if l, ok := length(kw); ok {
			st.basis = l
		}
	case "flex":
		st.applyFlex(kw, length)
	case "justify-content":
		st.justify, st.hasJustify = cssAlign(kw)
	case "align-items":
		st.alignItems, st.hasAlignItems = cssAlign(kw)
	case "place-items":
		if f := strings.Fields(kw); len(f) > 0 {
			st.alignItems, st.hasAlignItems = cssAlign(f[0])
		}
	case "align-self":
		st.alignSelf, st.hasAlignSel = cssAlign(kw)
	case "gap":
		f := cssFields(kw)
		if len(f) == 1 {
			f = append(f, f[0])
		}
		if len(f) == 2 {
			if r, ok := length(f[0]); ok && !r.pct {
				st.rowGap = r.v
			}
			if c, ok := length(f[1]); ok && !c.pct {
				st.colGap = c.v
			}
		}
	case "row-gap", "column-gap":
		g := float32(0)
		if l, ok := length(kw); ok && !l.pct {
			g = l.v
		}
		if prop == "row-gap" {
			st.rowGap = g
		} else {
			st.colGap = g
		}
	case "grid-template-columns":
		st.cols = cssTracks(kw, size, env)
	case "grid-column":
		st.colStart, st.colSpan = cssGridLine(kw)
	case "grid-row":
		st.rowStart, st.rowSpan = cssGridLine(kw)
	case "padding", "margin":
		sides, ok := cssSides(cssFields(kw), length)
		if !ok {
			return
		}
		if prop == "padding" {
			st.pad = sides
		} else {
			st.margin = sides
		}
	case "padding-top", "padding-right", "padding-bottom", "padding-left":
		if l, ok := length(kw); ok && !l.auto {
			st.pad[cssSide(prop)] = l
		}
	case "margin-top", "margin-right", "margin-bottom", "margin-left":
		if l, ok := length(kw); ok {
			st.margin[cssSide(prop)] = l
		}
	case "width", "height", "min-width", "min-height", "max-width", "max-height":
		l, ok := length(kw)
		if !ok {
			l = cssLen{}
		}
		switch prop {
		case "width":
			st.width = l
		case "height":
			st.height = l
		case "min-width":
			st.minW = l
		case "min-height":
			st.minH = l
		case "max-width":
			st.maxW = l
		case "max-height":
			st.maxH = l
		}
	case "border-radius":
		h, _, _ := strings.Cut(kw, "/")
		f := cssFields(h)
		var r [4]float32
		switch len(f) {
		case 1:
			f = []string{f[0], f[0], f[0], f[0]}
		case 2:
			f = []string{f[0], f[1], f[0], f[1]}
		case 3:
			f = []string{f[0], f[1], f[2], f[1]}
		case 4:
		default:
			return
		}
		for i, s := range f {
			l, ok := length(s)
			if !ok || l.pct {
				return
			}
			r[i] = l.v
		}
		st.radius = r
	case "border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius":
		if f := cssFields(kw); len(f) > 0 {
			if l, ok := length(f[0]); ok && !l.pct {
				st.radius[map[string]int{"border-top-left-radius": 0, "border-top-right-radius": 1, "border-bottom-right-radius": 2, "border-bottom-left-radius": 3}[prop]] = l.v
			}
		}
	case "background", "background-color":
		if kw == "none" {
			st.bg = ui.Transparent
			return
		}
		layers := cssSplit(v, ',')
		for _, tok := range cssFields(layers[len(layers)-1]) {
			if c, ok := cssColor(tok, st.color, env.dark); ok {
				st.bg = c
				return
			}
		}
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		w, on, c, cur, ok := st.borderOf(v, env)
		if !ok {
			return
		}
		for i := range 4 {
			if prop == "border" || cssSide(prop) == i {
				st.borderW[i], st.borderOn[i], st.borderC[i], st.borderCur[i] = w, on, c, cur
			}
		}
	case "border-width", "border-style", "border-color":
		f := cssFields(v)
		f4, ok := cssFour(f)
		if !ok {
			return
		}
		for i, s := range f4 {
			st.borderSide(prop, i, s, env)
		}
	case "border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
		"border-top-style", "border-right-style", "border-bottom-style", "border-left-style",
		"border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		st.borderSide(prop, cssSide(prop), v, env)
	case "box-shadow":
		st.nShadow = 0
		if kw == "none" {
			return
		}
		for _, layer := range cssSplit(v, ',') {
			if st.nShadow == len(st.shadows) {
				break
			}
			if sh, ok := cssShadowOf(layer, size, env); ok {
				st.shadows[st.nShadow] = sh
				st.nShadow++
			}
		}
	case "color":
		if c, ok := cssColor(v, ps.color, env.dark); ok {
			st.color = c
		}
	case "opacity":
		if f, ok := cssNumberOrPct(kw); ok {
			st.opacity = max(0, min(1, f))
		}
	case "visibility":
		st.invisible = kw == "hidden" || kw == "collapse"
	case "overflow", "overflow-x", "overflow-y":
		st.clip = !strings.Contains(kw, "visible")
	case "cursor":
		st.pointer = kw == "pointer"
	case "font-size":
		st.fontSize = cssFontSize(kw, ps.fontSize, env)
	case "font-weight":
		st.weight = cssWeight(kw, ps.weight)
	case "font-style":
		st.italic = kw == "italic" || kw == "oblique"
	case "font-family":
		st.family = cssFamily(v, env.mono)
	case "font":
		st.applyFont(v, ps, env)
	case "line-height":
		st.lineHeight, st.lineFixed = 0, false
		if kw == "normal" {
			return
		}
		if f, err := strconv.ParseFloat(kw, 32); err == nil {
			st.lineHeight = float32(f)
		} else if l, ok := length(kw); ok {
			if l.pct {
				st.lineHeight = l.v / 100
			} else {
				st.lineHeight, st.lineFixed = l.v, true
			}
		}
	case "letter-spacing":
		st.spacing = 0
		if l, ok := length(kw); ok && !l.pct && !l.auto {
			st.spacing = l.v
		}
	case "text-align":
		switch kw {
		case "center":
			st.textAlign, st.hasTextAlign = ui.Center, true
		case "right", "end":
			st.textAlign, st.hasTextAlign = ui.End, true
		default:
			st.textAlign, st.hasTextAlign = ui.Start, true
		}
	case "text-decoration", "text-decoration-line":
		st.underline, st.strike = false, false
		for _, f := range strings.Fields(kw) {
			switch f {
			case "underline":
				st.underline = true
			case "line-through":
				st.strike = true
			}
		}
	case "white-space":
		st.whiteSpace = kw
	case "text-transform":
		st.transform = kw
	case "list-style-type":
		st.listStyle = v
	case "list-style":
		for _, f := range cssFields(v) {
			switch strings.ToLower(f) {
			case "inside", "outside":
			default:
				st.listStyle = f
			}
		}
	}
}

// copyProp takes a property's value from another style: the parent's
// (inherit) or the initial one.
func (st *elStyle) copyProp(prop string, src *elStyle) {
	switch prop {
	case "display":
		st.display = src.display
	case "flex-direction", "flex-flow":
		st.col, st.reverse = src.col, src.reverse
		if prop == "flex-flow" {
			st.wrap = src.wrap
		}
	case "flex-wrap":
		st.wrap = src.wrap
	case "flex", "flex-grow", "flex-shrink", "flex-basis":
		st.grow, st.shrink, st.hasShrink, st.basis = src.grow, src.shrink, src.hasShrink, src.basis
	case "justify-content":
		st.justify, st.hasJustify = src.justify, src.hasJustify
	case "align-items", "place-items":
		st.alignItems, st.hasAlignItems = src.alignItems, src.hasAlignItems
	case "align-self":
		st.alignSelf, st.hasAlignSel = src.alignSelf, src.hasAlignSel
	case "gap", "row-gap", "column-gap":
		st.rowGap, st.colGap = src.rowGap, src.colGap
	case "grid-template-columns":
		st.cols = src.cols
	case "padding", "padding-top", "padding-right", "padding-bottom", "padding-left":
		st.pad = src.pad
	case "margin", "margin-top", "margin-right", "margin-bottom", "margin-left":
		st.margin = src.margin
	case "width", "height", "min-width", "min-height", "max-width", "max-height":
		st.width, st.height, st.minW, st.minH, st.maxW, st.maxH = src.width, src.height, src.minW, src.minH, src.maxW, src.maxH
	case "background", "background-color":
		st.bg = src.bg
	case "box-shadow":
		st.shadows, st.nShadow = src.shadows, src.nShadow
	case "color":
		st.color = src.color
	case "opacity":
		st.opacity = src.opacity
	case "visibility":
		st.invisible = src.invisible
	case "font-size":
		st.fontSize = src.fontSize
	case "font-weight":
		st.weight = src.weight
	case "font-style":
		st.italic = src.italic
	case "font-family":
		st.family = src.family
	case "font":
		st.fontSize, st.weight, st.italic, st.family = src.fontSize, src.weight, src.italic, src.family
	case "line-height":
		st.lineHeight, st.lineFixed = src.lineHeight, src.lineFixed
	case "letter-spacing":
		st.spacing = src.spacing
	case "text-align":
		st.textAlign, st.hasTextAlign = src.textAlign, src.hasTextAlign
	case "text-decoration", "text-decoration-line":
		st.underline, st.strike = src.underline, src.strike
	case "white-space":
		st.whiteSpace = src.whiteSpace
	case "text-transform":
		st.transform = src.transform
	case "list-style", "list-style-type":
		st.listStyle = src.listStyle
	case "cursor":
		st.pointer = src.pointer
	default:
		if strings.HasPrefix(prop, "border") {
			if strings.Contains(prop, "radius") {
				st.radius = src.radius
			} else {
				st.borderW, st.borderOn, st.borderC, st.borderCur = src.borderW, src.borderOn, src.borderC, src.borderCur
			}
		}
	}
}

// applyFlex reads the flex shorthand.
func (st *elStyle) applyFlex(kw string, length func(string) (cssLen, bool)) {
	switch kw {
	case "none":
		st.grow, st.shrink, st.hasShrink, st.basis = 0, 0, true, cssLen{}
		return
	case "auto":
		st.grow, st.shrink, st.hasShrink, st.basis = 1, 1, true, cssLen{}
		return
	}
	// <grow> [<shrink>] ‖ <basis>: grow and shrink 1 and basis 0 when
	// left out.
	grow, shrink, basis, nums := float32(1), float32(1), cssLen{set: true}, 0
	for _, f := range cssFields(kw) {
		if n, ok := cssNumber(f); ok {
			if nums == 0 {
				grow = n
			} else {
				shrink = n
			}
			nums++
		} else if l, ok := length(f); ok {
			basis = l
		}
	}
	st.grow, st.shrink, st.hasShrink, st.basis = grow, shrink, true, basis
}

// applyFont reads the font shorthand: [style ‖ weight] size[/line-height]
// family.
func (st *elStyle) applyFont(v string, ps *elStyle, env *cssEnv) {
	f := cssFields(v)
	for i, tok := range f {
		low := strings.ToLower(tok)
		switch {
		case low == "italic" || low == "oblique":
			st.italic = true
		case low == "normal":
		case cssWeight(low, -1) >= 0:
			st.weight = cssWeight(low, ps.weight)
		default:
			sz, lh, _ := strings.Cut(low, "/")
			st.fontSize = cssFontSize(sz, ps.fontSize, env)
			if lh != "" {
				if n, err := strconv.ParseFloat(lh, 32); err == nil {
					st.lineHeight, st.lineFixed = float32(n), false
				} else if l, ok := cssLength(lh, st.fontSize, env); ok && !l.pct {
					st.lineHeight, st.lineFixed = l.v, true
				}
			}
			st.family = cssFamily(strings.Join(f[i+1:], " "), env.mono)
			return
		}
	}
}

// borderOf reads a border shorthand: width ‖ style ‖ color.
func (st *elStyle) borderOf(v string, env *cssEnv) (w float32, on bool, c ui.Color, cur, ok bool) {
	w, cur = 3, true
	for _, tok := range cssFields(v) {
		low := strings.ToLower(tok)
		if bw, ok := cssBorderWidth(low, st.fontSize, env); ok {
			w = bw
		} else if s, ok := cssBorderStyle(low); ok {
			on = s
		} else if col, ok := cssColor(tok, st.color, env.dark); ok {
			c, cur = col, low == "currentcolor"
		} else {
			return 0, false, c, false, false
		}
	}
	return w, on, c, cur, true
}

// borderSide applies a border longhand to a side.
func (st *elStyle) borderSide(prop string, i int, v string, env *cssEnv) {
	low := strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.HasSuffix(prop, "width"):
		if w, ok := cssBorderWidth(low, st.fontSize, env); ok {
			st.borderW[i] = w
		}
	case strings.HasSuffix(prop, "style"):
		if s, ok := cssBorderStyle(low); ok {
			st.borderOn[i] = s
		}
	default:
		if c, ok := cssColor(v, st.color, env.dark); ok {
			st.borderC[i], st.borderCur[i] = c, low == "currentcolor"
		}
	}
}

func cssBorderWidth(s string, size float32, env *cssEnv) (float32, bool) {
	switch s {
	case "thin":
		return 1, true
	case "medium":
		return 3, true
	case "thick":
		return 5, true
	}
	l, ok := cssLength(s, size, env)
	if !ok || l.pct || l.auto {
		return 0, false
	}
	return l.v, true
}

func cssBorderStyle(s string) (on, ok bool) {
	switch s {
	case "none", "hidden":
		return false, true
	case "solid", "dashed", "dotted", "double", "groove", "ridge", "inset", "outset", "auto":
		return true, true
	}
	return false, false
}

// cssShadowOf reads one box shadow: inset? x y [blur [spread]] color?.
func cssShadowOf(layer string, size float32, env *cssEnv) (cssShadow, bool) {
	sh := cssShadow{cur: true}
	var lens []float32
	for _, tok := range cssFields(layer) {
		low := strings.ToLower(tok)
		if low == "inset" {
			sh.inset = true
		} else if l, ok := cssLength(low, size, env); ok && !l.pct && !l.auto {
			lens = append(lens, l.v)
		} else if c, ok := cssColor(tok, ui.Color{}, env.dark); ok && low != "currentcolor" {
			sh.c, sh.cur = c, false
		} else if low != "currentcolor" {
			return sh, false
		}
	}
	if len(lens) < 2 || len(lens) > 4 {
		return sh, false
	}
	lens = append(lens, 0, 0)
	sh.x, sh.y, sh.blur, sh.spread = lens[0], lens[1], lens[2], lens[3]
	return sh, true
}

// cssSides reads 1 to 4 lengths as top, right, bottom and left.
func cssSides(f []string, length func(string) (cssLen, bool)) ([4]cssLen, bool) {
	var out [4]cssLen
	f4, ok := cssFour(f)
	if !ok {
		return out, false
	}
	for i, s := range f4 {
		l, ok := length(s)
		if !ok {
			return out, false
		}
		out[i] = l
	}
	return out, true
}

// cssFour expands 1 to 4 values to the four sides.
func cssFour(f []string) ([4]string, bool) {
	switch len(f) {
	case 1:
		return [4]string{f[0], f[0], f[0], f[0]}, true
	case 2:
		return [4]string{f[0], f[1], f[0], f[1]}, true
	case 3:
		return [4]string{f[0], f[1], f[2], f[1]}, true
	case 4:
		return [4]string{f[0], f[1], f[2], f[3]}, true
	}
	return [4]string{}, false
}

// cssSide is the side a longhand names: 0 top, 1 right, 2 bottom, 3 left.
func cssSide(prop string) int {
	switch {
	case strings.Contains(prop, "top"):
		return 0
	case strings.Contains(prop, "right"):
		return 1
	case strings.Contains(prop, "bottom"):
		return 2
	case strings.Contains(prop, "left"):
		return 3
	}
	return -1
}

// cssAlign reads an alignment keyword.
func cssAlign(kw string) (ui.Align, bool) {
	switch kw {
	case "start", "flex-start", "self-start", "left":
		return ui.Start, true
	case "end", "flex-end", "self-end", "right":
		return ui.End, true
	case "center":
		return ui.Center, true
	case "stretch", "normal":
		return ui.Stretch, true
	case "baseline":
		return ui.Baseline, true
	case "space-between":
		return ui.SpaceBetween, true
	case "space-around":
		return ui.SpaceAround, true
	case "space-evenly":
		return ui.SpaceEvenly, true
	}
	return ui.Start, false
}

// cssMaxTracks bounds a grid track list, and cssTrackSteps the tokens its
// repeat()s expand, nested ones multiplying, so that a program's list
// costs bounded work.
const (
	cssMaxTracks  = 1024
	cssTrackSteps = 4096
)

// cssTracks reads a grid track list: lengths as fixed tracks, fr as
// shares, auto and the content keywords as tracks fitting their content,
// minmax() by its maximum, repeat() expanded; up to cssMaxTracks tracks.
func cssTracks(v string, size float32, env *cssEnv) []ui.Track {
	if v == "none" {
		return nil
	}
	var out []ui.Track
	steps := cssTrackSteps
	var add func(tok string)
	add = func(tok string) {
		if steps--; steps < 0 || len(out) >= cssMaxTracks {
			return
		}
		switch {
		case strings.HasPrefix(tok, "repeat("):
			if len(tok) <= len("repeat(") || !strings.HasSuffix(tok, ")") {
				return
			}
			n, list, ok := strings.Cut(tok[len("repeat("):len(tok)-1], ",")
			count, err := strconv.Atoi(strings.TrimSpace(n))
			if !ok || err != nil || count < 1 || count > 64 {
				return
			}
			toks := cssFields(list)
			for range count {
				for _, t := range toks {
					add(t)
				}
				if steps < 0 || len(out) >= cssMaxTracks {
					return
				}
			}
		case strings.HasPrefix(tok, "minmax("):
			if len(tok) <= len("minmax(") || !strings.HasSuffix(tok, ")") {
				return
			}
			parts := cssSplit(tok[len("minmax("):len(tok)-1], ',')
			if len(parts) == 2 {
				add(parts[1])
			}
		case strings.HasSuffix(tok, "fr"):
			if f, err := strconv.ParseFloat(strings.TrimSuffix(tok, "fr"), 32); err == nil {
				out = append(out, ui.Fr(float32(f)))
			}
		default:
			if l, ok := cssLength(tok, size, env); ok && !l.pct && !l.auto {
				out = append(out, ui.Fixed(l.v))
			} else {
				out = append(out, ui.FitContent())
			}
		}
	}
	for _, tok := range cssFields(v) {
		add(tok)
	}
	return out
}

// cssGridLine reads grid-column or grid-row: a start line and a span.
func cssGridLine(v string) (start, span int) {
	a, b, hasB := strings.Cut(v, "/")
	line := func(s string) (n int, isSpan bool) {
		f := strings.Fields(s)
		if len(f) == 2 && f[0] == "span" {
			n, _ = strconv.Atoi(f[1])
			return n, true
		}
		if len(f) == 1 {
			n, _ = strconv.Atoi(f[0])
		}
		return n, false
	}
	n, isSpan := line(a)
	if isSpan {
		return 0, n
	}
	start = n
	if hasB {
		m, mSpan := line(b)
		switch {
		case mSpan:
			span = m
		case m > start && start > 0:
			span = m - start
		}
	}
	return start, span
}

// cssFontSize reads a font size against the parent's.
func cssFontSize(kw string, parent float32, env *cssEnv) float32 {
	switch kw {
	case "smaller":
		return parent / 1.2
	case "larger":
		return parent * 1.2
	case "small":
		return 13
	case "medium":
		return 16
	case "large":
		return 18
	}
	l, ok := cssLength(kw, parent, env)
	if !ok || l.auto {
		return parent
	}
	if l.pct {
		return parent * l.v / 100
	}
	return l.v
}

// cssWeight reads a font weight against the parent's; -1 when kw is none
// and parent is -1.
func cssWeight(kw string, parent int) int {
	switch kw {
	case "normal":
		return 400
	case "bold":
		return 700
	case "bolder", "lighter":
		if parent < 0 {
			return 0
		}
		if kw == "bolder" {
			switch {
			case parent < 350:
				return 400
			case parent < 550:
				return 700
			}
			return 900
		}
		switch {
		case parent < 550:
			return 100
		case parent < 750:
			return 400
		}
		return 700
	}
	if n, err := strconv.Atoi(kw); err == nil && n >= 1 && n <= 1000 {
		return n
	}
	return parent
}

// cssFamily reads a font-family list into MyGo's: the generic families
// as the window has them, the monospaced one the editor's.
func cssFamily(v, mono string) string {
	var out []string
	for _, f := range strings.Split(v, ",") {
		f = cssUnquote(strings.TrimSpace(f))
		switch strings.ToLower(f) {
		case "":
			continue
		case "monospace", "ui-monospace":
			f = mono
		case "system-ui", "-apple-system", "blinkmacsystemfont", "sans-serif", "ui-sans-serif", "ui-rounded":
			f = "system-ui"
		case "ui-serif":
			f = "serif"
		}
		out = append(out, f)
	}
	return strings.Join(out, ", ")
}

// cssNumber reads a plain number.
func cssNumber(s string) (float32, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	return float32(f), err == nil
}

// cssNumberOrPct reads a number, or a percentage as a fraction.
func cssNumberOrPct(s string) (float32, bool) {
	if p, ok := strings.CutSuffix(s, "%"); ok {
		f, ok := cssNumber(p)
		return f / 100, ok
	}
	return cssNumber(s)
}

// cssLength reads a length or percentage: px, em (of size), ch, %, vw,
// vh, a bare 0, auto, or calc(), min(), max() and clamp() of them.
func cssLength(s string, size float32, env *cssEnv) (cssLen, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "auto" {
		return cssLen{auto: true, set: true}, true
	}
	q := cssCalc{s: s, size: size, env: env}
	v, ok := q.sum()
	q.ws()
	if !ok || q.i != len(s) {
		return cssLen{}, false
	}
	switch {
	case v.num && v.px != 0:
		return cssLen{}, false
	case v.pct != 0 && v.px == 0:
		return cssLen{v: v.pct, pct: true, set: true}, true
	}
	return cssLen{v: v.px, set: true}, true
}

// cssCalc evaluates a length expression.
type cssCalc struct {
	s    string
	i    int
	size float32
	env  *cssEnv
}

// cssCalcV is a value of a length expression: px and a percentage, or a
// plain number (num, in px).
type cssCalcV struct {
	px, pct float32
	num     bool
}

func (q *cssCalc) ws() {
	for q.i < len(q.s) && cssSpace(q.s[q.i]) {
		q.i++
	}
}

func (q *cssCalc) sum() (cssCalcV, bool) {
	a, ok := q.product()
	if !ok {
		return a, false
	}
	for {
		q.ws()
		if q.i >= len(q.s) || q.s[q.i] != '+' && q.s[q.i] != '-' {
			return a, true
		}
		op := q.s[q.i]
		q.i++
		b, ok := q.product()
		if !ok {
			return a, false
		}
		if a.num != b.num {
			// A bare 0 is a length.
			switch {
			case a.num && a.px == 0:
				a.num = false
			case b.num && b.px == 0:
				b.num = false
			default:
				return a, false
			}
		}
		if op == '-' {
			b.px, b.pct = -b.px, -b.pct
		}
		a.px, a.pct = a.px+b.px, a.pct+b.pct
	}
}

func (q *cssCalc) product() (cssCalcV, bool) {
	a, ok := q.factor()
	if !ok {
		return a, false
	}
	for {
		q.ws()
		if q.i >= len(q.s) || q.s[q.i] != '*' && q.s[q.i] != '/' {
			return a, true
		}
		op := q.s[q.i]
		q.i++
		b, ok := q.factor()
		if !ok {
			return a, false
		}
		switch {
		case op == '/' && b.num && b.px != 0:
			a.px, a.pct = a.px/b.px, a.pct/b.px
		case op == '*' && b.num:
			a.px, a.pct = a.px*b.px, a.pct*b.px
		case op == '*' && a.num:
			b.px, b.pct = b.px*a.px, b.pct*a.px
			a = b
		default:
			return a, false
		}
	}
}

func (q *cssCalc) factor() (cssCalcV, bool) {
	q.ws()
	if q.i >= len(q.s) {
		return cssCalcV{}, false
	}
	if q.s[q.i] == '(' {
		q.i++
		v, ok := q.sum()
		q.ws()
		if !ok || q.i >= len(q.s) || q.s[q.i] != ')' {
			return v, false
		}
		q.i++
		return v, true
	}
	for _, fn := range [...]string{"calc(", "min(", "max(", "clamp("} {
		if !strings.HasPrefix(q.s[q.i:], fn) {
			continue
		}
		q.i += len(fn)
		var args []cssCalcV
		for {
			v, ok := q.sum()
			if !ok {
				return v, false
			}
			args = append(args, v)
			q.ws()
			if q.i < len(q.s) && q.s[q.i] == ',' && fn != "calc(" {
				q.i++
				continue
			}
			break
		}
		if q.i >= len(q.s) || q.s[q.i] != ')' {
			return cssCalcV{}, false
		}
		q.i++
		return cssCalcFn(fn, args)
	}
	// A number and its unit.
	start := q.i
	if q.s[q.i] == '+' || q.s[q.i] == '-' {
		q.i++
	}
	for q.i < len(q.s) && (q.s[q.i] >= '0' && q.s[q.i] <= '9' || q.s[q.i] == '.') {
		q.i++
	}
	if q.i < len(q.s) && (q.s[q.i] == 'e') && q.i+1 < len(q.s) && (q.s[q.i+1] >= '0' && q.s[q.i+1] <= '9' || q.s[q.i+1] == '-') {
		q.i += 2
		for q.i < len(q.s) && q.s[q.i] >= '0' && q.s[q.i] <= '9' {
			q.i++
		}
	}
	n, err := strconv.ParseFloat(q.s[start:q.i], 32)
	if err != nil {
		return cssCalcV{}, false
	}
	f := float32(n)
	unitStart := q.i
	for q.i < len(q.s) && (q.s[q.i] >= 'a' && q.s[q.i] <= 'z' || q.s[q.i] == '%') {
		q.i++
	}
	switch q.s[unitStart:q.i] {
	case "":
		return cssCalcV{px: f, num: true}, true
	case "px":
		return cssCalcV{px: f}, true
	case "em":
		return cssCalcV{px: f * q.size}, true
	case "ch":
		return cssCalcV{px: f * q.size * 0.6}, true
	case "%":
		return cssCalcV{pct: f}, true
	case "vw":
		return cssCalcV{px: f * q.env.vw / 100}, true
	case "vh":
		return cssCalcV{px: f * q.env.vh / 100}, true
	}
	return cssCalcV{}, false
}

// cssCalcFn folds min(), max() and clamp() of comparable values.
func cssCalcFn(fn string, args []cssCalcV) (cssCalcV, bool) {
	if fn == "calc(" {
		return args[0], len(args) == 1
	}
	for _, a := range args[1:] {
		if a.num != args[0].num || (a.pct != 0) != (args[0].pct != 0) || a.pct != 0 && a.px != 0 {
			return cssCalcV{}, false
		}
	}
	key := func(v cssCalcV) float32 { return v.px + v.pct }
	switch fn {
	case "min(", "max(":
		best := args[0]
		for _, a := range args[1:] {
			if fn == "min(" && key(a) < key(best) || fn == "max(" && key(a) > key(best) {
				best = a
			}
		}
		return best, true
	}
	if len(args) != 3 {
		return cssCalcV{}, false
	}
	v := args[1]
	if key(v) < key(args[0]) {
		v = args[0]
	}
	if key(v) > key(args[2]) {
		v = args[2]
	}
	return v, true
}

// cssColor reads a color: hex, a named color, transparent, currentColor
// (cur), rgb() and rgba() (legacy and modern, and relative to another
// color: rgb(from c r g b / a)), hsl() and hsla(), light-dark() and
// color-mix() (mixed in sRGB).
func cssColor(s string, cur ui.Color, dark bool) (ui.Color, bool) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "#") {
		return cssHexColor(low[1:])
	}
	switch low {
	case "transparent":
		return ui.Transparent, true
	case "currentcolor":
		return cur, true
	}
	if rgb, ok := cssNamed[low]; ok {
		return ui.RGB(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb)), true
	}
	open := strings.IndexByte(low, '(')
	if open < 0 || !strings.HasSuffix(low, ")") {
		return ui.Color{}, false
	}
	fn, args := low[:open], strings.TrimSpace(low[open+1:len(low)-1])
	switch fn {
	case "light-dark":
		parts := cssSplit(args, ',')
		if len(parts) != 2 {
			return ui.Color{}, false
		}
		if dark {
			return cssColor(parts[1], cur, dark)
		}
		return cssColor(parts[0], cur, dark)
	case "color-mix":
		parts := cssSplit(args, ',')
		if len(parts) != 3 || !strings.HasPrefix(strings.ToLower(parts[0]), "in ") {
			return ui.Color{}, false
		}
		var cs [2]ui.Color
		var ps [2]float32
		var set [2]bool
		for i, part := range parts[1:] {
			var color string
			for _, tok := range cssFields(part) {
				if p, ok := strings.CutSuffix(tok, "%"); ok {
					f, ok := cssNumber(p)
					if !ok {
						return ui.Color{}, false
					}
					ps[i], set[i] = f, true
				} else {
					color = tok
				}
			}
			c, ok := cssColor(color, cur, dark)
			if !ok {
				return ui.Color{}, false
			}
			cs[i] = c
		}
		switch {
		case !set[0] && !set[1]:
			ps = [2]float32{50, 50}
		case !set[0]:
			ps[0] = 100 - ps[1]
		case !set[1]:
			ps[1] = 100 - ps[0]
		}
		if ps[0]+ps[1] <= 0 {
			return ui.Color{}, false
		}
		return cs[0].Mix(cs[1], ps[1]/(ps[0]+ps[1])), true
	case "rgb", "rgba", "hsl", "hsla":
		if rest, ok := strings.CutPrefix(strings.TrimSpace(args), "from "); ok {
			return cssRelative(fn, rest, cur, dark)
		}
		var comps []string
		alpha := "1"
		if strings.Contains(args, ",") {
			comps = cssSplit(args, ',')
			if len(comps) == 4 {
				alpha, comps = comps[3], comps[:3]
			}
		} else {
			main, a, hasA := strings.Cut(args, "/")
			comps = strings.Fields(main)
			if hasA {
				alpha = strings.TrimSpace(a)
			}
		}
		if len(comps) != 3 {
			return ui.Color{}, false
		}
		af, ok := cssNumberOrPct(strings.TrimSpace(alpha))
		if !ok {
			return ui.Color{}, false
		}
		if fn == "hsl" || fn == "hsla" {
			h, ok1 := cssHue(comps[0])
			sat, ok2 := cssNumberOrPct(strings.TrimSpace(comps[1]))
			l, ok3 := cssNumberOrPct(strings.TrimSpace(comps[2]))
			if !ok1 || !ok2 || !ok3 {
				return ui.Color{}, false
			}
			r, g, b := cssHSL(h, sat, l)
			return ui.RGBA(r, g, b, af), true
		}
		var ch [3]uint8
		for i, c := range comps {
			c = strings.TrimSpace(c)
			var f float32
			if p, isPct := strings.CutSuffix(c, "%"); isPct {
				v, ok := cssNumber(p)
				if !ok {
					return ui.Color{}, false
				}
				f = v * 255 / 100
			} else {
				v, ok := cssNumber(c)
				if !ok {
					return ui.Color{}, false
				}
				f = v
			}
			ch[i] = uint8(max(0, min(255, f)) + 0.5)
		}
		return ui.RGBA(ch[0], ch[1], ch[2], af), true
	}
	return ui.Color{}, false
}

// cssRelative reads a relative color after "from": the origin color, its
// channels by name or as numbers, and an alpha.
func cssRelative(fn, rest string, cur ui.Color, dark bool) (ui.Color, bool) {
	parts := cssSplit(rest, '/')
	f := cssFields(parts[0])
	if len(parts) > 2 || len(f) != 4 || fn != "rgb" && fn != "rgba" {
		return ui.Color{}, false
	}
	origin, ok := cssColor(f[0], cur, dark)
	if !ok {
		return ui.Color{}, false
	}
	from := [3]uint8{origin.R, origin.G, origin.B}
	var ch [3]uint8
	for i, tok := range f[1:] {
		if j := strings.Index("rgb", tok); len(tok) == 1 && j >= 0 {
			ch[i] = from[j]
			continue
		}
		n, ok := cssNumber(tok)
		if !ok {
			return ui.Color{}, false
		}
		ch[i] = uint8(max(0, min(255, n)) + 0.5)
	}
	alpha := float32(origin.A) / 255
	if len(parts) == 2 && parts[1] != "alpha" {
		if alpha, ok = cssNumberOrPct(parts[1]); !ok {
			return ui.Color{}, false
		}
	}
	return ui.RGBA(ch[0], ch[1], ch[2], alpha), true
}

// cssHue reads a hue in degrees: a number, deg, grad, rad or turn.
func cssHue(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	for _, u := range [...]struct {
		unit  string
		scale float32
	}{{"deg", 1}, {"grad", 0.9}, {"rad", 180 / math.Pi}, {"turn", 360}} {
		if n, ok := strings.CutSuffix(s, u.unit); ok {
			f, ok := cssNumber(n)
			return f * u.scale, ok
		}
	}
	return cssNumber(s)
}

// cssHSL converts a hue in degrees and saturation and lightness 0–1.
func cssHSL(h, s, l float32) (uint8, uint8, uint8) {
	h = float32(math.Mod(float64(h), 360))
	if h < 0 {
		h += 360
	}
	f := func(n float32) uint8 {
		k := float32(math.Mod(float64(n+h/30), 12))
		a := s * min(l, 1-l)
		v := l - a*max(-1, min(k-3, 9-k, 1))
		return uint8(max(0, min(1, v))*255 + 0.5)
	}
	return f(0), f(8), f(4)
}

// cssHexColor reads the digits of #rgb, #rgba, #rrggbb or #rrggbbaa.
func cssHexColor(h string) (ui.Color, bool) {
	var v [8]uint8
	if len(h) != 3 && len(h) != 4 && len(h) != 6 && len(h) != 8 {
		return ui.Color{}, false
	}
	for i := range len(h) {
		ch := h[i]
		switch {
		case ch >= '0' && ch <= '9':
			v[i] = ch - '0'
		case ch >= 'a' && ch <= 'f':
			v[i] = ch - 'a' + 10
		default:
			return ui.Color{}, false
		}
	}
	if len(h) <= 4 {
		a := uint8(15)
		if len(h) == 4 {
			a = v[3]
		}
		return ui.Color{R: v[0] * 17, G: v[1] * 17, B: v[2] * 17, A: a * 17}, true
	}
	a := uint8(255)
	if len(h) == 8 {
		a = v[6]<<4 | v[7]
	}
	return ui.Color{R: v[0]<<4 | v[1], G: v[2]<<4 | v[3], B: v[4]<<4 | v[5], A: a}, true
}

// cssHex writes a color as #rrggbbaa, as Tern's variables hold it.
func cssHex(c ui.Color) string {
	const digits = "0123456789abcdef"
	b := [9]byte{'#'}
	for i, v := range [4]uint8{c.R, c.G, c.B, c.A} {
		b[1+2*i], b[2+2*i] = digits[v>>4], digits[v&15]
	}
	return string(b[:])
}

// cssNamed are the basic named colors.
var cssNamed = map[string]uint32{
	"black": 0x000000, "white": 0xffffff, "red": 0xff0000, "green": 0x008000, "blue": 0x0000ff,
	"yellow": 0xffff00, "gray": 0x808080, "grey": 0x808080, "silver": 0xc0c0c0, "maroon": 0x800000,
	"purple": 0x800080, "fuchsia": 0xff00ff, "magenta": 0xff00ff, "lime": 0x00ff00, "olive": 0x808000,
	"navy": 0x000080, "teal": 0x008080, "aqua": 0x00ffff, "cyan": 0x00ffff, "orange": 0xffa500,
	"pink": 0xffc0cb, "brown": 0xa52a2a, "gold": 0xffd700, "indigo": 0x4b0082, "violet": 0xee82ee,
	"crimson": 0xdc143c, "coral": 0xff7f50, "tomato": 0xff6347, "salmon": 0xfa8072, "khaki": 0xf0e68c,
	"lightgray": 0xd3d3d3, "lightgrey": 0xd3d3d3, "darkgray": 0xa9a9a9, "darkgrey": 0xa9a9a9,
	"dimgray": 0x696969, "whitesmoke": 0xf5f5f5, "rebeccapurple": 0x663399, "royalblue": 0x4169e1,
	"steelblue": 0x4682b4, "skyblue": 0x87ceeb, "seagreen": 0x2e8b57, "forestgreen": 0x228b22,
	"darkred": 0x8b0000, "darkblue": 0x00008b, "darkgreen": 0x006400, "darkorange": 0xff8c00,
}

// cssDisplays are the display values the engine knows.
var cssDisplays = map[string]bool{"none": true, "block": true, "inline": true, "inline-block": true,
	"flex": true, "inline-flex": true, "grid": true, "inline-grid": true, "contents": true,
	"list-item": true, "flow-root": true, "table": true, "table-row": true, "table-cell": true,
	"table-row-group": true, "table-header-group": true, "-webkit-box": true, "-webkit-inline-box": true}

var cssPseudoElements = map[string]bool{"before": true, "after": true, "marker": true,
	"placeholder": true, "selection": true, "-webkit-scrollbar": true}

// cssAliases are the vendor and legacy names of properties.
var cssAliases = map[string]string{"-webkit-backdrop-filter": "backdrop-filter", "-webkit-mask": "mask",
	"-webkit-background-clip": "background-clip", "-webkit-user-select": "user-select",
	"-webkit-transform": "transform", "-webkit-filter": "filter", "-webkit-app-region": "app-region",
	"-webkit-line-clamp": "line-clamp", "word-wrap": "overflow-wrap", "grid-gap": "gap",
	"grid-row-gap": "row-gap", "grid-column-gap": "column-gap", "-webkit-mask-image": "mask-image",
	"-webkit-mask-position": "mask-position", "-webkit-mask-size": "mask-size",
	"-webkit-mask-repeat": "mask-repeat", "-webkit-mask-origin": "mask-origin",
	"-webkit-mask-clip": "mask-clip", "-webkit-mask-composite": "mask-composite"}

// cssIgnored are accepted for web sheets and dropped without an error.
var cssIgnored = map[string]bool{"-moz-osx-font-smoothing": true, "text-rendering": true,
	"-webkit-tap-highlight-color": true, "-webkit-box-orient": true}

// cssKnown are the properties of Tern's engine (styles/css.md): those
// the window does not draw are accepted and ignored.
var cssKnown = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Fields(`display position top right bottom left z-index box-sizing width height
		min-width min-height max-width max-height margin-top margin-right margin-bottom margin-left padding-top
		padding-right padding-bottom padding-left overflow-x overflow-y visibility zoom flex-direction flex-wrap
		flex-grow flex-shrink flex-basis order align-items align-self align-content justify-content justify-items
		justify-self row-gap column-gap grid-template-columns grid-template-rows grid-column grid-row color
		color-scheme opacity background-color background-image background-position background-size
		background-repeat background-origin background-clip object-fit image-rendering border-top-width
		border-right-width border-bottom-width border-left-width border-top-style border-right-style
		border-bottom-style border-left-style border-top-color border-right-color border-bottom-color
		border-left-color border-top-left-radius border-top-right-radius border-bottom-right-radius
		border-bottom-left-radius border-collapse outline-width outline-style outline-color outline-offset
		box-shadow transform transform-origin translate rotate scale perspective perspective-origin filter
		backdrop-filter mix-blend-mode isolation clip-path mask-image mask-position mask-size mask-repeat
		mask-origin mask-clip mask-composite mask-mode will-change font-family font-size font-weight font-style
		line-height letter-spacing text-align text-decoration-line text-decoration-style text-decoration-color
		text-overflow line-clamp white-space word-break overflow-wrap text-wrap text-transform
		font-variant-numeric -webkit-font-smoothing -stencil-text-snap tab-size vertical-align list-style-type
		list-style-position list-style-image content caret-color cursor pointer-events user-select touch-action
		scrollbar-width scrollbar-color overscroll-behavior scroll-behavior overflow-anchor scroll-padding-top
		scroll-padding-right scroll-padding-bottom scroll-padding-left scroll-snap-type scroll-snap-align
		scroll-snap-stop app-region transition-property transition-duration transition-timing-function
		transition-delay animation-name animation-duration animation-timing-function animation-delay
		animation-iteration-count animation-direction animation-fill-mode animation-play-state fill fill-opacity
		stroke stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset
		stroke-opacity stop-color stop-opacity rx ry all margin padding inset scroll-padding border-width
		border-style border-color border-top border-right border-bottom border-left border border-radius outline
		background mask font flex flex-flow gap place-items place-content overflow grid-template text-decoration
		list-style transition animation -stencil-caret-animation -stencil-shape -stencil-progress -stencil-time
		-stencil-text-effect -stencil-text-stagger -stencil-text-offset -stencil-text-scale -stencil-text-blur
		-stencil-text-duration -stencil-snapshot-transition -stencil-snapshot-duration -stencil-snapshot-target
		-stencil-background-extension -stencil-particles -stencil-follow -stencil-follow-target -stencil-squash`) {
		m[p] = true
	}
	return m
}()

// cssStripComments blanks the comments of a sheet, keeping its offsets
// and lines, outside strings.
func cssStripComments(s string) string {
	if !strings.Contains(s, "/*") {
		return s
	}
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '"', '\'':
			i = cssStringEnd(s, i)
		case '/':
			if i+1 < len(b) && b[i+1] == '*' {
				end := strings.Index(s[i+2:], "*/")
				stop := len(b)
				if end >= 0 {
					stop = i + 2 + end + 2
				}
				for j := i; j < stop; j++ {
					if b[j] != '\n' {
						b[j] = ' '
					}
				}
				i = stop - 1
			}
		}
	}
	return string(b)
}

// cssStringEnd is the index of the quote closing the string at i, or the
// last index.
func cssStringEnd(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case q:
			return j
		}
	}
	return len(s) - 1
}

// cssCloseParen is the index of the ')' closing the '(' at i, or -1.
func cssCloseParen(s string, i int) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '"', '\'':
			j = cssStringEnd(s, j)
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// cssSplit splits s at sep outside parentheses, brackets and strings,
// each part trimmed.
func cssSplit(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; ch {
		case '"', '\'':
			i = cssStringEnd(s, i)
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// cssFields splits s at whitespace outside parentheses and strings.
func cssFields(s string) []string {
	var out []string
	depth, start := 0, -1
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if cssSpace(ch) && depth == 0 {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
		switch ch {
		case '"', '\'':
			i = cssStringEnd(s, i)
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func cssUnquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func cssSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func cssIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c >= 0x80
}
