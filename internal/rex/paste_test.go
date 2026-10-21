package rex

import (
	"slices"
	"strings"
	"testing"
)

func TestPasteTracker(t *testing.T) {
	read := "\x1b]5522;type=read:pw=cGFzcw==:name=UGFzdGU=;aW1hZ2UvcG5n\x1b\\"
	for _, c := range []struct {
		name  string
		out   []string // what the session prints, read by read
		on    bool
		reads []string
	}{
		{"set", []string{"\x1b[?5522h"}, true, nil},
		{"set among modes", []string{"\x1b[?1049;5522;2004h"}, true, nil},
		{"set then reset", []string{"\x1b[?5522h", "x\x1b[?2004;5522l"}, false, nil},
		{"other modes", []string{"\x1b[?55221h\x1b[5522h\x1b[?5522$p"}, false, nil},
		{"split across reads", []string{"ab\x1b", "[?55", "22h"}, true, nil},
		{"full reset", []string{"\x1b[?5522h\x1bc"}, false, nil},
		{"paste read", []string{read}, false, []string{read}},
		{"paste read with BEL, split", []string{"\x1b]5522;type=read:pw=cA==", ";dGV4dC9wbGFpbg==\x07"}, false,
			[]string{"\x1b]5522;type=read:pw=cA==;dGV4dC9wbGFpbg==\x1b\\"}},
		{"read without a password", []string{"\x1b]5522;type=read;dGV4dC9wbGFpbg==\x1b\\"}, false, nil},
		{"write with a password", []string{"\x1b]5522;type=write:pw=cA==;\x1b\\"}, false, nil},
		{"a password in the payload", []string{"\x1b]5522;type=read;pw=\x1b\\"}, false, nil},
		{"too long", []string{"\x1b]5522;type=read:pw=" + strings.Repeat("A", maxPasteSequence) + ";\x1b\\"}, false, nil},
		{"cut short by another sequence", []string{"\x1b]5522;type=read:pw=cA==\x1b[?5522h"}, true, nil},
	} {
		var p pasteTracker
		for _, o := range c.out {
			p.feed([]byte(o))
		}
		if p.on != c.on || !slices.Equal(p.reads, c.reads) {
			t.Errorf("%s: on %v, reads %q; want %v, %q", c.name, p.on, p.reads, c.on, c.reads)
		}
	}
}
