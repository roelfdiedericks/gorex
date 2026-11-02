package main

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
	"gorex/internal/serialport"
)

// A serial host is a serial port of this machine, a switch's or a
// router's console behind it, which the user names: the window's session
// is one of a server of the host's own on this machine (binding.dir),
// which opens the port, keeps it open while the window is closed, and
// waits for it when it is unplugged. A port opens once, so the host has
// one tab of one pane.

// serialReason says why a serial host's window does not do what needs a
// machine's server: its files, more panes.
const serialReason = "Not on a serial port: its one session is the port's"

// newSerialKey returns the key of a new serial host: of its own, as the
// port it uses is a setting it may change.
func newSerialKey() string {
	var b [4]byte
	rand.Read(b[:])
	return "serial-" + hex.EncodeToString(b[:])
}

// config is the speed and framing of the serial host r, 115200 8N1 when
// it says none.
func (r savedRemote) config() serialport.Config {
	c := serialport.Config{Baud: r.Baud}
	if c.Baud <= 0 {
		c.Baud = 115200
	}
	if !serialport.ParseFraming(r.Framing, &c) {
		serialport.ParseFraming("8N1", &c)
	}
	return c
}

// serialBinding is the binding of the serial host r.
func serialBinding(r savedRemote) binding {
	c := r.config()
	return binding{serial: r.Key, port: filepath.Base(r.Port), line: strconv.Itoa(c.Baud) + " " + c.Framing()}
}

// serialOptions are what the session of the serial host r opens its port
// with, and its name, which another host's session finding the port open
// tells.
func serialOptions(r savedRemote) *rex.SerialOptions {
	c := r.config()
	return &rex.SerialOptions{
		Path: r.Port, VID: r.PortVID, PID: r.PortPID, Serial: r.PortSerial,
		Baud: c.Baud, DataBits: c.DataBits, Parity: c.Parity, StopBits: c.StopBits,
		Enter: r.Enter, Backspace: r.Backspace, Echo: r.Echo, Name: r.title(),
	}
}

// serialName is the name a serial host gets when the user gives it none:
// its port's, its adapter's and its speed, as "COM5 · FT232R USB UART ·
// 115200", the port first, which tells two adapters of a kind apart.
func serialName(p serialport.Port, baud int) string {
	return filepath.Base(p.Path) + " · " + p.Title() + " · " + strconv.Itoa(baud)
}

// findSerial returns the serial host of a name, the user's or its own,
// or of a key; a name in any case.
func findSerial(name string) (savedRemote, bool) {
	for _, r := range loadRemotes() {
		if r.Kind == kindSerial && (r.Key == name || strings.EqualFold(r.title(), name)) {
			return r, true
		}
	}
	return savedRemote{}, false
}

// openSaved switches the window to a saved host.
func (a *App) openSaved(r savedRemote) {
	if r.Kind == kindSerial {
		a.switchTo(serialBinding(r), false)
		return
	}
	a.openHost(r.Target)
}

// applySerial has the window's session, of the serial host r, take its
// settings as they are now: those the dialog changed, which a session
// made before had not.
func (a *App) applySerial(r savedRemote) {
	b := serialBinding(r)
	a.bind.port, a.bind.line = b.port, b.line
	serialLine = a.bind.line
	if a.client == nil {
		return
	}
	o, client := *serialOptions(r), a.client
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.SID != "" {
				go client.SerialSet(p.SID, o)
			}
		}
	}
}

// serialPane returns the pane of the serial host's session, for Send
// Break and Release Port; nil, and a note why, elsewhere.
func (a *App) serialPane() *Pane {
	if a.bind.serial == "" {
		a.toast(ui.Toast{ID: "serial", Title: "Not a serial port", Description: "Send Break and Release Port are for serial hosts' windows.", Timeout: 3 * time.Second})
		return nil
	}
	if t := a.tab(); t != nil && t.Focus != nil && t.Focus.SID != "" && a.client != nil {
		return t.Focus
	}
	return nil
}

// sendBreak sends a break on the serial port.
func (a *App) sendBreak() {
	if p := a.serialPane(); p != nil {
		client := a.client
		go client.SerialBreak(p.SID)
	}
}

// releasePort closes the serial port, which Enter in the pane opens again.
func (a *App) releasePort() {
	if p := a.serialPane(); p != nil {
		client := a.client
		go client.SerialRelease(p.SID)
	}
}

// onePane tells, with a note, that a serial host's window takes no more
// panes than the port's: it reports whether to refuse one.
func (a *App) onePane() bool {
	if a.bind.serial == "" || len(a.tabs) == 0 {
		return false
	}
	a.toast(ui.Toast{ID: "serial/pane", Title: "One session per serial port", Description: "A port opens once: its window has the one pane.", Timeout: 3 * time.Second})
	return true
}
