package main

import (
	"slices"
	"testing"
)

// A number steps to the nearest of its labels' keys past its value, or
// by its step within its bounds, without the float's noise; with
// nowhere to go, it reports so.
func TestTSPPrefsStep(t *testing.T) {
	labels := map[string]any{"0": "Off", "5": "5 min", "30": "30 min", "60": "1 h"}
	for _, c := range []struct {
		ctl       map[string]any
		dir       int
		want      float64
		ok        bool
		situation string
	}{
		{map[string]any{"value": 5.0, "labels": labels}, 1, 30, true, "labels up"},
		{map[string]any{"value": 5.0, "labels": labels}, -1, 0, true, "labels down"},
		{map[string]any{"value": 60.0, "labels": labels}, 1, 60, false, "labels at the top"},
		{map[string]any{"value": -1.0, "labels": labels}, 1, 0, true, "off the labels (unset)"},
		{map[string]any{"value": 4.0, "min": 1.0, "max": 8.0}, 1, 5, true, "step 1 by default"},
		{map[string]any{"value": 8.0, "min": 1.0, "max": 8.0}, 1, 8, false, "at the max"},
		{map[string]any{"value": 7.5, "max": 8.0, "step": 2.0}, 1, 8, true, "clamped to the max"},
		{map[string]any{"value": 1.0, "min": 1.0}, -1, 1, false, "at the min"},
		{map[string]any{"value": 0.2, "step": 0.1}, 1, 0.3, true, "a fraction, without noise"},
	} {
		got, ok := tspPrefsStep(c.ctl, c.dir)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %v %v, want %v %v", c.situation, got, ok, c.want, c.ok)
		}
	}
}

// A choice left to the terminal is segments with two to four short
// options, else a menu; its style says otherwise.
func TestTSPPrefsSegmented(t *testing.T) {
	opts := func(labels ...string) []any {
		var out []any
		for _, l := range labels {
			out = append(out, map[string]any{"value": l})
		}
		return out
	}
	for _, c := range []struct {
		ctl       map[string]any
		want      bool
		situation string
	}{
		{map[string]any{"options": opts("Dev", "Staging", "Prod")}, true, "three short"},
		{map[string]any{"options": opts("Only")}, false, "one"},
		{map[string]any{"options": opts("a", "b", "c", "d", "e")}, false, "five"},
		{map[string]any{"options": opts("A rather long option", "Another long option")}, false, "long labels"},
		{map[string]any{"options": opts("a", "b", "c", "d", "e"), "style": "segmented"}, true, "asked for segments"},
		{map[string]any{"options": opts("a", "b"), "style": "menu"}, false, "asked for a menu"},
	} {
		if got := tspPrefsSegmented(c.ctl); got != c.want {
			t.Errorf("%s: %v", c.situation, got)
		}
	}
}

// A multi's toggle turns a value off where it is, or on last; a move
// stays among those on; neither touches the program's slice.
func TestTSPMulti(t *testing.T) {
	values := []string{"slack", "email"}
	if got := tspMultiToggle(values, "pager"); !slices.Equal(got, []string{"slack", "email", "pager"}) {
		t.Errorf("on: %v", got)
	}
	if got := tspMultiToggle(values, "slack"); !slices.Equal(got, []string{"email"}) {
		t.Errorf("off: %v", got)
	}
	if got := tspMultiMove(values, "email", -1); !slices.Equal(got, []string{"email", "slack"}) {
		t.Errorf("up: %v", got)
	}
	if got := tspMultiMove(values, "email", 1); !slices.Equal(got, values) {
		t.Errorf("down past the end: %v", got)
	}
	if !slices.Equal(values, []string{"slack", "email"}) {
		t.Errorf("the program's values changed: %v", values)
	}
}

// The words of a search mark where they are in a label, in any case.
func TestTSPMatches(t *testing.T) {
	marks := tspMatches("Dark Mode for the Dark", "dark  MODE")
	var got []int
	for i, m := range marks {
		if m {
			got = append(got, i)
		}
	}
	want := []int{0, 1, 2, 3, 5, 6, 7, 8, 18, 19, 20, 21}
	if !slices.Equal(got, want) {
		t.Errorf("marked %v, want %v", got, want)
	}
	if tspMatches("Theme", "font") != nil || tspMatches("Theme", " ") != nil {
		t.Error("marks without a match")
	}
}

// A caret splits a text at a UTF-16 offset, clamped.
func TestTSPSplitAt(t *testing.T) {
	for _, c := range []struct {
		s         string
		at        int
		pre, post string
	}{
		{"abc", 1, "a", "bc"},
		{"a😀b", 3, "a😀", "b"},
		{"abc", 9, "abc", ""},
		{"abc", -2, "", "abc"},
	} {
		if pre, post := tspSplitAt(c.s, c.at); pre != c.pre || post != c.post {
			t.Errorf("%q at %d: %q|%q", c.s, c.at, pre, post)
		}
	}
}
