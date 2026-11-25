package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// dragPane drags pane p by its header to (x, y) and lets go, with
// frames between the moves as a pointer has. It returns the box the
// drag showed last, where the pane would land.
func dragPane(tt *ui.Tester, a *App, p *Pane, x, y float32) ui.Rect {
	// Apart from the last press, which a press soon after would make a
	// double click of, zooming the pane.
	tt.Advance(time.Second)
	tt.Frame()
	b := p.bounds
	sx, sy := b.X+40, b.Y+headerH/2
	tt.Press(sx, sy)
	tt.Frame()
	for i := 1; i <= 6; i++ {
		f := float32(i) / 6
		tt.Move(sx+(x-sx)*f, sy+(y-sy)*f)
		tt.Frame()
	}
	tt.Move(x, y)
	tt.Frame()
	// A frame more, for the panes to show where the drop puts them.
	tt.Frame()
	var shown ui.Rect
	if a.drag.at.frame == a.frame && a.drag.at.zone != zoneNone {
		shown = a.drag.at.rect
	}
	if shown.W > 0 && (x < shown.X || x > shown.X+shown.W || y < shown.Y || y > shown.Y+shown.H) {
		panic(fmt.Sprintf("the box %+v is not under the pointer at %v,%v", shown, x, y))
	}
	if a.drag.held != p || a.drag.size.W <= 0 {
		panic(fmt.Sprintf("no copy of the pane held: held %v, size %+v", a.drag.held == p, a.drag.size))
	}
	lastLive = map[*Pane]ui.Rect{}
	for _, t := range a.tabs {
		for _, q := range t.panes() {
			lastLive[q] = q.bounds
		}
	}
	tt.Release(x, y)
	for range 6 {
		tt.Frame()
	}
	return shown
}

// lastLive is where the last dragPane showed each pane before it let go.
var lastLive map[*Pane]ui.Rect

// othersAsShown fails unless every pane of the active tab went where the
// last drag showed it, live, before it was let go.
func othersAsShown(t *testing.T, a *App) {
	t.Helper()
	for _, p := range a.tab().panes() {
		if live, ok := lastLive[p]; ok {
			landsAsShown(t, p, live)
		}
	}
}

// landsAsShown fails unless pane p is where the drag showed it would be,
// give or take the pixels the layout rounds each split to.
func landsAsShown(t *testing.T, p *Pane, shown ui.Rect) {
	t.Helper()
	got := p.bounds
	near := func(a, b float32) bool { return a-b < 4 && b-a < 4 }
	if shown.W == 0 || !near(got.X, shown.X) || !near(got.Y, shown.Y) || !near(got.W, shown.W) || !near(got.H, shown.H) {
		t.Errorf("landed at %+v, the drag showed %+v", got, shown)
	}
}

// twoPanes is a test app with a tab of two panes side by side, laid out.
func twoPanes(t *testing.T) (*App, *ui.Tester, *Pane, *Pane) {
	a, tt := newTestApp(t)
	left := a.tab().Focus
	right := a.split(false)
	tt.Frame()
	tt.Frame()
	return a, tt, left, right
}

func TestDragPaneOntoEdge(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	b := left.bounds
	// The top of the left pane, inside the tree's own edge.
	shown := dragPane(tt, a, right, b.X+b.W/2, b.Y+b.H*0.2)
	landsAsShown(t, right, shown)
	othersAsShown(t, a)
	root := a.tab().Root
	if root.Pane != nil || !root.Vertical || root.A.Pane != right || root.B.Pane != left {
		t.Fatalf("after dropping on the top of the left pane: vertical %v, A %v, B %v", root.Vertical, root.A.Pane, root.B.Pane)
	}
	if a.tab().Focus != right {
		t.Error("the moved pane does not have the focus")
	}
}

func TestDragPaneSwaps(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	b := left.bounds
	shown := dragPane(tt, a, right, b.X+b.W/2, b.Y+b.H/2)
	landsAsShown(t, right, shown)
	othersAsShown(t, a)
	root := a.tab().Root
	if root.A.Pane != right || root.B.Pane != left || root.Vertical {
		t.Fatalf("after dropping in the middle of the left pane: A %v, B %v", root.A.Pane, root.B.Pane)
	}
}

func TestDragPaneOntoTreeEdge(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	a.tab().setFocus(right)
	below := a.split(true)
	tt.Frame()
	tt.Frame()
	tree := a.drag.tree
	if tree.W == 0 {
		t.Fatal("no bounds of the tree")
	}
	// The bottom edge of the tree, under the left pane.
	shown := dragPane(tt, a, left, tree.X+tree.W/4, tree.Y+tree.H-6)
	landsAsShown(t, left, shown)
	othersAsShown(t, a)
	root := a.tab().Root
	if !root.Vertical || root.B.Pane != left || root.Ratio < 0.6 || root.Ratio > 0.7 {
		t.Fatalf("after dropping on the bottom of the tree: vertical %v, B %v, ratio %v", root.Vertical, root.B.Pane, root.Ratio)
	}
	if top := root.A; top.Pane != nil || !top.Vertical || top.A.Pane != right || top.B.Pane != below {
		t.Fatalf("the other panes moved: %+v", top)
	}
}

func TestDragPaneOntoItself(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	b := right.bounds
	// Its middle: its sides split it (TestDragPaneOntoItsOwnSide).
	dragPane(tt, a, right, b.X+b.W/2, b.Y+b.H/2)
	root := a.tab().Root
	if root.A.Pane != left || root.B.Pane != right || root.Vertical {
		t.Fatal("a pane dropped on its middle moved")
	}
}

// Moving a pane into a smaller one beside it lands where the drag
// showed, though the pane it leaves grows into its place first.
func TestDragPaneIntoSmallerPane(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	a.tab().setFocus(left)
	below := a.split(true)
	tt.Frame()
	tt.Frame()
	for _, z := range []struct {
		name string
		fx   float32
		fy   float32
	}{{"bottom", 0.5, 0.85}, {"left", 0.12, 0.5}, {"right", 0.88, 0.5}, {"top", 0.5, 0.15}} {
		b := below.bounds
		shown := dragPane(tt, a, right, b.X+b.W*z.fx, b.Y+b.H*z.fy)
		if shown.W == 0 {
			t.Errorf("%s of the smaller pane: no box shown", z.name)
			continue
		}
		landsAsShown(t, right, shown)
		// Back to the right of the tree, for the next.
		tree := a.drag.tree
		dragPane(tt, a, right, tree.X+tree.W-6, tree.Y+tree.H/4)
	}
}

// In three columns, the first pane dragged onto either side of the others
// lands under the pointer, as the box showed.
func TestDragFirstOfThreeColumns(t *testing.T) {
	for _, c := range []struct {
		name   string
		onLast bool
		fx     float32
	}{{"right of the middle", false, 0.85}, {"left of the last", true, 0.15}, {"right of the last", true, 0.8}} {
		t.Run(c.name, func(t *testing.T) {
			a, tt, first, mid := twoPanes(t)
			last := a.split(false)
			tt.Frame()
			tt.Frame()
			target := mid
			if c.onLast {
				target = last
			}
			b := target.bounds
			shown := dragPane(tt, a, first, b.X+b.W*c.fx, b.Y+b.H/2)
			if shown.W == 0 {
				t.Fatal("no box shown")
			}
			landsAsShown(t, first, shown)
		})
	}
}

// Two drags one soon after the other are no double click, which would
// zoom the pane.
func TestTwoDragsDoNotZoom(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	a.tab().setFocus(left)
	below := a.split(true)
	tt.Frame()
	tt.Frame()
	for i := range 4 {
		b := right.bounds
		sx, sy := b.X+40, b.Y+headerH/2
		x, y := below.bounds.X+below.bounds.W/2, below.bounds.Y+below.bounds.H*0.85
		if i%2 == 1 {
			tree := a.drag.tree
			x, y = tree.X+tree.W-6, tree.Y+tree.H/2
		}
		tt.Press(sx, sy)
		tt.Frame()
		for i := 1; i <= 4; i++ {
			f := float32(i) / 4
			tt.Move(sx+(x-sx)*f, sy+(y-sy)*f)
			tt.Frame()
		}
		tt.Release(x, y)
		tt.Frame()
		tt.Frame()
	}
	if a.tab().Zoom != nil {
		t.Fatal("two drags zoomed the pane")
	}
}

func TestDragPaneOntoItsOwnSide(t *testing.T) {
	a, tt, _, right := twoPanes(t)
	b := right.bounds
	shown := dragPane(tt, a, right, b.X+b.W*0.85, b.Y+b.H/2)
	landsAsShown(t, right, shown)
	n := right.Node.Parent
	if n == nil || n.B != right.Node || n.A.Pane == nil || n.Vertical {
		t.Fatalf("no new pane left of the dragged one: %+v", n)
	}
	if got := len(a.tab().panes()); got != 3 {
		t.Fatalf("%d panes, want 3", got)
	}
	if a.tab().Focus != right {
		t.Error("the dragged pane does not have the focus")
	}
}

func TestDragPaneOntoNearSideOfNeighbour(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	b := right.bounds
	// The left pane is already on that side: it takes the left half of
	// the right pane, which shrinks to the other half.
	shown := dragPane(tt, a, left, b.X+b.W*0.15, b.Y+b.H/2)
	landsAsShown(t, left, shown)
	if got := right.bounds; got.W > b.W*0.6 || got.X < b.X+b.W/3 {
		t.Fatalf("the right pane did not give up half: %+v, was %+v", got, b)
	}
}

func TestDragPaneToAnotherTab(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	first := a.tab()
	a.newTab(os.TempDir())
	second := a.tab()
	other := second.Focus
	a.selectTab(0)
	tt.Frame()
	tt.Frame()
	track, ok := tt.Find("Tabs")
	if !ok {
		t.Fatal("no tab bar")
	}
	// Held over the second tab, the drag opens it.
	b := right.bounds
	sx, sy := b.X+40, b.Y+headerH/2
	tx, ty := track.X+track.W*0.75, track.Y+track.H/2
	tt.Press(sx, sy)
	tt.Frame()
	for i := 1; i <= 6; i++ {
		f := float32(i) / 6
		tt.Move(sx+(tx-sx)*f, sy+(ty-sy)*f)
		tt.Frame()
	}
	tt.Advance(tabSwitchDelay + 100*time.Millisecond)
	tt.Frame()
	if a.tab() != second {
		t.Fatal("holding a pane over a tab did not open it")
	}
	// Dropped on the left of the other tab's pane.
	tt.Frame()
	ob := other.bounds
	x, y := ob.X+ob.W*0.15+treeEdge, ob.Y+ob.H/2
	tt.Move(x, y-20)
	tt.Frame()
	tt.Move(x, y)
	tt.Frame()
	tt.Release(x, y)
	tt.Frame()
	tt.Frame()
	if right.Tab != second || second.Root.A.Pane != right || second.Root.B.Pane != other {
		t.Fatalf("the pane did not move to the left of the other tab's: tab %v, A %v", right.Tab == second, second.Root.A.Pane)
	}
	if first.Root.Pane != left {
		t.Error("the first tab's tree did not close up")
	}
}

// A drag given up in another tab, its pane out of sight there, ends.
func TestDragTerminalIntoEditorTab(t *testing.T) {
	a, tt, _, right := twoPanes(t)
	root := testDir(t)
	defer os.RemoveAll(root)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o644)
	ed := a.newEditorTab(root)
	editor := ed.Root.Pane
	a.selectTab(0)
	tt.Frame()
	tt.Frame()
	track, ok := tt.Find("Tabs")
	if !ok {
		t.Fatal("no tab bar")
	}
	b := right.bounds
	sx, sy := b.X+40, b.Y+headerH/2
	tx, ty := track.X+track.W*0.75, track.Y+track.H/2
	tt.Press(sx, sy)
	tt.Frame()
	for i := 1; i <= 6; i++ {
		f := float32(i) / 6
		tt.Move(sx+(tx-sx)*f, sy+(ty-sy)*f)
		tt.Frame()
	}
	tt.Advance(tabSwitchDelay + 100*time.Millisecond)
	tt.Frame()
	if a.tab() != ed {
		t.Fatal("holding a terminal over an editor tab did not open it")
	}
	tt.Frame()
	eb := editor.bounds
	x, y := eb.X+eb.W*0.15+treeEdge, eb.Y+eb.H/2
	tt.Move(x, y-20)
	tt.Frame()
	tt.Move(x, y)
	tt.Frame()
	tt.Release(x, y)
	tt.Frame()
	tt.Frame()
	if right.Tab != ed || ed.Root.A.Pane != right || ed.Root.B.Pane != editor {
		t.Fatalf("the terminal did not move to the left of the editor: tab %v", right.Tab == ed)
	}
	// With the terminal focused, a file opens in an editor beside it.
	ed.setFocus(right)
	a.openFile(ed, filepath.Join(root, "notes.txt"), false)
	tt.Frame()
	if right.Kind != paneTerminal || ed.treeFocus().Kind != paneEditor || ed.treeFocus().edit.path != filepath.Join(root, "notes.txt") {
		t.Error("opening a file with the terminal focused did not open it in an editor")
	}
	var term *savedNode
	var find func(s *savedNode)
	find = func(s *savedNode) {
		if s == nil {
			return
		}
		if s.SID != "" && s.SID == right.SID {
			term = s
		}
		find(s.A)
		find(s.B)
	}
	find(a.snapshot().Tabs[1].Root)
	if term == nil || !term.Term {
		t.Errorf("the terminal of the editor tab is not saved as one: %+v", term)
	}
}

func TestDragGivenUpInAnotherTab(t *testing.T) {
	a, tt, _, right := twoPanes(t)
	first := a.tab()
	a.newTab(os.TempDir())
	second := a.tab()
	a.selectTab(0)
	tt.Frame()
	tt.Frame()
	track, ok := tt.Find("Tabs")
	if !ok {
		t.Fatal("no tab bar")
	}
	b := right.bounds
	sx, sy := b.X+40, b.Y+headerH/2
	tx, ty := track.X+track.W*0.75, track.Y+track.H/2
	tt.Press(sx, sy)
	tt.Frame()
	for i := 1; i <= 6; i++ {
		f := float32(i) / 6
		tt.Move(sx+(tx-sx)*f, sy+(ty-sy)*f)
		tt.Frame()
	}
	tt.Advance(tabSwitchDelay + 100*time.Millisecond)
	tt.Frame()
	if a.tab() != second || a.drag.dragged != right {
		t.Fatal("holding a pane over a tab did not open it, dragging")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Release(tx, ty)
	tt.Frame()
	tt.Frame()
	if a.drag.dragged != nil {
		t.Error("the drag given up out of sight of its pane did not end")
	}
	if right.Tab != first {
		t.Error("the pane moved")
	}
}

func TestMoveLastPaneClosesItsTab(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.tab()
	p := first.Focus
	a.newTab(os.TempDir())
	second := a.tab()
	tt.Frame()
	a.movePane(p, second.Focus, zoneRight, false, nil)
	if len(a.tabs) != 1 || a.tabs[0] != second || p.Tab != second || a.tab() != second {
		t.Fatalf("%d tabs after moving the first tab's only pane", len(a.tabs))
	}
}

func TestZoneFor(t *testing.T) {
	for _, c := range []struct {
		x, y float32
		want dropZone
	}{
		{10, 50, zoneLeft}, {90, 50, zoneRight}, {50, 10, zoneTop}, {50, 90, zoneBottom},
		{50, 50, zoneCenter}, {5, 20, zoneLeft}, {20, 5, zoneTop},
	} {
		if got := zoneFor(c.x, c.y, 100, 100); got != c.want {
			t.Errorf("zone at %v,%v: %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// TestDragPaneToNewTab: a pane dropped on New Tab goes into a tab of its
// own, after its tab, still running and focused; one dropped on the room
// after the tabs does the same; the tab it leaves keeps its other pane.
func TestDragPaneToNewTab(t *testing.T) {
	a, tt, left, right := twoPanes(t)
	first := a.tab()
	plus, ok := tt.Find("New Tab")
	if !ok {
		t.Fatal("no New Tab button")
	}
	dragPane(tt, a, right, plus.X+plus.W/2, plus.Y+plus.H/2)
	if len(a.tabs) != 2 || a.tabs[0] != first || a.tabs[1] != right.Tab || a.tab() != right.Tab {
		t.Fatalf("after a drop on New Tab: %d tabs, the pane's tab at %d", len(a.tabs), slices.Index(a.tabs, right.Tab))
	}
	if right.closed || right.Tab.Root.Pane != right || right.Tab.Focus != right || right.Tab.Kind != paneTerminal {
		t.Errorf("the moved pane: closed %v, alone %v, focused %v", right.closed, right.Tab.Root.Pane == right, right.Tab.Focus == right)
	}
	if first.Root.Pane != left {
		t.Error("the first tab did not keep its other pane")
	}

	// Back with the other, then out by the room after the tabs.
	a.selectTab(0)
	tt.Frame()
	moved := a.split(false)
	tt.Frame()
	tt.Frame()
	track, _ := tt.Find("Tabs")
	dragPane(tt, a, moved, track.X+track.W+titleFree/2, track.Y+track.H/2)
	if len(a.tabs) != 3 || a.tabs[1] != moved.Tab || moved.Tab.Root.Pane != moved || a.tab() != moved.Tab {
		t.Fatalf("after a drop past the tabs: %d tabs, the pane's tab at %d", len(a.tabs), slices.Index(a.tabs, moved.Tab))
	}
}

// TestDragLonePaneToNewTab: a pane alone in its tab stays where it is
// when dropped on New Tab.
func TestDragLonePaneToNewTab(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	tt.Frame()
	p, tab := a.tab().Focus, a.tab()
	plus, _ := tt.Find("New Tab")
	dragPane(tt, a, p, plus.X+plus.W/2, plus.Y+plus.H/2)
	if len(a.tabs) != 1 || p.Tab != tab || p.closed {
		t.Errorf("%d tabs; the pane in its tab %v, closed %v", len(a.tabs), p.Tab == tab, p.closed)
	}
}

// TestDragFileToNewTab: a file's pane dropped on New Tab from a terminal
// tab goes into an editor tab whose tree opens at the file's folder, in
// place of the empty editor such a tab starts with.
func TestDragFileToNewTab(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	file := filepath.Join(root, "notes.md")
	os.WriteFile(file, []byte("# Notes\n"), 0o644)
	term := a.tab().Focus
	e := a.newEditorPane(file)
	a.splitWith(term, e, false)
	waitFor(t, tt, "the file", func() bool { return e.edit.buf != nil })
	tt.Frame()
	plus, _ := tt.Find("New Tab")
	dragPane(tt, a, e, plus.X+plus.W/2, plus.Y+plus.H/2)
	nt := e.Tab
	if len(a.tabs) != 2 || a.tabs[1] != nt || nt.Kind != paneEditor || a.tab() != nt {
		t.Fatalf("%d tabs; the file's tab at %d, an editor tab %v", len(a.tabs), slices.Index(a.tabs, nt), nt.Kind == paneEditor)
	}
	if ps := nt.panes(); len(ps) != 1 || ps[0] != e || e.closed {
		t.Errorf("the editor tab's panes: %d, the file's %v", len(ps), len(ps) == 1 && ps[0] == e)
	}
	if b := nt.Browser; b == nil || b.files.root != root {
		t.Errorf("the tree opens at %q, want %q", b.files.root, root)
	}
	if a.tabs[0].Root.Pane != term {
		t.Error("the terminal tab did not keep its terminal")
	}
}

// TestDragLoneTerminalOutOfEditorTab: a terminal alone in an editor tab,
// dropped on New Tab, goes into a terminal tab of its own; the editor tab
// stays, its tree with it, an empty editor in the terminal's place.
func TestDragLoneTerminalOutOfEditorTab(t *testing.T) {
	a, tt := newTestApp(t)
	root := testDir(t)
	defer os.RemoveAll(root)
	term := a.tab().Focus
	a.newTab(os.TempDir())
	ed := a.newEditorTab(root)
	a.swapPanes(term, ed.Root.Pane)
	tt.Frame()
	tt.Frame()
	if term.Tab != ed || ed.Root.Pane != term {
		t.Fatal("the terminal is not alone in the editor tab")
	}
	plus, _ := tt.Find("New Tab")
	dragPane(tt, a, term, plus.X+plus.W/2, plus.Y+plus.H/2)
	if !slices.Contains(a.tabs, ed) || term.Tab == ed || term.Tab.Kind != paneTerminal || term.closed {
		t.Fatalf("editor tab kept %v; the terminal in it %v, in a terminal tab %v", slices.Contains(a.tabs, ed), term.Tab == ed, term.Tab.Kind == paneTerminal)
	}
	if e := ed.Root.Pane; e == nil || e.Kind != paneEditor || e.edit.path != "" || ed.Focus != e || ed.Browser == nil {
		t.Errorf("the editor tab's pane: %+v", e)
	}
	if i := slices.Index(a.tabs, term.Tab); i != slices.Index(a.tabs, ed)+1 || a.tab() != term.Tab {
		t.Errorf("the terminal's tab at %d, the editor tab at %d", i, slices.Index(a.tabs, ed))
	}
}
