package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestPreviewURLs: a host path becomes a gorex-file URL and back.
func TestPreviewURLs(t *testing.T) {
	was := hostGOOS
	defer func() { hostGOOS = was }()
	for goos, p := range map[string]string{"windows": `C:\Users\me\My Docs\img one.png`, "linux": "/home/me/docs/img one.png"} {
		hostGOOS = goos
		u, err := url.Parse(fileURL(p))
		if err != nil || u.Scheme != "gorex-file" || u.Host != "localhost" {
			t.Fatalf("%s: %v %v", goos, u, err)
		}
		if back := hostPathOf(u); back != p {
			t.Errorf("%s: %q -> %q -> %q", goos, p, u, back)
		}
	}
	if !isMarkdown(`C:\x\README.md`) || !isMarkdown("/x/notes.markdown") || isMarkdown("/x/main.go") {
		t.Error("isMarkdown")
	}
	if !textLike("/x/main.go") || !textLike("/x/notes.txt") || textLike("/x/pic.png") {
		t.Error("textLike")
	}
}

// TestPreviewMode: a Markdown file opens in Preview by the setting, and
// the mode comes back from the layout.
func TestPreviewMode(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hi\n"), 0o644)
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	tab := a.newEditorTab(root)
	a.openFile(tab, filepath.Join(root, "README.md"), false)
	p := tab.treeFocus()
	waitFor(t, tt, "the file", func() bool { return p.edit.buf != nil })
	if p.edit.mode != modePreview {
		t.Errorf("mode %q, want preview by the setting", p.edit.mode)
	}
	// Without a window there is no web view: the pane shows the note.
	tt.Frame()
	if !tt.HasText("No preview") && !tt.HasText("Preview") {
		t.Errorf("preview pane: %q", tt.Texts())
	}
	a.openFile(tab, filepath.Join(root, "a.go"), false)
	waitFor(t, tt, "the Go file", func() bool { return p.edit.buf != nil && p.edit.path == filepath.Join(root, "a.go") })
	if p.edit.mode != modeEditor {
		t.Errorf("a Go file opened in %q", p.edit.mode)
	}
	// The layout keeps Preview.
	a.openFile(tab, filepath.Join(root, "README.md"), false)
	waitFor(t, tt, "README again", func() bool { return p.edit.buf != nil && p.edit.mode == modePreview })
	snap := a.snapshot()
	st := snap.Tabs[len(snap.Tabs)-1]
	if st.Root.Path != filepath.Join(root, "README.md") || st.Root.Mode != modePreview {
		t.Errorf("saved %+v", st.Root)
	}
	off := new(false)
	a.eff.Editor.PreviewOnOpen = off
	a.openFile(tab, filepath.Join(root, "a.go"), false)
	waitFor(t, tt, "a.go", func() bool { return p.edit.path == filepath.Join(root, "a.go") && p.edit.buf != nil })
	a.openFile(tab, filepath.Join(root, "README.md"), false)
	waitFor(t, tt, "README in the editor", func() bool { return p.edit.path == filepath.Join(root, "README.md") && p.edit.buf != nil })
	if p.edit.mode != modeEditor {
		t.Errorf("with the setting off, mode %q", p.edit.mode)
	}
}
