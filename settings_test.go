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
