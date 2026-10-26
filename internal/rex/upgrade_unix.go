//go:build darwin || linux

package rex

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"golang.org/x/sys/unix"
)

// upgradeState is what a server that replaces itself in place leaves the
// server it becomes, in the same process: the descriptors of its socket
// and lock, and its sessions, whose terminals stay open across the exec
// and whose processes stay its children.
type upgradeState struct {
	Listener int            `json:"listener"`
	Lock     int            `json:"lock"`
	Sessions []sessionState `json:"sessions"`
}

type sessionState struct {
	ID         string    `json:"id"`
	Shell      string    `json:"shell"`
	Created    time.Time `json:"created"`
	PID        int       `json:"pid"`
	FD         int       `json:"fd"` // the terminal's master; -1 once it ended
	Keep       int       `json:"keep"`
	Images     int64     `json:"images,omitempty"`
	Cols       int       `json:"cols"`
	Rows       int       `json:"rows"`
	CellWidth  int       `json:"cellWidth,omitempty"`
	CellHeight int       `json:"cellHeight,omitempty"`
	// Paste tells that a program takes paste events (mode 5522).
	Paste bool `json:"paste,omitempty"`
	// TSP tells that the session answers the Tern Surface Protocol;
	// Surfaces are its program's live surfaces, with their documents,
	// Kept those kept after their close, NextImage the last anchor's
	// image id and Blobs the program's blobs, oldest first, which it
	// sent once and does not again. Anchors are the rows of the
	// surfaces' anchors, by image id, from the screen's top (negative
	// in the scrollback), which the snapshot carries no image of.
	TSP        bool           `json:"tsp,omitempty"`
	Surfaces   []*Surface     `json:"surfaces,omitempty"`
	Kept       []*Surface     `json:"kept,omitempty"`
	Anchors    map[uint32]int `json:"anchors,omitempty"`
	NextImage  uint32         `json:"nextImage,omitempty"`
	Blobs      []*TSPBlob     `json:"blobs,omitempty"`
	Exited     bool           `json:"exited,omitempty"`
	Code       int            `json:"code,omitempty"`
	Output     uint64         `json:"output"`
	Bells      int64          `json:"bells,omitempty"`
	LastOutput time.Time      `json:"lastOutput"`
	LastInput  time.Time      `json:"lastInput"`
	// Screen is the title and the snapshot of the screen and scrollback,
	// which the new server's terminal starts from.
	Screen []byte `json:"screen,omitempty"`
}

func statePath() string { return filepath.Join(Dir(), "upgrade.json") }

// canUpgrade checks that exe is a program the server can become.
func canUpgrade(exe string) error {
	st, err := os.Stat(exe)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not a program", exe)
	}
	return nil
}

// upgrade replaces the server with exe in the same process: the sessions'
// terminals and processes, the socket and the lock stay; the screens go
// in the state file, which exe -server -resume reads. It returns only when
// it could not, the server going on as it was.
func (s *Server) upgrade(exe string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]*session, 0, len(s.order))
	for _, id := range s.order {
		all = append(all, s.sessions[id])
	}
	// Every reader stops, leaving what the terminals have unread to the
	// server that replaces this one.
	paused := make([]bool, len(all))
	defer func() {
		for i, ss := range all {
			ss.unpause(paused[i])
		}
	}()
	for i, ss := range all {
		var err error
		if paused[i], err = ss.pause(); err != nil {
			return err
		}
	}
	lnf, err := s.ln.(*net.UnixListener).File()
	if err != nil {
		return err
	}
	defer lnf.Close()
	st := upgradeState{Listener: -1, Lock: -1}
	keep := []*os.File{lnf, s.lock}
	for i, ss := range all {
		ss.mu.Lock()
		defer ss.mu.Unlock()
		ses := sessionState{
			ID: ss.id, Shell: ss.shell, Created: ss.created, PID: ss.p.pid(), FD: -1, Keep: ss.keep, Images: ss.images,
			Cols: ss.cols, Rows: ss.rows, CellWidth: ss.cellWidth, CellHeight: ss.cellHeight, Paste: ss.paste.on, TSP: ss.tsp.on,
			Surfaces: ss.tsp.live(), Kept: ss.tsp.kept, NextImage: ss.tsp.nextImage, Blobs: ss.tsp.blobList(),
			Exited: ss.exited || !paused[i], Code: ss.code, Output: ss.output,
			Bells: ss.bells.Load(), LastOutput: ss.lastOutput, LastInput: ss.lastInput,
		}
		if !ses.Exited {
			ses.Screen = ss.vt.Snapshot()
			for _, pl := range ss.vt.AppPlacements() {
				if ses.Anchors == nil {
					ses.Anchors = map[uint32]int{}
				}
				ses.Anchors[pl.Image] = pl.Row
			}
			if title := ss.vt.Title(); title != "" {
				ses.Screen = append([]byte("\x1b]2;"+title+"\x07"), ses.Screen...)
			}
			keep = append(keep, ss.p.master)
		}
		st.Sessions = append(st.Sessions, ses)
	}
	// Nothing forks while the descriptors lose close-on-exec, so that no
	// program the server starts meanwhile inherits them.
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()
	fds := make([]int, len(keep))
	for i, f := range keep {
		if err := withFD(f, func(fd int) error {
			fds[i] = fd
			_, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0)
			return err
		}); err != nil {
			return err
		}
		defer syscall.CloseOnExec(fds[i])
	}
	st.Listener, st.Lock = fds[0], fds[1]
	live := fds[2:]
	for i := range st.Sessions {
		if !st.Sessions[i].Exited {
			st.Sessions[i].FD, live = live[0], live[1:]
		}
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	path := statePath()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	log.Printf("rex: upgrading in place to %s (%d sessions)", exe, len(st.Sessions))
	err = syscall.Exec(exe, []string{exe, "-server", "-resume", path}, upgradeEnv(exe))
	os.Remove(path)
	return err
}

// upgradeEnv is the server's environment for exe: a server a bridge
// started loads the libghostty-vt beside its executable, which is exe's
// now.
func upgradeEnv(exe string) []string {
	env := os.Environ()
	for i, kv := range env {
		if v, ok := strings.CutPrefix(kv, "MYGO_GHOSTTY_VT="); ok {
			lib := filepath.Join(filepath.Dir(exe), filepath.Base(v))
			if _, err := os.Stat(lib); err == nil {
				env[i] = "MYGO_GHOSTTY_VT=" + lib
			}
		}
	}
	return env
}

// pause stops the session's reader and reports whether it waits to go on;
// a session whose reader is done has nothing more to read.
func (s *session) pause() (bool, error) {
	s.pausing.Store(true)
	if err := s.p.master.SetReadDeadline(time.Now()); err != nil {
		s.pausing.Store(false)
		select {
		case <-s.readDone:
			return false, nil
		default:
			return false, err
		}
	}
	select {
	case <-s.pauseAck:
		return true, nil
	case <-s.readDone:
		return false, nil
	}
}

// unpause has the reader of a session that paused go on.
func (s *session) unpause(paused bool) {
	if s.p.master != nil {
		s.p.master.SetReadDeadline(time.Time{})
	}
	s.pausing.Store(false)
	if paused {
		s.pauseEnd <- struct{}{}
	}
}

// readState reads the state file an upgrade left, and removes it.
func readState(path string) (*upgradeState, error) {
	b, err := os.ReadFile(path)
	os.Remove(path)
	if err != nil {
		return nil, err
	}
	var st upgradeState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("rex: %s: %w", path, err)
	}
	return &st, nil
}

// adopt takes over the lock and the socket of the server this one
// replaced. Clients that connected meanwhile wait in the socket's queue.
func (st *upgradeState) adopt() (*os.File, net.Listener, error) {
	syscall.CloseOnExec(st.Lock)
	syscall.CloseOnExec(st.Listener)
	lock := os.NewFile(uintptr(st.Lock), "server.lock")
	lnf := os.NewFile(uintptr(st.Listener), "server.sock")
	ln, err := net.FileListener(lnf)
	lnf.Close()
	if err != nil {
		lock.Close()
		return nil, nil, err
	}
	return lock, ln, nil
}

// resume takes the sessions of the server this one replaced.
func (s *Server) resume(st *upgradeState) {
	for _, ses := range st.Sessions {
		ss, err := resumeSession(ses, s.events.send)
		if err != nil {
			log.Printf("rex: resuming session %s: %v", ses.ID, err)
			continue
		}
		s.sessions[ss.id] = ss
		s.order = append(s.order, ss.id)
		go func() {
			<-ss.done
			s.events.broadcast("sessions", "")
		}()
	}
}

func resumeSession(st sessionState, emit func(Response)) (*session, error) {
	s := &session{
		id: st.ID, shell: st.Shell, created: st.Created, p: &pty{}, keep: st.Keep, images: st.Images, emit: emit,
		cols: st.Cols, rows: st.Rows, cellWidth: st.CellWidth, cellHeight: st.CellHeight, paste: pasteTracker{on: st.Paste},
		tsp:    tspTracker{on: st.TSP, cols: st.Cols, cellW: st.CellWidth, cellH: st.CellHeight, nextImage: st.NextImage},
		output: st.Output, lastOutput: st.LastOutput, lastInput: st.LastInput,
	}
	s.bells.Store(st.Bells)
	if st.Exited {
		// An ended session keeps its exit code, as it did.
		pr, pw := io.Pipe()
		pw.Close()
		vt, err := terminal.New(terminal.Options{Conn: vtConn{pr, s.p}, Scrollback: s.keep})
		if err != nil {
			return nil, err
		}
		vt.Close()
		s.vt, s.exited, s.code = vt, true, st.Code
		s.clients = map[*attached]struct{}{}
		s.done, s.readDone = make(chan struct{}), make(chan struct{})
		close(s.done)
		close(s.readDone)
		return s, nil
	}
	syscall.CloseOnExec(st.FD)
	proc, err := os.FindProcess(st.PID)
	if err != nil {
		unix.Close(st.FD)
		return nil, err
	}
	s.p = &pty{master: os.NewFile(uintptr(st.FD), "/dev/ptmx"), proc: proc}
	// The snapshot carries no anchors: those in the scrollback go into
	// it, at the starts of their rows, and those on the screen are
	// placed at their rows after it. A state without the anchors' rows
	// has the live surfaces' placed from the cursor (reanchor).
	screen, onScreen := placeAnchors(st.Screen, st.Anchors, st.Rows)
	if err := s.start(screen); err != nil {
		s.p.hangup()
		return nil, err
	}
	if len(st.Surfaces) > 0 || len(st.Kept) > 0 || len(st.Blobs) > 0 {
		s.mu.Lock()
		for _, b := range st.Blobs {
			s.tsp.addBlob(b.Mime, b.Data)
		}
		s.tsp.restore(st.Surfaces)
		s.tsp.kept = st.Kept
		if st.Anchors != nil {
			s.vt.Feed([]byte(onScreen))
		} else if len(st.Surfaces) > 0 {
			s.tsp.reanchor()
			s.vt.Feed(s.tsp.inject)
			s.tsp.inject = nil
		}
		s.tspPad()
		s.mu.Unlock()
	}
	return s, nil
}
