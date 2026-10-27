package main

import "testing"

// An anchored overlay goes to the side asked for, but for the other when
// the asked side has less room than it needs and the other has more: its
// height last frame, and some 200 DIPs at least, as an overlay capped to
// a sliver of room measures that sliver and would stay in it.
func TestTSPOverlayDown(t *testing.T) {
	for _, c := range []struct {
		above            bool
		below, aboveRoom float32
		h                float32
		want             bool
		situation        string
	}{
		{false, 400, 300, 120, true, "room under it"},
		{false, 40, 500, 0, false, "a field at the bottom, first frame"},
		{false, 40, 500, 40, false, "a field at the bottom, capped to the room under it"},
		{false, 150, 100, 0, true, "little room either side, more under it"},
		{true, 500, 300, 250, false, "above, as asked, with room"},
		{true, 500, 60, 0, true, "above asked, a node at the top"},
		{false, 260, 600, 320, false, "taller than the room under it"},
	} {
		if got := tspOverlayDown(c.above, c.below, c.aboveRoom, c.h); got != c.want {
			t.Errorf("%s: down %v, want %v", c.situation, got, c.want)
		}
	}
}
