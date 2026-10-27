package main

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

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

// A field takes what its program says unless it is an echo of what the
// field sent: typed ahead of the echoes, the field keeps its text; a
// text the program has of its own (a send, another window's typing)
// replaces it, even while echoes were awaited that will not come.
func TestTSPFieldHear(t *testing.T) {
	f := &tspField{}
	f.text = "ab"
	f.send("a", 1)
	f.send("ab", 2)
	if base, at := f.base(); base != "ab" || at != 2 {
		t.Errorf("base while typing: %q at %d", base, at)
	}
	// The echo of "a" comes after "ab" was typed: stale, kept.
	if f.hear("a", 1) || f.text != "ab" || len(f.sent) != 1 {
		t.Errorf("a stale echo: text %q, waiting %v", f.text, f.sent)
	}
	if f.hear("ab", 2) || f.text != "ab" || len(f.sent) != 0 {
		t.Errorf("the last echo: text %q, waiting %v", f.text, f.sent)
	}
	// An edit the program dropped is never echoed; another window's text
	// comes instead, and is taken.
	f.text = "abc"
	f.send("abc", 3)
	if !f.hear("from window two", 15) || f.text != "from window two" || len(f.sent) != 0 {
		t.Errorf("the program's own text: text %q, waiting %v", f.text, f.sent)
	}
	if base, at := f.base(); base != "from window two" || at != 15 {
		t.Errorf("base after the program's text: %q at %d", base, at)
	}
	// Said again unchanged: nothing.
	if f.hear("from window two", 15) {
		t.Error("the same word heard twice moved the caret")
	}
	// The program clears it after a send: taken.
	if !f.hear("", 0) || f.text != "" {
		t.Errorf("after a send: %q", f.text)
	}
	// The waiting texts are capped.
	for i := range tspMaxSent + 5 {
		f.send(string(rune('a'+i%26)), 1)
	}
	if len(f.sent) != tspMaxSent {
		t.Errorf("waiting %d texts", len(f.sent))
	}
}

// A chord typed in a program's field goes to the program but for the
// field's own: Ctrl+C is the program's interrupt but for a selection
// where Ctrl+C copies; Alt with a letter or digit is ESC and the key
// (on macOS only with Option as Alt), with an arrow the field's; the
// other Ctrl chords are the program's but the editing ones.
func TestTSPChordBytes(t *testing.T) {
	for _, c := range []struct {
		key                              ui.Key
		mods                             ui.Modifiers
		selected, ctrlCCopies, optionAlt bool
		want                             string
		situation                        string
	}{
		{ui.KeyC, ui.Ctrl, false, true, false, "\x03", "Ctrl+C with nothing selected"},
		{ui.KeyC, ui.Ctrl, true, true, false, "", "Ctrl+C copying a selection"},
		{ui.KeyC, ui.Ctrl, true, false, false, "\x03", "Ctrl+C with a selection, the setting off"},
		{ui.KeyC, ui.Ctrl | ui.Shift, true, true, false, "", "Ctrl+Shift+C"},
		{ui.KeyP, ui.Alt, false, false, true, "\x1bp", "Alt+P"},
		{ui.KeyP, ui.Alt | ui.Shift, false, false, true, "\x1bP", "Alt+Shift+P"},
		{ui.Key3, ui.Alt, false, false, true, "\x1b3", "Alt+3"},
		{ui.KeyLeft, ui.Alt, false, false, true, "", "Alt+Left, a word move"},
		{ui.KeyR, ui.Ctrl, false, false, false, "\x12", "Ctrl+R"},
		{ui.KeyV, ui.Ctrl, false, false, false, "", "Ctrl+V, the field's paste"},
	} {
		if got := string(tspChordBytes(c.key, c.mods, c.selected, c.ctrlCCopies, c.optionAlt)); got != c.want {
			t.Errorf("%s: %q, want %q", c.situation, got, c.want)
		}
	}
	if runtime.GOOS == "darwin" {
		if b := tspChordBytes(ui.KeyP, ui.Alt, false, false, false); b != nil {
			t.Errorf("Option+P without Option as Alt: %q", b)
		}
	}
}
