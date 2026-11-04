package cli

import (
	"encoding/json"
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
	// ssh runs here with the agent GoRex's would use.
	mode := savedAgent()
	openssh.SetAgent(mode)
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
		p("  GoRex cannot find ssh: %v", err)
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
		p("  * marks the ssh that GoRex runs, which is the first one on PATH. Your shell may run a")
		p("    different one because of an alias or a different PATH. To check, run `Get-Command ssh`")
		p("    (PowerShell) or `command -v ssh`.")
	}
	p("")

	p("ssh-agent")
	if runtime.GOOS == "windows" {
		p("  setting        %s (Settings, Hosts, ssh agent)", mode)
		if pipe, running := openssh.PageantPipe(); running {
			p("  Pageant        running at %s", pipe)
		} else {
			p("  Pageant        not running")
		}
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		p("  SSH_AUTH_SOCK  %s", sock)
	} else if runtime.GOOS == "windows" {
		p("  SSH_AUTH_SOCK  not set, so the Windows OpenSSH uses the pipe of the ssh-agent service")
	} else {
		p("  SSH_AUTH_SOCK  not set, so there is no agent")
	}
	if name, sock := openssh.AgentInUse(); name != "" {
		p("  GoRex uses     %s at %s. Its keys:", name, sock)
	}
	if ssh != "" {
		out, _ := openssh.Run(openssh.Beside(ssh, "ssh-add"), "-l")
		for _, l := range lines(out) {
			p("  %s", l)
		}
		switch {
		case strings.Contains(out, "no identities"):
			p("  -> The agent is running but holds no keys, so GoRex asks for a key's passphrase each time it connects.")
			p("     Run `ssh-add` to load the key once. Then GoRex does not need to ask.")
		case strings.Contains(out, "Error connecting to agent"), strings.Contains(out, "Could not open a connection"):
			p("  -> No agent is running, so GoRex asks for a key's passphrase each time it connects.")
			if runtime.GOOS == "windows" {
				p("     To enter the passphrase only once, run: Start-Service ssh-agent; Set-Service ssh-agent -StartupType Automatic; ssh-add")
			}
		}
	}

	if len(args) == 0 {
		p("")
		p("Run `gorex doctor user@host` to check a host as well. It connects the way GoRex does.")
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
		p("  settings from the ssh configuration (ssh -G):")
		for _, l := range lines(cfg) {
			k, _, _ := strings.Cut(l, " ")
			switch k {
			case "user", "hostname", "port", "identityfile", "identitiesonly", "identityagent", "forwardagent", "proxyjump", "proxycommand", "batchmode", "preferredauthentications":
				p("    %s", l)
			}
		}
	}
	p("  connecting the way GoRex does: %s %s", filepath.Base(ssh), strings.Join(openssh.ProbeArgs(spec), " "))
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
		p("  -> GoRex connects without asking for anything. If the GoRex window still fails, the problem is not in ssh. Check the Connection Log.")
		return 0
	case strings.Contains(out, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
		p("  -> The host's key has changed since ssh last saw it, so ssh refuses to connect. If the host was reinstalled,")
		p("     remove the old key with ssh-keygen -R <host>.")
	case strings.Contains(out, "Host key verification failed"):
		p("  -> ssh has not seen this host's key before. GoRex shows the key and asks whether to trust it.")
		return 0
	case strings.Contains(out, "Could not resolve"):
		p("  -> This computer cannot resolve the host name.")
	case strings.Contains(out, "timed out"), strings.Contains(out, "refused"):
		p("  -> The host cannot be reached on that port.")
	case strings.Contains(out, "Permission denied"):
		if !strings.Contains(out, "Offering public key") {
			p("  -> ssh offered no key. The agent has no keys, and ssh found no key it could read without a passphrase.")
		} else if !strings.Contains(out, "Server accepts key") {
			p("  -> ssh offered keys, but the host refused them. They are not in the host's authorized_keys file.")
		}
		if strings.Contains(methods, "password") || strings.Contains(methods, "keyboard-interactive") {
			p("     GoRex asks for the password (or a key's passphrase) in its window each time it connects.")
			return 0
		}
		p("     The host does not accept a password either, so GoRex cannot log in.")
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

// savedAgent is the ssh agent this PC's GoRex settings choose (Settings,
// Hosts, ssh agent), in its data folder's defaults.json: automatic when
// it says none.
func savedAgent() string {
	dir := os.Getenv("GOREX_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return openssh.AgentAuto
		}
		dir = filepath.Join(base, "GoRex")
	}
	var s struct {
		Hosts struct {
			Agent string `json:"agent"`
		} `json:"hosts"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "defaults.json")); err == nil {
		json.Unmarshal(b, &s)
	}
	if s.Hosts.Agent == "" {
		return openssh.AgentAuto
	}
	return s.Hosts.Agent
}
