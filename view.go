package main

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

const (
	gap       = 8  // between panes, and around them
	titleH    = 44 // the title bar
	cardR     = 12 // the panes' corners
	headerH   = 33 // the panes' headers
	activeFor = 1500 * time.Millisecond
)

func (a *App) view(c *ui.Context) {
	a.services = c.Services()
	a.runPosted()
	a.postMu.Lock()
	toasts := a.toastQueue
	a.toastQueue = nil
	a.postMu.Unlock()
	for _, t := range toasts {
		c.AddToast(t)
	}
	k := a.colors(c)
	focused := a.win == nil || a.win.IsFocused()
	a.gainedFocus = focused && !a.focusedWin
	a.focusedWin = focused
	a.frame++
	a.k = k
	if dark := c.Theme().Dark; dark != a.dark {
		a.dark = dark
		a.previewThemeChanged()
	}
	// Over the window's material, the gradient goes translucent, so that
	// the material shows and a tint still tells the host.
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(k.bgBottom)
	}
	ui.Column(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { paintBackground(p, r, k, vibrant) }).Children(func() {
		a.titleBar(c, k)
		ui.Column(c).Grow(1).MinHeight(0).Padding(0, gap, gap, gap).Children(func() {
			if t := a.tab(); t != nil {
				a.tabContent(c, k, t)
			}
		})
		if a.status != "" {
			a.connectionBar(c, k)
		}
	})
	if a.err != "" {
		banner(c, k, a.err, ui.Hex("#ef4444"), func() { a.err, a.errAction, a.errRun = "", "", nil }, a.errAction, a.errRun)
	}
	a.shortcuts(c)
	a.palette(c, k)
	a.connectionLog(c, k)
	a.settingsDialog(c, k)
	a.toasts(c, k)
	if a.saveDue && time.Since(a.lastSave) > time.Second {
		a.save()
	}
	// The window's title, which the taskbar, the Window menu and Mission
	// Control show: the host, the active tab, or both, as this PC wants.
	if a.win != nil {
		var parts []string
		mode := a.eff.Appearance.WindowTitle
		if mode != "tab" {
			parts = append(parts, a.hostName())
		}
		if t := a.tab(); t != nil {
			if mode != "host" {
				name, detail := t.label()
				if f := t.treeFocus(); t.Kind == paneEditor && f != nil && f.edit.path != "" {
					name, detail = f.edit.label()
				}
				parts = append(parts, strings.TrimSpace(name+" "+detail))
			}
		} else if a.status != "" {
			parts = append(parts, "Connecting…")
		}
		if title := strings.Join(parts, " — "); title != a.title {
			a.title = title
			a.win.SetTitle(title)
		}
	}
}

// ownControls tells that the title bar draws the window's controls: on
// Windows, whose own would look like none of the rest of it.
var ownControls = runtime.GOOS == "windows"

// titleBar draws the title bar under the window's controls: the host,
// the tabs, and the buttons of the command palette and a new tab.
func (a *App) titleBar(c *ui.Context, k *colors) {
	bar := c.TitleBar()
	left := bar.Left
	if left == 0 {
		left = 12 // in full screen, or beside no controls
	} else {
		left += 14
	}
	ui.Row(c).Height(titleH).Padding(0, max(bar.Right, 10), 0, left).Gap(14).AlignItems(ui.Center).DragWindow().Children(func() {
		a.hostChip(c, k)
		a.tabStrip(c, k)
		// The title bar between the tabs and the buttons, which drags the
		// window, and a double click on which zooms it.
		ui.Spacer(c).MinWidth(titleFree - 14)
		ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
			if iconButton(c, k, "command", "Command Palette", 30, 17).Tooltip(tip("Command Palette", &cmdPalette)).Clicked() {
				a.openPalette()
			}
			a.connectionButton(c, k)
			if iconButton(c, k, "settings-2", "Settings", 30, 17).Tooltip(tip("Settings", &cmdSettings)).Clicked() {
				a.openSettings()
			}
			if iconButton(c, k, "folder-tree", "New Editor Tab", 30, 17).Tooltip(tip("New Editor Tab", &cmdNewEditorTab)).Clicked() {
				a.newEditorTab("")
			}
			if iconButton(c, k, "plus", "New Tab", 30, 19).Tooltip(tip("New Tab", &cmdNewTab)).Clicked() {
				a.newTab(a.currentDir())
			}
		})
		a.windowControls(c, k)
	})
}

// windowControls draws the buttons that minimize, maximize and close the
// window, when it has none of its own: they are the title bar's other
// buttons, and close's face turns red, as Windows has it.
func (a *App) windowControls(c *ui.Context, k *colors) {
	win := a.win
	if !ownControls || win == nil || win.IsFullScreen() {
		return
	}
	maximized := win.IsMaximized()
	ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Size(1, 16).Margin(0, 6).Background(k.tabSep)
		if iconButton(c, k, "minus", "Minimize", 30, 16).Tooltip("Minimize").Clicked() {
			win.Minimize()
		}
		glyph, label := "square", "Maximize"
		if maximized {
			glyph, label = "copy", "Restore"
		}
		if iconButton(c, k, glyph, label, 30, 13).Tooltip(label).Clicked() {
			if maximized {
				win.Unmaximize()
			} else {
				win.Maximize()
			}
		}
		b := ui.Box(c).Size(30, 30).Center().Radius(30 / 2.6).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Close").Tooltip("Close")
		col := k.iconMuted
		if b.Pressed() {
			b.Background(closeRed.Alpha(0.8))
			col = ui.Hex("#ffffff")
		} else if b.Hovered() {
			b.Background(closeRed)
			col = ui.Hex("#ffffff")
		}
		b.Transition(ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond})
		b.Children(func() {
			ui.Icon(c, icon("x")).Size(17, 17).TextColor(col)
		})
		if b.Clicked() {
			win.Close()
		}
	})
}

// closeRed is the face of the close button under the pointer: Windows 11's.
var closeRed = ui.Hex("#c42b1c")

// tip is the tooltip of a button that runs a command: its label and its
// keys.
func tip(label string, cmd *command) string {
	if cmd.Keys == "" {
		return label
	}
	return label + "  " + cmd.Keys
}

// iconButton is a borderless button of an icon, with a face on hover;
// label names it for screen readers and its tooltip.
func iconButton(c *ui.Context, k *colors, name, label string, size, iconSize float32) ui.Element {
	b := ui.Box(c).Size(size, size).Center().Radius(size / 2.6).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(label)
	if b.Pressed() {
		b.Background(k.pressed)
	} else if b.Hovered() {
		b.Background(k.hover)
	}
	b.Transition(ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond})
	col := k.iconMuted
	if b.Hovered() {
		col = k.text
	}
	b.Children(func() {
		ui.Icon(c, icon(name)).Size(iconSize, iconSize).TextColor(col)
	})
	return b
}

// tabContent lays out the panes of a tab, or the one zoomed; an editor
// tab's file browser on the left, with a divider, and its tree.
func (a *App) tabContent(c *ui.Context, k *colors, t *Tab) {
	if t.Zoom != nil {
		a.paneCard(c, k, t, t.Zoom).Grow(1)
		return
	}
	if t.Kind != paneEditor || t.Browser == nil {
		a.node(c, k, t, t.Root).Grow(1)
		return
	}
	box := ui.Row(c.Key("editor-tab")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch)
	bounds := box.Bounds()
	ratio := t.BrowserRatio
	box.Children(func() {
		a.paneCard(c, k, t, t.Browser).Grow(ratio).Basis(0).MinWidth(browserMin).MinHeight(0)
		div := ui.Box(c.Key("divider")).Role(ui.RoleSplitter).Label("Divider").Width(gap).Cursor(ui.CursorResizeColumn)
		if dx, _, ok := div.Dragged(); ok && bounds.W > gap {
			t.BrowserRatio = min(max(t.BrowserRatio+dx/(bounds.W-gap), 0.1), 0.6)
			a.changed()
		}
		if div.DoubleClicked() {
			t.BrowserRatio = browserWidth
			a.changed()
		}
		if div.Hovered() || div.Dragging() || div.Pressed() {
			div.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X + r.W/2 - 1.5, Y: r.Y + r.H/2 - 18, W: 3, H: 36}, k.iconMuted.Alpha(0.6), 1.5)
			})
		}
		a.node(c, k, t, t.Root).Grow(1 - ratio).Basis(0).MinWidth(0).MinHeight(0)
	})
}

// node lays out a node of the tree of splits.
func (a *App) node(c *ui.Context, k *colors, t *Tab, n *Node) ui.Element {
	if n.Pane != nil {
		return a.paneCard(c, k, t, n.Pane)
	}
	var box ui.Element
	if n.Vertical {
		box = ui.Column(c.Key(n.ID))
	} else {
		box = ui.Row(c.Key(n.ID))
	}
	box.MinWidth(0).MinHeight(0).AlignItems(ui.Stretch)
	bounds := box.Bounds()
	// The tree may change as its panes build, as a split: build the
	// children it has now.
	first, second, ratio := n.A, n.B, n.Ratio
	box.Children(func() {
		a.node(c, k, t, first).Grow(ratio).Basis(0).MinWidth(0).MinHeight(0)
		div := ui.Box(c.Key("divider")).Role(ui.RoleSplitter).Label("Divider")
		if n.Vertical {
			div.Height(gap).Cursor(ui.CursorResizeRow)
		} else {
			div.Width(gap).Cursor(ui.CursorResizeColumn)
		}
		if dx, dy, ok := div.Dragged(); ok {
			total := bounds.W - gap
			d := dx
			if n.Vertical {
				total, d = bounds.H-gap, dy
			}
			if total > 0 {
				n.Ratio = min(max(n.Ratio+d/total, 0.08), 0.92)
				a.changed()
			}
		}
		if div.DoubleClicked() {
			n.Ratio = 0.5
			a.changed()
		}
		if div.Hovered() || div.Dragging() || div.Pressed() {
			div.Draw(func(p *ui.Painter, r ui.Rect) {
				if n.Vertical {
					p.Fill(ui.Rect{X: r.X + r.W/2 - 18, Y: r.Y + r.H/2 - 1.5, W: 36, H: 3}, k.iconMuted.Alpha(0.6), 1.5)
				} else {
					p.Fill(ui.Rect{X: r.X + r.W/2 - 1.5, Y: r.Y + r.H/2 - 18, W: 3, H: 36}, k.iconMuted.Alpha(0.6), 1.5)
				}
			})
		}
		a.node(c, k, t, second).Grow(1 - ratio).Basis(0).MinWidth(0).MinHeight(0)
	})
	return box
}

// paneCard draws a pane: a card with a header over its terminal.
func (a *App) paneCard(c *ui.Context, k *colors, t *Tab, p *Pane) ui.Element {
	focused := t.Focus == p
	card := ui.Column(c.Key(p.ID)).Radius(cardR).Clip().MinWidth(0).MinHeight(0)
	bg, border, shadow := k.card, k.cardBorder, k.shadow
	if focused {
		bg, border, shadow = k.cardFocused, k.cardBorderFocused, k.shadowFocused
	}
	card.Background(bg).Border(1, border).
		Shadow(0, 1, 2, 0, shadow).
		Shadow(0, 6, 22, -2, shadow)
	card.Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond})
	hovered := card.Hovered()
	card.Children(func() {
		switch p.Kind {
		case paneBrowser:
			a.browserHeader(c, k, t, p, focused, hovered)
		case paneEditor:
			a.editorHeader(c, k, t, p, focused, hovered)
		default:
			a.paneHeader(c, k, t, p, focused, hovered)
		}
		body := ui.Box(c).Grow(1).MinHeight(0).Padding(0, 5, 6, 5)
		body.Children(func() {
			switch p.Kind {
			case paneBrowser:
				a.browserBody(c, k, t, p)
				return
			case paneEditor:
				a.editorBody(c, k, t, p)
				return
			}
			if p.term == nil {
				return
			}
			// The terminal's view and the surfaces its programs draw
			// over it (tsp_view.go) share a box, so that a surface's
			// place is relative to the view, and cut to it.
			host := ui.Box(c).Grow(1).MinHeight(0).Clip()
			host.Children(func() {
				tv := terminal.View(c, p.term).Fill()
				if a.focusReq == p && a.tab() == t {
					tv.Focus()
					a.focusReq = nil
				}
				if tv.Focused() && t.Focus != p {
					t.setFocus(p)
					a.changed()
				}
				if tv.Focused() {
					p.attention = false
					if a.focusedWin {
						p.statusSeen = statusKey(p.info.Status)
					}
				}
				// A pane that starts showing in the window in use, or as
				// the window gets the focus, gives its session its size
				// again.
				if a.focusedWin && (a.gainedFocus || p.drawn+1 != a.frame) {
					a.claimSize(p)
				}
				p.drawn = a.frame
				live := 0
				for i := range p.surfaces {
					if !p.surfaces[i].Closed {
						live++
					}
				}
				// A surface draws over the terminal while the program
				// shows it. As one appears, the terminal takes the
				// focus, so that keys reach the program; after that the
				// focus goes where the user clicks, as selecting text in
				// a surface takes it (tsp_view.go).
				if live > p.surfacesLive && t.Focus == p && a.tab() == t {
					tv.Focus()
				}
				p.surfacesLive = live
				if len(p.surfaces) == 0 {
					return
				}
				a.surfaces(c, k, t, p)
			})
		})
	})
	p.bounds = card.Bounds()
	if card.Pressed() {
		a.claimSize(p)
		if t.Focus != p {
			t.setFocus(p)
			a.focusReq = p
		}
	}
	return card
}

// paneHeader draws a pane's program, title and directory, its activity,
// and its buttons.
func (a *App) paneHeader(c *ui.Context, k *colors, t *Tab, p *Pane, focused, hovered bool) {
	name, detail := p.label()
	prog := programOf(p.info.Program)
	if p.info.Idle || p.info.Program == "" {
		prog = programOf(p.info.Shell)
	}
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
		ui.Icon(c, icon(prog.Glyph)).Size(14.5, 14.5).TextColor(k.text)
		ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).ClipX().Children(func() {
			ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidthPercent(80)
			if detail != "" {
				ui.Text(c, detail).FontSize(12.5).FontWeight(500).TextColor(k.text.Alpha(0.86)).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			}
			a.activityBadge(c, k, p)
		})
		show := focused || hovered
		ui.Row(c).Gap(1).AlignItems(ui.Center).Opacity(map[bool]float32{true: 1, false: 0}[show]).Children(func() {
			if !show {
				return
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
					if a.confirmClose([]*Pane{p}) {
						a.closePane(p)
					}
				})
			}
		})
	})
}

// status is what a pane is doing.
type status int

const (
	statusIdle      status = iota // a shell at its prompt, or a program that says it is idle
	statusRunning                 // a program printing
	statusQuiet                   // a program running quietly
	statusAttention               // done or ringing out of sight
	// What a program says (OSC 7501): it works, has finished or failed
	// unseen, or waits on the user.
	statusWorking
	statusDone
	statusError
	statusBlocked
)

// statusRank orders what a tab shows of its panes: the one that most
// needs the user.
var statusRank = map[status]int{
	statusIdle: 0, statusQuiet: 1, statusRunning: 2, statusWorking: 3,
	statusDone: 4, statusAttention: 5, statusError: 6, statusBlocked: 7,
}

func (a *App) statusOf(c *ui.Context, p *Pane) status {
	st := p.info.Status
	if st != nil && !p.info.Exited {
		unseen := statusKey(st) != p.statusSeen
		switch {
		case st.State == "blocked":
			return statusBlocked
		case st.State == "error" && unseen:
			return statusError
		case st.State == "done" && unseen:
			return statusDone
		case p.attention:
			return statusAttention
		case st.State == "working":
			return statusWorking
		}
		// A program that says it is at rest is, whatever it prints.
		return statusIdle
	}
	if p.attention {
		return statusAttention
	}
	if p.info.Idle || p.info.Program == "" || programOf(p.info.Program).Shell {
		return statusIdle
	}
	last := time.Unix(0, p.lastData.Load())
	if since := c.Now().Sub(last); since < activeFor {
		c.After(activeFor - since)
		return statusRunning
	}
	return statusQuiet
}

// activityBadge shows what a pane does: a pulse while its program
// prints or says it works, a dot when it asks for attention, waits on
// the user or failed, a check when it finished.
func (a *App) activityBadge(c *ui.Context, k *colors, p *Pane) {
	if e, ok := statusMark(c, k, a.statusOf(c, p), p.info.Status, 6); ok {
		e.Margin(0, 0, 0, 2)
	}
}

// statusMark draws the mark of a status, of a size, and reports whether
// there is one; st is what the program said, for the tooltip.
func statusMark(c *ui.Context, k *colors, s status, st *rex.ProgramStatus, size float32) (ui.Element, bool) {
	tip := "Needs attention"
	if st != nil && s != statusAttention && s != statusRunning {
		tip = statusTip(st)
	}
	switch s {
	case statusRunning:
		return activityDot(c, k.busy, size), true
	case statusWorking:
		return activityDot(c, k.busy, size).Tooltip(tip), true
	case statusAttention, statusBlocked:
		return ui.Box(c).Size(size+1, size+1).Radius(4).Background(k.attention).Tooltip(tip), true
	case statusError:
		return ui.Box(c).Size(size+1, size+1).Radius(4).Background(ui.Hex("#ef4444")).Tooltip(tip), true
	case statusDone:
		return ui.Icon(c, icon("check")).Size(size+6, size+6).TextColor(k.busy).Tooltip(tip), true
	}
	return ui.Element{}, false
}

// activityDot is the dot of a program printing: a solid dot in a soft
// halo. It does not animate, as drawing frames all along would cost more
// than it tells.
func activityDot(c *ui.Context, col ui.Color, size float32) ui.Element {
	e := ui.Box(c).Size(size+6, size+6).Margin(0, 0, 0, 1).Tooltip("Printing")
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		halo := size/2 + 2.5
		p.Fill(ui.Rect{X: cx - halo, Y: cy - halo, W: 2 * halo, H: 2 * halo}, col.Alpha(0.22), halo)
		p.Fill(ui.Rect{X: cx - size/2, Y: cy - size/2, W: size, H: size}, col, size/2)
	})
	return e
}

// banner shows a message at the bottom of the window, as what went wrong
// with the server, beside a dot of its color; dismiss, when set, makes it
// closable.
func banner(c *ui.Context, k *colors, text string, dot ui.Color, dismiss func(), action string, run func()) {
	ui.Overlay(c, func() {
		ui.Row(c).Absolute().Bottom(18).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
			ui.Row(c).Gap(10).Padding(8, 10, 8, 14).Radius(12).Background(k.panel).Border(1, k.panelBorder).
				Shadow(0, 8, 24, 0, k.shadowFocused).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(dot)
				ui.Text(c, text).FontSize(12.5).TextColor(k.text).MaxWidth(520)
				if action != "" && textButton(c, k, action).Clicked() {
					a := run
					dismiss()
					a()
				}
				if dismiss != nil && iconButton(c, k, "x", "Dismiss", 22, 14).Clicked() {
					dismiss()
				}
			})
		})
	})
}

// toasts draws the window's toasts, as what GoRex does with a paste, in
// the banner's style: at the bottom, a dot of the kind's color, the title
// and what more there is to say.
func (a *App) toasts(c *ui.Context, k *colors) {
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(0, 0, 18, 0).AlignItems(ui.Center).Justify(ui.End).Gap(8)
		for _, t := range toasts {
			toast := ui.ToastBase(c, t)
			toast.Root.Row().Gap(10).Padding(8, 14).Radius(12).Background(k.panel).Border(1, k.panelBorder).
				Shadow(0, 8, 24, 0, k.shadowFocused).AlignItems(ui.Center).
				Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{}})
			toast.Root.Children(func() {
				dot := k.busy
				switch t.Type {
				case "error":
					dot = ui.Hex("#ef4444")
				case "working":
					dot = k.attention
				}
				ui.Box(c).Size(8, 8).Radius(4).Background(dot).Shrink(0)
				ui.Text(c, t.Title).FontSize(12.5).TextColor(k.text).MaxWidth(420)
				if t.Description != "" {
					ui.Text(c, t.Description).FontSize(12.5).TextColor(k.textFaint).MaxWidth(300)
				}
				if t.Type == "working" {
					ui.Spinner(c).Size(14, 14)
				}
			})
		}
	})
}

// pasteStatus tells how a paste of an image to a program goes, in one
// toast per pane, which changes as it goes and leaves once it is sent.
func (a *App) pasteStatus(p *Pane, status terminal.PasteStatus, sent, total int) {
	id := fmt.Sprintf("paste/%d", p.ID)
	host := a.hostName()
	size := func(n int) string {
		switch {
		case n >= 1<<20:
			return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
		case n >= 1<<10:
			return fmt.Sprintf("%d KB", n>>10)
		}
		return fmt.Sprintf("%d bytes", n)
	}
	switch status {
	case terminal.PasteOffered:
		a.toast(ui.Toast{ID: id, Title: "Pasting image…", Description: "to " + host, Type: "working", Timeout: -1})
	case terminal.PasteSending:
		a.toast(ui.Toast{ID: id, Title: "Sending image", Description: size(sent) + " of " + size(total) + " to " + host, Type: "working", Timeout: -1})
	case terminal.PasteSent:
		a.toast(ui.Toast{ID: id, Title: "Image pasted", Description: size(total) + " to " + host, Timeout: 2 * time.Second})
	case terminal.PasteNotTaken:
		a.toast(ui.Toast{ID: id, Title: "The program didn't take the image", Type: "error"})
	case terminal.PasteNoText:
		a.toast(ui.Toast{ID: id, Title: "This program doesn't take images", Type: "error"})
	}
}

// connectionBar shows how connecting to the server goes, under the panes,
// while the window is not connected: with a way to try again once it gave
// up, and to the Connection Log.
func (a *App) connectionBar(c *ui.Context, k *colors) {
	dot := k.attention
	if a.connDown {
		dot = ui.Hex("#ef4444")
	}
	ui.Row(c).Height(30).Padding(0, 10, 0, 14).Gap(10).AlignItems(ui.Center).Background(k.panel).
		BorderWidth(1, 0, 0, 0).BorderColor(k.panelBorder).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(dot).Shrink(0)
		ui.Text(c, a.status).FontSize(12.5).TextColor(k.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if a.connDown {
			if textButton(c, k, "Retry").Clicked() {
				a.later(c, func() { a.switchTo(a.bind, true) })
			}
		}
		if textButton(c, k, "Log").Clicked() {
			a.logOpen = true
		}
	})
}

// textButton is a small button of a word, as in the connection bar.
func textButton(c *ui.Context, k *colors, label string) ui.Element {
	b := ui.Box(c).Height(22).Padding(0, 9).Radius(6).Center().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(label).Shrink(0)
	if b.Pressed() {
		b.Background(k.pressed)
	} else if b.Hovered() {
		b.Background(k.hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(k.text)
	})
	return b
}

// connectionLog shows what connecting printed: the attempts, what ssh
// said, and when the connection was lost. It follows the end of the log
// unless scrolled up to read.
func (a *App) connectionLog(c *ui.Context, k *colors) {
	if !a.logOpen {
		a.logShown = 0
		return
	}
	lines := a.connLogText()
	if len(lines) != a.logShown {
		// Opening it, or new lines while at the end: show the end.
		atEnd := a.logShown == 0 || a.logScroll.Y >= a.logScroll.MaxY-1
		if atEnd {
			a.logScroll.Y = math.MaxFloat32
		}
		a.logShown = len(lines)
	}
	ui.DialogBase(c, &a.logOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(720).Height(440).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Clip()
		ui.Row(c).Padding(12, 16).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder).Shrink(0).Children(func() {
			ui.Text(c, "Connection Log").FontSize(14).FontWeight(700).TextColor(k.text).Grow(1)
			dot, state := k.busy, "connected"
			switch {
			case a.connDown:
				dot, state = ui.Hex("#ef4444"), "not connected"
			case a.client == nil, a.status != "":
				dot, state = k.attention, "connecting…"
			}
			ui.Box(c).Size(7, 7).Radius(4).Background(dot)
			ui.Text(c, a.bind.label()+" · "+state).FontSize(12.5).TextColor(k.textFaint)
			if textButton(c, k, "Clear").Clicked() {
				a.connLogMu.Lock()
				a.connLog = nil
				a.connLogMu.Unlock()
			}
			if iconButton(c, k, "x", "Close", 24, 14).Clicked() {
				a.logOpen = false
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(10, 16).TrackScroll(&a.logScroll).Children(func() {
			if len(lines) == 0 {
				ui.Text(c, "Nothing yet.").FontSize(12.5).TextColor(k.textFaint)
			}
			for i, l := range lines {
				ui.Text(c.Key(i), l).Font(termFont.Family).FontSize(12).TextColor(k.text).Selectable().LineHeight(1.5)
			}
		})
	})
}

// connectionButton shows the connection's state as a dot on a link glyph,
// and opens the Connection Log.
func (a *App) connectionButton(c *ui.Context, k *colors) {
	host := a.hostName()
	dot, label := k.busy, "Connected to "+host
	switch {
	case a.connDown:
		dot, label = ui.Hex("#ef4444"), "Can't reach "+host
	case a.client == nil || a.status != "":
		dot, label = k.attention, "Connecting to "+host+"…"
	}
	b := iconButton(c, k, "cable", "Connection Log", 30, 17).Tooltip(label + " · Connection Log")
	b.Children(func() {
		ui.Box(c).Absolute().Right(5).Top(5).Size(7, 7).Radius(4).Background(dot).Border(1, k.panel)
	})
	if b.Clicked() {
		a.logOpen = true
	}
}

// toast shows a toast with the next frame, from any goroutine.
func (a *App) toast(t ui.Toast) {
	a.postMu.Lock()
	a.toastQueue = append(a.toastQueue, t)
	a.postMu.Unlock()
	a.services.Invalidate()
}
