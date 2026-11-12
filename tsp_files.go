package main

import (
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Files a program names (a tool card's file, a file a message names) open
// as the OMP settings say: a click in a popup over the pane, a Ctrl+click
// (⌘ on macOS) in an editor tab, or as set. The popup shows the file as an
// editor pane does (fileViewUI), with the pane's buttons: split right or
// down, and the editor tab, keep the file there as it is; zoom fills the
// window; close, Escape or a click outside close it, asking first about
// unsaved changes.

// filePeek is the file popup: the file's view, kept in no tab of the
// window; the pane it is over, which has the focus back as it closes;
// and whether it fills the window.
type filePeek struct {
	view *fileView
	over *Pane
	big  bool
}

// tspCodeLinker makes the code of a pane's messages that names a file of
// the session's folder a link to it (file://), while the settings say so:
// code that looks like a file's name is looked up on the host once, in
// the background, and links once it is known to be a file.
func (a *App) tspCodeLinker(p *Pane) func(code string) (string, bool) {
	if !is(a.eff.OMP.FileLinks) {
		return nil
	}
	return func(code string) (string, bool) {
		if !looksLikeFile(code) || a.client == nil || a.bind.noFiles() {
			return "", false
		}
		file := mediaPath(p, code)
		if a.fileKnown == nil || len(a.fileKnown) > 4096 {
			a.fileKnown = map[string]bool{}
		}
		isFile, asked := a.fileKnown[file]
		if !asked {
			a.fileKnown[file] = false
			client := a.client
			go func() {
				fi, err := client.Stat(file)
				if err == nil && !fi.Dir {
					a.laterService(func() { a.fileKnown[file] = true })
				}
			}()
			return "", false
		}
		if !isFile {
			return "", false
		}
		slashed := strings.ReplaceAll(file, `\`, "/")
		if !strings.HasPrefix(slashed, "/") {
			slashed = "/" + slashed
		}
		return (&url.URL{Scheme: "file", Path: slashed}).String(), true
	}
}

// looksLikeFile reports whether code may name a file: one word with a
// folder in it, or a name with an extension of letters and digits, at
// least one a letter; no address, option or number.
func looksLikeFile(code string) bool {
	if code == "" || len(code) > 260 || strings.ContainsAny(code, " \t\n\"'`<>|*?") || strings.Contains(code, "://") || strings.HasPrefix(code, "-") {
		return false
	}
	name := code[strings.LastIndexAny(code, `/\`)+1:]
	dot := strings.LastIndex(name, ".")
	if dot <= 0 || dot == len(name)-1 {
		return strings.ContainsAny(strings.TrimRight(code, `/\`), `/\`) && name != ""
	}
	ext := name[dot+1:]
	letter := false
	for _, r := range ext {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letter = true
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return letter && len(ext) <= 10
}

// tspOpenLink opens a link of a message: a file of the host's as a click
// on it does (with Ctrl, or ⌘ on macOS, held now or not), a web address
// in the browser.
func (a *App) tspOpenLink(c *ui.Context, p *Pane, dest string) {
	if file := tspFileHref(dest); file != "" {
		a.openFileBy(p, file, c.Modifiers()&(ui.Ctrl|ui.Cmd) != 0)
		return
	}
	if webURL(dest) {
		mygo.Shell.OpenExternal(dest)
	}
}

// tspLinkItems are the items of a message's link's menu: open a file
// either way or copy its path; open a web address or copy it.
func (a *App) tspLinkItems(m *ui.Menu, p *Pane, dest string) {
	if file := tspFileHref(dest); file != "" {
		if m.Item("Open in Popup").Chosen() {
			a.openPeek(p, file)
		}
		if m.Item("Open in Editor Tab").Chosen() {
			a.openPaneFile(p, file)
		}
		if m.Item("Copy Path").Chosen() {
			a.copyLink(file)
		}
		return
	}
	if webURL(dest) && m.Item("Open Link").Chosen() {
		mygo.Shell.OpenExternal(dest)
	}
	if m.Item("Copy Link").Chosen() {
		a.copyLink(dest)
	}
}

// tspFileHref is the path on the host of a node's file link (an href of
// file://), "" for none.
func tspFileHref(href string) string {
	u, err := url.Parse(href)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return hostPathOf(u)
}

// openFileBy opens a file of the host's as a click on it does: with Ctrl
// (⌘ on macOS), or without.
func (a *App) openFileBy(p *Pane, path string, ctrl bool) {
	how := a.eff.OMP.FileClick
	if ctrl {
		how = a.eff.OMP.FileCtrlClick
	}
	switch how {
	case "popup":
		a.openPeek(p, path)
	case "tab":
		a.openPaneFile(p, path)
	}
}

// fileLink makes el, a file's name, open the file on a click, as the
// settings say, with a menu to open it either way or copy its path. It
// reports whether el was clicked.
func (a *App) fileLink(el ui.Element, p *Pane, path string) bool {
	el.Cursor(ui.CursorPointer).Tooltip(path + "\nClick: " + fileHow(a.eff.OMP.FileClick) + " · Ctrl+click: " + fileHow(a.eff.OMP.FileCtrlClick))
	el.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open in Popup").Chosen() {
			a.openPeek(p, path)
		}
		if m.Item("Open in Editor Tab").Chosen() {
			a.openPaneFile(p, path)
		}
		if m.Item("Copy Path").Chosen() {
			a.copyLink(path)
		}
	})
	if !el.Clicked() {
		return false
	}
	a.openFileBy(p, path, el.ClickModifiers()&(ui.Ctrl|ui.Cmd) != 0)
	return true
}

// fileHow names what a click setting does, for a tooltip.
func fileHow(how string) string {
	switch how {
	case "popup":
		return "show in a popup"
	case "tab":
		return "open in an editor tab"
	}
	return "nothing"
}

// openPeek shows a file of the host's in the popup over pane p, in place
// of the one it shows.
func (a *App) openPeek(p *Pane, path string) {
	file := mediaPath(p, path)
	if !mediaOpenable(file) {
		a.toast(ui.Toast{Title: "Can't show " + baseName(file), Description: "GoRex's viewer and editor can't open this kind of file.", Timeout: 4 * time.Second})
		return
	}
	if a.bind.noFiles() {
		a.toast(ui.Toast{ID: "plain/editor", Title: "Can't show " + baseName(file), Description: a.noFilesReason() + ".", Timeout: 4 * time.Second})
		return
	}
	big := false
	if pk := a.peek; pk != nil {
		if pk.view.path == file {
			return
		}
		if !a.confirmFile(pk.view) {
			return
		}
		big = pk.big
		a.dropPeek()
	}
	// Opened as the file browser opens it: Markdown in Preview when the
	// setting says so.
	e := a.newFileView(file)
	if e.buf == nil && isMarkdown(file) && is(a.eff.Editor.PreviewOnOpen) {
		e.previewOnLoad = true
	}
	e.keys = true
	a.peek = &filePeek{view: e, over: p, big: big}
	a.changed()
}

// closePeek closes the popup, asking first about unsaved changes; the pane
// it was over has the focus back.
func (a *App) closePeek() {
	pk := a.peek
	if pk == nil || !a.confirmFile(pk.view) {
		return
	}
	a.dropPeek()
	a.focusReq = pk.over
	a.changed()
}

// dropPeek takes the popup away, its file with it.
func (a *App) dropPeek() {
	if pk := a.peek; pk != nil {
		a.dropPreview(pk.view)
		pk.view.gone = true
		a.peek = nil
	}
}

// keepPeek keeps the popup's file, as it is (its text, its scroll and
// mode, its unsaved changes), in an editor pane split beside the pane it
// was over, right of it or below it; in the editor tab when that pane is
// gone.
func (a *App) keepPeek(vertical bool) {
	pk := a.peek
	if pk == nil {
		return
	}
	over := pk.over
	at := -1
	for i, t := range a.tabs {
		if t == over.Tab {
			at = i
		}
	}
	if over.closed || over.Node == nil || at < 0 {
		a.keepPeekTab()
		return
	}
	a.peek = nil
	a.selectTab(at)
	a.splitWith(over, &Pane{ID: a.id(), Kind: paneEditor, edit: pk.view}, vertical)
}

// keepPeekTab keeps the popup's file, as it is, in the editor tab.
func (a *App) keepPeekTab() {
	pk := a.peek
	if pk == nil {
		return
	}
	t := a.editorTabFor(dirName(pk.view.path))
	if t == nil {
		return
	}
	a.peek = nil
	a.putInTab(t, &Pane{ID: a.id(), Kind: paneEditor, edit: pk.view})
}

// peekHost is the popup as the place of its file view: its buttons keep
// the file in a split or the editor tab, fill the window, or close it,
// as Escape does; links to other files show them in the popup.
func (a *App) peekHost(pk *filePeek) *fileHost {
	h := &fileHost{focused: true, zoomed: pk.big, escape: a.closePeek}
	h.splitRight = fileAction{"Keep in a Split on the Right", &cmdSplitRight, func() { a.keepPeek(false) }}
	h.splitDown = fileAction{"Keep in a Split Below", &cmdSplitDown, func() { a.keepPeek(true) }}
	h.toTab = fileAction{"Keep in the Editor Tab", nil, a.keepPeekTab}
	zoom := "Fill the Window"
	if pk.big {
		zoom = "Restore Size"
	}
	h.zoom = fileAction{zoom, &cmdZoom, a.toggleZoom}
	h.close = fileAction{"Close", &cmdClosePane, a.closePeek}
	h.open = func(path, _ string) { a.openPeek(pk.over, path) }
	return h
}

// drawPeek draws the popup over the window: the file as an editor pane
// shows it, in a card like a pane's, and a line saying how it closes.
func (a *App) drawPeek(c *ui.Context, k *colors) {
	pk := a.peek
	if pk == nil {
		return
	}
	win := c.Root().Bounds()
	w, h := win.W*0.8, (win.H-titleH)*0.85
	if pk.big {
		w, h = win.W-24, win.H-titleH-24
	}
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("peek-back")).Absolute().Fill().Background(k.backdrop)
		if back.Clicked() {
			a.later(c, a.closePeek)
		}
		place := ui.Row(c.Key("peek-place")).Absolute().Left(0).Right(0).Top(titleH).Bottom(0).Justify(ui.Center).AlignItems(ui.Center).PassThrough()
		place.Children(func() {
			card := ui.Column(c.Key("peek")).Width(w).Height(h).Radius(cardR).Clip().
				Background(k.cardFocused).Border(1, k.cardBorderFocused).
				Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28))
			card.Children(func() {
				ui.Column(c.Key("peek-file")).Grow(1).MinHeight(0).FillWidth().Children(func() {
					a.fileViewUI(c, k, pk.view, a.peekHost(pk))
				})
				ui.Text(c.Key("peek-hint"), "Click outside or press Esc to close.").FontSize(11.5).TextColor(k.textMuted).TextAlign(ui.Center).FillWidth().
					Padding(6, 12).BorderWidth(1, 0, 0, 0).BorderColor(k.cardBorder).Shrink(0)
			})
			if card.Shortcut(0, ui.KeyEscape) {
				a.later(c, a.closePeek)
			}
		})
	})
}
