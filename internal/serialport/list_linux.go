package serialport

import (
	"os"
	"path/filepath"
	"strings"
)

func list() ([]Port, error) {
	return listSysfs("/sys/class/tty", "/dev", "/dev/serial/by-id")
}

// listSysfs lists the ttys of sys (/sys/class/tty) that a device is behind,
// at their paths in dev, but the legacy ports the 8250 driver keeps for
// hardware that is not there; a USB adapter's with its device's IDs and
// strings, and its name in byID, which stays when it is plugged in again.
func listSysfs(sys, dev, byID string) ([]Port, error) {
	entries, err := os.ReadDir(sys)
	if err != nil {
		return nil, err
	}
	stable := map[string]string{}
	if links, err := os.ReadDir(byID); err == nil {
		for _, l := range links {
			link := filepath.Join(byID, l.Name())
			if target, err := os.Readlink(link); err == nil {
				stable[filepath.Base(target)] = link
			}
		}
	}
	var ports []Port
	for _, e := range entries {
		name := e.Name()
		device, err := filepath.EvalSymlinks(filepath.Join(sys, name, "device"))
		if err != nil {
			continue // a virtual terminal, a pseudo-terminal
		}
		path := filepath.Join(dev, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		subsystem := linkBase(filepath.Join(device, "subsystem"))
		driver := linkBase(filepath.Join(device, "driver"))
		if subsystem == "platform" && driver == "serial8250" {
			continue
		}
		p := Port{Path: path, Stable: stable[name], Driver: driver}
		switch subsystem {
		case "usb-serial":
			usbDevice(filepath.Dir(filepath.Dir(device)), &p)
		case "usb":
			usbDevice(filepath.Dir(device), &p)
		}
		ports = append(ports, p)
	}
	return ports, nil
}

// usbDevice reads a USB device's IDs and strings from its sysfs folder.
func usbDevice(dir string, p *Port) {
	p.VID = strings.ToUpper(sysLine(filepath.Join(dir, "idVendor")))
	p.PID = strings.ToUpper(sysLine(filepath.Join(dir, "idProduct")))
	p.Serial = sysLine(filepath.Join(dir, "serial"))
	p.Maker = sysLine(filepath.Join(dir, "manufacturer"))
	p.Product = sysLine(filepath.Join(dir, "product"))
	p.Driver = ""
}

func sysLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func linkBase(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}
