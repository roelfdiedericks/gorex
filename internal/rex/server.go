//go:build darwin || linux || windows

package rex

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"
)

// Server owns the sessions.
type Server struct {
	mu       sync.Mutex
	sessions map[string]*session
	order    []string
	// docs are the documents the app keeps with the server, by key, as
	// "layout": read from <key>.json once, kept here, written on set.
	docs     map[string]json.RawMessage
	started  time.Time
	host     HostInfo
	hostDone chan struct{}
	ln       net.Listener
	lock     *os.File
	exe      string
	exeTime  time.Time
	controls int
	idleFrom time.Time
	quit     chan struct{}
	quitOnce sync.Once
	events   *events
}

// idleTimeout is how long a server with no sessions and no app waits
// before it exits.
const idleTimeout = 10 * time.Minute

// Serve runs the server until it is shut down: it returns an error at
// once when another server already runs.
func Serve() error { return serve("") }

// Resume runs the server that an upgrade in place made of the one before
// it, which left its socket, lock and sessions in the process, as the
// state file says.
func Resume(state string) error { return serve(state) }

func serve(state string) error {
	libraryBeside()
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sock := SocketPath()
	var (
		lock *os.File
		ln   net.Listener
		st   *upgradeState
		err  error
	)
	if state != "" {
		if st, err = readState(state); err != nil {
			return err
		}
		if lock, ln, err = st.adopt(); err != nil {
			return err
		}
	} else {
		if lock, err = os.OpenFile(filepath.Join(dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0o600); err != nil {
			return err
		}
		if err := lockFile(lock); err != nil {
			return errors.New("rex: a server already runs")
		}
		os.Remove(sock)
		if ln, err = net.Listen("unix", sock); err != nil {
			return err
		}
		os.Chmod(sock, 0o600)
	}
	defer lock.Close()
	s := &Server{
		sessions: map[string]*session{}, docs: map[string]json.RawMessage{}, started: time.Now(), ln: ln, lock: lock,
		hostDone: make(chan struct{}), idleFrom: time.Now(), quit: make(chan struct{}), events: newEvents(),
	}
	s.exe, s.exeTime = Executable()
	if st != nil {
		s.resume(st)
	}
	go func() {
		s.host = hostInfo()
		s.host.GOOS = runtime.GOOS
		close(s.hostDone)
	}()
	go s.watchIdle()
	go s.watchSessions()
	go func() {
		<-s.quit
		ln.Close()
	}()
	log.Printf("rex: serving on %s (pid %d, %d sessions)", sock, os.Getpid(), len(s.sessions))
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				s.mu.Lock()
				all := make([]*session, 0, len(s.sessions))
				for _, ss := range s.sessions {
					all = append(all, ss)
				}
				s.mu.Unlock()
				for _, ss := range all {
					ss.kill()
				}
				os.Remove(sock)
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) watchIdle() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		s.mu.Lock()
		idle := len(s.sessions) == 0 && s.controls == 0 && time.Since(s.idleFrom) > idleTimeout
		s.mu.Unlock()
		if idle {
			log.Print("rex: idle, exiting")
			s.shutdown()
			return
		}
	}
}

func (s *Server) shutdown() { s.quitOnce.Do(func() { close(s.quit) }) }

func (s *Server) handle(conn net.Conn) {
	r := bufio.NewReaderSize(conn, 64<<10)
	first, err := r.ReadBytes('\n')
	if err != nil {
		conn.Close()
		return
	}
	var a Attach
	if json.Unmarshal(first, &a) == nil && a.Op == "attach" {
		s.mu.Lock()
		ss := s.sessions[a.SID]
		s.mu.Unlock()
		if ss == nil {
			conn.Write([]byte(`{"error":"no such session"}` + "\n"))
			conn.Close()
			return
		}
		conn.Write([]byte(`{"ok":true}` + "\n"))
		// What the reader buffered past the first line is input.
		if n := r.Buffered(); n > 0 {
			b, _ := r.Peek(n)
			ss.input(b)
		}
		ss.attach(conn, a)
		return
	}
	s.control(conn, r, first)
}

// control serves a control connection: a request per line.
func (s *Server) control(conn net.Conn, r *bufio.Reader, first []byte) {
	s.mu.Lock()
	s.controls++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.controls--
		s.idleFrom = time.Now()
		s.mu.Unlock()
		conn.Close()
	}()
	var wmu sync.Mutex
	reply := func(res Response) {
		b, _ := json.Marshal(res)
		wmu.Lock()
		conn.Write(append(b, '\n'))
		wmu.Unlock()
	}
	ctl := &control{reply: reply}
	s.events.add(ctl)
	defer s.events.remove(ctl)
	line := first
	for {
		var req Request
		if err := json.Unmarshal(line, &req); err == nil {
			switch req.Op {
			case "kill":
				// Kills wait for the session to end: answer them aside.
				go func() { reply(s.request(req)) }()
			case "watch":
				reply(s.answer(req, nil, s.events.watch(ctl, req.Path)))
			case "unwatch":
				s.events.unwatch(ctl, req.Path)
				reply(Response{ID: req.ID})
			default:
				reply(s.request(req))
			}
		}
		var err error
		line, err = r.ReadBytes('\n')
		if err != nil {
			return
		}
	}
}

func (s *Server) request(req Request) Response {
	data, err := s.do(req)
	return s.answer(req, data, err)
}

// answer makes the response to req of data, or of err.
func (s *Server) answer(req Request, data any, err error) Response {
	res := Response{ID: req.ID}
	if err != nil {
		res.Error = err.Error()
	} else if data != nil {
		res.Data, _ = json.Marshal(data)
	}
	return res
}

func (s *Server) session(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ss := s.sessions[id]; ss != nil {
		return ss, nil
	}
	return nil, fmt.Errorf("no session %q", id)
}

func (s *Server) do(req Request) (any, error) {
	switch req.Op {
	case "hello":
		select {
		case <-s.hostDone:
		case <-time.After(3 * time.Second):
		}
		return Hello{Version: ProtocolVersion, PID: os.Getpid(), Started: s.started, Host: s.host, Exe: s.exe, ExeTime: s.exeTime}, nil
	case "list":
		s.mu.Lock()
		all := make([]*session, 0, len(s.order))
		for _, id := range s.order {
			all = append(all, s.sessions[id])
		}
		s.mu.Unlock()
		infos := make([]SessionInfo, len(all))
		for i, ss := range all {
			infos[i] = ss.info()
		}
		return infos, nil
	case "create":
		o := CreateOptions{}
		if req.Create != nil {
			o = *req.Create
		}
		id := newID()
		ss, err := newSession(id, o, s.events.send)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.sessions[id] = ss
		s.order = append(s.order, id)
		s.mu.Unlock()
		go func() {
			<-ss.done
			s.events.broadcast("sessions", "")
		}()
		s.events.broadcast("sessions", "")
		return ss.info(), nil
	case "kill":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, nil // already gone
		}
		ss.kill()
		s.mu.Lock()
		delete(s.sessions, req.SID)
		s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == req.SID })
		if len(s.sessions) == 0 {
			s.idleFrom = time.Now()
		}
		s.mu.Unlock()
		s.events.broadcast("sessions", "")
		return nil, nil
	case "resize":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.resize(req.Cols, req.Rows, req.CellWidth, req.CellHeight)
		return nil, nil
	case "theme":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		if req.Theme != nil {
			ss.vt.UseTheme(req.Theme)
		}
		return nil, nil
	case "motion":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.motion(req.Reduce)
		return nil, nil
	case "tspEvent":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.mu.Lock()
		b, err := ss.tsp.event(req.Event)
		exited := ss.exited
		ss.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if !exited {
			ss.p.write(b)
		}
		return nil, nil
	case "tspSize":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.tspSize(req.Surface, req.Rows, req.Dock)
		return nil, nil
	case "clear":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.clear()
		return nil, nil
	case "get":
		return s.get(req.Key)
	case "set":
		return nil, s.set(req.Key, req.Value)
	case "stat":
		return statFile(req.Path)
	case "readDir":
		return readDir(req.Path)
	case "readFile":
		return readFile(req.Path, req.Max)
	case "readGitFile":
		return readGitFile(req.Path, req.Rev, req.Blob, req.Max)
	case "writeFile":
		return writeFile(req.Path, req.Data, req.IfModTime)
	case "gitStatus":
		return gitStatus(req.Path)
	case "shutdown":
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.shutdown()
		}()
		return nil, nil
	case "upgrade":
		// The answer goes out before the server replaces itself; the
		// client then waits for the new server's hello.
		if err := canUpgrade(req.Path); err != nil {
			return nil, err
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			if err := s.upgrade(req.Path); err != nil {
				log.Printf("rex: upgrading to %s: %v", req.Path, err)
			}
		}()
		return nil, nil
	}
	return nil, fmt.Errorf("unknown op %q", req.Op)
}

// docKey tells whether a key names a document the server may keep.
func docKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, r := range key {
		if r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// get returns the document key, nil when there is none.
func (s *Server) get(key string) (any, error) {
	if !docKey(key) {
		return nil, fmt.Errorf("bad key %q", key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[key]
	if !ok {
		if b, err := os.ReadFile(filepath.Join(Dir(), key+".json")); err == nil && json.Valid(b) {
			d = b
		}
		s.docs[key] = d
	}
	if d == nil {
		return nil, nil
	}
	return d, nil
}

// set keeps the document key, on disk as <key>.json, whole or not at all.
func (s *Server) set(key string, v json.RawMessage) error {
	if !docKey(key) {
		return fmt.Errorf("bad key %q", key)
	}
	if !json.Valid(v) {
		return fmt.Errorf("%s is not JSON", key)
	}
	d := bytes.Clone(v)
	s.mu.Lock()
	s.docs[key] = d
	s.mu.Unlock()
	path := filepath.Join(Dir(), key+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, d, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Spawn starts a server in the background, as a process of its own that
// outlives the app: the executable run with -server.
func Spawn() error {
	exe, err := selfPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(Dir(), "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd := exec.Command(exe, "-server")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, logf, logf
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
