package rex

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
)

// What tspdemo sends (recorded through the SDK): the hello query with
// DA1, the modes it sets, the open, and two frames.
const (
	tspHello  = "\x1b_tsp;q;{\"q\":\"hello\",\"v\":[1],\"app\":\"tspdemo\"}\x1b\\\x1b[c"
	tspOpen   = "\x1b_tsp;o;{\"id\":\"s1\",\"mode\":\"screen\",\"title\":\"tspdemo\"}\x1b\\"
	tspFrame1 = "\x1b_tsp;f;{\"sf\":\"s1\",\"s\":1,\"ops\":[[\"add\",\"main\",\"s1\",null,{\"id\":\"main\",\"k\":\"col\",\"c\":[{\"id\":\"main.card\",\"k\":\"card\",\"p\":{\"head\":\"GoRex TSP demo\",\"key\":\"card\",\"status\":\"running\"},\"c\":[{\"id\":\"main.card.body\",\"k\":\"md\",\"p\":{\"key\":\"body\",\"text\":\"line\"}},{\"id\":\"main.card.row\",\"k\":\"row\",\"p\":{\"key\":\"row\"},\"c\":[{\"id\":\"main.card.row.button\",\"k\":\"badge\",\"p\":{\"actions\":{\"click\":\"click\"},\"key\":\"button\",\"text\":\"Click me\"}},{\"id\":\"main.card.row.count\",\"k\":\"text\",\"p\":{\"key\":\"count\",\"text\":\"clicked 0 times, tick 0\"}}]}]}]}]]}\x1b\\"
	tspFrame2 = "\x1b_tsp;f;{\"sf\":\"s1\",\"s\":2,\"ops\":[[\"set\",\"main.card\",{\"status\":\"done\"}],[\"text\",\"main.card.row.count\",\"replace\",\"clicked 0 times, tick 1\"]]}\x1b\\"
	tspClose  = "\x1b_tsp;x;{\"id\":\"s1\",\"keep\":false}\x1b\\"
)

// A surface takes no more rows than Surface.Most, so that the
// scrollback keeps its anchor: a live one its content's and its dock's
// together, a kept one's growth. The surfaces there are and those opened
// after carry it.
func TestTSPMost(t *testing.T) {
	ch := tspTracker{on: true, cols: 50}
	ch.setMost(30)
	ch.feed([]byte("\x1b_tsp;o;{\"id\":\"s1\",\"mode\":\"inline\"}\x1b\\"))
	s := ch.surfaces["s1"]
	if s == nil || s.Most != 30 {
		t.Fatalf("opened: %+v", s)
	}
	if !ch.resize("s1", 100, 5) || s.Rows != 25 || s.Dock != 5 {
		t.Errorf("content and dock over Most: rows %d dock %d", s.Rows, s.Dock)
	}
	if !ch.resize("s1", 100, 50) || s.Rows != 1 || s.Dock != 29 {
		t.Errorf("a dock over Most: rows %d dock %d", s.Rows, s.Dock)
	}
	ch.changed = false
	if ch.setMost(40); s.Most != 40 || !ch.changed {
		t.Errorf("Most set again: %d, changed %v", s.Most, ch.changed)
	}
	ch.resize("s1", 10, 0)
	ch.feed([]byte("\x1b_tsp;x;{\"id\":\"s1\",\"keep\":true}\x1b\\"))
	if len(ch.kept) != 1 || !ch.resize("s1", 500, 0) || ch.kept[0].Rows != 40 {
		t.Errorf("a kept surface grew past Most: %+v", ch.kept)
	}
}

func TestTSPTracker(t *testing.T) {
	tr := tspTracker{on: true, cols: 100, cellW: 8, cellH: 16, dark: true}
	// The hello is answered, before what follows it, and the messages
	// are taken out of the output: DA1 and the modes go to the terminal.
	out := string(tr.feed([]byte(tspHello + "\x1b[?2004h")))
	if out != "\x1b[c\x1b[?2004h" {
		t.Errorf("output after hello: %q", out)
	}
	if len(tr.replies) != 1 || !strings.HasPrefix(string(tr.replies[0]), "\x1b_tsp;r;{") {
		t.Fatalf("replies after hello: %q", tr.replies)
	}
	var hello map[string]any
	reply := string(tr.replies[0])
	if err := json.Unmarshal([]byte(reply[len("\x1b_tsp;r;"):len(reply)-2]), &hello); err != nil {
		t.Fatalf("hello reply %q: %v", reply, err)
	}
	if hello["r"] != "hello" || hello["cols"] != 100.0 || hello["dark"] != true || hello["credits"] != 2.0 {
		t.Errorf("hello reply %v", hello)
	}
	tr.replies = nil

	// A message split across reads, at the worst place: inside the APC
	// opener.
	all := tspOpen + tspFrame1
	for _, cut := range []int{1, 2, 5, 20, len(tspOpen), len(all) - 3} {
		tr2 := tspTracker{on: true}
		var out []byte
		out = append(out, tr2.feed([]byte(all[:cut]))...)
		out = append(out, tr2.feed([]byte(all[cut:]))...)
		if len(out) != 0 {
			t.Errorf("cut at %d: output %q", cut, out)
		}
		if tr2.surfaces["s1"] == nil || tr2.surfaces["s1"].Root == nil {
			t.Errorf("cut at %d: no surface", cut)
		}
	}

	// The open and the first frame make the document; the frame is acked.
	if out := tr.feed([]byte(tspOpen + tspFrame1)); len(out) != 0 {
		t.Errorf("output of open+frame: %q", out)
	}
	sf := tr.surfaces["s1"]
	if sf == nil || sf.Mode != "screen" || sf.Title != "tspdemo" || !tr.changed {
		t.Fatalf("surface after open: %+v changed %v", sf, tr.changed)
	}
	if len(tr.replies) != 1 || string(tr.replies[0]) != "\x1b_tsp;e;{\"ev\":\"ack\",\"sf\":\"s1\",\"s\":1}\x1b\\" {
		t.Errorf("ack: %q", tr.replies)
	}
	tr.replies = nil
	// The surface's root is a column of the program's top-level nodes,
	// by the surface's id, which the adds name as their parent.
	if sf.Root.ID != "s1" || sf.Root.Kind != "col" || len(sf.Root.Children) != 1 || sf.Root.Children[0].ID != "main" {
		t.Fatalf("root: %+v", sf.Root)
	}
	card := sf.Root.Children[0].Children[0]
	if card.Kind != "card" || card.Props["status"] != "running" || len(card.Children) != 2 {
		t.Fatalf("document: %+v", sf.Root.Children[0])
	}
	count := card.Children[1].Children[1]
	if count.ID != "main.card.row.count" || count.Props["text"] != "clicked 0 times, tick 0" {
		t.Errorf("count node %+v", count)
	}

	// The second frame sets and replaces text in place.
	tr.changed = false
	tr.feed([]byte(tspFrame2))
	if card.Props["status"] != "done" || count.Props["text"] != "clicked 0 times, tick 1" || !tr.changed {
		t.Errorf("after frame 2: status %v, count %v, changed %v", card.Props["status"], count.Props["text"], tr.changed)
	}
	if len(tr.replies) != 1 || !strings.Contains(string(tr.replies[0]), "\"s\":2") {
		t.Errorf("ack 2: %q", tr.replies)
	}

	// Other ops: append, del, add before a sibling, move.
	ops := func(ops string) {
		tr.feed([]byte("\x1b_tsp;f;{\"sf\":\"s1\",\"s\":3,\"ops\":[" + ops + "]}\x1b\\"))
	}
	ops(`["text","main.card.row.count","append","!"]`)
	if count.Props["text"] != "clicked 0 times, tick 1!" {
		t.Errorf("append: %v", count.Props["text"])
	}
	ops(`["add","x","main.card.row","main.card.row.count",{"id":"x","k":"text","p":{"text":"X"}}]`)
	row := card.Children[1]
	if len(row.Children) != 3 || row.Children[1].ID != "x" {
		t.Errorf("add before: %v", ids(row.Children))
	}
	ops(`["move","x","main.card",""]`)
	if len(row.Children) != 2 || card.Children[len(card.Children)-1].ID != "x" {
		t.Errorf("move: row %v card %v", ids(row.Children), ids(card.Children))
	}
	ops(`["del","main.card.row"]`)
	if len(card.Children) != 2 || tr.doc.nodes["s1#0"]["main.card.row.count"] != nil {
		t.Errorf("del: %v, count indexed %v", ids(card.Children), tr.doc.nodes["s1#0"]["main.card.row.count"] != nil)
	}
	ops(`["splice","main.card.body",0,4,"Line"]`)
	if body := card.Children[0]; body.Props["text"] != "Line" {
		t.Errorf("splice: %v", body.Props["text"])
	}

	// An event of a window names the surface; the close drops it.
	if b, err := tr.event(json.RawMessage(`{"ev":"action","sf":"s1","id":"main.card.row.button","act":"click"}`)); err != nil || !strings.HasPrefix(string(b), "\x1b_tsp;e;{") {
		t.Errorf("event: %q %v", b, err)
	}
	if _, err := tr.event(json.RawMessage(`{"ev":"action","sf":"s9","id":"x","act":"click"}`)); err == nil {
		t.Error("an event of an unknown surface went through")
	}
	tr.feed([]byte(tspClose))
	if len(tr.surfaces) != 0 || len(tr.list()) != 0 || len(tr.kept) != 0 {
		t.Errorf("after close: %v, kept %v", tr.surfaces, tr.kept)
	}

	// Off, nothing is taken out or answered.
	off := tspTracker{}
	if out := string(off.feed([]byte(tspHello))); out != tspHello || len(off.replies) != 0 {
		t.Errorf("off: %q %q", out, off.replies)
	}

	// A chunked body is reassembled; an inline surface gets an anchor in
	// the output, in the first cell of its row, and one row.
	ch := tspTracker{on: true, cols: 50}
	body := `{"id":"s2","mode":"screen"}`
	ch.feed([]byte("\x1b_tsp;o;c=a;m=1;" + body[:10] + "\x1b\\" + "\x1b_tsp;o;c=a;" + body[10:] + "\x1b\\"))
	if ch.surfaces["s2"] == nil || ch.surfaces["s2"].Image != 0 {
		t.Errorf("a chunked screen open: %+v", ch.surfaces["s2"])
	}
	out = string(ch.feed([]byte("\x1b_tsp;o;{\"id\":\"s4\",\"mode\":\"inline\"}\x1b\\")))
	s4 := ch.surfaces["s4"]
	if s4 == nil || s4.Image != TSPImageFirst || s4.Rows != 1 {
		t.Fatalf("inline %+v", s4)
	}
	if out != anchor(TSPImageFirst)+"\r\n" {
		t.Errorf("anchor: %q", out)
	}
	// A window measured it at 6 rows, then 4, with a dock of 2: the
	// surface has them.
	if !ch.resize("s4", 6, 0) || s4.Rows != 6 || !ch.resize("s4", 4, 2) || s4.Rows != 4 || s4.Dock != 2 || !ch.resize("s4", 4, 0) || s4.Dock != 0 {
		t.Errorf("resize: rows %d dock %d", s4.Rows, s4.Dock)
	}
	// The terminal lacks lines under the anchor while the cursor is
	// fewer rows below it than the surface has, and has some spare when
	// more, as many as are on the screen; an anchor not found, none.
	for _, c := range []struct{ anchor, cursor, want int }{{10, 11, 3}, {10, 14, 0}, {10, 20, -6}, {-5, 2, -2}, {-5, -2, 1}, {-20, 3, -3}} {
		if got := ch.padding(map[uint32]int{TSPImageFirst: c.anchor}, c.cursor, 40); got != c.want {
			t.Errorf("padding with the anchor at %d, the cursor at %d: %d, want %d", c.anchor, c.cursor, got, c.want)
		}
	}
	if ch.padding(map[uint32]int{}, 11, 40) != 0 {
		t.Error("padding without the anchor")
	}
	// A live surface with a dock takes the screen down from its anchor,
	// its own rows when they are more; closed, only its rows.
	s4.Root = &TSPNode{Children: []*TSPNode{{ID: "main"}, {ID: "dock"}}}
	for _, c := range []struct{ anchor, cursor, screen, want int }{{10, 11, 40, 28}, {10, 14, 12, 0}, {-5, 2, 40, 37}, {0, 30, 40, 9}, {0, 39, 40, 0}} {
		if got := ch.padding(map[uint32]int{TSPImageFirst: c.anchor}, c.cursor, c.screen); got != c.want {
			t.Errorf("docked padding with the anchor at %d, the cursor at %d, %d rows: %d, want %d", c.anchor, c.cursor, c.screen, got, c.want)
		}
	}
	// Its dock's rows count while it is live (4 + 3 on a screen too
	// short to reach further), not once it is kept.
	s4.Dock = 3
	if got := ch.padding(map[uint32]int{TSPImageFirst: 10}, 11, 12); got != 6 {
		t.Errorf("live padding with a dock: %d, want 6", got)
	}
	s4.Closed = true
	if got := ch.padding(map[uint32]int{TSPImageFirst: 10}, 20, 40); got != -6 {
		t.Errorf("closed docked padding: %d, want -6", got)
	}
	s4.Closed, s4.Root, s4.Dock = false, nil, 0
	if ch.resize("s4", 4, 0) || ch.resize("s2", 3, 0) || ch.resize("s9", 3, 0) {
		t.Error("a resize to the same rows, of a screen surface or of none went through")
	}
	// After an upgrade, the anchors are placed again from the cursor,
	// under the last surface's rows.
	ch.reanchor()
	if string(ch.inject) != "\x1b7\x1b[4A"+anchor(TSPImageFirst)+"\x1b8" {
		t.Errorf("reanchor: %q", ch.inject)
	}
	ch.inject = nil
	// Closed with keep, it stays, drawn as it was, among the kept, and
	// in the list before the open ones, and the rows padded under its
	// content are taken back; closed without, its anchor goes with all
	// its rows.
	ch.spareRows(map[uint32]int{TSPImageFirst: 10}, 40)
	if ch.spare != 26 || ch.under != 30 {
		t.Errorf("spare rows: %d under the content, %d under the anchor", ch.spare, ch.under)
	}
	if out := string(ch.feed([]byte("\x1b_tsp;x;{\"id\":\"s4\",\"keep\":true}\x1b\\"))); out != reclaim(26) {
		t.Errorf("close with keep: %q", out)
	}
	if len(ch.kept) != 1 || !ch.kept[0].Closed || ch.kept[0].Image != TSPImageFirst || len(ch.list()) != 2 || ch.list()[0] != ch.kept[0] {
		t.Errorf("kept: %+v list %v", ch.kept, ch.list())
	}
	ch.feed([]byte("\x1b_tsp;o;{\"id\":\"s5\",\"mode\":\"inline\"}\x1b\\"))
	ch.spareRows(map[uint32]int{TSPImageFirst: 10, TSPImageFirst + 1: 20}, 25)
	if out := string(ch.feed([]byte("\x1b_tsp;x;{\"id\":\"s5\"}\x1b\\"))); out != reclaim(5)+deleteAnchor(TSPImageFirst+1) {
		t.Errorf("close without keep: %q", out)
	}
	// Just after its close a kept surface grows once, when a window
	// measures it taller (its rows were a frame old): the terminal gets
	// the rows inserted under it, the screen scrolled first when they
	// would go past its end; not smaller, not later, not twice.
	k := ch.kept[0]
	if ch.resize("s4", 3, 0) || k.Rows != 4 {
		t.Error("a kept surface shrank")
	}
	if !ch.resize("s4", 6, 0) || k.Rows != 6 || len(ch.growing) != 1 {
		t.Errorf("a kept surface did not grow: rows %d, growing %v", k.Rows, ch.growing)
	}
	// Anchor at 10, its 4 old rows end at 14, the cursor at 16 on a
	// 40-row screen: 2 rows inserted at 14, the cursor to 18.
	if got := ch.grow(map[uint32]int{TSPImageFirst: 10}, 16, 40); got != "\x1b[15;1H\x1b[2L\x1b[19;1H" {
		t.Errorf("grow: %q", got)
	}
	if ch.resize("s4", 8, 0) {
		t.Error("a kept surface grew twice")
	}
	// Near the screen's end the screen scrolls first: cursor at 38 of
	// 40, 2 rows to insert, one over.
	ch.growing = []tspGrowth{{TSPImageFirst, 2}}
	if got := ch.grow(map[uint32]int{TSPImageFirst: 10}, 38, 40); got != "\x1b[40;1H\n\x1b[14;1H\x1b[2L\x1b[40;1H" {
		t.Errorf("grow at the end: %q", got)
	}
	// An end above the screen, or an anchor gone: nothing.
	ch.growing = []tspGrowth{{TSPImageFirst, 2}, {TSPImageFirst + 7, 1}}
	if got := ch.grow(map[uint32]int{TSPImageFirst: -6}, 5, 40); got != "" || len(ch.growing) != 0 {
		t.Errorf("grow above the screen: %q", got)
	}
	// A kept surface whose anchor the terminal lost, as after a clear,
	// goes; one scrolled into the scrollback stays.
	ch.changed = false
	if ch.sweep(map[uint32]int{TSPImageFirst: -30}) || ch.changed || len(ch.kept) != 1 {
		t.Error("a kept surface in the scrollback went")
	}
	if !ch.sweep(map[uint32]int{}) || !ch.changed || len(ch.kept) != 0 || len(ch.list()) != 1 {
		t.Errorf("after a clear: kept %v, list %v", ch.kept, ch.list())
	}
	if _, err := ch.event(json.RawMessage(`{"ev":"action","sf":"s4","id":"x","act":"click"}`)); err == nil {
		t.Error("an event of a kept surface went through")
	}

	// A screen surface closed with keep leaves its last view as text,
	// in the close's place in the output.
	kp := tspTracker{on: true}
	kp.feed([]byte(tspOpen + tspFrame1))
	out = string(kp.feed([]byte("before\x1b_tsp;x;{\"id\":\"s1\",\"keep\":true}\x1b\\after")))
	want := "before" +
		"GoRex TSP demo (running)\r\n" +
		"  line\r\n" +
		"  [Click me] clicked 0 times, tick 0\r\n" +
		"after"
	if out != want {
		t.Errorf("kept text:\n%q\nwant\n%q", out, want)
	}
	if len(kp.surfaces) != 0 || len(kp.kept) != 0 {
		t.Error("the screen surface stayed after a close with keep")
	}

	// A flow surface is laid out among the output as an inline one is,
	// and stays when it closes, keep or not, as output does.
	fl := tspTracker{on: true, cols: 50}
	out = string(fl.feed([]byte("\x1b_tsp;o;{\"id\":\"p\",\"mode\":\"flow\",\"listen\":false}\x1b\\")))
	if p := fl.surfaces["p"]; p == nil || p.Image != TSPImageFirst || out != anchor(TSPImageFirst)+"\r\n" {
		t.Fatalf("flow open: %+v, output %q", fl.surfaces["p"], out)
	}
	fl.feed([]byte("\x1b_tsp;x;{\"id\":\"p\"}\x1b\\"))
	if len(fl.kept) != 1 || fl.kept[0].ID != "p" || !fl.kept[0].Closed {
		t.Errorf("flow close: kept %+v", fl.kept)
	}
}

// A blob, chunked with its mime type in the first chunk only, is kept
// by the SHA-256 of its bytes; the blobs query says which are held;
// past the cap the oldest go.
func TestTSPBlobs(t *testing.T) {
	tr := tspTracker{on: true}
	data := []byte("\x89PNG fake image bytes")
	b64 := base64.StdEncoding.EncodeToString(data)
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	tr.feed([]byte("\x1b_tsp;b;mime=image/png;c=k;m=1;" + b64[:10] + "\x1b\\" + "\x1b_tsp;b;c=k;" + b64[10:] + "\x1b\\"))
	if b := tr.blobs[id]; b == nil || b.Mime != "image/png" || string(b.Data) != string(data) || len(tr.blobOrder) != 1 {
		t.Fatalf("blob: %+v order %v", b, tr.blobOrder)
	}
	tr.feed([]byte("\x1b_tsp;q;{\"q\":\"blobs\",\"ids\":[\"" + id + "\",\"feed\"]}\x1b\\"))
	if len(tr.replies) != 1 || string(tr.replies[0]) != "\x1b_tsp;r;{\"have\":[\""+id+"\"],\"r\":\"blobs\"}\x1b\\" {
		t.Errorf("blobs reply: %q", tr.replies)
	}
	big := make([]byte, tspMaxBlobs/2+1)
	tr.addBlob("", big)
	big2 := append([]byte{1}, big[1:]...)
	tr.addBlob("", big2)
	if tr.blobs[id] != nil || len(tr.blobOrder) != 1 || tr.blobBytes != len(big2) {
		t.Errorf("after the cap: %d blobs, %d bytes, the first kept %v", len(tr.blobOrder), tr.blobBytes, tr.blobs[id] != nil)
	}
}

func ids(ns []*TSPNode) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

func TestSpliceUTF16(t *testing.T) {
	for _, c := range []struct {
		s       string
		at, del int
		text    string
		want    string
	}{
		{"hello", 0, 0, "X", "Xhello"},
		{"hello", 5, 0, "!", "hello!"},
		{"hello", 1, 3, "", "ho"},
		{"a😀b", 1, 2, "-", "a-b"}, // the emoji is two UTF-16 units
		{"héllo", 1, 1, "e", "hello"},
		{"", 0, 0, "x", "x"},
		{"ab", 9, 9, "x", "abx"},
	} {
		if got := spliceUTF16(c.s, c.at, c.del, c.text); got != c.want {
			t.Errorf("splice(%q, %d, %d, %q) = %q, want %q", c.s, c.at, c.del, c.text, got, c.want)
		}
	}
}

// A picker's catalogue is sent once and patched by id: itemsAdd replaces
// an item in its place or adds it at the end, itemsDel takes one out, and
// neither stays among the props.
func TestPickerItemsPatch(t *testing.T) {
	tr := tspTracker{on: true}
	tr.feed([]byte(`` + "\x1b_tsp;o;{\"id\":\"s\",\"mode\":\"screen\"}\x1b\\" +
		"\x1b_tsp;f;{\"sf\":\"s\",\"s\":1,\"ops\":[[\"add\",\"p\",\"s\",null,{\"id\":\"p\",\"k\":\"picker\",\"p\":{\"items\":[{\"id\":\"a\",\"label\":\"A\"},{\"id\":\"b\",\"label\":\"B\"},{\"id\":\"c\",\"label\":\"C\"}]}}]]}\x1b\\" +
		"\x1b_tsp;f;{\"sf\":\"s\",\"s\":2,\"ops\":[[\"set\",\"p\",{\"itemsAdd\":[{\"id\":\"b\",\"label\":\"B2\"},{\"id\":\"d\",\"label\":\"D\"}],\"itemsDel\":[\"a\"]}]]}\x1b\\"))
	p := tr.doc.nodes["s#0"]["p"]
	if p == nil {
		t.Fatal("no picker")
	}
	items, _ := p.Props["items"].([]any)
	var got []string
	for _, it := range items {
		m := it.(map[string]any)
		got = append(got, m["id"].(string)+"="+m["label"].(string))
	}
	if strings.Join(got, ",") != "b=B2,c=C,d=D" {
		t.Errorf("items %v", got)
	}
	if _, ok := p.Props["itemsAdd"]; ok {
		t.Error("itemsAdd kept")
	}
	if _, ok := p.Props["itemsDel"]; ok {
		t.Error("itemsDel kept")
	}
}

// A window's mirror, made whole at one version and kept by the updates
// after it, has what the server has: opens, frames, closes that keep,
// a program run again under the same surface ids, and the hints of ops
// that keep nothing.
func TestSurfaceMirror(t *testing.T) {
	tr := tspTracker{on: true, cols: 80}
	same := func(m *SurfaceMirror, when string) {
		t.Helper()
		want, _ := json.Marshal(tr.list())
		var list []*Surface
		for _, sf := range m.List() {
			list = append(list, &sf)
		}
		got, _ := json.Marshal(list)
		if string(got) != string(want) {
			t.Errorf("%s:\n%s\nwant\n%s", when, got, want)
		}
	}
	update := func(m *SurfaceMirror, from uint64) []SurfaceHint {
		t.Helper()
		tr.commit()
		b, ok := tr.updatesSince(from)
		if !ok {
			t.Fatalf("no updates since %d", from)
		}
		var us []SurfaceUpdate
		if err := json.Unmarshal(b, &us); err != nil {
			t.Fatal(err)
		}
		var hints []SurfaceHint
		for _, u := range us {
			h, ok := m.Update(u)
			if !ok {
				t.Fatalf("update %d after %d", u.V, m.Version())
			}
			hints = append(hints, h...)
		}
		return hints
	}
	inline := strings.Replace(tspOpen, `"mode":"screen"`, `"mode":"inline"`, 1)
	tr.feed([]byte(inline + tspFrame1))
	var m SurfaceMirror
	v := tr.commit()
	b, _ := json.Marshal(SurfaceSet{V: v, Surfaces: tr.list()})
	var set SurfaceSet
	json.Unmarshal(b, &set)
	m.Reset(&set)
	same(&m, "whole")

	tr.feed([]byte(tspFrame2 + "\x1b_tsp;f;{\"sf\":\"s1\",\"s\":3,\"ops\":[[\"reveal\",\"main.card\",\"end\"]]}\x1b\\"))
	hints := update(&m, v)
	same(&m, "after a frame")
	if len(hints) != 1 || hints[0].Op != "reveal" || hints[0].ID != "main.card" || hints[0].Arg != "end" || hints[0].SF != "s1" {
		t.Errorf("hints %+v", hints)
	}

	// A palette and sheets, for the live surface when they name none: a
	// replaced sheet keeps its place, an empty one goes; a bad name or
	// too much CSS is refused, changing nothing.
	v = m.Version()
	tr.replies = nil
	tr.feed([]byte("\x1b_tsp;t;{\"dark\":{\"accent\":\"#00b4ff\"},\"light\":{\"accent\":\"#0060a0\"}}\x1b\\" +
		"\x1b_tsp;s;{\"sf\":\"s1\",\"name\":\"main\",\"css\":\".a{}\"}\x1b\\" +
		"\x1b_tsp;s;{\"name\":\"x\",\"css\":\".b{}\"}\x1b\\" +
		"\x1b_tsp;s;{\"name\":\"y\",\"css\":\".d{}\"}\x1b\\" +
		"\x1b_tsp;s;{\"name\":\"main\",\"css\":\".c{}\"}\x1b\\" +
		"\x1b_tsp;s;{\"name\":\"x\",\"css\":null}\x1b\\" +
		"\x1b_tsp;s;{\"name\":\"bad name\",\"css\":\".e{}\"}\x1b\\" +
		"\x1b_tsp;s;{\"name\":\"big\",\"css\":\"" + strings.Repeat("a", tspMaxCSS) + "\"}\x1b\\"))
	update(&m, v)
	same(&m, "after a palette and sheets")
	sf := m.List()[0]
	if sf.Palette == nil || sf.Palette.Dark["accent"] != "#00b4ff" || sf.Palette.Light["accent"] != "#0060a0" {
		t.Errorf("palette %+v", sf.Palette)
	}
	if want := []TSPSheet{{"main", ".c{}"}, {"y", ".d{}"}}; !slices.Equal(sf.Sheets, want) {
		t.Errorf("sheets %+v, want %+v", sf.Sheets, want)
	}
	if len(tr.replies) != 2 || !strings.Contains(string(tr.replies[0]), `"sheet":"bad name"`) || !strings.Contains(string(tr.replies[1]), `"sheet":"big"`) {
		t.Errorf("replies %q", tr.replies)
	}

	// Kept, then the program runs again: s1 opens anew beside the kept
	// s1, and draws.
	v = m.Version()
	tr.feed([]byte("\x1b_tsp;x;{\"id\":\"s1\",\"keep\":true}\x1b\\" + inline + tspFrame1))
	tr.resize("s1", 5, 2)
	update(&m, v)
	same(&m, "after a close that keeps and a reopen")
	if l := m.List(); len(l) != 2 || !l[0].Closed || l[1].Closed || l[1].Rows != 5 || l[1].Dock != 2 {
		t.Errorf("list %+v", l)
	}
	// An update that does not follow is refused.
	if _, ok := m.Update(SurfaceUpdate{V: m.Version() + 2}); ok {
		t.Error("an update out of order went through")
	}
}

// idleConn is a terminal's connection to nothing: it reads nothing and
// takes what is written.
type idleConn struct{ r *io.PipeReader }

func (c idleConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c idleConn) Write(p []byte) (int, error) { return len(p), nil }
func (c idleConn) Close() error                { return c.r.Close() }

func newAnchorTerm(t *testing.T) *terminal.Terminal {
	r, _ := io.Pipe()
	vt, err := terminal.New(terminal.Options{
		Conn:      idleConn{r},
		AppImages: struct{ First, Last uint32 }{TSPImageFirst, TSPImageLast},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vt.Close() })
	vt.ResizePixels(20, 6, 8, 16)
	return vt
}

// anchorRows returns the rows of the anchors in vt by image id, and
// endRows those of the end markers.
func anchorRows(vt *terminal.Terminal) map[uint32]int { return markerRowsOf(vt, 1) }
func endRows(vt *terminal.Terminal) map[uint32]int    { return markerRowsOf(vt, 2) }

func markerRowsOf(vt *terminal.Terminal, cols int) map[uint32]int {
	rows := map[uint32]int{}
	for _, pl := range vt.AppPlacements() {
		if pl.Cols == cols {
			rows[pl.Image] = pl.Row
		}
	}
	return rows
}

// An upgrade's snapshot, with the markers put back, places them in a new
// terminal where they were: anchors and end markers up in the scrollback
// and on the screen, an end whose anchor went, and a row long gone
// dropped.
func TestPlaceAnchors(t *testing.T) {
	vt := newAnchorTerm(t)
	var b strings.Builder
	for i := range 30 {
		switch i {
		case 3:
			b.WriteString(anchor(TSPImageFirst))
		case 5:
			b.WriteString(endMarker(TSPImageFirst))
		case 12:
			b.WriteString(anchor(TSPImageFirst + 1))
		case 26:
			b.WriteString(endMarker(TSPImageFirst + 1))
		case 27:
			b.WriteString(anchor(TSPImageFirst+2) + endMarker(TSPImageFirst+2))
		}
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	b.WriteString("$ ")
	vt.Feed([]byte(b.String()))
	want, wantEnds := anchorRows(vt), endRows(vt)
	if len(want) != 3 || len(wantEnds) != 3 || want[TSPImageFirst] >= 0 || want[TSPImageFirst+1] >= 0 || wantEnds[TSPImageFirst+1] < 0 || want[TSPImageFirst+2] < 0 {
		t.Fatalf("anchors placed at %v, ends at %v", want, wantEnds)
	}
	snap := vt.Snapshot()

	place := func(starts, ends map[uint32]int) *terminal.Terminal {
		out, onScreen := placeAnchors(snap, starts, ends, 6)
		got := newAnchorTerm(t)
		got.Feed(out)
		got.Feed([]byte(onScreen))
		return got
	}
	got := place(want, wantEnds)
	if rows, ends := anchorRows(got), endRows(got); !maps.Equal(rows, want) || !maps.Equal(ends, wantEnds) {
		t.Errorf("anchors at %v, ends at %v; want %v, %v", rows, ends, want, wantEnds)
	}
	if got.Text() != vt.Text() {
		t.Errorf("text %q, want %q", got.Text(), vt.Text())
	}

	// An end whose anchor went with the oldest lines, and a row the
	// snapshot no longer has.
	starts := maps.Clone(want)
	delete(starts, TSPImageFirst+1)
	starts[TSPImageFirst+3] = -1000
	got = place(starts, wantEnds)
	delete(starts, TSPImageFirst+3)
	if rows, ends := anchorRows(got), endRows(got); !maps.Equal(rows, starts) || !maps.Equal(ends, wantEnds) {
		t.Errorf("with an anchor and a row gone, anchors at %v, ends at %v; want %v, %v", rows, ends, starts, wantEnds)
	}
}
