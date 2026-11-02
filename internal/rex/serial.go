package rex

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"go.bug.st/serial"

	"gorex/internal/serialport"
)

// serialTTY is the tty of a session of a serial port. It opens the port
// as it starts, and again when it was unplugged and is back, which it
// looks for every second, found by serialport.Find. Its notes, what
// happens to the port, as "[COM5 is gone: waiting for it]", read before
// what the port sends, so that the terminal shows them in their place.
// Released, the port is closed until Enter is typed; a port that failed
// to open is tried again then. Its session ends only when it is closed.
type serialTTY struct {
	o    SerialOptions
	cfg  serialport.Config
	want serialport.Want

	mu sync.Mutex
	// port is the port open, nil while there is none; path the one it is
	// or was last at; state as SessionInfo.PortState; opened tells that it
	// was open before; holder, while busy, the session that has it.
	port   serial.Port
	path   string
	state  string
	opened bool
	holder PortUse
	notes  [][]byte
	// suspended is set while an upgrade in place pauses the session: the
	// port is closed, for the server that replaces this one to open.
	suspended bool
	closed    bool
	// poke wakes read: a note, an open, the end.
	poke chan struct{}
	done chan struct{}
	// in carries what the open port's pump read, and its end; rest is
	// what read has yet to hand on of a chunk, and ack its pump's, told
	// when the chunk is all handed on. rest and ack are read's alone.
	in   chan chunk
	rest []byte
	ack  chan struct{}
}

// chunk is what a pump read of port: data, which the pump keeps till ack,
// or, nil, that the port ended.
type chunk struct {
	port serial.Port
	data []byte
	ack  chan struct{}
}

// serialBreak is how long a break lasts: long enough for the consoles that
// watch for one (Cisco's ROMMON, among others), as terminal programs send.
const serialBreak = 300 * time.Millisecond

// openSerial makes the tty of o and opens its port, or notes why it can't.
func openSerial(o SerialOptions) *serialTTY {
	t := &serialTTY{
		o:    o,
		cfg:  serialport.Config{Baud: o.Baud, DataBits: o.DataBits, Parity: o.Parity, StopBits: o.StopBits},
		want: serialport.Want{Path: o.Path, VID: o.VID, PID: o.PID, Serial: o.Serial},
		path: o.Path,
		poke: make(chan struct{}, 1),
		done: make(chan struct{}),
		in:   make(chan chunk),
	}
	t.open()
	return t
}

// note adds a line for the terminal, on a line of its own; t.mu is held.
func (t *serialTTY) note(s string) {
	t.notes = append(t.notes, []byte("\r\n\x1b[0;2m["+s+"]\x1b[0m\r\n"))
	t.wake()
}

func (t *serialTTY) wake() {
	select {
	case t.poke <- struct{}{}:
	default:
	}
}

// open opens the port: where its adapter is now, or at its path. Not
// there, the tty waits for it; open in another host's session here, it
// says whose, and Enter takes it over; failing otherwise, as another
// program having it, it says why and waits for Enter.
func (t *serialTTY) open() { t.openFor(0) }

// openFor opens the port as open does, trying again for as long as wait
// while it is busy: a port another session released may take the system
// a moment to let go.
func (t *serialTTY) openFor(wait time.Duration) {
	t.mu.Lock()
	path, want, cfg := t.o.Path, t.want, t.cfg
	t.mu.Unlock()
	if ports, err := serialport.List(); err == nil {
		if p, ok := serialport.Find(want, ports); ok {
			path = p.Path
		}
	}
	port, err := serialport.Open(path, cfg)
	var oe *serialport.OpenError
	for deadline := time.Now().Add(wait); errors.As(err, &oe) && oe.Busy && time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
		port, err = serialport.Open(path, cfg)
	}
	oe = nil
	var holder PortUse
	held := false
	if errors.As(err, &oe) && oe.Busy {
		holder, held = heldBy(path)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.suspended {
		if port != nil {
			port.Close()
		}
		return
	}
	if want != t.want || cfg != t.cfg {
		// The settings changed meanwhile: open as they are now.
		if port != nil {
			port.Close()
		}
		go t.open()
		return
	}
	if err != nil {
		if oe != nil && oe.Missing {
			if t.state != "waiting" {
				t.state = "waiting"
				t.note(t.path + " is not there: waiting for it")
			}
			return
		}
		if held {
			t.state, t.holder = "busy", holder
			t.note(path + " is open in “" + holder.Host + "”. Press Enter to take it over.")
			return
		}
		t.state = "failed"
		t.note(err.Error() + ". Press Enter to try again.")
		return
	}
	if t.opened && t.state == "waiting" && path == t.path {
		t.note(path + " is back")
	} else {
		t.note("Connected to " + path + " at " + strconv.Itoa(t.o.Baud) + " " + t.cfg.Framing())
	}
	t.port, t.path, t.state, t.opened = port, path, "open", true
	go t.pump(port)
}

// pump reads port while it is open, for read, which reads it so and the
// notes as they come; it tells read when the port ends, closed here or
// unplugged.
func (t *serialTTY) pump(port serial.Port) {
	buf := make([]byte, 4096)
	ack := make(chan struct{}, 1)
	for {
		n, _ := port.Read(buf)
		c := chunk{port: port}
		if n > 0 {
			c.data, c.ack = buf[:n], ack
		}
		select {
		case t.in <- c:
		case <-t.done:
			return
		}
		if n == 0 {
			return
		}
		select {
		case <-ack:
		case <-t.done:
			return
		}
	}
}

func (t *serialTTY) read(b []byte) (int, error) {
	for {
		t.mu.Lock()
		if len(t.notes) > 0 {
			n := copy(b, t.notes[0])
			if t.notes[0] = t.notes[0][n:]; len(t.notes[0]) == 0 {
				t.notes = t.notes[1:]
			}
			t.mu.Unlock()
			return n, nil
		}
		if t.closed {
			t.mu.Unlock()
			return 0, io.EOF
		}
		if t.suspended {
			t.mu.Unlock()
			return 0, os.ErrDeadlineExceeded
		}
		state := t.state
		t.mu.Unlock()
		if len(t.rest) > 0 {
			n := copy(b, t.rest)
			if t.rest = t.rest[n:]; len(t.rest) == 0 {
				t.ack <- struct{}{}
			}
			return n, nil
		}
		var retry <-chan time.Time
		if state == "waiting" {
			retry = time.After(time.Second)
		}
		select {
		case c := <-t.in:
			if c.data != nil {
				t.rest, t.ack = c.data, c.ack
				continue
			}
			// The port ended: closed here, as released, or unplugged.
			t.mu.Lock()
			if t.port == c.port {
				t.port = nil
				c.port.Close()
				if !t.closed && !t.suspended {
					t.state = "waiting"
					t.note(t.path + " is gone: waiting for it")
				}
			}
			t.mu.Unlock()
		case <-t.poke:
		case <-t.done:
		case <-retry:
			t.open()
		}
	}
}

// write sends b to the port, when it is open; the terminal's answers to
// the device's questions come this way.
func (t *serialTTY) write(b []byte) (int, error) {
	t.mu.Lock()
	port := t.port
	t.mu.Unlock()
	if port != nil {
		port.Write(b)
	}
	return len(b), nil
}

// input sends what is typed: Enter and Backspace as the options say, and
// shown when they echo it. A port released, or that failed to open, opens
// again on Enter; one another host's session has, Enter takes over.
func (t *serialTTY) input(p []byte) {
	t.mu.Lock()
	state, o, holder := t.state, t.o, t.holder
	t.mu.Unlock()
	if state == "released" || state == "failed" || state == "busy" {
		if bytes.ContainsAny(p, "\r\n") {
			t.mu.Lock()
			t.state = "opening"
			t.mu.Unlock()
			if state == "busy" {
				go t.takeOver(holder)
			} else {
				go t.open()
			}
		}
		return
	}
	out := p
	switch o.Enter {
	case "crlf":
		out = bytes.ReplaceAll(out, []byte("\r"), []byte("\r\n"))
	case "lf":
		out = bytes.ReplaceAll(out, []byte("\r"), []byte("\n"))
	}
	if o.Backspace == "bs" {
		out = bytes.ReplaceAll(out, []byte{0x7f}, []byte{0x08})
	}
	t.write(out)
	if o.Echo {
		shown := bytes.ReplaceAll(p, []byte("\r"), []byte("\r\n"))
		shown = bytes.ReplaceAll(shown, []byte{0x7f}, []byte("\b \b"))
		t.mu.Lock()
		t.notes = append(t.notes, shown)
		t.wake()
		t.mu.Unlock()
	}
}

// release closes the port, keeping the session, which Enter opens again.
func (t *serialTTY) release() {
	t.mu.Lock()
	port := t.port
	if port != nil {
		t.port, t.state = nil, "released"
		t.note(t.path + " released. Press Enter to connect again.")
	}
	t.mu.Unlock()
	if port != nil {
		port.Close()
	}
}

// configure takes the options the host's settings changed to: what typing
// sends at once; a speed or framing on the port as it is; another port by
// closing this one, for that one to open.
func (t *serialTTY) configure(o SerialOptions) {
	cfg := serialport.Config{Baud: o.Baud, DataBits: o.DataBits, Parity: o.Parity, StopBits: o.StopBits}
	want := serialport.Want{Path: o.Path, VID: o.VID, PID: o.PID, Serial: o.Serial}
	t.mu.Lock()
	newPort, newLine := want != t.want, cfg != t.cfg
	t.o, t.cfg, t.want = o, cfg, want
	port := t.port
	if newPort {
		t.path = o.Path
		if port != nil {
			// read sees the port end as no unplugging.
			t.port, t.state = nil, "opening"
		}
	}
	t.mu.Unlock()
	switch {
	case port == nil:
		// Closed, the port opens with the options when it opens.
	case newPort:
		port.Close()
		go t.open()
	case newLine:
		err := serialport.SetConfig(port, cfg)
		t.mu.Lock()
		if err != nil {
			t.note("Can't change " + t.path + " to " + strconv.Itoa(cfg.Baud) + " " + cfg.Framing() + ": " + err.Error())
		} else {
			t.note(t.path + " now at " + strconv.Itoa(cfg.Baud) + " " + cfg.Framing())
		}
		t.mu.Unlock()
	}
}

// options are the options the port has now.
func (t *serialTTY) options() SerialOptions {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.o
}

// sendBreak sends a break, when the port is open.
func (t *serialTTY) sendBreak() {
	t.mu.Lock()
	port := t.port
	t.mu.Unlock()
	if port != nil {
		port.Break(serialBreak)
	}
}

// takeOver has the session of another host here that has the port
// release it, which says so there, and opens it.
func (t *serialTTY) takeOver(u PortUse) {
	if err := ReleasePort(u.Dir, u.SID); err != nil {
		t.mu.Lock()
		t.state = "failed"
		t.note("Can't take " + u.Path + " from “" + u.Host + "”: " + err.Error() + ". Press Enter to try again.")
		t.mu.Unlock()
		return
	}
	t.openFor(3 * time.Second)
}

// status is the port's path and state, and its host's name, for
// SessionInfo.
func (t *serialTTY) status() (path, state, host string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.path, t.state, t.o.Name
}

// suspend closes the port for an upgrade in place; read stops.
func (t *serialTTY) suspend() {
	t.mu.Lock()
	port := t.port
	t.port, t.suspended = nil, true
	t.mu.Unlock()
	if port != nil {
		port.Close()
	}
	t.wake()
}

// unsuspend opens the port again after an upgrade that did not happen.
func (t *serialTTY) unsuspend() {
	t.mu.Lock()
	t.suspended = false
	t.mu.Unlock()
	t.open()
	t.wake()
}

func (t *serialTTY) close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	port := t.port
	t.port = nil
	close(t.done)
	t.mu.Unlock()
	if port != nil {
		port.Close()
	}
	return nil
}

func (t *serialTTY) resize(cols, rows, width, height int) error { return nil }
func (t *serialTTY) pid() int                                   { return 0 }
func (t *serialTTY) foreground() int                            { return 0 }
func (t *serialTTY) kill()                                      { t.close() }
func (t *serialTTY) hangup()                                    { t.close() }

// wait waits for the session's end: a serial port's ends only when the
// session is closed.
func (t *serialTTY) wait() int {
	<-t.done
	return 0
}
