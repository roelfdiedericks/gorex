package main

import (
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A program's text fields (editor, input) are native: MyGo's text widgets,
// which the user types into, with the caret, selection, wrapping and
// input methods of any field, and the program is told what changed (edit
// events, as the hello reply's features promise), sends on Enter (the
// composer's send button pressed, else a send of the text) and undoes on
// Cmd+Z (undo); it answers with the text and caret it
// has, which the field takes when they differ from what it last said, as
// after a slash command or a history recall. Keys that type no text,
// Escape, Tab, the arrows at the edges, chords, go to the program through
// the terminal, encoded as the terminal encodes any key, in the keyboard
// mode the program asked for (Terminal.SendKey; tspChord): Ctrl+C copies
// a selection only, as the terminal's does, and Alt chords are the
// program's.
// A paste of text goes into the field, but a large one (tspLargePaste) in
// omp's composer goes to omp, as Tern's does; a paste without text (an
// image) goes to the program through the terminal, as a paste into the
// terminal would, which programs take as an attachment.

// tspLargePaste reports whether a pasted text is large as omp counts it:
// more than 10 lines or more than 1000 characters (UTF-16 units, as its
// JavaScript has them). omp makes such a paste a chip, or asks how to
// attach it, rather than typing it into the draft.
func tspLargePaste(text string) bool {
	if strings.Count(text, "\n") >= 10 {
		return true
	}
	n := 0
	for _, r := range text {
		n += utf16.RuneLen(r)
		if n > 1000 {
			return true
		}
	}
	return false
}

// tspField is what the window keeps of a program's field: its text, which
// the widget edits; what the program last said of it (said); the texts
// the field sent it that it has not echoed yet, oldest first (sent);
// and the caret as of the last frame, and whether text was selected.
type tspField struct {
	text       string
	said       string
	saidAt     int // UTF-16 units, as the program counts
	sent       []tspSent
	caretRunes int
	selected   bool
	press      *tspPress
}

// tspPress is an Enter waiting for the program to have the field's text,
// to press the composer's send button then: the button's node and the
// action its click sends, and when to give up and send the text instead.
type tspPress struct {
	id, act string
	until   time.Time
}

// tspPressWait is how long an Enter waits for the program to catch up
// with what was typed before pressing the send button.
const tspPressWait = time.Second

// tspSent is a text a field sent its program, with its caret.
type tspSent struct {
	text string
	at   int
}

// tspMaxSent caps the texts a field keeps waiting for their echoes: a
// program drops an edit it finds stale, and never echoes it.
const tspMaxSent = 32

// hear takes what the program says of the field. An echo of a text the
// field sent settles it and those before it; the program's word between
// them is stale, and the field keeps its own. A text it never sent is
// the program's own (a slash command, a history recall, a send, another
// window's typing), which the field takes, over what it was waiting for.
// It reports whether the caret is to go where the program put it.
func (f *tspField) hear(text string, at int) bool {
	if text == f.said && at == f.saidAt {
		return false
	}
	f.said, f.saidAt = text, at
	for i, s := range f.sent {
		if s.text == text {
			f.sent = f.sent[i+1:]
			return false
		}
	}
	f.sent = f.sent[:0]
	f.text = text
	return true
}

// base is the text and caret the program has as the field's next edit
// comes to it: the last it was sent, or the last it said.
func (f *tspField) base() (string, int) {
	if n := len(f.sent); n > 0 {
		return f.sent[n-1].text, f.sent[n-1].at
	}
	return f.said, f.saidAt
}

// send records a text sent to the program, awaiting its echo.
func (f *tspField) send(text string, at int) {
	if len(f.sent) == tspMaxSent {
		f.sent = f.sent[1:]
	}
	f.sent = append(f.sent, tspSent{text, at})
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
	placeCaret := f.hear(text, cursor)
	// A placeholder in spans (omp's session title, quoted in italics) is
	// painted on the empty field: Placeholder takes plain text.
	placeholder, _ := n.Props["placeholder"].(string)
	var richPlaceholder []ui.Span
	if spans, ok := n.Props["placeholder"].([]any); ok {
		for _, s := range spans {
			m, _ := s.(map[string]any)
			text, _ := m["t"].(string)
			style, _ := m["s"].(string)
			sp := ui.Span{Text: text, Size: a.tspText(13), Color: k.textMuted}
			for _, tok := range strings.Fields(style) {
				switch tok {
				case "em":
					sp.Italic = true
				case "strong":
					sp.Weight = 600
				}
			}
			richPlaceholder = append(richPlaceholder, sp)
		}
	}
	readonly, _ := n.Props["readonly"].(bool)
	sendable, _ := n.Props["sendable"].(bool)
	stream := p.stream
	send := func(ev map[string]any) {
		ev["sf"], ev["id"] = sf.ID, id
		go stream.TSPEvent(ev)
	}
	press := func(pr *tspPress) {
		go stream.TSPEvent(map[string]any{"ev": "action", "sf": sf.ID, "id": pr.id, "act": pr.act})
	}
	// An Enter waiting for the program to catch up presses the send button
	// once it has, or sends the text when it never does.
	if pr := f.press; pr != nil {
		switch {
		case f.said == f.text:
			f.press = nil
			press(pr)
		case time.Now().After(pr.until):
			f.press = nil
			send(map[string]any{"ev": "send", "text": f.text})
		}
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
	// The composer's text is the messages' size; other fields' the text's.
	size := a.tspText(13)
	if a.tspFieldBare {
		size = a.tspMsg(14)
	}
	el.FillWidth().FontSize(size).TextColor(k.text).Placeholder(placeholder).ReadOnly(readonly || sf.Closed)
	padX, padY := float32(10), float32(6)
	if a.tspFieldBare {
		// The composer's card is the field's box; it starts at one line,
		// as Lines asks, without a text area's least height.
		el.Background(ui.Transparent).Border(0, ui.Transparent).FocusRing(false).Padding(0).MinHeight(0)
		padX, padY = 0, 0
		// The pane's terminal passes the focus to it (view.go); the send
		// button is faint while it holds nothing to send (tspSubmit).
		p.composerAt, p.composerFocused = a.frame, el.Focused()
		p.composerEmpty = strings.TrimSpace(f.text) == ""
		if p.composerFocus {
			el.Focus()
			p.composerFocus = false
		}
	}
	if richPlaceholder != nil {
		el.Draw(func(pt *ui.Painter, r ui.Rect) {
			if f.text != "" {
				return
			}
			l := ui.ShapeRichText(richPlaceholder, ui.Font{Family: a.tspLook().font, Size: size}, max(r.W-2*padX, 1))
			pt.Clip(r, 0, func() { pt.TextLayout(l, r.X+padX, r.Y+padY, k.textMuted) })
		})
	}
	if a.tspFocus != "" && a.tspFocus == id {
		// The program's focus op: this field takes the keys.
		el.Focus()
		a.tspFocus = ""
	}
	if placeCaret {
		if runes := utf16ToRunes(f.text, cursor); runes >= 0 {
			el.SetTextSelection(runes, runes)
		}
	}
	term := p.term
	composer := a.tspFieldBare
	el.OnPaste(func(clip string) bool {
		if clip != "" {
			if !composer || !tspLargePaste(clip) {
				return false
			}
			// A large paste in omp's composer goes to omp as a paste, as
			// Tern's does, which collapses it to a chip or asks how to
			// attach it; the field takes the draft omp then has.
			p.pasteText = true
			term.Paste(clip)
			return true
		}
		// No text: an image or a file, which the program takes through
		// the terminal, as a paste into it.
		d := a.services.ReadClipboardData()
		if len(d.Formats()) == 0 {
			return false
		}
		p.pasteText = false
		term.PasteData(d)
		return true
	})
	caretRunes, selected := &f.caretRunes, &f.selected
	ctrlCCopies, optionAsAlt := is(a.eff.Terminal.CtrlCCopies), is(a.eff.Terminal.OptionAsAlt)
	el.OnTextKey(func(tk ui.TextKey) bool {
		// The text area takes the keys it edits with before the pane's
		// shortcuts (view.go) see them: the composer scrolls with them
		// itself.
		if by := tspComposerScroll(tk.Key, tk.Mods); composer && by != "" {
			a.scrollTerm(p, by)
			return true
		}
		switch {
		case tk.Key == ui.KeyEnter && tk.Mods == 0 && !area:
			// A one-line input's Enter is its program's key (a picker
			// takes the selected item).
			term.SendKey(0, ui.KeyEnter, "")
			return true
		case tk.Key == ui.KeyEnter && tk.Mods == 0 && sendable:
			// Enter submits omp's own draft, as its send button does:
			// with the images pasted into it, which a send of the text
			// loses (omp empties its draft, images and all, before taking
			// the text). While a turn runs, the button is Stop: the same
			// submit, to the field, queues the message. It waits for omp
			// to have what was typed. An empty field, or a composer that
			// is not omp's, sends the text, as an empty submit would stop
			// omp's turn.
			if composer && strings.TrimSpace(tk.Text()) != "" {
				// What is sent shows at the transcript's end (tspSent).
				term.ScrollToBottom()
			}
			pr := tspEnterPress(sf.Root, id)
			if pr == nil || strings.TrimSpace(tk.Text()) == "" {
				send(map[string]any{"ev": "send", "text": tk.Text()})
				return true
			}
			pr.until = time.Now().Add(tspPressWait)
			if f.said == tk.Text() {
				press(pr)
				return true
			}
			f.press = pr
			if win := a.win; win != nil {
				// A frame after the wait, to send the text if omp never
				// caught up.
				time.AfterFunc(tspPressWait+50*time.Millisecond, func() { win.Update(func() {}) })
			}
			return true
		case tk.Key == ui.KeyZ && tk.Mods&(ui.Ctrl|ui.Cmd) != 0 && tk.Mods&ui.Shift == 0:
			send(map[string]any{"ev": "undo"})
			return true
		case tk.Key == ui.KeyEscape && tk.Mods == 0 && composer && a.trayEscape(p, sf):
			// The tray's first: a peek closes, a row from the dots folds.
			return true
		case tk.Key == ui.KeyEscape, tk.Key == ui.KeyTab:
			term.SendKey(tk.Mods, tk.Key, "")
			return true
		case tk.Mods&(ui.Ctrl|ui.Cmd|ui.Alt) != 0:
			// The program's chords (Ctrl+C, Ctrl+R, Alt+P, …): through
			// the terminal, as any key; the field keeps its own.
			if text, ok := tspChord(tk.Key, tk.Mods, *selected, ctrlCCopies, optionAsAlt); ok {
				term.SendKey(tk.Mods, tk.Key, text)
				return true
			}
		case (tk.Key == ui.KeyUp || tk.Key == ui.KeyDown) && tk.Mods == 0:
			// At the field's edges the program gets them (its history,
			// its lists); inside, they move the caret. A one-line input
			// has no inside.
			text := tk.Text()
			at := runeByte(text, *caretRunes)
			if !area || (tk.Key == ui.KeyUp && !strings.Contains(text[:at], "\n")) || (tk.Key == ui.KeyDown && !strings.Contains(text[at:], "\n")) {
				term.SendKey(0, tk.Key, "")
				return true
			}
		}
		return false
	})
	// Alt with a letter or digit counts as typing to the text widget (Alt
	// types characters on some layouts), which OnTextKey then never sees:
	// taken as the key comes instead, the program's Meta chord.
	el.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputKeyDown || ev.Mods&ui.Alt == 0 {
			return false
		}
		text, ok := tspChord(ev.Key, ev.Mods, *selected, ctrlCCopies, optionAsAlt)
		if !ok {
			return false
		}
		term.SendKey(ev.Mods, ev.Key, text)
		return true
	})
	s, e := el.TextSelection()
	f.caretRunes, f.selected = s, s != e
	if el.Changed() {
		// What the user typed, as the edit over the part that differs
		// from what the program has, with the caret, which the program
		// is to echo (hear). A one-line input gets it as keys, through
		// the terminal: programs filter their lists as their fields
		// take keys (omp's pickers apply an edit event to the field
		// alone).
		cur := runesToUTF16(f.text, s)
		base, baseAt := f.base()
		if area {
			from, to, ins := textDelta(base, f.text)
			send(map[string]any{"ev": "edit", "from": from, "to": to, "text": ins, "cursor": cur, "len": utf16Len(base)})
		} else {
			tspSendKeys(term, tspEditKeys(base, baseAt, f.text, cur))
		}
		f.send(f.text, cur)
	}
	return el
}

// tspEnterPress is what Enter in the composer field of id presses: its
// send button (omp's omp.composer.send) with the action a click on it
// sends, or while a turn runs, when the button is Stop (omp.composer.stop),
// the submit the send button sends, to the field itself; nil for neither.
func tspEnterPress(root *rex.TSPNode, id string) *tspPress {
	running := false
	var found *tspPress
	var walk func(n *rex.TSPNode)
	walk = func(n *rex.TSPNode) {
		if n == nil || found != nil {
			return
		}
		switch role, _ := n.Props["role"].(string); role {
		case "omp.composer.send":
			if actions, ok := n.Props["actions"].(map[string]any); ok {
				if act, _ := actions["click"].(string); act != "" {
					found = &tspPress{id: n.ID, act: act}
					return
				}
			}
		case "omp.composer.stop":
			running = true
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(root)
	if found == nil && running {
		found = &tspPress{id: id, act: "submit"}
	}
	return found
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

// tspComposerScroll is how a key typed in the composer scrolls the
// transcript, as Tern's does: Page Up and Page Down by a page, Ctrl+Home
// and Ctrl+End to its ends; "" for a key the composer keeps, as Home and
// End, which move its caret.
func tspComposerScroll(key ui.Key, mods ui.Modifiers) string {
	switch {
	case key == ui.KeyPageUp && mods == 0:
		return "page-up"
	case key == ui.KeyPageDown && mods == 0:
		return "page-down"
	case key == ui.KeyHome && mods == ui.Ctrl:
		return "start"
	case key == ui.KeyEnd && mods == ui.Ctrl:
		return "end"
	}
	return ""
}

// tspChord reports whether a chord typed in a program's field goes to the
// program, through the terminal (Terminal.SendKey, in the keyboard mode the
// program asked for), and the text it sends with it; false for one the
// field keeps. Ctrl+C is the program's interrupt (omp's clear, and exit
// when pressed twice) but for a selection where Ctrl+C copies, as the
// terminal's setting says. Alt with a letter or digit is Meta's, sent with
// the key's character; with an arrow or Backspace it is the field's word
// moves, and on macOS Option types characters unless Option is Alt. The
// other Ctrl and Cmd chords are the program's but the field's own
// (tspEditChord).
func tspChord(key ui.Key, mods ui.Modifiers, selected, ctrlCCopies, optionAsAlt bool) (string, bool) {
	switch {
	case key == ui.KeyC && mods == ui.Ctrl:
		return "", !selected || !ctrlCCopies
	case mods&ui.Alt != 0 && mods&(ui.Ctrl|ui.Cmd) == 0:
		if runtime.GOOS == "darwin" && !optionAsAlt {
			return "", false
		}
		switch {
		case key >= ui.KeyA && key <= ui.KeyZ:
			ch := 'a' + rune(key-ui.KeyA)
			if mods&ui.Shift != 0 {
				ch = 'A' + rune(key-ui.KeyA)
			}
			return string(ch), true
		case key >= ui.Key0 && key <= ui.Key9:
			return string('0' + rune(key-ui.Key0)), true
		}
		return "", false
	case mods&(ui.Ctrl|ui.Cmd) != 0 && !tspEditChord(key):
		return "", true
	}
	return "", false
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

// tspKey is a key a field sends its program: key pressed, or with
// KeyUnknown the text typed.
type tspKey struct {
	key  ui.Key
	text string
}

// tspEditKeys returns the keys that turn a field's old text, with the
// caret at oldAt, into the new one with the caret at newAt (UTF-16
// offsets): the caret moves to the end of the part that differs, Backspace
// takes it out, its replacement is typed, and the caret moves to its
// place, by characters, as a program's line editor counts them.
func tspEditKeys(old string, oldAt int, new string, newAt int) []tspKey {
	from, to, ins := textDelta(old, new)
	var keys []tspKey
	move := func(at, to int) {
		for ; at < to; at++ {
			keys = append(keys, tspKey{key: ui.KeyRight})
		}
		for ; at > to; at-- {
			keys = append(keys, tspKey{key: ui.KeyLeft})
		}
	}
	at := max(utf16ToRunes(old, oldAt), 0)
	from, to = utf16ToRunes(old, from), utf16ToRunes(old, to)
	move(at, to)
	for range to - from {
		keys = append(keys, tspKey{key: ui.KeyBackspace})
	}
	if ins != "" {
		keys = append(keys, tspKey{text: ins})
	}
	move(from+utf8.RuneCountInString(ins), max(utf16ToRunes(new, newAt), 0))
	return keys
}

// tspSendKeys sends a field's keys to its program through the terminal,
// encoded in the keyboard mode the program asked for (Terminal.SendKey);
// typed text goes as text, as a terminal sends it.
func tspSendKeys(term *terminal.Terminal, keys []tspKey) {
	for _, k := range keys {
		if k.key == ui.KeyUnknown {
			term.Send([]byte(k.text))
		} else {
			term.SendKey(0, k.key, "")
		}
	}
}
