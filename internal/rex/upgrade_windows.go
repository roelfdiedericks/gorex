package rex

import (
	"errors"
	"net"
	"os"
)

// errNoUpgrade: a Windows server has no exec to replace itself in place
// with, nor a way to hand its pseudo consoles to another process.
var errNoUpgrade = errors.New("rex: upgrading in place needs Linux or macOS")

type upgradeState struct{}

func canUpgrade(string) error                                { return errNoUpgrade }
func (s *Server) upgrade(string) error                       { return errNoUpgrade }
func readState(string) (*upgradeState, error)                { return nil, errNoUpgrade }
func (s *Server) resume(*upgradeState)                       {}
func (*upgradeState) adopt() (*os.File, net.Listener, error) { return nil, nil, errNoUpgrade }
