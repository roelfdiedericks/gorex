package main

import "testing"

// A one-line field's change goes to its program as the keys that make
// it: the caret moves to the end of the part that differs, Backspace
// takes it out, the replacement is typed, and the caret moves to where
// the field has it.
func TestTSPEditKeys(t *testing.T) {
	for _, c := range []struct {
		old       string
		oldAt     int
		new       string
		newAt     int
		want      string
		situation string
	}{
		{"hai", 3, "haik", 4, "k", "typed at the end"},
		{"haiku", 5, "hai", 3, "\x7f\x7f", "erased at the end"},
		{"hiku", 1, "haiku", 2, "a", "typed in the middle"},
		{"haiku", 5, "hiku", 1, "\x1b[D\x1b[D\x1b[D\x7f", "erased in the middle, caret at the end"},
		{"opus", 4, "haiku", 5, "\x7f\x7f\x7f\x7fhaiku", "replaced, as a selection typed over"},
		{"a😀b", 4, "a😀xb", 4, "\x1b[Dx", "UTF-16 offsets around a character outside the BMP"},
		{"abc", 3, "abc", 0, "\x1b[D\x1b[D\x1b[D", "the caret moved alone"},
	} {
		if got := string(tspEditKeys(c.old, c.oldAt, c.new, c.newAt)); got != c.want {
			t.Errorf("%s: %q, want %q", c.situation, got, c.want)
		}
	}
}
