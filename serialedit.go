package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
	"gorex/internal/serialport"
)

// serialForm is a serial host's settings as the host dialog edits them,
// with the ports it offers: this machine's, listed again every second
// while the dialog shows them, so that one plugged in shows up.
type serialForm struct {
	// port is the one chosen; ports those of the latest list, and held
	// those other hosts' sessions have open.
	port    serialport.Port
	ports   []serialport.Port
	held    []rex.PortUse
	listErr string
	pick    bool
	stop    chan struct{}
	// speed is one of speedNames, custom the speed typed for Custom.
	speed, custom string
	// framing is a preset of serialport.Framings or Custom, and
	// dataBits, parity and stopBits its parts.
	framing, dataBits, parity, stopBits string
	enter, backspace                    int
	echo                                bool
}

const customChoice = "Custom"

var (
	dataBitsNames = []string{"5", "6", "7", "8"}
	parityNames   = []string{"None", "Even", "Odd", "Mark", "Space"}
	stopBitsNames = []string{"1", "1.5", "2"}
	// The choices of Enter and Backspace, as savedRemote keeps them.
	enterNames      = []string{"CR", "CR LF", "LF"}
	enterValues     = []string{"", "crlf", "lf"}
	backspaceNames  = []string{"DEL", "BS"}
	backspaceValues = []string{"", "bs"}
)

// speedNames are the speeds the dialog offers, and Custom.
func speedNames() []string {
	names := make([]string, 0, len(serialport.Speeds)+1)
	for _, s := range serialport.Speeds {
		names = append(names, strconv.Itoa(s))
	}
	return append(names, customChoice)
}

// setSerial fills the form from the serial host r.
func (s *serialForm) set(r savedRemote) {
	c := r.config()
	s.port = serialport.Port{Path: r.Port, VID: r.PortVID, PID: r.PortPID, Serial: r.PortSerial}
	s.speed = strconv.Itoa(c.Baud)
	if !slices.Contains(serialport.Speeds, c.Baud) {
		s.speed, s.custom = customChoice, strconv.Itoa(c.Baud)
	}
	s.setFraming(c)
	s.enter = max(slices.Index(enterValues, r.Enter), 0)
	s.backspace = max(slices.Index(backspaceValues, r.Backspace), 0)
	s.echo = r.Echo
}

// setFraming sets the framing's parts, and its preset, from c.
func (s *serialForm) setFraming(c serialport.Config) {
	s.dataBits = strconv.Itoa(c.DataBits)
	s.parity = parityNames[strings.Index("NEOMS", c.Parity)]
	s.stopBits = c.StopBits
	s.framing = c.Framing()
	if !slices.Contains(serialport.Framings, s.framing) {
		s.framing = customChoice
	}
}

// config is the speed and framing the form has, with ok false and a note
// when the speed typed is none.
func (s *serialForm) config() (c serialport.Config, err string) {
	speed := s.speed
	if speed == customChoice {
		speed = strings.TrimSpace(s.custom)
	}
	c.Baud, _ = strconv.Atoi(speed)
	if c.Baud <= 0 {
		return c, "Type the speed, in bits per second: 115200, or one of the port's own, as 74880."
	}
	c.DataBits, _ = strconv.Atoi(s.dataBits)
	c.Parity = s.parity[:1]
	c.StopBits = s.stopBits
	return c, ""
}

// watch lists the ports every second, and the hosts that hold them, till
// the form's dialog closes or leaves the serial port.
func (a *App) watchPorts(f *hostForm) {
	s := &f.serial
	s.stop = make(chan struct{})
	win, stop := a.win, s.stop
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			ports, err := serialport.List()
			skip := ""
			if f.old != "" {
				skip = hostDir(f.old)
			}
			held := rex.PortsInUse(filepath.Join(rex.Dir(), "hosts"), skip)
			win.Update(func() {
				if a.hostForm != f {
					return
				}
				s.ports, s.held, s.listErr = ports, held, ""
				if err != nil {
					s.listErr = "Can't list the serial ports: " + err.Error()
				}
				if p, ok := serialport.Find(want(s.port), ports); ok && s.port.Path != "" {
					// The port chosen, as the system now has it: its
					// path, and its names.
					s.port = p
				}
				a.changed()
			})
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
}

// stopPorts stops the form's listing of ports.
func (f *hostForm) stopPorts() {
	if f.serial.stop != nil {
		close(f.serial.stop)
		f.serial.stop = nil
	}
}

func want(p serialport.Port) serialport.Want {
	return serialport.Want{Path: p.Path, VID: p.VID, PID: p.PID, Serial: p.Serial}
}

// portNote says what keeps a port from the host: another's session that
// has it open, or its being unplugged.
func (s *serialForm) portNote(p serialport.Port) string {
	for _, u := range s.held {
		if strings.EqualFold(u.Path, p.Path) {
			return "in use by " + u.Host
		}
	}
	if s.ports != nil && !slices.ContainsFunc(s.ports, func(q serialport.Port) bool { return q.Path == p.Path }) {
		return "not plugged in"
	}
	return ""
}

// serialFields draws the serial host's part of the host dialog: the port,
// its speed and framing, and what typing sends.
func (a *App) serialFields(c *ui.Context, k *colors, f *hostForm, row func(string, func()), help func(string)) {
	s := &f.serial
	if s.stop == nil {
		a.watchPorts(f)
	}
	row("Name", func() {
		placeholder := "the port's, as Prolific PL2303 (COM13) · 115200"
		if s.port.Path != "" {
			c, _ := s.config()
			placeholder = serialName(s.port, c.Baud)
		}
		ui.TextInput(c, &f.label).FillWidth().FontSize(13).Placeholder(placeholder)
	})
	row("Port", func() { a.portPicker(c, k, s) })
	if s.listErr != "" {
		help(s.listErr)
	}
	row("Speed", func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Select(c, &s.speed, speedNames()).Width(140)
			if s.speed == customChoice {
				ui.TextInput(c, &s.custom).Width(120).FontSize(13).Placeholder("74880")
				ui.Text(c, "bits per second").FontSize(12).TextColor(k.textFaint)
			}
		})
	})
	row("Framing", func() {
		presets := append(slices.Clone(serialport.Framings), customChoice)
		if ui.Select(c, &s.framing, presets).MinWidth(140).Width(140).Changed() && s.framing != customChoice {
			var cf serialport.Config
			if serialport.ParseFraming(s.framing, &cf) {
				s.setFraming(cf)
			}
		}
	})
	row("Data, parity, stop", func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			// The theme's selects are 140 wide at least, too wide for
			// three in a row of short choices.
			part := func(v *string, names []string, w float32) {
				if ui.Select(c, v, names).MinWidth(w).Width(w).Changed() {
					cf, _ := s.config()
					s.setFraming(cf)
				}
			}
			part(&s.dataBits, dataBitsNames, 64)
			part(&s.parity, parityNames, 96)
			part(&s.stopBits, stopBitsNames, 72)
		})
	})
	row("Enter sends", func() { ui.Segmented(c, &s.enter, enterNames...) })
	row("Backspace sends", func() { ui.Segmented(c, &s.backspace, backspaceNames...) })
	row("Local echo", func() { ui.Switch(c, &s.echo) })
	help("Most consoles take 115200 8N1, Enter as CR and Backspace as DEL; local echo shows what you type on a device that doesn't echo it.")
}

// portPicker draws the port chosen, and on a click the ports to choose
// from: each its name above, and below the system's, and what keeps the
// port from the host.
func (a *App) portPicker(c *ui.Context, k *colors, s *serialForm) {
	lines := func(p serialport.Port, note string) {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, p.Title()).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…")
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.Detail()).FontSize(11.5).TextColor(k.textFaint).SingleLine().Shrink(0)
				if note != "" {
					ui.Text(c, note).FontSize(11.5).TextColor(k.attention).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				}
			})
		})
	}
	anchor := ui.Row(c).FillWidth().MinHeight(42).Padding(4, 10).Gap(8).Radius(8).Border(1, k.panelBorder).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("Port")
	if anchor.Hovered() || s.pick {
		anchor.Background(k.hover)
	}
	if anchor.Clicked() {
		s.pick = !s.pick
	}
	anchor.Children(func() {
		if s.port.Path == "" {
			ui.Text(c, "Choose a port").FontSize(13).TextColor(k.textMuted).Grow(1)
		} else {
			lines(s.port, s.portNote(s.port))
		}
		ui.Icon(c, icon("chevron-down")).Size(14, 14).TextColor(k.textMuted)
	})
	ui.PopoverBase(c, anchor, &s.pick, func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Width(400).Radius(10).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 12, 32, 0, k.shadowFocused)
		if len(s.ports) == 0 {
			ui.Text(c, "No serial ports: plug one in, and it shows here.").FontSize(12.5).TextColor(k.textFaint).Padding(14)
			return
		}
		ui.Scroll(c.Key("ports")).MaxHeight(320).Padding(6).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				for _, p := range s.ports {
					item := ui.Row(c.Key("port/"+p.Path)).Padding(5, 10).Radius(7).AlignItems(ui.Center).Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(p.Title() + " " + p.Path)
					if p.Path == s.port.Path {
						item.Background(k.panelSel)
					} else if item.Hovered() {
						item.Background(k.hover)
					}
					if item.Clicked() {
						s.port, s.pick = p, false
					}
					item.Children(func() { lines(p, s.portNote(p)) })
				}
			})
		})
	})
}

// saveSerial saves the serial host as the dialog has it, under a key of
// its own, and then, with connect, switches the window to it.
func (a *App) saveSerial(f *hostForm, connect bool) {
	s := &f.serial
	if s.port.Path == "" {
		f.err = "Choose the port."
		return
	}
	c, msg := s.config()
	if msg != "" {
		f.err = msg
		return
	}
	r := f.orig
	if r.Kind != kindSerial || r.Key == "" {
		// A host of ssh that becomes a port's is another host.
		r = savedRemote{Key: newSerialKey(), LastUsed: time.Now()}
	}
	r.Kind, r.Label = kindSerial, strings.TrimSpace(f.label)
	r.Name = serialName(s.port, c.Baud)
	r.Port, r.PortVID, r.PortPID, r.PortSerial = s.port.Path, s.port.VID, s.port.PID, s.port.Serial
	r.Baud, r.Framing = c.Baud, c.Framing()
	r.Enter, r.Backspace, r.Echo = enterValues[s.enter], backspaceValues[s.backspace], s.echo
	old := f.old
	if f.orig.Kind != kindSerial {
		// The ssh host it was stays as it was.
		old = ""
	}
	putRemote(old, r)
	a.remotes = loadRemotes()
	if a.bind.serial == r.Key {
		a.hostLabel = r.title()
		if !connect {
			a.applySerial(r)
		}
	}
	if connect {
		f.fromSettings = false
	}
	a.closeHostForm()
	if connect {
		b := serialBinding(r)
		a.switchTo(b, b.key() == a.bind.key())
	}
	a.changed()
}
