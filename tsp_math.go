package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"gorex/internal/rex"
)

// Formulas, the math kind and the math within md nodes ($…$, $$…$$),
// are typeset here from their TeX: a subset of LaTeX's math as language
// models and programs write it, parsed into a tree of atoms and laid out
// as TeX lays out math lists (its styles, its spacing between classes of
// atoms, its rules for scripts, fractions and radicals), with the
// metrics TeX's fonts have, in ems, as the system's fonts do not tell
// how tall each glyph's ink is. The glyphs are the system's serif and
// math fonts; delimiters taller than a line, radicals, rules and accents
// are drawn. What is laid out is kept, by its TeX, size and color, so
// that a formula streaming in or drawn every frame is laid out once.

// Fonts of formulas: letters and digits in a serif text face, which has
// a true italic, and symbols in a math face, which has them all.
const (
	mathTextFamily   = "Cambria, STIX Two Text, Times New Roman, Latin Modern Roman, DejaVu Serif, serif"
	mathSymbolFamily = "Cambria Math, STIX Two Math, Latin Modern Math, Cambria, Times New Roman, DejaVu Serif, serif"
	mathSansFamily   = "Segoe UI, Helvetica Neue, Arial, DejaVu Sans, sans-serif"
	mathMonoFamily   = "Consolas, Menlo, DejaVu Sans Mono, monospace"
)

// mathTextScale is how much larger than the text around them formulas
// are set: TeX's faces read smaller than a UI's at one size.
const mathTextScale = 1.1

// tspMathSize is the size of a surface's formulas, beside its text of 13.
const tspMathSize = 13 * mathTextScale

// tspMDExtensions are the Markdown syntax of md nodes: the default, which
// has math in $$, \[ and \(, and math between single dollars.
var tspMDExtensions = parser.Default | parser.ExtDollarMath

// tspMath draws a math node: its formula, in display style centered in
// the node's width, or inline at its start; one that does not typeset
// shows its source as code, with the error as its tooltip.
func (a *App) tspMath(c *ui.Context, key *ui.Context, k *colors, n *rex.TSPNode) ui.Element {
	text, _ := n.Props["text"].(string)
	display, _ := n.Props["display"].(bool)
	set := typesetMath(text, tspMathSize, display, k.text)
	if set.err != nil {
		el := a.mathError(key, k, text, set.err)
		if !a.tspHead {
			el.Selectable()
		}
		return el
	}
	return set.element(key, c, display)
}

// tspMDMath builds the display math of md nodes, at the size of their
// text.
func (a *App) tspMDMath(k *colors) func(c *ui.Context, tex string) {
	return func(c *ui.Context, tex string) {
		set := typesetMath(tex, c.Theme().FontSize*mathTextScale, true, k.text)
		if set.err != nil {
			a.mathError(c, k, tex, set.err)
			return
		}
		set.element(c, c, true)
	}
}

// tspInlineMath makes the math within md nodes' text objects of it; what
// does not typeset shows as its TeX.
func tspInlineMath(k *colors) func(tex string, size float32) (markdown.InlineObject, bool) {
	return func(tex string, size float32) (markdown.InlineObject, bool) {
		set := typesetMath(tex, size*mathTextScale, false, k.text)
		if set.err != nil {
			return markdown.InlineObject{}, false
		}
		return set.inline, true
	}
}

// mathError shows a formula that does not typeset: its source, as code,
// and why as its tooltip.
func (a *App) mathError(c *ui.Context, k *colors, tex string, err error) ui.Element {
	return ui.Text(c, tex).Font(a.editorFont().Family).FontSize(12).TextColor(k.textMuted).Tooltip(err.Error())
}

// mathSet is a formula typeset at a size, in a color: its box, or why it
// does not typeset, and how it paints, kept with it.
type mathSet struct {
	box    *mathBox
	err    error
	paint  func(p *ui.Painter, r ui.Rect)
	inline markdown.InlineObject
}

type mathKey struct {
	tex     string
	size    float32
	display bool
	color   ui.Color
}

// The cache's bounds: past the soft ones it drops the formulas not drawn
// for mathFresh, so a page of many formulas shown frame after frame stays
// typeset; past the hard ones it drops the least recently used whatever
// their age, down to the soft ones. Sizes are counted in paint ops.
const (
	mathSoftSets = 512
	mathHardSets = 4096
	mathSoftOps  = 1 << 18
	mathHardOps  = 1 << 20
	mathFresh    = 2 * time.Second
)

type mathEntry struct {
	set  *mathSet
	ops  int
	used time.Time
}

// mathCache keeps the formulas typeset lately, within the bounds above.
var mathCache struct {
	sync.Mutex
	m   map[mathKey]*mathEntry
	ops int // of all the entries
}

// typesetMath lays out tex, TeX without its delimiters, size DIPs, in
// display or text style, to be painted in color; the same formula again
// is the one laid out before. It typesets outside the cache's lock.
func typesetMath(tex string, size float32, display bool, color ui.Color) *mathSet {
	key := mathKey{tex, size, display, color}
	mathCache.Lock()
	if e, ok := mathCache.m[key]; ok {
		e.used = time.Now()
		mathCache.Unlock()
		return e.set
	}
	mathCache.Unlock()
	s := &mathSet{}
	ops := 0
	if nodes, err := parseMath(tex); err != nil {
		s.err = err
	} else {
		st := mathStyle{level: 1}
		if display {
			st.level = 0
		}
		b := (&mathLayout{px: size}).list(nodes, st)
		s.box = b
		s.paint = func(p *ui.Painter, r ui.Rect) { b.draw(p, r.X, r.Y+b.h, color) }
		s.inline = markdown.InlineObject{Width: b.w, Height: b.h, Depth: b.d, Draw: s.paint}
		ops = len(b.ops)
	}
	mathCache.Lock()
	defer mathCache.Unlock()
	now := time.Now()
	if e, ok := mathCache.m[key]; ok {
		e.used = now
		return e.set
	}
	if mathCache.m == nil {
		mathCache.m = map[mathKey]*mathEntry{}
	}
	mathCache.m[key] = &mathEntry{set: s, ops: ops, used: now}
	mathCache.ops += ops
	mathTrim(now)
	return s
}

// mathTrim keeps the cache within its bounds, its lock held.
func mathTrim(now time.Time) {
	c := &mathCache
	if len(c.m) <= mathSoftSets && c.ops <= mathSoftOps {
		return
	}
	for k, e := range c.m {
		if now.Sub(e.used) >= mathFresh {
			delete(c.m, k)
			c.ops -= e.ops
		}
	}
	if len(c.m) <= mathHardSets && c.ops <= mathHardOps {
		return
	}
	keys := make([]mathKey, 0, len(c.m))
	for k := range c.m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b mathKey) int { return c.m[a].used.Compare(c.m[b].used) })
	for _, k := range keys {
		if len(c.m) <= mathSoftSets && c.ops <= mathSoftOps {
			break
		}
		c.ops -= c.m[k].ops
		delete(c.m, k)
	}
}

// element shows the formula as a box of its size: on its own, or, in
// display style, centered in the width, scrolling sideways when wider.
func (s *mathSet) element(key, c *ui.Context, display bool) ui.Element {
	b := s.box
	if !display {
		return ui.Box(key).Size(max(b.w, 1), b.h+b.d).Shrink(0).AlignSelf(ui.Start).Draw(s.paint)
	}
	row := ui.ScrollHorizontal(key).FillWidth().Padding(4, 0)
	row.Children(func() {
		ui.Box(c).Size(max(b.w, 1), b.h+b.d).Shrink(0).Margin(0, ui.Auto).Draw(s.paint)
	})
	return row
}

// The parse: a formula is a list of nodes, atoms of a class and the
// structures TeX builds of lists, as fractions and scripts.

// mathClass is the class of an atom, which sets the space between it
// and its neighbours.
type mathClass uint8

const (
	mOrd mathClass = iota
	mOp
	mBin
	mRel
	mOpen
	mClose
	mPunct
	mInner
	mNone // spacing, which no space is put around
)

// mathFont is the alphabet of a part of a formula, as \mathbf sets it.
type mathFont uint8

const (
	fontDefault mathFont = iota // letters in italic, the rest upright
	fontRm
	fontIt
	fontBf
	fontBfIt
	fontBb
	fontCal
	fontFrak
	fontSf
	fontTt
)

type mathKind uint8

const (
	kAtom      mathKind = iota // a symbol, or an operator's name: text
	kText                      // text, as \text{…} has it
	kGroup                     // a list in braces, of class class
	kScripts                   // base with sup and sub
	kFrac                      // a over b, with a rule or (binomials) in parentheses
	kSqrt                      // the root of a, its index b
	kLeftRight                 // a between delimiters left and right
	kBigDelim                  // a delimiter of a size, \big(
	kAccent                    // a under an accent, text
	kOverline                  // a with a rule over it
	kUnderline                 // a with a rule under it
	kBrace                     // a with a brace over it (text "over") or under it
	kBoxed                     // a in a frame
	kPhantom                   // the room a takes
	kSpace                     // a space em wide
	kStyle                     // what follows in style style
	kArray                     // the cells of rows, in environment text
)

// mathNode is a node of a formula.
type mathNode struct {
	kind  mathKind
	class mathClass
	text  string
	font  mathFont
	// big is an operator drawn larger in display style, as \sum, and
	// limits whether its scripts are set above and below it: 1 in display
	// style, 2 always (\limits), -1 never.
	big    bool
	limits int8
	a, b   []*mathNode
	base   *mathNode
	sup    []*mathNode
	sub    []*mathNode
	hasSup bool
	hasSub bool
	// style is a style's level (0 display to 3 scriptscript), for kStyle
	// and for fractions in a style of their own (\dfrac), -1 otherwise.
	style       int8
	rule        bool // a fraction's rule
	left, right string
	em          float32 // a space's width, a delimiter's size
	rows        [][][]*mathNode
	cols        string // an array's columns' alignments
}

type mathSym struct {
	text   string
	class  mathClass
	big    bool
	limits int8
}

// mathSymbols are the symbols of commands, \alpha to \Rightarrow.
var mathSymbols = func() map[string]mathSym {
	m := map[string]mathSym{}
	add := func(class mathClass, pairs string) {
		f := strings.Fields(pairs)
		for i := 0; i+1 < len(f); i += 2 {
			m[f[i]] = mathSym{text: f[i+1], class: class}
		}
	}
	add(mOrd, `alpha α beta β gamma γ delta δ epsilon ϵ varepsilon ε zeta ζ eta η theta θ vartheta ϑ
		iota ι kappa κ varkappa ϰ lambda λ mu μ nu ν xi ξ omicron ο pi π varpi ϖ rho ρ varrho ϱ
		sigma σ varsigma ς tau τ upsilon υ phi ϕ varphi φ chi χ psi ψ omega ω digamma ϝ
		Gamma Γ Delta Δ Theta Θ Lambda Λ Xi Ξ Pi Π Sigma Σ Upsilon Υ Phi Φ Psi Ψ Omega Ω
		varGamma Γ varDelta Δ varTheta Θ varLambda Λ varXi Ξ varPi Π varSigma Σ varPhi Φ varPsi Ψ varOmega Ω`)
	add(mOrd, `infty ∞ partial ∂ nabla ∇ hbar ℏ hslash ℏ ell ℓ Re ℜ Im ℑ aleph ℵ beth ℶ gimel ℷ wp ℘
		emptyset ∅ varnothing ∅ prime ′ backprime ‵ angle ∠ measuredangle ∡ triangle △ forall ∀ exists ∃
		nexists ∄ neg ¬ lnot ¬ top ⊤ bot ⊥ surd √ flat ♭ sharp ♯ natural ♮ clubsuit ♣ diamondsuit ♢
		heartsuit ♡ spadesuit ♠ S § P ¶ checkmark ✓ mho ℧ Box □ square □ blacksquare ■ backslash \
		vert | Vert ‖ | ‖ imath ı jmath ȷ eth ð complement ∁ degree ° pounds £ yen ¥ copyright ©
		circledR ® maltese ✠ Finv Ⅎ Game ⅁ $ $ % % & & # # _ _ lozenge ◊ blacklozenge ⧫
		bigstar ★ diagup ╱ diagdown ╲ infin ∞`)
	add(mInner, `ldots … cdots ⋯ dots … dotsc … dotsb ⋯ dotsm ⋯ dotsi ⋯ dotso … vdots ⋮ ddots ⋱`)
	add(mBin, `pm ± mp ∓ times × div ÷ cdot ⋅ ast ∗ circ ∘ bullet ∙ oplus ⊕ ominus ⊖ otimes ⊗
		oslash ⊘ odot ⊙ cup ∪ cap ∩ uplus ⊎ sqcup ⊔ sqcap ⊓ vee ∨ lor ∨ wedge ∧ land ∧ setminus ∖
		smallsetminus ∖ wr ≀ amalg ⨿ bigcirc ◯ dagger † ddagger ‡ dag † ddag ‡ triangleleft ◁
		triangleright ▷ bigtriangleup △ bigtriangledown ▽ diamond ⋄ lhd ⊲ rhd ⊳ unlhd ⊴ unrhd ⊵
		ltimes ⋉ rtimes ⋊ boxplus ⊞ boxminus ⊟ boxtimes ⊠ boxdot ⊡ dotplus ∔ centerdot ⋅ intercal ⊺
		circledast ⊛ circledcirc ⊚ divideontimes ⋇ curlyvee ⋎ curlywedge ⋏ star ⋆`)
	add(mRel, `leq ≤ le ≤ geq ≥ ge ≥ neq ≠ ne ≠ approx ≈ approxeq ≊ equiv ≡ sim ∼ simeq ≃ cong ≅
		propto ∝ ll ≪ gg ≫ lll ⋘ ggg ⋙ prec ≺ succ ≻ preceq ⪯ succeq ⪰ subset ⊂ supset ⊃ subseteq ⊆
		supseteq ⊇ subsetneq ⊊ supsetneq ⊋ nsubseteq ⊈ nsupseteq ⊉ sqsubset ⊏ sqsupset ⊐ sqsubseteq ⊑
		sqsupseteq ⊒ in ∈ ni ∋ owns ∋ notin ∉ mid ∣ nmid ∤ parallel ∥ nparallel ∦ perp ⊥ models ⊨
		vdash ⊢ dashv ⊣ vDash ⊨ Vdash ⊩ to → rightarrow → leftarrow ← gets ← leftrightarrow ↔
		Rightarrow ⇒ Leftarrow ⇐ Leftrightarrow ⇔ implies ⟹ impliedby ⟸ iff ⟺ longrightarrow ⟶
		longleftarrow ⟵ longleftrightarrow ⟷ Longrightarrow ⟹ Longleftarrow ⟸ Longleftrightarrow ⟺
		mapsto ↦ longmapsto ⟼ uparrow ↑ downarrow ↓ updownarrow ↕ Uparrow ⇑ Downarrow ⇓
		Updownarrow ⇕ nearrow ↗ searrow ↘ swarrow ↙ nwarrow ↖ hookrightarrow ↪ hookleftarrow ↩
		rightharpoonup ⇀ rightharpoondown ⇁ leftharpoonup ↼ leftharpoondown ↽ rightleftharpoons ⇌
		leftrightharpoons ⇋ rightleftarrows ⇄ leftrightarrows ⇆ twoheadrightarrow ↠
		twoheadleftarrow ↞ rightsquigarrow ⇝ leadsto ⇝ circlearrowleft ↺ circlearrowright ↻
		curvearrowleft ↶ curvearrowright ↷ asymp ≍ doteq ≐ bowtie ⋈ Join ⋈ smile ⌣ frown ⌢
		lesssim ≲ gtrsim ≳ leqslant ⩽ geqslant ⩾ lessgtr ≶ gtrless ≷ nless ≮ ngtr ≯ nleq ≰ ngeq ≱
		nsim ≁ ncong ≇ coloneqq ≔ coloneq ≔ eqqcolon ≕ triangleq ≜ eqcirc ≖ circeq ≗ thicksim ∼
		thickapprox ≈ backsim ∽ vartriangleleft ⊲ vartriangleright ⊳ trianglelefteq ⊴
		trianglerighteq ⊵ lt < gt > because ∵ therefore ∴ between ≬ pitchfork ⋔ multimap ⊸
		leqq ≦ geqq ≧ precsim ≾ succsim ≿ sqsubsetneq ⋤ sqsupsetneq ⋥`)
	add(mPunct, `colon : ldotp . cdotp ⋅`)
	add(mOpen, `{ { lbrace { langle ⟨ lfloor ⌊ lceil ⌈ lvert | lVert ‖ lbrack [ lgroup ⟮ ulcorner ⌜ llcorner ⌞`)
	add(mClose, `} } rbrace } rangle ⟩ rfloor ⌋ rceil ⌉ rvert | rVert ‖ rbrack ] rgroup ⟯ urcorner ⌝ lrcorner ⌟`)
	// Big operators: sums and the like with their limits above and below
	// in display style, integrals with them beside.
	for _, f := range [][2]string{{"sum", "∑"}, {"prod", "∏"}, {"coprod", "∐"}, {"bigcup", "⋃"}, {"bigcap", "⋂"},
		{"bigvee", "⋁"}, {"bigwedge", "⋀"}, {"bigoplus", "⨁"}, {"bigotimes", "⨂"}, {"bigodot", "⨀"},
		{"biguplus", "⨄"}, {"bigsqcup", "⨆"}} {
		m[f[0]] = mathSym{text: f[1], class: mOp, big: true, limits: 1}
	}
	for _, f := range [][2]string{{"int", "∫"}, {"iint", "∬"}, {"iiint", "∭"}, {"oint", "∮"}, {"oiint", "∯"}, {"intop", "∫"}} {
		m[f[0]] = mathSym{text: f[1], class: mOp, big: true, limits: -1}
	}
	return m
}()

// mathRuneClasses are the classes of the symbols the commands stand for,
// for a formula that has them as they are: ≤ is a relation.
// Only binary operators and relations, the latter where a symbol is
// both, and the big operators and fences, which mean one thing each.
var mathRuneClasses = func() map[rune]mathClass {
	m := map[rune]mathClass{}
	for _, class := range []mathClass{mBin, mRel} {
		for _, s := range mathSymbols {
			if r, n := utf8.DecodeRuneInString(s.text); n == len(s.text) && s.class == class {
				m[r] = class
			}
		}
	}
	for _, r := range "∑∏∐⋃⋂⋁⋀∫∬∭∮" {
		m[r] = mOp
	}
	for _, r := range "⟨⌊⌈" {
		m[r] = mOpen
	}
	for _, r := range "⟩⌋⌉" {
		m[r] = mClose
	}
	return m
}()

// mathFuncs are the operators named in upright letters, sin to lim: those
// with limits have them above and below in display style.
var mathFuncs = func() map[string]mathSym {
	m := map[string]mathSym{}
	for _, f := range strings.Fields("sin cos tan cot sec csc arcsin arccos arctan arccot sinh cosh tanh coth sech csch log ln lg exp ker dim hom deg arg sgn") {
		m[f] = mathSym{text: f, class: mOp, limits: -1}
	}
	for _, f := range strings.Fields("lim max min sup inf det gcd Pr") {
		m[f] = mathSym{text: f, class: mOp, limits: 1}
	}
	m["limsup"] = mathSym{text: "lim sup", class: mOp, limits: 1}
	m["liminf"] = mathSym{text: "lim inf", class: mOp, limits: 1}
	m["injlim"] = mathSym{text: "inj lim", class: mOp, limits: 1}
	m["projlim"] = mathSym{text: "proj lim", class: mOp, limits: 1}
	return m
}()

// mathSpaces are the widths of the spacing commands, in ems.
var mathSpaces = map[string]float32{
	",": 3.0 / 18, "thinspace": 3.0 / 18, ":": 4.0 / 18, ">": 4.0 / 18, "medspace": 4.0 / 18,
	";": 5.0 / 18, "thickspace": 5.0 / 18, "!": -3.0 / 18, "negthinspace": -3.0 / 18,
	"negmedspace": -4.0 / 18, "negthickspace": -5.0 / 18, "quad": 1, "qquad": 2, "enspace": 0.5,
	" ": 1.0 / 3, "space": 1.0 / 3, "nobreakspace": 1.0 / 3,
}

// mathDelims are the delimiters \left, \right and \big take, by command.
var mathDelims = map[string]string{
	"{": "{", "}": "}", "lbrace": "{", "rbrace": "}", "langle": "⟨", "rangle": "⟩", "lvert": "|",
	"rvert": "|", "vert": "|", "|": "‖", "Vert": "‖", "lVert": "‖", "rVert": "‖", "lfloor": "⌊",
	"rfloor": "⌋", "lceil": "⌈", "rceil": "⌉", "backslash": "\\", "lbrack": "[", "rbrack": "]",
}

// mathBigs are the sizes of \big's delimiters, in ems, and the class of
// each kind.
var mathBigs = func() map[string]mathNode {
	m := map[string]mathNode{}
	for name, em := range map[string]float32{"big": 1.2, "Big": 1.8, "bigg": 2.4, "Bigg": 3} {
		m[name] = mathNode{em: em, class: mOrd}
		m[name+"l"] = mathNode{em: em, class: mOpen}
		m[name+"r"] = mathNode{em: em, class: mClose}
		m[name+"m"] = mathNode{em: em, class: mRel}
	}
	return m
}()

// mathFonts are the alphabets of commands, \mathbf{…}.
var mathFonts = map[string]mathFont{
	"mathrm": fontRm, "mathup": fontRm, "mathit": fontIt, "mathbf": fontBf, "boldsymbol": fontBfIt,
	"bm": fontBfIt, "mathbb": fontBb, "mathcal": fontCal, "mathscr": fontCal, "mathfrak": fontFrak,
	"mathsf": fontSf, "mathtt": fontTt, "mathnormal": fontDefault,
}

// mathTexts are the commands of text, \text{…}, and their alphabets.
var mathTexts = map[string]mathFont{
	"text": fontRm, "textrm": fontRm, "textnormal": fontRm, "textup": fontRm, "mbox": fontRm,
	"hbox": fontRm, "textbf": fontBf, "textit": fontIt, "textsf": fontSf, "texttt": fontTt,
}

// mathClasses are the commands that make a list an atom of a class.
var mathClasses = map[string]mathClass{
	"mathord": mOrd, "mathop": mOp, "mathbin": mBin, "mathrel": mRel, "mathopen": mOpen,
	"mathclose": mClose, "mathpunct": mPunct, "mathinner": mInner,
}

// mathAccents are the accents, and whether each is as wide as what it is
// over.
var mathAccents = map[string]bool{
	"hat": false, "widehat": true, "check": false, "widecheck": true, "tilde": false,
	"widetilde": true, "bar": false, "vec": false, "overrightarrow": true, "overleftarrow": true,
	"overleftrightarrow": true, "dot": false, "ddot": false, "acute": false, "grave": false,
	"breve": false, "mathring": false,
}

// mathEnv is how an environment lays out its cells.
type mathEnv struct {
	left, right string
	// align is every column's alignment: 'l', 'c' or 'r', or 0 for
	// pairs of columns, the first aligned right and the second left, as
	// aligned has them.
	align   byte
	style   int8    // the cells' style, -1 for the formula's
	colsep  float32 // between columns (pairs of them), in ems
	pad     float32 // before the first column and after the last
	stretch float32 // of each row's height and depth
	gap     float32 // between rows, in ems
}

var mathEnvs = map[string]mathEnv{
	"matrix":      {align: 'c', style: 1, colsep: 1, stretch: 1},
	"pmatrix":     {left: "(", right: ")", align: 'c', style: 1, colsep: 1, stretch: 1},
	"bmatrix":     {left: "[", right: "]", align: 'c', style: 1, colsep: 1, stretch: 1},
	"Bmatrix":     {left: "{", right: "}", align: 'c', style: 1, colsep: 1, stretch: 1},
	"vmatrix":     {left: "|", right: "|", align: 'c', style: 1, colsep: 1, stretch: 1},
	"Vmatrix":     {left: "‖", right: "‖", align: 'c', style: 1, colsep: 1, stretch: 1},
	"smallmatrix": {align: 'c', style: 2, colsep: 0.5, stretch: 0.8},
	"array":       {align: 'c', style: 1, colsep: 1, pad: 0.5, stretch: 1},
	"cases":       {left: "{", align: 'l', style: 1, colsep: 1, stretch: 1.2},
	"dcases":      {left: "{", align: 'l', style: 0, colsep: 1, stretch: 1.2},
	"rcases":      {right: "}", align: 'l', style: 1, colsep: 1, stretch: 1.2},
	"aligned":     {style: 0, colsep: 1, stretch: 1, gap: 0.25},
	"align":       {style: 0, colsep: 1, stretch: 1, gap: 0.25},
	"align*":      {style: 0, colsep: 1, stretch: 1, gap: 0.25},
	"split":       {style: 0, colsep: 1, stretch: 1, gap: 0.25},
	"gathered":    {align: 'c', style: 0, stretch: 1, gap: 0.25},
	"gather":      {align: 'c', style: 0, stretch: 1, gap: 0.25},
	"gather*":     {align: 'c', style: 0, stretch: 1, gap: 0.25},
	"equation":    {align: 'c', style: 0, stretch: 1},
	"equation*":   {align: 'c', style: 0, stretch: 1},
	"substack":    {align: 'c', style: -1, stretch: 0.8},
}

// mathTok is a token of TeX: a command, without its backslash, or a
// character, and where it is in the source.
type mathTok struct {
	cmd     string
	r       rune // -1 at the end
	at, end int
}

func (t mathTok) is(r rune) bool { return t.cmd == "" && t.r == r }
func (t mathTok) eof() bool      { return t.cmd == "" && t.r < 0 }

// ends reports whether t ends a list: the end, a closing brace, a cell's
// or row's end, \end or \right.
func (t mathTok) ends() bool {
	return t.eof() || t.is('}') || t.is('&') || t.cmd == "\\" || t.cmd == "cr" || t.cmd == "end" || t.cmd == "right"
}

// mathTokens splits TeX into tokens, leaving out its spaces and comments.
func mathTokens(src string) ([]mathTok, error) {
	var out []mathTok
	for i := 0; i < len(src); {
		r, n := utf8.DecodeRuneInString(src[i:])
		switch {
		case r == '%':
			if j := strings.IndexByte(src[i:], '\n'); j >= 0 {
				i += j + 1
			} else {
				i = len(src)
			}
			continue
		case unicode.IsSpace(r):
			i += n
			continue
		case r == '\\':
			j := i + 1
			for j < len(src) && (src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z') {
				j++
			}
			if j == i+1 {
				if j == len(src) {
					return nil, errors.New(`a lone \ ends the formula`)
				}
				_, m := utf8.DecodeRuneInString(src[j:])
				j += m
			}
			out = append(out, mathTok{cmd: src[i+1 : j], at: i, end: j})
			i = j
			continue
		}
		out = append(out, mathTok{r: r, at: i, end: i + n})
		i += n
	}
	return out, nil
}

// mathMaxDepth is how deep atoms nest, in groups, arguments, scripts and
// environments; mathMaxCells is how many cells a formula's environments
// may lay out in all, rows times columns each, missing cells counted.
const (
	mathMaxDepth = 32
	mathMaxCells = 10000
)

type mathParser struct {
	src   string
	toks  []mathTok
	i     int
	depth int // of the list being parsed
	cells int // laid out by the environments parsed so far
}

func (p *mathParser) peek() mathTok {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return mathTok{r: -1, at: len(p.src), end: len(p.src)}
}

// parseMath parses a formula: a list, or rows of them, which \\ breaks,
// centered under each other.
func parseMath(tex string) ([]*mathNode, error) {
	toks, err := mathTokens(tex)
	if err != nil {
		return nil, err
	}
	p := &mathParser{src: tex, toks: toks}
	var rows [][][]*mathNode
	for {
		l, err := p.list(fontDefault)
		if err != nil {
			return nil, err
		}
		t := p.peek()
		switch {
		case t.eof():
			if rows == nil {
				return l, nil
			}
			if len(l) > 0 {
				rows = append(rows, [][]*mathNode{l})
			}
			return []*mathNode{{kind: kArray, text: "gathered", rows: rows}}, nil
		case t.cmd == "\\" || t.cmd == "cr":
			p.i++
			if err := p.skipLength(); err != nil {
				return nil, err
			}
			rows = append(rows, [][]*mathNode{l})
		default:
			return nil, p.stray(t)
		}
	}
}

// stray is the error of a token where it cannot be.
func (p *mathParser) stray(t mathTok) error {
	switch {
	case t.eof():
		return errors.New("unbalanced braces: a { is not closed")
	case t.is('}'):
		return errors.New("unbalanced braces: a } closes nothing")
	case t.is('&'):
		return errors.New("& outside an environment")
	case t.cmd == "\\" || t.cmd == "cr":
		return errors.New(`\\ outside an environment`)
	case t.cmd == "end":
		return errors.New(`\end without its \begin`)
	case t.cmd == "right":
		return errors.New(`\right without its \left`)
	}
	return fmt.Errorf("unexpected %s", p.src[t.at:t.end])
}

// list parses nodes up to what ends a list (mathTok.ends), which it
// leaves to the caller.
func (p *mathParser) list(font mathFont) ([]*mathNode, error) {
	var out []*mathNode
	for !p.peek().ends() {
		n, err := p.atom(font)
		if err != nil {
			return nil, err
		}
		if n.kind != kStyle && n.kind != kSpace {
			if n, err = p.scripts(n, font); err != nil {
				return nil, err
			}
		}
		out = append(out, n)
	}
	return out, nil
}

// group parses a list in braces, the opening one next.
func (p *mathParser) group(font mathFont) ([]*mathNode, error) {
	p.i++
	l, err := p.list(font)
	if err != nil {
		return nil, err
	}
	if t := p.peek(); !t.is('}') {
		return nil, p.stray(t)
	}
	p.i++
	return l, nil
}

// arg parses a command's argument: a group, or a single atom.
func (p *mathParser) arg(font mathFont, cmd string) ([]*mathNode, error) {
	t := p.peek()
	switch {
	case t.ends() || t.is('^') || t.is('_') || t.is('\''):
		return nil, fmt.Errorf("%s is missing its argument", cmd)
	case t.is('{'):
		return p.group(font)
	}
	n, err := p.atom(font)
	if err != nil {
		return nil, err
	}
	return []*mathNode{n}, nil
}

// raw reads an argument as its source: what is in its braces, or a
// single token.
func (p *mathParser) raw(cmd string) (string, error) {
	t := p.peek()
	if t.ends() {
		return "", fmt.Errorf(`\%s is missing its argument`, cmd)
	}
	if !t.is('{') {
		p.i++
		return p.src[t.at:t.end], nil
	}
	depth := 0
	for j := t.at; j < len(p.src); j++ {
		switch p.src[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				for p.i < len(p.toks) && p.toks[p.i].at <= j {
					p.i++
				}
				return p.src[t.at+1 : j], nil
			}
		}
	}
	return "", errors.New("unbalanced braces: a { is not closed")
}

// skipLength skips the optional length after \\, as \\[4pt].
func (p *mathParser) skipLength() error {
	if !p.peek().is('[') {
		return nil
	}
	for p.i++; !p.peek().is(']'); p.i++ {
		if p.peek().eof() {
			return errors.New(`\\[ is missing its ]`)
		}
	}
	p.i++
	return nil
}

// scripts parses the superscript, subscript and primes of base, and
// \limits or \nolimits after an operator.
func (p *mathParser) scripts(base *mathNode, font mathFont) (*mathNode, error) {
	var n *mathNode
	primes := false
	for {
		t := p.peek()
		switch {
		case t.is('^'), t.is('\''):
			p.i++
			if n == nil {
				n = &mathNode{kind: kScripts, base: base}
			}
			if n.hasSup && !primes {
				return nil, errors.New("double superscript")
			}
			n.hasSup = true
			if t.is('\'') {
				primes = true
				n.sup = append(n.sup, &mathNode{kind: kAtom, text: "′"})
				continue
			}
			primes = false
			arg, err := p.arg(font, "^")
			if err != nil {
				return nil, err
			}
			n.sup = append(n.sup, arg...)
		case t.is('_'):
			p.i++
			if n == nil {
				n = &mathNode{kind: kScripts, base: base}
			}
			if n.hasSub {
				return nil, errors.New("double subscript")
			}
			arg, err := p.arg(font, "_")
			if err != nil {
				return nil, err
			}
			n.sub, n.hasSub = arg, true
		case t.cmd == "limits" || t.cmd == "nolimits":
			if base.class != mOp || n != nil {
				return nil, fmt.Errorf(`\%s follows only an operator`, t.cmd)
			}
			p.i++
			base.limits = 2
			if t.cmd == "nolimits" {
				base.limits = -1
			}
		default:
			if n == nil {
				return base, nil
			}
			return n, nil
		}
	}
}

// atom parses the next node.
func (p *mathParser) atom(font mathFont) (*mathNode, error) {
	if p.depth >= mathMaxDepth {
		return nil, fmt.Errorf("nested more than %d deep", mathMaxDepth)
	}
	p.depth++
	defer func() { p.depth-- }()
	t := p.peek()
	if t.cmd == "" {
		switch t.r {
		case '{':
			l, err := p.group(font)
			return &mathNode{kind: kGroup, a: l}, err
		case '^', '_', '\'':
			// Scripts of nothing.
			return &mathNode{kind: kGroup}, nil
		}
		p.i++
		return mathChar(t.r, font)
	}
	p.i++
	return p.command(t.cmd, font)
}

// mathChar is the atom of a character of the formula.
func mathChar(r rune, font mathFont) (*mathNode, error) {
	n := &mathNode{kind: kAtom, text: string(r), font: font}
	switch r {
	case '+':
		n.class = mBin
	case '-':
		n.class, n.text = mBin, "−"
	case '*':
		n.class, n.text = mBin, "∗"
	case '=', '<', '>', ':':
		n.class = mRel
	case '(', '[':
		n.class = mOpen
	case ')', ']', '!', '?':
		n.class = mClose
	case ',', ';':
		n.class = mPunct
	case '~':
		return &mathNode{kind: kSpace, em: 1.0 / 3}, nil
	case '#', '$':
		return nil, fmt.Errorf("%c is not allowed in math", r)
	default:
		if c, ok := mathRuneClasses[r]; ok {
			n.class = c
			n.big = c == mOp
			n.limits = 1
			if strings.ContainsRune("∫∬∭∮", r) {
				n.limits = -1
			}
		}
	}
	return n, nil
}

// command parses a command, its name next, and its arguments.
func (p *mathParser) command(name string, font mathFont) (*mathNode, error) {
	if s, ok := mathSymbols[name]; ok {
		return &mathNode{kind: kAtom, class: s.class, text: s.text, big: s.big, limits: s.limits, font: font}, nil
	}
	if s, ok := mathFuncs[name]; ok {
		return &mathNode{kind: kAtom, class: mOp, text: s.text, limits: s.limits, font: fontRm}, nil
	}
	if em, ok := mathSpaces[name]; ok {
		return &mathNode{kind: kSpace, em: em}, nil
	}
	if f, ok := mathFonts[name]; ok {
		l, err := p.arg(f, `\`+name)
		return &mathNode{kind: kGroup, a: l}, err
	}
	if f, ok := mathTexts[name]; ok {
		s, err := p.raw(name)
		return &mathNode{kind: kText, text: mathUnescape(s), font: f}, err
	}
	if c, ok := mathClasses[name]; ok {
		l, err := p.arg(font, `\`+name)
		n := &mathNode{kind: kGroup, class: c, a: l}
		if c == mOp {
			n.limits = 1
		}
		return n, err
	}
	if _, ok := mathAccents[name]; ok {
		l, err := p.arg(font, `\`+name)
		return &mathNode{kind: kAccent, text: name, a: l}, err
	}
	if b, ok := mathBigs[name]; ok {
		d, err := p.delim(name)
		return &mathNode{kind: kBigDelim, class: b.class, em: b.em, text: d}, err
	}
	switch name {
	case "frac", "dfrac", "tfrac", "cfrac", "binom", "dbinom", "tbinom":
		num, err := p.arg(font, `\`+name)
		if err != nil {
			return nil, err
		}
		den, err := p.arg(font, `\`+name)
		if err != nil {
			return nil, err
		}
		n := &mathNode{kind: kFrac, a: num, b: den, rule: !strings.HasSuffix(name, "binom"), style: -1, class: mInner}
		switch name[0] {
		case 'd', 'c':
			n.style = 0
		case 't':
			n.style = 1
		}
		return n, nil
	case "sqrt":
		n := &mathNode{kind: kSqrt}
		if p.peek().is('[') {
			p.i++
			for !p.peek().is(']') {
				if p.peek().ends() {
					return nil, errors.New(`\sqrt[ is missing its ]`)
				}
				m, err := p.atom(font)
				if err == nil && m.kind != kStyle && m.kind != kSpace {
					m, err = p.scripts(m, font)
				}
				if err != nil {
					return nil, err
				}
				n.b = append(n.b, m)
			}
			p.i++
		}
		var err error
		n.a, err = p.arg(font, `\sqrt`)
		return n, err
	case "left":
		left, err := p.delim("left")
		if err != nil {
			return nil, err
		}
		body, err := p.list(font)
		if err != nil {
			return nil, err
		}
		if t := p.peek(); t.cmd != "right" {
			if t.eof() {
				return nil, errors.New(`\left is missing its \right`)
			}
			return nil, p.stray(t)
		}
		p.i++
		right, err := p.delim("right")
		return &mathNode{kind: kLeftRight, class: mInner, left: left, right: right, a: body}, err
	case "overline", "underline", "overbrace", "underbrace", "boxed", "phantom":
		l, err := p.arg(font, `\`+name)
		n := &mathNode{kind: kOverline, a: l}
		switch name {
		case "underline":
			n.kind = kUnderline
		case "overbrace", "underbrace":
			n.kind, n.text, n.class = kBrace, name[:len(name)-5], mOp
		case "boxed":
			n.kind = kBoxed
		case "phantom":
			n.kind = kPhantom
		}
		return n, err
	case "overset", "underset", "stackrel":
		over, err := p.arg(font, `\`+name)
		if err != nil {
			return nil, err
		}
		body, err := p.arg(font, `\`+name)
		if err != nil {
			return nil, err
		}
		base := &mathNode{kind: kGroup, a: body}
		if len(body) == 1 {
			base.class = mathNodeClass(body[0])
		}
		if name == "stackrel" {
			base.class = mRel
		}
		n := &mathNode{kind: kScripts, base: base, limits: 2}
		if name == "underset" {
			n.sub, n.hasSub = over, true
		} else {
			n.sup, n.hasSup = over, true
		}
		return n, nil
	case "operatorname", "operatorname*":
		n := &mathNode{kind: kGroup, class: mOp, limits: -1}
		if name == "operatorname*" || p.peek().is('*') {
			if p.peek().is('*') {
				p.i++
			}
			n.limits = 1
		}
		var err error
		n.a, err = p.arg(fontRm, `\operatorname`)
		return n, err
	case "not":
		t := p.peek()
		if t.ends() {
			return nil, errors.New(`\not is missing its symbol`)
		}
		m, err := p.atom(font)
		if err != nil {
			return nil, err
		}
		if m.kind != kAtom {
			return nil, errors.New(`\not takes a symbol`)
		}
		if neg, ok := mathNegations[m.text]; ok {
			m.text = neg
		} else {
			m.text += "\u0338"
		}
		return m, nil
	case "begin":
		env, err := p.raw("begin")
		if err != nil {
			return nil, err
		}
		if _, ok := mathEnvs[env]; !ok || env == "substack" {
			return nil, fmt.Errorf("unknown environment {%s}", env)
		}
		n := &mathNode{kind: kArray, text: env}
		if env == "array" {
			spec, err := p.raw("begin{array}")
			if err != nil {
				return nil, err
			}
			for _, r := range spec {
				if r == 'l' || r == 'c' || r == 'r' {
					n.cols += string(r)
				}
			}
			if n.cols == "" {
				return nil, errors.New(`\begin{array} has no columns`)
			}
		}
		n.rows, err = p.rows(font, env)
		return n, err
	case "substack":
		if !p.peek().is('{') {
			return nil, errors.New(`\substack is missing its argument`)
		}
		p.i++
		rows, err := p.rows(font, "")
		return &mathNode{kind: kArray, text: "substack", rows: rows}, err
	case "displaystyle", "textstyle", "scriptstyle", "scriptscriptstyle":
		level := map[string]int8{"displaystyle": 0, "textstyle": 1, "scriptstyle": 2, "scriptscriptstyle": 3}[name]
		return &mathNode{kind: kStyle, style: level}, nil
	case "bmod":
		return &mathNode{kind: kAtom, class: mBin, text: "mod", font: fontRm}, nil
	case "pmod":
		arg, err := p.arg(font, `\pmod`)
		return &mathNode{kind: kGroup, a: []*mathNode{
			{kind: kSpace, em: 1}, {kind: kAtom, class: mOpen, text: "("}, {kind: kAtom, text: "mod", font: fontRm},
			{kind: kSpace, em: 1.0 / 3}, {kind: kGroup, a: arg}, {kind: kAtom, class: mClose, text: ")"},
		}}, err
	case "limits", "nolimits":
		return nil, fmt.Errorf(`\%s follows only an operator`, name)
	}
	return nil, fmt.Errorf(`unknown command \%s`, name)
}

// mathNegations are the negated relations \not makes.
var mathNegations = map[string]string{
	"=": "≠", "<": "≮", ">": "≯", "≤": "≰", "≥": "≱", "∈": "∉", "∋": "∌", "⊂": "⊄", "⊃": "⊅",
	"⊆": "⊈", "⊇": "⊉", "∼": "≁", "≅": "≇", "≈": "≉", "≡": "≢", "∣": "∤", "∥": "∦",
}

// mathNodeClass is the class a node takes in the list it is in.
func mathNodeClass(n *mathNode) mathClass {
	switch n.kind {
	case kAtom, kGroup, kFrac, kLeftRight, kBigDelim, kBrace:
		return n.class
	case kScripts:
		return mathNodeClass(n.base)
	}
	return mOrd
}

// delim parses the delimiter after \left, \right or \big.
func (p *mathParser) delim(cmd string) (string, error) {
	t := p.peek()
	if t.cmd == "" {
		switch t.r {
		case '(', ')', '[', ']', '|', '/':
			p.i++
			return string(t.r), nil
		case '.':
			p.i++
			return "", nil
		case '<':
			p.i++
			return "⟨", nil
		case '>':
			p.i++
			return "⟩", nil
		}
	} else if d, ok := mathDelims[t.cmd]; ok {
		p.i++
		return d, nil
	}
	if t.eof() {
		return "", fmt.Errorf(`\%s is missing its delimiter`, cmd)
	}
	return "", fmt.Errorf(`\%s takes a delimiter, not %s`, cmd, p.src[t.at:t.end])
}

// rows parses the cells of an environment up to its \end, or, without a
// name (\substack), its closing brace.
func (p *mathParser) rows(font mathFont, env string) ([][][]*mathNode, error) {
	var rows [][][]*mathNode
	var row [][]*mathNode
	done := func() ([][][]*mathNode, error) {
		if len(row) > 1 || len(row) == 1 && len(row[0]) > 0 {
			rows = append(rows, row)
		}
		ncols := 0
		for _, r := range rows {
			ncols = max(ncols, len(r))
		}
		if p.cells += len(rows) * ncols; p.cells > mathMaxCells {
			return nil, fmt.Errorf("more than %d cells in environments", mathMaxCells)
		}
		return rows, nil
	}
	for {
		cell, err := p.list(font)
		if err != nil {
			return nil, err
		}
		row = append(row, cell)
		t := p.peek()
		switch {
		case t.is('&'):
			p.i++
		case t.cmd == "\\" || t.cmd == "cr":
			p.i++
			if err := p.skipLength(); err != nil {
				return nil, err
			}
			rows, row = append(rows, row), nil
		case env != "" && t.cmd == "end":
			p.i++
			name, err := p.raw("end")
			if err != nil {
				return nil, err
			}
			if name != env {
				return nil, fmt.Errorf(`\begin{%s} ended by \end{%s}`, env, name)
			}
			return done()
		case env == "" && t.is('}'):
			p.i++
			return done()
		case t.eof() && env != "":
			return nil, fmt.Errorf(`\begin{%s} is missing its \end{%s}`, env, env)
		default:
			return nil, p.stray(t)
		}
	}
}

// mathUnescape is the text of \text{…}: its escaped characters as they
// are, its newlines spaces.
func mathUnescape(s string) string {
	if !strings.ContainsAny(s, "\\~\n\r\t") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && strings.IndexByte("{}$%&#_ ", s[i+1]) >= 0:
			i++
			b.WriteByte(s[i])
		case c == '\\' && i+1 < len(s) && s[i+1] == '\\':
			i++
			b.WriteByte(' ')
		case c == '~', c == '\n', c == '\r', c == '\t':
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// The layout: each node a box, laid out in a style, whose size is its
// font's; boxes of lists are their nodes side by side, with TeX's spaces
// between them.

// mathStyle is TeX's style: display, text, script or scriptscript (0 to
// 3), and whether it is cramped, its superscripts set lower.
type mathStyle struct {
	level   int8
	cramped bool
}

var mathScales = [4]float32{1, 1, 0.7, 0.5}

func (s mathStyle) sup() mathStyle {
	if s.level < 2 {
		return mathStyle{2, s.cramped}
	}
	return mathStyle{3, s.cramped}
}

func (s mathStyle) sub() mathStyle { return mathStyle{s.sup().level, true} }

func (s mathStyle) num() mathStyle {
	return mathStyle{min(s.level+1, 3), s.cramped}
}

func (s mathStyle) den() mathStyle { return mathStyle{s.num().level, true} }

// mathBox is a laid out part of a formula: w wide, h above its baseline
// and d below it, and what it paints, from its origin on its baseline at
// its left. ic is the italic correction of its last letter, which a
// superscript leaves room for, and subKern how much further left a
// subscript sits, as under an integral's slant.
type mathBox struct {
	w, h, d     float32
	ic, subKern float32
	ops         []mathOp
	scratch     ui.Path
}

type mathOpKind uint8

const (
	opGlyphs mathOpKind = iota
	opRule
	opPath
)

// mathOp is a thing a box paints, at x, y from its origin: glyphs on
// that baseline, a rule (rect, from x, y), or a path (segs, from x, y),
// stroked stroke wide or, without, filled.
type mathOp struct {
	kind   mathOpKind
	x, y   float32
	glyphs []ui.Glyph
	rect   ui.Rect
	segs   []mathSeg
	stroke float32
}

// mathSeg is a command of a path: move, line, quad, cubic or close (0 to
// 4), with its points.
type mathSeg struct {
	op uint8
	p  [6]float32
}

type mathPath []mathSeg

func (p *mathPath) move(x, y float32) { *p = append(*p, mathSeg{op: 0, p: [6]float32{x, y}}) }
func (p *mathPath) line(x, y float32) { *p = append(*p, mathSeg{op: 1, p: [6]float32{x, y}}) }
func (p *mathPath) quad(cx, cy, x, y float32) {
	*p = append(*p, mathSeg{op: 2, p: [6]float32{cx, cy, x, y}})
}
func (p *mathPath) cube(c1x, c1y, c2x, c2y, x, y float32) {
	*p = append(*p, mathSeg{op: 3, p: [6]float32{c1x, c1y, c2x, c2y, x, y}})
}
func (p *mathPath) close() { *p = append(*p, mathSeg{op: 4}) }

// circle adds a circle, of four cubic curves.
func (p *mathPath) circle(cx, cy, r float32) {
	k := 0.5523 * r
	p.move(cx+r, cy)
	p.cube(cx+r, cy+k, cx+k, cy+r, cx, cy+r)
	p.cube(cx-k, cy+r, cx-r, cy+k, cx-r, cy)
	p.cube(cx-r, cy-k, cx-k, cy-r, cx, cy-r)
	p.cube(cx+k, cy-r, cx+r, cy-k, cx+r, cy)
	p.close()
}

// mirror flips the path across the middle of a box w wide.
func (p mathPath) mirror(w float32) mathPath {
	for i := range p {
		for k := 0; k < 6; k += 2 {
			p[i].p[k] = w - p[i].p[k]
		}
	}
	return p
}

// add paints c in b, with its origin at x, y from b's.
func (b *mathBox) add(c *mathBox, x, y float32) {
	for _, o := range c.ops {
		o.x += x
		o.y += y
		b.ops = append(b.ops, o)
	}
}

func (b *mathBox) rule(x, y, w, h float32) {
	b.ops = append(b.ops, mathOp{kind: opRule, rect: ui.Rect{X: x, Y: y, W: w, H: h}})
}

func (b *mathBox) path(p mathPath, stroke float32) {
	b.ops = append(b.ops, mathOp{kind: opPath, segs: p, stroke: stroke})
}

// draw paints the box with its origin at x, y, in color c.
func (b *mathBox) draw(p *ui.Painter, x, y float32, c ui.Color) {
	for i := range b.ops {
		o := &b.ops[i]
		ox, oy := x+o.x, y+o.y
		switch o.kind {
		case opGlyphs:
			p.Glyphs(o.glyphs, ox, oy, c)
		case opRule:
			p.Fill(ui.Rect{X: ox + o.rect.X, Y: oy + o.rect.Y, W: o.rect.W, H: o.rect.H}, c, 0)
		case opPath:
			path := b.scratch.Reset()
			for _, s := range o.segs {
				q := s.p
				switch s.op {
				case 0:
					path.MoveTo(ox+q[0], oy+q[1])
				case 1:
					path.LineTo(ox+q[0], oy+q[1])
				case 2:
					path.QuadTo(ox+q[0], oy+q[1], ox+q[2], oy+q[3])
				case 3:
					path.CubeTo(ox+q[0], oy+q[1], ox+q[2], oy+q[3], ox+q[4], oy+q[5])
				case 4:
					path.Close()
				}
			}
			if o.stroke > 0 {
				p.StrokePath(path, o.stroke, c)
			} else {
				p.FillPath(path, c)
			}
		}
	}
}

// mathLayout lays out formulas whose text style is px DIPs.
type mathLayout struct {
	px float32
}

func (m *mathLayout) em(s mathStyle) float32 { return m.px * mathScales[s.level] }

// rule is the thickness of rules, fraction bars and radicals' bars.
func (m *mathLayout) rule(em float32) float32 { return max(0.05*em, 0.8) }

// mathSpacing is TeX's space between atoms of two classes, in thin (1),
// medium (2) and thick (3) spaces; a negative one is left out in script
// styles.
var mathSpacing = [8][8]int8{
	//   ord op bin rel open close punct inner
	{0, 1, -2, -3, 0, 0, 0, -1},     // ord
	{1, 1, 0, -3, 0, 0, 0, -1},      // op
	{-2, -2, 0, 0, -2, 0, 0, -2},    // bin
	{-3, -3, 0, 0, -3, 0, 0, -3},    // rel
	{0, 0, 0, 0, 0, 0, 0, 0},        // open
	{0, 1, -2, -3, 0, 0, 0, -1},     // close
	{-1, -1, 0, -1, -1, -1, -1, -1}, // punct
	{-1, 1, -2, -3, -1, 0, -1, -1},  // inner
}

// list lays out a list of nodes side by side, spaced by their classes.
func (m *mathLayout) list(nodes []*mathNode, s mathStyle) *mathBox {
	type item struct {
		box   *mathBox
		class mathClass
		style mathStyle
	}
	items := make([]item, 0, len(nodes))
	for _, n := range nodes {
		switch n.kind {
		case kStyle:
			s.level = n.style
			continue
		case kSpace:
			items = append(items, item{&mathBox{w: n.em * m.em(s)}, mNone, s})
			continue
		}
		b, c := m.node(n, s)
		items = append(items, item{b, c, s})
	}
	// A binary operator with nothing to operate on is an ordinary atom:
	// at the start, after an operator, a relation, an opening or
	// punctuation, or before a relation, a closing or punctuation.
	prev := -1
	for i := range items {
		c := items[i].class
		if c == mNone {
			continue
		}
		if c == mBin && (prev < 0 || items[prev].class == mBin || items[prev].class == mOp || items[prev].class == mRel || items[prev].class == mOpen || items[prev].class == mPunct) {
			items[i].class = mOrd
		}
		if (c == mRel || c == mClose || c == mPunct) && prev >= 0 && items[prev].class == mBin {
			items[prev].class = mOrd
		}
		prev = i
	}
	if prev >= 0 && items[prev].class == mBin {
		items[prev].class = mOrd
	}
	out := &mathBox{}
	var x float32
	prev = -1
	for i, it := range items {
		if it.class != mNone {
			if prev >= 0 {
				if sp := mathSpacing[items[prev].class][it.class]; sp > 0 || sp < 0 && it.style.level < 2 {
					x += [4]float32{0, 3, 4, 5}[max(sp, -sp)] / 18 * m.em(it.style)
				}
			}
			prev = i
		}
		out.add(it.box, x, 0)
		x += it.box.w
		out.h = max(out.h, it.box.h)
		out.d = max(out.d, it.box.d)
		out.ic = it.box.ic
	}
	out.w = x
	return out
}

// node lays out a node, and returns its class.
func (m *mathLayout) node(n *mathNode, s mathStyle) (*mathBox, mathClass) {
	em := m.em(s)
	switch n.kind {
	case kAtom:
		if n.big {
			return m.bigOp(n, s), n.class
		}
		text, f := mathFace(n.text, n.font, em)
		return m.run(text, f), n.class
	case kText:
		f := ui.Font{Family: mathTextFamily, Size: em}
		switch n.font {
		case fontBf:
			f.Weight = 700
		case fontIt:
			f.Italic = true
		case fontSf:
			f.Family = mathSansFamily
		case fontTt:
			f.Family = mathMonoFamily
		}
		return m.run(n.text, f), mOrd
	case kGroup:
		return m.list(n.a, s), n.class
	case kScripts:
		return m.scripts(n, s)
	case kFrac:
		return m.frac(n, s), mInner
	case kSqrt:
		return m.sqrt(n, s), mOrd
	case kLeftRight:
		inner := m.list(n.a, s)
		return m.fence(n.left, n.right, inner, mathFenceSize(inner, em), s), mInner
	case kBigDelim:
		return m.delimiter(n.text, n.em*em, s), n.class
	case kAccent:
		return m.accent(n, s), mOrd
	case kOverline, kUnderline:
		body := m.list(n.a, mathStyle{s.level, s.cramped || n.kind == kOverline})
		t := m.rule(em)
		out := &mathBox{w: body.w, h: body.h, d: body.d}
		out.add(body, 0, 0)
		if n.kind == kOverline {
			out.rule(0, -(body.h + 3*t), body.w, t)
			out.h = body.h + 5*t
		} else {
			out.rule(0, body.d+3*t-t, body.w, t)
			out.d = body.d + 4*t
		}
		return out, mOrd
	case kBrace:
		return m.brace(n, s), mOp
	case kBoxed:
		body := m.list(n.a, s)
		t, pad := m.rule(em), 0.25*em
		out := &mathBox{w: body.w + 2*pad + 2*t, h: body.h + pad + t, d: body.d + pad + t}
		out.add(body, pad+t, 0)
		out.rule(0, -out.h, out.w, t)
		out.rule(0, out.d-t, out.w, t)
		out.rule(0, -out.h, t, out.h+out.d)
		out.rule(out.w-t, -out.h, t, out.h+out.d)
		return out, mOrd
	case kPhantom:
		body := m.list(n.a, s)
		return &mathBox{w: body.w, h: body.h, d: body.d}, mOrd
	case kArray:
		return m.array(n, s), mOrd
	}
	return &mathBox{}, mOrd
}

// mathFace is the text of an atom in an alphabet, and its font: letters
// in italic by default, the double-struck, script and fraktur alphabets
// as Unicode has them.
func mathFace(text string, font mathFont, em float32) (string, ui.Font) {
	f := ui.Font{Family: mathSymbolFamily, Size: em}
	letters := true
	for _, r := range text {
		if !(r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r >= 0x370 && r <= 0x3FF || r == ' ') {
			letters = false
		}
	}
	if letters {
		f.Family = mathTextFamily
	}
	switch font {
	case fontDefault:
		f.Italic = mathItalic(text)
	case fontIt:
		f.Italic = letters
	case fontBf:
		f.Weight = 700
	case fontBfIt:
		f.Weight, f.Italic = 700, mathItalic(text)
	case fontSf:
		f.Family = mathSansFamily
	case fontTt:
		f.Family = mathMonoFamily
	case fontBb, fontCal, fontFrak:
		f.Family = mathSymbolFamily
		var b strings.Builder
		for _, r := range text {
			b.WriteRune(mathAlphabet(r, font))
		}
		text = b.String()
	}
	return text, f
}

// mathItalic reports whether text is set in italic by default: Latin
// letters and lowercase Greek ones.
func mathItalic(text string) bool {
	for _, r := range text {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 'α' && r <= 'ω' || strings.ContainsRune("ϵϑϕϖϱϰϝ", r)) {
			return false
		}
	}
	return text != ""
}

// mathAlphabet is a letter in the double-struck, script or fraktur
// alphabet, where Unicode has it: some in the Letterlike Symbols, the
// rest in the Mathematical Alphanumeric Symbols.
func mathAlphabet(r rune, font mathFont) rune {
	var upper, lower rune
	var special map[rune]rune
	switch font {
	case fontBb:
		upper, lower = 0x1D538, 0x1D552
		special = map[rune]rune{'C': 'ℂ', 'H': 'ℍ', 'N': 'ℕ', 'P': 'ℙ', 'Q': 'ℚ', 'R': 'ℝ', 'Z': 'ℤ'}
		if r >= '0' && r <= '9' {
			return 0x1D7D8 + r - '0'
		}
	case fontCal:
		upper, lower = 0x1D49C, 0x1D4B6
		special = map[rune]rune{'B': 'ℬ', 'E': 'ℰ', 'F': 'ℱ', 'H': 'ℋ', 'I': 'ℐ', 'L': 'ℒ', 'M': 'ℳ', 'R': 'ℛ', 'e': 'ℯ', 'g': 'ℊ', 'o': 'ℴ'}
	case fontFrak:
		upper, lower = 0x1D504, 0x1D51E
		special = map[rune]rune{'C': 'ℭ', 'H': 'ℌ', 'I': 'ℑ', 'R': 'ℜ', 'Z': 'ℨ'}
	default:
		return r
	}
	if s, ok := special[r]; ok {
		return s
	}
	switch {
	case r >= 'A' && r <= 'Z':
		return upper + r - 'A'
	case r >= 'a' && r <= 'z':
		return lower + r - 'a'
	}
	return r
}

// mathHD is how far a character's ink reaches above its baseline and
// below it, in ems, as TeX's fonts have it: the system's fonts tell only
// their line's.
func mathHD(r rune) (h, d float32) {
	switch {
	case r == ' ':
		return 0, 0
	case strings.ContainsRune("acemnorsuvwxzıαεικνοπστυωϖϵ", r):
		return 0.45, 0
	case strings.ContainsRune("gpqyγημρχςϱ", r):
		return 0.45, 0.21
	case strings.ContainsRune("fjβζξφψϕ", r):
		return 0.7, 0.21
	case r == 't':
		return 0.62, 0
	case r >= 'a' && r <= 'z', strings.ContainsRune("δθλϑ∂ℓ", r):
		return 0.7, 0
	case r == 'Q':
		return 0.69, 0.1
	case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r >= 'Α' && r <= 'Ω':
		return 0.69, 0
	case strings.ContainsRune("()[]{}|‖⟨⟩⌊⌋⌈⌉/\\", r):
		return 0.75, 0.25
	case strings.ContainsRune(",;", r):
		return 0.12, 0.2
	case strings.ContainsRune(".…", r):
		return 0.12, 0
	case strings.ContainsRune("⋯⋅∙∘·", r):
		return 0.3, 0
	case r == '′':
		return 0.75, 0
	case strings.ContainsRune("+−±∓×÷∗⋆=<>≤≥≠≈≡∼≃≅∝:", r), r >= 0x2190 && r <= 0x21FF, r >= 0x27F0 && r <= 0x27FF:
		return 0.58, 0.08
	case r >= 0x1D400 && r <= 0x1D7FF, r >= 0x2100 && r <= 0x214F:
		return 0.69, 0
	}
	return 0.7, 0.05
}

// run is text shaped in a font: as wide as its glyphs' advances, as tall
// and deep as its characters.
func (m *mathLayout) run(text string, f ui.Font) *mathBox {
	b := &mathBox{}
	if text == "" {
		return b
	}
	g := ui.Shape(text, f)
	for _, gl := range g {
		b.w = max(b.w, gl.X+gl.Advance)
	}
	for _, r := range text {
		h, d := mathHD(r)
		b.h, b.d = max(b.h, h*f.Size), max(b.d, d*f.Size)
	}
	if f.Italic {
		if r, _ := utf8.DecodeLastRuneInString(text); unicode.IsLetter(r) {
			b.ic = 0.06 * f.Size
		}
	}
	b.ops = append(b.ops, mathOp{kind: opGlyphs, glyphs: g})
	return b
}

// bigOp is a big operator, larger in display style, centered on the
// axis, where minus signs and fraction bars are.
func (m *mathLayout) bigOp(n *mathNode, s mathStyle) *mathBox {
	em := m.em(s)
	integral := strings.ContainsAny(n.text, "∫∬∭∮∯")
	scale := float32(1)
	if s.level == 0 {
		scale = 1.4
		if integral {
			scale = 1.8
		}
	}
	gs := em * scale
	f := ui.Font{Family: mathSymbolFamily, Size: gs}
	if n.font == fontBf || n.font == fontBfIt {
		f.Weight = 700
	}
	b := m.run(n.text, f)
	// The glyph is centered on its own axis, which its size moves up.
	axis, half := 0.25*em, 0.45*gs
	if integral {
		half = 0.55 * gs
		b.ic, b.subKern = 0.1*gs, -0.12*gs
	} else {
		b.ic = 0
	}
	b.ops[0].y = 0.25 * (gs - em)
	b.h, b.d = axis+half, half-axis
	return b
}

// stacked reports whether a scripts node sets its scripts above and below.
func (n *mathNode) stacked(s mathStyle) bool {
	if n.limits == 2 || n.base.kind == kBrace {
		return true
	}
	b := n.base
	return b.class == mOp && (b.limits == 2 || b.limits == 1 && s.level == 0)
}

// scripts sets a superscript and subscript to their base, as TeX's rule
// 18 does, or, on an operator with limits, above and below it.
func (m *mathLayout) scripts(n *mathNode, s mathStyle) (*mathBox, mathClass) {
	base, class := m.node(n.base, s)
	em := m.em(s)
	var sup, sub *mathBox
	if n.hasSup {
		sup = m.list(n.sup, s.sup())
	}
	if n.hasSub {
		sub = m.list(n.sub, s.sub())
	}
	out := &mathBox{h: base.h, d: base.d}
	if n.stacked(s) {
		w := base.w
		if sup != nil {
			w = max(w, sup.w)
		}
		if sub != nil {
			w = max(w, sub.w)
		}
		out.add(base, (w-base.w)/2, 0)
		if sup != nil {
			at := base.h + max(0.111*em+sup.d, 0.2*em)
			out.add(sup, (w-sup.w)/2+base.ic/2, -at)
			out.h = at + sup.h + 0.1*em
		}
		if sub != nil {
			at := base.d + max(0.167*em+sub.h, 0.6*em)
			out.add(sub, (w-sub.w)/2-base.ic/2, at)
			out.d = at + sub.d + 0.1*em
		}
		out.w = w
		return out, class
	}
	emSup := m.em(s.sup())
	var u, v float32
	if n.base.kind != kAtom || n.base.big {
		u, v = base.h-0.386*emSup, base.d+0.05*emSup
	}
	xh := 0.45 * em
	if sup != nil {
		p := float32(0.363)
		switch {
		case s.cramped:
			p = 0.289
		case s.level == 0:
			p = 0.413
		}
		u = max(u, p*em, sup.d+0.25*xh)
	}
	if sub != nil && sup == nil {
		v = max(v, 0.15*em, sub.h-0.8*xh)
	}
	if sub != nil && sup != nil {
		v = max(v, 0.247*em)
		t := m.rule(em)
		if gap := (u - sup.d) - (sub.h - v); gap < 4*t {
			v += 4*t - gap
			if psi := 0.8*xh - (u - sup.d); psi > 0 {
				u += psi
				v -= psi
			}
		}
	}
	out.add(base, 0, 0)
	w := base.w
	if sup != nil {
		out.add(sup, base.w+base.ic, -u)
		out.h = max(out.h, u+sup.h)
		w = max(w, base.w+base.ic+sup.w)
	}
	if sub != nil {
		out.add(sub, base.w+base.subKern, v)
		out.d = max(out.d, v+sub.d)
		w = max(w, base.w+base.subKern+sub.w)
	}
	out.w = w + 0.05*em
	return out, class
}

// frac sets a numerator over a denominator, as TeX's rule 15 does: over
// a rule on the axis, or, for a binomial, without one, in parentheses.
func (m *mathLayout) frac(n *mathNode, s mathStyle) *mathBox {
	if n.style >= 0 {
		s.level = n.style
	}
	em := m.em(s)
	num, den := m.list(n.a, s.num()), m.list(n.b, s.den())
	t, axis := m.rule(em), 0.25*em
	var u, v, phi float32
	if s.level == 0 {
		u, v, phi = 0.677*em, 0.686*em, 3*t
	} else {
		u, v, phi = 0.394*em, 0.345*em, t
	}
	if n.rule {
		if c := (u - num.d) - (axis + t/2); c < phi {
			u += phi - c
		}
		if c := (axis - t/2) - (den.h - v); c < phi {
			v += phi - c
		}
	} else {
		phi = 3 * t
		if s.level == 0 {
			phi = 7 * t
		}
		if c := (u - num.d) - (den.h - v); c < phi {
			u += (phi - c) / 2
			v += (phi - c) / 2
		}
	}
	pad := 0.12 * em
	mw := max(num.w, den.w)
	out := &mathBox{w: mw + 2*pad, h: u + num.h, d: v + den.d}
	out.add(num, pad+(mw-num.w)/2, -u)
	out.add(den, pad+(mw-den.w)/2, v)
	if n.rule {
		out.rule(pad, -(axis + t/2), mw, t)
		return out
	}
	size := mathFenceSize(out, em)
	if s.level == 0 {
		size = max(size, 2.39*em)
	} else {
		size = max(size, 1.01*em)
	}
	return m.fence("(", ")", out, size, s)
}

// mathFenceSize is the size of the delimiters around a box: as TeX makes
// them, covering at least 90% of it, and to within 0.5em of covering it,
// both ways from the axis.
func mathFenceSize(b *mathBox, em float32) float32 {
	axis := 0.25 * em
	delta := max(b.h-axis, b.d+axis)
	return max(2*delta*0.901, 2*delta-0.5*em)
}

// fence sets a box between delimiters size tall.
func (m *mathLayout) fence(left, right string, inner *mathBox, size float32, s mathStyle) *mathBox {
	l, r := m.delimiter(left, size, s), m.delimiter(right, size, s)
	out := &mathBox{w: l.w + inner.w + r.w, h: max(l.h, inner.h, r.h), d: max(l.d, inner.d, r.d)}
	out.add(l, 0, 0)
	out.add(inner, l.w, 0)
	out.add(r, l.w+inner.w, 0)
	return out
}

// delimiter is a delimiter size tall, centered on the axis: the font's
// glyph where that is about its size, a drawn one where taller; nothing,
// a little space wide, for a null one (\left.).
func (m *mathLayout) delimiter(d string, size float32, s mathStyle) *mathBox {
	em := m.em(s)
	if d == "" {
		return &mathBox{w: 0.12 * em}
	}
	if size <= 1.2*em {
		_, f := mathFace(d, fontRm, em)
		f.Family = mathSymbolFamily
		return m.run(d, f)
	}
	axis := 0.25 * em
	top, bot, mid := -axis-size/2, -axis+size/2, -axis
	t := min(0.06*em+0.012*size, 0.12*em)
	b := &mathBox{h: axis + size/2, d: size/2 - axis}
	var p mathPath
	switch d {
	case "(", ")":
		b.w = min(0.33*em+0.07*size, 0.75*em)
		xl, xr, e := 0.08*em, b.w-0.06*em, 0.4*t
		p.move(xr, top)
		p.quad(2*xl-xr, mid, xr, bot)
		p.line(xr-e, bot)
		p.quad(2*(xl+1.5*t)-(xr-e), mid, xr-e, top)
		p.close()
		if d == ")" {
			p.mirror(b.w)
		}
		b.path(p, 0)
	case "[", "]", "⌊", "⌋", "⌈", "⌉":
		b.w = min(0.38*em+0.02*size, 0.55*em)
		x0, x1, stem := 0.12*em, b.w-0.06*em, 1.2*t
		if d == "]" || d == "⌋" || d == "⌉" {
			x0, x1 = b.w-x1, b.w-x0
			b.rule(x1-stem, top, stem, size)
		} else {
			b.rule(x0, top, stem, size)
		}
		if d == "[" || d == "]" || d == "⌈" || d == "⌉" {
			b.rule(x0, top, x1-x0, t)
		}
		if d == "[" || d == "]" || d == "⌊" || d == "⌋" {
			b.rule(x0, bot-t, x1-x0, t)
		}
	case "|":
		b.w = 0.3 * em
		b.rule(b.w/2-0.6*t, top, 1.2*t, size)
	case "‖":
		b.w = 0.5 * em
		b.rule(b.w/2-0.12*em-0.6*t, top, 1.2*t, size)
		b.rule(b.w/2+0.12*em-0.6*t, top, 1.2*t, size)
	case "{", "}":
		b.w = 0.55 * em
		xl, xr := 0.08*em, b.w-0.08*em
		xm, r := (xl+xr)/2, min(0.22*em+0.03*size, size/4)
		p.move(xr, top)
		p.quad(xm, top, xm, top+r)
		p.line(xm, mid-r)
		p.quad(xm, mid, xl, mid)
		p.quad(xm, mid, xm, mid+r)
		p.line(xm, bot-r)
		p.quad(xm, bot, xr, bot)
		if d == "}" {
			p.mirror(b.w)
		}
		b.path(p, 1.3*t)
	case "⟨", "⟩":
		b.w = min(0.35*em+0.08*size, 0.7*em)
		xl, xr := 0.1*em, b.w-0.08*em
		p.move(xr, top)
		p.line(xl, mid)
		p.line(xr, bot)
		if d == "⟩" {
			p.mirror(b.w)
		}
		b.path(p, 1.2*t)
	case "/", "\\":
		b.w = min(0.3*em+0.25*size, 1.2*em)
		p.move(b.w-0.08*em, top)
		p.line(0.08*em, bot)
		if d == "\\" {
			p.mirror(b.w)
		}
		b.path(p, 1.2*t)
	default:
		_, f := mathFace(d, fontRm, em)
		f.Family = mathSymbolFamily
		return m.run(d, f)
	}
	return b
}

// sqrt sets a radicand under a radical, as TeX's rule 11 does, with its
// index over the radical's hook.
func (m *mathLayout) sqrt(n *mathNode, s mathStyle) *mathBox {
	em := m.em(s)
	body := m.list(n.a, mathStyle{s.level, true})
	t := m.rule(em)
	phi := t
	if s.level == 0 {
		phi = 0.45 * em
	}
	psi := t + phi/4
	top := -(body.h + psi + t)
	bot := max(body.d, 0.05*em)
	if bot-top < 0.9*em {
		bot = top + 0.9*em
	}
	total := bot - top
	k := min(1, total/em)
	ws := 0.5*em + min(0.1*(total-em), 0.3*em)
	if ws < 0.5*em {
		ws = 0.5 * em
	}
	out := &mathBox{h: -top + t, d: bot}
	var dx float32
	var index *mathBox
	if len(n.b) > 0 {
		index = m.list(n.b, mathStyle{3, false})
		// Over the hook, its right edge at 60% of the radical's width,
		// its baseline 60% of the way up.
		dx = max(0, index.w-0.6*ws)
	}
	var tick, stroke mathPath
	tick.move(dx, bot-0.42*em*k)
	tick.line(dx+0.12*em, bot-0.5*em*k)
	tick.line(dx+0.3*em, bot)
	stroke.move(dx+0.3*em, bot)
	stroke.line(dx+ws, top+t/2)
	stroke.line(dx+ws+body.w+0.08*em, top+t/2)
	out.path(tick, 1.6*t)
	out.path(stroke, 1.1*t)
	out.add(body, dx+ws+0.04*em, 0)
	out.w = dx + ws + body.w + 0.12*em
	if index != nil {
		at := bot - 0.6*total
		out.add(index, dx+0.6*ws-index.w, at-index.d)
		out.h = max(out.h, -(at - index.d - index.h))
	}
	return out
}

// accent sets an accent over a box: as wide as it for the wide ones, a
// letter's width over its middle (to the right, over italics) for the
// rest.
func (m *mathLayout) accent(n *mathNode, s mathStyle) *mathBox {
	em := m.em(s)
	body := m.list(n.a, mathStyle{s.level, true})
	t := m.rule(em)
	wide := mathAccents[n.text]
	cx := body.w/2 + body.ic/2
	aw := min(0.45*em, max(body.w, 0.3*em))
	if wide {
		aw = max(body.w, 0.45*em)
		cx = body.w / 2
	}
	x0, x1 := cx-aw/2, cx+aw/2
	base := -(max(body.h, 0.45*em) + 0.08*em)
	ah := 0.2 * em
	out := &mathBox{w: body.w, h: body.h, d: body.d, ic: body.ic}
	out.add(body, 0, 0)
	var p mathPath
	stroke := 1.1 * t
	switch n.text {
	case "hat", "widehat":
		if wide {
			ah = 0.25 * em
		}
		p.move(x0, base)
		p.line(cx, base-ah)
		p.line(x1, base)
	case "check", "widecheck":
		p.move(x0, base-ah)
		p.line(cx, base)
		p.line(x1, base-ah)
	case "tilde", "widetilde":
		ah = 0.14 * em
		w := x1 - x0
		p.move(x0, base-0.2*ah)
		p.cube(x0+w/3, base-1.4*ah, x0+2*w/3, base+0.4*ah, x1, base-ah)
	case "bar":
		ah = t
		out.rule(x0, base-t, aw, t)
	case "vec", "overrightarrow", "overleftarrow", "overleftrightarrow":
		ah = 0.2 * em
		y, hd := base-ah/2, 0.1*em
		p.move(x0, y)
		p.line(x1, y)
		if n.text != "overleftarrow" {
			p.move(x1-hd*1.2, y-hd)
			p.line(x1, y)
			p.line(x1-hd*1.2, y+hd)
		}
		if n.text == "overleftarrow" || n.text == "overleftrightarrow" {
			p.move(x0+hd*1.2, y-hd)
			p.line(x0, y)
			p.line(x0+hd*1.2, y+hd)
		}
	case "dot", "ddot":
		ah = 0.12 * em
		r := 0.055 * em
		if n.text == "dot" {
			p.circle(cx, base-r, r)
		} else {
			p.circle(cx-0.11*em, base-r, r)
			p.circle(cx+0.11*em, base-r, r)
		}
		stroke = 0
	case "mathring":
		ah = 0.18 * em
		p.circle(cx, base-ah/2, ah/2-t/2)
	case "acute":
		p.move(cx-0.06*em, base)
		p.line(cx+0.1*em, base-ah)
	case "grave":
		p.move(cx+0.06*em, base)
		p.line(cx-0.1*em, base-ah)
	case "breve":
		ah = 0.14 * em
		p.move(x0+0.05*em, base-ah)
		p.quad(cx, base+ah*0.6, x1-0.05*em, base-ah)
	}
	if len(p) > 0 {
		out.path(p, stroke)
	}
	out.h = max(body.h, -base+ah)
	return out
}

// brace sets a horizontal brace over (or under) a box, its scripts then
// above (or below) it.
func (m *mathLayout) brace(n *mathNode, s mathStyle) *mathBox {
	em := m.em(s)
	body := m.list(n.a, s)
	t := m.rule(em)
	bh := 0.3 * em
	x0, x1 := 0.04*em, max(body.w-0.04*em, 0.3*em)
	cx := (x0 + x1) / 2
	r := min(0.25*em, (x1-x0)/4)
	yb, sign := -(body.h + 0.1*em), float32(-1)
	if n.text == "under" {
		yb, sign = body.d+0.1*em, 1
	}
	ym, yt := yb+sign*bh/2, yb+sign*bh
	var p mathPath
	p.move(x0, yb)
	p.quad(x0, ym, x0+r, ym)
	p.line(cx-r, ym)
	p.quad(cx, ym, cx, yt)
	p.quad(cx, ym, cx+r, ym)
	p.line(x1-r, ym)
	p.quad(x1, ym, x1, yb)
	out := &mathBox{w: max(body.w, x1+0.04*em), h: body.h, d: body.d}
	out.add(body, 0, 0)
	out.path(p, 1.3*t)
	if n.text == "under" {
		out.d = body.d + 0.1*em + bh
	} else {
		out.h = body.h + 0.1*em + bh
	}
	return out
}

// array lays out an environment's cells in a grid centered on the axis,
// each column as wide as its widest cell, each row as tall as its
// tallest, with the environment's delimiters around.
func (m *mathLayout) array(n *mathNode, s mathStyle) *mathBox {
	env := mathEnvs[n.text]
	cs := s
	if env.style >= 0 {
		cs = mathStyle{env.style, false}
		if s.level >= 2 {
			cs.level = max(env.style, s.level)
		}
	}
	em := m.em(cs)
	ncols := 0
	for _, row := range n.rows {
		ncols = max(ncols, len(row))
	}
	pairs := env.align == 0
	align := func(j int) byte {
		switch {
		case j < len(n.cols):
			return n.cols[j]
		case pairs && j%2 == 0:
			return 'r'
		case pairs:
			return 'l'
		}
		return env.align
	}
	cells := make([][]*mathBox, len(n.rows))
	colW := make([]float32, ncols)
	rowH := make([]float32, len(n.rows))
	rowD := make([]float32, len(n.rows))
	for i, row := range n.rows {
		rowH[i], rowD[i] = 0.84*em*env.stretch, 0.36*em*env.stretch
		cells[i] = make([]*mathBox, ncols)
		for j := range ncols {
			var nodes []*mathNode
			if j < len(row) {
				nodes = row[j]
			}
			// The second of a pair starts after an empty atom, so that its
			// relation is spaced as it is after the first.
			if pairs && j%2 == 1 && len(nodes) > 0 {
				nodes = append([]*mathNode{{kind: kGroup}}, nodes...)
			}
			b := m.list(nodes, cs)
			cells[i][j] = b
			colW[j] = max(colW[j], b.w)
			rowH[i], rowD[i] = max(rowH[i], b.h), max(rowD[i], b.d)
		}
	}
	colX := make([]float32, ncols)
	x := env.pad * em
	for j := range ncols {
		if j > 0 && (!pairs || j%2 == 0) {
			x += env.colsep * em
		}
		colX[j] = x
		x += colW[j]
	}
	var total float32
	for i := range n.rows {
		total += rowH[i] + rowD[i]
	}
	if len(n.rows) > 1 {
		total += float32(len(n.rows)-1) * env.gap * em
	}
	axis := 0.25 * m.em(s)
	grid := &mathBox{w: x + env.pad*em, h: total/2 + axis, d: total/2 - axis}
	y := -grid.h
	for i := range n.rows {
		y += rowH[i]
		for j, b := range cells[i] {
			dx := colX[j]
			switch align(j) {
			case 'c':
				dx += (colW[j] - b.w) / 2
			case 'r':
				dx += colW[j] - b.w
			}
			grid.add(b, dx, y)
		}
		y += rowD[i] + env.gap*em
	}
	if env.left == "" && env.right == "" {
		return grid
	}
	return m.fence(env.left, env.right, grid, mathFenceSize(grid, m.em(s))+0.2*m.em(s), s)
}
