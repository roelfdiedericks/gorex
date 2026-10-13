package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"gorex/internal/markdown"
)

// The Markdown preview of an editor pane: a web view showing the
// buffer rendered to HTML, the host's files served to it, and its links
// taken by the app.

//go:embed assets/preview
var previewAssets embed.FS

const (
	modePreview = "preview"
	// previewDebounce is how long after an edit the preview renders.
	previewDebounce = 150 * time.Millisecond
	// previewFileCap is the most bytes the file protocol serves of one
	// file, as an image in a README.
	previewFileCap = 32 << 20
)

// previewState is an editor pane's preview: its web view, made when the
// pane enters Preview and destroyed when it leaves, and the render in
// flight.
type previewState struct {
	web     *mygo.WebView
	loaded  bool   // the page is loaded
	version uint64 // the buffer version rendered last
	due     bool   // a render is scheduled
	busy    bool   // a render is in flight
	again   bool   // the buffer changed during it
	dark    bool
	navOff  func()
}

// isMarkdown reports whether a file's name is a Markdown file's.
func isMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(strings.ReplaceAll(name, `\`, "/"))) {
	case ".md", ".markdown", ".mdown", ".mkd", ".mdx":
		return true
	}
	return false
}

// registerProtocols serves the preview's page and the host's files to
// web views: gorex-asset://localhost/<file> from the embedded assets,
// gorex-file://host/<path> through the window's server.
func (a *App) registerProtocols() {
	assets, err := fs.Sub(previewAssets, "assets/preview")
	if err != nil {
		log.Fatal(err)
	}
	mygo.Protocol.HandleFunc("gorex-asset", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "chroma.css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			w.Write([]byte(markdown.CSS("github")))
			return
		}
		http.ServeFileFS(w, r, assets, name)
	})
	mygo.Protocol.HandleFunc("gorex-file", func(w http.ResponseWriter, r *http.Request) {
		p := hostPathOf(r.URL)
		client := a.client
		if client == nil || p == "" {
			http.Error(w, "not connected", http.StatusServiceUnavailable)
			return
		}
		fd, err := client.ReadFile(p, previewFileCap)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if ct := mime.TypeByExtension(strings.ToLower(path.Ext(p))); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Write(fd.Data)
	})
}

// fileURL is the preview's URL of a path of the host's.
func fileURL(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/… becomes /C:/…
	}
	return "gorex-file://host" + (&url.URL{Path: p}).EscapedPath()
}

// hostPathOf is the host's path a gorex-file URL names, in the host's
// separators.
func hostPathOf(u *url.URL) string {
	p := u.Path
	if hostGOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
		p = strings.ReplaceAll(p, "/", `\`)
	}
	return p
}

// showPreview switches an editor pane into Preview, or out of it.
func (a *App) showPreview(p *Pane, on bool) {
	if p == nil || p.Kind != paneEditor || p.edit.buf == nil {
		return
	}
	e := p.edit
	if on == (e.mode == modePreview) {
		return
	}
	if on {
		e.mode = modePreview
		a.ensurePreview(p)
		// Where the caret is: the heading above it.
		if e.state != nil {
			line := e.state.CursorPos().Line
			id := ""
			for _, h := range markdown.Headings(e.buf.String()) {
				if h.Line <= line {
					id = h.ID
				}
			}
			if id != "" {
				a.previewScrollTo(p, id)
			}
		}
	} else {
		e.mode = modeEditor
		a.dropPreview(p)
	}
	a.focusReq = p
	a.changed()
}

// togglePreview switches the focused editor pane's preview.
func (a *App) togglePreview(p *Pane) {
	if p != nil && p.Kind == paneEditor && isMarkdown(p.edit.path) {
		a.showPreview(p, p.edit.mode != modePreview)
	}
}

// ensurePreview makes the pane's web view and loads the page, once.
func (a *App) ensurePreview(p *Pane) {
	e := p.edit
	if e.preview != nil && e.preview.web != nil && !e.preview.web.IsDestroyed() {
		a.renderPreview(p)
		return
	}
	if a.win == nil {
		return
	}
	web, err := a.win.NewWebView(mygo.WebViewOptions{Transparent: true, BackgroundColor: "transparent"})
	if err != nil {
		log.Printf("preview: %v", err)
		a.toast(ui.Toast{Title: "Can't show the preview", Description: err.Error(), Type: "error"})
		e.mode = modeEditor
		return
	}
	pv := &previewState{web: web}
	e.preview = pv
	page := web.Page()
	pv.navOff = page.OnWillNavigate(func(ev *mygo.NavigateEvent) {
		if !ev.UserInitiated {
			return
		}
		ev.PreventDefault()
		target := ev.URL
		a.update(func() { a.followLink(p, target) })
	})
	page.OnDidNavigate(func(string) {
		a.update(func() {
			if e.preview != pv {
				return
			}
			pv.loaded = true
			a.applyPreviewTheme(p)
			pv.version = 0
			a.renderPreview(p)
		})
	})
	html, _ := previewAssets.ReadFile("assets/preview/index.html")
	base := fileURL(dirName(e.path)) + "/"
	page.LoadHTML(string(html), base)
}

// dropPreview destroys the pane's web view.
func (a *App) dropPreview(p *Pane) {
	e := p.edit
	if e.preview == nil {
		return
	}
	pv := e.preview
	e.preview = nil
	if pv.navOff != nil {
		pv.navOff()
	}
	if pv.web != nil && !pv.web.IsDestroyed() {
		pv.web.Destroy()
	}
}

// renderPreview renders the buffer to the page when it changed, a moment
// after the last edit, one render at a time.
func (a *App) renderPreview(p *Pane) {
	e := p.edit
	pv := e.preview
	if pv == nil || !pv.loaded || e.buf == nil {
		return
	}
	if pv.busy {
		pv.again = true
		return
	}
	if pv.due {
		return
	}
	pv.due = true
	time.AfterFunc(previewDebounce, func() {
		a.update(func() {
			if e.preview != pv {
				return
			}
			pv.due = false
			if e.buf == nil || pv.busy {
				return
			}
			v := e.buf.Version()
			if v == pv.version && pv.version != 0 {
				return
			}
			text := e.buf.String()
			pv.busy = true
			web := pv.web
			go func() {
				html := markdown.Render(text)
				js, _ := json.Marshal(html)
				_, err := web.Page().Eval("return gorexSetContent(" + string(js) + ")")
				a.update(func() {
					if e.preview != pv {
						return
					}
					pv.busy = false
					if err != nil {
						log.Printf("preview: %v", err)
					} else {
						pv.version = v
					}
					if pv.again {
						pv.again = false
						a.renderPreview(p)
					}
				})
			}()
		})
	})
}

// previewChanged tells a pane's preview that its buffer changed, which
// renders again after a moment.
func (a *App) previewChanged(p *Pane) {
	if e := p.edit; e != nil && e.mode == modePreview && e.preview != nil {
		a.renderPreview(p)
	}
}

// applyPreviewTheme gives the page the window's colors and fonts.
func (a *App) applyPreviewTheme(p *Pane) {
	e := p.edit
	pv := e.preview
	if pv == nil || !pv.loaded || a.win == nil {
		return
	}
	dark := a.darkWindow()
	pv.dark = dark
	k := a.colorsFor(dark)
	font := a.editorFont()
	vars := map[string]string{
		"text":      hexOf(k.text),
		"muted":     hexOf(k.textMuted),
		"accent":    hexOf(k.busy),
		"danger":    hexOf(k.attention),
		"code-font": fmt.Sprintf("%q, ui-monospace, Consolas, Menlo, monospace", font.Family),
	}
	js, _ := json.Marshal(vars)
	mermaid := is(a.eff.Editor.Mermaid)
	web := pv.web
	go func() {
		web.Page().Eval(fmt.Sprintf("gorexSetTheme(%v, %s); gorexSetMermaid(%v); return true", dark, js, mermaid))
	}()
}

// previewThemeChanged re-themes every preview, as the appearance or the
// settings change.
func (a *App) previewThemeChanged() {
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.Kind == paneEditor && p.edit.preview != nil {
				a.applyPreviewTheme(p)
			}
		}
	}
}

// followLink takes a link the preview's page navigated to: the web's
// open with the system, the host's files open in the tab, as the file
// browser opens them.
func (a *App) followLink(p *Pane, raw string) {
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	switch u.Scheme {
	case "http", "https", "mailto":
		mygo.Shell.OpenExternal(raw)
		return
	case "gorex-file", "file":
	case "gorex-asset", "about", "":
		return
	default:
		mygo.Shell.OpenExternal(raw)
		return
	}
	target := hostPathOf(u)
	if target == "" {
		return
	}
	a.openHostPath(p, target, u.Fragment)
}

// openHostPath opens a path of the host's from a preview: a Markdown or
// text file in the tab's editor (Markdown in Preview), a directory in
// the browser, anything else in the page itself when the engine shows
// it, else with the system is not possible here: a toast says so.
func (a *App) openHostPath(p *Pane, target, fragment string) {
	t := p.Tab
	client := a.client
	if client == nil {
		return
	}
	go func() {
		fi, err := client.Stat(target)
		a.update(func() {
			switch {
			case err != nil:
				a.toast(ui.Toast{Title: "Not found on " + a.hostName(), Description: target, Type: "error"})
			case fi.Dir:
				if b := t.Browser; b != nil {
					b.files.tree.Open.Add(target)
					b.files.isDir[target] = true
					a.changed()
				}
			case isMarkdown(target) || textLike(target):
				a.openFile(t, target, false)
				if np := t.treeFocus(); np != nil && np.edit.path == target && isMarkdown(target) {
					a.previewWhenLoaded(np, fragment)
				}
			default:
				// An image or a document: the page shows it where it can.
				if pv := p.edit.preview; pv != nil && pv.web != nil {
					pv.loaded = false
					pv.web.Page().LoadURL(fileURL(target))
					a.toast(ui.Toast{Title: "Showing " + baseName(target), Description: "Toggle Preview twice to come back to the document", Timeout: 4 * time.Second})
				}
			}
		})
	}()
}

// textLike reports whether a file is one the editor opens from a link:
// a language the editor knows, or a plain text extension.
func textLike(name string) bool {
	if editorLanguage(name) {
		return true
	}
	switch strings.ToLower(path.Ext(strings.ReplaceAll(name, `\`, "/"))) {
	case ".txt", ".text", ".log", ".cfg", ".conf", ".ini", ".env", ".csv", ".rst", ".adoc", "":
		return true
	}
	return false
}

// previewWhenLoaded puts a pane into Preview once its file is read, and
// scrolls to a fragment.
func (a *App) previewWhenLoaded(p *Pane, fragment string) {
	e := p.edit
	if e.buf != nil {
		a.showPreview(p, true)
		if fragment != "" {
			a.previewScrollTo(p, fragment)
		}
		return
	}
	e.previewOnLoad, e.previewFragment = true, fragment
}

// previewScrollTo scrolls a preview to a heading's id, once rendered.
func (a *App) previewScrollTo(p *Pane, id string) {
	pv := p.edit.preview
	if pv == nil || pv.web == nil {
		return
	}
	js, _ := json.Marshal(id)
	web := pv.web
	go func() {
		time.Sleep(previewDebounce + 250*time.Millisecond)
		web.Page().Eval("return gorexScrollTo(" + string(js) + ")")
	}()
}

// darkWindow reports whether the window's theme is dark, as of the last
// frame.
func (a *App) darkWindow() bool { return a.dark }

// colorsFor is the palette of a light or dark window, untinted.
func (a *App) colorsFor(dark bool) *colors {
	if dark {
		return &darkColors
	}
	return &lightColors
}

// editorLanguage reports whether the editor knows a language for a file
// name.
func editorLanguage(name string) bool { return editor.LanguageFor(name) != nil }
