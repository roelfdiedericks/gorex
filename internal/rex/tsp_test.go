package rex

import (
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
	card := sf.Root.Children[0]
	if sf.Root.Kind != "col" || card.Kind != "card" || card.Props["status"] != "running" || len(card.Children) != 2 {
		t.Fatalf("document: %+v", sf.Root)
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
		t.Errorf("after close: %v, kept %q", tr.surfaces, tr.kept)
	}

	// Off, nothing is taken out or answered.
	off := tspTracker{}
	if out := string(off.feed([]byte(tspHello))); out != tspHello || len(off.replies) != 0 {
		t.Errorf("off: %q %q", out, off.replies)
	}

	// A chunked body is reassembled; a flow surface is not kept, an
	// inline one is.
	ch := tspTracker{on: true}
	body := `{"id":"s2","mode":"screen"}`
	ch.feed([]byte("\x1b_tsp;o;c=a;m=1;" + body[:10] + "\x1b\\" + "\x1b_tsp;o;c=a;" + body[10:] + "\x1b\\"))
	if ch.surfaces["s2"] == nil {
		t.Error("a chunked open was not read")
	}
	ch.feed([]byte("\x1b_tsp;o;{\"id\":\"s3\",\"mode\":\"flow\"}\x1b\\\x1b_tsp;o;{\"id\":\"s4\",\"mode\":\"inline\"}\x1b\\"))
	if ch.surfaces["s3"] != nil || ch.surfaces["s4"] == nil {
		t.Errorf("flow kept %v, inline kept %v", ch.surfaces["s3"] != nil, ch.surfaces["s4"] != nil)
	}

	// Closed with keep, the last view goes to the terminal as text, in
	// the close's place in the output.
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
	if len(kp.surfaces) != 0 {
		t.Error("the surface stayed after a close with keep")
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
