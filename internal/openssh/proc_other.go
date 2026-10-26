//go:build !windows

package openssh

import (
	"os/exec"
	"syscall"
)

// Detach runs cmd in a session of its own, without the terminal GoRex may
// have been started from: ssh asks its askpass, never that terminal.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func system() string { return "" }
