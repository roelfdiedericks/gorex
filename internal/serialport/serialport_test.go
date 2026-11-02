package serialport

import (
	"slices"
	"testing"
)

// ioreg's tree as macOS 15 prints it: a hub with a device under it whose
// serial port is two objects down, and a hub of no port.
const ioregSample = `+-o USB2 Hub@02100000  <class IOUSBHostDevice, id 0x1000008af, registered, matched, active, busy 0 (7 ms), retain 27>
  | {
  |   "USB Serial Number" = "7423J07"
  |   "USB Vendor Name" = "Apple"
  |   "USB Product Name" = "USB2 Hub"
  |   "idVendor" = 1452
  |   "idProduct" = 32779
  | }
  | 
  +-o AppleUSB20InternalHub@02100000  <class AppleUSB20InternalHub, id 0x1000008b2, registered, matched, active, busy 0 (0 ms), retain 15>
  | | {
  | |   "IOClass" = "AppleUSB20InternalHub"
  | | }
  | | 
  | +-o FT232R USB UART@02110000  <class IOUSBHostDevice, id 0x100000974, registered, matched, active, busy 0 (307 ms), retain 34>
  | |   | {
  | |   |   "idProduct" = 24577
  | |   |   "USB Product Name" = "FT232R USB UART"
  | |   |   "USB Serial Number" = "A10KXYZ"
  | |   |   "USB Vendor Name" = "FTDI"
  | |   |   "idVendor" = 1027
  | |   | }
  | |   | 
  | |   +-o AppleUSBFTDI@0  <class AppleUSBFTDI, id 0x100000980, registered, matched, active, busy 0, retain 8>
  | |     | {
  | |     |   "IOClass" = "AppleUSBFTDI"
  | |     | }
  | |     | 
  | |     +-o IOSerialBSDClient  <class IOSerialBSDClient, id 0x100000985, registered, matched, active, busy 0, retain 5>
  | |         {
  | |           "IOTTYBaseName" = "usbserial-"
  | |           "IOCalloutDevice" = "/dev/cu.usbserial-A10KXYZ"
  | |           "IODialinDevice" = "/dev/tty.usbserial-A10KXYZ"
  | |         }
  | |         
  | +-o AppleUSB20HubPort@02120000  <class AppleUSB20HubPort, id 0x1000008b6, registered, matched, active, busy 0 (0 ms), retain 12>
  |     {
  |       "port" = <02000000>
  |     }
  |     

+-o USB3 Gen2 Hub@02200000  <class IOUSBHostDevice, id 0x1000008a1, registered, matched, active, busy 0 (4 ms), retain 27>
  | {
  |   "USB Product Name" = "USB3 Gen2 Hub"
  |   "idVendor" = 1452
  | }
`

// A serial port in ioreg's tree takes what the nearest USB device above it
// tells, not a hub's further up; USB IDs come in decimal and go out as hex.
func TestParseIoreg(t *testing.T) {
	ports := parseIoreg(ioregSample)
	want := Port{Path: "/dev/cu.usbserial-A10KXYZ", Product: "FT232R USB UART", Maker: "FTDI", VID: "0403", PID: "6001", Serial: "A10KXYZ"}
	if len(ports) != 1 || ports[want.Path] != want {
		t.Errorf("parsed %+v, want only %+v", ports, want)
	}
}

// Ports are matched again after a replug by the adapter's serial number
// first, then the path (or Linux's lasting name), then vendor and product
// when only one adapter has them; ambiguity matches nothing.
func TestFind(t *testing.T) {
	ftdiA := Port{Path: "/dev/ttyUSB1", Stable: "/dev/serial/by-id/usb-FTDI_A", VID: "0403", PID: "6001", Serial: "A"}
	ftdiB := Port{Path: "/dev/ttyUSB0", VID: "0403", PID: "6001", Serial: "B"}
	ch340 := Port{Path: "/dev/ttyUSB2", VID: "1A86", PID: "7523"}
	builtin := Port{Path: "/dev/ttyS0", Driver: "serial"}
	ports := []Port{ftdiA, ftdiB, ch340, builtin}
	for _, c := range []struct {
		name string
		w    Want
		want string
	}{
		{"serial number, moved", Want{Path: "/dev/ttyUSB0", VID: "0403", PID: "6001", Serial: "A"}, "/dev/ttyUSB1"},
		{"lowercase ids", Want{Path: "/dev/ttyUSB9", VID: "0403", PID: "6001", Serial: "B"}, "/dev/ttyUSB0"},
		{"path", Want{Path: "/dev/ttyS0"}, "/dev/ttyS0"},
		{"lasting name", Want{Path: "/dev/serial/by-id/usb-FTDI_A"}, "/dev/ttyUSB1"},
		{"one of its ids", Want{Path: "/dev/ttyUSB7", VID: "1a86", PID: "7523"}, "/dev/ttyUSB2"},
		{"two of its ids", Want{Path: "/dev/ttyUSB7", VID: "0403", PID: "6001"}, ""},
		{"gone", Want{Path: "COM9"}, ""},
	} {
		p, ok := Find(c.w, ports)
		if ok != (c.want != "") || p.Path != c.want {
			t.Errorf("%s: found %q (%v), want %q", c.name, p.Path, ok, c.want)
		}
	}
}

// A port is named by its product and maker, else its chip, else its
// driver; its detail is the path, the IDs and the serial number.
func TestTitles(t *testing.T) {
	for _, c := range []struct {
		p             Port
		title, detail string
	}{
		{Port{Path: "COM5", Product: "FT232R USB UART", Maker: "FTDI", VID: "0403", PID: "6001", Serial: "A10KXYZ"}, "FT232R USB UART · FTDI", "COM5 · 0403:6001 · A10KXYZ"},
		{Port{Path: "COM3", VID: "1A86", PID: "7523"}, "WCH CH340", "COM3 · 1a86:7523"},
		{Port{Path: "/dev/ttyACM0", Product: "USB JTAG/serial debug unit", Maker: "Espressif", VID: "303A", PID: "1001"}, "USB JTAG/serial debug unit · Espressif", "/dev/ttyACM0 · 303a:1001"},
		{Port{Path: "/dev/ttyUSB0", Product: "Silicon Labs CP2102", Maker: "Silicon Labs"}, "Silicon Labs CP2102", "/dev/ttyUSB0"},
		{Port{Path: "/dev/ttyAMA0", Driver: "uart-pl011"}, "Built-in serial port (uart-pl011)", "/dev/ttyAMA0"},
		{Port{Path: "COM1", Product: "Communications Port"}, "Communications Port", "COM1"},
		{Port{Path: "/dev/cu.usbmodem1"}, "Serial port", "/dev/cu.usbmodem1"},
	} {
		if got := c.p.Title(); got != c.title {
			t.Errorf("Title of %+v = %q, want %q", c.p, got, c.title)
		}
		if got := c.p.Detail(); got != c.detail {
			t.Errorf("Detail of %+v = %q, want %q", c.p, got, c.detail)
		}
	}
}

// USB adapters list first, and ports by the numbers in their paths.
func TestSortPorts(t *testing.T) {
	ports := []Port{{Path: "COM10"}, {Path: "COM13", VID: "067B"}, {Path: "COM2"}, {Path: "COM1"}, {Path: "COM3", VID: "1A86"}}
	sortPorts(ports)
	var got []string
	for _, p := range ports {
		got = append(got, p.Path)
	}
	if want := []string{"COM3", "COM13", "COM1", "COM2", "COM10"}; !slices.Equal(got, want) {
		t.Errorf("order %q, want %q", got, want)
	}
}

// Framings read as consoles write them, and nothing else.
func TestParseFraming(t *testing.T) {
	for s, want := range map[string]*Config{
		"8N1":   {DataBits: 8, Parity: "N", StopBits: "1"},
		"7E1":   {DataBits: 7, Parity: "E", StopBits: "1"},
		"5S1.5": {DataBits: 5, Parity: "S", StopBits: "1.5"},
		"8N2":   {DataBits: 8, Parity: "N", StopBits: "2"},
		"9N1":   nil,
		"8X1":   nil,
		"8N3":   nil,
		"8N":    nil,
	} {
		var c Config
		ok := ParseFraming(s, &c)
		if ok != (want != nil) || ok && c != *want {
			t.Errorf("ParseFraming(%q) = %+v, %v", s, c, ok)
		}
		if ok && c.Framing() != s {
			t.Errorf("Framing of %q is %q", s, c.Framing())
		}
	}
}
