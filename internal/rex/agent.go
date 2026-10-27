//go:build darwin || linux || windows

package rex

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"sync"
	"time"
)

// A window that connects over ssh with its ssh agent forwarded (ssh's
// ForwardAgent) has the agent at a socket of that connection, which ends
// with it, while the server and its sessions go on. The server's
// sessions use an agent at a path that stays (AgentPath), which the
// server relays to the agent of a bridge connected now, the latest one
// to connect: a session's ssh and git find the agent of the window the
// user works from, after a reconnect too, and none while no window that
// forwards one is connected.
//
// A bridge tells the server its agent with an "agent" request (Path) on
// a control connection that it keeps open as long as it runs; the agent
// is the relay's as long as that connection is.

// AgentPath returns the path of the agent the server's sessions use: in
// Dir, unless that is too long a path for a socket, then in the
// temporary directory.
func AgentPath() string { return sockPath("agent.sock") }

// agentRelay relays connections to AgentPath to the agents of the
// bridges connected now. It listens from the first one on.
type agentRelay struct {
	mu     sync.Mutex
	ln     net.Listener
	agents []*string // the bridges' agents, the latest last
}

// add makes the bridge's agent at sock the one the relay goes to, until
// the returned remove; the relay starts listening, and the sessions
// started from then on use it.
func (r *agentRelay) add(sock string) (remove func(), err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln == nil {
		path := AgentPath()
		os.Remove(path)
		ln, err := net.Listen("unix", path)
		if err != nil {
			return nil, err
		}
		os.Chmod(path, 0o600)
		r.ln = ln
		// What sessions inherit (sessionEnv), and what an upgrade in
		// place passes on.
		os.Setenv("SSH_AUTH_SOCK", path)
		go r.serve(ln)
		log.Printf("rex: relaying the ssh agent at %s", path)
	}
	a := &sock
	r.agents = append(r.agents, a)
	return func() {
		r.mu.Lock()
		r.agents = slices.DeleteFunc(r.agents, func(o *string) bool { return o == a })
		r.mu.Unlock()
	}, nil
}

// close stops the relay.
func (r *agentRelay) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln != nil {
		r.ln.Close()
		os.Remove(AgentPath())
	}
}

func (r *agentRelay) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go r.relay(conn)
	}
}

// relay joins a session's connection to the agent of the latest bridge
// whose agent answers; with none, it closes it, as an agent that is not
// there.
func (r *agentRelay) relay(conn net.Conn) {
	r.mu.Lock()
	agents := make([]string, len(r.agents))
	for i, a := range r.agents {
		agents[len(agents)-1-i] = *a
	}
	r.mu.Unlock()
	for _, sock := range agents {
		agent, err := net.DialTimeout("unix", sock, 2*time.Second)
		if err != nil {
			continue
		}
		go func() {
			io.Copy(agent, conn)
			agent.Close()
		}()
		io.Copy(conn, agent)
		conn.Close()
		agent.Close()
		return
	}
	conn.Close()
}

// offerAgent tells the server of the ssh agent forwarded to this bridge,
// at sock, and keeps telling it, again after the server restarted or
// upgraded, until done closes. It returns once the server knows it the
// first time, or could not be told.
func offerAgent(sock string, dial Dialer, done <-chan struct{}) {
	first := make(chan struct{})
	var once sync.Once
	ready := func() { once.Do(func() { close(first) }) }
	go func() {
		defer ready()
		for {
			conn, err := dial()
			if err == nil {
				b, _ := json.Marshal(Request{ID: 1, Op: "agent", Path: sock})
				if _, err = conn.Write(append(b, '\n')); err == nil {
					br := bufio.NewReader(conn)
					// The answer, then nothing until the server ends.
					br.ReadBytes('\n')
					ready()
					go func() {
						<-done
						conn.Close()
					}()
					io.Copy(io.Discard, br)
				}
				conn.Close()
			}
			ready()
			select {
			case <-done:
				return
			case <-time.After(time.Second):
			}
		}
	}()
	<-first
}
