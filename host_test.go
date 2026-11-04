package main

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// What is typed in the host popover filters its hosts by name and line;
// the only one left is picked, but an address picks only a host of that
// address in full.
func TestFilterHosts(t *testing.T) {
	entries := []hostEntry{
		{title: "This PC", detail: "local sessions", current: true, pc: true},
		{title: "unifi", detail: "root@192.168.0.12", key: "root@192.168.0.12:22", addrs: []string{"root@192.168.0.12", "root@192.168.0.12:22"}},
		{title: "debian-vpp", detail: "root@192.168.0.180", key: "root@192.168.0.180:22", addrs: []string{"root@192.168.0.180", "root@192.168.0.180:22"}},
		{title: "lab-cgnat", detail: "root@100.99.24.13", key: "root@100.99.24.13:22", addrs: []string{"root@100.99.24.13", "root@100.99.24.13:22"}},
		{title: "home-cgnat", detail: "root@192.168.0.227", key: "root@192.168.0.227:22", addrs: []string{"root@192.168.0.227", "root@192.168.0.227:22"}},
		{title: "COM8", detail: "serial · COM8 · 115200 8N1", key: "serial:COM8"},
	}
	for _, c := range []struct {
		q     string
		shown []int
		pick  int
	}{
		{"", []int{0, 1, 2, 3, 4, 5}, -1},
		{"unifi", []int{1}, 1},
		{" UNI ", []int{1}, 1},
		{"ssh unifi", []int{1}, 1},
		{"cgnat", []int{3, 4}, -1},
		{"com8", []int{5}, 5},
		{"192.168.0.12", []int{1}, 1},
		{"root@192.168.0.12:22", []int{1}, 1},
		// The only one left, but not of this address: a new host.
		{"192.168.0.18", []int{2}, -1},
		{"root@192.168.0.18", []int{2}, -1},
		{"root@192.168.0.1", []int{1, 2}, -1},
		{"newbox", nil, -1},
	} {
		shown, pick := filterHosts(entries, c.q)
		if !slices.Equal(shown, c.shown) || pick != c.pick {
			t.Errorf("%q: shown %v pick %d, want %v %d", c.q, shown, pick, c.shown, c.pick)
		}
	}
}

// The popover's field filters the list as it is typed; the arrows choose
// a row, Escape clears the text, and the line under the field says what
// Enter does.
func TestHostsFilterKeys(t *testing.T) {
	registerFonts()
	a := &App{hostOpen: true, remotes: []savedRemote{
		{Key: "root@192.168.0.12:22", Target: "root@192.168.0.12", Label: "unifi", Kind: kindServer},
		{Key: "root@100.99.24.13:22", Target: "root@100.99.24.13", Label: "lab-cgnat", Kind: kindServer},
		{Key: "root@192.168.0.227:22", Target: "root@192.168.0.227", Label: "home-cgnat", Kind: kindServer},
	}}
	tt := ui.NewTester(func(c *ui.Context) { a.hostsSection(c, colorsOf(c)) }, 560, 500)
	tt.Frame()
	tt.Type("cgnat")
	tt.Frame()
	if tt.HasText("unifi") || !tt.HasText("lab-cgnat") || !tt.HasText("home-cgnat") {
		t.Fatal("cgnat: the list isn't filtered to the two cgnat hosts")
	}
	if !tt.HasText("↵ connect with ssh to cgnat") {
		t.Error("two left: Enter doesn't say it connects to the text")
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !tt.HasText("↵ switch to lab-cgnat") {
		t.Error("Down doesn't choose the first row left")
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if !tt.HasText("↵ connect with ssh to cgnat") {
		t.Error("Up from the first row doesn't leave the rows to the text")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.hostQuery != "" || !tt.HasText("unifi") {
		t.Fatalf("Escape: text %q, every host shown %v", a.hostQuery, tt.HasText("unifi"))
	}
	tt.Type("unifi")
	tt.Frame()
	if !tt.HasText("↵ switch to unifi") {
		t.Error("the only one left isn't picked")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Type(thisMachine())
	tt.Frame()
	if !tt.HasText("↵ stay on " + thisMachine()) {
		t.Fatal("the window's own host isn't picked")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.hostOpen || a.hostQuery != "" {
		t.Errorf("Enter on the window's own host: open %v, text %q", a.hostOpen, a.hostQuery)
	}
}

// A host's icon is its kind's, else its machine's as its server told: a
// host it told nothing of yet is a server.
func TestHostGlyph(t *testing.T) {
	for _, c := range []struct {
		kind, model, goos string
		portable          bool
		want              string
	}{
		{kindSerial, "", "", false, "cable"},
		{kindPlain, "MacBook Pro", "darwin", true, "globe"},
		{kindServer, "MacBook Air", "darwin", false, "laptop"},
		{kindServer, "", "windows", true, "laptop"},
		{kindServer, "iMac", "darwin", false, "monitor"},
		{kindServer, "", "windows", false, "monitor"},
		{kindServer, "Linux", "linux", false, "server"},
		{kindServer, "Mac mini", "darwin", false, "mac-studio"},
		{kindServer, "", "", false, "server"},
	} {
		if got := hostGlyph(c.kind, c.model, c.goos, c.portable); got != c.want {
			t.Errorf("%q %q %q portable %v: %q, want %q", c.kind, c.model, c.goos, c.portable, got, c.want)
		}
	}
}
