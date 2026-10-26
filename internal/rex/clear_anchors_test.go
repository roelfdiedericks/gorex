package rex

import (
	"fmt"
	"maps"
	"strings"
	"testing"
)

// Erasing the scrollback (clear's ED 3) leaves the terminal's placements
// of anchors there, moved to the screen's top, where their surfaces
// would draw over the text: the session deletes a kept surface's. A
// live surface's anchor stays, at the top, and so does an anchor on the
// screen, where it is.
func TestEraseScrollbackDropsAnchors(t *testing.T) {
	kept, live, shown := uint32(TSPImageFirst), uint32(TSPImageFirst+1), uint32(TSPImageFirst+2)
	s := &session{vt: newAnchorTerm(t)}
	s.tsp.kept = []*Surface{{ID: "k", Image: kept, Closed: true}, {ID: "s", Image: shown, Closed: true}}
	var b strings.Builder
	b.WriteString(anchor(kept) + "\r\n" + anchor(live) + "\r\n")
	for i := range 20 {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	b.WriteString(anchor(shown) + "\r\nx")
	s.vt.Feed([]byte(b.String()))
	before := anchorRows(s.vt)
	if before[kept] >= 0 || before[live] >= 0 || before[shown] < 0 {
		t.Fatalf("anchors placed at %v", before)
	}
	out := []byte("\x1b[3J")
	erased := s.scrollbackAnchors(out)
	s.vt.Feed(out)
	s.dropAnchors(erased)
	got := anchorRows(s.vt)
	if want := map[uint32]int{live: 0, shown: before[shown]}; !maps.Equal(got, want) {
		t.Errorf("anchors at %v, want %v", got, want)
	}
	// Output that erases nothing leaves the anchors.
	if ids := s.scrollbackAnchors([]byte("\x1b[2Jtext")); ids != nil {
		t.Errorf("anchors to drop without ED 3: %v", ids)
	}
}
