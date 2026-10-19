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
	modeView    = "view"
	// previewDebounce is how long after an edit the preview renders.
	previewDebounce = 150 * time.Millisecond
	// previewFileCap is the most bytes the file protocol serves of one
	// file, as an image in a README, or a document shown by itself.
	previewFileCap = 128 << 20
	// previewCaretReach is how far below the editor's top line the caret
	// may stay as the editor follows the page; further, it comes along.
	previewCaretReach = 40
)

// hostFileCSP makes every document of the host's the engine shows
// script-free: its styles, images and fonts load, from the web too, but
// nothing in it runs, fetches or submits, and it gets a throwaway
// origin. Subresources of the Markdown page are unaffected.
const hostFileCSP = "sandbox; default-src * data: blob: 'unsafe-inline'; script-src 'none'; connect-src 'none'; form-action 'none'"

// backMarker is the URL a document shown by itself goes to on Escape,
// by the preload script: the pane takes it as Back.
const backMarker = "/?gorex-back"

// preloadScript runs in every document the pane's web view loads: Escape
// in one shown by itself asks for the document back, unless the page
// took the key (the lightbox closing).
const preloadScript = `document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !e.defaultPrevented) location.href = new URL("` + backMarker + `", location.href).href;
});`

// viewable is how the engine shows a file by its name: "image" in
// GoRex's viewer page, "document" by the engine itself (PDF, HTML,
// video, audio), or "" for the editor's text check.
func viewable(name string) string {
	switch strings.ToLower(path.Ext(strings.ReplaceAll(name, `\`, "/"))) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".avif", ".ico", ".svg":
		return "image"
	case ".pdf", ".html", ".htm", ".xhtml", ".mp4", ".webm", ".mov", ".mp3", ".wav", ".ogg", ".oga", ".m4a", ".flac":
		return "document"
	}
	return ""
}

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
	// showing is a file the page shows instead of the document: the
	// pane's own in View, or one a link opened, which Back leaves.
	showing string
	// title is what the page shown says of itself, for the status line:
	// the viewer page reports the image's kind and size.
	title string
	// caretLine is where the caret was as the pane last left Preview, or
	// -1: coming back scrolls the page only when the caret moved.
	caretLine int
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
	mygo.Protocol.HandleFunc("gorex-file", a.serveHostFile)
}

// serveHostFile answers a gorex-file request with the host's file, read
// through the window's server, script-free for the engine.
func (a *App) serveHostFile(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Security-Policy", hostFileCSP)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(fd.Data)
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

// showPreview switches an editor pane into Preview, or out of it, each
// view taking up where the other was: the page scrolls to the caret's
// line, unless it is as it was left and the caret has not moved; the
// editor scrolls to the line at the top of the page.
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
		if e.state != nil && !(kept && e.state.CursorPos().Line == e.preview.caretLine) {
			a.previewScrollToLine(p, e.state.CursorPos().Line)
		}
	} else {
		e.mode = modeEditor
		if pv := e.preview; pv != nil {
			pv.caretLine = -1
			if e.state != nil {
				pv.caretLine = e.state.CursorPos().Line
			}
			a.editorFollowsPreview(p)
		}
	}
	a.focusReq = p
	a.changed()
}

// editorFollowsPreview scrolls the editor to the line at the top of the
// page, as the pane leaves Preview, and brings the caret along when it
// would be far out of view, so that typing goes where the eye is.
func (a *App) editorFollowsPreview(p *Pane) {
	e := p.edit
	pv := e.preview
	if pv == nil || pv.web == nil || !pv.loaded || pv.showing != "" || e.state == nil {
		return
	}
	web := pv.web
	go func() {
		v, err := web.Page().Eval("return gorexTopLine()")
		f, ok := v.(float64)
		if err != nil || !ok {
			return
		}
		a.update(func() {
			if p.edit != e || e.state == nil || e.buf == nil || e.mode != modeEditor {
				return
			}
			n := e.buf.LineCount()
			line := min(int(f), n-1)
			e.state.ScrollTo(line)
			if c := e.state.CursorPos().Line; c < line || c > line+previewCaretReach {
				e.state.GoTo(editor.Pos{Line: line})
			}
			pv.caretLine = e.state.CursorPos().Line
			a.changed()
		})
	}()
}

// togglePreview cycles the focused editor pane of a Markdown file through
// the editor, the web view's preview and MyGo's.
func (a *App) togglePreview(p *Pane) {
	if p == nil || p.Kind != paneEditor || !isMarkdown(p.edit.path) {
		return
	}
	switch p.edit.mode {
	case modePreview:
		a.switchRenderer(p)
	case modeNative:
		p.edit.mode = modeEditor
		a.focusReq = p
		a.changed()
	default:
		a.showPreview(p, true)
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
	pv := a.makeWebView(p)
	if pv == nil {
		return
	}
	a.loadPreviewPage(pv)
}

// makeWebView makes the pane's web view and page, with the pane's
// handlers; nil when it can't, with the pane back in the editor.
func (a *App) makeWebView(p *Pane) *previewState {
	e := p.edit
	if a.win == nil {
		return nil
	}
	log.Printf("preview: making the web view of %s", e.path)
	web, err := a.win.NewWebView(mygo.WebViewOptions{BackgroundColor: hexOf(a.k.bgBottom), Page: mygo.PageOptions{PreloadScript: preloadScript}})
	if err != nil {
		log.Printf("preview: %v", err)
		a.toast(ui.Toast{Title: "Can't show the file", Description: err.Error(), Type: "error"})
		e.mode = modeEditor
		return nil
	}
	pv := &previewState{web: web, caretLine: -1}
	e.preview = pv
	// The pane's current preview: the state moves to the next file's.
	current := func() bool { return p.edit != nil && p.edit.preview == pv }
	page := web.Page()
	pv.navOff = page.OnWillNavigate(func(ev *mygo.NavigateEvent) {
		log.Printf("preview: navigating to %s (user %v)", ev.URL, ev.UserInitiated)
		if strings.HasSuffix(ev.URL, backMarker) {
			ev.PreventDefault()
			a.update(func() {
				if current() {
					a.previewBack(p)
				}
			})
			return
		}
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
	page.OnPageTitleUpdated(func(ev *mygo.TitleEvent) {
		title := ev.Title
		a.update(func() {
			if current() && pv.showing != "" {
				pv.title = title
				a.changed()
			}
		})
	})
	return pv
}

// loadPreviewPage loads the preview's page, at the origin of the host's
// files: the file's directory is set as its base per render.
func (a *App) loadPreviewPage(pv *previewState) {
	pv.loaded, pv.phase = false, "loading the page…"
	html, _ := previewAssets.ReadFile("assets/preview/index.html")
	log.Printf("preview: loading the page")
	pv.web.Page().LoadHTML(strings.ReplaceAll(string(html), "gorex-asset://localhost", assetOrigin), "gorex-file://localhost/")
}

// showFile puts a pane in View on a file the engine shows: an image in
// GoRex's viewer page, a document by the engine itself.
func (a *App) showFile(p *Pane, path string) {
	e := p.edit
	pv := e.preview
	if pv == nil || pv.web == nil || pv.web.IsDestroyed() {
		pv = a.makeWebView(p)
		if pv == nil {
			return
		}
	}
	a.showInPage(pv, path)
}

// showInPage makes the pane's page show a file instead of its document.
func (a *App) showInPage(pv *previewState, path string) {
	pv.showing, pv.title, pv.phase = path, "", ""
	page := pv.web.Page()
	if viewable(path) == "image" {
		html, _ := previewAssets.ReadFile("assets/preview/view.html")
		src, _ := json.Marshal(strings.TrimPrefix(fileURL(path), "gorex-file://localhost"))
		name, _ := json.Marshal(baseName(path))
		doc := strings.ReplaceAll(string(html), "gorex-asset://localhost", assetOrigin)
		doc = strings.Replace(doc, "/*gorex-file*/", "gorexShow("+string(src)+", "+string(name)+", "+hexOfJSON(a.k.bgBottom)+", "+hexOfJSON(a.k.text)+");", 1)
		page.LoadHTML(doc, "gorex-file://localhost/")
		return
	}
	page.LoadURL(fileURL(path))
}

func hexOfJSON(c ui.Color) string {
	js, _ := json.Marshal(hexOf(c))
	return string(js)
}

// previewBack takes a pane's page from a file a link opened back to its
// document; a pane in View has nothing to go back to.
func (a *App) previewBack(p *Pane) {
	e := p.edit
	pv := e.preview
	if pv == nil || pv.showing == "" || e.mode == modeView {
		return
	}
	pv.showing = ""
	a.loadPreviewPage(pv)
	a.changed()
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
				// The tree opens and chooses it.
				if b := t.Browser; b != nil {
					b.files.isDir[target] = true
					b.files.tree.Open.Add(target)
					b.files.reveal = target
					a.changed()
				}
			case viewable(target) != "":
				// The engine shows it in the pane; Back or Escape returns.
				if pv := p.edit.preview; pv != nil && pv.web != nil {
					a.showInPage(pv, target)
					a.changed()
				}
			case isMarkdown(target) || textLike(target):
				a.openFile(t, target, false)
				if np := t.treeFocus(); np != nil && np.edit.path == target && isMarkdown(target) {
					a.previewWhenLoaded(np, fragment)
				}
			default:
				a.toast(ui.Toast{Title: "Can't show " + baseName(target), Description: "Not a file the preview shows.", Timeout: 4 * time.Second})
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

// previewScrollTo scrolls a preview to a heading's id, previewScrollToLine
// to a source line: at once when the page shows the buffer as it is,
// else once the render due has run.
func (a *App) previewScrollTo(p *Pane, id string) {
	js, _ := json.Marshal(id)
	a.previewEval(p, "return gorexScrollTo("+string(js)+")")
}

func (a *App) previewScrollToLine(p *Pane, line int) {
	a.previewEval(p, fmt.Sprintf("return gorexScrollToLine(%d)", line))
}

func (a *App) previewEval(p *Pane, code string) {
	e := p.edit
	pv := e.preview
	if pv == nil || pv.web == nil {
		return
	}
	web := pv.web
	settled := pv.loaded && !pv.due && !pv.busy && e.buf != nil && pv.version == e.buf.Version() && pv.path == e.path
	go func() {
		if !settled {
			time.Sleep(previewDebounce + 250*time.Millisecond)
		}
		web.Page().Eval(code)
	}()
}

// editorLanguage reports whether the editor knows a language for a file
// name.
func editorLanguage(name string) bool { return editor.LanguageFor(name) != nil }
