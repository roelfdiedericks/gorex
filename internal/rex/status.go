package rex

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
)

// Program status: what the programs of a session say they are doing, by
// the Program Status Protocol (OSC 7501,
// https://www.superlogical.com/rex/docs/build/program-status), and by OSC
// 9;4 progress until a program speaks the protocol. The server reads them
// from what the session prints, so that every window, or none, sees the
// same.

// ProgramStatus is the status of a session: the record of its programs
// that most needs the user.
type ProgramStatus struct {
	// State is "idle", "working", "blocked", "done" or "error".
	State string `json:"state"`
	// Kind is what a blocked program waits for: "permission",
	// "question", "auth", or "" for unsaid.
	Kind string `json:"kind,omitempty"`
	// App names the program, as "omp" or "cargo", when it says.
	App string `json:"app,omitempty"`
	// Msg is what it is doing, waiting for or finished, one line.
	Msg string `json:"msg,omitempty"`
	// Progress is a percentage, -1 when the program gives none.
	Progress int `json:"progress"`
}

// The protocol's limits: a report that breaks one is dropped whole.
const (
	maxStatusSequence = 4096
	maxStatusKey      = 16
	maxStatusMsg      = 2732 // encoded; 2048 decoded
	maxStatusMsgText  = 2048
	maxStatusTitle    = 256 // encoded; 192 decoded
	maxStatusTitleTxt = 192
	maxStatusApp      = 32
	maxStatusID       = 128
	maxStatusRecords  = 64
)

// statusRecord is a record of the protocol.
type statusRecord struct {
	ProgramStatus
	used uint64 // when it was last reported, for evicting the oldest
}

// statusTracker reads a session's output for program status. Its methods
// run under the session's lock.
type statusTracker struct {
	records map[string]*statusRecord
	clock   uint64
	// spoke tells that a program reported by OSC 7501, after which OSC
	// 9;4 maps to nothing until a full reset.
	spoke bool
	// The sequence being read, across reads: escaped after an ESC, osc
	// within an OSC, with its body so far; over when it is longer than
	// the protocol allows.
	escaped, osc, oscEscaped, over bool
	body                           []byte
	// query tells that a program asked whether the terminal speaks the
	// protocol, to answer.
	query bool
}

// feed reads output, and reports whether the status may have changed.
func (t *statusTracker) feed(p []byte) (changed bool) {
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch {
		case t.oscEscaped:
			t.oscEscaped = false
			if b == '\\' {
				changed = t.end() || changed
				continue
			}
			// Another sequence begins: the OSC is cut short.
			t.osc, t.body, t.over = false, t.body[:0], false
			t.escaped = true
			i--
		case t.osc:
			switch b {
			case 0x07:
				changed = t.end() || changed
			case 0x1b:
				t.oscEscaped = true
			default:
				if len(t.body) < maxStatusSequence {
					t.body = append(t.body, b)
				} else {
					t.over = true
				}
			}
		case t.escaped:
			t.escaped = false
			switch b {
			case ']':
				t.osc, t.body, t.over = true, t.body[:0], false
			case 'c':
				// A full reset removes every record.
				changed = len(t.records) > 0 || changed
				t.records, t.spoke = nil, false
			case 0x1b:
				t.escaped = true
			}
		default:
			j := bytes.IndexByte(p[i:], 0x1b)
			if j < 0 {
				return changed
			}
			i += j
			t.escaped = true
		}
	}
	return changed
}

// end takes the OSC read.
func (t *statusTracker) end() bool {
	t.osc = false
	body := string(t.body)
	t.body = t.body[:0]
	if t.over {
		t.over = false
		return false
	}
	switch {
	case body == "7501;?":
		t.query = true
		return false
	case strings.HasPrefix(body, "7501;"):
		return t.report(body[len("7501;"):])
	case strings.HasPrefix(body, "9;4;") || body == "9;4":
		return t.progress(body)
	case body == "133;A" || strings.HasPrefix(body, "133;A;"):
		// A new shell prompt.
		return t.dropRunning()
	}
	return false
}

// report applies a report of the protocol, or drops it.
func (t *statusTracker) report(body string) bool {
	if len(body)+len("\x1b]7501;\x1b\\") > maxStatusSequence {
		return false
	}
	kv := map[string]string{}
	for pair := range strings.SplitSeq(body, ":") {
		k, v, ok := strings.Cut(pair, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || !isStatusKey(k) || !isStatusValue(v) {
			continue
		}
		if len(k) > maxStatusKey {
			return false
		}
		kv[k] = v
	}
	id := kv["id"]
	if id != "" && !isStatusID(id) {
		return false
	}
	msg, ok := statusText(kv["msg"], maxStatusMsg, maxStatusMsgText)
	if !ok {
		return false
	}
	if _, ok := statusText(kv["title"], maxStatusTitle, maxStatusTitleTxt); !ok {
		return false
	}
	app := kv["app"]
	if len(app) > maxStatusApp {
		return false
	}
	if !isStatusApp(app) {
		app = ""
	}
	state := kv["state"]
	switch state {
	case "clear":
		t.spoke = true
		return t.clear(id)
	case "idle", "working", "done", "blocked", "error":
	default:
		return false
	}
	t.spoke = true
	st := ProgramStatus{State: state, App: app, Msg: msg, Progress: -1}
	if state == "blocked" {
		switch k := kv["kind"]; k {
		case "permission", "question", "auth":
			st.Kind = k
		}
	}
	if state == "working" || state == "blocked" {
		if n, err := strconv.Atoi(kv["progress"]); err == nil && n >= 0 && n <= 100 {
			st.Progress = n
		}
	}
	t.set(id, st)
	return true
}

// progress maps OSC 9;4 to the root record, as long as no program spoke
// the protocol: 0 removes it, 1 is progress, 2 an error, 3 work without a
// percentage and 4 paused work.
func (t *statusTracker) progress(body string) bool {
	if t.spoke {
		return false
	}
	parts := strings.Split(body, ";")
	state := "0"
	if len(parts) > 2 {
		state = parts[2]
	}
	pct := -1
	if len(parts) > 3 {
		if n, err := strconv.Atoi(parts[3]); err == nil && n >= 0 && n <= 100 {
			pct = n
		}
	}
	switch state {
	case "0":
		return t.clear("")
	case "1", "4":
		t.set("", ProgramStatus{State: "working", Progress: pct})
	case "2":
		t.set("", ProgramStatus{State: "error", Progress: -1})
	case "3":
		t.set("", ProgramStatus{State: "working", Progress: -1})
	default:
		return false
	}
	return true
}

// set replaces the record of id, making room past the cap by evicting the
// record reported least recently.
func (t *statusTracker) set(id string, st ProgramStatus) {
	if t.records == nil {
		t.records = map[string]*statusRecord{}
	}
	if _, ok := t.records[id]; !ok && len(t.records) >= maxStatusRecords {
		oldest, at := "", ^uint64(0)
		for k, r := range t.records {
			if r.used < at {
				oldest, at = k, r.used
			}
		}
		delete(t.records, oldest)
	}
	t.clock++
	t.records[id] = &statusRecord{ProgramStatus: st, used: t.clock}
}

// clear removes the record of id and those beneath it, or with no id
// every record.
func (t *statusTracker) clear(id string) bool {
	n := len(t.records)
	for k := range t.records {
		if id == "" || k == id || strings.HasPrefix(k, id+"/") {
			delete(t.records, k)
		}
	}
	return len(t.records) != n
}

// shells are the programs whose prompt, back in the foreground, ends what
// ran before it: a session of another program is idle while it runs.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "mksh": true,
	"tcsh": true, "csh": true, "nu": true, "elvish": true, "xonsh": true,
	"pwsh": true, "powershell": true, "cmd": true,
}

func isShellName(name string) bool {
	return shells[strings.TrimSuffix(strings.ToLower(name), ".exe")]
}

// dropRunning removes the records of programs working or blocked, which
// end with the program, at a new shell prompt.
func (t *statusTracker) dropRunning() bool {
	n := len(t.records)
	for k, r := range t.records {
		if r.State == "working" || r.State == "blocked" {
			delete(t.records, k)
		}
	}
	return len(t.records) != n
}

// statusRank orders states by how much they need the user.
var statusRank = map[string]int{"idle": 1, "working": 2, "done": 3, "error": 4, "blocked": 5}

// status returns the record that most needs the user, the root record
// first among equals, with the app of its nearest ancestor when it says
// none; nil with no records.
func (t *statusTracker) status() *ProgramStatus {
	var best *statusRecord
	bestID := ""
	for id, r := range t.records {
		if best == nil || statusRank[r.State] > statusRank[best.State] ||
			statusRank[r.State] == statusRank[best.State] && (id == "" || bestID != "" && r.used > best.used) {
			best, bestID = r, id
		}
	}
	if best == nil {
		return nil
	}
	st := best.ProgramStatus
	for id := bestID; st.App == "" && id != ""; {
		i := strings.LastIndexByte(id, '/')
		if i < 0 {
			id = ""
		} else {
			id = id[:i]
		}
		if r, ok := t.records[id]; ok {
			st.App = r.App
		}
	}
	return &st
}

// statusText decodes a free-text value: standard base64, padding
// optional, of UTF-8 without control characters, within its limits.
func statusText(v string, encoded, decoded int) (string, bool) {
	if v == "" {
		return "", true
	}
	if len(v) > encoded {
		return "", false
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(v, "="))
	if err != nil || len(b) > decoded {
		return "", false
	}
	for _, r := range string(b) {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return "", false
		}
	}
	return string(b), true
}

func isStatusKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 'a' || k[i] > 'z' {
			return false
		}
	}
	return true
}

// isStatusValue tells a value of the protocol's character set:
// [A-Za-z0-9_.,+/=-]*.
func isStatusValue(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_.,+/=-", c) >= 0) {
			return false
		}
	}
	return true
}

func isStatusApp(v string) bool {
	if v == "" || len(v) > maxStatusApp {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_.+-", c) >= 0) {
			return false
		}
	}
	return true
}

// isStatusID tells an id of the protocol: segments of 1 to 32 of
// [A-Za-z0-9_.+-], by "/", at most 8 deep and 128 long.
func isStatusID(id string) bool {
	if len(id) > maxStatusID {
		return false
	}
	segs := strings.Split(id, "/")
	if len(segs) > 8 {
		return false
	}
	for _, s := range segs {
		if s == "" || len(s) > 32 || !isStatusApp(s) {
			return false
		}
	}
	return true
}
