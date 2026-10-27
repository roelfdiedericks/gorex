package rex

import (
	"fmt"
	"maps"
	"strings"
	"testing"
)

// Erasing the scrollback (clear's ED 3) leaves the terminal's placements
// of anchors there, moved to the screen's top, where their surfaces
// would draw over the text: the session deletes a kept surface's. One
// whose end marker is on the screen keeps it, its anchor deleted. A live
// surface's anchor stays, at the top, and so does an anchor on the
// screen, where it is.
func TestEraseScrollbackDropsAnchors(t *testing.T) {
	kept, live, shown, ends := uint32(TSPImageFirst), uint32(TSPImageFirst+1), uint32(TSPImageFirst+2), uint32(TSPImageFirst+3)
	s := &session{vt: newAnchorTerm(t)}
	s.tsp.kept = []*Surface{{ID: "k", Image: kept, Closed: true}, {ID: "s", Image: shown, Closed: true}, {ID: "e", Image: ends, Closed: true, Rows: 21}}
	var b strings.Builder
	b.WriteString(anchor(kept) + "\r\n" + anchor(live) + "\r\n" + anchor(ends) + "\r\n")
	for i := range 20 {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	b.WriteString(endMarker(ends) + "\r\n" + anchor(shown) + "\r\nx")
	s.vt.Feed([]byte(b.String()))
	before, beforeEnds := anchorRows(s.vt), endRows(s.vt)
	if before[kept] >= 0 || before[live] >= 0 || before[ends] >= 0 || beforeEnds[ends] < 0 || before[shown] < 0 {
		t.Fatalf("anchors placed at %v, ends at %v", before, beforeEnds)
	}
	out := []byte("\x1b[3J")
	erased := s.scrollbackAnchors(out)
	s.vt.Feed(out)
	s.dropAnchors(erased)
	got := anchorRows(s.vt)
	if want := map[uint32]int{live: 0, shown: before[shown]}; !maps.Equal(got, want) {
		t.Errorf("anchors at %v, want %v", got, want)
	}
	if got := endRows(s.vt); !maps.Equal(got, beforeEnds) {
		t.Errorf("ends at %v, want %v", got, beforeEnds)
	}
	// The surface whose anchor went is where it was, up from its end.
	if rows, _ := s.anchorRows(); rows[ends] != beforeEnds[ends]-20 {
		t.Errorf("the surface without its anchor at %d, want %d", rows[ends], beforeEnds[ends]-20)
	}
	// Output that erases nothing leaves the anchors.
	if ids := s.scrollbackAnchors([]byte("\x1b[2Jtext")); ids != nil {
		t.Errorf("anchors to drop without ED 3: %q", ids)
	}
}

// A surface's end marker goes on the last row of its content once it
// has its rows, and again when they change, with the cursor put back;
// not on a row under the cursor.
func TestPlaceEnds(t *testing.T) {
	img := uint32(TSPImageFirst)
	s := &session{vt: newAnchorTerm(t), rows: 6}
	s.tsp.kept = []*Surface{{ID: "k", Image: img, Closed: true, Rows: 3}}
	s.vt.Feed([]byte("$ cmd\r\n" + anchor(img) + "a\r\nb\r\nc\r\n$ "))
	s.placeEnds()
	start := anchorRows(s.vt)[img]
	if end, ok := endRows(s.vt)[img]; !ok || end != start+2 {
		t.Errorf("end at %d (%v), the anchor at %d", end, ok, start)
	}
	if col, row := s.vt.Cursor(); col != 2 || row != start+3 {
		t.Errorf("cursor at %d,%d, want 2,%d", col, row, start+3)
	}
	s.tsp.kept[0].Rows = 2
	s.placeEnds()
	if end := endRows(s.vt)[img]; end != start+1 {
		t.Errorf("after the rows changed, end at %d, want %d", end, start+1)
	}
	s.tsp.kept[0].Rows = 9
	s.placeEnds()
	if end := endRows(s.vt)[img]; end != start+1 {
		t.Errorf("an end under the cursor was placed: %d", end)
	}
}

// What a program prints under its live inline surface, where it is to
// print nothing, is erased: a window would show it in the strip beside
// the surface. What came before the surface, its anchor and end marker,
// and the cursor stay. With a dock, the surface takes the screen down to
// its end; without, the rows after it are the program's.
func TestEraseUnderLiveSurface(t *testing.T) {
	img := uint32(TSPImageFirst)
	for _, docked := range []bool{false, true} {
		s := &session{vt: newAnchorTerm(t), rows: 6}
		sf := &Surface{ID: "s", Image: img, Rows: 3, Root: &TSPNode{ID: "s", Kind: "col"}}
		if docked {
			sf.Root.Children = []*TSPNode{{ID: "dock", Kind: "col"}}
		}
		s.tsp.surfaces = map[string]*Surface{"s": sf}
		s.tsp.order = []string{"s"}
		// Rows: 0 the command, 1 the anchor, 2, 3 the end marker, 4 the
		// cursor, 5 below it.
		s.vt.Feed([]byte("$ cmd\r\n" + anchor(img) + "\r\n\r\n" + endMarker(img) + "\r\n"))
		out := []byte("\x1b[2A\r\x1b[14Cstray\x1b[1B\r\x1b[14Cstray\x1b[2B\r\x1b[14Cbelow\x1b[1A\r")
		if !printable(out) {
			t.Fatal("the stray text is not printable")
		}
		s.vt.Feed(out)
		s.eraseUnder()
		text := s.vt.Text()
		if strings.Contains(text, "stray") || !strings.Contains(text, "$ cmd") {
			t.Errorf("docked %v: text %q", docked, text)
		}
		if got := strings.Contains(text, "below"); got == docked {
			t.Errorf("docked %v: the row after the surface kept: %v", docked, got)
		}
		if a, e := anchorRows(s.vt), endRows(s.vt); a[img] != 1 || e[img] != 3 {
			t.Errorf("docked %v: anchor at %v, end at %v", docked, a, e)
		}
		if col, row := s.vt.Cursor(); col != 0 || row != 4 {
			t.Errorf("docked %v: cursor at %d,%d, want 0,4", docked, col, row)
		}
	}
}

// Output with no text, only escape sequences and controls, is not
// printable: it leaves the rows under a surface as they are.
func TestPrintable(t *testing.T) {
	for _, c := range []struct {
		out  string
		want bool
	}{
		{"\x1b[?25l\x1b[2;5H\x1b[38;2;10;20;30m\r\n\a", false},
		{"\x1b]0;a title\a\x1b]9;4;1;50\x1b\\", false},
		{"\x1bP+q544e\x1b\\\x1b7\x1b8", false},
		{"\x1b]0;title\ax", true},
		{"\x1b[1mbold", true},
		{"é", true},
	} {
		if got := printable([]byte(c.out)); got != c.want {
			t.Errorf("printable(%q) = %v, want %v", c.out, got, c.want)
		}
	}
}
