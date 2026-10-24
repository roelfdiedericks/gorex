package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"gorex/internal/remote"
	"gorex/internal/rex"
)

// Tab is a tab of the window: panes in a tree of splits. An editor tab
// has a file browser beside the tree, whose panes are editors.
type Tab struct {
	ID   int
	Kind paneKind // paneTerminal, or paneEditor for a tab of editors
	Name string   // the name the user gave it, "" to follow its panes
	Root *Node
	// Browser is the file browser of an editor tab, left of the tree, and
	// BrowserRatio its share of the tab's width.
	Browser      *Pane
	BrowserRatio float32
	// Focus is the pane with the focus, and Zoom the pane that fills the
	// tab, when one does.
	Focus *Pane
	Zoom  *Pane
	// recent lists the panes by when they had the focus last, the latest
	// last: closing a pane gives the focus back to the one before.
	recent []*Pane
}

// Node is a pane, or a split of two nodes.
type Node struct {
	ID     int
	Pane   *Pane
	Parent *Node
	// Vertical stacks A above B; otherwise A is left of B. Ratio is A's
	// share of the room.
	Vertical bool
	Ratio    float32
	A, B     *Node
}

// paneKind is what a pane holds.
type paneKind int

const (
	paneTerminal paneKind = iota // a terminal on a session of the server
	paneBrowser                  // the file browser of an editor tab
	paneEditor                   // a file in the code editor
)

// Pane is a terminal attached to a session of the server, or a file
// browser or an editor of an editor tab: Kind says which, and only that
// kind's fields are set.
type Pane struct {
	ID     int
	Kind   paneKind
	SID    string
	Tab    *Tab
	Node   *Node
	term   *terminal.Terminal
	stream *rex.Stream
	// surfaces are the session's surfaces of the Tern Surface Protocol,
	// as the server last sent them, drawn over the terminal
	// (tsp_view.go); surfaceRows are the rows the window last told the
	// server each live inline one needs.
	surfaces    []rex.Surface
	surfaceRows map[string]int
	// surfacesLive is how many of them were open at the last frame: one
	// more gives the terminal the focus.
	surfacesLive int
	// files is a browser pane's tree, and edit an editor pane's file.
	files *browserState
	edit  *editorState

	// info is what the server last said of the session.
	info rex.SessionInfo
	// title is the last title the program set.
	title string
	// lastData is when the session last printed, in Unix nanoseconds.
	lastData atomic.Int64
	// attention marks a pane that rang the bell or whose program finished
	// while it was out of sight; seen clears it.
	attention bool
	// statusSeen is the program status the user last saw in the pane,
	// which a done or failed one no longer marks, and noticed when its
	// program's status last made a notification.
	statusSeen string
	noticed    time.Time
	// drawn is the frame the pane was last drawn in.
	drawn uint64
	// bounds is where the pane's card was in the last frame.
	bounds ui.Rect
	closed bool
	// restored tells that the session was made anew for a pane of a
	// saved layout whose session was gone.
	restored bool
	startDir string
}

// hostHome and hostGOOS describe the machine the window's server runs on.
var hostHome, _ = os.UserHomeDir()
var hostGOOS = runtime.GOOS

// setHost takes the machine the window's server runs on from its hello.
// A remote server too old to tell its OS runs on Linux.
func setHost(h rex.Hello, remote bool) {
	hostGOOS = h.Host.GOOS
	if hostGOOS == "" {
		hostGOOS = runtime.GOOS
		if remote {
			hostGOOS = "linux"
		}
	}
	hostHome = h.Host.Home
	if hostHome == "" && !remote {
		hostHome, _ = os.UserHomeDir()
	}
}

// App is the state of GoRex's window.
type App struct {
	win    *mygo.Window
	client *rex.Client
	hello  rex.Hello
	// err shows in a banner, with errAction a button running errRun when
	// there is something to do about it.
	err       string
	errAction string
	errRun    func()
	// restartNext has the next connection restart the host's server.
	restartNext bool
	// upgrades counts the window's upgrades of its server, which stop when
	// another GoRex keeps upgrading it back.
	upgrades upgradeGuard

	// bind is the server the window shows the sessions of, and link the
	// ssh that reaches it when it is remote. status tells how connecting
	// to it goes, "" once connected; gen counts the attempts, the latest
	// one the only one to go on.
	bind     binding
	link     *remote.Link
	status   string
	connDown bool // gave up connecting: status tells why
	gen      atomic.Int64
	stateKey string
	// connLog is what connecting printed, for the Connection Log.
	connLogMu sync.Mutex
	connLog   []string
	logOpen   bool
	logAuto   bool // the log opened by itself, for connecting: it closes by itself
	// logScroll is how far the log is scrolled, and logShown how many
	// lines it showed last, to follow new ones while at the end.
	logScroll ui.ScrollState
	logShown  int
	// remotes are the known remote servers, for the palette.
	remotes []savedRemote
	// defaults are this PC's settings, host the current host's overrides,
	// and eff what applies: see Settings. termOpts is what the panes'
	// terminals were made with, to make them anew when it changes.
	defaults, host, eff Settings
	// defEff is what the Defaults give on their own, over the built-in
	// values: what the Defaults pages show.
	defEff   Settings
	termOpts string
	// material is the window material applied last.
	material    string
	settingsDue bool
	settingsErr string
	settings    settingsUI

	tabs   []*Tab
	active int
	nextID int
	// keptEdits are editor buffers with unsaved changes kept across a
	// reconnect, by path, until a pane takes them over.
	keptEdits map[string]*editorState
	// dark is whether the window's theme was dark in the last frame, k
	// the palette it drew with, for what paints outside a frame.
	dark bool
	k    *colors

	// focusReq asks the view to give the keyboard focus to a pane's
	// terminal.
	focusReq *Pane

	// The command palette, the host's popover and the tab being renamed.
	paletteOpen  bool
	paletteQuery string
	paletteSel   int
	hostOpen     bool
	// hostQuery is the ssh destination typed in the host's popover, and
	// hostErr why connecting to it failed.
	hostQuery    string
	hostErr      string
	renaming     *Tab
	renameText   string
	renameFocus  bool
	saveDue      bool
	lastSave     time.Time
	quitting     bool
	focusedWin   bool
	lastSnapshot string
	title        string

	// frame counts the window's frames, and gainedFocus tells that the
	// window got the focus in this one: for the panes that start showing
	// in the window in use (claimSize).
	frame       uint64
	gainedFocus bool

	// posted are changes to make in the next frame, from terminals of a
	// view without a window, as in tests.
	postMu sync.Mutex
	posted []func()
	// toasts are queued from other goroutines, for the next frame to
	// show (AddToast needs the frame's context).
	toastQueue []ui.Toast
	// services keeps redraw requests available to menu callbacks.
	services ui.Services
}

func (a *App) post(fn func()) {
	a.postMu.Lock()
	a.posted = append(a.posted, fn)
	a.postMu.Unlock()
}

// later changes the tree of tabs and panes once the frame building now
// is built, as closing what it is building would pull it from under it.
func (a *App) later(c *ui.Context, fn func()) {
	a.post(fn)
	c.Invalidate()
}

// runPosted makes the changes posted since the last frame.
func (a *App) runPosted() {
	a.postMu.Lock()
	fns := a.posted
	a.posted = nil
	a.postMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (a *App) id() int {
	a.nextID++
	return a.nextID
}

func (a *App) tab() *Tab {
	if a.active >= 0 && a.active < len(a.tabs) {
		return a.tabs[a.active]
	}
	return nil
}

// panes returns the panes of a node in order, left to right and top to
// bottom.
func (n *Node) panes() []*Pane {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		return []*Pane{n.Pane}
	}
	return append(n.A.panes(), n.B.panes()...)
}

func (t *Tab) panes() []*Pane { return t.Root.panes() }

// setFocus gives a pane the focus of its tab.
func (t *Tab) setFocus(p *Pane) {
	if p == nil {
		return
	}
	t.Focus = p
	p.attention = false
	t.recent = slices.DeleteFunc(t.recent, func(q *Pane) bool { return q == p })
	t.recent = append(t.recent, p)
}

// newPane makes a pane with a new session in dir; one with no session, and
// a message, when the server can't start it.
func (a *App) newPane(dir string, cols, rows int) *Pane {
	if cols <= 0 {
		cols, rows = 80, 24
	}
	client := a.client
	var info rex.SessionInfo
	err := errors.New("not connected to the server")
	o := shellOptions(&a.eff)
	o.Dir, o.Cols, o.Rows = dir, cols, rows
	if client != nil {
		info, err = client.Create(o)
	}
	if err != nil && dir != hostHome && client != nil && !closed(client.Closed()) {
		// The server answered, so the directory may be gone: the home
		// directory then.
		log.Printf("creating a session in %s: %v", dir, err)
		dir = hostHome
		o.Dir = dir
		info, err = client.Create(o)
	}
	if err != nil {
		log.Printf("creating a session: %v", err)
		a.err = err.Error()
		info = rex.SessionInfo{ID: "", Exited: true}
	}
	p := &Pane{ID: a.id(), SID: info.ID, info: info, startDir: dir}
	if p.info.Dir == "" {
		p.info.Dir = dir
	}
	a.attach(p, cols, rows)
	return p
}

// closed reports whether a channel is closed, without waiting.
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// attach makes the pane's terminal, attached to its session.
func (a *App) attach(p *Pane, cols, rows int) {
	light, dark := termThemes(&a.eff)
	if p.SID == "" {
		term, err := terminal.New(terminal.Options{Conn: nopConn{}, Transparent: true, Font: termFont, Theme: light, DarkTheme: dark})
		if err == nil {
			term.Feed([]byte("\x1b[31mCould not start a session: " + a.err + "\x1b[0m\r\n"))
			p.term = term
		}
		return
	}
	client := a.client
	p.stream = client.Stream(p.SID, cols, rows)
	p.stream.OnData = func(int) { p.lastData.Store(time.Now().UnixNano()) }
	// The terminal's callbacks change the state on the main thread, in
	// the window that made the pane, as long as it is open.
	win := a.win
	update := func(fn func()) {
		if win != nil {
			win.Update(fn)
		} else {
			a.post(fn)
		}
	}
	// stream tells the terminal made here apart from one made later for
	// the same pane, as when the settings remake it: its exit is not the
	// session's.
	stream := p.stream
	term, err := terminal.New(terminal.Options{
		Conn: p.stream,
		// The server's terminal lays out the program's output; this one
		// shows its frames, at the size the server says.
		ResizeByConn: true,
		// The anchors of programs' surfaces, which the server places and
		// its Mirror carries here (tsp_view.go).
		AppImages:    struct{ First, Last uint32 }{rex.TSPImageFirst, rex.TSPImageLast},
		Font:         termFont,
		Theme:        light,
		DarkTheme:    dark,
		Transparent:  is(a.eff.Appearance.Transparent),
		Cursor:       cursorOf(&a.eff),
		NoBlink:      !is(a.eff.Terminal.Blink),
		Scrollback:   a.eff.Terminal.Scrollback * 256,
		OptionAsAlt:  is(a.eff.Terminal.OptionAsAlt),
		CopyOnSelect: copyOnSelectOf(&a.eff),
		Keybinds:     keybindsOf(&a.eff),
		RightClick:   rightClickOf(&a.eff),
		// Images of the Kitty graphics protocol, in the memory the
		// settings give them.
		ImageLimit: imageLimit(&a.eff),
		OnZoom: func(z terminal.Zoom) bool {
			update(func() {
				if z.Reset {
					a.defaultFontSize()
				} else {
					a.stepFontSize(z.Steps)
				}
			})
			return true
		},
		OnKey: func(ui.Modifiers, ui.Key) bool {
			// Typing in a pane makes this window the one in use.
			a.claimSize(p)
			return false
		},
		OnPasteStatus: func(pr terminal.PasteProgress) {
			update(func() { a.pasteStatus(p, pr.Status, int(pr.Sent), int(pr.Total)) })
		},
		OnTitle: func(title string) {
			update(func() { p.title = title })
		},
		OnClear: func(terminal.Clear) bool {
			// The server's terminal clears, and its next frame shows it.
			go stream.Clear()
			return true
		},
		OnExit: func(int) {
			// The stream also ends when the connection to the server
			// is lost: then the pane stays, for when it comes back.
			go func() {
				if !serverAnswers(client) {
					return
				}
				update(func() {
					if !p.closed && !a.quitting && p.stream == stream {
						a.closePane(p)
					}
				})
			}()
		},
	})
	if err != nil {
		log.Printf("terminal: %v", err)
		a.err = err.Error()
		return
	}
	// Until the server tells the session's size, the terminal has the
	// size the session had, which the server's next frames are drawn at.
	term.Resize(cols, rows)
	stream.OnSize(term.Resize)
	stream.OnSurfaces(func(sfs []rex.Surface) {
		update(func() {
			if p.stream != stream {
				return
			}
			p.surfaces = sfs
			// The rows told for a surface stand until the server's
			// anchor has them, or the surface is gone.
			for id, rows := range p.surfaceRows {
				live := false
				for i := range sfs {
					if sfs[i].ID == id && !sfs[i].Closed && sfs[i].Rows != rows {
						live = true
					}
				}
				if !live {
					delete(p.surfaceRows, id)
				}
			}
			a.changed()
		})
	})
	p.term = term
}

// serverAnswers reports whether the server still answers: a session's
// stream that ends while it does ended with its session, not with the
// connection.
func serverAnswers(client *rex.Client) bool {
	_, err := client.Hello()
	return err == nil
}

// nopConn is the Conn of a terminal that only shows what the app feeds it.
type nopConn struct{}

func (nopConn) Read([]byte) (int, error)    { select {} }
func (nopConn) Write(p []byte) (int, error) { return len(p), nil }
func (nopConn) Close() error                { return nil }

// ring takes a session's bell: the pane asks for attention when it is
// out of sight, and the system's sound plays, as the settings say.
func (a *App) ring(p *Pane) {
	bell := a.eff.Terminal.Bell
	if bell == "none" {
		return
	}
	if t := a.tab(); (t == nil || t.Focus != p || p.Tab != t || !a.focusedWin) && is(a.eff.General.Attention) {
		p.attention = true
	}
	if bell == "sound" {
		mygo.Shell.Beep()
	}
}

// newTab opens a tab with a shell in dir, after the active tab; none while
// the window connects to its server.
func (a *App) newTab(dir string) *Tab {
	if a.client == nil {
		return nil
	}
	t := &Tab{ID: a.id()}
	p := a.newPane(dir, 0, 0)
	t.Root = &Node{ID: a.id(), Pane: p}
	p.Tab, p.Node = t, t.Root
	t.setFocus(p)
	at := min(a.active+1, len(a.tabs))
	a.tabs = slices.Insert(a.tabs, at, t)
	a.active = at
	a.focusReq = p
	a.changed()
	return t
}

// currentDir returns the directory new panes open in: the focused pane's,
// or the home directory, as the settings say.
func (a *App) currentDir() string {
	if a.eff.Shell.StartIn == "home" {
		return hostHome
	}
	if t := a.tab(); t != nil && t.Focus != nil {
		if d := t.Focus.info.Dir; d != "" {
			return d
		}
	}
	return hostHome
}

// split splits the focused pane, the new pane right of it or below it: a
// shell in a terminal tab, an empty editor in an editor tab. From the
// file browser, the editor pane focused last splits.
func (a *App) split(vertical bool) *Pane {
	t := a.tab()
	if t == nil || t.Focus == nil || a.client == nil {
		return nil
	}
	old := t.treeFocus()
	if old == nil {
		return nil
	}
	return a.splitPane(old, vertical)
}

// splitPane splits a pane of the tree.
func (a *App) splitPane(old *Pane, vertical bool) *Pane {
	t := old.Tab
	t.Zoom = nil
	n := old.Node
	var p *Pane
	if t.Kind == paneEditor {
		p = a.newEditorPane("")
	} else {
		cols, rows := 80, 24
		if old.term != nil {
			cols, rows = old.term.Size()
			if vertical {
				rows = max(rows/2, 2)
			} else {
				cols = max(cols/2, 10)
			}
		}
		p = a.newPane(a.currentDir(), cols, rows)
	}
	p.Tab = t
	left := &Node{ID: a.id(), Pane: old, Parent: n}
	right := &Node{ID: a.id(), Pane: p, Parent: n}
	old.Node, p.Node = left, right
	n.Pane, n.A, n.B, n.Vertical, n.Ratio = nil, left, right, vertical, 0.5
	t.setFocus(p)
	a.focusReq = p
	a.changed()
	return p
}

// treeFocus is the focused pane of the tree: the focused pane, or, when
// the file browser has the focus, the pane of the tree focused last.
func (t *Tab) treeFocus() *Pane {
	if t.Focus != nil && t.Focus.Kind != paneBrowser {
		return t.Focus
	}
	for i := len(t.recent) - 1; i >= 0; i-- {
		if p := t.recent[i]; p.Kind != paneBrowser && !p.closed {
			return p
		}
	}
	if ps := t.panes(); len(ps) > 0 {
		return ps[0]
	}
	return nil
}

// closePane closes a pane and ends its session; a file browser is its
// tab's and does not close.
func (a *App) closePane(p *Pane) {
	if p.closed || p.Kind == paneBrowser {
		return
	}
	p.closed = true
	if p.term != nil {
		p.term.Close()
	}
	if p.Kind == paneEditor {
		a.dropPreview(p)
	}
	if sid := p.SID; sid != "" {
		c := a.client
		go c.Kill(sid)
	}
	t := p.Tab
	if t.Zoom == p {
		t.Zoom = nil
	}
	t.recent = slices.DeleteFunc(t.recent, func(q *Pane) bool { return q == p })
	n := p.Node
	if n.Parent == nil {
		a.removeTab(t)
		return
	}
	parent := n.Parent
	other := parent.A
	if other == n {
		other = parent.B
	}
	// The other side takes the parent's place.
	parent.Pane, parent.A, parent.B, parent.Vertical, parent.Ratio = other.Pane, other.A, other.B, other.Vertical, other.Ratio
	if parent.Pane != nil {
		parent.Pane.Node = parent
	} else {
		parent.A.Parent, parent.B.Parent = parent, parent
	}
	if t.Focus == p {
		next := (*Pane)(nil)
		if k := len(t.recent); k > 0 {
			next = t.recent[k-1]
		} else if ps := t.panes(); len(ps) > 0 {
			next = ps[0]
		}
		t.Focus = nil
		t.setFocus(next)
	}
	if a.tab() == t {
		a.focusReq = t.Focus
	}
	a.changed()
}

func (a *App) removeTab(t *Tab) {
	i := slices.Index(a.tabs, t)
	if i < 0 {
		return
	}
	a.tabs = slices.Delete(a.tabs, i, i+1)
	if a.active > i || a.active >= len(a.tabs) {
		a.active = max(a.active-1, 0)
	}
	if a.renaming == t {
		a.renaming = nil
	}
	if len(a.tabs) == 0 {
		a.changed()
		a.save()
		if a.win != nil {
			a.win.Close()
		}
		return
	}
	if nt := a.tab(); nt != nil {
		a.focusReq = nt.Focus
	}
	a.changed()
}

// closeTab closes a tab and ends the sessions of its panes.
func (a *App) closeTab(t *Tab) {
	if t.Browser != nil {
		a.unwatchAll(t.Browser)
	}
	for _, p := range t.allPanes() {
		p.closed = true
		if p.term != nil {
			p.term.Close()
		}
		if p.Kind == paneEditor {
			a.dropPreview(p)
		}
		if p.SID != "" {
			go a.client.Kill(p.SID)
		}
	}
	a.removeTab(t)
}

// allPanes is the panes of the tree and the file browser, when there is
// one.
func (t *Tab) allPanes() []*Pane {
	ps := t.panes()
	if t.Browser != nil {
		ps = append([]*Pane{t.Browser}, ps...)
	}
	return ps
}

func (a *App) selectTab(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.active = i
	t := a.tabs[i]
	if t.Focus != nil {
		t.Focus.attention = false
	}
	a.focusReq = t.Focus
	a.changed()
}

// moveFocus gives the focus to the pane next to the focused one, toward
// dx, dy.
func (a *App) moveFocus(dx, dy int) {
	t := a.tab()
	if t == nil || t.Focus == nil || t.Zoom != nil {
		return
	}
	from := t.Focus.bounds
	cx, cy := from.X+from.W/2, from.Y+from.H/2
	var best *Pane
	bestScore := float32(1e9)
	for _, p := range t.allPanes() {
		if p == t.Focus {
			continue
		}
		r := p.bounds
		var dist, overlap float32
		switch {
		case dx > 0:
			dist = r.X - (from.X + from.W)
			overlap = min(r.Y+r.H, from.Y+from.H) - max(r.Y, from.Y)
		case dx < 0:
			dist = from.X - (r.X + r.W)
			overlap = min(r.Y+r.H, from.Y+from.H) - max(r.Y, from.Y)
		case dy > 0:
			dist = r.Y - (from.Y + from.H)
			overlap = min(r.X+r.W, from.X+from.W) - max(r.X, from.X)
		default:
			dist = from.Y - (r.Y + r.H)
			overlap = min(r.X+r.W, from.X+from.W) - max(r.X, from.X)
		}
		if dist < -4 || overlap <= 0 {
			continue
		}
		// The nearest, then the one most in line with the focused pane's
		// center.
		px, py := r.X+r.W/2, r.Y+r.H/2
		off := px - cx
		if dx != 0 {
			off = py - cy
		}
		score := dist*4 + abs(off)
		if score < bestScore {
			best, bestScore = p, score
		}
	}
	if best != nil {
		t.setFocus(best)
		a.focusReq = best
	}
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// resizeFocused moves the nearest divider of the focused pane on the
// axis of dx, dy.
func (a *App) resizeFocused(dx, dy int) {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	vertical := dy != 0
	if t.Focus.Kind == paneBrowser || t.Focus.Node == nil {
		if !vertical {
			t.BrowserRatio = min(max(t.BrowserRatio+0.05*float32(dx), 0.1), 0.6)
			a.changed()
		}
		return
	}
	for n := t.Focus.Node; n.Parent != nil; n = n.Parent {
		p := n.Parent
		if p.Vertical != vertical {
			continue
		}
		step := float32(0.05)
		if dx < 0 || dy < 0 {
			step = -step
		}
		p.Ratio = min(max(p.Ratio+step, 0.1), 0.9)
		a.changed()
		return
	}
}

// equalize gives every split of the tab equal shares, by the panes on
// either side.
func (a *App) equalize() {
	t := a.tab()
	if t == nil {
		return
	}
	var walk func(n *Node) (cols, rows int)
	walk = func(n *Node) (int, int) {
		if n.Pane != nil {
			return 1, 1
		}
		ac, ar := walk(n.A)
		bc, br := walk(n.B)
		if n.Vertical {
			n.Ratio = float32(ar) / float32(ar+br)
			return max(ac, bc), ar + br
		}
		n.Ratio = float32(ac) / float32(ac+bc)
		return ac + bc, max(ar, br)
	}
	walk(t.Root)
	a.changed()
}

func (a *App) toggleZoom() {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	if t.Zoom != nil {
		t.Zoom = nil
	} else if len(t.panes()) > 1 {
		t.Zoom = t.Focus
	}
	a.focusReq = t.Focus
	a.changed()
}

// label returns how a pane names itself: its program, or the title the
// program set, and the directory it works in.
func (p *Pane) label() (name, detail string) {
	switch p.Kind {
	case paneBrowser:
		return "files", shortDir(p.files.root)
	case paneEditor:
		return p.edit.label()
	}
	in := p.info
	prog := programOf(in.Program)
	dir := shortDir(in.Dir)
	if in.Program == "" {
		return in.Shell, shortDir(p.startDir)
	}
	if in.Idle || prog.Shell {
		name := in.Program
		if name == "" {
			name = in.Shell
		}
		return name, dir
	}
	if t := strings.TrimSpace(p.title); t != "" && !titleOfShell(t, in) {
		return t, ""
	}
	name = prog.Name
	if name == "ssh" || in.Program == "ssh" {
		if host := sshHost(in.Args); host != "" {
			return "SSH", host
		}
	}
	return name, dir
}

// titleOfShell tells a title a shell set, naming the program or the
// directory, from one a program set of its own.
func titleOfShell(title string, in rex.SessionInfo) bool {
	l := strings.ToLower(title)
	for _, w := range []string{strings.ToLower(in.Program), strings.ToLower(in.Shell)} {
		// Windows's consoles title themselves with the shell's file:
		// "C:\WINDOWS\system32\cmd.exe - ping".
		if w != "" && (strings.HasPrefix(l, w) || strings.Contains(l, `\`+w+".exe")) {
			return true
		}
	}
	if home := hostHome; strings.Contains(title, "~") || (home != "" && strings.Contains(title, home)) {
		return true
	}
	return strings.Contains(title, "@") && strings.Contains(title, ":")
}

func sshHost(args []string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if len(a) == 2 && strings.Contains("bcDEeFIiJLlmOopQRSWw", a[1:]) {
				i++
			}
			continue
		}
		return a
	}
	return ""
}

// shortDir shows a directory as shells do, from ~, and only its last
// parts when it is deep.
func shortDir(d string) string {
	if d == "" {
		return ""
	}
	sep := "/"
	if hostGOOS == "windows" {
		sep = `\`
	}
	if home := hostHome; home != "" {
		if samePath(d, home) {
			return "~"
		}
		if len(d) > len(home) && samePath(d[:len(home)], home) && d[len(home)] == sep[0] {
			d = "~" + d[len(home):]
		}
	}
	parts := strings.Split(d, sep)
	if len(parts) > 5 || len(d) > 48 && len(parts) > 3 {
		return "…" + sep + strings.Join(parts[len(parts)-2:], sep)
	}
	return d
}

// samePath reports whether two paths are the same, without case on a
// Windows server.
func samePath(a, b string) bool {
	if hostGOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// tabLabel names a tab: its name, or its focused pane's.
func (t *Tab) label() (name, detail string) {
	if t.Name != "" {
		return t.Name, ""
	}
	if t.Kind == paneEditor && t.Browser != nil {
		return baseName(t.Browser.files.root), ""
	}
	if t.Focus == nil {
		return "Shell", ""
	}
	return t.Focus.label()
}

// The saved layout: the tabs, their splits and the sessions of their
// panes, which the server keeps, and the app restores when it starts.
type savedLayout struct {
	Active int        `json:"active"`
	Tabs   []savedTab `json:"tabs"`
}

type savedTab struct {
	// Kind is "" for a terminal tab, "editor" for a tab of editors with
	// Browser, its file browser.
	Kind    string        `json:"kind,omitempty"`
	Name    string        `json:"name,omitempty"`
	Browser *savedBrowser `json:"browser,omitempty"`
	Root    *savedNode    `json:"root"`
	// Focus is the focused pane's place among the tree's panes, -1 for
	// the file browser.
	Focus int  `json:"focus"`
	Zoom  bool `json:"zoom,omitempty"`
}

type savedBrowser struct {
	Root   string   `json:"root"`
	Ratio  float32  `json:"ratio,omitempty"`
	Open   []string `json:"open,omitempty"`
	Hidden bool     `json:"hidden,omitempty"`
}

type savedNode struct {
	SID      string     `json:"sid,omitempty"`
	Dir      string     `json:"dir,omitempty"`
	Cols     int        `json:"cols,omitempty"`
	Rows     int        `json:"rows,omitempty"`
	// Path and Mode are an editor pane's file and "editor" or "changes".
	Path     string     `json:"path,omitempty"`
	Mode     string     `json:"mode,omitempty"`
	Vertical bool       `json:"vertical,omitempty"`
	Ratio    float32    `json:"ratio,omitempty"`
	A        *savedNode `json:"a,omitempty"`
	B        *savedNode `json:"b,omitempty"`
}

func (a *App) snapshot() savedLayout {
	l := savedLayout{Active: a.active}
	var save func(n *Node) *savedNode
	save = func(n *Node) *savedNode {
		if p := n.Pane; p != nil {
			if p.Kind == paneEditor {
				mode := p.edit.mode
				if mode == modeNative {
					mode = modePreview // the trial's renderer isn't kept
				}
				return &savedNode{Path: p.edit.path, Mode: mode}
			}
			s := &savedNode{SID: p.SID, Dir: p.info.Dir}
			if s.Dir == "" {
				s.Dir = p.startDir
			}
			if p.term != nil {
				s.Cols, s.Rows = p.term.Size()
			}
			return s
		}
		return &savedNode{Vertical: n.Vertical, Ratio: n.Ratio, A: save(n.A), B: save(n.B)}
	}
	for _, t := range a.tabs {
		st := savedTab{Name: t.Name, Root: save(t.Root), Zoom: t.Zoom != nil}
		st.Focus = max(slices.Index(t.panes(), t.Focus), 0)
		if t.Kind == paneEditor && t.Browser != nil {
			st.Kind = "editor"
			st.Browser = t.Browser.files.saved(t.BrowserRatio)
			if t.Focus == t.Browser {
				st.Focus = -1
			}
		}
		l.Tabs = append(l.Tabs, st)
	}
	return l
}

// changed notes that the layout changed, to save it soon.
func (a *App) changed() { a.saveDue = true }

// save sends the layout to the server, which keeps it on disk.
func (a *App) save() {
	a.saveDue = false
	if a.client == nil {
		return
	}
	b, err := json.Marshal(a.snapshot())
	if err != nil || string(b) == a.lastSnapshot {
		return
	}
	a.lastSnapshot = string(b)
	a.lastSave = time.Now()
	c := a.client
	go c.Set("layout", b)
}

// restore opens the tabs of the saved layout, attaching to the sessions
// the server still has and starting new ones in place of those it lost.
func (a *App) restore() bool {
	raw, err := a.client.Get("layout")
	if err != nil || len(raw) == 0 {
		return false
	}
	var l savedLayout
	if json.Unmarshal(raw, &l) != nil || len(l.Tabs) == 0 {
		return false
	}
	live := map[string]rex.SessionInfo{}
	if infos, err := a.client.List(); err == nil {
		for _, in := range infos {
			if !in.Exited {
				live[in.ID] = in
			}
		}
	}
	used := map[string]bool{}
	var load func(s *savedNode, t *Tab, parent *Node) *Node
	load = func(s *savedNode, t *Tab, parent *Node) *Node {
		n := &Node{ID: a.id(), Parent: parent}
		if s.A == nil || s.B == nil {
			var p *Pane
			switch {
			case t.Kind == paneEditor:
				p = a.newEditorPane(s.Path)
				if s.Mode == modeChanges || s.Mode == modePreview {
					p.edit.mode = s.Mode
				}
				if viewable(s.Path) != "" {
					p.edit.mode = modeView
				}
			default:
				if in, ok := live[s.SID]; ok && !used[s.SID] {
					used[s.SID] = true
					p = &Pane{ID: a.id(), SID: s.SID, info: in, startDir: s.Dir}
					a.attach(p, in.Cols, in.Rows)
				} else {
					p = a.newPane(s.Dir, s.Cols, s.Rows)
					p.restored = true
				}
			}
			p.Tab, p.Node = t, n
			n.Pane = p
			return n
		}
		n.Vertical, n.Ratio = s.Vertical, min(max(s.Ratio, 0.05), 0.95)
		n.A, n.B = load(s.A, t, n), load(s.B, t, n)
		return n
	}
	for _, st := range l.Tabs {
		if st.Root == nil {
			continue
		}
		t := &Tab{ID: a.id(), Name: st.Name}
		if st.Kind == "editor" {
			t.Kind = paneEditor
			b := st.Browser
			if b == nil {
				b = &savedBrowser{Root: hostHome}
			}
			t.Browser = a.newBrowserPane(t, b)
			t.BrowserRatio = b.Ratio
		}
		t.Root = load(st.Root, t, nil)
		ps := t.panes()
		for _, p := range ps {
			t.setFocus(p)
		}
		if st.Focus < 0 && t.Browser != nil {
			t.setFocus(t.Browser)
		} else {
			t.setFocus(ps[min(max(st.Focus, 0), len(ps)-1)])
		}
		if st.Zoom && len(ps) > 1 {
			t.Zoom = t.Focus
		}
		a.tabs = append(a.tabs, t)
	}
	// Sessions no tab shows, as those of another window that crashed,
	// get a tab of their own rather than running unseen.
	for _, in := range sortedInfos(live) {
		if used[in.ID] {
			continue
		}
		t := &Tab{ID: a.id()}
		p := &Pane{ID: a.id(), SID: in.ID, info: in, startDir: in.Dir}
		a.attach(p, in.Cols, in.Rows)
		t.Root = &Node{ID: a.id(), Pane: p}
		p.Tab, p.Node = t, t.Root
		t.setFocus(p)
		a.tabs = append(a.tabs, t)
	}
	if len(a.tabs) == 0 {
		return false
	}
	a.active = min(max(l.Active, 0), len(a.tabs)-1)
	a.adoptKeptEdits()
	a.focusReq = a.tab().Focus
	return true
}

func sortedInfos(m map[string]rex.SessionInfo) []rex.SessionInfo {
	out := make([]rex.SessionInfo, 0, len(m))
	for _, in := range m {
		out = append(out, in)
	}
	slices.SortFunc(out, func(a, b rex.SessionInfo) int { return a.Created.Compare(b.Created) })
	return out
}

// poll asks the server what runs in each session when it tells of a
// change, and every ten seconds in case, until the window closes; and
// hands the server's other events to the tabs.
func (a *App) poll(win *mygo.Window, client *rex.Client) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	// Events come in bursts; a list is asked at most every 100 ms.
	var listDue <-chan time.Time
	list := func() {
		listDue = nil
		infos, err := client.List()
		if err != nil {
			return
		}
		byID := make(map[string]rex.SessionInfo, len(infos))
		for _, in := range infos {
			byID[in.ID] = in
		}
		win.Update(func() {
			if a.win == win {
				a.apply(byID)
			}
		})
	}
	list()
	for {
		select {
		case <-client.Closed():
			// The tabs stay until the window connects again, which shows
			// them as the server has them then.
			win.Update(func() {
				if a.quitting || a.client != client {
					return
				}
				a.logConn("lost the connection to " + a.bind.label())
				a.startConnecting(win, a.bind, "Lost the connection to "+a.bind.label()+" — reconnecting…")
			})
			return
		case <-tick.C:
			list()
		case <-listDue:
			list()
		case ev := <-client.Events:
			switch ev.Event {
			case "sessions":
				if listDue == nil {
					listDue = time.After(100 * time.Millisecond)
				}
			case "changed":
				win.Update(func() {
					if a.win == win && a.client == client {
						a.dirChanged(ev.Path)
					}
				})
			case "bell":
				win.Update(func() {
					if a.win == win && a.client == client {
						a.sessionBell(ev.Path)
					}
				})
			case "notify":
				var n rex.Notice
				json.Unmarshal(ev.Data, &n)
				win.Update(func() {
					if a.win == win && a.client == client {
						a.sessionNotice(ev.Path, n)
					}
				})
			}
		}
	}
}

// sessionBell takes the bell a session's program rang, in the panes of
// this window that show it.
func (a *App) sessionBell(sid string) {
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.SID == sid && !p.closed {
				a.ring(p)
			}
		}
	}
}

// sessionNotice shows the desktop notification a session's program sent,
// when a pane of this window shows the session.
func (a *App) sessionNotice(sid string, n rex.Notice) {
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.SID == sid && !p.closed {
				if n.Title == "" {
					n.Title = "GoRex"
				}
				mygo.NewNotification(mygo.NotificationOptions{Title: n.Title, Body: n.Body}).Show()
				return
			}
		}
	}
}

// apply takes what the server said of the sessions.
func (a *App) apply(byID map[string]rex.SessionInfo) {
	for ti, t := range a.tabs {
		for _, p := range t.panes() {
			in, ok := byID[p.SID]
			if !ok {
				continue
			}
			was := p.info
			p.info = in
			seen := ti == a.active && t.Focus == p && a.focusedWin
			a.statusNotice(p, was.Status, in.Status, seen, in.Dir)
			// A program that ran a while and finished out of sight
			// asks for attention.
			if !was.Idle && was.Program != "" && in.Idle && was.Program != in.Program {
				if !seen && is(a.eff.General.Attention) {
					p.attention = true
				}
				ran := time.Since(was.LastInput)
				if !a.focusedWin && is(a.eff.General.Notify) && ran > time.Duration(a.eff.General.NotifyAfter)*time.Second {
					prog := programOf(was.Program)
					n := mygo.NewNotification(mygo.NotificationOptions{
						Title: prog.Name + " finished",
						Body:  "in " + shortDir(in.Dir),
					})
					n.Show()
				}
			}
			if was.Dir != in.Dir {
				a.changed()
			}
		}
	}
	if a.saveDue && time.Since(a.lastSave) > time.Second {
		a.save()
	}
}

// laterService queues a menu action without keeping a build context.
func (a *App) laterService(fn func()) { a.post(fn); a.services.Invalidate() }
