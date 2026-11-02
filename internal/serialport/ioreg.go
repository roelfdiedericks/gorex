package serialport

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ioregNode is an object of the IORegistry as ioreg -l prints it: its
// class, how deep in the tree its line starts, and its properties.
type ioregNode struct {
	class string
	depth int
	props map[string]string
}

var (
	ioregObject   = regexp.MustCompile(`^([ |]*)\+-o .*?  <class ([^,>]+)`)
	ioregProperty = regexp.MustCompile(`^[ |]*"([^"]+)" = (.*)$`)
)

// parseIoreg reads the tree ioreg -r -c IOUSBHostDevice -l -w0 prints
// into the serial ports in it, by the path that opens them (their callout
// device, /dev/cu.*): each with what the USB device it is under tells of
// itself, its product and maker, IDs and serial number.
func parseIoreg(out string) map[string]Port {
	ports := map[string]Port{}
	var stack []*ioregNode
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := ioregObject.FindStringSubmatch(line); m != nil {
			n := &ioregNode{class: m[2], depth: len(m[1]), props: map[string]string{}}
			for len(stack) > 0 && stack[len(stack)-1].depth >= n.depth {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, n)
			continue
		}
		m := ioregProperty.FindStringSubmatch(line)
		if m == nil || len(stack) == 0 {
			continue
		}
		top := stack[len(stack)-1]
		top.props[m[1]] = m[2]
		if m[1] != "IOCalloutDevice" {
			continue
		}
		path := unquote(m[2])
		p := Port{Path: path}
		for i := len(stack) - 1; i >= 0; i-- {
			if d := stack[i]; d.class == "IOUSBHostDevice" || d.class == "IOUSBDevice" {
				p.Product = unquote(d.props["USB Product Name"])
				p.Maker = unquote(d.props["USB Vendor Name"])
				p.Serial = unquote(d.props["USB Serial Number"])
				p.VID, p.PID = usbID(d.props["idVendor"]), usbID(d.props["idProduct"])
				break
			}
		}
		ports[path] = p
	}
	return ports
}

func unquote(v string) string {
	if s, err := strconv.Unquote(v); err == nil {
		return s
	}
	return strings.Trim(v, `"`)
}

// usbID writes a USB ID as ioreg prints it, in decimal, as four hex
// digits.
func usbID(v string) string {
	n, err := strconv.ParseUint(strings.TrimSpace(v), 0, 16)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%04X", n)
}
