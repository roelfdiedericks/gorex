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

// assetOrigin is where the page finds gorex-asset:// from: the scheme
// itself, but on Windows, where custom schemes are http://<scheme>.localhost
// and a page at one scheme can't name another.
var assetOrigin = "gorex-asset://localhost"

const (
	modePreview = "preview"
	// previewDebounce is how long after an edit the preview renders.
	previewDebounce = 150 * time.Millisecond
	// previewFileCap is the most bytes the file protocol serves of one
	// file, as an image in a README.
	previewFileCap = 32 << 20
)

// previewState is an editor pane's preview: its web view and page, made
// as the pane first enters Preview and kept, hidden while the pane shows
// the editor and across the Markdown files it shows, until the pane
// closes or moves on to another kind of file; and the render in flight.
type previewState struct {
	web     *mygo.WebView
	loaded  bool   // the page is loaded
	version uint64 // the buffer version rendered last
	path    string // the file rendered last
	base    string // the directory the page resolves relative URLs in
	due     bool   // a render is scheduled
	busy    bool   // a render is in flight
	again   bool   // the buffer changed during it
	dark    bool
	navOff  func()
	// showing is a file the page shows instead of the document, as an
	// image a link opened; the document loads again on the way back.
	showing string
	// section is the heading the caret was under as the pane last left
	// Preview: coming back scrolls only when the caret moved elsewhere.
	section string
	// phase says what the preview waits on until the page shows the
	// text, or what went wrong, in the pane's status line.
	phase string
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
// gorex-file://localhost/<path> through the window's server. (MyGo serves
// custom schemes at localhost only: on Windows they become
// http://<scheme>.localhost/.)
func (a *App) registerProtocols() {
	assets, err := fs.Sub(previewAssets, "assets/preview")
	if err != nil {
		log.Fatal(err)
	}
	mygo.Protocol.HandleFunc("gorex-asset", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "chroma.css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			w.Write([]byte(markdown.CSS("github", "github-dark")))
			return
		}
		http.ServeFileFS(w, r, assets, name)
	})
	mygo.Protocol.HandleFunc("gorex-file", func(w http.ResponseWriter, r *http.Request) {
		p := hostPathOf(r.URL)
		if r.URL.Path == "/favicon.ico" { // the engine's, on a file shown by itself
			http.NotFound(w, r)
			return
		}
		client := a.client
		if client == nil || p == "" {
			log.Printf("preview: file %s: not connected", r.URL)
			http.Error(w, "not connected", http.StatusServiceUnavailable)
			return
		}
		fd, err := client.ReadFile(p, previewFileCap)
		if err != nil {
			log.Printf("preview: file %s: %v", p, err)
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
	return "gorex-file://localhost" + (&url.URL{Path: p}).EscapedPath()
}

// previewBase is a directory of the host's as a path of the page's
// origin: "/C:/docs/" or "/home/me/docs/".
func previewBase(dir string) string {
	return strings.TrimPrefix(fileURL(dir), "gorex-file://localhost") + "/"
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
		kept := e.preview != nil && e.preview.loaded && e.preview.showing == "" && e.preview.path == e.path
		a.ensurePreview(p)
		// Where the caret is: the heading above it, unless the page is
		// as it was left and the caret is still in that section.
		if section := a.caretSection(e); section != "" && !(kept && section == e.preview.section) {
			a.previewScrollTo(p, section)
		}
	} else {
		e.mode = modeEditor
		if pv := e.preview; pv != nil {
			pv.section = a.caretSection(e)
		}
	}
	a.focusReq = p
	a.changed()
}

// caretSection is the id of the heading above the editor's caret, or "".
func (a *App) caretSection(e *editorState) string {
	if e.state == nil || e.buf == nil {
		return ""
	}
	line := e.state.CursorPos().Line
	id := ""
	for _, h := range markdown.Headings(e.buf.String()) {
		if h.Line <= line {
			id = h.ID
		}
	}
	return id
}

// togglePreview switches the focused editor pane's preview.
func (a *App) togglePreview(p *Pane) {
	if p != nil && p.Kind == paneEditor && isMarkdown(p.edit.path) {
		a.showPreview(p, p.edit.mode != modePreview)
	}
}

// ensurePreview makes the pane's web view and loads the page, once; a
// page showing something else loads the document again.
func (a *App) ensurePreview(p *Pane) {
	e := p.edit
	if pv := e.preview; pv != nil && pv.web != nil && !pv.web.IsDestroyed() {
		if pv.showing != "" {
			pv.showing = ""
			a.loadPreviewPage(pv)
			return
		}
		a.renderPreview(p)
		return
	}
	if a.win == nil {
		return
	}
	log.Printf("preview: making the web view of %s", e.path)
	web, err := a.win.NewWebView(mygo.WebViewOptions{BackgroundColor: hexOf(a.k.bgBottom)})
	if err != nil {
		log.Printf("preview: %v", err)
		a.toast(ui.Toast{Title: "Can't show the preview", Description: err.Error(), Type: "error"})
		e.mode = modeEditor
		return
	}
	pv := &previewState{web: web}
	e.preview = pv
	// The pane's current preview: the state moves to the next file's.
	current := func() bool { return p.edit != nil && p.edit.preview == pv }
	page := web.Page()
	pv.navOff = page.OnWillNavigate(func(ev *mygo.NavigateEvent) {
		log.Printf("preview: navigating to %s (user %v)", ev.URL, ev.UserInitiated)
		if !ev.UserInitiated {
			return
		}
		ev.PreventDefault()
		target := ev.URL
		a.update(func() {
			if current() {
				a.followLink(p, target)
			}
		})
	})
	page.OnDidFinishLoad(func() {
		a.update(func() {
			if !current() || pv.showing != "" {
				return
			}
			log.Printf("preview: page loaded")
			pv.loaded = true
			pv.phase = "rendering…"
			a.applyPreviewTheme(p)
			pv.version, pv.path, pv.base = 0, "", ""
			a.renderPreview(p)
			a.changed()
		})
	})
	page.OnDidFailLoad(func(err *mygo.LoadError) {
		a.update(func() {
			// A link's navigation, prevented, reports as one too.
			if !current() || pv.loaded {
				return
			}
			log.Printf("preview: page failed: %v", err)
			pv.phase = "the page failed to load: " + err.Error()
			a.changed()
		})
	})
	a.loadPreviewPage(pv)
}

// loadPreviewPage loads the preview's page, at the origin of the host's
// files: the file's directory is set as its base per render.
func (a *App) loadPreviewPage(pv *previewState) {
	pv.loaded, pv.phase = false, "loading the page…"
	html, _ := previewAssets.ReadFile("assets/preview/index.html")
	log.Printf("preview: loading the page")
	pv.web.Page().LoadHTML(strings.ReplaceAll(string(html), "gorex-asset://localhost", assetOrigin), "gorex-file://localhost/")
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
	if pv == nil || !pv.loaded || pv.showing != "" || e.buf == nil {
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
			// Another file: its directory as the base, from the top.
			path, base := e.path, previewBase(dirName(e.path))
			code := ""
			if base != pv.base {
				bjs, _ := json.Marshal(base)
				code = "gorexSetBase(" + string(bjs) + "); "
			}
			keep := path == pv.path
			go func() {
				html := markdown.Render(text)
				js, _ := json.Marshal(html)
				_, err := web.Page().Eval(code + fmt.Sprintf("return gorexSetContent(%s, %v)", js, keep))
				a.update(func() {
					if e.preview != pv {
						return
					}
					pv.busy = false
					if err != nil {
						log.Printf("preview: %v", err)
						pv.phase = "the page failed: " + err.Error()
					} else {
						pv.version, pv.path, pv.base = v, path, base
						pv.phase = ""
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
	dark := a.dark
	pv.dark = dark
	k := a.k
	font := a.editorFont()
	vars := map[string]string{
		"bg":        hexOf(k.bgBottom),
		"text":      hexOf(k.text),
		"muted":     hexOf(k.textMuted),
		"accent":    hexOf(k.busy),
		"danger":    hexOf(k.attention),
		"code-font": fmt.Sprintf("%q, ui-monospace, Consolas, Menlo, monospace", font.Family),
	}
	js, _ := json.Marshal(vars)
	mermaid := is(a.eff.Editor.Mermaid)
	web := pv.web
	// The window's own background follows the system's appearance, not
	// the app's: the web view paints the theme's.
	web.SetBackgroundColor(hexOf(k.bgBottom))
	go func() {
		if _, err := web.Page().Eval(fmt.Sprintf("gorexSetTheme(%v, %s); gorexSetMermaid(%v); return true", dark, js, mermaid)); err != nil {
			log.Printf("preview: theme: %v", err)
		}
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
					pv.showing = target
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

// previewScrollTo scrolls a preview to a heading's id: at once when the
// page shows the buffer as it is, else once the render due has run.
func (a *App) previewScrollTo(p *Pane, id string) {
	e := p.edit
	pv := e.preview
	if pv == nil || pv.web == nil {
		return
	}
	js, _ := json.Marshal(id)
	web := pv.web
	settled := pv.loaded && !pv.due && !pv.busy && e.buf != nil && pv.version == e.buf.Version() && pv.path == e.path
	go func() {
		if !settled {
			time.Sleep(previewDebounce + 250*time.Millisecond)
		}
		web.Page().Eval("return gorexScrollTo(" + string(js) + ")")
	}()
}

// editorLanguage reports whether the editor knows a language for a file
// name.
func editorLanguage(name string) bool { return editor.LanguageFor(name) != nil }
