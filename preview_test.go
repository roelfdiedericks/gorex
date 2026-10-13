package main

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// TestViewable: what the engine shows, and how.
func TestViewable(t *testing.T) {
	for name, want := range map[string]string{
		`C:\x\shot.PNG`: "image", "/x/logo.svg": "image", "/x/a.webp": "image",
		"/x/paper.pdf": "document", "/x/index.html": "document", "/x/clip.mp4": "document",
		"/x/notes.md": "", "/x/main.go": "", "/x/data.bin": "",
	} {
		if got := viewable(name); got != want {
			t.Errorf("viewable(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestViewMode: a viewable file opens in View with nothing read as
// text, the layout keeps it, and a text file after it is the editor's.
func TestViewMode(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	os.WriteFile(filepath.Join(root, "pic.png"), []byte{0x89, 'P', 'N', 'G', 0, 1, 2}, 0o644)
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	tab := a.newEditorTab(root)
	a.openFile(tab, filepath.Join(root, "pic.png"), false)
	p := tab.treeFocus()
	tt.Frame()
	if p.edit.mode != modeView || p.edit.buf != nil || p.edit.loading || p.edit.err != "" {
		t.Fatalf("mode %q buf %v loading %v err %q", p.edit.mode, p.edit.buf != nil, p.edit.loading, p.edit.err)
	}
	snap := a.snapshot()
	st := snap.Tabs[len(snap.Tabs)-1]
	if st.Root.Mode != modeView {
		t.Errorf("saved mode %q", st.Root.Mode)
	}
	a.saveNow()
	b := &App{client: a.client}
	if !b.restore() {
		t.Fatal("nothing restored")
	}
	var q *Pane
	for _, x := range b.tabs[len(b.tabs)-1].panes() {
		if x.Kind == paneEditor {
			q = x
		}
	}
	if q == nil || q.edit.mode != modeView || q.edit.path != filepath.Join(root, "pic.png") {
		t.Errorf("restored %+v", q)
	}
	a.openFile(tab, filepath.Join(root, "a.go"), false)
	waitFor(t, tt, "the Go file", func() bool { return p.edit.buf != nil && p.edit.path == filepath.Join(root, "a.go") })
	if p.edit.mode != modeEditor {
		t.Errorf("a Go file after a picture is in %q", p.edit.mode)
	}
}

// TestHostFileServed: a host's file comes with its type and script-free.
func TestHostFileServed(t *testing.T) {
	a, _ := newTestApp(t)
	root := testDir(t)
	defer os.RemoveAll(root)
	os.WriteFile(filepath.Join(root, "page.html"), []byte("<script>alert(1)</script>"), 0o644)
	u, _ := url.Parse(fileURL(filepath.Join(root, "page.html")))
	rec := httptest.NewRecorder()
	a.serveHostFile(rec, httptest.NewRequest("GET", u.String(), nil))
	res := rec.Result()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Errorf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'none'") || !strings.Contains(csp, "sandbox") {
		t.Errorf("CSP %q", csp)
	}
	if rec.Body.String() != "<script>alert(1)</script>" {
		t.Errorf("body %q", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	a.serveHostFile(rec, httptest.NewRequest("GET", "gorex-file://localhost/favicon.ico", nil))
	if rec.Code != 404 {
		t.Errorf("favicon: %d", rec.Code)
	}
}
