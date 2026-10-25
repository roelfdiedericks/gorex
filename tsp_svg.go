package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// A surface's SVGs (omp's charts, as fenced svg blocks in its Markdown)
// color with the reader's theme through var(--fg), var(--muted), … and
// label with text, which MyGo's SVGs leave out: the colors are the
// theme's before it is parsed, and the text is laid over the image, each
// label where the SVG puts it.

// tspSVG is an SVG parsed for a theme, with its text.
type tspSVG struct {
	svg   *ui.SVG
	w, h  float32 // its viewBox's size, which the labels' places are in
	ox    float32 // the viewBox's origin
	oy    float32
	texts []svgText
}

// svgText is a label of an SVG: where, in the viewBox, anchored at its
// start, middle or end, in which color and size.
type svgText struct {
	x, y   float32
	anchor string
	color  ui.Color
	size   float32
	bold   bool
	text   string
}

var svgVar = regexp.MustCompile(`var\(\s*--([a-zA-Z0-9-]+)\s*(?:,\s*([^)]*))?\)`)

// tspSVGFor returns src parsed for the colors k, nil when it does not
// parse; the last ones are kept, by source and theme.
func (a *App) tspSVGFor(k *colors, src string) *tspSVG {
	key := fmt.Sprintf("%v/%s", a.dark, src)
	if s, ok := a.tspSVGs[key]; ok {
		return s
	}
	themed := svgVar.ReplaceAllStringFunc(src, func(m string) string {
		sub := svgVar.FindStringSubmatch(m)
		if c, ok := svgThemeColor(k, sub[1]); ok {
			return hexOf(c)
		}
		if sub[2] != "" {
			return strings.TrimSpace(sub[2])
		}
		return hexOf(k.text)
	})
	s := &tspSVG{}
	svg, err := ui.ParseSVG([]byte(themed))
	if err != nil {
		s = nil
	} else {
		s.svg = svg
		s.w, s.h = svg.Size()
		s.ox, s.oy, s.texts = svgTexts(themed, k)
		if vb := svgViewBox(themed); vb[2] > 0 && vb[3] > 0 {
			s.ox, s.oy, s.w, s.h = vb[0], vb[1], vb[2], vb[3]
		}
	}
	if a.tspSVGs == nil || len(a.tspSVGs) > 64 {
		a.tspSVGs = map[string]*tspSVG{}
	}
	a.tspSVGs[key] = s
	return s
}

// draw shows the SVG at most its own width, with its labels over it,
// scaled as the image is.
func (s *tspSVG) draw(c *ui.Context) {
	box := ui.Box(c).MaxWidthPercent(100).AlignSelf(ui.Start)
	box.Children(func() {
		img := ui.Image(c, s.svg).Width(s.w).MaxWidthPercent(100)
		scale := float32(1)
		if r := img.Bounds(); r.W > 0 && s.w > 0 {
			scale = r.W / s.w
		}
		const span = 400 // room for an anchored label, either side
		for i, t := range s.texts {
			size := t.size * scale
			x := (t.x - s.ox) * scale
			// y is the baseline: the text's box starts about a size above.
			y := (t.y-s.oy)*scale - size*0.95
			// A label anchored at its middle or end is aligned in a box
			// around x, its width not known before it is laid out.
			at := ui.Box(c.Key(i)).Absolute().Top(y)
			switch t.anchor {
			case "middle":
				at.Left(x - span).Width(2 * span).AlignItems(ui.Center)
			case "end":
				at.Left(x - span).Width(span).AlignItems(ui.End)
			default:
				at.Left(x)
			}
			at.Children(func() {
				l := ui.Text(c, t.text).FontSize(size).TextColor(t.color).NoWrap()
				if t.bold {
					l.Bold()
				}
			})
		}
	})
}

// svgThemeColor is the color of a theme variable of the SVGs programs
// draw for a reader's theme.
func svgThemeColor(k *colors, name string) (ui.Color, bool) {
	switch name {
	case "fg", "text", "foreground":
		return k.text, true
	case "muted", "dim":
		return k.textMuted, true
	case "border":
		return k.cardBorder, true
	case "surface", "bg", "background":
		return k.card, true
	case "accent", "success", "c1":
		return k.busy, true
	case "warning", "error", "c2":
		return k.attention, true
	case "c3":
		return ui.RGB(0x60, 0xa5, 0xfa), true
	case "c4":
		return ui.RGB(0xc0, 0x84, 0xfc), true
	case "c5":
		return ui.RGB(0xf4, 0x72, 0xb6), true
	case "c6":
		return ui.RGB(0x2d, 0xd4, 0xbf), true
	}
	return ui.Color{}, false
}

// svgViewBox reads the root's viewBox: x, y, w, h, zero without one.
func svgViewBox(src string) [4]float32 {
	var vb [4]float32
	d := xml.NewDecoder(strings.NewReader(src))
	for {
		tok, err := d.Token()
		if err != nil {
			return vb
		}
		if se, ok := tok.(xml.StartElement); ok {
			for _, at := range se.Attr {
				if at.Name.Local == "viewBox" {
					f := strings.FieldsFunc(at.Value, func(r rune) bool { return r == ' ' || r == ',' })
					for i := 0; i < 4 && i < len(f); i++ {
						v, _ := strconv.ParseFloat(f[i], 32)
						vb[i] = float32(v)
					}
				}
			}
			return vb
		}
	}
}

// svgTexts reads the text elements of an SVG, placed by their x and y
// and the translations of the groups they are in, with their fill, size,
// weight and anchor, inherited from the groups.
func svgTexts(src string, k *colors) (ox, oy float32, out []svgText) {
	type frame struct {
		dx, dy float32
		fill   string
		size   float32
		anchor string
		bold   bool
	}
	stack := []frame{{size: 13, anchor: "start"}}
	d := xml.NewDecoder(bytes.NewReader([]byte(src)))
	var cur *svgText
	for {
		tok, err := d.Token()
		if err != nil {
			return 0, 0, out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			f := stack[len(stack)-1]
			var x, y float32
			for _, at := range t.Attr {
				switch at.Name.Local {
				case "transform":
					if dx, dy, ok := svgTranslate(at.Value); ok {
						f.dx, f.dy = f.dx+dx, f.dy+dy
					}
				case "fill":
					f.fill = at.Value
				case "font-size":
					if v, err := strconv.ParseFloat(strings.TrimSuffix(at.Value, "px"), 32); err == nil {
						f.size = float32(v)
					}
				case "text-anchor":
					f.anchor = at.Value
				case "font-weight":
					f.bold = at.Value == "bold" || at.Value >= "600"
				case "x":
					v, _ := strconv.ParseFloat(strings.Fields(at.Value + " 0")[0], 32)
					x = float32(v)
				case "y":
					v, _ := strconv.ParseFloat(strings.Fields(at.Value + " 0")[0], 32)
					y = float32(v)
				}
			}
			stack = append(stack, f)
			if t.Name.Local == "text" {
				color := k.text
				if f.fill != "" && f.fill != "none" && strings.HasPrefix(f.fill, "#") {
					if c, err := parseHexColor(f.fill); err == nil {
						color = c
					}
				}
				cur = &svgText{x: f.dx + x, y: f.dy + y, anchor: f.anchor, color: color, size: f.size, bold: f.bold}
			}
		case xml.CharData:
			if cur != nil {
				cur.text += string(t)
			}
		case xml.EndElement:
			if t.Name.Local == "text" && cur != nil {
				if s := strings.TrimSpace(cur.text); s != "" {
					cur.text = s
					out = append(out, *cur)
				}
				cur = nil
			}
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// svgTranslate reads translate(x y) or translate(x, y) out of a
// transform, the only one charts' labels are moved by.
func svgTranslate(s string) (dx, dy float32, ok bool) {
	i := strings.Index(s, "translate(")
	if i < 0 {
		return 0, 0, false
	}
	rest := s[i+len("translate("):]
	j := strings.IndexByte(rest, ')')
	if j < 0 {
		return 0, 0, false
	}
	f := strings.FieldsFunc(rest[:j], func(r rune) bool { return r == ' ' || r == ',' })
	if len(f) == 0 {
		return 0, 0, false
	}
	x, _ := strconv.ParseFloat(f[0], 32)
	var y float64
	if len(f) > 1 {
		y, _ = strconv.ParseFloat(f[1], 32)
	}
	return float32(x), float32(y), true
}

// parseHexColor parses #rgb or #rrggbb.
func parseHexColor(s string) (c ui.Color, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("bad color %q", s)
		}
	}()
	return ui.Hex(s), nil
}
