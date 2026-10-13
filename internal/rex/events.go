package rex

import (
	"errors"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Events the server sends on its own, to every control connection: a
// watched directory that changed, and sessions that changed. See
// Response.

// maxWatches caps the directories a server watches.
const maxWatches = 512

// control is a control connection, as the server's events see it.
type control struct {
	reply   func(Response)
	watches map[string]bool // directories this connection watches
}

// events is the server's event side: the connections to tell, the
// watcher and its refcounts, and what the sessions looked like last.
type events struct {
	mu      sync.Mutex
	conns   map[*control]bool
	watcher *fsnotify.Watcher
	refs    map[string]int // watchers of each directory, over connections
	dirty   map[string]bool
	timer   *time.Timer
	// last is what each session's info was when the sessions were
	// compared last, by what the app shows of it.
	last map[string]sessionShape
}

// sessionShape is what of a session the app shows, compared to tell a
// change.
type sessionShape struct {
	program, dir, title string
	idle, exited        bool
	attached            int
}

func shapeOf(in SessionInfo) sessionShape {
	return sessionShape{program: in.Program, dir: in.Dir, title: in.Title, idle: in.Idle, exited: in.Exited, attached: in.Attached}
}

func newEvents() *events {
	return &events{conns: map[*control]bool{}, refs: map[string]int{}, dirty: map[string]bool{}, last: map[string]sessionShape{}}
}

// broadcast sends an event to every control connection.
func (e *events) broadcast(event, path string) {
	e.mu.Lock()
	conns := make([]*control, 0, len(e.conns))
	for c := range e.conns {
		conns = append(conns, c)
	}
	e.mu.Unlock()
	res := Response{Event: event, Path: path}
	for _, c := range conns {
		c.reply(res)
	}
}

// add registers a connection; remove drops it and its watches.
func (e *events) add(c *control) {
	e.mu.Lock()
	e.conns[c] = true
	e.mu.Unlock()
}

func (e *events) remove(c *control) {
	e.mu.Lock()
	delete(e.conns, c)
	dirs := c.watches
	c.watches = nil
	e.mu.Unlock()
	for dir := range dirs {
		e.unwatch(c, dir)
	}
}

// watch has the connection watch a directory, which the server watches
// once however many connections do.
func (e *events) watch(c *control, dir string) error {
	dir, err := filePath(dir)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if c.watches[dir] {
		return nil
	}
	if e.refs[dir] == 0 {
		if len(e.refs) >= maxWatches {
			return errors.New("too many directories watched")
		}
		if e.watcher == nil {
			w, err := fsnotify.NewWatcher()
			if err != nil {
				return err
			}
			e.watcher = w
			go e.run(w)
		}
		if err := e.watcher.Add(dir); err != nil {
			return err
		}
	}
	e.refs[dir]++
	if c.watches == nil {
		c.watches = map[string]bool{}
	}
	c.watches[dir] = true
	return nil
}

func (e *events) unwatch(c *control, dir string) {
	dir, err := filePath(dir)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if c.watches != nil && !c.watches[dir] {
		return
	}
	delete(c.watches, dir)
	if e.refs[dir] <= 1 {
		delete(e.refs, dir)
		if e.watcher != nil {
			e.watcher.Remove(dir)
		}
		return
	}
	e.refs[dir]--
}

// run takes the watcher's events, marking directories dirty, and tells
// of them a moment later, once per directory however many files changed.
func (e *events) run(w *fsnotify.Watcher) {
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			dir := filepath.Dir(ev.Name)
			e.mu.Lock()
			if e.refs[ev.Name] > 0 && ev.Has(fsnotify.Remove|fsnotify.Rename) {
				// A watched directory itself went: tell of it as of its
				// parent.
				e.dirty[ev.Name] = true
			}
			e.dirty[dir] = true
			if e.timer == nil {
				e.timer = time.AfterFunc(150*time.Millisecond, e.flush)
			}
			e.mu.Unlock()
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("rex: watching: %v", err)
		}
	}
}

// flush tells of the directories marked dirty.
func (e *events) flush() {
	e.mu.Lock()
	dirs := e.dirty
	e.dirty = map[string]bool{}
	e.timer = nil
	e.mu.Unlock()
	for dir := range dirs {
		e.broadcast("changed", dir)
	}
}

// compare tells of the sessions when what the app shows of them changed
// since the last comparison.
func (e *events) compare(infos []SessionInfo) {
	e.mu.Lock()
	changed := len(infos) != len(e.last)
	now := make(map[string]sessionShape, len(infos))
	for _, in := range infos {
		sh := shapeOf(in)
		now[in.ID] = sh
		if was, ok := e.last[in.ID]; !ok || was != sh {
			changed = true
		}
	}
	e.last = now
	e.mu.Unlock()
	if changed {
		e.broadcast("sessions", "")
	}
}

// watchSessions compares the sessions every half second while a control
// connection is there to tell.
func (s *Server) watchSessions() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-tick.C:
		}
		s.mu.Lock()
		n := s.controls
		all := make([]*session, 0, len(s.order))
		for _, id := range s.order {
			all = append(all, s.sessions[id])
		}
		s.mu.Unlock()
		if n == 0 {
			continue
		}
		infos := make([]SessionInfo, len(all))
		for i, ss := range all {
			infos[i] = ss.info()
		}
		s.events.compare(infos)
	}
}
