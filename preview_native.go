package main

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// The MyGo preview of an editor pane, a trial beside the web view's:
// MyGo's ui/markdown shows the buffer in native UI. The web view's stays
// the preview; Ctrl+Shift+V goes on from it to this one, and the chip in
// the header switches between them. Links to the web and to headings
// work; links to the host's files, scroll sync with the editor, Mermaid
// and raw HTML are the web view's alone. A pane saved in it comes back
// in the web view's.

// modeNative is an editor pane showing the MyGo preview.
const modeNative = "native"

// nativePreview is an editor pane's MyGo preview: the text it shows, of
// the buffer's version it has, and the view's options, made anew for
// each file, which load its images from its directory.
type nativePreview struct {
	path    string
	text    string
	version uint64
	due     bool
	opts    *markdown.Options
}

// switchRenderer moves a view in preview between the web view's preview
// and MyGo's.
func (a *App) switchRenderer(e *fileView) {
	switch e.mode {
	case modePreview:
		e.mode = modeNative
	case modeNative:
		a.showPreview(e, true)
	default:
		return
	}
	e.keys = true
	a.changed()
}

// nativeBody draws the MyGo preview of a view's buffer, which follows
// edits a moment after them, as the web view's does.
func (a *App) nativeBody(c *ui.Context, k *colors, e *fileView, h *fileHost) {
	np := e.native
	if np == nil || np.path != e.path {
		np = &nativePreview{path: e.path, text: e.buf.String(), version: e.buf.Version()}
		np.opts = &markdown.Options{LoadImage: a.hostImages(dirName(e.path))}
		e.native = np
	}
	if v := e.buf.Version(); v != np.version && !np.due {
		np.due = true
		time.AfterFunc(previewDebounce, func() {
			a.update(func() {
				np.due = false
				if e.native != np || e.buf == nil {
					return
				}
				np.text, np.version = e.buf.String(), e.buf.Version()
				a.changed()
			})
		})
	}
	np.opts.Theme = &markdown.Theme{Text: k.text, Muted: k.textMuted, Link: k.busy, CodeFont: a.editorFont().Family}
	s := ui.Scroll(c.Key("native-preview")).Grow(1).MinHeight(0).Background(k.bgBottom).Padding(16, 28).Focusable()
	s.Children(func() {
		markdown.View(c, np.text, np.opts)
	})
	a.fileFocus(s, e, h)
}

// hostImages loads the images of a Markdown file of the host's, as the
// web view's preview does through gorex-file: paths relative to its
// directory, read through the window's server. Images on the web are not
// loaded: they show their descriptions.
func (a *App) hostImages(dir string) func(url string) (ui.ImageSource, error) {
	return func(u string) (ui.ImageSource, error) {
		if strings.Contains(u, "://") || strings.HasPrefix(u, "data:") {
			return nil, os.ErrNotExist
		}
		target := strings.TrimPrefix(u, "file:")
		if i := strings.IndexAny(target, "?#"); i >= 0 {
			target = target[:i]
		}
		if !strings.HasPrefix(target, "/") && !(len(target) > 1 && target[1] == ':') {
			target = joinPath(dir, target)
		}
		if hostGOOS == "windows" {
			target = strings.ReplaceAll(target, "/", `\`)
		}
		client := a.client
		if client == nil {
			return nil, os.ErrNotExist
		}
		fd, err := client.ReadFile(target, previewFileCap)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(path.Ext(target), ".svg") {
			return ui.ParseSVG(fd.Data)
		}
		return ui.DecodeBitmap(fd.Data)
	}
}
