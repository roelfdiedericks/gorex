//go:build darwin || linux || windows

package rex

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// scrollback is about how many bytes of output a session keeps above its
// screen, for the windows that attach to it, when the session asks for
// no particular number of lines; scrollbackLine is about what a line
// takes, to size the buffer from a number of lines.
const (
	scrollback     = 8 << 20
	scrollbackLine = 256
)

// session is a pseudo-terminal of the server and its shell.
type session struct {
	id      string
	shell   string
	created time.Time
	p       *pty
	// keep is how many bytes of scrollback the screen keeps, and images
	// the most bytes its images take (terminal.Options.ImageLimit).
	keep   int
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
	// surfaced tells that the window was sent the surfaces.
	surfaced bool
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
	env := sessionEnv(id, o.Identity, o.Env)
	p, err := startPTY(path, argv, dir, env, cols, rows)
	if err != nil {
		return nil, err
	}
	keep := scrollback
	if o.Scrollback > 0 {
		keep = o.Scrollback * scrollbackLine
	}
	s := &session{id: id, shell: name, created: time.Now(), p: p, keep: keep, images: o.ImageLimit, cols: cols, rows: rows, emit: emit}
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
		Conn:       vtConn{pr, s.p},
		Scrollback: s.keep,
		ImageLimit: s.images,
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
// TERM_PROGRAM by identity.
func sessionEnv(id, identity string, extra []string) []string {
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
		// omp never tries TSP: GoRex draws only screen surfaces so far,
		// and omp's UI is an inline one. The Tern SDK reads TERN_TSP.
		"PI_TUI_NATIVE=0",
	)
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
			// hello reply comes before the terminal's DA1 answer.
			out := s.tsp.feed(buf[:n])
			replies := s.tsp.replies
			s.tsp.replies = nil
			if s.tsp.changed {
				s.wake()
			}
			s.mu.Unlock()
			for _, r := range replies {
				s.p.write(r)
			}
			s.mu.Lock()
			s.vt.Feed(out)
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
	if t := &s.tsp; t.changed || !a.surfaced {
		t.changed, a.surfaced = false, true
		if b, err := json.Marshal(t.list()); err == nil {
			var h [5]byte
			h[0] = msgSurface
			binary.BigEndian.PutUint32(h[1:], uint32(len(b)))
			bufs = append(bufs, h[:], b)
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

// clear clears the screen as the user asked in a window, and has the
// windows show it.
func (s *session) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exited && s.vt.ClearScreen() {
		s.wake()
	}
}

// tspSize takes the rows a window measured a live surface to need, and
// feeds the terminal what moves the surface's anchor over them.
func (s *session) tspSize(sf string, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited || !s.tsp.resize(sf, rows) {
		return
	}
	s.tspPad()
	s.wake()
}

// tspPad feeds the terminal the lines the last opened inline surface
// lacks under its anchor, or takes back those it has spare
// (tspTracker.padding). s.mu held.
func (s *session) tspPad() {
	if len(s.tsp.order) == 0 {
		return
	}
	rows := map[uint32]int{}
	for _, pl := range s.vt.AppPlacements() {
		rows[pl.Image] = pl.Row
	}
	_, cursor := s.vt.Cursor()
	switch n := s.tsp.padding(rows, cursor); {
	case n > 0:
		s.vt.Feed([]byte(strings.Repeat("\r\n", n)))
	case n < 0:
		// Up over the spare rows, which are blank, and clear them.
		s.vt.Feed([]byte("\x1b[" + strconv.Itoa(-n) + "A\r\x1b[J"))
	}
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
