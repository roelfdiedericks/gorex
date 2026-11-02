package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The folder of the servers on a host, as a host's settings name it, is
// the one the host's sh makes of it: ~ the user's home, any other
// characters as they are.
func TestServerDir(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	for dir, want := range map[string]string{
		"":                "/home/me/.cache/gorex",
		"~":               "/home/me",
		"~/my servers":    "/home/me/my servers",
		"/opt/it's $HOME": "/opt/it's $HOME",
		"rel/dir":         "rel/dir",
	} {
		cmd := exec.Command(sh, "-c", "c="+serverDir(dir)+`; printf '%s' "$c"`)
		cmd.Env = append(os.Environ(), "HOME=/home/me")
		out, err := cmd.Output()
		if err != nil || string(out) != want {
			t.Errorf("serverDir(%q) gives %q (%v), want %q", dir, out, err, want)
		}
	}
}

// A folder the servers land in is the user's own, or not used: mine tells
// one of the user's from one of another's (root's /).
func TestMine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a POSIX host's test")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	own := t.TempDir()
	cases := map[string]bool{own: true, filepath.Join(own, "none"): false}
	if os.Getuid() != 0 {
		cases["/"] = false
	}
	for dir, want := range cases {
		err := exec.Command(sh, "-c", "c="+serverDir(dir)+"; "+mine).Run()
		if got := err == nil; got != want {
			t.Errorf("mine of %s: %v, want %v (%v)", dir, got, want, err)
		}
	}
}

// The search for a folder of the server answers with the first folder of
// the user's where a script runs, made if need be, past one that can't be
// made (under a file) and one of another user's; and with none when none
// does.
func TestFindDirScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a POSIX host's test")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	tmp := t.TempDir()
	file := filepath.Join(tmp, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	made := filepath.Join(tmp, "new", "gorex")
	run := func(cands ...string) (string, bool) {
		var words []string
		for _, c := range cands {
			words = append(words, serverDir(c))
		}
		out, err := exec.Command(sh, "-c", findDirScript(words)).Output()
		if err != nil {
			t.Fatalf("%v", err)
		}
		return dirAnswer(string(out))
	}
	cands := []string{filepath.Join(file, "gorex"), made}
	if os.Getuid() != 0 {
		cands = append([]string{"/"}, cands...)
	}
	if dir, ok := run(cands...); !ok || dir != made {
		t.Errorf("found %q (%v), want %q", dir, ok, made)
	}
	if st, err := os.Stat(made); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("the folder made: %v, %v; want 0700", st, err)
	}
	if entries, _ := os.ReadDir(made); len(entries) != 0 {
		t.Errorf("the probe left %v", entries)
	}
	if dir, ok := run(filepath.Join(file, "gorex")); !ok || dir != "" {
		t.Errorf("with no folder to use: %q (%v), want none", dir, ok)
	}
}

func TestPlatformOf(t *testing.T) {
	for system, want := range map[string]string{
		"Linux x86_64":    "linux-amd64",
		"Linux amd64":     "linux-amd64",
		"Linux aarch64":   "linux-arm64",
		"Linux arm64":     "linux-arm64",
		"Darwin x86_64":   "darwin-amd64",
		"Darwin arm64":    "darwin-arm64",
		" Darwin  arm64 ": "darwin-arm64",
		"Linux armv7l":    "",
		"FreeBSD amd64":   "",
		"Darwin":          "",
		"":                "",
	} {
		p, ok := platformOf(system)
		if ok != (want != "") || p.name != want {
			t.Errorf("platformOf(%q) = %q, %v; want %q", system, p.name, ok, want)
		}
	}
}
