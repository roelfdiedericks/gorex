package ppk

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// The files in testdata are throwaway keys PuTTYgen 0.81 wrote, in both
// formats, both Argon2 flavors and every key type ssh's format holds;
// public-openssh.txt has the public key PuTTYgen tells of each.
const passphrase = "gorex-test"

func publicLines(t *testing.T) map[string]string {
	b, err := os.ReadFile(filepath.Join("testdata", "public-openssh.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(strings.ReplaceAll(string(b), "\r", "")), "\n") {
		f := strings.Fields(l)
		lines[f[2]] = l
	}
	return lines
}

// Each key file gives its key: the public half without the passphrase, the
// private one with it (and not with a wrong one), and in OpenSSH's format
// the same key, under the same passphrase, with PuTTYgen's public line.
func TestConvert(t *testing.T) {
	want := publicLines(t)
	files, _ := filepath.Glob(filepath.Join("testdata", "*.ppk"))
	if len(files) != len(want) {
		t.Fatalf("%d key files, %d public lines", len(files), len(want))
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".ppk")
		b, _ := os.ReadFile(path)
		f, err := Parse(b)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		pub, err := f.PublicKey()
		if err != nil {
			t.Errorf("%s: public key: %v", name, err)
			continue
		}
		if got := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + f.Comment; got != want[name] {
			t.Errorf("%s: public key %q, want %q", name, got, want[name])
		}
		pass := []byte(nil)
		if f.Encrypted() {
			pass = []byte(passphrase)
			if _, err := f.Decrypt([]byte("not it")); !errors.Is(err, ErrWrongPassphrase) {
				t.Errorf("%s: a wrong passphrase gives %v", name, err)
			}
		}
		k, err := f.Decrypt(pass)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		priv, line, err := OpenSSH(k, f.Comment, pass)
		if err != nil {
			t.Errorf("%s: OpenSSH: %v", name, err)
			continue
		}
		if got := strings.TrimSpace(string(line)); got != want[name] {
			t.Errorf("%s: .pub %q, want %q", name, got, want[name])
		}
		var back any
		if f.Encrypted() {
			var missing *ssh.PassphraseMissingError
			if _, err := ssh.ParseRawPrivateKey(priv); !errors.As(err, &missing) {
				t.Errorf("%s: the converted key opens without its passphrase (%v)", name, err)
			}
			back, err = ssh.ParseRawPrivateKeyWithPassphrase(priv, pass)
		} else {
			back, err = ssh.ParseRawPrivateKey(priv)
		}
		if err != nil {
			t.Errorf("%s: ssh can't read the converted key: %v", name, err)
			continue
		}
		signer, err := ssh.NewSignerFromKey(back)
		if err != nil || !bytes.Equal(signer.PublicKey().Marshal(), pub.Marshal()) {
			t.Errorf("%s: the converted key isn't the same key (%v)", name, err)
		}
	}
}

// Files that are not PuTTY private keys say what they are; a damaged one
// fails, and Windows' line ends read as Unix's.
func TestParseOthers(t *testing.T) {
	for _, c := range []struct {
		text string
		want error
	}{
		{"---- BEGIN SSH2 PUBLIC KEY ----\nComment: \"imported-openssh-key\"\nAAAA\n---- END SSH2 PUBLIC KEY ----\n", ErrPublicOnly},
		{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFuQ me\n", ErrPublicOnly},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbg==\n-----END OPENSSH PRIVATE KEY-----\n", ErrOpenSSH},
	} {
		if _, err := Parse([]byte(c.text)); !errors.Is(err, c.want) {
			t.Errorf("%.30q: %v, want %v", c.text, err, c.want)
		}
	}
	if _, err := Parse([]byte("hello\n")); err == nil {
		t.Error("a file of no key parses")
	}
	b, _ := os.ReadFile(filepath.Join("testdata", "ed25519-v3-plain.ppk"))
	b = bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
	f, err := Parse(b)
	if err != nil {
		t.Fatalf("CRLF: %v", err)
	}
	if _, err := f.Decrypt(nil); err != nil {
		t.Errorf("CRLF: %v", err)
	}
	// One hex digit of the MAC changed.
	damaged := bytes.Replace(b, []byte("Private-MAC: ae"), []byte("Private-MAC: af"), 1)
	f, err = Parse(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Decrypt(nil); err == nil || errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("a damaged unencrypted key gives %v", err)
	}
}
