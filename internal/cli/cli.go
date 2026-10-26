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
		{"doctor", "[user@host]", "what this GoRex sees of its system, its ssh and ssh's agent; with a host, how a connection to it goes, made as GoRex makes it", doctor},
		{"help", "", "these commands", help},
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
	line := func(use, what string) { fmt.Fprintf(w, "  %-25s %s\n", strings.TrimSpace(use), what) }
	fmt.Fprintf(w, "GoRex, a terminal of tabs, panes and sessions that stay\n\n")
	line("gorex", "open a window on this machine's sessions")
	line("gorex ssh user@host", "open a window on the sessions of a server on host, over ssh")
	for _, c := range commands {
		line("gorex "+c.name+" "+c.args, c.help)
	}
	return 0
}
