package rex

import (
	"encoding/base64"
	"strings"
	"testing"
)

func osc7501(body string) string { return "\x1b]7501;" + body + "\x1b\\" }

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// What a session's programs print sets its status, by the Program Status
// Protocol's rules.
func TestProgramStatus(t *testing.T) {
	type want struct {
		state, kind, app, msg string
		progress              int
	}
	none := want{}
	for _, c := range []struct {
		name   string
		output []string // the reads
		want   want
	}{
		{"a report", []string{osc7501("state=working:app=omp")}, want{"working", "", "omp", "", -1}},
		{"blocked with its kind and message", []string{osc7501("state=blocked:kind=permission:app=terraform:msg=" + b64("Apply 3 to add?"))},
			want{"blocked", "permission", "terraform", "Apply 3 to add?", -1}},
		{"BEL ends it too, padding optional", []string{"\x1b]7501;state=done:msg=" + strings.TrimRight(b64("ok!"), "=") + "\x07"}, want{"done", "", "", "ok!", -1}},
		{"split across reads", []string{"text \x1b]75", "01;state=wor", "king:progress=40\x1b", "\\ more"}, want{"working", "", "", "", 40}},
		{"a report replaces the record whole", []string{osc7501("state=working:app=omp"), osc7501("state=idle")}, want{"idle", "", "", "", -1}},
		{"an unknown state is ignored", []string{osc7501("state=working"), osc7501("state=paused")}, want{"working", "", "", "", -1}},
		{"no state is ignored", []string{osc7501("app=omp")}, none},
		{"kind only when blocked, progress only when working or blocked",
			[]string{osc7501("state=done:kind=question:progress=50")}, want{"done", "", "", "", -1}},
		{"an unknown kind is absent", []string{osc7501("state=blocked:kind=coffee")}, want{"blocked", "", "", "", -1}},
		{"progress out of range is absent", []string{osc7501("state=working:progress=101")}, want{"working", "", "", "", -1}},
		{"a malformed pair is skipped, the rest kept", []string{osc7501("state=working:bad:app=a!b:x=1")}, want{"working", "", "", "", -1}},
		{"a control character in msg drops the report", []string{osc7501("state=working"), osc7501("state=error:msg=" + b64("a\nb"))}, want{"working", "", "", "", -1}},
		{"bad base64 drops the report", []string{osc7501("state=error:msg=A")}, none},
		{"a value outside the character set is a skipped pair", []string{osc7501("state=error:msg=@@@")}, want{"error", "", "", "", -1}},
		{"msg past its limit drops the report", []string{osc7501("state=error:msg=" + b64(strings.Repeat("x", 2049)))}, none},
		{"a key past 16 bytes drops the report", []string{osc7501("state=done:abcdefghijklmnopq=1")}, none},
		{"a sequence past 4096 bytes is dropped", []string{osc7501("state=done:x=" + strings.Repeat("a", 4100))}, none},
		{"a bad id is ignored, not the root", []string{osc7501("state=idle"), osc7501("state=error:id=a//b")}, want{"idle", "", "", "", -1}},
		{"clear removes everything without an id", []string{osc7501("state=working"), osc7501("state=done:id=x"), osc7501("state=clear")}, none},
		{"clear removes a record and those beneath it",
			[]string{osc7501("state=idle:app=deploy"), osc7501("state=blocked:id=eu"), osc7501("state=blocked:id=eu/db"), osc7501("state=clear:id=eu")},
			want{"idle", "", "deploy", "", -1}},
		{"the record that most needs the user, with its ancestor's app",
			[]string{osc7501("state=working:app=deploy"), osc7501("state=working:id=us:progress=40"), osc7501("state=blocked:id=eu:kind=permission")},
			want{"blocked", "permission", "deploy", "", -1}},
		{"the root first among equals", []string{osc7501("state=working:id=a:app=x"), osc7501("state=working:app=root")}, want{"working", "", "root", "", -1}},
		{"a shell prompt (OSC 133 A) ends working and blocked, not done",
			[]string{osc7501("state=working:id=a"), osc7501("state=done:id=b"), "\x1b]133;A\x07"}, want{"done", "", "", "", -1}},
		{"a full reset removes every record", []string{osc7501("state=done"), "\x1bc"}, none},
		{"OSC 9;4 is work", []string{"\x1b]9;4;1;30\x1b\\"}, want{"working", "", "", "", 30}},
		{"OSC 9;4 errors and removes", []string{"\x1b]9;4;2\x07"}, want{"error", "", "", "", -1}},
		{"OSC 9;4 0 removes", []string{"\x1b]9;4;3\x07", "\x1b]9;4;0\x07"}, none},
		{"OSC 9;4 maps to nothing once a program spoke OSC 7501", []string{osc7501("state=idle:app=omp"), "\x1b]9;4;3\x07"}, want{"idle", "", "omp", "", -1}},
		{"an OSC cut short by another sequence counts for nothing", []string{"\x1b]7501;state=error\x1b[0m"}, none},
		{"OSC 9 notifications are not progress", []string{"\x1b]9;hello\x07"}, none},
	} {
		var tr statusTracker
		for _, r := range c.output {
			tr.feed([]byte(r))
		}
		st := tr.status()
		got := none
		if st != nil {
			got = want{st.State, st.Kind, st.App, st.Msg, st.Progress}
		}
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// A feature query is noted for the server to answer; past 64 records, the
// one reported least recently goes.
func TestProgramStatusQueryAndCap(t *testing.T) {
	var tr statusTracker
	tr.feed([]byte("\x1b]7501;?\x1b\\"))
	if !tr.query {
		t.Error("the query was not noted")
	}
	tr.feed([]byte(osc7501("state=blocked:id=first")))
	for i := range maxStatusRecords {
		tr.feed([]byte(osc7501("state=idle:id=r" + string(rune('a'+i%26)) + string(rune('a'+i/26)))))
	}
	if len(tr.records) != maxStatusRecords {
		t.Fatalf("%d records", len(tr.records))
	}
	if _, ok := tr.records["first"]; ok {
		t.Error("the oldest record was kept past the cap")
	}
}
