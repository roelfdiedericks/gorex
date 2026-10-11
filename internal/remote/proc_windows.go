package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// hide keeps a console from showing for cmd, run from the GUI app.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// systemSSH is where Windows's optional OpenSSH Client installs ssh, which
// a PATH changed since may miss.
func systemSSH() string {
	p := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH", "ssh.exe")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
