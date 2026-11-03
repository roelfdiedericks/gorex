package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"gorex/internal/ppk"
)

// A PuTTY key converts into the user's .ssh folder under its own name,
// never over a key there (the next is -2), keeping its passphrase; a
// wrong passphrase writes nothing.
func TestConvertPPK(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	src := filepath.Join("internal", "ppk", "testdata", "ed25519-v3-argon2id.ppk")
	sshDir := filepath.Join(home, ".ssh")

	if _, err := convertPPK(src, "not it"); !errors.Is(err, ppk.ErrWrongPassphrase) {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	if entries, _ := os.ReadDir(sshDir); len(entries) != 0 {
		t.Fatalf("a wrong passphrase wrote %d files", len(entries))
	}
	first, err := convertPPK(src, "gorex-test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := convertPPK(src, "gorex-test")
	if err != nil {
		t.Fatal(err)
	}
	if first != filepath.Join(sshDir, "ed25519-v3-argon2id") || second != first+"-2" {
		t.Fatalf("converted to %s, then %s", first, second)
	}
	b, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	var missing *ssh.PassphraseMissingError
	if _, err := ssh.ParseRawPrivateKey(b); !errors.As(err, &missing) {
		t.Errorf("the converted key lost its passphrase (%v)", err)
	}
	if _, err := ssh.ParseRawPrivateKeyWithPassphrase(b, []byte("gorex-test")); err != nil {
		t.Errorf("the converted key doesn't open with its passphrase: %v", err)
	}
	if _, err := os.Stat(second + ".pub"); err != nil {
		t.Errorf("no public key beside it: %v", err)
	}
}

// checkPPK tells a key to convert, one that needs a passphrase, a public
// key only, and a file in ssh's format.
func TestCheckPPK(t *testing.T) {
	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.ppk")
	os.WriteFile(pub, []byte("---- BEGIN SSH2 PUBLIC KEY ----\nAAAA\n---- END SSH2 PUBLIC KEY ----\n"), 0o644)
	own := filepath.Join(dir, "own.ppk")
	os.WriteFile(own, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbg==\n-----END OPENSSH PRIVATE KEY-----\n"), 0o600)
	for _, c := range []struct {
		path      string
		kind      int
		encrypted bool
	}{
		{filepath.Join("internal", "ppk", "testdata", "ed25519-v3-plain.ppk"), ppkConvert, false},
		{filepath.Join("internal", "ppk", "testdata", "rsa1024-v2-aes.ppk"), ppkConvert, true},
		{pub, ppkPublicOnly, false},
		{own, ppkOpenSSH, false},
		{filepath.Join(dir, "missing.ppk"), ppkBad, false},
	} {
		if got := checkPPK(c.path); got.kind != c.kind || got.encrypted != c.encrypted {
			t.Errorf("%s: kind %d encrypted %v, want %d %v", filepath.Base(c.path), got.kind, got.encrypted, c.kind, c.encrypted)
		}
	}
}
