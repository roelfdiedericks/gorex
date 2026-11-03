package openssh

import (
	"net"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh/agent"
)

// The agent GoRex's ssh uses is as this PC's Settings, Hosts, ssh agent
// says (Windows): AgentAuto, Pageant (or KiTTY's Kageant) when it runs,
// unless SSH_AUTH_SOCK names an agent; AgentPageant, Pageant when it runs;
// AgentWindows, ssh's own, SSH_AUTH_SOCK's or Windows' ssh-agent service.
// Windows' OpenSSH talks to Pageant at its named pipe, as Pageant's
// --openssh-config tells it to.
const (
	AgentAuto    = "auto"
	AgentPageant = "pageant"
	AgentWindows = "windows"
)

// windowsAgentPipe is the pipe of Windows' ssh-agent service.
const windowsAgentPipe = `\\.\pipe\openssh-ssh-agent`

var agentMode atomic.Value // string

// SetAgent sets the agent the ssh GoRex runs from now on uses: AgentAuto,
// AgentPageant or AgentWindows.
func SetAgent(mode string) { agentMode.Store(mode) }

// AgentSock returns the agent GoRex tells its ssh of, as SSH_AUTH_SOCK:
// Pageant's pipe, or "" for ssh's own.
func AgentSock() string {
	pipe, ok := PageantPipe()
	if !ok {
		return ""
	}
	switch mode, _ := agentMode.Load().(string); {
	case mode == AgentPageant:
		return pipe
	case mode == AgentWindows, os.Getenv("SSH_AUTH_SOCK") != "":
		return ""
	}
	return pipe
}

// Env is the environment of an ssh GoRex runs, with the agent AgentSock
// chooses; nil, the inherited one, for ssh's own.
func Env() []string {
	if s := AgentSock(); s != "" {
		return append(os.Environ(), "SSH_AUTH_SOCK="+s)
	}
	return nil
}

// AgentInUse names the agent GoRex's ssh uses, and its socket or pipe:
// "" for none (outside Windows, no SSH_AUTH_SOCK).
func AgentInUse() (name, sock string) {
	if s := AgentSock(); s != "" {
		return "Pageant", s
	}
	if s := os.Getenv("SSH_AUTH_SOCK"); s != "" {
		return "SSH_AUTH_SOCK", s
	}
	if runtime.GOOS == "windows" {
		return "Windows ssh-agent", windowsAgentPipe
	}
	return "", ""
}

// AgentKeys lists the keys of the agent at sock, a named pipe or a Unix
// socket, as ssh would be offered them.
func AgentKeys(sock string) ([]*agent.Key, error) {
	var conn interface {
		Read([]byte) (int, error)
		Write([]byte) (int, error)
		Close() error
	}
	if strings.HasPrefix(sock, `\\.\pipe\`) {
		f, err := os.OpenFile(sock, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		conn = f
	} else {
		c, err := net.DialTimeout("unix", sock, 5*time.Second)
		if err != nil {
			return nil, err
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		conn = c
	}
	defer conn.Close()
	return agent.NewClient(conn).List()
}
