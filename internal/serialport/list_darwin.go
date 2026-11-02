package serialport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// list lists the callout devices of /dev (cu.*), which open without
// waiting for a carrier as the tty.* ones do, but those of the Mac's own
// (Bluetooth's, the debug console), with what ioreg tells of the USB
// devices they are of.
func list() ([]Port, error) {
	paths, err := filepath.Glob("/dev/cu.*")
	if err != nil {
		return nil, err
	}
	var usb map[string]Port
	if out, err := exec.Command("ioreg", "-r", "-c", "IOUSBHostDevice", "-l", "-w0").Output(); err == nil {
		usb = parseIoreg(string(out))
	}
	var ports []Port
	for _, path := range paths {
		name := strings.TrimPrefix(path, "/dev/cu.")
		if strings.Contains(name, "Bluetooth") || name == "debug-console" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		p, ok := usb[path]
		if !ok {
			p = Port{Path: path}
		}
		ports = append(ports, p)
	}
	return ports, nil
}
