//go:build !linux && !windows && !darwin

package serialport

import "go.bug.st/serial"

// list lists the ports go.bug.st/serial finds, by path alone.
func list() ([]Port, error) {
	paths, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	ports := make([]Port, len(paths))
	for i, p := range paths {
		ports[i] = Port{Path: p}
	}
	return ports, nil
}
