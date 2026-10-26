package rex

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// Client is a control connection to the server.
type Client struct {
	conn net.Conn
	dial Dialer

	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan Response
	err     error
	closed  chan struct{}
	// Events are what the server tells on its own: watched directories
	// that changed, sessions that changed, bells and notifications. The
	// channel is buffered; events a slow reader leaves are dropped, and a
	// reader drains it.
	Events chan Event
}

// Dialer opens a connection to the server.
type Dialer func() (net.Conn, error)

// EnsureServer makes sure the local server runs, starting one in the
// background when none does.
func EnsureServer() error {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		if err := Spawn(); err != nil {
			return fmt.Errorf("rex: starting the server: %w", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			time.Sleep(30 * time.Millisecond)
			conn, err = net.Dial("unix", SocketPath())
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("rex: connecting to the server: %w", err)
			}
		}
	}
	conn.Close()
	return nil
}

// LocalDialer dials the local server, starting one when none runs.
func LocalDialer() Dialer {
	return func() (net.Conn, error) {
		conn, err := net.Dial("unix", SocketPath())
		if err == nil {
			return conn, nil
		}
		if err := EnsureServer(); err != nil {
			return nil, err
		}
		return net.Dial("unix", SocketPath())
	}
}

// Connect opens a control connection through dial, which also opens the
// connections of the client's streams.
func Connect(dial Dialer) (*Client, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, dial: dial, pending: map[int64]chan Response{}, closed: make(chan struct{}), Events: make(chan Event, 256)}
	go c.read()
	return c, nil
}

// Executable returns the path of the running executable and when it was
// modified last.
func Executable() (string, time.Time) {
	exe, err := os.Executable()
	if err != nil {
		return "", time.Time{}
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	st, err := os.Stat(exe)
	if err != nil {
		return exe, time.Time{}
	}
	return exe, st.ModTime()
}

// Restart shuts the server down, ending its sessions, and connects to a
// new one: one that answers with a process ID other than pid.
func (c *Client) Restart(pid int) (*Client, error) {
	c.Shutdown()
	c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		nc, err := Connect(c.dial)
		if err != nil {
			continue
		}
		if h, err := nc.Hello(); err == nil && h.PID != pid {
			return nc, nil
		}
		nc.Close()
	}
	return nil, errors.New("rex: the server did not restart")
}

// ErrNoUpgrade is Upgrade's error when the server can't upgrade in place,
// as on Windows, or as an older server can't: Restart replaces it then.
var ErrNoUpgrade = errors.New("rex: the server can't upgrade in place")

// Upgrade has the server h describes replace itself in place with exe,
// keeping its sessions, and connects to the new server: one that started
// after it, from exe.
func (c *Client) Upgrade(h Hello, exe string) (*Client, error) {
	if err := c.call(Request{Op: "upgrade", Path: exe}, nil); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoUpgrade, err)
	}
	c.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		nc, err := Connect(c.dial)
		if err != nil {
			continue
		}
		if nh, err := nc.Hello(); err == nil && nh.Exe == exe && nh.Started.After(h.Started) {
			return nc, nil
		}
		nc.Close()
	}
	return nil, errors.New("rex: the server did not upgrade")
}

// Closed is closed once the connection to the server is lost.
func (c *Client) Closed() <-chan struct{} { return c.closed }

func (c *Client) read() {
	r := bufio.NewReaderSize(c.conn, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			c.mu.Lock()
			c.err = err
			for id, ch := range c.pending {
				ch <- Response{ID: id, Error: "connection to the server lost"}
				delete(c.pending, id)
			}
			c.mu.Unlock()
			close(c.closed)
			return
		}
		var res Response
		if json.Unmarshal(line, &res) != nil {
			continue
		}
		if res.ID == 0 {
			select {
			case c.Events <- Event{Event: res.Event, Path: res.Path, Data: res.Data}:
			default:
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[res.ID]
		delete(c.pending, res.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- res
		}
	}
}

// call sends a request and decodes the response's data into out.
func (c *Client) call(req Request, out any) error {
	ch := make(chan Response, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return errors.New("rex: connection to the server lost")
	}
	c.nextID++
	req.ID = c.nextID
	c.pending[req.ID] = ch
	c.mu.Unlock()
	b, err := json.Marshal(req)
	if err == nil {
		c.wmu.Lock()
		_, err = c.conn.Write(append(b, '\n'))
		c.wmu.Unlock()
	}
	if err != nil {
		c.mu.Lock()
		delete(c.pending, req.ID)
		c.mu.Unlock()
		return err
	}
	var res Response
	select {
	case res = <-ch:
	case <-time.After(15 * time.Second):
		c.mu.Lock()
		delete(c.pending, req.ID)
		c.mu.Unlock()
		return fmt.Errorf("rex: %s timed out", req.Op)
	}
	if res.Error != "" {
		return errors.New(res.Error)
	}
	if out != nil && len(res.Data) > 0 {
		return json.Unmarshal(res.Data, out)
	}
	return nil
}

func (c *Client) Hello() (h Hello, err error) {
	err = c.call(Request{Op: "hello"}, &h)
	return
}

func (c *Client) List() (s []SessionInfo, err error) {
	err = c.call(Request{Op: "list"}, &s)
	return
}

func (c *Client) Create(o CreateOptions) (s SessionInfo, err error) {
	err = c.call(Request{Op: "create", Create: &o}, &s)
	return
}

func (c *Client) Kill(sid string) error { return c.call(Request{Op: "kill", SID: sid}, nil) }

// Resize tells the session the size of its screen in cells, and of a cell
// in pixels when cellWidth and cellHeight are not zero.
func (c *Client) Resize(sid string, cols, rows, cellWidth, cellHeight int) error {
	return c.call(Request{Op: "resize", SID: sid, Cols: cols, Rows: rows, CellWidth: cellWidth, CellHeight: cellHeight}, nil)
}

// Get returns a document the app keeps with the server, nil for none.
func (c *Client) Get(key string) (json.RawMessage, error) {
	var v json.RawMessage
	err := c.call(Request{Op: "get", Key: key}, &v)
	return v, err
}

// Set keeps a document with the server, which writes it to disk.
func (c *Client) Set(key string, v json.RawMessage) error {
	return c.call(Request{Op: "set", Key: key, Value: v}, nil)
}

// Stat describes a file of the host's.
func (c *Client) Stat(path string) (fi FileInfo, err error) {
	err = c.call(Request{Op: "stat", Path: path}, &fi)
	return
}

// ReadDir lists a directory of the host's, by name.
func (c *Client) ReadDir(path string) (fis []FileInfo, err error) {
	err = c.call(Request{Op: "readDir", Path: path}, &fis)
	return
}

// ReadFile reads a file of the host's, of at most max bytes (0 for no
// cap): its bytes and the time they were modified.
func (c *Client) ReadFile(path string, max int64) (fd FileData, err error) {
	err = c.call(Request{Op: "readFile", Path: path, Max: max}, &fd)
	return
}

// WriteFile writes a file of the host's, refusing one modified since
// ifModTime when that is set, and tells the time written.
func (c *Client) WriteFile(path string, data []byte, ifModTime time.Time) (time.Time, error) {
	var fd FileData
	err := c.call(Request{Op: "writeFile", Path: path, Data: data, IfModTime: ifModTime}, &fd)
	return fd.ModTime, err
}

// Watch has the server tell of changes in a directory, as "changed"
// events; Unwatch stops that.
func (c *Client) Watch(path string) error { return c.call(Request{Op: "watch", Path: path}, nil) }

func (c *Client) Unwatch(path string) error { return c.call(Request{Op: "unwatch", Path: path}, nil) }

// GitStatus asks what git sees changed in the repository a directory is
// in; an empty status without git or a repository.
func (c *Client) GitStatus(path string) (st GitStatus, err error) {
	err = c.call(Request{Op: "gitStatus", Path: path}, &st)
	return
}

func (c *Client) Shutdown() error { return c.call(Request{Op: "shutdown"}, nil) }

func (c *Client) Close() error { return c.conn.Close() }

// Stream is a session attached to, for the Conn of a terminal that sets
// ResizeByConn: the server's frames are the terminal's output, and the
// session's size, which the server tells before the frames drawn at it,
// goes to OnSize, for the terminal to take. It attaches once it knows the
// size the terminal's view fits, from the first ResizePixels, or after a
// moment at the size it had last.
type Stream struct {
	c        *Client
	sid      string
	attachMu sync.Mutex // one attach at a time
	mu       sync.Mutex
	cond     *sync.Cond
	conn     net.Conn
	r        *bufio.Reader
	err      error
	closed   bool
	// fit is the size the terminal's view fits and theme its colors;
	// sent and sentTheme are what the server was told last.
	fit, sent        fit
	theme, sentTheme *terminal.Theme
	// cols and rows are the session's size, as the server told it last,
	// zero before; onSize takes it.
	cols, rows int
	onSize     func(cols, rows int)
	// onSurfaces takes the session's surfaces (OnSurfaces).
	onSurfaces func([]Surface)
	// onBlob takes the programs' blobs (OnBlob).
	onBlob func(id, mime string, data []byte)
	// left is what is left to read of the frame being read, by the
	// terminal's reading goroutine alone.
	left int

	// OnData, when set, is called with the size of the frames read, from
	// the reading goroutine.
	OnData func(n int)
}

// fit is a size a terminal's view fits: cells, and a cell in pixels,
// zero when unknown.
type fit struct{ cols, rows, cellWidth, cellHeight int }

// errFraming is a stream's error for what the server never sends.
var errFraming = errors.New("rex: bad frame")

// Stream returns a stream of the session sid, which attaches lazily.
func (c *Client) Stream(sid string, cols, rows int) *Stream {
	s := &Stream{c: c, sid: sid, fit: fit{cols: cols, rows: rows}}
	s.cond = sync.NewCond(&s.mu)
	time.AfterFunc(400*time.Millisecond, func() {
		s.attach()
		s.sync()
	})
	return s
}

// attach attaches to the session at the terminal's size, once.
func (s *Stream) attach() {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.mu.Lock()
	if s.conn != nil || s.err != nil || s.closed {
		s.mu.Unlock()
		return
	}
	f, theme := s.fit, s.theme
	s.mu.Unlock()
	conn, err := s.c.dial()
	if err == nil {
		b, _ := json.Marshal(Attach{Op: "attach", SID: s.sid, Cols: f.cols, Rows: f.rows, CellWidth: f.cellWidth, CellHeight: f.cellHeight, Theme: theme})
		if _, err = conn.Write(append(b, '\n')); err == nil {
			err = readOK(conn)
		}
		if err != nil {
			conn.Close()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if err == nil {
			conn.Close()
		}
		return
	}
	if err != nil {
		s.err = err
	} else {
		s.conn, s.r, s.sent, s.sentTheme = conn, bufio.NewReaderSize(conn, 64<<10), f, theme
	}
	s.cond.Broadcast()
}

// sync tells the server the terminal's size and colors, once attached,
// when it was told others.
func (s *Stream) sync() error {
	s.mu.Lock()
	if s.conn == nil {
		s.mu.Unlock()
		return nil
	}
	f, theme := s.fit, s.theme
	resize, retheme := f != s.sent, theme != s.sentTheme
	s.sent, s.sentTheme = f, theme
	s.mu.Unlock()
	if retheme {
		if err := s.c.call(Request{Op: "theme", SID: s.sid, Theme: theme}, nil); err != nil {
			return err
		}
	}
	if resize {
		return s.c.Resize(s.sid, f.cols, f.rows, f.cellWidth, f.cellHeight)
	}
	return nil
}

// readOK reads the server's answer to an attach, a byte at a time so as
// to read nothing past it.
func readOK(conn net.Conn) error {
	var line []byte
	b := make([]byte, 1)
	for {
		if _, err := conn.Read(b); err != nil {
			return err
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
		if len(line) > 4096 {
			return errors.New("rex: bad answer")
		}
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(line, &res); err != nil {
		return err
	}
	if !res.OK {
		return errors.New(res.Error)
	}
	return nil
}

func (s *Stream) wait() (net.Conn, *bufio.Reader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.conn == nil && s.err == nil && !s.closed {
		s.cond.Wait()
	}
	if s.closed {
		return nil, nil, io.EOF
	}
	return s.conn, s.r, s.err
}

// Read reads the frames, and gives OnSize the session's size where the
// server tells it.
func (s *Stream) Read(p []byte) (int, error) {
	_, r, err := s.wait()
	if err != nil {
		return 0, err
	}
	for s.left == 0 {
		var h [5]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return 0, err
		}
		n := int(binary.BigEndian.Uint32(h[1:]))
		switch h[0] {
		case msgFrame:
			s.left = n
		case msgSize:
			var b [4]byte
			if n != len(b) {
				return 0, errFraming
			}
			if _, err := io.ReadFull(r, b[:]); err != nil {
				return 0, err
			}
			cols, rows := int(binary.BigEndian.Uint16(b[:2])), int(binary.BigEndian.Uint16(b[2:]))
			s.mu.Lock()
			s.cols, s.rows = cols, rows
			onSize := s.onSize
			s.mu.Unlock()
			if onSize != nil {
				onSize(cols, rows)
			}
		case msgSurface:
			if n > 64<<20 {
				return 0, errFraming
			}
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return 0, err
			}
			var surfaces []Surface
			if err := json.Unmarshal(b, &surfaces); err != nil {
				return 0, errFraming
			}
			s.mu.Lock()
			onSurfaces := s.onSurfaces
			s.mu.Unlock()
			if onSurfaces != nil {
				onSurfaces(surfaces)
			}
		case msgBlob:
			if n < 65 || n > 32<<20 {
				return 0, errFraming
			}
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return 0, err
			}
			ml := int(b[64])
			if 65+ml > n {
				return 0, errFraming
			}
			s.mu.Lock()
			onBlob := s.onBlob
			s.mu.Unlock()
			if onBlob != nil {
				onBlob(string(b[:64]), string(b[65:65+ml]), b[65+ml:])
			}
		default:
			return 0, errFraming
		}
	}
	n, err := r.Read(p[:min(len(p), s.left)])
	s.left -= n
	if n > 0 && s.OnData != nil {
		s.OnData(n)
	}
	return n, err
}

func (s *Stream) Write(p []byte) (int, error) {
	conn, _, err := s.wait()
	if err != nil {
		return 0, err
	}
	return conn.Write(p)
}

// OnSize has the terminal take the session's size, where the server
// tells it, and now when it told one already: the terminal's Resize.
func (s *Stream) OnSize(f func(cols, rows int)) {
	s.mu.Lock()
	s.onSize = f
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	if cols > 0 && rows > 0 {
		f(cols, rows)
	}
}

// OnSurfaces has f take the session's surfaces of the Tern Surface
// Protocol, as the server sends them: all of them, whenever one changed,
// from the reading goroutine.
func (s *Stream) OnSurfaces(f func([]Surface)) {
	s.mu.Lock()
	s.onSurfaces = f
	s.mu.Unlock()
}

// OnBlob gives f the blobs of the session's programs, images their
// surfaces show by id, each once.
func (s *Stream) OnBlob(f func(id, mime string, data []byte)) {
	s.mu.Lock()
	s.onBlob = f
	s.mu.Unlock()
}

// TSPEvent sends the session's program an event of a surface: what the
// user did on it in the terminal.
func (s *Stream) TSPEvent(ev any) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return s.c.call(Request{Op: "tspEvent", SID: s.sid, Event: b}, nil)
}

// TSPSize tells the session the rows a surface of its program's needs,
// as the window laid it out, and those of the dock pinned under it.
func (s *Stream) TSPSize(sf string, rows, dock int) error {
	return s.c.call(Request{Op: "tspSize", SID: s.sid, Surface: sf, Rows: rows, Dock: dock}, nil)
}

// ResizePixels tells the session the size the terminal's view fits, in
// cells and its cells in pixels, attaching first: the server's terminal
// answers programs in them. The view tells it (Options.ResizeByConn).
func (s *Stream) ResizePixels(cols, rows, cellWidth, cellHeight int) error {
	s.mu.Lock()
	s.fit = fit{cols, rows, cellWidth, cellHeight}
	s.mu.Unlock()
	s.attach()
	return s.sync()
}

// UseTheme tells the session the terminal's colors, which the server's
// terminal answers programs that ask with, once attached.
func (s *Stream) UseTheme(theme *terminal.Theme) error {
	s.mu.Lock()
	s.theme = theme
	s.mu.Unlock()
	return s.sync()
}

// Clear has the server clear the session's screen, as the user asked in
// the terminal (Options.OnClear).
func (s *Stream) Clear() error {
	return s.c.call(Request{Op: "clear", SID: s.sid}, nil)
}

// Claim gives the session the size the terminal's view fits again, when
// the session has another, as the window in use does when another window
// gave the session its own.
func (s *Stream) Claim() error {
	s.mu.Lock()
	f := s.fit
	if s.conn == nil || f.cols <= 0 || f.rows <= 0 || f.cols == s.cols && f.rows == s.rows {
		s.mu.Unlock()
		return nil
	}
	// The server says the size next; until then, the stream does not
	// ask again.
	s.cols, s.rows, s.sent = f.cols, f.rows, f
	s.mu.Unlock()
	return s.c.Resize(s.sid, f.cols, f.rows, f.cellWidth, f.cellHeight)
}

// Close detaches; the session goes on.
func (s *Stream) Close() error {
	s.mu.Lock()
	s.closed = true
	conn := s.conn
	s.cond.Broadcast()
	s.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}
