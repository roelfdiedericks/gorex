// Package askpass lets the ssh GoRex runs ask the user, in GoRex's
// window, what it would ask in a terminal: a password, a key's
// passphrase, a one-time code, whether to trust a host's key it has not
// seen. GoRex runs ssh with SSH_ASKPASS naming GoRex itself and
// SSH_ASKPASS_REQUIRE=force (Session.Env); ssh runs that GoRex with its
// prompt, which, in askpass mode (Main), hands the prompt to the window
// over a Unix socket (Server), with the token of the connect it belongs
// to, and prints the window's answer for ssh.
package askpass

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// envVar holds, in ssh's environment, the token of the connect and the
// path of the window's socket: "<token>@<path>".
const envVar = "GOREX_ASKPASS"

// Kind is what a prompt asks for.
type Kind int

const (
	// Secret is a password, a passphrase or a code: typed, not shown.
	Secret Kind = iota
	// HostKey is whether to trust a host's key ssh has not seen: yes or
	// no.
	HostKey
	// Confirm is a yes or no, as ssh-agent asks before using a key added
	// with ssh-add -c.
	Confirm
	// Notice tells something, as to touch a security key, until ssh
	// takes it away.
	Notice
)

// Prompt is what ssh asks: its text, as ssh wrote it, and its kind. Again
// is set when the same ssh asks the same again: the answer was wrong.
type Prompt struct {
	Text  string
	Kind  Kind
	Again bool
}

// kindOf tells a prompt's kind by what ssh says of it in
// SSH_ASKPASS_PROMPT, and by its text.
func kindOf(text, prompt string) Kind {
	switch {
	case strings.Contains(text, "(yes/no"):
		return HostKey
	case prompt == "confirm":
		return Confirm
	case prompt == "none":
		return Notice
	}
	return Secret
}

type request struct {
	Token  string `json:"token"`
	Text   string `json:"text"`
	Prompt string `json:"prompt"` // SSH_ASKPASS_PROMPT
	PPID   int    `json:"ppid"`   // the ssh asking
}

type reply struct {
	OK     bool   `json:"ok"`
	Answer string `json:"answer"`
}

// Active reports whether this process is ssh's askpass: GoRex's main
// runs Main then, before anything else.
func Active() bool { return os.Getenv(envVar) != "" }

// Main is GoRex as ssh's askpass: it asks the window for the answer to
// the prompt in args and prints it, returning 0, or returns 1 when the
// user cancelled or the window is gone.
func Main(args []string) int {
	token, path, ok := strings.Cut(os.Getenv(envVar), "@")
	if !ok {
		return 1
	}
	conn, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gorex askpass:", err)
		return 1
	}
	defer conn.Close()
	req := request{Token: token, Text: strings.Join(args, " "), Prompt: os.Getenv("SSH_ASKPASS_PROMPT"), PPID: os.Getppid()}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return 1
	}
	var r reply
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&r); err != nil || !r.OK {
		return 1
	}
	if _, err := fmt.Fprintln(os.Stdout, r.Answer); err != nil {
		return 1
	}
	return 0
}

// Server is the window's end: a Unix socket that the askpass GoRex
// connects to, which hands each prompt to the Session of its token.
type Server struct {
	ln   net.Listener
	path string

	mu       sync.Mutex
	sessions map[string]*Session
}

// Listen starts a Server with its socket in dir, or in the temporary
// directory when that path is too long for a socket.
func Listen(dir string) (*Server, error) {
	name := fmt.Sprintf("askpass-%d.sock", os.Getpid())
	path := filepath.Join(dir, name)
	if len(path) >= 100 {
		path = filepath.Join(os.TempDir(), name)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	os.Chmod(path, 0o600)
	s := &Server{ln: ln, path: path, sessions: map[string]*Session{}}
	go s.serve()
	return s, nil
}

// Close stops the server.
func (s *Server) Close() {
	s.ln.Close()
	os.Remove(s.path)
}

func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	var req request
	if err := json.NewDecoder(br).Decode(&req); err != nil {
		return
	}
	conn.SetReadDeadline(time.Time{})
	s.mu.Lock()
	se := s.sessions[req.Token]
	s.mu.Unlock()
	if se == nil {
		json.NewEncoder(conn).Encode(reply{})
		return
	}
	// When the askpass ends, as when ssh takes a notice away, or the
	// session does, the prompt goes.
	ctx, cancel := context.WithCancel(se.ctx)
	defer cancel()
	go func() {
		br.ReadByte()
		cancel()
	}()
	answer, ok := se.answer(ctx, req)
	json.NewEncoder(conn).Encode(reply{OK: ok, Answer: answer})
}

// Session is the prompts of one connect, whose ssh's have Env: each goes
// to ask, which waits for the user. An answer is kept for the connect's
// other ssh's, which ask the same: connecting runs ssh more than once.
type Session struct {
	s      *Server
	token  string
	ask    func(context.Context, Prompt) (string, bool)
	ctx    context.Context // ends with the session
	cancel context.CancelFunc
	stop   chan struct{} // closed when the user cancels

	mu        sync.Mutex
	answers   map[string]string       // by prompt
	seen      map[int]map[string]bool // by ssh, the prompts it asked
	asked     bool                    // the user answered a secret
	cancelled bool                    // the user cancelled a prompt
	refused   bool                    // the user did not trust a host key
	asking    int                     // prompts waiting for the user
}

// Session starts a Session whose prompts go to ask, which returns the
// answer, or false when the user cancelled; ctx ends when the prompt is
// to go without an answer.
func (s *Server) Session(ask func(ctx context.Context, p Prompt) (answer string, ok bool)) *Session {
	b := make([]byte, 16)
	rand.Read(b)
	ctx, cancel := context.WithCancel(context.Background())
	se := &Session{s: s, token: hex.EncodeToString(b), ask: ask, ctx: ctx, cancel: cancel, stop: make(chan struct{}),
		answers: map[string]string{}, seen: map[int]map[string]bool{}}
	s.mu.Lock()
	s.sessions[se.token] = se
	s.mu.Unlock()
	return se
}

// Close ends the session: its answers are forgotten, and its prompts
// still waiting go.
func (se *Session) Close() {
	se.s.mu.Lock()
	delete(se.s.sessions, se.token)
	se.s.mu.Unlock()
	se.cancel()
	se.mu.Lock()
	clear(se.answers)
	se.mu.Unlock()
}

// Env is what ssh's environment needs for it to ask through the session:
// exe is the GoRex that ssh runs as its askpass. ssh before 8.4 knows no
// SSH_ASKPASS_REQUIRE, and asks its askpass only with a DISPLAY.
func (se *Session) Env(exe string) []string {
	env := []string{"SSH_ASKPASS=" + exe, "SSH_ASKPASS_REQUIRE=force", envVar + "=" + se.token + "@" + se.s.path}
	if runtime.GOOS != "windows" && os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=:0")
	}
	return env
}

// Done is closed when the user cancelled a prompt: the connect's ssh's
// are to be stopped, rather than go on with no answer.
func (se *Session) Done() <-chan struct{} { return se.stop }

// Ended is closed when the session is.
func (se *Session) Ended() <-chan struct{} { return se.ctx.Done() }

// Asking reports whether a prompt waits for the user, which time limits
// on connecting don't count.
func (se *Session) Asking() bool {
	se.mu.Lock()
	defer se.mu.Unlock()
	return se.asking > 0
}

// Outcome reports what the user did: answered a secret, cancelled a
// prompt, refused a host key.
func (se *Session) Outcome() (asked, cancelled, refused bool) {
	se.mu.Lock()
	defer se.mu.Unlock()
	return se.asked, se.cancelled, se.refused
}

// answer answers the request: with what the user answered the same
// prompt before in this connect, unless the same ssh asks again, which
// means it was wrong; else by asking.
func (se *Session) answer(ctx context.Context, req request) (string, bool) {
	p := Prompt{Text: req.Text, Kind: kindOf(req.Text, req.Prompt)}
	se.mu.Lock()
	if se.cancelled {
		se.mu.Unlock()
		return "", false
	}
	if p.Kind == Secret {
		asked := se.seen[req.PPID]
		if asked == nil {
			asked = map[string]bool{}
			se.seen[req.PPID] = asked
		}
		p.Again = asked[p.Text]
		asked[p.Text] = true
		if a, ok := se.answers[p.Text]; ok && !p.Again {
			se.mu.Unlock()
			return a, true
		}
	}
	se.asking++
	se.mu.Unlock()
	answer, ok := se.ask(ctx, p)
	se.mu.Lock()
	defer se.mu.Unlock()
	se.asking--
	switch {
	case ctx.Err() != nil:
		// The prompt went without the user: ssh took it away, or the
		// connect ended.
		return "", false
	case !ok && p.Kind != Notice:
		if !se.cancelled {
			se.cancelled = true
			close(se.stop)
		}
		return "", false
	case p.Kind == Secret:
		se.asked = true
		se.answers[p.Text] = answer
	case p.Kind == HostKey && answer != "yes":
		se.refused = true
	}
	return answer, ok
}
