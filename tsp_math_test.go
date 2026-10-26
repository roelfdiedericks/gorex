package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestMathErrors reports what does not typeset, and why.
func TestMathErrors(t *testing.T) {
	for _, c := range []struct{ tex, want string }{
		{`\frac{1}`, "missing its argument"},
		{`{x`, "not closed"},
		{`x}`, "closes nothing"},
		{`\foo x`, `unknown command \foo`},
		{`x^1^2`, "double superscript"},
		{`x_1_2`, "double subscript"},
		{`x'^2'`, "double superscript"},
		{`\left( x`, `missing its \right`},
		{`x \right)`, `\right without its \left`},
		{`\left\uparrow x \right)`, "takes a delimiter"},
		{`a & b`, "& outside an environment"},
		{`\begin{foo} x \end{foo}`, "unknown environment {foo}"},
		{`\begin{matrix} a`, `missing its \end{matrix}`},
		{`\begin{matrix} a \end{pmatrix}`, `ended by \end{pmatrix}`},
		{`x \end{matrix}`, `\end without its \begin`},
		{`\sqrt[3 x`, "missing its ]"},
		{`x^`, "missing its argument"},
		{`x\limits_2`, "follows only an operator"},
		{`x \`, `a lone \`},
	} {
		_, err := parseMath(c.tex)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one saying %q", c.tex, err, c.want)
		}
	}
	for _, tex := range []string{
		`e^{i\pi} + 1 = 0`,
		`\sum_{k=1}^{n} k = \frac{n(n+1)}{2}`,
		`f'(x) = \lim_{h \to 0} \frac{f(x+h) - f(x)}{h}`,
		`\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}`,
		`\left\{ x \in \mathbb{R} \mid x \geq 0 \right\}`,
		`\begin{pmatrix} a & b \\ c & d \end{pmatrix}`,
		`|x| = \begin{cases} x & x \ge 0 \\ -x & \text{otherwise} \end{cases}`,
		`\begin{aligned} a &= b + c \\ &= d \end{aligned}`,
		`\hat{x} + \vec{v} \cdot \overline{AB} + \sqrt[3]{8} + \operatorname{arg\,max}_x f`,
		`\overbrace{1 + \cdots + n}^{n} \not= \mathcal{O}(n^2)`,
		`a \\ b`,
	} {
		if _, err := parseMath(tex); err != nil {
			t.Errorf("%s: %v", tex, err)
		}
	}
}

// TestMathSymbols looks commands and characters up.
func TestMathSymbols(t *testing.T) {
	for name, want := range map[string]mathSym{
		"alpha":      {text: "α", class: mOrd},
		"Omega":      {text: "Ω", class: mOrd},
		"leq":        {text: "≤", class: mRel},
		"times":      {text: "×", class: mBin},
		"Rightarrow": {text: "⇒", class: mRel},
		"langle":     {text: "⟨", class: mOpen},
		"sum":        {text: "∑", class: mOp, big: true, limits: 1},
		"int":        {text: "∫", class: mOp, big: true, limits: -1},
	} {
		if got := mathSymbols[name]; got != want {
			t.Errorf(`\%s is %+v, want %+v`, name, got, want)
		}
	}
	if mathRuneClasses['≤'] != mRel || mathRuneClasses['∑'] != mOp || mathRuneClasses['⊲'] != mRel {
		t.Errorf("≤, ∑ and ⊲ typed as they are: classes %v, %v, %v", mathRuneClasses['≤'], mathRuneClasses['∑'], mathRuneClasses['⊲'])
	}
	for _, r := range "|.‖" {
		if n, _ := mathChar(r, fontDefault); n.class != mOrd {
			t.Errorf("%c typed is of class %v, not ordinary", r, n.class)
		}
	}
	if got := mathAlphabet('R', fontBb); got != 'ℝ' {
		t.Errorf("double-struck R is %c", got)
	}
	if got := mathAlphabet('A', fontBb); got != 0x1D538 {
		t.Errorf("double-struck A is %U", got)
	}
	if got := mathAlphabet('L', fontCal); got != 'ℒ' {
		t.Errorf("script L is %c", got)
	}
	if !mathItalic("x") || mathItalic("2") || mathItalic("Γ") || !mathItalic("α") {
		t.Error("italics: letters and lowercase Greek only")
	}
	nodes, _ := parseMath(`\sin x`)
	if len(nodes) != 2 || nodes[0].class != mOp || nodes[0].font != fontRm || nodes[0].text != "sin" {
		t.Errorf(`\sin is %+v`, nodes[0])
	}
}

// mathOf lays tex out, failing the test where it does not.
func mathOf(t *testing.T, tex string, display bool) *mathBox {
	t.Helper()
	s := typesetMath(tex, 20, display, ui.Color{})
	if s.err != nil {
		t.Fatalf("%s: %v", tex, s.err)
	}
	return s.box
}

// TestMathLayout checks what scripts, fractions, radicals, limits and
// fences do to a formula's box.
func TestMathLayout(t *testing.T) {
	x := mathOf(t, `x`, false)
	if x.w <= 0 || x.h <= 0 {
		t.Fatalf("x is %+v", x)
	}
	// A superscript raises above the base, and widens it.
	sup := mathOf(t, `x^2`, false)
	if sup.h <= x.h || sup.w <= x.w {
		t.Errorf("x^2 is %vx%v+%v, x %vx%v+%v", sup.w, sup.h, sup.d, x.w, x.h, x.d)
	}
	var raised bool
	for _, o := range sup.ops[1:] {
		raised = raised || o.y < 0
	}
	if !raised {
		t.Error("x^2's 2 is not above the baseline")
	}
	// A subscript drops below it.
	if sub := mathOf(t, `x_i`, false); sub.d <= x.d {
		t.Errorf("x_i's depth %v, x's %v", sub.d, x.d)
	}
	// A fraction is taller and deeper than its numerator alone, with
	// its bar on the axis.
	a := mathOf(t, `a`, false)
	frac := mathOf(t, `\frac{a}{b}`, false)
	if frac.h+frac.d <= a.h+a.d || frac.h <= a.h || frac.d <= a.d {
		t.Errorf("\\frac{a}{b} is %v+%v, a %v+%v", frac.h, frac.d, a.h, a.d)
	}
	var bar bool
	for _, o := range frac.ops {
		if o.kind == opRule && o.rect.Y < 0 && o.rect.Y+o.rect.H > -0.5*20 {
			bar = true
		}
	}
	if !bar {
		t.Error("\\frac{a}{b} has no bar about the axis")
	}
	// Display style sets it larger.
	if dfrac := mathOf(t, `\frac{a}{b}`, true); dfrac.h <= frac.h || dfrac.d <= frac.d {
		t.Errorf("display fraction %v+%v, text %v+%v", dfrac.h, dfrac.d, frac.h, frac.d)
	}
	// A radical is wider and taller than its radicand.
	if r := mathOf(t, `\sqrt{x}`, false); r.w <= x.w || r.h <= x.h {
		t.Errorf("\\sqrt{x} is %vx%v, x %vx%v", r.w, r.h, x.w, x.h)
	}
	if r, r3 := mathOf(t, `\sqrt{x}`, false), mathOf(t, `\sqrt[3]{x}`, false); r3.h < r.h {
		t.Errorf("\\sqrt[3]{x} %v tall, \\sqrt{x} %v", r3.h, r.h)
	}
	// Display limits sit above and below the sum; inline ones beside.
	inline := mathOf(t, `\sum_{i=1}^{n} i`, false)
	display := mathOf(t, `\sum_{i=1}^{n} i`, true)
	if display.h <= inline.h || display.d <= inline.d {
		t.Errorf("display sum %v+%v, inline %v+%v", display.h, display.d, inline.h, inline.d)
	}
	sum := mathOf(t, `\sum`, true)
	if display.d <= sum.d+0.3*20 || display.h <= sum.h+0.3*20 {
		t.Errorf("display limits %v+%v around a sum of %v+%v", display.h, display.d, sum.h, sum.d)
	}
	// \left( \right) grow with what they hold.
	small := mathOf(t, `\left( x \right)`, true)
	big := mathOf(t, `\left( \frac{a}{b} \right)`, true)
	if big.h+big.d <= small.h+small.d {
		t.Errorf("fenced fraction %v+%v, fenced x %v+%v", big.h, big.d, small.h, small.d)
	}
	// A matrix is as tall as its rows.
	one := mathOf(t, `\begin{pmatrix} a \end{pmatrix}`, false)
	two := mathOf(t, `\begin{pmatrix} a \\ b \end{pmatrix}`, false)
	if two.h+two.d <= one.h+one.d {
		t.Errorf("two rows %v tall, one %v", two.h+two.d, one.h+one.d)
	}
	// Relations are spaced, and not in scripts.
	tight, spaced := mathOf(t, `ab`, false), mathOf(t, `a=b`, false)
	eq := mathOf(t, `=`, false)
	if spaced.w < tight.w+eq.w+0.4*20 {
		t.Errorf("a=b %v wide: a b %v and = %v, without spaces", spaced.w, tight.w, eq.w)
	}
	// A leading minus is a sign, not spaced as an operator.
	if neg, minus := mathOf(t, `-x`, false), mathOf(t, `-`, false); neg.w > minus.w+x.w+0.01 {
		t.Errorf("-x %v wide, - and x %v", neg.w, minus.w+x.w)
	}
}

// TestMathCache lays a formula out once, for each size and color.
func TestMathCache(t *testing.T) {
	a := typesetMath(`\frac{1}{2}`, 17, true, ui.Color{R: 1})
	b := typesetMath(`\frac{1}{2}`, 17, true, ui.Color{R: 1})
	if a != b || a.box == nil {
		t.Errorf("the same formula laid out twice: %p, %p", a, b)
	}
	if c := typesetMath(`\frac{1}{2}`, 18, true, ui.Color{R: 1}); c == a {
		t.Error("another size is the same layout")
	}
	bad := typesetMath(`\oops`, 17, false, ui.Color{})
	if bad.err == nil || typesetMath(`\oops`, 17, false, ui.Color{}) != bad {
		t.Error("a formula that does not typeset is not kept")
	}
	obj := a.inline
	if obj.Width != a.box.w || obj.Height != a.box.h || obj.Depth != a.box.d || obj.Draw == nil {
		t.Errorf("inline object %+v for box %vx%v+%v", obj, a.box.w, a.box.h, a.box.d)
	}
}

// TestMathHostile bounds the work of formulas a program sends to blow up:
// a matrix of many rows and columns, matrices nested in each other's
// cells, and nesting deeper than any formula needs.
func TestMathHostile(t *testing.T) {
	n := 10000
	wide := `\begin{matrix}` + strings.Repeat("&", n) + strings.Repeat(`\\`, n) + `\end{matrix}`
	nested := strings.Repeat(`\begin{matrix}a&b\\c&d\\`, 40) + strings.Repeat(`\end{matrix}`, 40)
	many := strings.Repeat(`\begin{matrix}`+strings.Repeat("&", 99)+`\\`+`\end{matrix}`, 200)
	for _, c := range []struct{ name, tex, want string }{
		{"wide", wide, "cells in environments"},
		{"many", many, "cells in environments"},
		{"nested", nested, "nested more than"},
		{"braces", strings.Repeat("{", 30000) + strings.Repeat("}", 30000), "nested more than"},
		{"sqrt", strings.Repeat(`\sqrt`, 30000) + "x", "nested more than"},
	} {
		start := time.Now()
		s := typesetMath(c.tex, 17, true, ui.Color{})
		if s.err == nil || !strings.Contains(s.err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.name, s.err, c.want)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%s: took %v", c.name, d)
		}
	}
	if s := typesetMath(`\begin{pmatrix}a&b\\c&\begin{matrix}1&2\\3&4\end{matrix}\end{pmatrix}`, 17, true, ui.Color{}); s.err != nil {
		t.Errorf("a small nested matrix: %v", s.err)
	}
}

// TestMathCacheMany keeps every formula of a page with more of them than
// the cache's soft bound typeset from one frame to the next, and keeps
// the cache within its hard bound however many there are.
func TestMathCacheMany(t *testing.T) {
	page := make([]*mathSet, mathSoftSets+200)
	for i := range page {
		page[i] = typesetMath(fmt.Sprintf("x_{%d}", i), 13, false, ui.Color{G: 1})
	}
	for i := range page {
		if typesetMath(fmt.Sprintf("x_{%d}", i), 13, false, ui.Color{G: 1}) != page[i] {
			t.Fatalf("formula %d of %d typeset again the next frame", i, len(page))
		}
	}
	for i := range mathHardSets + 100 {
		typesetMath(fmt.Sprintf("y_{%d}", i), 13, false, ui.Color{G: 1})
	}
	mathCache.Lock()
	sets, ops := len(mathCache.m), mathCache.ops
	mathCache.Unlock()
	if sets > mathHardSets || ops > mathHardOps {
		t.Errorf("cache holds %d formulas, %d ops; bounds %d, %d", sets, ops, mathHardSets, mathHardOps)
	}
}
