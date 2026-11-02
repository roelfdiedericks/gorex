package serialport

import (
	"regexp"
	"strings"

	"go.bug.st/serial/enumerator"
)

// list lists the COM ports Windows has, with the IDs and serial number of
// their USB adapters and the name their driver gives them, as
// "Silicon Labs CP210x USB to UART Bridge (COM7)". It asks the adapters
// nothing themselves: their own product strings would need a request to
// each device, which may disturb one in use.
func list() ([]Port, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	ports := make([]Port, 0, len(details))
	for _, d := range details {
		p := Port{Path: d.Name, Product: windowsName(d.Product)}
		if d.IsUSB {
			p.VID, p.PID, p.Serial = strings.ToUpper(d.VID), strings.ToUpper(d.PID), d.SerialNumber
			if generic[p.Product] && chips[p.VID+":"+p.PID] != "" {
				p.Product = ""
			}
		}
		ports = append(ports, p)
	}
	return ports, nil
}

var comSuffix = regexp.MustCompile(`\s*\(COM\d+\)$`)

// windowsName is a port's name as its driver gives it, without the COM
// port it ends with, which the port's path tells.
func windowsName(s string) string { return comSuffix.ReplaceAllString(s, "") }

// generic are the names drivers give every adapter they drive, which the
// chip's name tells more than.
var generic = map[string]bool{"USB Serial Port": true, "USB Serial Device": true, "USB-SERIAL": true, "": true}

func permissionHint(string) string { return "" }
