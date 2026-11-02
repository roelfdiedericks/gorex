package rex

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Each serial host has a server of its own on this machine, in a folder
// "serial-…" beside the others' (binding.dir in the app): a port one of
// them has open, another finds by asking them.

// PortUse is a serial port open in a session of a server on this machine:
// its path, the name of the session's host, the server's folder and the
// session's id.
type PortUse struct {
	Path, Host, Dir, SID string
}

// PortsInUse lists the ports open in the sessions of the servers of the
// serial hosts in folder hosts, but that of folder skip. A server that
// doesn't answer in a moment is passed over.
func PortsInUse(hosts, skip string) []PortUse {
	entries, err := os.ReadDir(hosts)
	if err != nil {
		return nil
	}
	var uses []PortUse
	for _, e := range entries {
		dir := filepath.Join(hosts, e.Name())
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "serial-") || dir == filepath.Clean(skip) {
			continue
		}
		c, err := dialServer(dir)
		if err != nil {
			continue
		}
		list, _ := c.List()
		c.Close()
		for _, in := range list {
			if in.Port != "" && in.PortState == "open" {
				uses = append(uses, PortUse{Path: in.Port, Host: in.PortHost, Dir: dir, SID: in.ID})
			}
		}
	}
	return uses
}

// ReleasePort has session sid of the server of folder dir release its
// port.
func ReleasePort(dir, sid string) error {
	c, err := dialServer(dir)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.SerialRelease(sid)
}

// heldBy returns the session of another serial host's server here that
// has the port at path open.
func heldBy(path string) (PortUse, bool) {
	for _, u := range PortsInUse(filepath.Dir(Dir()), Dir()) {
		if strings.EqualFold(u.Path, path) {
			if u.Host == "" {
				u.Host = "another GoRex serial host"
			}
			return u, true
		}
	}
	return PortUse{}, false
}

// dialServer connects to the server of folder dir, if one runs, for a
// question or two: the connection gives up after two seconds.
func dialServer(dir string) (*Client, error) {
	return Connect(func() (net.Conn, error) {
		conn, err := net.DialTimeout("unix", SocketPath(dir), time.Second)
		if err == nil {
			conn.SetDeadline(time.Now().Add(2 * time.Second))
		}
		return conn, err
	})
}
