package main

import (
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"gorex/internal/remote"
	"gorex/internal/rex"
)

// binding is the server a window shows the sessions of: this machine's,
// or one reached over ssh.
type binding struct {
	remote bool
	target remote.Target
}

func (b binding) key() string {
	if !b.remote {
		return "local"
	}
	return "ssh/" + b.target.Key()
}

func (b binding) label() string {
	if !b.remote {
		return thisMachine()
	}
	return b.target.Spec
}

var (
	payloadOnce sync.Once
	payload     remote.Payload
	payloadErr  error
)

// loadPayload finds the Linux server among the app's resources, once.
func loadPayload() (remote.Payload, error) {
	payloadOnce.Do(func() {
		dir, err := mygo.App.Path(mygo.PathResources)
		if err != nil {
			payloadErr = err
			return
		}
		payload, payloadErr = remote.LoadPayload(dir)
	})
	return payload, payloadErr
}

// dialBinding connects to the server of b, telling prog how it goes. The
// link is nil for this machine's server.
func dialBinding(b binding, prog remote.Progress) (*rex.Client, *remote.Link, rex.Hello, error) {
	var client *rex.Client
	var link *remote.Link
	var err error
	if !b.remote {
		client, err = rex.Connect(rex.LocalDialer())
	} else {
		var p remote.Payload
		if p, err = loadPayload(); err != nil {
			return nil, nil, rex.Hello{}, err
		}
		if link, err = remote.Open(b.target, filepath.Base(rex.Dir()), p, prog); err != nil {
			return nil, nil, rex.Hello{}, err
		}
		if client, err = rex.Connect(link.Dial); err != nil {
			link.Close()
			return nil, nil, rex.Hello{}, err
		}
	}
	if err != nil {
		return nil, nil, rex.Hello{}, err
	}
	fail := func(err error) (*rex.Client, *remote.Link, rex.Hello, error) {
		client.Close()
		if link != nil {
			link.Close()
		}
		return nil, nil, rex.Hello{}, err
	}
	h, err := client.Hello()
	if err != nil {
		return fail(err)
	}
	if mygo.IsDev() && (!b.remote && staleServer(h) || b.remote && h.Exe != link.Exe()) {
		// A server a build before this one started runs that build's
		// code: start one of this build's, ending its sessions.
		prog.Status("Restarting the session server of " + b.label() + "…")
		prog.Log(fmt.Sprintf("the server runs another build (%s): restarting it", h.Exe))
		nc, err := client.Restart(h.PID)
		if err != nil {
			return fail(err)
		}
		client = nc
		if h, err = client.Hello(); err != nil {
			return fail(err)
		}
	}
	return client, link, h, nil
}

// connectAttempts is how many times a window tries to connect before it
// gives up, waiting retryDelays between them.
const connectAttempts = 5

var retryDelays = []time.Duration{1, 2, 4, 8}

// startConnecting connects the window to the server of b, in the
// background. For a host over ssh it shows the Connection Log until the
// connection is made, which closes it unless the user opened it.
func (a *App) startConnecting(win *mygo.Window, b binding, status string) {
	a.status, a.connDown = status, false
	if b.remote && !a.logOpen {
		a.logOpen, a.logAuto = true, true
	}
	go a.connectLoop(win, b, a.gen.Add(1))
}

// connectLoop connects the window to the server of b, trying a few times,
// unless the window moves on to another attempt (gen) meanwhile.
func (a *App) connectLoop(win *mygo.Window, b binding, gen int64) {
	current := func() bool { return a.gen.Load() == gen }
	prog := remote.Progress{
		Status: func(s string) {
			win.Update(func() {
				if current() {
					a.status = s
				}
			})
		},
		Log: func(s string) {
			if current() {
				a.logConn(s)
			}
		},
	}
	for try := 1; ; try++ {
		prog.Log(fmt.Sprintf("connecting to %s, attempt %d of %d", b.label(), try, connectAttempts))
		client, link, h, err := dialBinding(b, prog)
		if !current() {
			if err == nil {
				client.Close()
				if link != nil {
					link.Close()
				}
			}
			return
		}
		if err == nil {
			prog.Log("connected to " + b.label())
			win.Update(func() { a.attachBinding(win, b, gen, client, link, h) })
			return
		}
		prog.Log("failed: " + err.Error())
		if try == connectAttempts {
			win.Update(func() {
				if current() {
					a.status = fmt.Sprintf("Can't reach %s: %v", b.label(), err)
					a.connDown = true
				}
			})
			return
		}
		d := retryDelays[min(try-1, len(retryDelays)-1)] * time.Second
		win.Update(func() {
			if current() {
				a.status = fmt.Sprintf("Can't reach %s: %v — retrying in %ds", b.label(), err, int(d.Seconds()))
			}
		})
		time.Sleep(d)
		if !current() {
			return
		}
	}
}

// connLogLines is how many lines of the connection's log the window keeps.
const connLogLines = 500

// logConn adds a line to the connection's log, which the Connection Log
// shows, and to the app's.
func (a *App) logConn(s string) {
	log.Print(s)
	a.connLogMu.Lock()
	a.connLog = append(a.connLog, time.Now().Format("15:04:05 ")+s)
	if len(a.connLog) > connLogLines {
		a.connLog = a.connLog[len(a.connLog)-connLogLines:]
	}
	a.connLogMu.Unlock()
}

// connLogText returns the connection's log, one line per element.
func (a *App) connLogText() []string {
	a.connLogMu.Lock()
	defer a.connLogMu.Unlock()
	return slices.Clone(a.connLog)
}

// attachBinding shows the sessions of the server the window connected to.
func (a *App) attachBinding(win *mygo.Window, b binding, gen int64, client *rex.Client, link *remote.Link, h rex.Hello) {
	if a.gen.Load() != gen || a.win != win {
		client.Close()
		if link != nil {
			link.Close()
		}
		return
	}
	a.detachAll()
	a.reset()
	a.client, a.link, a.hello = client, link, h
	setHost(h, b.remote)
	a.status, a.connDown = "", false
	if a.logAuto {
		a.logOpen, a.logAuto = false, false
	}
	if h.Version != rex.ProtocolVersion {
		a.err = "The session server is of another version of GoRex: quit and end all sessions to restart it."
	}
	if !a.restore() {
		a.newTab(hostHome)
	}
	a.changed()
	go a.poll(win, client)
	if b.remote {
		touchRemote(savedRemote{Key: b.target.Key(), Target: b.target.Spec, Name: h.Host.Name, LastUsed: time.Now()})
	}
}

// detachAll detaches the window from its sessions, which go on, and from
// the server.
func (a *App) detachAll() {
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			p.closed = true
			if p.term != nil {
				p.term.Close() // detaches: the session goes on
			}
		}
	}
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
	if a.link != nil {
		a.link.Close()
		a.link = nil
	}
}
