//go:build !gorexserver

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	mermaid "github.com/bvolpato/mermaid-go-renderer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// A surface's Mermaid diagrams (omp's mermaid fences in its Markdown) are
// drawn as Tern draws them: a figure with the diagram's kind and Source,
// Copy and Open over the drawing. mmdg, a port of mermaid to Go, makes a
// picture of each off the UI thread. A block shows as code until its
// fence closes and while its picture is made, and stays code when the
// diagram does not render.

// tspMermaidScale is the pixels per point of a diagram's picture: sharp
// on a high-density screen, scaled down on others.
const tspMermaidScale = 2

// tspMermaid is a diagram drawn for a theme: its picture once made, the
// kind of diagram, and whether the figure shows its source instead; or
// why it could not be drawn. Its SVG draws parts of it sharp in the zoom
// view (sharpenDiagram).
type tspMermaid struct {
	bm     *ui.Bitmap
	kind   string
	source bool
	err    string
	svg    string
}

// tspMermaidSlots bounds the diagrams being drawn at once, as when a
// transcript with many of them loads.
var tspMermaidSlots = make(chan struct{}, 2)

// tspMermaidFor returns the diagram src drawn for the window's theme,
// starting to draw it when it is new; the last ones are kept, by source
// and theme.
func (a *App) tspMermaidFor(src string) *tspMermaid {
	key := fmt.Sprintf("%v/%s", a.dark, src)
	if m, ok := a.tspMermaids[key]; ok {
		return m
	}
	if a.tspMermaids == nil || len(a.tspMermaids) > 32 {
		a.tspMermaids = map[string]*tspMermaid{}
	}
	m := &tspMermaid{}
	a.tspMermaids[key] = m
	theme := "default"
	if a.dark {
		theme = "dark"
	}
	go func() {
		tspMermaidSlots <- struct{}{}
		defer func() { <-tspMermaidSlots }()
		kind, svg, png, err := renderMermaid(src, theme)
		var bm *ui.Bitmap
		if err == nil {
			bm, err = ui.DecodeBitmap(png)
		}
		if err != nil {
			log.Printf("mermaid: %v", err)
			a.laterService(func() { m.err = mermaidErr(err) })
			return
		}
		a.laterService(func() { m.bm, m.kind, m.svg = bm, kind, svg })
	}()
	return m
}

// renderMermaid draws a diagram as a PNG with no background of its own,
// so that the figure's shows through it, in mermaid's theme, and tells
// its kind and its SVG. A diagram that takes more than ten seconds is
// given up.
func renderMermaid(src, theme string) (kind, svg string, png []byte, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := []mermaid.Option{mermaid.WithTheme(theme), mermaid.WithScale(tspMermaidScale), mermaid.WithBackgroundColor("transparent")}
	res, err := mermaid.RenderContext(ctx, src, opts...)
	if err != nil {
		return "", "", nil, err
	}
	png, err = mermaid.SVGToPNG(res.SVG, opts...)
	return res.Type, res.SVG, png, err
}

// sharpenDiagram draws the part of the zoom view's diagram that shows
// again at the zoom it shows, which past its picture's own pixels, scaled
// up, would blur: the view lays it over the picture (tspViewer.sharp).
func (a *App) sharpenDiagram(v *tspViewer) {
	m := v.diagram
	if m == nil || m.svg == "" || v.zoom <= 1 {
		return
	}
	// The part in view, in the picture's DIPs.
	x0, y0 := max(0, v.cx-v.vw/2/v.zoom), max(0, v.cy-v.vh/2/v.zoom)
	x1, y1 := min(v.w, v.cx+v.vw/2/v.zoom), min(v.h, v.cy+v.vh/2/v.zoom)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	zoom, svg := v.zoom, m.svg
	go func() {
		tspMermaidSlots <- struct{}{}
		defer func() { <-tspMermaidSlots }()
		png, err := cropMermaid(svg, x0, y0, x1-x0, y1-y0, zoom*tspMermaidScale)
		var bm *ui.Bitmap
		if err == nil {
			bm, err = ui.DecodeBitmap(png)
		}
		if err != nil {
			log.Printf("mermaid: drawing a part: %v", err)
			return
		}
		a.laterService(func() {
			if a.tspZoom == v && v.zoom == zoom {
				v.sharp = &tspSharp{bm: bm, x: x0, y: y0, w: x1 - x0, h: y1 - y0, zoom: zoom}
			}
		})
	}()
}

// The attributes of a diagram's svg element that size it: the renderer
// draws it max-width wide, its viewBox scaled to that.
var (
	mermaidViewBox  = regexp.MustCompile(`viewBox="([-0-9.e]+)[ ,]+([-0-9.e]+)[ ,]+([-0-9.e]+)[ ,]+([-0-9.e]+)"`)
	mermaidMaxWidth = regexp.MustCompile(`max-width: ?([0-9.e]+)px`)
)

// cropMermaid draws the part x, y, w, h of a diagram's SVG, in the DIPs
// of its own size, at scale pixels a DIP: its svg element cut to that part
// (viewBox) and drawn that wide (max-width).
func cropMermaid(svg string, x, y, w, h, scale float32) ([]byte, error) {
	start := strings.Index(svg, "<svg")
	if start < 0 {
		return nil, errors.New("no svg element")
	}
	end := strings.IndexByte(svg[start:], '>')
	if end < 0 {
		return nil, errors.New("no svg element")
	}
	root := svg[start : start+end]
	vb, mw := mermaidViewBox.FindStringSubmatch(root), mermaidMaxWidth.FindStringSubmatch(root)
	if vb == nil || mw == nil {
		return nil, errors.New("its svg element is sized in a way GoRex can't cut")
	}
	var box [4]float64
	for i := range box {
		box[i], _ = strconv.ParseFloat(vb[i+1], 64)
	}
	width, _ := strconv.ParseFloat(mw[1], 64)
	if width <= 0 || box[2] <= 0 {
		return nil, errors.New("its svg element has no size")
	}
	// The viewBox's units a DIP.
	u := box[2] / width
	cut := fmt.Sprintf(`viewBox="%g %g %g %g"`, box[0]+float64(x)*u, box[1]+float64(y)*u, float64(w)*u, float64(h)*u)
	root = mermaidViewBox.ReplaceAllLiteralString(root, cut)
	root = mermaidMaxWidth.ReplaceAllLiteralString(root, fmt.Sprintf("max-width: %gpx", w))
	return mermaid.SVGToPNG(svg[:start]+root+svg[start+end:], mermaid.WithScale(float64(scale)), mermaid.WithBackgroundColor("transparent"))
}

// mermaidErr says in a line why a diagram could not be drawn: the first
// of the renderer's (the parse error and its line).
func mermaidErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "it took too long"
	}
	line, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSuffix(strings.TrimSpace(line), ":")
}

// tspMermaidBlock builds a mermaid code block of the surface's Markdown in
// pane p: the diagram's figure once its picture is made, else the code;
// a diagram that can't be drawn shows its code under why.
func (a *App) tspMermaidBlock(c *ui.Context, k *colors, p *Pane, b *markdown.CodeBlockView) {
	if b.Open || !is(a.eff.Editor.Mermaid) {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	m := a.tspMermaidFor(b.Code())
	if m.bm == nil && m.err == "" {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	size := a.tspMsg(14)
	name := "Mermaid diagram"
	if m.kind != "" {
		name = "Mermaid " + m.kind
	}
	open := func() {
		a.zoomPicture(p, m.bm, tspMermaidScale, name, "", m)
	}
	fig := ui.Column(c).AlignSelf(ui.Stretch).Background(k.card).Border(1, k.cardBorder).Radius(6).Clip()
	fig.Role(ui.RoleGroup).Label(name)
	fig.Children(func() {
		head := ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(0.35*size, 0.4*size, 0, 0.8*size)
		head.Children(func() {
			label := "mermaid"
			if m.kind != "" {
				label += " · " + m.kind
			}
			ui.Text(c, label).FontSize(0.8*size).TextColor(k.textMuted).Unselectable().Shrink(0)
			if m.err != "" {
				ui.Text(c, " · Can't draw the diagram: "+m.err).FontSize(0.8*size).TextColor(k.attention).
					SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Tooltip(m.err)
			}
			ui.Spacer(c)
			if m.err == "" {
				toggle := "Source"
				if m.source {
					toggle = "Diagram"
				}
				if tspFigureButton(c, k, size, toggle).Clicked() {
					m.source = !m.source
				}
			}
			b.CopyButton(c)
			if m.err == "" && tspFigureButton(c, k, size, "Open").Clicked() {
				open()
			}
		})
		if m.source || m.err != "" {
			b.Body(c)
			return
		}
		w, h := m.bm.Size()
		body := ui.ScrollHorizontal(c).FillWidth().Padding(0.25*size, 0.8*size, 0.8*size, 0.8*size)
		body.Children(func() {
			// Centered while narrower than the figure; wider, it scrolls
			// from its left edge.
			img := ui.Image(c, m.bm).Size(float32(w)/tspMermaidScale, float32(h)/tspMermaidScale).Shrink(0).Margin(0, ui.Auto).Cursor(ui.CursorPointer)
			if img.Clicked() {
				open()
			}
		})
	})
}

// tspFigureButton is a button of a word in a figure's head, the size of
// a code block's Copy, whose em is size.
func tspFigureButton(c *ui.Context, k *colors, size float32, label string) ui.Element {
	btn := ui.ButtonBase(c.Key(label)).Padding(0.2*size, 0.45*size).Radius(5).Label(label)
	if btn.Hovered() {
		btn.Background(k.text.Alpha(0.08))
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(0.8*size).TextColor(k.textMuted)
	})
	return btn
}
