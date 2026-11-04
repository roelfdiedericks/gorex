package main

import (
	"testing"

	"gorex/internal/rex"
)

// A node's copy and open actions act on its href in the window, as in
// Tern: copy copies it, open opens a web page or an email address only;
// a node without an href, or another action, leaves it to the program.
func TestTSPLocalAct(t *testing.T) {
	node := func(href string) *rex.TSPNode {
		p := map[string]any{}
		if href != "" {
			p["href"] = href
		}
		return &rex.TSPNode{ID: "n", Kind: "row", Props: p}
	}
	const link = "https://claude.ai/oauth/authorize?client_id=x&code=true"
	for _, c := range []struct {
		situation, href, act, copied, opened string
	}{
		{"Copy on the sign-in link", link, "copy", link, ""},
		{"Open sign-in page", link, "open", "", link},
		{"open an email address", "mailto:me@example.com", "open", "", "mailto:me@example.com"},
		{"copy a device code", "ABCD-1234", "copy", "ABCD-1234", ""},
		{"open a file of the host's: not here", "file:///root/notes.md", "open", "", ""},
		{"open a code, which is no address", "ABCD-1234", "open", "", ""},
		{"copy without an href", "", "copy", "", ""},
		{"the program's own action", link, "submit", "", ""},
	} {
		copied, opened := tspLocalAct(node(c.href), c.act)
		if copied != c.copied || opened != c.opened {
			t.Errorf("%s: copied %q, opened %q; want %q, %q", c.situation, copied, opened, c.copied, c.opened)
		}
	}
}
