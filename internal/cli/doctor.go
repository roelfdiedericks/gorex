package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gorex/internal/openssh"
)

// doctor writes what this GoRex sees of its system and of ssh to w:
// which ssh it runs among those there are, ssh's agent, and, for a host
// given, what ssh's configuration makes of it and how a connection goes,
// made as GoRex makes it, with what went wrong said in words. It returns
// 1 when GoRex has no ssh or can't connect to the host, else 0.
func doctor(w io.Writer, args []string) int {
	p := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	exe, _ := os.Executable()
	p("GoRex doctor")
	p("")
	p("GoRex     %s, %s/%s", orNone(version), runtime.GOOS, runtime.GOARCH)
	p("program   %s", exe)
	p("PATH      as GoRex sees it:")
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		p("          %s", d)
	}
	p("")

	p("ssh")
	ssh, err := openssh.Path()
	if err != nil {
		p("  GoRex finds none: %v", err)
	}
	for _, s := range openssh.Found() {
		mark := "   "
		if strings.EqualFold(s, ssh) {
			mark = " * "
		}
		v, _ := openssh.Run(s, "-V")
		p(" %s%s  %s", mark, s, firstLine(v))
	}
	if ssh != "" {
		p("  * is the one GoRex runs: the first on PATH. A shell may run another, by an alias or")
		p("    a PATH of its own: compare with `Get-Command ssh` (PowerShell) or `command -v ssh`.")
	}
	p("")

	p("ssh-agent")
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		p("  SSH_AUTH_SOCK  %s", sock)
	} else if runtime.GOOS == "windows" {
		p("  SSH_AUTH_SOCK  unset: Windows' OpenSSH uses the ssh-agent service's pipe")
	} else {
		p("  SSH_AUTH_SOCK  unset: no agent")
	}
	if ssh != "" {
		out, _ := openssh.Run(openssh.Beside(ssh, "ssh-add"), "-l")
		for _, l := range lines(out) {
			p("  %s", l)
		}
		switch {
		case strings.Contains(out, "no identities"):
			p("  -> the agent runs but holds no key: GoRex asks for a key's passphrase on each connect;")
			p("     `ssh-add` loads the key once, and GoRex need not ask")
		case strings.Contains(out, "Error connecting to agent"), strings.Contains(out, "Could not open a connection"):
			p("  -> no agent: GoRex asks for a key's passphrase on each connect")
			if runtime.GOOS == "windows" {
				p("     to ask once instead: Start-Service ssh-agent; Set-Service ssh-agent -StartupType Automatic; ssh-add")
			}
		}
	}

	if len(args) == 0 {
		p("")
		p("`gorex doctor user@host` checks a host too, connecting as GoRex does.")
		if ssh == "" {
			return 1
		}
		return 0
	}
	spec := args[0]
	p("")
	p("%s", spec)
	if ssh == "" {
		return 1
	}
	cfg, err := openssh.Run(ssh, "-G", "--", spec)
	if err != nil {
		p("  ssh -G: %v %s", err, cfg)
	} else {
		p("  as ssh's configuration makes it (ssh -G):")
		for _, l := range lines(cfg) {
			k, _, _ := strings.Cut(l, " ")
			switch k {
			case "user", "hostname", "port", "identityfile", "identitiesonly", "identityagent", "forwardagent", "proxyjump", "proxycommand", "batchmode", "preferredauthentications":
				p("    %s", l)
			}
		}
	}
	p("  connecting as GoRex does: %s %s", filepath.Base(ssh), strings.Join(openssh.ProbeArgs(spec), " "))
	out, err := openssh.Probe(spec)
	var methods string
	// What says how the connection went: ssh's own lines, and its debug
	// lines on the host, the keys and the methods.
	for _, l := range lines(out) {
		debug := strings.HasPrefix(l, "debug")
		l = strings.TrimPrefix(l, "debug1: ")
		if strings.HasPrefix(l, "Authentications that can continue:") {
			methods = strings.TrimSpace(strings.TrimPrefix(l, "Authentications that can continue:"))
		}
		switch {
		case !debug && !strings.HasPrefix(l, "OpenSSH"),
			strings.HasPrefix(l, "Connecting to"), strings.HasPrefix(l, "Authenticating to"),
			strings.HasPrefix(l, "Reading configuration"), strings.Contains(l, "Applying options"),
			strings.HasPrefix(l, "get_agent_identities"), strings.HasPrefix(l, "Will attempt key"),
			strings.HasPrefix(l, "Authentications that can continue"),
			strings.HasPrefix(l, "Offering public key"), strings.HasPrefix(l, "Server accepts key"),
			strings.HasPrefix(l, "Authenticated to"), strings.HasPrefix(l, "Exit status"),
			strings.Contains(l, "passphrase"), strings.Contains(l, "Permission denied"):
			p("    %s", l)
		}
	}
	p("")
	// The check asks nothing (BatchMode): what GoRex's window would ask,
	// it says.
	switch {
	case err == nil:
		p("  -> GoRex connects without asking. If its window still fails, the trouble is past ssh: see its Connection Log.")
		return 0
	case strings.Contains(out, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
		p("  -> the host's key changed since ssh last saw it, which ssh refuses: if the host was reinstalled,")
		p("     remove the old key with ssh-keygen -R <host>.")
	case strings.Contains(out, "Host key verification failed"):
		p("  -> ssh has not seen this host's key: GoRex shows it, and asks whether to trust it.")
		return 0
	case strings.Contains(out, "Could not resolve"):
		p("  -> the name does not resolve here.")
	case strings.Contains(out, "timed out"), strings.Contains(out, "refused"):
		p("  -> the host can't be reached on that port.")
	case strings.Contains(out, "Permission denied"):
		if !strings.Contains(out, "Offering public key") {
			p("  -> ssh offered no key: none in the agent, and none it could read without a passphrase.")
		} else if !strings.Contains(out, "Server accepts key") {
			p("  -> ssh offered keys the host refused: it doesn't list them in authorized_keys.")
		}
		if strings.Contains(methods, "password") || strings.Contains(methods, "keyboard-interactive") {
			p("     GoRex asks for the password (or a key's passphrase) in its window, on each connect.")
			return 0
		}
		p("     The host takes no password either: GoRex can't log in.")
	default:
		p("  -> ssh failed: %v", err)
	}
	return 1
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	return l
}

func orNone(s string) string {
	if s == "" {
		return "(no version)"
	}
	return s
}
