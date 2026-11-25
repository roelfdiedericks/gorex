package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// TestTrayActs: a card's click peeks at it, or closes its peek when it
// shows, and Enter opens it; a card with nothing to peek at only opens,
// and the card of what finished only peeks.
func TestTrayActs(t *testing.T) {
	for _, c := range []struct {
		ref, peekRef, item string
		look               bool
		peek, open         string
	}{
		{"bg_40", "", "job", true, "tray.peek=bg_40", "tray.open=bg_40"},
		{"bg_40", "bg_40", "job", true, "tray.peek=", "tray.open=bg_40"},
		{"bg_40", "a1", "job", true, "tray.peek=bg_40", "tray.open=bg_40"},
		{"a1", "", "agent", false, "", "tray.open=a1"},
		{"done", "", "done", true, "tray.peek=done", ""},
		{"", "", "job", true, "", ""},
	} {
		peek, open := trayActs(c.ref, c.peekRef, c.item, c.look)
		if peek != c.peek || open != c.open {
			t.Errorf("%+v: peek %q open %q", c, peek, open)
		}
	}
}

// TestTray: omp's tray draws the main agent's strip, a card of each job
// with its name, id and last line, and the peek of the one peeked at; a
// tray omp hides draws nothing.
func TestTray(t *testing.T) {
	a, _ := newTestApp(t)
	a.eff = builtin
	p := a.tab().Focus
	sf := &rex.Surface{ID: "s", Image: 1}
	text := func(id, role, s string) *rex.TSPNode {
		return &rex.TSPNode{ID: id, Kind: "text", Props: map[string]any{"role": role, "text": s, "wrap": "none"}}
	}
	card := &rex.TSPNode{ID: "c1", Kind: "col", Props: map[string]any{"role": "omp.tray.card", "item": "job", "ref": "bg_40", "status": "running"},
		Children: []*rex.TSPNode{
			{ID: "h1", Kind: "row", Props: map[string]any{"role": "omp.tray.head"}, Children: []*rex.TSPNode{
				{ID: "d1", Kind: "text", Props: map[string]any{"role": "omp.tray.dot", "spans": []any{map[string]any{"t": "●", "s": "warning", "fx": "pulse"}}}},
				text("n1", "omp.tray.name", "Building the site"),
				text("i1", "omp.tray.id", "bg_40"),
			}},
			text("o1", "omp.tray.out", "windows/amd64"),
		}}
	peek := &rex.TSPNode{ID: "pk", Kind: "col", Props: map[string]any{"role": "omp.tray.peek", "ref": "bg_40"},
		Children: []*rex.TSPNode{text("pm", "omp.tray.peek.meta", "make website")}}
	tray := &rex.TSPNode{ID: "tr", Kind: "col", Props: map[string]any{"role": "omp.tray", "main": "waiting", "peek": "bg_40"},
		Children: []*rex.TSPNode{card, peek}}
	draw := func(n *rex.TSPNode) *ui.Tester {
		tt := ui.NewTester(func(c *ui.Context) {
			a.tspPinned = true
			a.tspNode(c, a.k, a.tab(), p, sf, n)
			a.tspPinned = false
		}, 900, 300)
		tt.Frame()
		return tt
	}
	tt := draw(tray)
	for _, want := range []string{"M", "A", "I", "N", "Building the site", "bg_40", "windows/amd64", "make website"} {
		if !tt.HasText(want) {
			t.Errorf("the tray lacks %q: %q", want, tt.Texts())
		}
	}
	tray.Props["hidden"] = true
	if tt := draw(tray); tt.HasText("Building the site") {
		t.Error("a hidden tray was drawn")
	}
}

// trayCards are n cards of subagents as omp sends them, named Agent i,
// each doing Doing i, the first running, and the card of 33 finished.
func trayCards(n int) []*rex.TSPNode {
	var cards []*rex.TSPNode
	for i := range n {
		id := fmt.Sprint(i)
		status := "done"
		if i == 0 {
			status = "running"
		}
		cards = append(cards, &rex.TSPNode{ID: "c" + id, Kind: "col", Props: map[string]any{"role": "omp.tray.card", "item": "agent", "ref": "a" + id, "status": status, "tone": "success"},
			Children: []*rex.TSPNode{
				{ID: "d" + id, Kind: "text", Props: map[string]any{"role": "omp.tray.dot", "spans": []any{map[string]any{"t": "●", "s": "success"}}}},
				{ID: "n" + id, Kind: "text", Props: map[string]any{"role": "omp.tray.name", "text": "Agent " + id, "wrap": "none"}},
				{ID: "g" + id, Kind: "text", Props: map[string]any{"role": "omp.tray.doing", "text": "Doing " + id, "wrap": "none"}},
			}})
	}
	return append(cards, &rex.TSPNode{ID: "done", Kind: "col", Props: map[string]any{"role": "omp.tray.card", "item": "done", "ref": "done"},
		Children: []*rex.TSPNode{{ID: "dn", Kind: "text", Props: map[string]any{"role": "omp.tray.name", "text": "33 done", "wrap": "none"}}}})
}

// TestTrayRowAndDots: many cards make one row, side by side, however many
// (it scrolls sideways); a quiet tray shows only its dots and how many
// finished, and a click on the pill shows the cards.
func TestTrayRowAndDots(t *testing.T) {
	a, _ := newTestApp(t)
	a.eff = builtin
	p := a.tab().Focus
	sf := &rex.Surface{ID: "s", Image: 1}
	tray := &rex.TSPNode{ID: "tr", Kind: "col", Props: map[string]any{"role": "omp.tray"}, Children: trayCards(15)}
	tt := ui.NewTester(func(c *ui.Context) {
		a.tspPinned = true
		a.tspNode(c, a.k, a.tab(), p, sf, tray)
		a.tspPinned = false
	}, 800, 400)
	tt.Frame()
	// Newest first, as Tern's: omp sends the oldest first.
	first, ok := tt.Find("Doing 14")
	if !ok {
		t.Fatalf("no cards: %q", tt.Texts())
	}
	if last, ok := tt.Find("Doing 0"); !ok || last.Y != first.Y || last.X <= first.X {
		t.Errorf("the cards are not one row, newest first: first %+v, last %+v %v", first, last, ok)
	}
	tray.Props["quiet"] = true
	// The row shrinks away first (trayRowMove), then its dots show.
	tt.Frame()
	if !tt.HasText("Doing 0") {
		t.Error("the row went at once, without shrinking away")
	}
	time.Sleep(trayRowMove + 50*time.Millisecond)
	tt.Advance(time.Second)
	tt.Frame()
	if tt.HasText("Doing 0") || !tt.HasText("+40") {
		t.Fatalf("a quiet tray: %q", tt.Texts())
	}
	if err := tt.Click("Show the cards"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Doing 0") {
		t.Errorf("a click on the pill did not show the cards: %q", tt.Texts())
	}
}

// TestTrayInDock: in a dock, a quiet tray's dots go in the status row,
// which omp sends empty, and only there; the pointer over a dot shows its
// card, and a click on it shows the row. While the composer views a
// subagent, its card has an eye and MAIN goes back to the main session.
func TestTrayInDock(t *testing.T) {
	a, _ := newTestApp(t)
	a.eff = builtin
	p := a.tab().Focus
	sf := &rex.Surface{ID: "s", Image: 1}
	tray := &rex.TSPNode{ID: "tr", Kind: "col", Props: map[string]any{"role": "omp.tray", "quiet": true}, Children: trayCards(4)}
	status := &rex.TSPNode{ID: "st", Kind: "row", Props: map[string]any{"role": "omp.hud.activity"}}
	dock := &rex.TSPNode{ID: "dock", Kind: "col", Children: []*rex.TSPNode{status, tray}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Padding(200, 20, 20, 20).Children(func() {
			a.dockNodes(c, a.k, a.tab(), p, sf, dock)
		})
	}, 800, 500)
	tt.Frame()
	count := 0
	for _, s := range tt.Texts() {
		if s == "+33" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the dots were drawn %d times: %q", count, tt.Texts())
	}
	pip, ok := tt.Find("Agent 2")
	if !ok {
		t.Fatalf("no dot of Agent 2: %q", tt.Texts())
	}
	tt.Move(pip.X+pip.W/2, pip.Y+pip.H/2)
	tt.Frame()
	if !tt.HasText("Doing 2") || tt.HasText("Doing 1") {
		t.Errorf("the pointer over a dot did not show its card alone: %q", tt.Texts())
	}
	if err := tt.Click("Agent 2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Doing 1") || p.trayOf(sf.Image).sel != "a2" {
		t.Errorf("a click on a dot did not show the row, that card chosen: %q", tt.Texts())
	}
	if _, ok := tt.Find("Back to the main session"); ok {
		t.Error("MAIN goes back on the main session")
	}
	dock.Children = append(dock.Children, &rex.TSPNode{ID: "fo", Kind: "row", Props: map[string]any{"role": "omp.composer.focus"}, Children: []*rex.TSPNode{
		{ID: "ag", Kind: "text", Props: map[string]any{"role": "omp.composer.agent", "text": "Agent 1"}},
		{ID: "ex", Kind: "row", Props: map[string]any{"role": "omp.composer.exit", "actions": map[string]any{"click": "focus:Main"}},
			Children: []*rex.TSPNode{{ID: "exl", Kind: "text", Props: map[string]any{"text": "main"}}}},
	}})
	tt.Frame()
	if _, ok := tt.Find("Viewing"); !ok {
		t.Errorf("the card viewed has no eye: %q", tt.Texts())
	}
	if _, ok := tt.Find("Back to the main session"); !ok {
		t.Errorf("MAIN does not go back while a subagent shows: %q", tt.Texts())
	}
}

// TestTrayDotCards: the quiet tray has dots of the 8 newest cards, newest
// first, and counts the rest, as Tern does: 41 subagents are 8 dots and
// +33. One still running keeps a dot however old, in place of the oldest
// finished, and omp's card of what finished adds its count.
func TestTrayDotCards(t *testing.T) {
	cards := trayCards(41)[:41]
	cards[0].Props["status"] = "done"
	dots, more := trayDotCards(cards)
	if len(dots) != 8 || more != 33 || trayName(dots[0]) != "Agent 40" || trayName(dots[7]) != "Agent 33" {
		t.Fatalf("%d dots from %q to %q, +%d", len(dots), trayName(dots[0]), trayName(dots[len(dots)-1]), more)
	}
	cards[5].Props["status"] = "running"
	dots, more = trayDotCards(append(cards, trayCards(0)...))
	if len(dots) != 8 || more != 66 || trayName(dots[7]) != "Agent 5" || trayName(dots[6]) != "Agent 34" {
		t.Errorf("with Agent 5 running and 33 done: %d dots, the last %q, +%d", len(dots), trayName(dots[7]), more)
	}
}

// TestTrayOnComposer: while something runs, the tray's row is drawn once,
// as a tab on top of the composer's card, and a row wider than the tab
// scrolls (its state survives the frames, as overlays' does not).
func TestTrayOnComposer(t *testing.T) {
	a, _ := newTestApp(t)
	a.eff = builtin
	p := a.tab().Focus
	sf := &rex.Surface{ID: "s", Image: 1}
	tray := &rex.TSPNode{ID: "tr", Kind: "col", Props: map[string]any{"role": "omp.tray", "main": "working"}, Children: trayCards(8)[:8]}
	editor := &rex.TSPNode{ID: "ed", Kind: "col", Props: map[string]any{"role": "omp.editor"}, Children: []*rex.TSPNode{
		{ID: "ph", Kind: "text", Props: map[string]any{"text": "What are we cooking?"}},
	}}
	dock := &rex.TSPNode{ID: "dock", Kind: "col", Children: []*rex.TSPNode{tray, editor}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().MaxWidth(760).Padding(200, 0, 0, 0).Children(func() {
			a.dockNodes(c, a.k, a.tab(), p, sf, dock)
		})
	}, 800, 500)
	tt.Frame()
	tt.Advance(time.Second) // the row grown
	for range 3 {
		tt.Frame()
	}
	count := 0
	for _, s := range tt.Texts() {
		if s == "Doing 1" {
			count++
		}
	}
	card, _ := tt.Find("Doing 1")
	field, ok := tt.Find("What are we cooking?")
	if count != 1 || !ok || card.Y >= field.Y {
		t.Errorf("the tray drawn %d times, card at %v, the composer's text at %v", count, card.Y, field.Y)
	}
	if p.trayOf(sf.Image).scroll.MaxX <= 0 {
		t.Error("eight cards in 760 DIPs do not scroll")
	}
}

// TestTrayEscape: as Tern's, the first Escape closes a card's peek, the
// next folds a row opened from the dots, and after that Escape is omp's.
func TestTrayEscape(t *testing.T) {
	a, _ := newTestApp(t)
	p := a.tab().Focus
	sf := &rex.Surface{ID: "s", Image: 1}
	if a.trayEscape(p, sf) {
		t.Fatal("a pane without a tray took Escape")
	}
	st := p.trayOf(sf.Image)
	st.open, st.sel, st.peek, st.id = true, "a2", "a2", "tr"
	if !a.trayEscape(p, sf) || st.peek != "" || !st.open {
		t.Fatalf("the first Escape: peek %q, open %v", st.peek, st.open)
	}
	if !a.trayEscape(p, sf) || st.open || st.sel != "" {
		t.Fatalf("the second Escape: open %v, chosen %q", st.open, st.sel)
	}
	if a.trayEscape(p, sf) {
		t.Error("the third Escape did not go to omp")
	}
}
