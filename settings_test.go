package main

import (
	"encoding/json"
	"testing"
)

// TestEffective checks how the layers combine by kind: look fields from
// the host when set, else the Defaults; host fields from the host only;
// pc fields from the Defaults only; the built-in values under both.
func TestEffective(t *testing.T) {
	defaults := Settings{
		Appearance: Appearance{Mode: "dark"},
		Terminal:   Terminal{Size: 14, Scrollback: 500, Bell: "sound"},
		Shell:      Shell{Command: "/bin/pc-shell"},
		General:    General{ReconnectAttempts: 2},
	}
	host := Settings{
		Terminal: Terminal{Size: 16, Scrollback: 20000, Bell: "none", CopyOnSelect: on},
		Shell:    Shell{Command: "/usr/bin/fish"},
		General:  General{ReconnectAttempts: 9},
	}
	eff := effective(&defaults, &host)
	for name, got := range map[string][2]any{
		"look from defaults":    {eff.Appearance.Mode, "dark"},
		"look from host":        {eff.Terminal.Size, float32(16)},
		"look built in":         {eff.Terminal.Cursor, "block"},
		"host from host":        {eff.Shell.Command, "/usr/bin/fish"},
		"host ignores defaults": {eff.Terminal.Scrollback, 20000},
		"pc from defaults":      {eff.Terminal.Bell, "sound"},
		"pc ignores host":       {eff.General.ReconnectAttempts, 2},
		"pc built in":           {is(eff.General.Notify), true},
	} {
		if got[0] != got[1] {
			t.Errorf("%s: got %v, want %v", name, got[0], got[1])
		}
	}
	if is(eff.Terminal.CopyOnSelect) {
		t.Error("a pc field set on the host applied")
	}

	// A host with nothing of its own is the Defaults, and one with only
	// host fields keeps the Defaults' look.
	if eff := effective(&defaults, &Settings{}); eff.Terminal.Size != 14 || eff.Shell.Command != "" {
		t.Errorf("empty host: %+v", eff.Terminal)
	}

	// What goes to a host carries no pc fields; the look alone is what
	// copying a look takes.
	var sent Settings
	json.Unmarshal(encodeHost(host), &sent)
	if sent.Terminal.CopyOnSelect != nil || sent.General.ReconnectAttempts != 0 || sent.Shell.Command != "/usr/bin/fish" {
		t.Errorf("sent to the host: %+v %+v", sent.Terminal, sent.General)
	}
	if l := lookOnly(host); l.Terminal.Size != 16 || l.Shell.Command != "" || l.Terminal.Scrollback != 0 {
		t.Errorf("look only: %+v %+v", l.Terminal, l.Shell)
	}

	// Every field declares a kind and a label.
	for _, f := range fields {
		switch f.kind {
		case "look", "host", "pc":
		default:
			t.Errorf("%s has kind %q", f.key, f.kind)
		}
		if f.label == "" {
			t.Errorf("%s has no label", f.key)
		}
	}
}

// A document before version 2 has the Tern Surface Protocol switch under
// the terminal, where only a host set it: read, it is omp.tsp, on or off;
// a document that has omp.tsp keeps it.
func TestDecodeSettingsMovesTSP(t *testing.T) {
	for _, c := range []struct {
		doc  string
		want *bool
	}{
		{`{"v":1,"terminal":{"tsp":true,"size":14}}`, on},
		{`{"terminal":{"tsp":false}}`, off},
		{`{"v":1,"terminal":{"size":14}}`, nil},
		{`{"v":2,"omp":{"tsp":true},"terminal":{"tsp":false}}`, on},
	} {
		s := decodeSettings([]byte(c.doc))
		if (s.OMP.TSP == nil) != (c.want == nil) || s.OMP.TSP != nil && *s.OMP.TSP != *c.want {
			t.Errorf("%s: omp.tsp %v, want %v", c.doc, s.OMP.TSP, c.want)
		}
	}
	// Saved again, it is version 2's.
	var sent map[string]map[string]any
	json.Unmarshal(encodeHost(decodeSettings([]byte(`{"v":1,"terminal":{"tsp":true}}`))), &sent)
	if sent["omp"]["tsp"] != true || sent["terminal"]["tsp"] != nil {
		t.Errorf("sent to the host: %v", sent)
	}
}

// The Tern Surface Protocol switch is the Defaults' on hosts that leave
// it, and a host's own over them; new sessions are asked for by it.
func TestTSPDefaultsAndHost(t *testing.T) {
	defaults := Settings{OMP: OMP{TSP: on}}
	if eff := effective(&defaults, &Settings{}); !shellOptions(&eff).TSP {
		t.Error("the Defaults' switch did not reach a host without its own")
	}
	if eff := effective(&defaults, &Settings{OMP: OMP{TSP: off}}); shellOptions(&eff).TSP {
		t.Error("a host's switch off did not win over the Defaults'")
	}
	if eff := effective(&Settings{}, &Settings{OMP: OMP{TSP: on}}); !shellOptions(&eff).TSP {
		t.Error("a host's switch on did not apply")
	}
}

// The surfaces' code follows the terminal's font, size and ligatures
// until the OMP settings set them; sizes keep their ratios to the base
// sizes; the message width is none unless set.
func TestTSPStyle(t *testing.T) {
	s := builtin
	s.Terminal.Font, s.Terminal.Size, s.Terminal.Ligatures = "Iosevka", 15, off
	st := tspStyleOf(&s)
	if st.mono != termFamily("Iosevka") || st.monoSize != 15 || len(st.monoFeatures) == 0 {
		t.Errorf("following the terminal: %q %v %v", st.mono, st.monoSize, st.monoFeatures)
	}
	if st.font != "system-ui, "+symbolsFont || st.msgW != 0 || st.dockW != 800 || st.gap != 1 {
		t.Errorf("builtin: %+v", st)
	}
	s.OMP.CodeFont, s.OMP.CodeSize, s.OMP.CodeLigatures = "Fira Code", 11, on
	s.OMP.Font, s.OMP.Size, s.OMP.MessageWidth, s.OMP.Spacing = "Segoe UI", 15.6, 960, "compact"
	st = tspStyleOf(&s)
	if st.mono != termFamily("Fira Code") || st.monoSize != 11 || len(st.monoFeatures) != 0 {
		t.Errorf("overridden code: %q %v %v", st.mono, st.monoSize, st.monoFeatures)
	}
	if st.font != "Segoe UI, "+symbolsFont || st.msgW != 960 || st.gap != 0.6 {
		t.Errorf("overridden: %+v", st)
	}
	a := &App{tsp: st}
	if got := a.tspText(10); got != 12 {
		t.Errorf("text 10 at 15.6: %v, want 12", got)
	}
	if got := a.tspCode(12); got != 11 {
		t.Errorf("code 12 at 11: %v, want 11", got)
	}
	// A host's "No limit" over a Defaults' width.
	s.OMP.MessageWidth = -1
	if st := tspStyleOf(&s); st.msgW != 0 {
		t.Errorf("no limit: %v", st.msgW)
	}
}
