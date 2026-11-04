// Package cli is GoRex's commands: what `gorex <command>` does in the
// terminal it was typed in, rather than opening a window. On Windows,
// GoRex.exe is a GUI program, which a shell starts without a console;
// gorex.com beside it (cmd/gorex), which the shell runs for `gorex`
// first, is the same commands as a console program, and starts GoRex.exe
// for the rest. Elsewhere GoRex runs them itself.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// command is a command: its name, what it takes, what it does in a line,
// and what runs it, returning the status to exit with.
type command struct {
	name, args, help string
	run              func(w io.Writer, args []string) int
}

// commands are the commands, in the order help lists them.
var commands []command

func init() {
	commands = []command{
		{"doctor", "[user@host]", "show the system, ssh and ssh agent that GoRex sees; with a host, test a connection to it the way GoRex connects", doctor},
		{"help", "", "show these commands", help},
	}
}

// version is GoRex's version, as Run was told.
var version string

// Run runs the command args name, if they name one, writing to w, and
// reports whether there was one and the status to exit with. -h, --help
// and /? are help. ver is GoRex's version.
func Run(w io.Writer, args []string, ver string) (ran bool, code int) {
	if len(args) == 0 {
		return false, 0
	}
	name := args[0]
	if name == "-h" || name == "--help" || name == "/?" {
		name = "help"
	}
	for _, c := range commands {
		if c.name == name {
			version = ver
			return true, c.run(w, args[1:])
		}
	}
	return false, 0
}

func help(w io.Writer, _ []string) int {
	line := func(use, what string) { fmt.Fprintf(w, "  %-31s %s\n", strings.TrimSpace(use), what) }
	fmt.Fprintf(w, "GoRex: a terminal with tabs, panes and sessions that keep running\n\n")
	line("gorex", "open a window on the sessions on this computer")
	line("gorex ssh [-A | -a] [--plain | --server] user@host", "open a window on the host's sessions over ssh. The host's settings choose GoRex's server on the host or plain ssh; --server or --plain overrides them. -A forwards your ssh agent to the host and -a does not; both override the host setting and your ssh config")
	line("gorex serial <name>", "open a window on the serial host with that name (add serial hosts in the Add Host dialog)")
	for _, c := range commands {
		line("gorex "+c.name+" "+c.args, c.help)
	}
	return 0
}
