// Package openssh finds and runs the OpenSSH client GoRex connects to
// other machines with, and tells what there is of it, for gorex doctor.
package openssh

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Options are the options of every ssh GoRex runs, which give up on a
// link that stopped answering. Without BatchMode=yes, which GoRex adds
// when nothing can answer, ssh asks for a password, a passphrase or
// whether to trust a host's key through its askpass, GoRex's window
// (internal/askpass).
var Options = []string{"-T", "-o", "ConnectTimeout=10",
	"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}

// Batch is the option of an ssh that asks nothing: its login fails
// where it would ask.
var Batch = []string{"-o", "BatchMode=yes"}

// Path returns the ssh GoRex runs: the first on PATH, else the one
// Windows' OpenSSH Client installs, which a PATH changed since may miss.
func Path() (string, error) {
	if p, err := exec.LookPath("ssh"); err == nil {
		return p, nil
	}
	if p := system(); p != "" {
		return p, nil
	}
	return "", errors.New("GoRex needs OpenSSH's ssh: install the OpenSSH Client")
}

// Found returns every ssh on PATH, in PATH's order, then the system's
// when it is not among them.
func Found() []string {
	name := "ssh"
	if runtime.GOOS == "windows" {
		name = "ssh.exe"
	}
	var out []string
	add := func(p string) {
		if !slices.ContainsFunc(out, func(q string) bool { return strings.EqualFold(q, p) }) {
			out = append(out, p)
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			add(p)
		}
	}
	if p := system(); p != "" {
		add(p)
	}
	return out
}

// Beside returns the path of program (ssh-add) in the directory of the
// ssh at path.
func Beside(path, program string) string {
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	return filepath.Join(filepath.Dir(path), program)
}

// Run runs path with args, for at most 30 seconds, and returns what it
// printed.
func Run(path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	Detach(cmd)
	out, err := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\r\n"), err
}

// ProbeArgs are the arguments Probe runs ssh with: GoRex's Options,
// asking nothing, verbose, and uname -sm on the host.
func ProbeArgs(spec string) []string {
	return append(slices.Concat(Options, Batch), "-v", "--", spec, "uname -sm")
}

// Probe connects to spec as GoRex does, with its ssh and Options, and
// returns what ssh printed (ProbeArgs).
func Probe(spec string) (string, error) {
	ssh, err := Path()
	if err != nil {
		return "", err
	}
	return Run(ssh, ProbeArgs(spec)...)
}
