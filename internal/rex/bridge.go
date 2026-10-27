package rex

import (
	"io"
	"os"
	"runtime"
	"time"

	"github.com/hashicorp/yamux"
)

// Bridge serves a client at the other end of r and w, as ssh's stdin and
// stdout: every stream the client opens becomes a connection to the
// server, started when none runs. It returns once the client is gone.
//
// The ssh agent forwarded to this connection, if any (SSH_AUTH_SOCK), is
// the connection's: the server, which outlives it, does not inherit it,
// but is told of it, and its sessions use it while the bridge runs
// (agent.go).
func Bridge(r io.Reader, w io.Writer) error {
	agent := os.Getenv("SSH_AUTH_SOCK")
	os.Unsetenv("SSH_AUTH_SOCK")
	if err := EnsureServer(); err != nil {
		return err
	}
	done := make(chan struct{})
	defer close(done)
	if agent != "" && runtime.GOOS != "windows" {
		// Before the client starts sessions, which then use it.
		offerAgent(agent, LocalDialer(), done)
	}
	exe, _ := Executable()
	if _, err := io.WriteString(w, BridgeReady+exe+"\n"); err != nil {
		return err
	}
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.LogOutput = os.Stderr
	sess, err := yamux.Server(rwc{r, w}, cfg)
	if err != nil {
		return err
	}
	defer sess.Close()
	// Dialing per stream starts the server again when it died, as after
	// a development build restarted it.
	dial := LocalDialer()
	for {
		st, err := sess.Accept()
		if err != nil {
			return nil
		}
		go splice(st, dial)
	}
}

// splice copies between a stream of the client and a connection to the
// server, until either ends.
func splice(st io.ReadWriteCloser, dial Dialer) {
	conn, err := dial()
	if err != nil {
		st.Close()
		return
	}
	go func() {
		io.Copy(conn, st)
		conn.Close()
	}()
	io.Copy(st, conn)
	st.Close()
	conn.Close()
}

// rwc joins a reader and a writer into an io.ReadWriteCloser.
type rwc struct {
	io.Reader
	io.Writer
}

func (c rwc) Close() error {
	if cl, ok := c.Reader.(io.Closer); ok {
		cl.Close()
	}
	if cl, ok := c.Writer.(io.Closer); ok {
		cl.Close()
	}
	return nil
}
