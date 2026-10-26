package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// A program's output keeps its colors and styles as runs: the palette's
// 16, the 256 and true color, bold, italic, underline, reverse; a reset
// goes back to the base style; other escapes are dropped.
func TestANSISpans(t *testing.T) {
	var pal [16]ui.Color
	for i := range pal {
		pal[i] = ui.Color{R: uint8(i), A: 255}
	}
	fg := ui.Color{R: 200, G: 200, B: 200, A: 255}
	bg := ui.Color{R: 10, G: 10, B: 10, A: 255}
	base := ui.Span{Size: 12, Color: fg}
	got := ansiSpans("a\x1b[1;31mred\x1b[0m b\x1b[38;5;196mx\x1b[38;2;1;2;3my\x1b[3;4mz\x1b[7mr\x1b[0m\x1b]8;;http://x\x07link\x1b]8;;\x1b\\\x1b[2Kend\r", base, &pal, bg)
	want := []ui.Span{
		{Text: "a", Size: 12, Color: fg},
		{Text: "red", Size: 12, Color: pal[1], Weight: 700},
		{Text: " b", Size: 12, Color: fg},
		{Text: "x", Size: 12, Color: ui.Color{R: 255, A: 255}},
		{Text: "y", Size: 12, Color: ui.Color{R: 1, G: 2, B: 3, A: 255}},
		{Text: "z", Size: 12, Color: ui.Color{R: 1, G: 2, B: 3, A: 255}, Italic: true, Underline: true},
		{Text: "r", Size: 12, Color: bg, Background: ui.Color{R: 1, G: 2, B: 3, A: 255}, Italic: true, Underline: true},
		{Text: "linkend", Size: 12, Color: fg},
	}
	if len(got) != len(want) {
		t.Fatalf("%d runs %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("run %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	// Lines split at the newlines inside runs.
	lines := ansiLines(ansiSpans("one\x1b[32mtwo\nthree\x1b[0m\n", base, &pal, bg))
	if len(lines) != 3 || len(lines[0]) != 2 || lines[1][0].Text != "three" || lines[1][0].Color != pal[2] || len(lines[2]) != 0 {
		t.Errorf("lines %+v", lines)
	}
}
