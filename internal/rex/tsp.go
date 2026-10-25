package rex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
)

// The Tern Surface Protocol (TSP, docs.stencil.so/tern/protocol): a
// program describes its UI as a tree of nodes, in-band on the pty as APC
// messages (ESC _ tsp;<verb>;<k=v;>...<body> ESC \), and the terminal
// draws it. The server keeps each surface's document, so that every
// window shows it and one attaching gets it, and answers the program:
// the hello reply, before its terminal's DA1 answer, and an ack for each
// frame (flow control follows the server, which always exists). Windows
// get whole documents (Surface) and send back the events of what the user
// does on them. Only screen surfaces for now, and no blobs, palettes or
// stylesheets: see TSP.md.

// tspKinds are the node kinds the windows draw: the hello reply says so,
// and programs fall back for the others.
var tspKinds = []string{"col", "row", "card", "section", "text", "md", "badge", "kv", "list", "item", "code", "table", "progress", "spinner", "status", "rule", "spacer", "kbd", "icon"}

const (
	tspAPC     = 65536 // the largest body a message carries
	tspCredits = 2     // frames the program may send unacknowledged
	// tspMaxBody caps a reassembled body; a longer one is dropped whole.
	tspMaxBody = 4 << 20
)

// Surface is a program's surface as a window draws it.
type Surface struct {
	ID    string   `json:"id"`
	Mode  string   `json:"mode"`
	Title string   `json:"title,omitempty"`
	Root  *TSPNode `json:"root,omitempty"`
	// Image is the id of the anchor an inline surface has in the
	// terminal (terminal.Options.AppImages), whose placement is where
	// the window draws it, and Rows how many rows it takes there; 0 for a
	// screen surface, drawn over the whole pane.
	Image uint32 `json:"image,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	// Closed tells a surface kept after its program closed it: drawn as
	// it was, taking no events.
	Closed bool `json:"closed,omitempty"`
}

// TSPNode is a node of a surface's document: the kind, the props and the
// children, as the protocol's add op carries them.
type TSPNode struct {
	ID       string         `json:"id"`
	Kind     string         `json:"k"`
	Props    map[string]any `json:"p,omitempty"`
	Children []*TSPNode     `json:"c,omitempty"`
}

// tspTracker reads a session's output for TSP messages and keeps the
// surfaces. Its methods run under the session's lock.
type tspTracker struct {
	// on tells that the session answers TSP (the host's setting).
	on bool
	// surfaces are the open surfaces, by id, and order their ids as
	// opened.
	surfaces map[string]*Surface
	order    []string
	// nodes index each surface's nodes by id, and parent each node's
	// parent, "" for the root.
	nodes  map[string]map[string]*TSPNode
	parent map[string]map[string]string
	// replies are what the program is to be told, in order: the hello
	// reply, acks. The reader writes them to the pty.
	replies [][]byte
	// inject is what the terminal is to be fed in the message's place:
	// anchors, kept text; feed puts it in the output there. Anything
	// else the tracker feeds the terminal goes the same way (resize).
	inject []byte
	// kept are the surfaces closed with keep, drawn where they were,
	// oldest first; nextImage is the last anchor's image id.
	kept      []*Surface
	nextImage uint32
	// changed tells that a surface changed since the windows were told.
	changed bool
	// hello is the hello reply's terminal facts the session knows: its
	// size, cell size and whether the window's theme is dark.
	cols, cellW, cellH int
	dark               bool

	// The message being read, across reads: opening is how much of
	// tspOpener was read, in within the APC, with its body so far; over
	// when it is longer than tspMaxBody; chunks collects a chunked body
	// by its id.
	opening             int
	in, inEscaped, over bool
	body                []byte
	chunks              map[string][]byte
}

// tspOpener is what starts a message of ours.
const tspOpener = "\x1b_tsp;"

// feed reads output, taking the TSP messages out of it: it returns the
// output without them, which goes to the terminal.
func (t *tspTracker) feed(p []byte) []byte {
	if !t.on {
		return p
	}
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch {
		case t.inEscaped:
			t.inEscaped = false
			if b == '\\' {
				t.end()
				if len(t.inject) > 0 {
					out = append(out, t.inject...)
					t.inject = nil
				}
				continue
			}
			// Another sequence begins: the message is cut short.
			t.in, t.body, t.over = false, t.body[:0], false
			out = append(out, 0x1b)
			i--
		case t.in:
			if b == 0x1b {
				t.inEscaped = true
			} else if len(t.body) < tspMaxBody {
				t.body = append(t.body, b)
			} else {
				t.over = true
			}
		case t.opening > 0:
			// Reading the opener, which a read may split anywhere: the
			// bytes go through as they were when it turns out not ours.
			if b == tspOpener[t.opening] {
				t.opening++
				if t.opening == len(tspOpener) {
					t.opening = 0
					t.in, t.body, t.over = true, t.body[:0], false
				}
				continue
			}
			out = append(out, tspOpener[:t.opening]...)
			t.opening = 0
			i--
		default:
			if b == 0x1b {
				t.opening = 1
			} else {
				out = append(out, b)
			}
		}
	}
	return out
}

// end takes the message read.
func (t *tspTracker) end() {
	t.in = false
	body := t.body
	t.body = t.body[:0]
	if t.over {
		t.over = false
		return
	}
	// verb;k=v;...;body: params are key=value runs ended by ';', a body
	// never starts so.
	verb, rest, ok := strings.Cut(string(body), ";")
	if !ok {
		return
	}
	params := map[string]string{}
	for {
		k, v, found := cutParam(rest)
		if !found {
			break
		}
		params[k] = v
		rest = rest[len(k)+len(v)+2:]
	}
	if c := params["c"]; c != "" {
		if t.chunks == nil {
			t.chunks = map[string][]byte{}
		}
		joined := append(t.chunks[c], rest...)
		if params["m"] == "1" {
			if len(joined) <= tspMaxBody {
				t.chunks[c] = joined
			} else {
				delete(t.chunks, c)
			}
			return
		}
		delete(t.chunks, c)
		rest = string(joined)
	}
	t.message(verb, []byte(rest))
}

// cutParam reads a key=value; at the start of s.
func cutParam(s string) (k, v string, ok bool) {
	i := 0
	for i < len(s) && (s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] == '_' || s[i] == '-') {
		i++
	}
	if i == 0 || i >= len(s) || s[i] != '=' {
		return "", "", false
	}
	j := i + 1
	for j < len(s) && s[j] >= 0x21 && s[j] <= 0x7e && s[j] != ';' {
		j++
	}
	if j >= len(s) || s[j] != ';' {
		return "", "", false
	}
	return s[:i], s[i+1 : j], true
}

// message takes a message of the program.
func (t *tspTracker) message(verb string, body []byte) {
	switch verb {
	case "q":
		var q struct {
			Q string `json:"q"`
		}
		if json.Unmarshal(body, &q) == nil && q.Q == "hello" {
			t.replies = append(t.replies, t.helloReply())
		}
	case "o":
		var o struct {
			ID, Mode, Title string
		}
		if json.Unmarshal(body, &o) != nil || o.ID == "" {
			return
		}
		if o.Mode == "" {
			o.Mode = "inline"
		}
		if o.Mode == "flow" {
			log.Printf("tsp: a flow surface, which is not drawn yet")
			return
		}
		if t.surfaces == nil {
			t.surfaces, t.nodes, t.parent = map[string]*Surface{}, map[string]map[string]*TSPNode{}, map[string]map[string]string{}
		}
		if old := t.surfaces[o.ID]; old != nil && old.Image != 0 {
			t.inject = append(t.inject, deleteAnchor(old.Image)...)
		} else if old == nil {
			t.order = append(t.order, o.ID)
		}
		sf := &Surface{ID: o.ID, Mode: o.Mode, Title: o.Title}
		if o.Mode == "inline" {
			// An anchor, and one row, until a window measures the
			// surface.
			t.nextImage++
			if t.nextImage < TSPImageFirst || t.nextImage > TSPImageLast {
				t.nextImage = TSPImageFirst
			}
			sf.Image, sf.Rows = t.nextImage, 1
			t.inject = append(t.inject, (anchor(sf.Image) + "\r\n")...)
		}
		t.surfaces[o.ID] = sf
		t.nodes[o.ID], t.parent[o.ID] = map[string]*TSPNode{}, map[string]string{}
		t.changed = true
	case "f":
		var f struct {
			SF  string            `json:"sf"`
			S   int64             `json:"s"`
			Ops []json.RawMessage `json:"ops"`
		}
		if json.Unmarshal(body, &f) != nil {
			return
		}
		if sf := t.surfaces[f.SF]; sf != nil {
			for _, op := range f.Ops {
				t.apply(sf, op)
			}
			t.changed = true
		}
		// Acked whether the surface is drawn or not, so that a program
		// whose surface is not never stalls.
		t.replies = append(t.replies, []byte(fmt.Sprintf("\x1b_tsp;e;{\"ev\":\"ack\",\"sf\":%q,\"s\":%d}\x1b\\", f.SF, f.S)))
	case "x":
		var x struct {
			ID   string `json:"id"`
			Keep bool   `json:"keep"`
		}
		if json.Unmarshal(body, &x) != nil {
			return
		}
		sf, ok := t.surfaces[x.ID]
		if !ok {
			return
		}
		delete(t.surfaces, x.ID)
		delete(t.nodes, x.ID)
		delete(t.parent, x.ID)
		for i, id := range t.order {
			if id == x.ID {
				t.order = append(t.order[:i], t.order[i+1:]...)
				break
			}
		}
		switch {
		case x.Keep && sf.Image != 0:
			// The last view stays where it is, drawn as it was, in the
			// rows the anchor keeps; the oldest kept goes when there are
			// many.
			sf.Closed = true
			t.kept = append(t.kept, sf)
			if len(t.kept) > tspMaxKept {
				t.inject = append(t.inject, deleteAnchor(t.kept[0].Image)...)
				t.kept = t.kept[1:]
			}
		case x.Keep:
			// A screen surface has no rows of its own: its last view
			// stays as text, where the program's output was.
			var b strings.Builder
			for _, l := range plain(sf.Root) {
				b.WriteString(l)
				b.WriteString("\r\n")
			}
			t.inject = append(t.inject, b.String()...)
		case sf.Image != 0:
			t.inject = append(t.inject, deleteAnchor(sf.Image)...)
		}
		t.changed = true
	}
}

// Anchors: an inline surface takes rows of the terminal, as the output
// it stands among does, and scrolls with them. The server places an
// image of its own in the first cell of the surface's first row
// (terminal.Options.AppImages: TSPImageFirst to TSPImageLast, for the
// windows' terminals too), which the terminal tracks through scrolling,
// reflow and its Mirror, and which the windows draw the surface from,
// over the terminal's width and the rows the surface has. The rows are
// blank lines the server feeds its terminal after the anchor, as output
// would be, more as the windows measure the surface taller: the program,
// in raw mode, prints nothing while its surface is live, so the cursor
// stays under them. tspMaxKept caps the surfaces kept after their close.
const (
	TSPImageFirst = 0x7fff0000
	TSPImageLast  = 0x7fffffff
	tspMaxKept    = 64
)

// anchor returns what places the anchor of an image in the first cell of
// the cursor's row, transmitting the image's pixel first, and leaves the
// cursor there.
func anchor(image uint32) string {
	return fmt.Sprintf("\r\x1b_Ga=t,f=32,s=1,v=1,i=%d,q=2;AAAAAA==\x1b\\\x1b_Ga=p,i=%d,p=1,c=1,r=1,C=1,q=2\x1b\\", image, image)
}

// deleteAnchor returns what deletes an anchor's image, and so its
// placement; its rows stay, blank.
func deleteAnchor(image uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", image)
}

// resize takes the rows a window measured a live surface to need: the
// surface takes them, with blank lines fed after its rows for the
// more, where the cursor is, under the last surface opened; fewer rows
// leave the lines there. It reports whether anything changed.
func (t *tspTracker) resize(sf string, rows int) bool {
	s := t.surfaces[sf]
	if s == nil || s.Image == 0 || rows <= 0 || rows == s.Rows {
		return false
	}
	if rows > s.Rows && len(t.order) > 0 && t.order[len(t.order)-1] == sf {
		t.inject = append(t.inject, strings.Repeat("\r\n", rows-s.Rows)...)
	}
	s.Rows = rows
	t.changed = true
	return true
}

// reanchor places the live surfaces' anchors again, after a server
// upgrade whose screen carried none: the cursor is under the last
// surface opened, and each anchor is that many rows above it, as far as
// the screen's top, which an anchor in the scrollback is placed at.
func (t *tspTracker) reanchor() {
	up := 0
	for i := len(t.order) - 1; i >= 0; i-- {
		s := t.surfaces[t.order[i]]
		if s.Image == 0 {
			continue
		}
		up += s.Rows
		t.inject = append(t.inject, fmt.Sprintf("\x1b7\x1b[%dA%s\x1b8", up, anchor(s.Image))...)
	}
}

// live returns the open surfaces, in the order opened, for a server
// upgrade to carry.
func (t *tspTracker) live() []*Surface {
	out := make([]*Surface, 0, len(t.order))
	for _, id := range t.order {
		out = append(out, t.surfaces[id])
	}
	return out
}

// restore takes the surfaces an upgrade carried, indexing their nodes.
func (t *tspTracker) restore(sfs []*Surface) {
	t.surfaces, t.nodes, t.parent = map[string]*Surface{}, map[string]map[string]*TSPNode{}, map[string]map[string]string{}
	t.order = t.order[:0]
	for _, sf := range sfs {
		t.surfaces[sf.ID] = sf
		t.order = append(t.order, sf.ID)
		nodes, parent := map[string]*TSPNode{}, map[string]string{}
		var index func(n *TSPNode, p string)
		index = func(n *TSPNode, p string) {
			nodes[n.ID], parent[n.ID] = n, p
			for _, c := range n.Children {
				index(c, n.ID)
			}
		}
		if sf.Root != nil {
			index(sf.Root, "")
		}
		t.nodes[sf.ID], t.parent[sf.ID] = nodes, parent
	}
	t.changed = true
}

// apply applies a frame op to a surface's document.
func (t *tspTracker) apply(sf *Surface, raw json.RawMessage) {
	var op []json.RawMessage
	if json.Unmarshal(raw, &op) != nil || len(op) == 0 {
		return
	}
	var name string
	if json.Unmarshal(op[0], &name) != nil {
		return
	}
	str := func(i int) string {
		var s string
		if i < len(op) {
			json.Unmarshal(op[i], &s)
		}
		return s
	}
	nodes, parents := t.nodes[sf.ID], t.parent[sf.ID]
	switch name {
	case "add":
		// ["add", id, parent, before, node]
		if len(op) < 5 {
			return
		}
		var n TSPNode
		if json.Unmarshal(op[4], &n) != nil || n.ID == "" {
			return
		}
		t.remove(sf, n.ID)
		parent, before := str(2), str(3)
		t.insert(sf, &n, parent, before)
		var index func(n *TSPNode, parent string)
		index = func(n *TSPNode, parent string) {
			nodes[n.ID], parents[n.ID] = n, parent
			for _, c := range n.Children {
				index(c, n.ID)
			}
		}
		index(&n, parent)
	case "set":
		// ["set", id, props]: a null prop deletes it.
		n := nodes[str(1)]
		if n == nil || len(op) < 3 {
			return
		}
		var props map[string]any
		if json.Unmarshal(op[2], &props) != nil {
			return
		}
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		for k, v := range props {
			if v == nil {
				delete(n.Props, k)
			} else {
				n.Props[k] = v
			}
		}
	case "text":
		// ["text", id, "append"|"replace", text]
		n := nodes[str(1)]
		if n == nil || len(op) < 4 {
			return
		}
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		text, _ := n.Props["text"].(string)
		if str(2) == "append" {
			n.Props["text"] = text + str(3)
		} else {
			n.Props["text"] = str(3)
		}
	case "splice":
		// ["splice", id, at, del, text]: at and del count UTF-16 units.
		n := nodes[str(1)]
		if n == nil || len(op) < 5 {
			return
		}
		var at, del int
		json.Unmarshal(op[2], &at)
		json.Unmarshal(op[3], &del)
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		text, _ := n.Props["text"].(string)
		n.Props["text"] = spliceUTF16(text, at, del, str(4))
	case "move":
		// ["move", id, parent, before]
		n := nodes[str(1)]
		if n == nil {
			return
		}
		t.remove(sf, n.ID)
		t.insert(sf, n, str(2), str(3))
		parents[n.ID] = str(2)
		nodes[n.ID] = n
	case "del":
		t.remove(sf, str(1))
		var drop func(id string)
		drop = func(id string) {
			if n := nodes[id]; n != nil {
				for _, c := range n.Children {
					drop(c.ID)
				}
			}
			delete(nodes, id)
			delete(parents, id)
		}
		drop(str(1))
	}
	// settle, focus, reveal, scroll, suspend, resume: hints, nothing kept.
}

// insert puts n under parent (the surface itself when parent is the
// surface's id or unknown) before the child before, at the end when "".
func (t *tspTracker) insert(sf *Surface, n *TSPNode, parent, before string) {
	if parent == sf.ID || parent == "" || t.nodes[sf.ID][parent] == nil {
		sf.Root = n
		return
	}
	p := t.nodes[sf.ID][parent]
	at := len(p.Children)
	if before != "" {
		for i, c := range p.Children {
			if c.ID == before {
				at = i
				break
			}
		}
	}
	p.Children = append(p.Children, nil)
	copy(p.Children[at+1:], p.Children[at:])
	p.Children[at] = n
}

// remove takes the node id out of its parent's children, keeping it in
// the index.
func (t *tspTracker) remove(sf *Surface, id string) {
	n := t.nodes[sf.ID][id]
	if n == nil {
		return
	}
	if sf.Root == n {
		sf.Root = nil
		return
	}
	p := t.nodes[sf.ID][t.parent[sf.ID][id]]
	if p == nil {
		return
	}
	for i, c := range p.Children {
		if c == n {
			p.Children = append(p.Children[:i], p.Children[i+1:]...)
			return
		}
	}
}

// spliceUTF16 replaces del UTF-16 units of s at at with text.
func spliceUTF16(s string, at, del int, text string) string {
	// Positions in UTF-16 units map to byte offsets in s.
	units := 0
	start, end := -1, -1
	for i, r := range s {
		if units >= at && start < 0 {
			start = i
		}
		if units >= at+del && end < 0 {
			end = i
		}
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	if start < 0 {
		start = len(s)
	}
	if end < 0 {
		end = len(s)
	}
	return s[:start] + text + s[end:]
}

// helloReply is the reply to the program's hello query.
func (t *tspTracker) helloReply() []byte {
	r := map[string]any{
		"r": "hello", "v": 1, "term": "gorex", "ver": "0.1.0",
		"kinds": tspKinds, "features": []string{},
		"apc": tspAPC, "credits": tspCredits,
		"cols": t.cols, "cell": map[string]int{"w": t.cellW, "h": t.cellH},
		"dark": t.dark, "reduceMotion": false,
	}
	b, _ := json.Marshal(r)
	return append(append([]byte("\x1b_tsp;r;"), b...), "\x1b\\"...)
}

// list returns the surfaces the windows draw: the kept ones, oldest
// first, then the open ones in the order opened.
func (t *tspTracker) list() []*Surface {
	out := make([]*Surface, 0, len(t.kept)+len(t.order))
	out = append(out, t.kept...)
	for _, id := range t.order {
		out = append(out, t.surfaces[id])
	}
	return out
}

// event returns what tells the program an event of a window, when the
// event names a surface of the session.
func (t *tspTracker) event(ev json.RawMessage) ([]byte, error) {
	var e struct {
		Ev string `json:"ev"`
		SF string `json:"sf"`
	}
	if json.Unmarshal(ev, &e) != nil || e.Ev == "" || e.Ev == "ack" {
		return nil, fmt.Errorf("not an event")
	}
	if _, ok := t.surfaces[e.SF]; !ok {
		return nil, fmt.Errorf("no surface %q", e.SF)
	}
	return append(append([]byte("\x1b_tsp;e;"), bytes.TrimSpace(ev)...), "\x1b\\"...), nil
}

// darkTheme reports whether a theme's background is dark.
func darkTheme(t *terminal.Theme) bool {
	bg := t.Background
	return 0.299*float64(bg.R)+0.587*float64(bg.G)+0.114*float64(bg.B) < 128
}

// plain renders a surface's document as text, by the Tern SDK's Plain
// rules for the kinds drawn: a card is its head and status over its body,
// indented; a row joins its children on one line; md and text are their
// text; a badge is bracketed. Unknown kinds show their children.
func plain(n *TSPNode) []string {
	if n == nil {
		return nil
	}
	if hidden, _ := n.Props["hidden"].(bool); hidden {
		return nil
	}
	children := func() []string {
		var lines []string
		for _, c := range n.Children {
			lines = append(lines, plain(c)...)
		}
		return lines
	}
	text, _ := n.Props["text"].(string)
	switch n.Kind {
	case "row":
		var parts []string
		for _, c := range n.Children {
			if l := plain(c); len(l) > 0 {
				parts = append(parts, strings.Join(l, " "))
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return []string{strings.Join(parts, " ")}
	case "card":
		head, _ := n.Props["head"].(string)
		if status, _ := n.Props["status"].(string); status != "" {
			head = strings.TrimSpace(head + " (" + status + ")")
		}
		body := children()
		if head == "" {
			return body
		}
		lines := []string{head}
		for _, l := range body {
			lines = append(lines, "  "+l)
		}
		return lines
	case "text", "md", "code":
		if text == "" {
			return nil
		}
		return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	case "badge":
		if text == "" {
			return nil
		}
		return []string{"[" + text + "]"}
	}
	return children()
}
