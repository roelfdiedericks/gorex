package main

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A program's text fields (editor, input) are native: MyGo's text widgets,
// which the user types into, with the caret, selection, wrapping and
// input methods of any field, and the program is told what changed (edit
// events, as the hello reply's features promise), sends on Enter (send)
// and undoes on Cmd+Z (undo); it answers with the text and caret it
// has, which the field takes when they differ from what it last said, as
// after a slash command or a history recall. Keys that type no text,
// Escape, Tab, the arrows at the edges, chords, go to the program through
// the terminal, as any key. A paste of text goes into the field; a paste
// without text (an image) goes to the program through the terminal, as a
// paste into the terminal would, which programs take as an attachment.

// tspField is what the window keeps of a program's field: its text, which
// the widget edits; what the program last said of it (said), so that a
// new word from the program is told from a stale one; what the field
// last sent it (sent), which the program echoes back; and the caret as
// of the last frame.
type tspField struct {
	text       string
	said       string
	saidAt     int // UTF-16 units, as the program counts
	sent       string
	sentAt     int
	caretRunes int
	pending    int // edits sent, not yet echoed
}

// tspField draws a program's text field, an editor (many lines) or an
// input (one), as a native field.
func (a *App) tspField(c *ui.Context, key *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode, area bool) ui.Element {
	id := n.ID
	fieldKey := strconvImage(sf.Image) + ":" + id
	if p.fields == nil {
		p.fields = map[string]*tspField{}
	}
	f := p.fields[fieldKey]
	if f == nil {
		f = &tspField{}
		p.fields[fieldKey] = f
	}
	text, _ := n.Props["text"].(string)
	cursor := utf16Len(text)
	if v, ok := n.Props["cursor"].(float64); ok {
		cursor = int(v)
	}
	// What the program says. An echo of what the field sent settles a
	// pending edit; while edits are pending, its text is stale, and
	// stays; otherwise a new text is the program's own (a slash command,
	// a history recall, a send), which the field takes.
	placeCaret := false
	if text == f.sent && cursor == f.sentAt {
		f.pending = 0
	} else if f.pending == 0 && (text != f.said || cursor != f.saidAt) {
		f.text = text
		f.sent, f.sentAt = text, cursor
		placeCaret = true
	}
	f.said, f.saidAt = text, cursor
	placeholder, _ := n.Props["placeholder"].(string)
	readonly, _ := n.Props["readonly"].(bool)
	sendable, _ := n.Props["sendable"].(bool)
	stream := p.stream
	send := func(ev map[string]any) {
		ev["sf"], ev["id"] = sf.ID, id
		go stream.TSPEvent(ev)
	}
	var el ui.Element
	if area {
		maxLines := 18
		if v, ok := n.Props["maxLines"].(float64); ok && v > 0 {
			maxLines = int(v)
		}
		el = ui.TextArea(key, &f.text).Lines(1, maxLines)
	} else {
		el = ui.TextInput(key, &f.text)
	}
	el.FillWidth().FontSize(13).TextColor(k.text).Placeholder(placeholder).ReadOnly(readonly || sf.Closed)
	if placeCaret {
		if runes := utf16ToRunes(f.text, cursor); runes >= 0 {
			el.SetTextSelection(runes, runes)
		}
	}
	term := p.term
	el.OnPaste(func(clip string) bool {
		if clip != "" {
			return false
		}
		// No text: an image or a file, which the program takes through
		// the terminal, as a paste into it.
		d := a.services.ReadClipboardData()
		if len(d.Formats()) == 0 {
			return false
		}
		term.PasteData(d)
		return true
	})
	caretRunes := &f.caretRunes
	el.OnTextKey(func(tk ui.TextKey) bool {
		switch {
		case tk.Key == ui.KeyEnter && tk.Mods == 0 && (sendable || !area):
			send(map[string]any{"ev": "send", "text": tk.Text()})
			return true
		case tk.Key == ui.KeyZ && tk.Mods&(ui.Ctrl|ui.Cmd) != 0 && tk.Mods&ui.Shift == 0:
			send(map[string]any{"ev": "undo"})
			return true
		case tk.Key == ui.KeyEscape, tk.Key == ui.KeyTab:
			term.Send(tspKeyBytes(tk.Key, tk.Mods))
			return true
		case tk.Mods&(ui.Ctrl|ui.Cmd) != 0 && !tspEditChord(tk.Key):
			// The program's chords (Ctrl+C, Ctrl+R, Ctrl+P, …): through
			// the terminal, as any key.
			if b := tspKeyBytes(tk.Key, tk.Mods); b != nil {
				term.Send(b)
				return true
			}
		case (tk.Key == ui.KeyUp || tk.Key == ui.KeyDown) && tk.Mods == 0:
			// At the field's edges the program gets them (its history,
			// its lists); inside, they move the caret.
			text := tk.Text()
			at := runeByte(text, *caretRunes)
			if (tk.Key == ui.KeyUp && !strings.Contains(text[:at], "\n")) || (tk.Key == ui.KeyDown && !strings.Contains(text[at:], "\n")) {
				term.Send(tspKeyBytes(tk.Key, 0))
				return true
			}
		}
		return false
	})
	s, _ := el.TextSelection()
	f.caretRunes = s
	if el.Changed() {
		// What the user typed, as the edit over the part that differs
		// from what the program has, with the caret; the program's
		// answer is awaited before its text is taken again.
		cur := runesToUTF16(f.text, s)
		from, to, ins := textDelta(f.sent, f.text)
		send(map[string]any{"ev": "edit", "from": from, "to": to, "text": ins, "cursor": cur, "len": utf16Len(f.sent)})
		f.sent, f.sentAt = f.text, cur
		f.pending++
	}
	return el
}

// tspEditChord reports whether a chord is the field's own: copy, cut,
// paste, select all, undo and redo, and the caret's moves.
func tspEditChord(key ui.Key) bool {
	switch key {
	case ui.KeyC, ui.KeyX, ui.KeyV, ui.KeyA, ui.KeyZ, ui.KeyY, ui.KeyLeft, ui.KeyRight, ui.KeyHome, ui.KeyEnd, ui.KeyBackspace, ui.KeyDelete:
		return true
	}
	return false
}

func strconvImage(image uint32) string {
	var b [10]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + image%10)
		image /= 10
		if image == 0 {
			return string(b[i:])
		}
	}
}

// utf16Len is the length of s in UTF-16 units, as the protocol counts.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// utf16ToRunes converts an offset in UTF-16 units to one in runes; -1
// past the end.
func utf16ToRunes(s string, units int) int {
	u, i := 0, 0
	for _, r := range s {
		if u >= units {
			return i
		}
		u += utf16.RuneLen(r)
		i++
	}
	if u >= units {
		return i
	}
	return -1
}

// runesToUTF16 converts an offset in runes to one in UTF-16 units.
func runesToUTF16(s string, runes int) int {
	u, i := 0, 0
	for _, r := range s {
		if i >= runes {
			break
		}
		u += utf16.RuneLen(r)
		i++
	}
	return u
}

// runeByte is the byte offset of the rune at i.
func runeByte(s string, i int) int {
	b := 0
	for n := 0; n < i && b < len(s); n++ {
		_, size := utf8.DecodeRuneInString(s[b:])
		b += size
	}
	return b
}

// tspKeyBytes is what a key sends a program on the pty: the usual
// sequences of a terminal; nil for a key with none.
func tspKeyBytes(key ui.Key, mods ui.Modifiers) []byte {
	switch key {
	case ui.KeyEscape:
		return []byte{0x1b}
	case ui.KeyTab:
		if mods&ui.Shift != 0 {
			return []byte("\x1b[Z")
		}
		return []byte{'\t'}
	case ui.KeyUp:
		return []byte("\x1b[A")
	case ui.KeyDown:
		return []byte("\x1b[B")
	case ui.KeyRight:
		return []byte("\x1b[C")
	case ui.KeyLeft:
		return []byte("\x1b[D")
	case ui.KeyEnter:
		return []byte{'\r'}
	}
	if mods&ui.Ctrl != 0 && key >= ui.KeyA && key <= ui.KeyZ {
		return []byte{byte(key-ui.KeyA) + 1}
	}
	return nil
}

// textDelta is the edit that turns old into new: the UTF-16 range of
// old it replaces and the text put there, around the longest common
// prefix and suffix.
func textDelta(old, new string) (from, to int, text string) {
	o, n := []rune(old), []rune(new)
	p := 0
	for p < len(o) && p < len(n) && o[p] == n[p] {
		p++
	}
	s := 0
	for s < len(o)-p && s < len(n)-p && o[len(o)-1-s] == n[len(n)-1-s] {
		s++
	}
	from = runesToUTF16(old, p)
	to = runesToUTF16(old, len(o)-s)
	return from, to, string(n[p : len(n)-s])
}
