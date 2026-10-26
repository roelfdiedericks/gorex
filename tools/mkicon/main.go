// Mkicon draws GoRex's icon, resources/icon.png, from its master,
// tools/mkicon/icon.svg: the dark tile and the cyan rex of mitred lines
// on it, 1024 pixels square, from which MyGo's builds make every
// platform's icon (make icon).
//
// It reads the subset of SVG the master is drawn in, and refuses any
// other: rect elements (x, y, width, height, rx, fill) and path elements
// of absolute M, L and Z, filled or stroked with mitred joins and butt
// ends, in groups that translate and scale them and give them fill,
// stroke, stroke-width and stroke-miterlimit.
package main

import (
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

const (
	src  = "tools/mkicon/icon.svg"
	dst  = "resources/icon.png"
	size = 1024
)

func main() {
	f, err := os.Open(src)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	if err := render(img, f); err != nil {
		log.Fatalf("%s: %v", src, err)
	}
	out, err := os.Create(dst)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(out, img); err != nil {
		log.Fatal(err)
	}
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
}

// style is what a group gives the shapes in it: their transform, x' =
// dx + s*x, and how they are painted.
type style struct {
	dx, dy, s   float64
	fill        string
	stroke      string
	width       float64
	miterLimit  float64
	strokeGiven bool
}

// render paints the SVG's shapes on img, in the order they come.
func render(img *image.RGBA, r io.Reader) error {
	d := xml.NewDecoder(r)
	stack := []style{{s: 1, fill: "#000000", stroke: "none", width: 1, miterLimit: 4}}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			st, err := styled(stack[len(stack)-1], t)
			if err != nil {
				return fmt.Errorf("<%s>: %v", t.Name.Local, err)
			}
			stack = append(stack, st)
			switch t.Name.Local {
			case "svg", "g", "title":
			case "rect":
				err = drawRect(img, st, t)
			case "path":
				err = drawPath(img, st, attr(t, "d"))
			default:
				err = fmt.Errorf("not drawn: only rect, path and g are")
			}
			if err != nil {
				return fmt.Errorf("<%s>: %v", t.Name.Local, err)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

// styled is the style an element gives itself and its children, over
// its parent's.
func styled(st style, e xml.StartElement) (style, error) {
	for _, a := range e.Attr {
		v := a.Value
		var err error
		switch a.Name.Local {
		case "fill":
			st.fill = v
		case "stroke":
			st.stroke, st.strokeGiven = v, true
		case "stroke-width":
			st.width, err = strconv.ParseFloat(v, 64)
		case "stroke-miterlimit":
			st.miterLimit, err = strconv.ParseFloat(v, 64)
		case "stroke-linejoin":
			if v != "miter" {
				err = fmt.Errorf("stroke-linejoin %q: only miter is drawn", v)
			}
		case "stroke-linecap":
			if v != "butt" {
				err = fmt.Errorf("stroke-linecap %q: only butt is drawn", v)
			}
		case "transform":
			st, err = transformed(st, v)
		case "opacity", "fill-opacity", "stroke-opacity", "style", "clip-path", "mask", "filter":
			err = fmt.Errorf("%s is not drawn", a.Name.Local)
		}
		if err != nil {
			return st, err
		}
	}
	return st, nil
}

// transformed applies a transform of translate and scale functions, as
// SVG does: each in turn, the last nearest the shapes.
func transformed(st style, v string) (style, error) {
	for v = strings.TrimSpace(v); v != ""; v = strings.TrimSpace(v) {
		name, rest, ok := strings.Cut(v, "(")
		args, after, ok2 := strings.Cut(rest, ")")
		if !ok || !ok2 {
			return st, fmt.Errorf("transform %q", v)
		}
		nums, err := numbers(args)
		if err != nil {
			return st, err
		}
		switch strings.TrimSpace(name) {
		case "translate":
			if len(nums) == 1 {
				nums = append(nums, 0)
			}
			if len(nums) != 2 {
				return st, fmt.Errorf("translate(%s)", args)
			}
			st.dx += st.s * nums[0]
			st.dy += st.s * nums[1]
		case "scale":
			if len(nums) != 1 {
				return st, fmt.Errorf("scale(%s): only a uniform scale is drawn", args)
			}
			st.s *= nums[0]
		default:
			return st, fmt.Errorf("transform %s: only translate and scale are drawn", name)
		}
		v = after
	}
	return st, nil
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func num(e xml.StartElement, name string) float64 {
	v, _ := strconv.ParseFloat(attr(e, name), 64)
	return v
}

// numbers splits a list of numbers by spaces and commas.
func numbers(s string) ([]float64, error) {
	var out []float64
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' }) {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// paint is the color of a fill or stroke attribute; ok is false for none.
func paint(v string) (c color.NRGBA, ok bool, err error) {
	if v == "none" || v == "" {
		return c, false, nil
	}
	h := strings.TrimPrefix(v, "#")
	if len(h) != 6 || h == v {
		return c, false, fmt.Errorf("paint %q: only #rrggbb is drawn", v)
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return c, false, fmt.Errorf("paint %q", v)
	}
	return color.NRGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}, true, nil
}

type pt struct{ x, y float64 }

// at is a point of the shape in the image.
func (st style) at(p pt) pt { return pt{st.dx + st.s*p.x, st.dy + st.s*p.y} }

// polygon adds a closed polygon to the rasterizer, turned so that every
// one winds the same way: their coverage adds, and where they overlap,
// as a line's segments and joins do, the shape stays whole.
func polygon(r *vector.Rasterizer, ps []pt) {
	area := 0.0
	for i, p := range ps {
		q := ps[(i+1)%len(ps)]
		area += p.x*q.y - q.x*p.y
	}
	if area < 0 {
		for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
			ps[i], ps[j] = ps[j], ps[i]
		}
	}
	r.MoveTo(float32(ps[0].x), float32(ps[0].y))
	for _, p := range ps[1:] {
		r.LineTo(float32(p.x), float32(p.y))
	}
	r.ClosePath()
}

func drawRect(img *image.RGBA, st style, e xml.StartElement) error {
	c, ok, err := paint(st.fill)
	if err != nil || !ok {
		return err
	}
	if st.strokeGiven && st.stroke != "none" {
		return fmt.Errorf("a stroked rect is not drawn")
	}
	x, y, w, h, rad := num(e, "x"), num(e, "y"), num(e, "width"), num(e, "height"), num(e, "rx")
	p, q := st.at(pt{x, y}), st.at(pt{x + w, y + h})
	rad *= st.s
	r := vector.NewRasterizer(size, size)
	// Quarter circles as cubic Béziers, k of the radius from each end.
	k := 0.5523 * rad
	f := func(v float64) float32 { return float32(v) }
	r.MoveTo(f(p.x+rad), f(p.y))
	r.LineTo(f(q.x-rad), f(p.y))
	r.CubeTo(f(q.x-rad+k), f(p.y), f(q.x), f(p.y+rad-k), f(q.x), f(p.y+rad))
	r.LineTo(f(q.x), f(q.y-rad))
	r.CubeTo(f(q.x), f(q.y-rad+k), f(q.x-rad+k), f(q.y), f(q.x-rad), f(q.y))
	r.LineTo(f(p.x+rad), f(q.y))
	r.CubeTo(f(p.x+rad-k), f(q.y), f(p.x), f(q.y-rad+k), f(p.x), f(q.y-rad))
	r.LineTo(f(p.x), f(p.y+rad))
	r.CubeTo(f(p.x), f(p.y+rad-k), f(p.x+rad-k), f(p.y), f(p.x+rad), f(p.y))
	r.ClosePath()
	r.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
	return nil
}

// drawPath fills a path's polygons, or strokes its lines, or both.
func drawPath(img *image.RGBA, st style, d string) error {
	lines, err := parsePath(d)
	if err != nil {
		return err
	}
	for i, l := range lines {
		for j, p := range l.pts {
			lines[i].pts[j] = st.at(p)
		}
	}
	if c, ok, err := paint(st.fill); err != nil {
		return err
	} else if ok {
		r := vector.NewRasterizer(size, size)
		for _, l := range lines {
			if len(l.pts) >= 3 {
				polygon(r, append([]pt(nil), l.pts...))
			}
		}
		r.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
	}
	if c, ok, err := paint(st.stroke); err != nil {
		return err
	} else if ok {
		r := vector.NewRasterizer(size, size)
		for _, l := range lines {
			stroke(r, l, st.width*st.s/2, st.miterLimit)
		}
		r.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
	}
	return nil
}

// line is a subpath: its points, closed or open.
type line struct {
	pts    []pt
	closed bool
}

// parsePath reads a path of absolute M, L and Z commands.
func parsePath(d string) ([]line, error) {
	var out []line
	cmd := byte(0)
	fields := strings.FieldsFunc(d, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' })
	var toks []string
	for _, f := range fields {
		// A command letter may be stuck to its first number: M102.
		for len(f) > 0 && strings.ContainsRune("MLZmlzHVCSQTAhvcsqta", rune(f[0])) {
			toks = append(toks, f[:1])
			f = f[1:]
		}
		if f != "" {
			toks = append(toks, f)
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if len(t) == 1 && (t[0] < '0' || t[0] > '9') && t[0] != '-' && t[0] != '.' {
			cmd = t[0]
			switch cmd {
			case 'Z':
				if len(out) == 0 {
					return nil, fmt.Errorf("Z before M")
				}
				out[len(out)-1].closed = true
				continue
			case 'M', 'L':
				continue
			default:
				return nil, fmt.Errorf("path command %c: only M, L and Z are drawn", cmd)
			}
		}
		if i+1 >= len(toks) {
			return nil, fmt.Errorf("path: a coordinate without its pair")
		}
		x, err1 := strconv.ParseFloat(toks[i], 64)
		y, err2 := strconv.ParseFloat(toks[i+1], 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("path: %q %q", toks[i], toks[i+1])
		}
		i++
		switch cmd {
		case 'M':
			out = append(out, line{pts: []pt{{x, y}}})
			cmd = 'L' // coordinates after a moveto are lines
		case 'L':
			if len(out) == 0 {
				return nil, fmt.Errorf("L before M")
			}
			out[len(out)-1].pts = append(out[len(out)-1].pts, pt{x, y})
		default:
			return nil, fmt.Errorf("path: a coordinate before a command")
		}
	}
	return out, nil
}

// stroke adds a line of half width hw to the rasterizer: a quadrilateral
// along each segment, its ends butt, and at each corner the wedge that
// mitres it, or bevels it where the mitre would reach further than
// limit times the width.
func stroke(r *vector.Rasterizer, l line, hw, limit float64) {
	ps := l.pts
	if l.closed && len(ps) > 1 && ps[0] == ps[len(ps)-1] {
		ps = ps[:len(ps)-1]
	}
	n := len(ps)
	if n < 2 {
		return
	}
	segs := n - 1
	if l.closed {
		segs = n
	}
	normal := func(a, b pt) pt {
		dx, dy := b.x-a.x, b.y-a.y
		d := math.Hypot(dx, dy)
		return pt{-dy / d * hw, dx / d * hw}
	}
	for i := 0; i < segs; i++ {
		a, b := ps[i], ps[(i+1)%n]
		m := normal(a, b)
		polygon(r, []pt{{a.x + m.x, a.y + m.y}, {b.x + m.x, b.y + m.y}, {b.x - m.x, b.y - m.y}, {a.x - m.x, a.y - m.y}})
	}
	for i := 0; i < n; i++ {
		if !l.closed && (i == 0 || i == n-1) {
			continue
		}
		prev, p, next := ps[(i+n-1)%n], ps[i], ps[(i+1)%n]
		m1, m2 := normal(prev, p), normal(p, next)
		// The outer side of the turn: the one the segments' ends open on.
		cross := (p.x-prev.x)*(next.y-p.y) - (p.y-prev.y)*(next.x-p.x)
		if cross > 0 {
			m1, m2 = pt{-m1.x, -m1.y}, pt{-m2.x, -m2.y}
		}
		if cross == 0 {
			continue
		}
		e1, e2 := pt{p.x + m1.x, p.y + m1.y}, pt{p.x + m2.x, p.y + m2.y}
		// The mitre's tip, where the segments' outer edges meet: along the
		// bisector of the normals, hw over the cosine of half the turn.
		bx, by := m1.x+m2.x, m1.y+m2.y
		bl := math.Hypot(bx, by)
		cosHalf := bl / (2 * hw)
		if cosHalf <= 0 || 1/cosHalf > limit {
			polygon(r, []pt{p, e1, e2})
			continue
		}
		reach := hw / cosHalf
		tip := pt{p.x + bx/bl*reach, p.y + by/bl*reach}
		polygon(r, []pt{p, e1, tip, e2})
	}
}
