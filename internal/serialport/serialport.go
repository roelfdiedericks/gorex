// Package serialport finds this machine's serial ports, names them for
// people, finds a port again after it was unplugged, and opens one with
// a speed and framing, through go.bug.st/serial.
package serialport

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"go.bug.st/serial"
)

// Port is a serial port of this machine: Path opens it (/dev/ttyUSB0,
// /dev/cu.usbserial-A10KXYZ, COM5), Stable is a name of it that stays
// when it is plugged in again elsewhere (/dev/serial/by-id/… on Linux),
// "" where there is none. A USB adapter tells its maker and product,
// vendor and product IDs (four hex digits, upper case) and its serial
// number; a port of no USB device the driver it has, when known.
type Port struct {
	Path, Stable     string
	Product, Maker   string
	VID, PID, Serial string
	Driver           string
}

// Title names the port for people: its product and maker, else the chip
// its IDs are known for, else its driver.
func (p Port) Title() string {
	name := p.Product
	if name == "" {
		name = chips[p.VID+":"+p.PID]
	}
	if name == "" && p.Driver != "" {
		name = "Built-in serial port (" + p.Driver + ")"
	}
	if name == "" {
		name = "Serial port"
	}
	if p.Maker != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(p.Maker)) {
		name += " · " + p.Maker
	}
	return name
}

// Detail is the port as the system knows it: its path, the USB IDs and
// the serial number.
func (p Port) Detail() string {
	parts := []string{p.Path}
	if p.VID != "" {
		parts = append(parts, strings.ToLower(p.VID+":"+p.PID))
	}
	if p.Serial != "" {
		parts = append(parts, p.Serial)
	}
	return strings.Join(parts, " · ")
}

// usb reports whether the port is a USB adapter's.
func (p Port) usb() bool { return p.VID != "" }

// chips names the USB serial chips and boards by vendor and product ID,
// for adapters that tell no product of their own.
var chips = map[string]string{
	"0403:6001": "FTDI FT232R USB UART",
	"0403:6010": "FTDI FT2232H",
	"0403:6011": "FTDI FT4232H",
	"0403:6014": "FTDI FT232H",
	"0403:6015": "FTDI FT231X",
	"10C4:EA60": "Silicon Labs CP210x USB to UART",
	"10C4:EA70": "Silicon Labs CP2105",
	"1A86:7523": "WCH CH340",
	"1A86:5523": "WCH CH341",
	"1A86:55D3": "WCH CH343",
	"1A86:55D4": "WCH CH9102",
	"067B:2303": "Prolific PL2303",
	"067B:23A3": "Prolific PL2303GC",
	"303A:1001": "Espressif USB JTAG/serial",
	"303A:0002": "Espressif ESP32-S2",
	"2341:0043": "Arduino Uno",
	"2341:0042": "Arduino Mega 2560",
	"2341:8036": "Arduino Leonardo",
	"0483:5740": "STM32 Virtual COM Port",
	"2E8A:0005": "Raspberry Pi Pico",
	"2E8A:000A": "Raspberry Pi Pico",
	"05A6:0009": "Cisco USB console",
}

// List returns the serial ports of this machine, USB adapters first, by
// path within each.
func List() ([]Port, error) {
	ports, err := list()
	if err != nil {
		return nil, err
	}
	sortPorts(ports)
	return ports, nil
}

func sortPorts(ports []Port) {
	slices.SortStableFunc(ports, func(a, b Port) int {
		if a.usb() != b.usb() {
			if a.usb() {
				return -1
			}
			return 1
		}
		return comparePaths(a.Path, b.Path)
	})
}

// comparePaths orders paths by their text, but the numbers in them by
// value: COM2 before COM10.
func comparePaths(a, b string) int {
	for a != "" && b != "" {
		da, db := digits(a), digits(b)
		if da > 0 && db > 0 {
			na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
			if c := len(na) - len(nb); c != 0 {
				return c
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			a, b = a[da:], b[db:]
			continue
		}
		if a[0] != b[0] {
			return int(a[0]) - int(b[0])
		}
		a, b = a[1:], b[1:]
	}
	return len(a) - len(b)
}

func digits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// Want is the port a serial host uses: the path it was chosen at, and
// the USB IDs and serial number of its adapter, when it is one.
type Want struct {
	Path, VID, PID, Serial string
}

// Find finds the port of w among ports, as a USB adapter plugged in again
// may come back at another path: the adapter of its serial number, else
// the port at its path, else the one adapter of its vendor and product
// IDs, as adapters with no serial number can only be told apart so.
func Find(w Want, ports []Port) (Port, bool) {
	if w.Serial != "" {
		for _, p := range ports {
			if p.Serial == w.Serial && strings.EqualFold(p.VID, w.VID) && strings.EqualFold(p.PID, w.PID) {
				return p, true
			}
		}
	}
	for _, p := range ports {
		if p.Path == w.Path || p.Stable != "" && p.Stable == w.Path {
			return p, true
		}
	}
	if w.VID != "" {
		var found []Port
		for _, p := range ports {
			if strings.EqualFold(p.VID, w.VID) && strings.EqualFold(p.PID, w.PID) {
				found = append(found, p)
			}
		}
		if len(found) == 1 {
			return found[0], true
		}
	}
	return Port{}, false
}

// Config is how a port is opened: its speed in bits per second, and its
// framing: data bits (5 to 8), parity ("N", "E", "O", "M" or "S") and
// stop bits ("1", "1.5" or "2").
type Config struct {
	Baud     int
	DataBits int
	Parity   string
	StopBits string
}

// Framing is the config's framing as consoles write it, as 8N1.
func (c Config) Framing() string {
	return fmt.Sprintf("%d%s%s", c.DataBits, c.Parity, c.StopBits)
}

// Speeds are the speeds offered first, the console speeds of most
// network equipment (9600) and of boards and their bootloaders (115200)
// among them.
var Speeds = []int{9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600}

// Framings are the framings offered first.
var Framings = []string{"8N1", "7E1", "8E1", "8N2", "7O1"}

// ParseFraming reads a framing as consoles write it (8N1, 7E1, 8N1.5)
// into c.
func ParseFraming(s string, c *Config) bool {
	if len(s) < 3 || s[0] < '5' || s[0] > '8' || !strings.Contains("NEOMS", s[1:2]) {
		return false
	}
	switch s[2:] {
	case "1", "1.5", "2":
	default:
		return false
	}
	c.DataBits, c.Parity, c.StopBits = int(s[0]-'0'), s[1:2], s[2:]
	return true
}

// Open opens the port at path with c. Its errors say what went wrong as
// a person can act on it (OpenError).
func Open(path string, c Config) (serial.Port, error) {
	p, err := serial.Open(path, c.mode())
	if err != nil {
		return nil, classify(path, err)
	}
	return p, nil
}

// SetConfig changes the speed and framing of the open port p to c.
func SetConfig(p serial.Port, c Config) error { return p.SetMode(c.mode()) }

func (c Config) mode() *serial.Mode {
	mode := &serial.Mode{BaudRate: c.Baud, DataBits: c.DataBits}
	switch c.Parity {
	case "E":
		mode.Parity = serial.EvenParity
	case "O":
		mode.Parity = serial.OddParity
	case "M":
		mode.Parity = serial.MarkParity
	case "S":
		mode.Parity = serial.SpaceParity
	}
	switch c.StopBits {
	case "1.5":
		mode.StopBits = serial.OnePointFiveStopBits
	case "2":
		mode.StopBits = serial.TwoStopBits
	}
	return mode
}

// OpenError is why a port did not open: Missing when it is not there,
// Busy when another program has it, else neither, the message telling.
type OpenError struct {
	Missing, Busy bool
	msg           string
}

func (e *OpenError) Error() string { return e.msg }

func classify(path string, err error) error {
	var pe *serial.PortError
	code := serial.PortErrorCode(-1)
	if errors.As(err, &pe) {
		code = pe.Code()
	}
	switch {
	case code == serial.PortNotFound || errors.Is(err, fs.ErrNotExist):
		return &OpenError{Missing: true, msg: path + " is not there"}
	case code == serial.PortBusy:
		return &OpenError{Busy: true, msg: path + " is in use by another program"}
	case code == serial.PermissionDenied || errors.Is(err, fs.ErrPermission):
		return &OpenError{msg: "No permission to open " + path + permissionHint(path)}
	case code == serial.InvalidSpeed:
		return &OpenError{msg: path + " does not take that speed"}
	}
	return &OpenError{msg: "Can't open " + path + ": " + err.Error()}
}
