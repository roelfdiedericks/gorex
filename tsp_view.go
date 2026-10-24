package main

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"gorex/internal/rex"
)

// A surface of the Tern Surface Protocol (TSP.md) draws over the pane's
// terminal: the program's tree of nodes, laid out in MyGo's UI. A
// screen surface fills the pane while it is open. An inline surface
// draws at the rows its anchor takes in the terminal (internal/rex
// tsp.go), which scroll with the output: as tall as its content, which
// the window measures and tells the server, whose anchor then takes
// that many rows; closed with keep, it stays drawn there, in the rows it
// had. The kinds drawn are those the server's hello reply names
// (internal/rex tspKinds); a node of another kind draws nothing, as the
// program was told, and falls back itself. A click on a node that has a
// click action goes to the program as an action event.

// surfaces draws the pane's surfaces over its terminal, in the box the
// terminal's view fills.
func (a *App) surfaces(c *ui.Context, k *colors, t *Tab, p *Pane) {
	var placements []terminal.AppPlacement
	for i := range p.surfaces {
		sf := &p.surfaces[i]
		switch {
		case sf.Mode == "screen":
			a.screenSurface(c, k, t, p, sf)
		case sf.Image != 0:
			if placements == nil {
				placements = p.term.AppPlacements()
			}
			for _, pl := range placements {
				if pl.Image == sf.Image {
					a.inlineSurface(c, k, t, p, sf, pl)
					break
				}
			}
		}
	}
}

// screenSurface draws a screen surface over a pane's terminal, filling
// the box it is in.
func (a *App) screenSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface) {
	s := ui.Scroll(c.Key("tsp-"+sf.ID)).Absolute().Fill().Background(k.bgBottom).Padding(12, 16)
	s.Children(func() {
		if sf.Root != nil {
			a.tspNode(c, k, p, sf, sf.Root)
		}
	})
	if s.Pressed() && t.Focus != p {
		t.setFocus(p)
		a.changed()
	}
}

// inlineSurface draws an inline surface at its anchor's rows. Live, it
// is as tall as its content, and its height in rows, as the last frame
// laid it out, goes to the server when it differs from the anchor's;
// kept, it is cut to the anchor's rows. Scrolled out of the viewport it
// draws nothing.
func (a *App) inlineSurface(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, pl terminal.AppPlacement) {
	if !pl.Visible && pl.Row+pl.Rows <= 0 {
		return
	}
	box := ui.Box(c.Key("tsp-"+sf.ID)).Absolute().Left(pl.Rect.X).Top(pl.Rect.Y).Width(pl.Rect.W).Background(k.bgBottom).Padding(4, 8)
	if sf.Closed {
		box.Height(pl.Rect.H).Clip()
	}
	box.Children(func() {
		if sf.Root != nil {
			a.tspNode(c, k, p, sf, sf.Root)
		}
	})
	if box.Pressed() && t.Focus != p {
		t.setFocus(p)
		a.changed()
	}
	if sf.Closed || pl.Rows <= 0 {
		return
	}
	cell := pl.Rect.H / float32(pl.Rows)
	if h := box.Bounds().H; h > 0 && cell > 0 {
		rows := int(math.Ceil(float64(h/cell - 0.01)))
		if rows > 0 && rows != sf.Rows && rows != p.surfaceRows[sf.ID] {
			if p.surfaceRows == nil {
				p.surfaceRows = map[string]int{}
			}
			p.surfaceRows[sf.ID] = rows
			stream, id := p.stream, sf.ID
			go stream.TSPSize(id, rows)
		}
	}
}

// tspNode draws a node and its children.
func (a *App) tspNode(c *ui.Context, k *colors, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	key := c.Key("n-" + n.ID)
	text, _ := n.Props["text"].(string)
	var el ui.Element
	switch n.Kind {
	case "col":
		col := ui.Column(key).Gap(8).FillWidth()
		col.Children(func() { a.tspChildren(c, k, p, sf, n) })
		el = col
	case "row":
		row := ui.Row(key).Gap(8).AlignItems(ui.Center).FillWidth()
		row.Children(func() { a.tspChildren(c, k, p, sf, n) })
		el = row
	case "card":
		head, _ := n.Props["head"].(string)
		status, _ := n.Props["status"].(string)
		card := ui.Column(key).Radius(cardR).Background(k.card).Border(1, k.cardBorder).Padding(12, 14).Gap(10).FillWidth()
		card.Children(func() {
			h := ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth()
			h.Children(func() {
				ui.Text(c, head).FontSize(14).Bold().TextColor(k.text).Grow(1)
				if status != "" {
					a.tspStatus(c, k, status)
				}
			})
			a.tspChildren(c, k, p, sf, n)
		})
		el = card
	case "text":
		el = ui.Text(c, text).FontSize(13).TextColor(k.text)
	case "md":
		box := ui.Box(key).FillWidth()
		box.Children(func() {
			markdown.View(c, text, &markdown.Options{Theme: &markdown.Theme{Text: k.text, Muted: k.textMuted, Link: k.busy, CodeFont: a.editorFont().Family}})
		})
		el = box
	case "badge":
		b := ui.Box(key).Radius(6).Background(k.hover).Padding(3, 10)
		b.Children(func() {
			ui.Text(c, text).FontSize(12).TextColor(k.text)
		})
		el = b
	default:
		return
	}
	// A click action, where the node has one.
	if actions, ok := n.Props["actions"].(map[string]any); ok {
		if act, ok := actions["click"].(string); ok && act != "" {
			if el.Clicked() {
				stream := p.stream
				ev := map[string]any{"ev": "action", "sf": sf.ID, "id": n.ID, "act": act}
				go func() {
					if err := stream.TSPEvent(ev); err != nil {
						a.post(func() {
							a.toast(ui.Toast{Title: "The program did not get the click", Description: err.Error(), Type: "error"})
						})
					}
				}()
			}
		}
	}
}

func (a *App) tspChildren(c *ui.Context, k *colors, p *Pane, sf *rex.Surface, n *rex.TSPNode) {
	for _, ch := range n.Children {
		a.tspNode(c, k, p, sf, ch)
	}
}

// tspStatus draws a card's status chip.
func (a *App) tspStatus(c *ui.Context, k *colors, status string) {
	color := k.textMuted
	switch status {
	case "running":
		color = k.busy
	case "done":
		color = k.busy
	case "error", "failed":
		color = k.attention
	}
	chip := ui.Row(c).Gap(5).AlignItems(ui.Center).Radius(6).Background(k.hover).Padding(2, 8)
	chip.Children(func() {
		if status == "running" {
			activityDot(c, color, 8)
		}
		ui.Text(c, fmt.Sprint(status)).FontSize(11).TextColor(color)
	})
}
