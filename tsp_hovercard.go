package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A node's hovercard is a richer tooltip, as Tern draws it: a floating
// card of its node tree, which opens when the pointer rests on the node,
// below it (above where there is no room), and stays open while the
// pointer is over the node or the card. It closes on leaving both, after
// a short grace, or on a press. While it shows, it takes the place of the
// node's title tooltip. A card that is one md is a preview of text: wider,
// and the pointer passes through it.

const (
	tspCardDelay = 350 * time.Millisecond
	tspCardGrace = 150 * time.Millisecond
)

// tspHoverCard is the hover card showing, or about to: the node it is
// for, by surface image and node id; when the pointer came onto the node,
// and when it left the node and the card; and the card's tree, parsed
// from the prop of node at rev.
type tspHoverCard struct {
	key         string
	since, left time.Time
	open        bool
	overCard    bool
	node        *rex.TSPNode
	rev         uint64
	tree        *rex.TSPNode
}

// tspHoverCardFor shows the hover card of node n, drawn as el, when it is
// due, and reports whether it shows.
func (a *App) tspHoverCardFor(c *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, el ui.Element) bool {
	prop, ok := n.Props["hovercard"].(map[string]any)
	if !ok {
		return false
	}
	h := &a.tspCard
	key := fmt.Sprintf("%d/%s", sf.Image, n.ID)
	now := c.Now()
	switch {
	case el.Pressed():
		// A press closes it, and it waits for the pointer to come again.
		*h = tspHoverCard{key: key, since: now.Add(time.Hour)}
		return false
	case el.Hovered():
		if h.key != key {
			*h = tspHoverCard{key: key, since: now}
		}
		h.left = time.Time{}
		if !h.open {
			if wait := tspCardDelay - now.Sub(h.since); wait > 0 {
				if wait < time.Minute {
					c.After(wait)
				}
				return false
			}
			h.open = true
		}
	case h.key != key:
		return false
	case !h.open:
		*h = tspHoverCard{}
		return false
	case h.overCard:
		h.left = time.Time{}
	default:
		if h.left.IsZero() {
			h.left = now
		}
		if wait := tspCardGrace - now.Sub(h.left); wait > 0 {
			c.After(wait)
		} else {
			*h = tspHoverCard{}
			return false
		}
	}
	if h.tree == nil || h.node != n || h.rev != n.Rev {
		h.tree, h.node, h.rev = tspPropTree(prop, "card"), n, n.Rev
	}
	if h.tree == nil {
		return false
	}
	text := h.tree.Kind == "md"
	pinned := a.tspPinned
	ui.Overlay(c, func() {
		card := ui.Column(c.Key("tsp-hovercard")).AttachTo(el, ui.AnchorBottomLeft, ui.AnchorTopLeft).MarginY(6).
			Gap(a.tspGap(8)).Padding(10, 12).Radius(10).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 8, 24, 0, k.shadowFocused).Font(a.tspLook().font)
		if text {
			card.Width(640).PassThrough()
		} else {
			card.MaxWidth(640)
		}
		card.Children(func() {
			// Drawn as in the transcript, not as the dock's controls.
			a.tspPinned = false
			a.tspNode(c, k, t, p, sf, h.tree)
			a.tspPinned = pinned
		})
		h.overCard = !text && card.Hovered()
	})
	return true
}

// tspPropTree makes a node of a prop's node tree ({k, p?, c?}), whose
// nodes have no ids: each is given one by its place under id.
func tspPropTree(v any, id string) *rex.TSPNode {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	n := &rex.TSPNode{ID: id}
	n.Kind, _ = m["k"].(string)
	n.Props, _ = m["p"].(map[string]any)
	cs, _ := m["c"].([]any)
	for i, ch := range cs {
		if cn := tspPropTree(ch, fmt.Sprintf("%s.%d", id, i)); cn != nil {
			n.Children = append(n.Children, cn)
		}
	}
	return n
}
