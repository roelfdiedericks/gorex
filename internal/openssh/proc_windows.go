package openssh

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// Detach keeps cmd off any console: none shows for it, run from the GUI
// app, and ssh asks its askpass, never a console.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// system is where Windows's optional OpenSSH Client installs ssh.
func system() string {
	p := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH", "ssh.exe")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
