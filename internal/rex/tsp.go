package rex

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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
var tspKinds = []string{"col", "row", "card", "section", "text", "md", "badge", "kv", "list", "item", "code", "table", "progress", "spinner", "status", "rule", "spacer", "kbd", "icon", "editor", "meter", "seg", "image", "elapsed", "rate", "ansi", "checklist", "tree", "overlay", "input", "diff", "chart", "agent", "toast", "tool", "picker"}

const (
	tspAPC     = 65536 // the largest body a message carries
	tspCredits = 2     // frames the program may send unacknowledged
	// tspMaxBody caps a message's body, and tspMaxJoined a chunked one
	// reassembled (a blob of tspMaxBlob, in base64); a longer one is
	// dropped whole.
	tspMaxBody   = 4 << 20
	tspMaxJoined = 24 << 20
	// tspMaxBlob is the largest blob, decoded, as the protocol has it,
	// and tspMaxBlobs how much a session keeps of them: the oldest go.
	tspMaxBlob  = 16 << 20
	tspMaxBlobs = 64 << 20
)

// TSPBlob is a program's blob: an image its surfaces show by id.
type TSPBlob struct {
	ID, Mime string
	Data     []byte
}

// addBlob keeps a blob, by the hex SHA-256 of its bytes, letting go of
// the oldest past tspMaxBlobs.
func (t *tspTracker) addBlob(mime string, data []byte) {
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	if t.blobs == nil {
		t.blobs = map[string]*TSPBlob{}
	}
	if t.blobs[id] != nil {
		return
	}
	t.blobs[id] = &TSPBlob{ID: id, Mime: mime, Data: data}
	t.blobOrder = append(t.blobOrder, id)
	t.blobBytes += len(data)
	for t.blobBytes > tspMaxBlobs && len(t.blobOrder) > 1 {
		old := t.blobOrder[0]
		t.blobOrder = t.blobOrder[1:]
		t.blobBytes -= len(t.blobs[old].Data)
		delete(t.blobs, old)
	}
}

// blobList returns the blobs, oldest first.
func (t *tspTracker) blobList() []*TSPBlob {
	out := make([]*TSPBlob, 0, len(t.blobOrder))
	for _, id := range t.blobOrder {
		out = append(out, t.blobs[id])
	}
	return out
}

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
	// Dock is how many rows the dock region pinned under a live
	// surface's content takes, which its rows reach over while it is
	// live; kept, the surface has its content's rows only.
	Dock int `json:"dock,omitempty"`
	// Closed tells a surface kept after its program closed it: drawn as
	// it was, taking no events; closedAt is when, for its rows to grow
	// once (resize).
	Closed   bool `json:"closed,omitempty"`
	closedAt time.Time
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
	// doc indexes the surfaces' documents and applies their ops.
	doc tspDoc
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
	// spare is how many rows the cursor is under the last opened inline
	// surface's content, and under its rows anchor: told by the session
	// before each read, for a close to take them back (reclaim).
	spare, under int
	// growing are the rows kept surfaces are to get (resize, grow).
	growing []tspGrowth
	// changed tells that a surface changed since the windows were told;
	// version counts those changes, which each window is told of
	// (session.frame): pending is what changed since the last, which
	// commit makes the update of the next version, and updates the
	// last ones made, encoded, for the windows that lag.
	changed bool
	version uint64
	pending SurfaceUpdate
	updates []tspUpdate
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
	chunkParams         map[string]map[string]string

	// blobs are the program's blobs (images), by id, the hex SHA-256 of
	// their bytes, blobOrder their ids oldest first and blobBytes their
	// size together, which tspMaxBlobs caps.
	blobs     map[string]*TSPBlob
	blobOrder []string
	blobBytes int
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
			t.chunks, t.chunkParams = map[string][]byte{}, map[string]map[string]string{}
		}
		// The first chunk carries the message's params.
		if _, ok := t.chunkParams[c]; !ok {
			t.chunkParams[c] = params
		}
		joined := append(t.chunks[c], rest...)
		if params["m"] == "1" {
			if len(joined) <= tspMaxJoined {
				t.chunks[c] = joined
			} else {
				delete(t.chunks, c)
				delete(t.chunkParams, c)
			}
			return
		}
		params = t.chunkParams[c]
		delete(t.chunks, c)
		delete(t.chunkParams, c)
		rest = string(joined)
	}
	t.message(verb, params, []byte(rest))
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
func (t *tspTracker) message(verb string, params map[string]string, body []byte) {
	switch verb {
	case "q":
		var q struct {
			Q   string   `json:"q"`
			IDs []string `json:"ids"`
		}
		if json.Unmarshal(body, &q) != nil {
			return
		}
		switch q.Q {
		case "hello":
			t.replies = append(t.replies, t.helloReply())
		case "blobs":
			// Which of the program's blobs the session still holds.
			have := []string{}
			for _, id := range q.IDs {
				if t.blobs[id] != nil {
					have = append(have, id)
				}
			}
			b, _ := json.Marshal(map[string]any{"r": "blobs", "have": have})
			t.replies = append(t.replies, append(append([]byte("\x1b_tsp;r;"), b...), "\x1b\\"...))
		}
	case "b":
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
		if err != nil || len(data) > tspMaxBlob {
			return
		}
		t.addBlob(params["mime"], data)
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
		// A flow surface is laid out among the output as an inline one
		// is, and stays as output does (its close keeps it).
		if t.surfaces == nil {
			t.surfaces = map[string]*Surface{}
		}
		if old := t.surfaces[o.ID]; old != nil {
			t.doc.drop(old)
			if old.Image != 0 {
				t.inject = append(t.inject, deleteAnchor(old.Image)...)
			}
		} else {
			t.order = append(t.order, o.ID)
		}
		sf := &Surface{ID: o.ID, Mode: o.Mode, Title: o.Title}
		if o.Mode == "inline" || o.Mode == "flow" {
			// An anchor, and one row, until a window measures the
			// surface.
			t.nextImage++
			if t.nextImage < TSPImageFirst || t.nextImage > TSPImageLast {
				t.nextImage = TSPImageFirst
			}
			sf.Image, sf.Rows = t.nextImage, 1
			t.inject = append(t.inject, (anchor(sf.Image) + "\r\n")...)
		}
		// The surface is a column of the nodes added to it: its root,
		// by its own id, which the adds name as their parent.
		sf.Root = &TSPNode{ID: o.ID, Kind: "col"}
		t.surfaces[o.ID] = sf
		t.doc.index(sf)
		if b, err := json.Marshal(sf); err == nil {
			t.pending.Opens = append(t.pending.Opens, b)
		}
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
				t.doc.apply(sf, op)
			}
			t.pending.Frames = append(t.pending.Frames, SurfaceFrame{SF: sf.ID, Image: sf.Image, Ops: f.Ops})
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
		t.doc.drop(sf)
		for i, id := range t.order {
			if id == x.ID {
				t.order = append(t.order[:i], t.order[i+1:]...)
				break
			}
		}
		switch {
		case (x.Keep || sf.Mode == "flow") && sf.Image != 0:
			// The last view stays where it is, drawn as it was, in the
			// rows the anchor keeps, and the rows padded under them go
			// (reclaim); the oldest kept goes when there are many.
			sf.Closed, sf.closedAt = true, time.Now()
			t.kept = append(t.kept, sf)
			t.inject = append(t.inject, reclaim(t.spare)...)
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
			t.inject = append(t.inject, (reclaim(t.under) + deleteAnchor(sf.Image))...)
		}
		t.spare, t.under = 0, 0
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

// reclaim returns what takes back n blank rows above the cursor: up over
// them and clear from there down. Nothing for none.
func reclaim(n int) string {
	if n <= 0 {
		return ""
	}
	return "\x1b[" + strconv.Itoa(n) + "A\r\x1b[J"
}

// spareRows tells the tracker the rows the cursor is under the last
// opened inline surface's content (spare) and under its anchor (under),
// from where the anchors are and the cursor's row, as the session reads
// output: a close in it takes them back.
func (t *tspTracker) spareRows(placements map[uint32]int, cursor int) {
	t.spare, t.under = 0, 0
	if len(t.order) == 0 {
		return
	}
	s := t.surfaces[t.order[len(t.order)-1]]
	if s == nil || s.Image == 0 {
		return
	}
	row, ok := placements[s.Image]
	if !ok {
		return
	}
	// Rows above the screen are the scrollback's, which stay.
	t.under = max(0, min(cursor, cursor-row))
	t.spare = max(0, min(cursor, cursor-row-s.Rows))
}

// sweep drops the kept surfaces whose anchors the terminal no longer
// has, as after a clear: they are gone from the windows with it. It
// reports whether any went.
func (t *tspTracker) sweep(placements map[uint32]int) bool {
	n := 0
	for _, s := range t.kept {
		if _, ok := placements[s.Image]; ok {
			t.kept[n] = s
			n++
		}
	}
	if n == len(t.kept) {
		return false
	}
	clear(t.kept[n:])
	t.kept = t.kept[:n]
	t.changed = true
	return true
}

// resize takes the rows a window measured a surface to need, its content
// and the dock pinned under it. The session's terminal gets the lines
// for a live one (padding). A kept one grows once, when a window
// measures it taller just after its close: its program's last frame
// and its close come in one write, before any window measured the
// frame, so the rows it was kept with are a frame old; the terminal
// gets the rows it lacks inserted under it (grow). It reports whether
// anything changed.
func (t *tspTracker) resize(sf string, rows, dock int) bool {
	s := t.surfaces[sf]
	if s == nil {
		for _, k := range t.kept {
			if k.ID == sf && rows > k.Rows && time.Since(k.closedAt) < tspGrowFor {
				t.growing = append(t.growing, tspGrowth{k.Image, rows - k.Rows})
				k.Rows = rows
				k.closedAt = time.Time{}
				t.changed = true
				return true
			}
		}
		return false
	}
	if s.Image == 0 || rows <= 0 || dock < 0 || rows == s.Rows && dock == s.Dock {
		return false
	}
	s.Rows, s.Dock = rows, dock
	t.changed = true
	return true
}

// tspGrowth is rows a kept surface is to get under its anchor.
type tspGrowth struct {
	image uint32
	rows  int
}

// tspGrowFor is how long after its close a kept surface may grow.
const tspGrowFor = 2 * time.Second

// grow returns what inserts the rows kept surfaces are to get under
// their anchors, given where the anchors are, the cursor's row and the
// screen's rows, and leaves the cursor as many rows further down: the
// rows between the surface's end and the cursor move down, and the
// screen scrolls first when they would go past its end. Right after a
// close nothing is under the cursor, which the close erased, so nothing
// is lost. A surface whose end is above the screen does not grow.
func (t *tspTracker) grow(placements map[uint32]int, cursor, screen int) string {
	var b strings.Builder
	for _, g := range t.growing {
		row, ok := placements[g.image]
		var s *Surface
		for _, k := range t.kept {
			if k.Image == g.image {
				s = k
			}
		}
		if !ok || s == nil {
			continue
		}
		end := row + s.Rows - g.rows
		if end < 0 || end > cursor {
			continue
		}
		if over := cursor + g.rows - (screen - 1); over > 0 {
			fmt.Fprintf(&b, "\x1b[%d;1H%s", screen, strings.Repeat("\n", over))
			end, cursor = end-over, cursor-over
			if end < 0 {
				continue
			}
		}
		fmt.Fprintf(&b, "\x1b[%d;1H\x1b[%dL\x1b[%d;1H", end+1, g.rows, cursor+g.rows+1)
		cursor += g.rows
	}
	t.growing = nil
	return b.String()
}

// padding returns how many lines the terminal needs fed for the last
// opened inline surface to have its rows above the cursor, given where
// the anchors are (placements: image id → row), the cursor's row and
// the screen's rows: the program prints nothing while its surface is
// live, but what moves the cursor otherwise (a reflow, an upgrade,
// output that came anyway) is made up for. A live surface with a dock
// (a composer pinned at the bottom) takes the screen down from its
// anchor, as its program would in a terminal of its own, so that no
// terminal shows between it and its dock. Negative, it is how many rows
// the surface has spare under it, which are blank, and as many as are
// on the screen are taken back, so that what follows the surface comes
// right after it.
func (t *tspTracker) padding(placements map[uint32]int, cursor, screen int) int {
	if len(t.order) == 0 {
		return 0
	}
	s := t.surfaces[t.order[len(t.order)-1]]
	if s == nil || s.Image == 0 {
		return 0
	}
	row, ok := placements[s.Image]
	if !ok {
		return 0
	}
	rows := s.Rows
	if !s.Closed {
		rows += s.Dock
		if s.docked() {
			// Down to the screen's last row, where the cursor stays:
			// a row further would scroll, and the anchor with it,
			// without end.
			rows = max(rows, screen-1-row)
		}
	}
	n := rows - (cursor - row)
	if n < 0 {
		// Rows above the screen are the scrollback's, which stay.
		n = max(n, -cursor)
	}
	return n
}

// docked reports whether the surface has a dock region, which windows
// pin at the bottom of the pane.
func (s *Surface) docked() bool {
	if s.Root == nil {
		return false
	}
	for _, n := range s.Root.Children {
		if n.ID == "dock" {
			return true
		}
	}
	return false
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

// resized returns what tells the program each live surface's width and
// cell size, after they changed.
func (t *tspTracker) resized() [][]byte {
	var out [][]byte
	for _, id := range t.order {
		out = append(out, []byte(fmt.Sprintf("\x1b_tsp;e;{\"ev\":\"resize\",\"sf\":%q,\"cols\":%d,\"cell\":{\"w\":%d,\"h\":%d},\"visible\":true}\x1b\\", id, t.cols, t.cellW, t.cellH)))
	}
	return out
}

// themed returns what tells the program each live surface's appearance,
// after it switched.
func (t *tspTracker) themed() [][]byte {
	var out [][]byte
	for _, id := range t.order {
		out = append(out, []byte(fmt.Sprintf("\x1b_tsp;e;{\"ev\":\"theme\",\"sf\":%q,\"dark\":%v}\x1b\\", id, t.dark)))
	}
	return out
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
	t.surfaces, t.doc = map[string]*Surface{}, tspDoc{}
	t.order = t.order[:0]
	for _, sf := range sfs {
		t.surfaces[sf.ID] = sf
		t.order = append(t.order, sf.ID)
		t.doc.index(sf)
	}
	t.changed = true
}

// tspUpdate is an update made of a version's changes, encoded once for
// every window.
type tspUpdate struct {
	v    uint64
	json []byte
}

// tspMaxUpdates caps the updates kept for windows that lag: one further
// behind gets the surfaces whole.
const tspMaxUpdates = 256

// commit makes what changed since the last version the update of the
// next one, with the surfaces there are now, and reports the version.
func (t *tspTracker) commit() uint64 {
	if t.version == 0 {
		// Windows that have the surfaces have a version; 0 is none.
		t.version = 1
	}
	if !t.changed {
		return t.version
	}
	t.changed = false
	t.version++
	u := t.pending
	u.V = t.version
	u.List = make([]Surface, 0, len(t.kept)+len(t.order))
	for _, sf := range t.list() {
		meta := *sf
		meta.Root = nil
		u.List = append(u.List, meta)
	}
	t.pending = SurfaceUpdate{}
	if b, err := json.Marshal(u); err == nil {
		if len(t.updates) == tspMaxUpdates {
			t.updates = append(t.updates[:0], t.updates[1:]...)
		}
		t.updates = append(t.updates, tspUpdate{t.version, b})
	}
	return t.version
}

// updatesSince returns the updates after version v, as a JSON array; ok
// is false when some of them are no longer kept, or v is none.
func (t *tspTracker) updatesSince(v uint64) (b []byte, ok bool) {
	if v == 0 || len(t.updates) == 0 || t.updates[0].v > v+1 {
		return nil, false
	}
	b = append(b, '[')
	n := 0
	for _, u := range t.updates {
		if u.v <= v {
			continue
		}
		if n > 0 {
			b = append(b, ',')
		}
		b = append(b, u.json...)
		n++
	}
	return append(b, ']'), true
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
		// dock: a surface's dock region stays at the bottom of the pane;
		// blobs: images come as blobs, which the session keeps; edit,
		// undo, send: the windows edit the program's fields natively,
		// telling it what changed; flow: flow surfaces stay among the
		// output; scroll: the windows act on scroll ops.
		"kinds": tspKinds, "features": []string{"dock", "blobs", "edit", "undo", "send", "flow", "scroll"},
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

// patchItems applies a picker's itemsAdd (upserts by id) and itemsDel
// (removals by id) to its items, the catalogue a program sends once and
// patches after, and drops them.
func patchItems(props map[string]any) {
	add, hasAdd := props["itemsAdd"].([]any)
	del, hasDel := props["itemsDel"].([]any)
	if !hasAdd && !hasDel {
		return
	}
	delete(props, "itemsAdd")
	delete(props, "itemsDel")
	items, _ := props["items"].([]any)
	id := func(v any) string {
		m, _ := v.(map[string]any)
		s, _ := m["id"].(string)
		return s
	}
	gone := map[string]bool{}
	for _, d := range del {
		if s, ok := d.(string); ok {
			gone[s] = true
		}
	}
	at := map[string]int{}
	out := make([]any, 0, len(items)+len(add))
	for _, it := range items {
		if s := id(it); !gone[s] {
			at[s] = len(out)
			out = append(out, it)
		}
	}
	for _, it := range add {
		s := id(it)
		if i, ok := at[s]; ok {
			out[i] = it
		} else {
			at[s] = len(out)
			out = append(out, it)
		}
	}
	props["items"] = out
}
