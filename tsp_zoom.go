package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// The zoom view shows a surface's picture (an image, a Mermaid diagram)
// over its pane, as an image viewer does. It opens fitted to the pane, or
// at its own size when smaller. The mouse wheel and a pinch zoom it around
// the pointer, up to eight times its own size; dragging it, or scrolling a
// touchpad, moves it; a double-click goes between fitted and its own size.
// The keys + and - zoom, 0 fits, 1 shows its own size, the arrows move it.
// A click beside it, or Escape, closes it. A diagram zoomed past its
// picture's pixels has the part in view drawn again, sharp
// (sharpenDiagram).

const (
	zoomMax    = 8   // the largest zoom, of the picture's own size
	zoomStep   = 1.2 // a wheel's notch, or a key, zooms by as much
	zoomMargin = 16  // around the picture fitted, in DIPs
	zoomArrow  = 40  // an arrow key moves the picture by as much, in DIPs
	// zoomSettle is how long the zoom and the place stay before a
	// diagram's part in view is drawn again, sharp.
	zoomSettle = 250 * time.Millisecond
	// zoomGlide is how long a double-click's or a key's zoom takes.
	zoomGlide = 150 * time.Millisecond
)

// tspViewer is the zoom view's picture and how it shows: zoom is the size
// it shows at, of its own (w, h, in DIPs), and cx, cy its point at the
// middle of the view, of size vw, vh as last drawn; fit keeps it fitted to
// the view as the view's size changes. A glide (double-click, keys) moves
// the view from glideFrom to where it is; sharp is the part of a diagram
// drawn at a zoom. The picture comes from file path, when it does, which
// Open File opens.
type tspViewer struct {
	pane       *Pane
	bm         *ui.Bitmap
	name, path string
	diagram    *tspMermaid
	w, h       float32
	fit        bool
	zoom       float32
	cx, cy     float32
	vw, vh     float32
	// pressed and dragged follow a press on the view: a press that
	// dragged clicks nothing.
	pressed, dragged bool
	glideFrom        [3]float32
	glides           int
	// due counts the changes of zoom and place, which a sharp drawing
	// waits on to settle.
	due   int
	sharp *tspSharp
}

// tspSharp is the part x, y, w, h of a diagram, in its DIPs, drawn sharp
// at zoom.
type tspSharp struct {
	bm         *ui.Bitmap
	x, y, w, h float32
	zoom       float32
}

// zoomPicture opens the zoom view over pane p on picture bm, whose own
// size is its pixels over scale, named name, from file path ("" for
// none); m is its diagram, when it is one.
func (a *App) zoomPicture(p *Pane, bm *ui.Bitmap, scale float32, name, path string, m *tspMermaid) {
	if bm == nil {
		return
	}
	w, h := bm.Size()
	a.tspZoom = &tspViewer{pane: p, bm: bm, name: name, path: path, diagram: m,
		w: float32(w) / scale, h: float32(h) / scale, fit: true, zoom: 1}
}

// fitZoom is the zoom at which the picture fits the view, at most its own
// size.
func (v *tspViewer) fitZoom() float32 {
	return min(1, (v.vw-2*zoomMargin)/v.w, (v.vh-2*zoomMargin)/v.h)
}

// zoomAt zooms to z, within its bounds, keeping the picture's point at
// x, y of the view where it is; at the fitted zoom it is fitted again.
func (v *tspViewer) zoomAt(z, x, y float32) {
	lo := v.fitZoom()
	z = min(max(z, lo), zoomMax)
	px, py := v.cx+(x-v.vw/2)/v.zoom, v.cy+(y-v.vh/2)/v.zoom
	v.zoom, v.fit = z, z == lo
	v.cx, v.cy = px-(x-v.vw/2)/z, py-(y-v.vh/2)/z
	v.clamp()
}

// moveBy moves the view over the picture by dx, dy DIPs of the screen.
func (v *tspViewer) moveBy(dx, dy float32) {
	v.cx += dx / v.zoom
	v.cy += dy / v.zoom
	v.clamp()
}

// clamp keeps the picture over the view: a side larger than the view
// leaves no gap at its edges, one that fits stays in the middle.
func (v *tspViewer) clamp() {
	v.cx = zoomAxis(v.cx, v.w, v.vw/v.zoom)
	v.cy = zoomAxis(v.cy, v.h, v.vh/v.zoom)
}

func zoomAxis(c, size, view float32) float32 {
	if size <= view {
		return size / 2
	}
	return min(max(c, view/2), size-view/2)
}

// glide makes change, a double-click's or a key's, move the view there in
// a moment, from where it shows now (shown).
func (v *tspViewer) glide(shown [3]float32, change func()) {
	change()
	v.glideFrom = shown
	v.glides++
}

// surfaceZoom draws the zoom view over pane p, when it shows a picture of
// p's.
func (a *App) surfaceZoom(c *ui.Context, k *colors, t *Tab, p *Pane) {
	v := a.tspZoom
	if v == nil || v.pane != p {
		return
	}
	closed, open := false, false
	view := ui.Column(c.Key("tsp-zoom")).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.75)).
		Gap(8).Padding(16).Focusable().FocusRing(false).Focus().Font(a.tspLook().font)
	view.Children(func() {
		stage := ui.Box(c.Key("stage")).Grow(1).FillWidth().MinHeight(0).Clip()
		r := stage.Bounds()
		if r.W != v.vw || r.H != v.vh {
			v.vw, v.vh = r.W, r.H
			if !v.fit && v.vw > 0 {
				v.clamp()
				a.zoomSettled(v)
			}
		}
		if v.vw <= 0 || v.vh <= 0 {
			return // laid out in the next frame
		}
		if v.fit {
			v.zoom, v.cx, v.cy = v.fitZoom(), v.w/2, v.h/2
		}
		// What shows: where the view is, or on its way there.
		to := [3]float32{v.zoom, v.cx, v.cy}
		shown := to
		if f := stage.Animate("glide", float32(v.glides), zoomGlide); f < float32(v.glides) {
			s := float32(v.glides) - f // from 1 at the start to 0
			for i := range shown {
				shown[i] = to[i] + (v.glideFrom[i]-to[i])*s
			}
		}
		z, cx, cy := shown[0], shown[1], shown[2]
		left, top := v.vw/2-cx*z, v.vh/2-cy*z
		stage.HandleInput(func(ev ui.InputEvent) bool {
			switch {
			case ev.Kind == ui.InputZoom:
				v.zoomAt(v.zoom*ev.Scale, ev.X, ev.Y)
			case ev.Kind == ui.InputScroll && ev.Precise:
				// A touchpad's scrolling moves the picture.
				v.moveBy(ev.DX, ev.DY)
			case ev.Kind == ui.InputScroll && ev.DY != 0:
				step := float32(zoomStep)
				if ev.DY > 0 {
					step = 1 / step
				}
				v.zoomAt(v.zoom*step, ev.X, ev.Y)
			default:
				return false
			}
			v.glideFrom = [3]float32{v.zoom, v.cx, v.cy}
			a.zoomSettled(v)
			return true
		})
		pressed := stage.Pressed()
		if pressed && !v.pressed {
			v.dragged = false
		}
		v.pressed = pressed
		if dx, dy, held := stage.Dragged(); held && (dx != 0 || dy != 0) {
			v.moveBy(-dx, -dy)
			v.dragged = true
			a.zoomSettled(v)
		}
		if v.w*z > v.vw+0.5 || v.h*z > v.vh+0.5 {
			stage.Cursor(ui.CursorGrab)
			if pressed {
				stage.Cursor(ui.CursorGrabbing)
			}
		}
		if stage.DoubleClicked() {
			if x, y, ok := stage.PointerPosition(); ok {
				v.glide(shown, func() {
					if v.fit {
						v.zoomAt(1, x, y)
					} else {
						v.fit = true
					}
				})
				a.zoomSettled(v)
			}
		} else if stage.Clicked() && !v.dragged {
			// Beside the picture: the view closes.
			if x, y, ok := stage.PointerPosition(); ok && (x < left || y < top || x > left+v.w*z || y > top+v.h*z) {
				closed = true
			}
		}
		stage.Children(func() {
			// On the panel's color, for a picture with no background of
			// its own (a Mermaid diagram's) over the dimmed pane.
			ui.Image(c.Key("picture"), v.bm).Absolute().Left(left).Top(top).Size(v.w*z, v.h*z).
				Background(k.panel).Radius(6).PassThrough()
			if s := v.sharp; s != nil && s.zoom == v.zoom && z == v.zoom {
				ui.Image(c.Key("sharp"), s.bm).Absolute().Left(left+s.x*z).Top(top+s.y*z).Size(s.w*z, s.h*z).PassThrough()
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).AlignSelf(ui.Center).Shrink(0).Children(func() {
			bw, bh := v.bm.Size()
			caption := fmt.Sprintf("%d×%d · %.0f%%", bw, bh, z*100)
			if v.name != "" {
				caption = v.name + "  " + caption
			}
			ui.Text(c, caption).FontSize(a.tspText(12)).TextColor(ui.RGB(230, 230, 230))
			if v.path != "" {
				open = textButton(c, k, "Open File").Tooltip(v.path).Clicked()
			}
		})
		// The keys, from where the picture shows.
		key := func(mods ui.Modifiers, k ui.Key) bool { return view.Shortcut(mods, k) }
		switch {
		case key(0, ui.KeyEqual), key(ui.Shift, ui.KeyEqual):
			v.glide(shown, func() { v.zoomAt(v.zoom*zoomStep, v.vw/2, v.vh/2) })
		case key(0, ui.KeyMinus):
			v.glide(shown, func() { v.zoomAt(v.zoom/zoomStep, v.vw/2, v.vh/2) })
		case key(0, ui.Key0):
			v.glide(shown, func() { v.fit = true })
		case key(0, ui.Key1):
			v.glide(shown, func() { v.zoomAt(1, v.vw/2, v.vh/2) })
		case key(0, ui.KeyLeft):
			v.moveBy(-zoomArrow, 0)
		case key(0, ui.KeyRight):
			v.moveBy(zoomArrow, 0)
		case key(0, ui.KeyUp):
			v.moveBy(0, -zoomArrow)
		case key(0, ui.KeyDown):
			v.moveBy(0, zoomArrow)
		case key(0, ui.KeyEscape):
			closed = true
		default:
			return
		}
		a.zoomSettled(v)
	})
	if open {
		path := v.path
		a.tspZoom = nil
		a.update(func() { a.openPaneFile(p, path) })
		return
	}
	if closed {
		a.tspZoom = nil
		a.surfacePressed(t, p)
	}
}

// zoomSettled has the part of the view's diagram in view drawn sharp once
// the zoom and the place stay as they are for a moment.
func (a *App) zoomSettled(v *tspViewer) {
	if v.diagram == nil {
		return
	}
	v.due++
	due := v.due
	time.AfterFunc(zoomSettle, func() {
		a.laterService(func() {
			if a.tspZoom == v && v.due == due {
				a.sharpenDiagram(v)
			}
		})
	})
}
