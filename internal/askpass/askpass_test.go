package askpass

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gorex/internal/openssh"
)

// The test binary is the askpass the tests run, as GoRex is ssh's.
func TestMain(m *testing.M) {
	if Active() {
		os.Exit(Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// askpassExe is a copy of the test binary in a directory with a space in
// its name, as a Windows user's profile often has.
func askpassExe(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "with space")
	os.Mkdir(dir, 0o755)
	exe := filepath.Join(dir, filepath.Base(self))
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func listen(t *testing.T) *Server {
	t.Helper()
	s, err := Listen(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// runAskpass runs the askpass as ssh does: the prompt as its argument,
// the session's environment; it returns what it printed and its status.
func runAskpass(t *testing.T, exe string, env []string, prompt string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, prompt)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// The askpass prints the window's answer and exits 0; a cancel, an
// unknown token or a session ended give ssh no answer: exit 1.
func TestHandOff(t *testing.T) {
	exe := askpassExe(t)
	s := listen(t)
	var got []Prompt
	answer, ok := "pa ss", true
	se := s.Session(func(_ context.Context, p Prompt) (string, bool) {
		got = append(got, p)
		return answer, ok
	})
	defer se.Close()
	env := se.Env(exe)

	if out, code := runAskpass(t, exe, env, "root@host's password: "); code != 0 || out != "pa ss\n" {
		t.Errorf("answered: %q, exit %d", out, code)
	}
	if len(got) != 1 || got[0].Text != "root@host's password: " || got[0].Kind != Secret {
		t.Errorf("the window got %+v", got)
	}

	ok = false
	if out, code := runAskpass(t, exe, env, "Enter passphrase for key 'id': "); code != 1 || out != "" {
		t.Errorf("cancelled: %q, exit %d", out, code)
	}
	select {
	case <-se.Done():
	default:
		t.Error("Done is open after a cancel")
	}
	if _, cancelled, _ := se.Outcome(); !cancelled {
		t.Error("the outcome lacks the cancel")
	}

	bad := slices.Clone(env)
	for i, e := range bad {
		if strings.HasPrefix(e, envVar+"=") {
			bad[i] = envVar + "=0000" + e[strings.Index(e, "@"):]
		}
	}
	n := len(got)
	if _, code := runAskpass(t, exe, bad, "password: "); code != 1 || len(got) != n {
		t.Errorf("an unknown token: exit %d, the window asked %d times", code, len(got)-n)
	}
}

// A prompt waiting for the user goes when the session ends, and the
// askpass gives ssh no answer.
func TestSessionEnds(t *testing.T) {
	exe := askpassExe(t)
	s := listen(t)
	asking := make(chan struct{})
	gone := make(chan struct{})
	se := s.Session(func(ctx context.Context, p Prompt) (string, bool) {
		close(asking)
		<-ctx.Done()
		close(gone)
		return "", false
	})
	go func() {
		<-asking
		if !se.Asking() {
			t.Error("Asking is false while the user is asked")
		}
		se.Close()
	}()
	if _, code := runAskpass(t, exe, se.Env(exe), "password: "); code != 1 {
		t.Errorf("exit %d", code)
	}
	<-gone
	if _, cancelled, _ := se.Outcome(); cancelled {
		t.Error("the end of the session counts as the user's cancel")
	}
}

// An answer serves the connect's other ssh's, asking the same; the same
// ssh asking again was answered wrong, and the user is asked again.
func TestAnswerKept(t *testing.T) {
	s := listen(t)
	var asked []Prompt
	answers := []string{"wrong", "right"}
	se := s.Session(func(_ context.Context, p Prompt) (string, bool) {
		asked = append(asked, p)
		a := answers[0]
		answers = answers[1:]
		return a, true
	})
	defer se.Close()
	ask := func(ppid int, text string) string {
		a, ok := se.answer(context.Background(), request{Text: text, PPID: ppid})
		if !ok {
			t.Fatalf("no answer to %q from %d", text, ppid)
		}
		return a
	}
	const prompt = "root@host's password: "
	if a := ask(1, prompt); a != "wrong" || len(asked) != 1 || asked[0].Again {
		t.Fatalf("first: %q, asked %+v", a, asked)
	}
	if a := ask(1, prompt); a != "right" || len(asked) != 2 || !asked[1].Again {
		t.Fatalf("the same ssh again: %q, asked %+v", a, asked)
	}
	if a := ask(2, prompt); a != "right" || len(asked) != 2 {
		t.Fatalf("another ssh: %q, asked %d times", a, len(asked))
	}
	if asked, _, _ := se.Outcome(); !asked {
		t.Error("the outcome lacks the answer")
	}
}

// A host key's question is asked each time, never answered from before,
// and a no is a refusal.
func TestHostKeyAskedEachTime(t *testing.T) {
	s := listen(t)
	n := 0
	se := s.Session(func(_ context.Context, p Prompt) (string, bool) {
		n++
		if p.Kind != HostKey {
			t.Errorf("kind %v", p.Kind)
		}
		return "no", true
	})
	defer se.Close()
	q := "Are you sure you want to continue connecting (yes/no/[fingerprint])? "
	se.answer(context.Background(), request{Text: q, PPID: 1})
	se.answer(context.Background(), request{Text: q, PPID: 2})
	if n != 2 {
		t.Errorf("asked %d times", n)
	}
	if _, _, refused := se.Outcome(); !refused {
		t.Error("no is not a refusal")
	}
}

func TestKindOf(t *testing.T) {
	for _, c := range []struct {
		text, prompt string
		want         Kind
	}{
		{"root@192.168.0.180's password: ", "", Secret},
		{"Enter passphrase for key 'C:\\Users\\x/.ssh/id_rsa': ", "", Secret},
		{"(root@host) Verification code: ", "", Secret},
		{"The authenticity of host '192.168.0.180 (192.168.0.180)' can't be established.\r\nED25519 key fingerprint is SHA256:x.\r\nAre you sure you want to continue connecting (yes/no/[fingerprint])? ", "", HostKey},
		{"Allow use of key id_ed25519?", "confirm", Confirm},
		{"Confirm user presence for key ED25519-SK", "none", Notice},
	} {
		if got := kindOf(c.text, c.prompt); got != c.want {
			t.Errorf("%q (%q): %v, want %v", c.text, c.prompt, got, c.want)
		}
	}
}

// With a real ssh and a host that takes a password, set in
// GOREX_TEST_PASSWORD_HOST (user@host) and GOREX_TEST_PASSWORD: ssh asks
// the window to trust the host's key, which it has not seen (a known
// hosts file of the test's own), then for the password, through the
// askpass in a directory with a space; a wrong one is asked again.
func TestRealSSH(t *testing.T) {
	host, password := os.Getenv("GOREX_TEST_PASSWORD_HOST"), os.Getenv("GOREX_TEST_PASSWORD")
	if host == "" {
		t.Skip("GOREX_TEST_PASSWORD_HOST is not set")
	}
	ssh, err := openssh.Path()
	if err != nil {
		t.Skip(err)
	}
	exe := askpassExe(t)
	s := listen(t)
	var mu sync.Mutex
	var asked []Prompt
	tries := []string{"wrong", password}
	se := s.Session(func(_ context.Context, p Prompt) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, p)
		if p.Kind == HostKey {
			return "yes", true
		}
		a := tries[0]
		tries = tries[1:]
		return a, true
	})
	defer se.Close()
	known := filepath.Join(t.TempDir(), "known_hosts")
	args := append(slices.Clone(openssh.Options), "-o", "UserKnownHostsFile="+known, "-o", "PubkeyAuthentication=no",
		"-o", "PreferredAuthentications=password,keyboard-interactive", "--", host, "echo gorex-ok")
	cmd := exec.Command(ssh, args...)
	cmd.Env = append(os.Environ(), se.Env(exe)...)
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	openssh.Detach(cmd)
	done := make(chan struct{})
	var out []byte
	go func() {
		out, err = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		cmd.Process.Kill()
		t.Fatal("ssh did not finish in 60 seconds")
	}
	if err != nil || !strings.Contains(string(out), "gorex-ok") {
		t.Fatalf("ssh: %v, %q; asked %+v", err, out, asked)
	}
	if len(asked) != 3 || asked[0].Kind != HostKey || asked[1].Kind != Secret || asked[1].Again || !asked[2].Again {
		t.Errorf("asked %+v", asked)
	}
	for _, p := range asked {
		t.Logf("%v %q again=%v", p.Kind, p.Text, p.Again)
	}
}
