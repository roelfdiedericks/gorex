package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// TestEditorTab: an editor tab lists a directory in its browser, opens a
// file into its editor pane on a click, splits, saves through the server,
// and comes back from the layout as it was.
func TestEditorTab(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".hidden"), []byte("h\n"), 0o644)

	tab := a.newEditorTab(root)
	if tab == nil || tab.Kind != paneEditor || tab.Browser == nil || len(tab.panes()) != 1 {
		t.Fatalf("editor tab %+v", tab)
	}
	if tab.Focus != tab.Browser {
		t.Error("the browser does not have the focus")
	}
	waitFor(t, tt, "the root's entries", func() bool { return tt.HasText("notes.txt") && tt.HasText("src") })
	if !tt.HasText(".hidden") {
		t.Error("hidden files are not shown by default")
	}
	if !tt.HasText("No file open") {
		t.Errorf("the empty editor pane: %q", tt.Texts())
	}

	// A click opens the file in the editor pane.
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	ed := tab.treeFocus()
	if ed == nil || ed.edit.path != filepath.Join(root, "notes.txt") {
		t.Fatalf("clicking opened %+v", ed)
	}
	waitFor(t, tt, "the file", func() bool { return ed.edit.buf != nil })
	tt.Frame()
	if !tt.HasText("1 lines · Plain Text") && !tt.HasText("2 lines · Plain Text") {
		t.Errorf("status line: %q", tt.Texts())
	}
	if name, _ := tab.label(); name != baseName(root) {
		t.Errorf("tab label %q", name)
	}

	// Opening a Go file goes into the same pane, which is unmodified.
	tab.Browser.files.tree.Open.Add(filepath.Join(root, "src"))
	waitFor(t, tt, "src's entries", func() bool { return tt.HasText("main.go") })
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	if n := len(tab.panes()); n != 1 || tab.treeFocus() != ed {
		t.Fatalf("%d panes; focus moved %v", n, tab.treeFocus() != ed)
	}
	waitFor(t, tt, "the Go file", func() bool { return ed.edit.buf != nil && ed.edit.path == filepath.Join(root, "src", "main.go") })
	tt.Frame()
	if lang := langName(ed.edit); lang != "Go" {
		t.Errorf("language %q", lang)
	}

	// An edit, then a click on another file: a new split, the edit kept.
	ed.edit.buf.Edit(editor.Edit{Text: "// edited\n"})
	if !ed.edit.modified() {
		t.Fatal("the buffer is not modified")
	}
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if n := len(tab.panes()); n != 2 {
		t.Fatalf("%d panes after opening over a modified pane", n)
	}
	second := tab.treeFocus()
	if second == ed || second.edit.path != filepath.Join(root, "notes.txt") {
		t.Fatalf("the new pane shows %q", second.edit.path)
	}
	waitFor(t, tt, "notes again", func() bool { return second.edit.buf != nil })
	tt.Frame()
	if !tt.HasText("main.go") || !tt.HasText("notes.txt") {
		t.Errorf("headers: %q", tt.Texts())
	}

	// Saving writes the edit to disk and clears the mark.
	a.saveFile(ed)
	waitFor(t, tt, "the save", func() bool { return !ed.edit.modified() })
	if b, _ := os.ReadFile(filepath.Join(root, "src", "main.go")); !strings.HasPrefix(string(b), "// edited\n") {
		t.Errorf("saved %q", b)
	}

	// The layout round trip: an editor tab with its browser, panes and
	// modes.
	ed.edit.mode = modeChanges
	snap := a.snapshot()
	b, _ := json.Marshal(snap)
	var l savedLayout
	json.Unmarshal(b, &l)
	st := l.Tabs[len(l.Tabs)-1]
	if st.Kind != "editor" || st.Browser == nil || st.Browser.Root != root || !st.Browser.Hidden {
		t.Fatalf("saved tab %+v %+v", st, st.Browser)
	}
	if st.Root.A == nil || st.Root.A.Path != filepath.Join(root, "src", "main.go") || st.Root.A.Mode != modeChanges || st.Root.B.Path != filepath.Join(root, "notes.txt") {
		t.Errorf("saved tree %+v %+v", st.Root.A, st.Root.B)
	}
	if !slicesContains(st.Browser.Open, filepath.Join(root, "src")) {
		t.Errorf("open folders %v", st.Browser.Open)
	}
	a.save()
	waitFor(t, tt, "the layout to be saved", func() bool {
		raw, _ := a.client.Get("layout")
		return strings.Contains(string(raw), `"kind":"editor"`)
	})
	a.tabs, a.active = nil, 0
	if !a.restore() {
		t.Fatal("restore")
	}
	rt := a.tabs[len(a.tabs)-1]
	if rt.Kind != paneEditor || rt.Browser == nil || rt.Browser.files.root != root || len(rt.panes()) != 2 {
		t.Fatalf("restored %+v", rt)
	}
	if ps := rt.panes(); ps[0].edit.mode != modeChanges || ps[1].edit.path != filepath.Join(root, "notes.txt") {
		t.Errorf("restored panes %+v %+v", ps[0].edit, ps[1].edit)
	}
	if !rt.Browser.files.tree.Open.Has(filepath.Join(root, "src")) {
		t.Error("the open folder was not restored")
	}
	// Closing a modified pane: Don't Save is not asked for in tests (no
	// window for the dialog), so the buffer check happens on the state.
	tt.Frame()
}

// TestEditorKeepsEditsAcrossReconnect: a modified buffer survives a
// detach, and a pane of the next layout on the same file takes it over.
func TestEditorKeepsEditsAcrossReconnect(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644)
	tab := a.newEditorTab(root)
	a.openFile(tab, filepath.Join(root, "a.txt"), false)
	p := tab.treeFocus()
	waitFor(t, tt, "the file", func() bool { return p.edit.buf != nil })
	p.edit.buf.Edit(editor.Edit{Text: "zero\n"})
	kept := p.edit
	a.save()
	waitFor(t, tt, "the layout", func() bool { raw, _ := a.client.Get("layout"); return strings.Contains(string(raw), "a.txt") })

	client := a.client
	a.keepEdits()
	a.tabs = nil
	if a.keptEdits[filepath.Join(root, "a.txt")] != kept {
		t.Fatal("the edit was not kept")
	}
	a.client = client
	if !a.restore() {
		t.Fatal("restore")
	}
	np := a.tabs[len(a.tabs)-1].treeFocus()
	if np.edit != kept || !np.edit.modified() || len(a.keptEdits) != 0 {
		t.Errorf("the restored pane took %v, modified %v, kept %d", np.edit == kept, np.edit.modified(), len(a.keptEdits))
	}

	// A kept edit whose place is gone from the layout gets a pane, noted.
	a.keepEdits()
	a.tabs = nil
	a.client.Set("layout", []byte(`{"active":0,"tabs":[{"root":{"dir":"`+strings.ReplaceAll(os.TempDir(), `\`, `\\`)+`"}}]}`))
	if !a.restore() {
		t.Fatal("restore 2")
	}
	var found *Pane
	for _, tb := range a.tabs {
		for _, q := range tb.panes() {
			if q.Kind == paneEditor && q.edit == kept {
				found = q
			}
		}
	}
	if found == nil || !found.edit.elsewhere {
		t.Errorf("the orphaned edit: %+v", found)
	}
	tt.Frame()
}

func slicesContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

var _ = ui.KeyEnter
