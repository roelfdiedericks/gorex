//go:build !windows

package remote

import "os/exec"

func hide(*exec.Cmd) {}

func systemSSH() string { return "" }
