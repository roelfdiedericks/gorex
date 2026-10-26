// Command gorex is GoRex's console program on Windows, built as gorex.com
// beside GoRex.exe (make dev; the installer puts it in GoRex's bin, on
// the PATH). A shell runs gorex.com for `gorex`, before GoRex.exe, as
// .COM comes before .EXE in PATHEXT: it runs GoRex's commands
// (internal/cli) itself, with the console a console program has, so the
// shell waits and gets the exit status, and starts GoRex.exe, the GUI
// program, for anything else, returning at once. This is Visual Studio's
// devenv.com and devenv.exe.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gorex/internal/cli"
)

// version is GoRex's, set by the build (-X main.version).
var version string

func main() {
	if ran, code := cli.Run(os.Stdout, os.Args[1:], version); ran {
		os.Exit(code)
	}
	exe, err := gui()
	if err == nil {
		err = exec.Command(exe, os.Args[1:]...).Start()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gorex:", err)
		os.Exit(1)
	}
}

// gui returns GoRex.exe: beside this program, as in make dev's dev, or in
// the directory above, as the installer's bin is in GoRex's.
func gui() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(self)
	for _, p := range []string{filepath.Join(dir, "GoRex.exe"), filepath.Join(dir, "..", "GoRex.exe")} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("GoRex.exe is not beside %s, nor above it", self)
}
