package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// fileHost is the place a file view is drawn in: an editor pane of a
// tab, or the file popup. It says whether the view has the keys or the
// pointer, and what the header's buttons, the view's links to other
// files and Escape in its page do there. A button whose action is nil
// is left out.
type fileHost struct {
	focused, hovered bool
	// took tells the place the view took the keys, or was pressed.
	took func()
	// The buttons after the view's own: split right and down, move to
	// the editor tab, zoom (zoomed while it is) and close.
	splitRight, splitDown, toTab, zoom, close fileAction
	zoomed                                    bool
	// escape is Escape in the view's page with nothing to go back to.
	escape func()
	// open opens a Markdown or text file a link of the preview names,
	// reveal a folder.
	open   func(path, fragment string)
	reveal func(dir string)
}

// fileAction is a button of the place: its tooltip, the command whose
// keys the tooltip shows (nil for none), and what it does.
type fileAction struct {
	tip string
	cmd *command
	run func()
}

func (h *fileHost) take() {
	if h.took != nil {
		h.took()
	}
}

// paneHost is the place of an editor pane in a tab: the tab's focus,
// splits, zoom and close. A pane of a terminal tab moves to the editor
// tab, and opens the files its preview links to as a click on a file
// does; one of an editor tab opens them in the tab, and folders in its
// browser.
func (a *App) paneHost(t *Tab, p *Pane, focused, hovered bool) *fileHost {
	h := &fileHost{focused: focused, hovered: hovered, zoomed: t.Zoom == p}
	h.took = func() {
		if t.Focus != p {
			t.setFocus(p)
			a.changed()
		}
	}
	h.splitRight = fileAction{"Split Right", &cmdSplitRight, func() { t.setFocus(p); a.split(false) }}
	h.splitDown = fileAction{"Split Down", &cmdSplitDown, func() { t.setFocus(p); a.split(true) }}
	zoom := "Zoom"
	if h.zoomed {
		zoom = "Unzoom"
	}
	h.zoom = fileAction{zoom, &cmdZoom, func() { t.setFocus(p); a.toggleZoom() }}
	h.close = fileAction{"Close Pane", &cmdClosePane, func() {
		if a.confirmUnsaved([]*Pane{p}) {
			a.closePane(p)
		}
	}}
	if t.Kind != paneEditor {
		h.toTab = fileAction{"Move to Editor Tab", nil, func() { a.moveToEditorTab(p) }}
		h.open = func(path, _ string) { a.openFileBy(p, path, false) }
		return h
	}
	h.open = func(path, fragment string) {
		a.openFile(t, path, false)
		if np := t.treeFocus(); np != nil && np.edit.path == path && isMarkdown(path) {
			a.previewWhenLoaded(np.edit, fragment)
		}
	}
	h.reveal = func(dir string) {
		// The tree opens and chooses it.
		if b := t.Browser; b != nil {
			b.files.isDir[dir] = true
			b.files.tree.Open.Add(dir)
			b.files.reveal = dir
			a.changed()
		}
	}
	return h
}

// fileViewUI draws a file view: its header (the file, its modes, the
// place's buttons), the file, and its status line.
func (a *App) fileViewUI(c *ui.Context, k *colors, e *fileView, h *fileHost) {
	e.host = h
	a.fileHeader(c, k, e, h)
	ui.Box(c).Grow(1).MinHeight(0).Padding(0, 5, 6, 5).Children(func() {
		a.fileBody(c, k, e, h)
	})
}

// fileHeader draws a file view's header: the file, a dot when it has
// unsaved changes, its directory, the language, the buttons of its
// modes, and the place's.
func (a *App) fileHeader(c *ui.Context, k *colors, e *fileView, h *fileHost) {
	name, detail := e.label()
	row := ui.Row(c).Height(headerH).Padding(0, 8, 0, 12).Gap(7).AlignItems(ui.Center).MinWidth(0)
	if row.DoubleClicked() && h.zoom.run != nil {
		a.later(c, h.zoom.run)
	}
	if row.Clicked() {
		h.take()
		e.keys = true
	}
	row.Children(func() {
		glyph := "file"
		if e.path != "" && editor.LanguageFor(e.path) != nil {
			glyph = "file-code"
		}
		ui.Icon(c, icon(glyph)).Size(14.5, 14.5).TextColor(k.text)
		ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).ClipX().Children(func() {
			ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidthPercent(80)
			if e.modified() {
				ui.Box(c).Size(7, 7).Radius(4).Background(k.attention).Shrink(0).Tooltip("Unsaved changes")
			}
			if detail != "" {
				ui.Text(c, detail).FontSize(12.5).FontWeight(500).TextColor(k.text.Alpha(0.86)).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			}
			if e.elsewhere {
				ui.Text(c, "closed in another window").FontSize(11.5).TextColor(k.attention).SingleLine().Shrink(0)
			}
			if e.onDisk != "" {
				ui.Text(c, e.onDisk).FontSize(11.5).TextColor(k.attention).SingleLine().Shrink(0).Tooltip("The file on the host is different from this copy. Reload File discards your changes. Save overwrites the file.")
			}
		})
		show := h.focused || h.hovered
		ui.Row(c).Gap(1).AlignItems(ui.Center).Opacity(map[bool]float32{true: 1, false: 0}[show]).Children(func() {
			if !show {
				return
			}
			if lang := langName(e); lang != "" {
				ui.Text(c, lang).FontSize(11.5).TextColor(k.textFaint).SingleLine().Margin(0, 6, 0, 0)
			}
			if pv := e.preview; pv != nil && pv.showing != "" && e.mode == modePreview {
				// A file a link opened in the page: back to the document.
				if iconButton(c, k, "arrow-left", "Back to "+baseName(e.path), 26, 15).Tooltip("Back to the document (Esc)").Clicked() {
					h.take()
					a.previewBack(e)
				}
			}
			if e.buf != nil {
				label := "Show Changes"
				if e.mode == modeChanges {
					label = "Show Editor"
				}
				diff := iconButton(c, k, "diff", label, 26, 15).Tooltip(tip(label, &cmdToggleChanges))
				if e.mode == modeChanges {
					diff.Background(k.pressed)
				}
				if diff.Clicked() {
					h.take()
					a.toggleChanges(e)
				}
				if isMarkdown(e.path) {
					previewing := e.mode == modePreview || e.mode == modeNative
					if previewing {
						// The renderer of the preview: the web view's, or
						// MyGo's, on trial.
						label, tooltip := "Web", "Shown by the web view. Click to use MyGo's renderer."
						if e.mode == modeNative {
							label, tooltip = "MyGo", "Shown by MyGo's renderer. Click to use the web view."
						}
						if textButton(c, k, label).Tooltip(tooltip).Clicked() {
							h.take()
							a.switchRenderer(e)
						}
					}
					label := "Show Preview"
					switch e.mode {
					case modePreview:
						label = "Show MyGo Preview"
					case modeNative:
						label = "Show Editor"
					}
					eye := iconButton(c, k, "eye", label, 26, 15).Tooltip(tip(label, &cmdTogglePreview))
					if previewing {
						eye.Background(k.pressed)
					}
					if eye.Clicked() {
						h.take()
						a.togglePreview(e)
					}
				}
			}
			zoom := "maximize-2"
			if h.zoomed {
				zoom = "minimize-2"
			}
			for _, b := range []struct {
				act      fileAction
				glyph    string
				iconSize float32
			}{
				{h.splitRight, "columns-2", 16},
				{h.splitDown, "rows-2", 16},
				{h.toTab, "square-arrow-out-up-right", 15},
				{h.zoom, zoom, 16},
				{h.close, "x", 17},
			} {
				if b.act.run == nil {
					continue
				}
				label := b.act.tip
				if b.act.cmd != nil {
					label = tip(label, b.act.cmd)
				}
				if iconButton(c.Key(b.glyph), k, b.glyph, b.act.tip, 26, b.iconSize).Tooltip(label).Clicked() {
					a.later(c, b.act.run)
				}
			}
		})
	})
}

func langName(e *fileView) string {
	if e.buf == nil {
		return ""
	}
	if l := e.buf.Language(); l != nil {
		return l.Name
	}
	return "Plain Text"
}

// fileBody draws the file of a view: the editor, the Changes view, a
// preview or the engine's view, with its status line; or why the file
// is not shown.
func (a *App) fileBody(c *ui.Context, k *colors, e *fileView, h *fileHost) {
	col := ui.Column(c).Fill().MinHeight(0)
	col.Children(func() {
		if e.mode == modeView {
			a.viewBody(c, k, e, h)
			return
		}
		switch {
		case e.path == "":
			a.editorNote(c, k, "No file open", "Choose a file in the browser, or select one and press Enter, to open it here.")
		case e.loading:
			a.editorNote(c, k, "Loading…", e.path)
		case e.err != "":
			ui.Column(c).Grow(1).Center().Gap(10).Padding(16).Children(func() {
				ui.Text(c, e.err).FontSize(12.5).TextColor(k.textMuted).MaxWidth(520)
				ui.Row(c).Gap(8).Children(func() {
					if e.tooLarge && textButton(c, k, "Open Anyway").Clicked() {
						a.loadFile(e, -1)
					}
					if textButton(c, k, "Reload").Clicked() {
						a.reloadFile(e)
					}
				})
			})
		}
		if e.buf == nil || e.loading || e.err != "" {
			// Without an editor, the view itself takes the focus, so that
			// its place keeps it and its keys apply.
			col.Focusable()
			a.fileFocus(col, e, h)
			return
		}
		font := a.editorFont()
		wrap := is(a.eff.Editor.SoftWrap)
		if e.wrap != nil {
			wrap = *e.wrap
		}
		if e.mode == modeNative {
			a.nativeBody(c, k, e, h)
		} else if e.mode == modePreview {
			pv := e.preview
			if pv == nil || pv.web == nil || pv.web.IsDestroyed() {
				a.ensurePreview(e)
				pv = e.preview
			}
			if pv != nil && pv.web != nil && !pv.loaded {
				a.editorNote(c, k, "Preview", pv.phase)
				col.Focusable()
				a.fileFocus(col, e, h)
				return
			}
			if pv != nil && pv.web != nil {
				if pv.version != e.buf.Version() {
					a.renderPreview(e)
				}
				v := ui.WebView(c.Key("preview"), pv.web).Grow(1).FillWidth().MinHeight(0)
				a.webFocus(v, pv, e, h)
			} else {
				a.editorNote(c, k, "No preview", "The web view could not be created.")
			}
		} else if e.mode == modeChanges {
			d := e.diff
			d.Font, d.Wrap = font, wrap
			d.Layout = editor.DiffSplit
			if a.eff.Editor.DiffLayout == "unified" {
				d.Layout = editor.DiffUnified
			}
			switch a.eff.Editor.DiffInline {
			case "characters":
				d.Inline = editor.DiffInlineChar
			case "off":
				d.Inline = editor.DiffInlineNone
			default:
				d.Inline = editor.DiffInlineWordAlt
			}
			if len(d.Hunks()) == 0 {
				a.editorNote(c, k, "No changes", "Edit "+baseName(e.path)+" to see its changes here.")
			} else {
				v := editor.DiffView(c.Key("diff"), d).Grow(1).FillWidth().MinHeight(0)
				a.fileFocus(v, e, h)
			}
		} else {
			st := e.state
			st.Font, st.SoftWrap = font, wrap
			st.LineHeight = a.eff.Editor.LineHeight
			st.WrapGuides = wrapGuides(a.eff.Editor.WrapGuides)
			v := editor.View(c.Key("editor"), st).Grow(1).FillWidth().MinHeight(0)
			a.fileFocus(v, e, h)
		}
		a.editorStatus(c, k, e)
	})
}

// fileFocus gives an element of a view the keys the view was asked for,
// and tells the place when the view takes them.
func (a *App) fileFocus(v ui.Element, e *fileView, h *fileHost) {
	if e.keys {
		v.Focus()
		e.keys = false
	}
	if v.Focused() && !h.focused {
		h.take()
	}
}

// webFocus is fileFocus for a view's web view, whose focus is the
// engine's: a press in it tells the place.
func (a *App) webFocus(v ui.Element, pv *previewState, e *fileView, h *fileHost) {
	if e.keys {
		pv.web.Focus()
		e.keys = false
	}
	if v.Pressed() && !h.focused {
		h.take()
	}
}

func (a *App) editorNote(c *ui.Context, k *colors, title, text string) {
	ui.Column(c).Grow(1).Center().Gap(6).Padding(16).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(k.text)
		ui.Text(c, text).FontSize(12.5).TextColor(k.textMuted).MaxWidth(520)
	})
}

// viewBody shows a file the engine shows, in the view's web view, with
// what the page says of it in the status line.
func (a *App) viewBody(c *ui.Context, k *colors, e *fileView, h *fileHost) {
	pv := e.preview
	if pv == nil || pv.web == nil || pv.web.IsDestroyed() || pv.showing != e.path {
		a.showFile(e, e.path)
		pv = e.preview
		if e.modTime.IsZero() {
			// Its time, for the folder's changes to compare with.
			a.checkFile(e)
		}
	}
	if pv == nil || pv.web == nil {
		a.editorNote(c, k, "Can't show "+baseName(e.path), "The web view could not be created.")
		return
	}
	v := ui.WebView(c.Key("view"), pv.web).Grow(1).FillWidth().MinHeight(0)
	a.webFocus(v, pv, e, h)
	status := pv.title
	if status == "" {
		status = strings.ToUpper(strings.TrimPrefix(path.Ext(e.path), "."))
		if pv.phase != "" {
			status += " · " + pv.phase
		}
	}
	ui.Row(c).Gap(16).Padding(3, 10, 2, 10).Shrink(0).Children(func() {
		ui.Text(c, status).FontSize(11.5).TextColor(k.textFaint).SingleLine()
		ui.Box(c).Grow(1)
		ui.Text(c, "View").FontSize(11.5).TextColor(k.textFaint).SingleLine()
	})
}

// editorStatus is the row under an editor: the caret, the changes, the
// lines and the language.
func (a *App) editorStatus(c *ui.Context, k *colors, e *fileView) {
	st := e.state
	pos := st.CursorPos()
	status := fmt.Sprintf("Ln %d, Col %d", pos.Line+1, pos.Col+1)
	if n := len(st.Selections()); n > 1 {
		status += fmt.Sprintf(" · %d selections", n)
	}
	if added, deleted := e.diff.Stats(); added > 0 || deleted > 0 {
		status += fmt.Sprintf(" · +%d −%d", added, deleted)
	}
	switch e.mode {
	case modePreview:
		status = "Preview"
		if pv := e.preview; pv != nil && pv.phase != "" {
			status = "Preview · " + pv.phase
		}
	case modeNative:
		status = "Preview · MyGo"
	}
	ui.Row(c).Gap(16).Padding(3, 10, 2, 10).Shrink(0).Children(func() {
		ui.Text(c, status).FontSize(11.5).TextColor(k.textFaint).SingleLine()
		ui.Box(c).Grow(1)
		ui.Text(c, fmt.Sprintf("%d lines · %s", e.buf.LineCount(), langName(e))).FontSize(11.5).TextColor(k.textFaint).SingleLine()
	})
}

// editorFont is the editor's font: its own, or the terminal's.
func (a *App) editorFont() ui.Font {
	fam, size := a.eff.Editor.Font, a.eff.Editor.Size
	if fam == "" {
		fam = a.eff.Terminal.Font
	}
	if size == 0 {
		size = a.eff.Terminal.Size
	}
	return ui.Font{Family: termFamily(fam), Size: size}
}

// wrapGuides parses the columns of the setting, "80, 100".
func wrapGuides(s string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if n, err := strconv.Atoi(f); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// toggleChanges switches a file view between the editor and its
// changes.
func (a *App) toggleChanges(e *fileView) {
	if e == nil || e.buf == nil {
		return
	}
	if e.mode == modeChanges {
		e.mode = modeEditor
	} else {
		e.mode = modeChanges
	}
	e.keys = true
	a.changed()
}

// toggleWrap switches a file view's soft wrap.
func (a *App) toggleWrap(e *fileView) {
	if e == nil {
		return
	}
	wrap := is(a.eff.Editor.SoftWrap)
	if e.wrap != nil {
		wrap = *e.wrap
	}
	wrap = !wrap
	e.wrap = &wrap
}
