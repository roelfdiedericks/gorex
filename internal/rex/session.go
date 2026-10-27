//go:build darwin || linux || windows

package rex

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// ScrollbackLines is how many lines of output a session keeps above its
// screen, for the windows that attach to it, when the session asks for
// no particular number.
const ScrollbackLines = 10000

// session is a pseudo-terminal of the server and its shell.
type session struct {
	id      string
	shell   string
	created time.Time
	p       *pty
	// lines is how many lines of scrollback the screen keeps, and images
	// the most bytes its images take (terminal.Options.ImageLimit).
	lines  int
	images int64
	// emit sends the server's control connections an event.
	emit func(Response)

	mu sync.Mutex
	// vt is the session's terminal, which the program writes to and which
	// answers it, kept by the terminal emulator of the app's terminals
	// without a view: windows show it through frames of a Mirror each.
	vt         *terminal.Terminal
	vtIn       *io.PipeWriter
	output     uint64
	bells      atomic.Int64
	clients    map[*attached]struct{}
	lastOutput time.Time
	lastInput  time.Time
	// status is what the session's programs say they are doing, paste
	// what they do with paste events, and tsp what they show through
	// the Tern Surface Protocol.
	status     statusTracker
	paste      pasteTracker
	tsp        tspTracker
	exited     bool
	code       int
	cols, rows int
	done       chan struct{}
	// cellWidth and cellHeight are the size of a window's cells in pixels,
	// zero when unknown.
	cellWidth, cellHeight int
	// readDone is closed when the reader is done. While pausing, the
	// reader, its read cut short, says so on pauseAck and waits for
	// pauseEnd: an upgrade in place pauses every session.
	readDone           chan struct{}
	pausing            atomic.Bool
	pauseAck, pauseEnd chan struct{}
}

// attached is a connection attached to a session, whose frames a
// goroutine writes (session.serve), so that a slow window never holds up
// the others: it frames all that changed when it gets to it.
type attached struct {
	conn net.Conn
	m    *terminal.Mirror
	// wake asks for a frame; done ends the writer, after a last one.
	wake chan struct{}
	done chan struct{}
	once sync.Once
	// cols and rows are the size the window was told last, paste the
	// paste events' mode, and reads the paste reads it is to get;
	// session.mu guards them.
	cols, rows int
	paste      bool
	reads      []string
	// surfaced is the version of the surfaces the window has (0: none
	// yet), and blobs the blobs it was sent, by id.
	surfaced uint64
	blobs    map[string]bool
}

func (a *attached) poke() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *attached) finish() { a.once.Do(func() { close(a.done) }) }

func newSession(id string, o CreateOptions, emit func(Response)) (*session, error) {
	path, argv, name, err := shellCommand(o.Command, o.Login)
	if err != nil {
		return nil, err
	}
	cols, rows := o.Cols, o.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	dir := o.Dir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	env := sessionEnv(id, o.Identity, o.TSP, o.Env)
	p, err := startPTY(path, argv, dir, env, cols, rows)
	if err != nil {
		return nil, err
	}
	lines := ScrollbackLines
	if o.Scrollback > 0 {
		lines = o.Scrollback
	}
	s := &session{id: id, shell: name, created: time.Now(), p: p, lines: lines, images: o.ImageLimit, cols: cols, rows: rows, emit: emit}
	s.tsp.on, s.tsp.cols = o.TSP, cols
	if err := s.start(nil); err != nil {
		p.hangup()
		return nil, err
	}
	return s, nil
}

// start runs the session: its terminal, first fed screen when the session
// is resumed from the server this one replaced, and the goroutines that
// read what it prints and wait for its end.
func (s *session) start(screen []byte) error {
	s.clients = map[*attached]struct{}{}
	s.done, s.readDone = make(chan struct{}), make(chan struct{})
	s.pauseAck, s.pauseEnd = make(chan struct{}), make(chan struct{})
	pr, pw := io.Pipe()
	vt, err := terminal.New(terminal.Options{
		Conn:            vtConn{pr, s.p},
		ScrollbackLines: s.lines,
		ImageLimit:      s.images,
		// Programs' reads of the clipboard go on to a window, which
		// answers them as it would its own program's.
		ClipboardRead: terminal.ClipboardForward,
		// The anchors of programs' surfaces (tsp.go).
		AppImages: struct{ First, Last uint32 }{TSPImageFirst, TSPImageLast},
		OnBell: func() {
			s.bells.Add(1)
			go s.emit(Response{Event: "bell", Path: s.id})
		},
		OnNotify: func(title, body string) {
			data, _ := json.Marshal(Notice{Title: title, Body: body})
			go s.emit(Response{Event: "notify", Path: s.id, Data: data})
		},
	})
	if err != nil {
		return err
	}
	s.vt = vt
	vt.ResizePixels(s.cols, s.rows, s.cellWidth, s.cellHeight)
	s.tsp.setMost(s.tspMost())
	if len(screen) > 0 {
		vt.Feed(screen)
	}
	s.vtIn = pw
	go func() {
		s.read()
		close(s.readDone)
	}()
	go func() {
		code := s.p.wait()
		// What it printed last is still to read; then hang up whatever
		// else holds the terminal, as terminal apps do.
		select {
		case <-s.readDone:
		case <-time.After(150 * time.Millisecond):
			s.p.close()
			<-s.readDone
		}
		s.mu.Lock()
		s.exited, s.code = true, code
		for a := range s.clients {
			a.finish()
		}
		clear(s.clients)
		s.mu.Unlock()
		close(s.done)
		s.vtIn.Close()
		s.vt.Close()
	}()
	return nil
}

// sessionEnv returns the environment of a session's shell: the server's,
// without what other terminals set, and what terminal apps set, with
// TERM_PROGRAM by identity; tsp tells that the session answers TSP.
func sessionEnv(id, identity string, tsp bool, extra []string) []string {
	var env []string
	hasLang := false
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "TERM", k == "COLORTERM", k == "TERM_PROGRAM", k == "TERM_PROGRAM_VERSION",
			k == "TERM_SESSION_ID", k == "TMUX", k == "TMUX_PANE", k == "STY", k == "WINDOWID",
			k == "COLUMNS", k == "LINES", k == "SHLVL", k == "OLDPWD", k == "PWD", k == "_",
			strings.HasPrefix(k, "ITERM_"), strings.HasPrefix(k, "GHOSTTY_"), strings.HasPrefix(k, "KITTY_"),
			strings.HasPrefix(k, "VSCODE_"), strings.HasPrefix(k, "WEZTERM_"), strings.HasPrefix(k, "ALACRITTY_"),
			strings.HasPrefix(k, "GOREX_"), strings.HasPrefix(k, "MYGO_"):
			continue
		case k == "LANG" || k == "LC_ALL" || k == "LC_CTYPE":
			hasLang = true
		}
		env = append(env, kv)
	}
	program, version := termProgram(identity)
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM="+program,
		"TERM_PROGRAM_VERSION="+version,
		"GOREX_SESSION="+id,
	)
	if !tsp {
		// The session answers no TSP hello: omp, which would ask and
		// wait for the reply, keeps its own renderer at once. With TSP,
		// omp asks, and draws natively once the session answers.
		env = append(env, "PI_TUI_NATIVE=0")
	}
	if !hasLang {
		env = append(env, "LANG=en_US.UTF-8")
	}
	return append(env, extra...)
}

// ghosttyVersion is the release of Ghostty whose terminal GoRex's is: the
// libghostty-vt MyGo loads, of Ghostty's commit befcdfd2.
const ghosttyVersion = "1.3.2-dev"

// termProgram returns the TERM_PROGRAM and TERM_PROGRAM_VERSION of an
// identity: Ghostty's, so that programs use what its terminal draws, or
// GoRex's own.
func termProgram(identity string) (string, string) {
	if identity == "ghostty" {
		return "ghostty", ghosttyVersion
	}
	return "GoRex", "0.1.0"
}

func (s *session) read() {
	buf := make([]byte, 64<<10)
	for {
		n, err := s.p.read(buf)
		if n > 0 {
			s.mu.Lock()
			// TSP messages are the server's: taken out before the
			// terminal sees the output, and answered first, so that the
			// hello reply comes before the terminal's DA1 answer. A
			// close among them takes back the rows padded under its
			// surface, as they are now (spareRows).
			s.tsp.spareRows(s.anchorRows())
			out := s.tsp.feed(buf[:n])
			replies := s.tsp.replies
			s.tsp.replies = nil
			if s.tsp.changed {
				// Each read's changes are an update, so that what the
				// windows are to be sent stays small with none attached.
				s.tsp.commit()
				s.wake()
			}
			s.mu.Unlock()
			for _, r := range replies {
				s.p.write(r)
			}
			s.mu.Lock()
			erased := s.scrollbackAnchors(out)
			s.vt.Feed(out)
			s.dropAnchors(erased)
			if len(s.tsp.order) > 0 && printable(out) {
				s.eraseUnder()
			}
			s.tspPad()
			s.status.feed(out)
			s.paste.feed(out)
			for a := range s.clients {
				a.reads = append(a.reads, s.paste.reads...)
			}
			s.paste.reads = nil
			query := s.status.query
			s.status.query = false
			s.output += uint64(n)
			s.lastOutput = time.Now()
			s.wake()
			s.mu.Unlock()
			if query {
				// The program asks whether the terminal speaks the
				// Program Status Protocol, which the server does.
				s.p.write([]byte("\x1b]7501;?\x1b\\"))
			}
		}
		if err != nil {
			// A pause for an upgrade in place: what the terminal has is
			// left to the server that replaces this one.
			if s.pausing.Load() && errors.Is(err, os.ErrDeadlineExceeded) {
				s.pauseAck <- struct{}{}
				<-s.pauseEnd
				continue
			}
			return
		}
	}
}

// wake has every window's writer frame the screen; s.mu is held.
func (s *session) wake() {
	for a := range s.clients {
		a.poke()
	}
}

// attach attaches a connection: the session takes its size and colors,
// it gets frames of the session's screen, and what it sends is typed.
func (s *session) attach(conn net.Conn, at Attach) {
	m, err := s.vt.NewMirror()
	if err != nil {
		conn.Close()
		return
	}
	a := &attached{conn: conn, m: m, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.mu.Lock()
	exited := s.exited
	if exited {
		a.finish()
	} else {
		s.clients[a] = struct{}{}
	}
	s.mu.Unlock()
	if !exited {
		if at.Theme != nil {
			s.vt.UseTheme(at.Theme)
			s.mu.Lock()
			dark := darkTheme(at.Theme)
			var events [][]byte
			if dark != s.tsp.dark {
				s.tsp.dark = dark
				events = s.tsp.themed()
			}
			s.mu.Unlock()
			for _, e := range events {
				s.p.write(e)
			}
		}
		if at.Cols > 0 && at.Rows > 0 {
			s.resize(at.Cols, at.Rows, at.CellWidth, at.CellHeight)
		}
		s.motion(at.Reduce)
	}
	a.poke()
	go s.serve(a)
	buf := make([]byte, 32<<10)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			s.input(buf[:n])
		}
		if err != nil {
			break
		}
	}
	s.mu.Lock()
	delete(s.clients, a)
	s.mu.Unlock()
	a.finish()
}

// motion takes whether the window asks for reduced motion: the
// session's program, told in its hello reply, is told when it changes.
func (s *session) motion(reduce bool) {
	s.mu.Lock()
	var e []byte
	if reduce != s.tsp.reduce {
		s.tsp.reduce = reduce
		e = s.tsp.moved()
	}
	exited := s.exited
	s.mu.Unlock()
	if e != nil && !exited {
		s.p.write(e)
	}
}

// frameRetry is how soon a frame a synchronized update held back is
// tried again.
const frameRetry = 20 * time.Millisecond

// serve writes a window its frames, as the session's screen changes, until
// the window detaches or the session ends, with a last frame then.
func (s *session) serve(a *attached) {
	defer func() {
		a.m.Close()
		a.conn.Close()
	}()
	var size [9]byte
	for {
		bufs, due := s.frame(a, &size)
		if len(bufs) > 0 {
			if _, err := bufs.WriteTo(a.conn); err != nil {
				return
			}
		}
		var retry <-chan time.Time
		if due {
			retry = time.After(frameRetry)
		}
		select {
		case <-a.wake:
		case <-retry:
		case <-a.done:
			if bufs, _ := s.frame(a, &size); len(bufs) > 0 {
				bufs.WriteTo(a.conn)
			}
			return
		}
	}
}

// frame returns what brings a window to the session's screen: the
// session's size first, when the window was told another, then a frame,
// then the paste events' mode and reads, then the surfaces when they
// changed; due reports a frame a synchronized update holds back. size is
// where the size message is put.
func (s *session) frame(a *attached, size *[9]byte) (bufs net.Buffers, due bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.cols != s.cols || a.rows != s.rows {
		a.cols, a.rows = s.cols, s.rows
		size[0] = msgSize
		binary.BigEndian.PutUint32(size[1:], 4)
		binary.BigEndian.PutUint16(size[5:], uint16(s.cols))
		binary.BigEndian.PutUint16(size[7:], uint16(s.rows))
		bufs = append(bufs, size[:])
	}
	f, due := a.m.Frame()
	if a.paste != s.paste.on {
		a.paste = s.paste.on
		if a.paste {
			f = append(f, "\x1b[?5522h"...)
		} else {
			f = append(f, "\x1b[?5522l"...)
		}
	}
	for _, r := range a.reads {
		f = append(f, r...)
	}
	a.reads = nil
	// The blobs the window lacks, before the surfaces that show them.
	for _, id := range s.tsp.blobOrder {
		if a.blobs[id] {
			continue
		}
		if a.blobs == nil {
			a.blobs = map[string]bool{}
		}
		a.blobs[id] = true
		b := s.tsp.blobs[id]
		head := make([]byte, 5, 5+64+1+len(b.Mime))
		head[0] = msgBlob
		binary.BigEndian.PutUint32(head[1:], uint32(64+1+len(b.Mime)+len(b.Data)))
		head = append(head, b.ID...)
		head = append(head, byte(len(b.Mime)))
		head = append(head, b.Mime...)
		bufs = append(bufs, head, b.Data)
	}
	// Each window gets the surfaces whole as it attaches, and after
	// that the updates since the version it has: the ops the programs
	// applied, a few hundred bytes for a document of tens of
	// kilobytes; whole again when it lags further than the updates
	// kept.
	t := &s.tsp
	v := t.commit()
	switch {
	case a.surfaced == v:
	case a.surfaced != 0:
		if b, ok := t.updatesSince(a.surfaced); ok {
			var h [5]byte
			h[0] = msgSurfaceUpdates
			binary.BigEndian.PutUint32(h[1:], uint32(len(b)))
			bufs = append(bufs, h[:], b)
			a.surfaced = v
			break
		}
		fallthrough
	default:
		if b, err := json.Marshal(SurfaceSet{V: v, Surfaces: t.list()}); err == nil {
			var h [5]byte
			h[0] = msgSurface
			binary.BigEndian.PutUint32(h[1:], uint32(len(b)))
			bufs = append(bufs, h[:], b)
			a.surfaced = v
		}
	}
	if len(f) > 0 {
		var h [5]byte
		h[0] = msgFrame
		binary.BigEndian.PutUint32(h[1:], uint32(len(f)))
		bufs = append(bufs, h[:], f)
	}
	return bufs, due
}

func (s *session) input(p []byte) {
	s.mu.Lock()
	s.lastInput = time.Now()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		s.p.write(p)
	}
}

// resize sets the size of the terminal, in cells, and of its cells in
// pixels, which a zero size leaves as they were.
func (s *session) resize(cols, rows, cellWidth, cellHeight int) {
	s.mu.Lock()
	if cellWidth <= 0 || cellHeight <= 0 {
		cellWidth, cellHeight = s.cellWidth, s.cellHeight
	}
	changed := cols != s.cols || rows != s.rows || cellWidth != s.cellWidth || cellHeight != s.cellHeight
	s.cols, s.rows, s.cellWidth, s.cellHeight = cols, rows, cellWidth, cellHeight
	var events [][]byte
	if cols != s.tsp.cols || cellWidth != s.tsp.cellW || cellHeight != s.tsp.cellH {
		s.tsp.cols, s.tsp.cellW, s.tsp.cellH = cols, cellWidth, cellHeight
		events = s.tsp.resized()
	}
	exited := s.exited
	if changed {
		s.vt.ResizePixels(cols, rows, cellWidth, cellHeight)
		s.tsp.setMost(s.tspMost())
		s.wake()
	}
	s.mu.Unlock()
	if changed && !exited {
		s.p.resize(cols, rows, cols*cellWidth, rows*cellHeight)
	}
	if !exited {
		for _, e := range events {
			s.p.write(e)
		}
	}
}

// tspMost is the most rows a surface takes (Surface.Most): the lines the
// scrollback keeps for sure, which is its limit less a page of the
// terminal's, which goes whole (some 380 KB, of 8-byte cells and an 8-byte
// head a row), and the screen's but the cursor's row. s.mu held, or the
// session not started.
func (s *session) tspMost() int {
	page := 380_000/(8*(s.cols+1)) + 1
	return max(s.lines-page, 0) + s.rows - 1
}

// clear clears the screen as the user asked in a window, and has the
// windows show it.
func (s *session) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	erased := s.scrollbackAnchors([]byte(eraseScrollback))
	if !s.exited && s.vt.ClearScreen() {
		s.dropAnchors(erased)
		s.tspPad()
		s.wake()
	}
}

// eraseScrollback is the sequence that erases the scrollback (ED 3).
const eraseScrollback = "\x1b[3J"

// scrollbackAnchors returns what deletes the markers of surfaces in the
// scrollback when what is to be fed erases it, as clear does: the
// terminal keeps their placements, moved to the top of the screen, where
// those surfaces would draw over the text that is there (dropAnchors). A
// kept surface whose end is in the scrollback goes whole; one whose
// anchor alone is there, live or kept, keeps its end, drawn up from it.
// A live surface without an end keeps its anchor, at the top: the
// surface is the program's, drawn while it runs. s.mu held.
func (s *session) scrollbackAnchors(fed []byte) []byte {
	if !bytes.Contains(fed, []byte(eraseScrollback)) {
		return nil
	}
	var b []byte
	for image, mk := range s.markerRows() {
		sf := s.tsp.byImage(image)
		switch {
		case sf == nil:
		case sf.Closed && (!mk.hasEnd || mk.end < 0) && (!mk.hasStart || mk.start < 0):
			b = append(b, deleteAnchor(image)...)
		case mk.hasStart && mk.start < 0 && mk.hasEnd && mk.end >= 0:
			b = append(b, deleteStart(image)...)
		}
	}
	return b
}

// dropAnchors feeds the terminal the deletes of markers whose rows went
// (scrollbackAnchors); the next sweep drops the surfaces left without
// any. s.mu held.
func (s *session) dropAnchors(deletes []byte) {
	if len(deletes) > 0 {
		s.vt.Feed(deletes)
	}
}

// tspSize takes the rows a window measured a live surface to need, and
// those of its dock, and feeds the terminal the lines for them.
func (s *session) tspSize(sf string, rows, dock int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited || !s.tsp.resize(sf, rows, dock) {
		return
	}
	s.tspPad()
	s.wake()
}

// markers are where an inline surface's anchor (start) and end marker
// are in the terminal, from the screen's top, negative in the
// scrollback, and which of them there are.
type markers struct {
	start, end       int
	hasStart, hasEnd bool
}

// markerRows returns the surfaces' markers in the terminal, by image id.
// s.mu held.
func (s *session) markerRows() map[uint32]markers {
	m := map[uint32]markers{}
	for _, pl := range s.vt.AppPlacements() {
		mk := m[pl.Image]
		if pl.Cols == 2 {
			mk.end, mk.hasEnd = pl.Row, true
		} else {
			mk.start, mk.hasStart = pl.Row, true
		}
		m[pl.Image] = mk
	}
	return m
}

// anchorRows returns the rows of the surfaces' anchors (image id → row,
// negative in the scrollback) and the cursor's row: where the anchor is,
// or for one the scrollback dropped, where it was, its surface's rows up
// from its end marker. s.mu held.
func (s *session) anchorRows() (map[uint32]int, int) {
	rows := map[uint32]int{}
	for image, mk := range s.markerRows() {
		switch sf := s.tsp.byImage(image); {
		case mk.hasStart:
			rows[image] = mk.start
		case mk.hasEnd && sf != nil:
			rows[image] = mk.end - (max(sf.Rows, 1) - 1)
		}
	}
	_, cursor := s.vt.Cursor()
	return rows, cursor
}

// placeEnds places the end markers of surfaces whose content ends on
// another row than their marker, or that have none yet: just after a
// surface's rows changed, live or kept, when that row is on the screen,
// above the cursor, which it puts back. A surface whose anchor went has
// its end where it stays. s.mu held.
func (s *session) placeEnds() {
	var b strings.Builder
	col, cursor := s.vt.Cursor()
	for image, mk := range s.markerRows() {
		sf := s.tsp.byImage(image)
		if sf == nil || !mk.hasStart {
			continue
		}
		end := mk.start + max(sf.Rows, 1) - 1
		if mk.hasEnd && mk.end == end || end < 0 || end >= cursor {
			continue
		}
		fmt.Fprintf(&b, "\x1b[%d;1H%s", end+1, endMarker(image))
	}
	if b.Len() > 0 {
		fmt.Fprintf(&b, "\x1b[%d;%dH", cursor+1, col+1)
		s.vt.Feed([]byte(b.String()))
	}
}

// tspPad feeds the terminal the lines the last opened inline surface
// lacks under its anchor, or takes back those it has spare
// (tspTracker.padding), inserts the rows kept surfaces are to get
// (grow), drops the kept surfaces whose markers are gone, as after a
// clear (sweep), and places the end markers where the surfaces' content
// ends now (placeEnds). The lines fed are erased as they come: they are
// the surface's, and what a program printed there before, as a shell's
// last words, would show through. s.mu held.
func (s *session) tspPad() {
	if len(s.tsp.order) == 0 && len(s.tsp.kept) == 0 {
		return
	}
	rows, cursor := s.anchorRows()
	s.tsp.sweep(rows)
	if len(s.tsp.growing) > 0 {
		if g := s.tsp.grow(rows, cursor, s.rows); g != "" {
			s.vt.Feed([]byte(g))
			rows, cursor = s.anchorRows()
		}
	}
	switch n := s.tsp.padding(rows, cursor, s.rows); {
	case n > 0:
		s.vt.Feed([]byte(strings.Repeat("\r\n\x1b[2K", n)))
	case n < 0:
		// Up over the spare rows, which are blank, and clear them.
		s.vt.Feed([]byte(reclaim(-n)))
	}
	s.placeEnds()
}

// eraseUnder erases what the program printed in the screen's rows under
// its live inline surfaces, where it is to print nothing (padding): a
// late paint of its rows, as omp has been seen to make. A window would
// show it beside the surface, in the strip the terminal's scroll bar
// keeps clear. The anchor's row, with what came before the anchor, and
// the end marker's two cells stay; the cursor goes back where it was.
// s.mu held.
func (s *session) eraseUnder() {
	rows, _ := s.anchorRows()
	col, cursor := s.vt.Cursor()
	var b strings.Builder
	for _, id := range s.tsp.order {
		sf := s.tsp.surfaces[id]
		if sf == nil || sf.Image == 0 {
			continue
		}
		top, ok := rows[sf.Image]
		if !ok {
			continue
		}
		// A surface with a dock takes the screen down from its anchor.
		last := top + max(sf.Rows, 1) + sf.Dock - 1
		if sf.docked() {
			last = s.rows - 1
		}
		for r := max(top+1, 0); r <= min(last, s.rows-1); r++ {
			fmt.Fprintf(&b, "\x1b[%d;3H\x1b[K", r+1)
		}
	}
	if b.Len() > 0 {
		fmt.Fprintf(&b, "\x1b[%d;%dH", cursor+1, col+1)
		s.vt.Feed([]byte(b.String()))
	}
}

// printable reports whether output has text to show, outside the escape
// sequences (CSI, OSC, DCS, APC and the like) and control characters.
func printable(out []byte) bool {
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case c == 0x1b && i+1 < len(out):
			i++
			switch out[i] {
			case '[':
				// To the final byte.
				for i++; i < len(out) && (out[i] < 0x40 || out[i] > 0x7e); i++ {
				}
			case ']', 'P', '_', '^', 'X':
				// A string, to BEL or ST.
				for i++; i < len(out) && out[i] != 0x07 && !(out[i] == 0x1b && i+1 < len(out) && out[i+1] == '\\'); i++ {
				}
				if i < len(out) && out[i] == 0x1b {
					i++
				}
			}
		case c >= 0x20 && c != 0x7f:
			return true
		}
	}
	return false
}

// kill hangs up the session and waits a little for it to end.
func (s *session) kill() {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		s.p.hangup()
	}
	select {
	case <-s.done:
	case <-time.After(time.Second):
		s.p.kill()
	}
}

// info describes the session and what runs in its foreground.
func (s *session) info() SessionInfo {
	s.mu.Lock()
	in := SessionInfo{
		ID: s.id, Shell: s.shell, Created: s.created,
		Title: s.vt.Title(), LastOutput: s.lastOutput, LastInput: s.lastInput,
		Output: s.output, Bells: int(s.bells.Load()), Exited: s.exited, ExitCode: s.code,
		Attached: len(s.clients), Cols: s.cols, Rows: s.rows, Status: s.status.status(),
	}
	s.mu.Unlock()
	in.PID = s.p.pid()
	if in.Exited {
		return in
	}
	fg := s.p.foreground()
	if fg <= 0 {
		fg = in.PID
	}
	in.Idle = fg == in.PID
	if in.Idle && isShellName(s.shell) {
		// The shell is back at its prompt: what ran before has ended.
		s.mu.Lock()
		s.status.dropRunning()
		in.Status = s.status.status()
		s.mu.Unlock()
	}
	p := inspect(fg)
	in.Program, in.Args, in.Dir = p.programName(), p.args, p.dir
	if in.Program == "" {
		in.Program = s.shell
	}
	if in.Dir == "" && !in.Idle {
		in.Dir = inspect(in.PID).dir
	}
	return in
}

// vtConn is the Conn of a session's terminal: it reads from a pipe that
// closes with the session, which feeds the terminal what the program
// prints itself, and writes the terminal's answers to the program's
// questions to the pseudo-terminal.
type vtConn struct {
	r *io.PipeReader
	p *pty
}

func (c vtConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c vtConn) Write(p []byte) (int, error) {
	c.p.write(p)
	return len(p), nil
}
func (c vtConn) Close() error { return c.r.Close() }
