package serialport

import (
	"os"
	"path/filepath"
	"testing"
)

// sysfs as Linux lays it out for a USB serial converter (usb-serial), a
// CDC-ACM device (usb), a built-in UART, a legacy 8250 port with no
// hardware, and a virtual terminal: the ttys a device is behind list, but
// the 8250's placeholder, with their USB devices' IDs and strings and
// their lasting names.
func TestListSysfs(t *testing.T) {
	root := t.TempDir()
	sys, dev, byID := filepath.Join(root, "class/tty"), filepath.Join(root, "dev"), filepath.Join(root, "dev/serial/by-id")
	devices := filepath.Join(root, "devices")
	mk := func(path string) string {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write := func(path, s string) {
		if err := os.WriteFile(path, []byte(s+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, path string) {
		mk(filepath.Dir(path))
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	bus := map[string]string{}
	for _, b := range []string{"usb-serial", "usb", "platform", "amba"} {
		bus[b] = mk(filepath.Join(root, "bus", b))
	}
	drivers := map[string]string{}
	for _, d := range []string{"ftdi_sio", "cdc_acm", "serial8250", "uart-pl011"} {
		drivers[d] = mk(filepath.Join(root, "drivers", d))
	}
	tty := func(name, device, subsystem, driver string) {
		mk(device)
		link(bus[subsystem], filepath.Join(device, "subsystem"))
		link(drivers[driver], filepath.Join(device, "driver"))
		link(device, filepath.Join(sys, name, "device"))
		write(filepath.Join(mk(dev), name), "")
	}
	usb := func(dir, vid, pid, serial, maker, product string) {
		write(filepath.Join(dir, "idVendor"), vid)
		write(filepath.Join(dir, "idProduct"), pid)
		write(filepath.Join(dir, "serial"), serial)
		write(filepath.Join(dir, "manufacturer"), maker)
		write(filepath.Join(dir, "product"), product)
	}

	ftdi := mk(filepath.Join(devices, "usb1/1-2"))
	usb(ftdi, "0403", "6001", "A10KXYZ", "FTDI", "FT232R USB UART")
	tty("ttyUSB0", filepath.Join(ftdi, "1-2:1.0/ttyUSB0"), "usb-serial", "ftdi_sio")
	link(filepath.Join(dev, "ttyUSB0"), filepath.Join(byID, "usb-FTDI_FT232R_USB_UART_A10KXYZ-if00-port0"))

	esp := mk(filepath.Join(devices, "usb1/1-3"))
	usb(esp, "303a", "1001", "F4:12:FA", "Espressif", "USB JTAG/serial debug unit")
	tty("ttyACM0", filepath.Join(esp, "1-3:1.0"), "usb", "cdc_acm")

	tty("ttyAMA0", filepath.Join(devices, "platform/fe201000.serial"), "amba", "uart-pl011")
	tty("ttyS3", filepath.Join(devices, "platform/serial8250"), "platform", "serial8250")
	mk(filepath.Join(sys, "tty1")) // a virtual terminal, of no device
	write(filepath.Join(dev, "tty1"), "")

	ports, err := listSysfs(sys, dev, byID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Port{
		filepath.Join(dev, "ttyUSB0"): {Path: filepath.Join(dev, "ttyUSB0"), Stable: filepath.Join(byID, "usb-FTDI_FT232R_USB_UART_A10KXYZ-if00-port0"),
			Product: "FT232R USB UART", Maker: "FTDI", VID: "0403", PID: "6001", Serial: "A10KXYZ"},
		filepath.Join(dev, "ttyACM0"): {Path: filepath.Join(dev, "ttyACM0"), Product: "USB JTAG/serial debug unit", Maker: "Espressif", VID: "303A", PID: "1001", Serial: "F4:12:FA"},
		filepath.Join(dev, "ttyAMA0"): {Path: filepath.Join(dev, "ttyAMA0"), Driver: "uart-pl011"},
	}
	if len(ports) != len(want) {
		t.Fatalf("listed %+v, want %d ports", ports, len(want))
	}
	for _, p := range ports {
		if p != want[p.Path] {
			t.Errorf("listed %+v, want %+v", p, want[p.Path])
		}
	}
}
