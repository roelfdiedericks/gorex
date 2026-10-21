package rex

import (
	"bytes"
	"strings"
)

// Paste events, of the Kitty clipboard protocol: a program that takes them
// (mode 5522) is offered what the user pastes, and reads what it wants of
// it with the paste's password (OSC 5522). The server's terminal leaves a
// read with a password unanswered, for the window that pasted, which holds
// what was pasted, to answer; frames carry neither the mode nor the reads.
// The server reads both from what the session prints and passes them on
// with the frames.

// maxPasteSequence is the longest sequence read for paste events: a read
// is short, and longer OSC 5522 sequences are writes, which frames carry.
const maxPasteSequence = 4096

// pasteTracker reads a session's output for paste events. Its methods run
// under the session's lock.
type pasteTracker struct {
	// on tells that a program takes paste events.
	on bool
	// reads are paste reads read and not yet passed on, as sequences for
	// the windows' terminals.
	reads []string
	// The sequence being read, across reads: escaped after an ESC, csi
	// within a CSI and osc within an OSC, with its body so far; over when
	// it is longer than maxPasteSequence.
	escaped, csi, osc, oscEscaped, over bool
	body                                []byte
}

// feed reads output.
func (t *pasteTracker) feed(p []byte) {
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch {
		case t.oscEscaped:
			t.oscEscaped = false
			if b == '\\' {
				t.endOSC()
				continue
			}
			// Another sequence begins: the OSC is cut short.
			t.osc, t.body, t.over = false, t.body[:0], false
			t.escaped = true
			i--
		case t.osc:
			switch b {
			case 0x07:
				t.endOSC()
			case 0x1b:
				t.oscEscaped = true
			default:
				t.add(b)
			}
		case t.csi:
			switch {
			case b == 0x1b:
				t.csi, t.escaped = false, true
			case b >= 0x40 && b <= 0x7e:
				t.endCSI(b)
			case b >= 0x20:
				t.add(b)
			}
		case t.escaped:
			t.escaped = false
			switch b {
			case ']':
				t.osc, t.body, t.over = true, t.body[:0], false
			case '[':
				t.csi, t.body, t.over = true, t.body[:0], false
			case 'c':
				// A full reset resets the mode.
				t.on = false
			case 0x1b:
				t.escaped = true
			}
		default:
			j := bytes.IndexByte(p[i:], 0x1b)
			if j < 0 {
				return
			}
			i += j
			t.escaped = true
		}
	}
}

func (t *pasteTracker) add(b byte) {
	if len(t.body) < maxPasteSequence {
		t.body = append(t.body, b)
	} else {
		t.over = true
	}
}

// endCSI takes the CSI read, which final ends: DECSET and DECRST of mode
// 5522 among their modes.
func (t *pasteTracker) endCSI(final byte) {
	t.csi = false
	body := t.body
	t.body = t.body[:0]
	if t.over || final != 'h' && final != 'l' || len(body) == 0 || body[0] != '?' {
		return
	}
	for m := range strings.SplitSeq(string(body[1:]), ";") {
		if m == "5522" {
			t.on = final == 'h'
		}
	}
}

// endOSC takes the OSC read: a read of the clipboard with a paste's
// password.
func (t *pasteTracker) endOSC() {
	t.osc = false
	body := string(t.body)
	t.body = t.body[:0]
	if t.over {
		t.over = false
		return
	}
	rest, ok := strings.CutPrefix(body, "5522;")
	if !ok {
		return
	}
	meta, _, _ := strings.Cut(rest, ";")
	read, pw := false, false
	for kv := range strings.SplitSeq(meta, ":") {
		read = read || kv == "type=read"
		pw = pw || strings.HasPrefix(kv, "pw=")
	}
	if read && pw {
		t.reads = append(t.reads, "\x1b]"+body+"\x1b\\")
	}
}
