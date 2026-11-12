package main

import "testing"

// Code in a message is asked about as a file only when it looks like a
// file's name: a name with an extension of letters, or a path; not a
// version, a number, a command, an option or an address.
func TestLooksLikeFile(t *testing.T) {
	for _, c := range []struct {
		code string
		want bool
	}{
		{"FOO.md", true},
		{"main.go", true},
		{"internal/rex/tsp.go", true},
		{"docs/", false}, // a folder, which opens in no popup
		{"src/cmd", true},
		{`C:\experiment\TSP.md`, true},
		{"v1.2", false},
		{"0.95", false},
		{"e.g.", false},
		{"Makefile", false},
		{"go build ./...", false},
		{"--force", false},
		{"https://example.com/a.md", false},
		{"*.go", false},
		{"", false},
	} {
		if got := looksLikeFile(c.code); got != c.want {
			t.Errorf("looksLikeFile(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}
