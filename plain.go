package main

import (
	"fmt"

	"gorex/internal/openssh"
	"gorex/internal/rex"
)

// A plain host is one GoRex reaches with ssh alone, as where its server
// can't run: the window's sessions are those of a server of the host's
// own on this machine (binding.dir), each running ssh to the host. What
// needs GoRex's server on the host, its files, git and the folders the
// shells are in, the window can't do there (plainReason).

// plainReason says why a plain host's window can't do what needs GoRex's
// server on the host.
const plainReason = "Not available on a plain ssh host, because GoRex's server doesn't run there"

// noFilesReason says why the window does not reach its host's files: a
// plain host's or a serial port's reason.
func (a *App) noFilesReason() string {
	if a.bind.serial != "" {
		return serialReason
	}
	return plainReason
}

// plainCommand returns what a session of the plain host b runs: ssh to
// it, which gives up on a link that stopped answering, with the host's
// options, forwarding the agent as gorex ssh -A or -a, or the host's
// switch, says, and the host's command, when it has one, in a terminal.
func plainCommand(b binding) ([]string, error) {
	ssh, err := openssh.Path()
	if err != nil {
		return nil, err
	}
	r, saved := findRemote(b.target.Key())
	opts := b.target.Opts
	if saved {
		// Its settings as they are now, which may have changed since
		// the window connected.
		opts = r.sshOpts()
	}
	args := append([]string{ssh, "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}, opts...)
	agent := b.target.Agent
	if agent == "" && r.ForwardAgent {
		agent = "yes"
	}
	if agent != "" {
		args = append(args, "-o", "ForwardAgent="+agent)
	}
	if r.Command != "" {
		return append(args, "-t", "--", b.target.Spec, r.Command), nil
	}
	return append(args, "--", b.target.Spec), nil
}

// exitCode returns the exit code of the session sid, as its server tells
// it: -1 when it can't.
func exitCode(client *rex.Client, sid string) int {
	infos, err := client.List()
	if err != nil {
		return -1
	}
	for _, info := range infos {
		if info.ID == sid {
			return info.ExitCode
		}
	}
	return -1
}

// sshEnded keeps the pane of a plain host whose ssh ended with code, as
// when the link went or the host could not be reached, rather than
// closing it as a logout does: it says so, under what ssh said, and Enter
// connects it again. Windows tells the code as 32 bits unsigned: ssh's -1
// comes as 4294967295.
func (a *App) sshEnded(p *Pane, code int) {
	p.ended = true
	if p.term != nil {
		p.term.Feed(fmt.Appendf(nil, "\r\n\x1b[0;2m[ssh ended with code %d] Press Enter to connect again.\x1b[0m\r\n", int32(code)))
	}
	a.changed()
}
