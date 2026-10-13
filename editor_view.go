package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// editorHeader draws an editor pane's header: the file, a dot when it
// has unsaved changes, its directory, the language, and the buttons of a
// terminal pane's with one for the Changes mode before them.
func (a *App) editorHeader(c *ui.Context, k *colors, t *Tab, p *Pane, focused, hovered bool) {
	e := p.edit
	name, detail := e.label()
	h := ui.Row(c).Height(headerH).Padding(0, 8, 0, 12).Gap(7).AlignItems(ui.Center).MinWidth(0)
	if h.DoubleClicked() {
		t.setFocus(p)
		a.toggleZoom()
	}
	if h.Clicked() {
		t.setFocus(p)
		a.focusReq = p
	}
	h.Children(func() {
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
				ui.Text(c, "closed elsewhere").FontSize(11.5).TextColor(k.attention).SingleLine().Shrink(0)
			}
			if e.onDisk != "" {
				ui.Text(c, e.onDisk).FontSize(11.5).TextColor(k.attention).SingleLine().Shrink(0).Tooltip("The file on the host differs from this buffer: Reload File drops your changes, Save overwrites it.")
			}
		})
		show := focused || hovered
		ui.Row(c).Gap(1).AlignItems(ui.Center).Opacity(map[bool]float32{true: 1, false: 0}[show]).Children(func() {
			if !show {
				return
			}
			if lang := langName(e); lang != "" {
				ui.Text(c, lang).FontSize(11.5).TextColor(k.textFaint).SingleLine().Margin(0, 6, 0, 0)
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
					t.setFocus(p)
					a.toggleChanges(p)
				}
				if isMarkdown(e.path) {
					label := "Show Preview"
					if e.mode == modePreview {
						label = "Show Editor"
					}
					eye := iconButton(c, k, "eye", label, 26, 15).Tooltip(tip(label, &cmdTogglePreview))
					if e.mode == modePreview {
						eye.Background(k.pressed)
					}
					if eye.Clicked() {
						t.setFocus(p)
						a.togglePreview(p)
					}
				}
			}
			if iconButton(c, k, "columns-2", "Split Right", 26, 16).Tooltip(tip("Split Right", &cmdSplitRight)).Clicked() {
				t.setFocus(p)
				a.split(false)
			}
			if iconButton(c, k, "rows-2", "Split Down", 26, 16).Tooltip(tip("Split Down", &cmdSplitDown)).Clicked() {
				t.setFocus(p)
				a.split(true)
			}
			zoom, label := "maximize-2", "Zoom"
			if t.Zoom == p {
				zoom, label = "minimize-2", "Unzoom"
			}
			if iconButton(c, k, zoom, "Zoom", 26, 16).Tooltip(tip(label, &cmdZoom)).Clicked() {
				t.setFocus(p)
				a.toggleZoom()
			}
			if iconButton(c, k, "x", "Close Pane", 26, 17).Tooltip(tip("Close Pane", &cmdClosePane)).Clicked() {
				a.later(c, func() {
					if a.confirmUnsaved([]*Pane{p}) {
						a.closePane(p)
					}
				})
			}
		})
	})
}

func langName(e *editorState) string {
	if e.buf == nil {
		return ""
	}
	if l := e.buf.Language(); l != nil {
		return l.Name
	}
	return "Plain Text"
}

// editorBody draws the editor, or the Changes view, of a pane, with its
// status line; or why the file is not shown.
func (a *App) editorBody(c *ui.Context, k *colors, t *Tab, p *Pane) {
	e := p.edit
	col := ui.Column(c).Fill().MinHeight(0)
	col.Children(func() {
		switch {
		case e.path == "":
			a.editorNote(c, k, "No file open", "Choose a file in the browser, or press Enter on one to open it here.")
		case e.loading:
			a.editorNote(c, k, "Loading…", e.path)
		case e.err != "":
			ui.Column(c).Grow(1).Center().Gap(10).Padding(16).Children(func() {
				ui.Text(c, e.err).FontSize(12.5).TextColor(k.textMuted).MaxWidth(520)
				ui.Row(c).Gap(8).Children(func() {
					if e.tooLarge && textButton(c, k, "Open anyway").Clicked() {
						a.loadFile(p, -1)
					}
					if textButton(c, k, "Reload").Clicked() {
						a.reloadFile(p)
					}
				})
			})
		}
		if e.buf == nil || e.loading || e.err != "" {
			// Without an editor, the pane itself takes the focus, so that
			// it stays the focused pane and its keys apply.
			col.Focusable()
			a.editorFocus(col, t, p)
			return
		}
		font := a.editorFont()
		wrap := is(a.eff.Editor.SoftWrap)
		if e.wrap != nil {
			wrap = *e.wrap
		}
		if e.mode == modePreview {
			pv := e.preview
			if pv == nil || pv.web == nil || pv.web.IsDestroyed() {
				a.ensurePreview(p)
				pv = e.preview
			}
			if pv != nil && pv.web != nil && !pv.loaded {
				a.editorNote(c, k, "Preview", pv.phase)
				col.Focusable()
				a.editorFocus(col, t, p)
				return
			}
			if pv != nil && pv.web != nil {
				if pv.version != e.buf.Version() {
					a.renderPreview(p)
				}
				v := ui.WebView(c.Key("preview"), pv.web).Grow(1).FillWidth().MinHeight(0)
				if a.focusReq == p && a.tab() == t {
					pv.web.Focus()
					a.focusReq = nil
				}
				if v.Pressed() && t.Focus != p {
					t.setFocus(p)
					a.changed()
				}
			} else {
				a.editorNote(c, k, "No preview", "The web view could not be made.")
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
				a.editorFocus(v, t, p)
			}
		} else {
			st := e.state
			st.Font, st.SoftWrap = font, wrap
			st.LineHeight = a.eff.Editor.LineHeight
			st.WrapGuides = wrapGuides(a.eff.Editor.WrapGuides)
			v := editor.View(c.Key("editor"), st).Grow(1).FillWidth().MinHeight(0)
			a.editorFocus(v, t, p)
		}
		a.editorStatus(c, k, e)
	})
}

// editorFocus gives an editor view the focus the tab asks, and follows
// the focus it takes.
func (a *App) editorFocus(v ui.Element, t *Tab, p *Pane) {
	if a.focusReq == p && a.tab() == t {
		v.Focus()
		a.focusReq = nil
	}
	if v.Focused() && t.Focus != p {
		t.setFocus(p)
		a.changed()
	}
}

func (a *App) editorNote(c *ui.Context, k *colors, title, text string) {
	ui.Column(c).Grow(1).Center().Gap(6).Padding(16).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(k.text)
		ui.Text(c, text).FontSize(12.5).TextColor(k.textMuted).MaxWidth(520)
	})
}

// editorStatus is the row under an editor: the caret, the changes, the
// lines and the language.
func (a *App) editorStatus(c *ui.Context, k *colors, e *editorState) {
	st := e.state
	pos := st.CursorPos()
	status := fmt.Sprintf("Ln %d, Col %d", pos.Line+1, pos.Col+1)
	if n := len(st.Selections()); n > 1 {
		status += fmt.Sprintf(" · %d selections", n)
	}
	if added, deleted := e.diff.Stats(); added > 0 || deleted > 0 {
		status += fmt.Sprintf(" · +%d −%d", added, deleted)
	}
	if e.mode == modePreview {
		status = "Preview"
		if pv := e.preview; pv != nil && pv.phase != "" {
			status = "Preview · " + pv.phase
		}
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

// toggleChanges switches an editor pane between the editor and its
// changes.
func (a *App) toggleChanges(p *Pane) {
	if p == nil || p.Kind != paneEditor || p.edit.buf == nil {
		return
	}
	if p.edit.mode == modeChanges {
		p.edit.mode = modeEditor
	} else {
		p.edit.mode = modeChanges
	}
	a.focusReq = p
	a.changed()
}

// toggleWrap switches an editor pane's soft wrap.
func (a *App) toggleWrap(p *Pane) {
	if p == nil || p.Kind != paneEditor {
		return
	}
	wrap := is(a.eff.Editor.SoftWrap)
	if p.edit.wrap != nil {
		wrap = *p.edit.wrap
	}
	wrap = !wrap
	p.edit.wrap = &wrap
}
