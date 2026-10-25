package rex

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
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
	if len(card.Children) != 2 || tr.nodes["s1"]["main.card.row.count"] != nil {
		t.Errorf("del: %v, count indexed %v", ids(card.Children), tr.nodes["s1"]["main.card.row.count"] != nil)
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

	// A chunked body is reassembled; a flow surface is not kept, an
	// inline one is, and gets an anchor in the output, in the first cell
	// of its row, and one row.
	ch := tspTracker{on: true, cols: 50}
	body := `{"id":"s2","mode":"screen"}`
	ch.feed([]byte("\x1b_tsp;o;c=a;m=1;" + body[:10] + "\x1b\\" + "\x1b_tsp;o;c=a;" + body[10:] + "\x1b\\"))
	if ch.surfaces["s2"] == nil || ch.surfaces["s2"].Image != 0 {
		t.Errorf("a chunked screen open: %+v", ch.surfaces["s2"])
	}
	out = string(ch.feed([]byte("\x1b_tsp;o;{\"id\":\"s3\",\"mode\":\"flow\"}\x1b\\\x1b_tsp;o;{\"id\":\"s4\",\"mode\":\"inline\"}\x1b\\")))
	s4 := ch.surfaces["s4"]
	if ch.surfaces["s3"] != nil || s4 == nil || s4.Image != TSPImageFirst || s4.Rows != 1 {
		t.Fatalf("flow kept %v, inline %+v", ch.surfaces["s3"] != nil, s4)
	}
	if out != anchor(TSPImageFirst)+"\r\n" {
		t.Errorf("anchor: %q", out)
	}
	// A window measured it at 6 rows, then 4: the surface has them.
	if !ch.resize("s4", 6) || s4.Rows != 6 || !ch.resize("s4", 4) || s4.Rows != 4 {
		t.Errorf("resize: rows %d", s4.Rows)
	}
	// The terminal lacks lines under the anchor while the cursor is
	// fewer rows below it than the surface has, and has some spare when
	// more, as many as are on the screen; an anchor not found, none.
	for _, c := range []struct{ anchor, cursor, want int }{{10, 11, 3}, {10, 14, 0}, {10, 20, -6}, {-5, 2, -2}, {-5, -2, 1}, {-20, 3, -3}} {
		if got := ch.padding(map[uint32]int{TSPImageFirst: c.anchor}, c.cursor); got != c.want {
			t.Errorf("padding with the anchor at %d, the cursor at %d: %d, want %d", c.anchor, c.cursor, got, c.want)
		}
	}
	if ch.padding(map[uint32]int{}, 11) != 0 {
		t.Error("padding without the anchor")
	}
	if ch.resize("s4", 4) || ch.resize("s2", 3) || ch.resize("s9", 3) {
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
	// in the list before the open ones; closed without, its anchor goes.
	ch.feed([]byte("\x1b_tsp;x;{\"id\":\"s4\",\"keep\":true}\x1b\\"))
	if len(ch.kept) != 1 || !ch.kept[0].Closed || ch.kept[0].Image != TSPImageFirst || len(ch.list()) != 2 || ch.list()[0] != ch.kept[0] {
		t.Errorf("kept: %+v list %v", ch.kept, ch.list())
	}
	ch.feed([]byte("\x1b_tsp;o;{\"id\":\"s5\",\"mode\":\"inline\"}\x1b\\"))
	if out := string(ch.feed([]byte("\x1b_tsp;x;{\"id\":\"s5\"}\x1b\\"))); out != deleteAnchor(TSPImageFirst+1) {
		t.Errorf("close without keep: %q", out)
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
