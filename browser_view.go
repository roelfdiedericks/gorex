package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// browserHeader draws a file browser's header: the tree glyph, the root,
// and a Refresh button and an overflow menu.
func (a *App) browserHeader(c *ui.Context, k *colors, t *Tab, p *Pane, focused, hovered bool) {
	b := p.files
	h := ui.Row(c).Height(headerH).Padding(0, 8, 0, 12).Gap(7).AlignItems(ui.Center).MinWidth(0)
	if h.Clicked() {
		t.setFocus(p)
		a.focusReq = p
	}
	h.Children(func() {
		ui.Icon(c, icon("folder-tree")).Size(14.5, 14.5).TextColor(k.text)
		ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).ClipX().Children(func() {
			ui.Text(c, baseName(b.root)).FontSize(12.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidthPercent(80)
			ui.Text(c, shortDir(b.root)).FontSize(12.5).FontWeight(500).TextColor(k.text.Alpha(0.86)).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		})
		show := focused || hovered
		ui.Row(c).Gap(1).AlignItems(ui.Center).Opacity(map[bool]float32{true: 1, false: 0}[show]).Children(func() {
			if !show {
				return
			}
			if iconButton(c, k, "refresh-cw", "Refresh", 26, 14).Tooltip(tip("Refresh Files", &cmdRefreshFiles)).Clicked() {
				a.refreshFocusedDir(p)
			}
			more := iconButton(c, k, "ellipsis", "More", 26, 16).Tooltip("More")
			more.Menu(func(m *ui.Menu) {
				if m.Item("Hidden files").Checked(b.hidden).Chosen() {
					b.hidden = !b.hidden
					a.changed()
				}
				if m.Item("Change root…").Chosen() {
					b.changing, b.rootField = true, b.root
				}
				if m.Item("Root at home").Disabled(samePath(b.root, hostHome)).Chosen() {
					a.setRoot(p, hostHome)
				}
			})
		})
	})
}

// refreshFocusedDir reads the chosen directory again: the directory of
// the chosen row, or the root.
func (a *App) refreshFocusedDir(p *Pane) {
	b := p.files
	dir := b.root
	if b.row >= 0 && b.row < b.tree.Rows() {
		item := b.tree.Item(b.row)
		if b.isDir[item] {
			dir = item
		} else {
			dir = dirName(item)
		}
	}
	a.refreshDir(p, dir)
}

// browserBody draws the tree of a file browser, or the Change root field
// and the root's error.
func (a *App) browserBody(c *ui.Context, k *colors, t *Tab, p *Pane) {
	b := p.files
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		if b.changing {
			ui.Row(c).Padding(4, 8, 8, 8).Gap(6).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c, &b.rootField).Grow(1).FontSize(12.5).Placeholder("a directory of " + a.hostName()).AutoFocus()
				if in.Submitted() {
					a.setRoot(p, b.rootField)
					b.changing = false
				}
				if textButton(c, k, "Go").Clicked() {
					a.setRoot(p, b.rootField)
					b.changing = false
				}
				if iconButton(c, k, "x", "Cancel", 22, 13).Clicked() {
					b.changing = false
				}
			})
		}
		if b.rootErr != "" {
			ui.Column(c).Grow(1).Padding(12).Gap(8).Children(func() {
				ui.Text(c, "Can't read "+b.root).FontSize(12.5).FontWeight(600).TextColor(k.text)
				ui.Text(c, b.rootErr).FontSize(12).TextColor(k.textMuted)
				if textButton(c, k, "Change root…").Clicked() {
					b.changing, b.rootField = true, b.root
				}
			})
			return
		}
		b.tree.List.Selected = &b.row
		b.tree.List.Label = func(row int) string { return baseName(b.tree.Item(row)) }
		focusedFile := ""
		if f := t.treeFocus(); f != nil && f.Kind == paneEditor {
			focusedFile = f.edit.path
		}
		modified := map[string]bool{}
		for _, q := range t.panes() {
			if q.Kind == paneEditor && q.edit.modified() {
				modified[q.edit.path] = true
			}
		}
		o := ui.Outline(c, &b.tree, []string{b.root}, func(dir string) []string { return a.browserEntries(p, dir) }, func(item string) {
			a.browserRow(c, k, b, item, item == focusedFile, modified[item])
		}).Grow(1).MinHeight(0)
		if a.focusReq == p && a.tab() == t {
			o.Focus()
			a.focusReq = nil
		}
		if o.Focused() && t.Focus != p {
			t.setFocus(p)
			a.changed()
		}
		if o.Changed() && b.row >= 0 {
			if item := b.tree.Item(b.row); !b.isDir[item] {
				a.openFile(t, item, c.Modifiers()&ui.Shift != 0)
			} else {
				a.changed()
			}
		}
		if o.Submitted() && b.row >= 0 {
			if item := b.tree.Item(b.row); !b.isDir[item] {
				a.openFile(t, item, true)
			}
		}
		if o.Focused() && c.Shortcut(0, ui.KeyF5) {
			a.refreshFocusedDir(p)
		}
	})
}

// browserRow draws a row of the tree: its icon and name, the accent color
// on the focused editor's file, and a dot after a file with unsaved
// changes.
func (a *App) browserRow(c *ui.Context, k *colors, b *browserState, item string, current, modified bool) {
	name := baseName(item)
	if item == b.root {
		name = b.root
		if h := hostHome; samePath(item, h) {
			name = "~"
		}
	}
	glyph := "file"
	switch {
	case b.isDir[item] && b.tree.Open.Has(item):
		glyph = "folder-open"
	case b.isDir[item]:
		glyph = "folder"
	case editor.LanguageFor(item) != nil:
		glyph = "file-code"
	}
	col, iconCol := k.text, k.iconMuted
	if current {
		col, iconCol = k.busy, k.busy
	}
	if modified {
		name += " •"
	}
	if b.errs[item] != "" {
		iconCol = k.attention
	}
	if strings.HasPrefix(name, ".") && !current {
		col = k.textMuted
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
		ui.Icon(c, icon(glyph)).Size(14, 14).TextColor(iconCol).Shrink(0)
		ui.Text(c, name).FontSize(12.5).TextColor(col).SingleLine().Ellipsis("…").MinWidth(0)
	})
}
