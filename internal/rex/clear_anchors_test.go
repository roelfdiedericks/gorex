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
