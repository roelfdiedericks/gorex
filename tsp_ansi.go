package main

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// ansiSpans turns a program's terminal output into styled runs: SGR
// sets the colors (the 16 of the terminal's palette, the 256, true
// color), bold, dim, italic, underline, strikethrough and reverse; other
// escape sequences are dropped. base is the run's style before any SGR,
// fg its color, which dim fades.
func ansiSpans(s string, base ui.Span, pal *[16]ui.Color, bg ui.Color) []ui.Span {
	var spans []ui.Span
	cur := base
	var fg, back ui.Color
	var hasFg, hasBack, bold, dim, reverse bool
	style := func() ui.Span {
		sp := cur
		f, b := base.Color, ui.Color{}
		if hasFg {
			f = fg
		}
		if hasBack {
			b = back
		}
		if reverse {
			f, b = b, f
			if f == (ui.Color{}) {
				f = bg
			}
		}
		if bold {
			sp.Weight = 700
		}
		if dim {
			f.A = f.A * 6 / 10
		}
		sp.Color, sp.Background = f, b
		return sp
	}
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		sp := style()
		sp.Text = text.String()
		spans = append(spans, sp)
		text.Reset()
	}
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			if s[i] == '\r' {
				continue
			}
			text.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		switch s[i+1] {
		case '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				flush()
				params := strings.Split(s[i+2:j], ";")
				for p := 0; p < len(params); p++ {
					n, _ := strconv.Atoi(strings.SplitN(params[p], ":", 2)[0])
					switch {
					case n == 0:
						cur = base
						hasFg, hasBack, bold, dim, reverse = false, false, false, false, false
					case n == 1:
						bold = true
					case n == 2:
						dim = true
					case n == 3:
						cur.Italic = true
					case n == 4:
						cur.Underline = true
					case n == 7:
						reverse = true
					case n == 9:
						cur.Strikethrough = true
					case n == 22:
						bold, dim = false, false
					case n == 23:
						cur.Italic = false
					case n == 24:
						cur.Underline = false
					case n == 27:
						reverse = false
					case n == 29:
						cur.Strikethrough = false
					case n >= 30 && n <= 37:
						fg, hasFg = pal[n-30], true
					case n >= 90 && n <= 97:
						fg, hasFg = pal[n-90+8], true
					case n == 39:
						hasFg = false
					case n >= 40 && n <= 47:
						back, hasBack = pal[n-40], true
					case n >= 100 && n <= 107:
						back, hasBack = pal[n-100+8], true
					case n == 49:
						hasBack = false
					case n == 38 || n == 48:
						c, used, ok := ansiExtended(params[p+1:], pal)
						p += used
						if ok && n == 38 {
							fg, hasFg = c, true
						} else if ok {
							back, hasBack = c, true
						}
					}
				}
			}
			i = j
		case ']':
			// An OSC, as a hyperlink or a title: up to BEL or ST.
			j := i + 2
			for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
				j++
			}
			if j < len(s) && s[j] == 0x1b {
				j++
			}
			i = j
		default:
			i++
		}
	}
	flush()
	return spans
}

// ansiExtended reads the color of 38 or 48 from the parameters after it:
// 5;n (the 256) or 2;r;g;b. It reports how many it used.
func ansiExtended(params []string, pal *[16]ui.Color) (ui.Color, int, bool) {
	if len(params) == 0 {
		return ui.Color{}, 0, false
	}
	switch params[0] {
	case "5":
		if len(params) < 2 {
			return ui.Color{}, len(params), false
		}
		n, err := strconv.Atoi(params[1])
		if err != nil || n < 0 || n > 255 {
			return ui.Color{}, 2, false
		}
		return ansi256(n, pal), 2, true
	case "2":
		if len(params) < 4 {
			return ui.Color{}, len(params), false
		}
		var v [3]int
		for k := range v {
			v[k], _ = strconv.Atoi(params[1+k])
		}
		return ui.Color{R: uint8(v[0]), G: uint8(v[1]), B: uint8(v[2]), A: 255}, 4, true
	}
	return ui.Color{}, 1, false
}

// ansi256 is color n of the 256: the palette's 16, the 6×6×6 cube and the
// 24 grays.
func ansi256(n int, pal *[16]ui.Color) ui.Color {
	switch {
	case n < 16:
		return pal[n]
	case n < 232:
		n -= 16
		level := func(v int) uint8 {
			if v == 0 {
				return 0
			}
			return uint8(55 + v*40)
		}
		return ui.Color{R: level(n / 36), G: level(n / 6 % 6), B: level(n % 6), A: 255}
	default:
		g := uint8(8 + (n-232)*10)
		return ui.Color{R: g, G: g, B: g, A: 255}
	}
}

// ansiPalette is the terminal's palette in the window's appearance, for
// the colors of programs' output in their surfaces.
func (a *App) ansiPalette() *terminal.Theme {
	key := a.eff.Appearance.ThemeLight + "\x00" + a.eff.Appearance.Theme
	if a.ansiThemes.key != key || a.ansiThemes.dark == nil {
		light, dark := termThemes(&a.eff)
		a.ansiThemes.key, a.ansiThemes.light, a.ansiThemes.dark = key, light, dark
	}
	if a.dark {
		return a.ansiThemes.dark
	}
	return a.ansiThemes.light
}

// ansiLines splits styled runs into lines at their newlines.
func ansiLines(spans []ui.Span) [][]ui.Span {
	lines := [][]ui.Span{nil}
	for _, sp := range spans {
		parts := strings.Split(sp.Text, "\n")
		for i, part := range parts {
			if i > 0 {
				lines = append(lines, nil)
			}
			if part != "" {
				p := sp
				p.Text = part
				lines[len(lines)-1] = append(lines[len(lines)-1], p)
			}
		}
	}
	return lines
}
