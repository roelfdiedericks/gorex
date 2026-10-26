package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// tspTabs draws a tab strip: its tabs over a hairline, the active one
// underlined in the accent. The active tab is the program's: a click
// sends select with the tab's id, and the strip changes when the
// program says so.
func (a *App) tspTabs(c *ui.Context, key *ui.Context, k *colors, p *Pane, sf *rex.Surface, n *rex.TSPNode) ui.Element {
	items, _ := n.Props["items"].([]any)
	active, _ := n.Props["active"].(string)
	strip := ui.Column(key).FillWidth()
	strip.Children(func() {
		row := ui.Row(c).FillWidth().Clip().Unselectable()
		row.Children(func() {
			for i, it := range items {
				it, _ := it.(map[string]any)
				id, _ := it["id"].(string)
				if id == "" {
					continue
				}
				on := id == active
				tab := ui.Column(c.Key(fmt.Sprintf("tab-%d-%s", i, id))).Shrink(0)
				tab.Children(func() {
					l := ui.Box(c).Padding(4, 10, 3, 10)
					l.Children(func() {
						t := a.tspRich(c, k, it["label"], 12, false)
						if !on {
							t.TextColor(k.textMuted)
						}
					})
					if on {
						l.Background(ui.Transparent)
					} else if tab.Hovered() {
						l.Background(k.hover)
					}
					bar := ui.Box(c).Height(2).FillWidth().Radius(1)
					if on {
						bar.Background(k.busy)
					}
				})
				if tab.Clicked() && !sf.Closed && !sf.NoListen {
					a.tspSend(p, map[string]any{"ev": "select", "sf": sf.ID, "id": n.ID, "item": id})
				}
			}
		})
		ui.Box(c).Height(1).FillWidth().Background(k.cardBorder)
	})
	return strip
}

// tspEffortLevels are the thinking-effort rungs, from none to the full
// ring, and the palette token and span token each colors its arc with.
var tspEffortLevels = map[string]struct {
	rung        int
	token, tone string
}{
	"off": {0, "thinkingOff", ""}, "minimal": {1, "thinkingMinimal", "muted"}, "low": {2, "thinkingLow", "info"},
	"medium": {3, "thinkingMedium", "accent"}, "high": {4, "thinkingHigh", "warning"},
	"xhigh": {5, "thinkingXhigh", "error"}, "max": {6, "thinkingMax", "error"},
}

// tspEffort draws the thinking-effort glyph: a 14px ring filled a sixth
// a rung in its level's color, on fire at max, its flames flickering
// unless the desktop asks for less motion; a dashed empty ring for a
// level not known (auto).
func (a *App) tspEffort(c *ui.Context, key *ui.Context, k *colors, sf *rex.Surface, n *rex.TSPNode) ui.Element {
	level, _ := n.Props["level"].(string)
	lv, known := tspEffortLevels[level]
	arc := k.textMuted
	if c, ok := a.tspPal[lv.token]; ok {
		arc = c
	} else if c, ok := a.spanColor(k, lv.tone); ok {
		arc = c
	}
	still := a.reduceMotion || sf.Closed
	track := k.textFaint.Alpha(0.45)
	fire, fireHi := arc.Mix(ui.Hex("#ff6a1a"), 0.5), ui.Hex("#ffc24a")
	el := ui.Box(key).Size(16, 16).Shrink(0).Tooltip("Thinking effort: " + level)
	el.Draw(func(pt *ui.Painter, r ui.Rect) {
		cx, cy, rad := r.X+r.W/2, r.Y+r.H/2+1, float32(6)
		ring := ui.Rect{X: cx - rad, Y: cy - rad, W: 2 * rad, H: 2 * rad}
		switch {
		case !known:
			pt.StrokeDashed(ring, track, rad, 1.5)
		case lv.rung == 6:
			// On fire: the ring blazes, and a crown of flames rises off
			// its top, flickering.
			pt.Stroke(ring, fire, rad, 2)
			phase := 0
			if !still {
				phase = int(pt.Now().UnixMilli()/350) % 4
				pt.After(350 * time.Millisecond)
			}
			for i, dx := range []float32{-3, 0, 3} {
				h := []float32{3, 4.5, 3}[i] + float32((phase+i)%3)*0.6
				pt.Line(cx+dx, cy-rad+0.5, cx+dx*0.6, cy-rad-h, 1.6, fireHi.Mix(fire, float32(i%2)*0.5))
			}
			pt.Fill(ui.Rect{X: cx - 1.5, Y: cy - 1.5, W: 3, H: 3}, fireHi, 1.5)
		default:
			pt.Stroke(ring, track, rad, 1.5)
			// The arc, clockwise from the top, in short chords.
			end := float64(lv.rung) / 6 * 2 * math.Pi
			const step = math.Pi / 24
			for t := 0.0; t < end-1e-9; t += step {
				t1 := math.Min(t+step, end)
				x0, y0 := cx+rad*float32(math.Sin(t)), cy-rad*float32(math.Cos(t))
				x1, y1 := cx+rad*float32(math.Sin(t1)), cy-rad*float32(math.Cos(t1))
				pt.Line(x0, y0, x1, y1, 2, arc)
			}
		}
	})
	return el
}

// tspShimmer draws a working label with a highlight sweeping across it,
// character by character: each character goes from the low color to
// the high one and back through the middle, a little after the one
// before (the protocol's sf-sh: 2.25s in 7 steps, 45ms apart), the
// classic mode restarting from the left, kitt bouncing back. Less motion,
// or a kept surface, shows the plain text.
func (a *App) tspShimmer(c *ui.Context, key *ui.Context, k *colors, sf *rex.Surface, n *rex.TSPNode) ui.Element {
	var spans []ui.Span
	switch v := n.Props["spans"].(type) {
	case []any:
		for _, s := range v {
			m, _ := s.(map[string]any)
			text, _ := m["t"].(string)
			style, _ := m["s"].(string)
			sp := ui.Span{Text: text, Size: 13, Color: k.text}
			for _, tok := range strings.Fields(style) {
				switch tok {
				case "strong":
					sp.Weight = 600
				case "em":
					sp.Italic = true
				}
			}
			spans = append(spans, sp)
		}
	default:
		text, _ := n.Props["text"].(string)
		spans = []ui.Span{{Text: text, Size: 13, Color: k.text}}
	}
	// A label sweeps; a long text, a span a character each frame after
	// frame, would cost more than it shows: it stays still.
	if a.reduceMotion || sf.Closed || len(shimmerText(spans)) > tspShimmerMax {
		return ui.RichText(key, spans...)
	}
	lo, mid, hi := k.textFaint, k.textMuted, k.text
	if pal, ok := n.Props["palette"].(map[string]any); ok {
		pick := func(v any, def ui.Color) ui.Color {
			s, _ := v.(string)
			if f := strings.Fields(s); len(f) > 0 {
				if col, ok := a.spanColor(k, f[0]); ok {
					return col
				}
			}
			return def
		}
		lo, mid, hi = pick(pal["low"], lo), pick(pal["mid"], mid), pick(pal["high"], hi)
	}
	kitt, _ := n.Props["mode"].(string)
	const period, steps, lag = 2250.0, 7, 45.0
	now := float64(time.Now().UnixMilli())
	var out []ui.Span
	i := 0
	for _, sp := range spans {
		for _, r := range sp.Text {
			t := now - float64(i)*lag
			if kitt == "kitt" {
				// alternate: every other cycle runs backwards.
				cycle := math.Floor(t / period)
				t = math.Mod(t, period)
				if int(cycle)%2 == 1 {
					t = period - t
				}
			} else {
				t = math.Mod(t, period)
			}
			if t < 0 {
				t += period
			}
			// steps(7) over the keyframes: lo, hi at 14%, mid at 28%,
			// lo from 42% on.
			f := math.Floor(t/period*steps) / steps
			col := lo
			switch {
			case f >= 0.14 && f < 0.28:
				col = hi
			case f >= 0.28 && f < 0.42:
				col = mid
			}
			ch := sp
			ch.Text, ch.Color = string(r), col
			out = append(out, ch)
			i++
		}
	}
	if !a.shimmerDue {
		// The band moves a character every 45ms: two characters a
		// redraw keeps it sweeping without redrawing at that rate.
		a.shimmerDue = true
		time.AfterFunc(90*time.Millisecond, func() {
			a.shimmerDue = false
			c.Invalidate()
		})
	}
	return ui.RichText(key, out...).Tooltip(shimmerText(spans))
}

// tspShimmerMax is the longest shimmer, in bytes, that sweeps.
const tspShimmerMax = 256

// shimmerText is a shimmer's plain text.
func shimmerText(spans []ui.Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}

// tspRows draws rows a program rendered for a terminal (the migration
// fallback): every line as it is, in its colors, unwrapped, with a dim
// fallback mark.
func (a *App) tspRows(c *ui.Context, key *ui.Context, k *colors, n *rex.TSPNode) ui.Element {
	lines, _ := n.Props["lines"].([]any)
	th := a.ansiPalette()
	font := a.editorFont().Family
	var spans []ui.Span
	count := 0
	for _, l := range lines {
		s, ok := l.(string)
		if !ok {
			continue
		}
		if count > 0 {
			spans = append(spans, ui.Span{Text: "\n", Font: font, Size: 12})
		}
		spans = append(spans, ansiSpans(s, ui.Span{Font: font, Size: 12, Color: k.text}, &th.Palette, k.bgLow)...)
		count++
	}
	block := ui.Column(key).Radius(6).Background(k.bgLow).Padding(6, 10).Gap(2).Clip()
	block.Children(func() {
		mark := ui.Row(c).FillWidth().Justify(ui.End)
		mark.Children(func() {
			ui.Text(c, "fallback").FontSize(10).TextColor(k.textFaint).Tooltip(fmt.Sprintf("Fallback rows (%d)", count))
		})
		if len(spans) > 0 {
			ui.RichText(c, spans...).NoWrap().Selectable()
		}
	})
	return block
}

// tspOmpLogo is omp's brand mark, the block grid its terminal banner
// draws: full blocks, and a half-shaded one.
var tspOmpLogo = []string{"████████████", "   ██  ██   ", "   ██  ██   ", "   ▒▒  ██   ", "       ██   "}

// tspOmpStops are the stops of the mark's diagonal gradient, top left to
// bottom right.
var tspOmpStops = []ui.Color{ui.RGB(248, 79, 204), ui.RGB(147, 98, 244), ui.RGB(0, 219, 228)}

// tspLogo draws a builtin image (image.builtin): omp's mark, w wide (its
// cells as tall as two wide, as in a terminal), in its gradient.
func (a *App) tspLogo(key *ui.Context, w float32, alt string) ui.Element {
	if w <= 0 {
		w = 96
	}
	cols, rows := float32(12), float32(len(tspOmpLogo))
	cw := w / cols
	ch := 2 * cw
	el := ui.Box(key).Size(w, ch*rows).Shrink(0).Tooltip(alt)
	el.Draw(func(pt *ui.Painter, r ui.Rect) {
		for y, line := range tspOmpLogo {
			for x, b := range []rune(line) {
				if b == ' ' {
					continue
				}
				// t runs along the diagonal, 0 to 1.
				t := (float32(x)/(cols-1) + float32(y)/(rows-1)) / 2
				seg := t * float32(len(tspOmpStops)-1)
				i := min(int(seg), len(tspOmpStops)-2)
				col := tspOmpStops[i].Mix(tspOmpStops[i+1], seg-float32(i))
				if b == '▒' {
					col = col.Alpha(0.5)
				}
				// A hair over the cell, so that neighbours join.
				pt.Fill(ui.Rect{X: r.X + float32(x)*cw, Y: r.Y + float32(y)*ch, W: cw + 0.5, H: ch + 0.5}, col, 0)
			}
		}
	})
	return el
}
