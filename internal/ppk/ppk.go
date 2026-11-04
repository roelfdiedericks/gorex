// Package ppk reads the private key files of PuTTY (.ppk), in the formats
// PuTTYgen writes (2 and 3), and writes their keys in OpenSSH's format,
// which ssh reads. It follows PuTTY's sshpubk.c: a file is headers, the
// public key and the private one in base64, and a MAC over both; the
// private key is encrypted with AES-256-CBC under a key made of the
// passphrase, with SHA-1 (format 2) or Argon2 (format 3).
package ppk

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

var (
	// ErrPublicOnly is the error of a file that holds a public key only.
	ErrPublicOnly = errors.New("it holds only a public key, not the private key that ssh needs")
	// ErrOpenSSH is the error of a file in a format ssh reads as it is.
	ErrOpenSSH = errors.New("it is already in the OpenSSH format")
	// ErrWrongPassphrase is the error of a wrong passphrase.
	ErrWrongPassphrase = errors.New("wrong passphrase")
)

// File is a PuTTY private key file: its format version, the key's
// algorithm (ssh-ed25519), its encryption ("none" or "aes256-cbc"),
// comment and public key (in ssh's encoding), and what decrypts and checks
// the private key.
type File struct {
	Version                        int
	Algorithm, Encryption, Comment string
	Public                         []byte
	private, mac                   []byte
	// The Argon2 parameters of an encrypted format 3 file.
	kdf            string
	memory, passes uint32
	parallelism    uint8
	salt           []byte
}

// Parse reads a .ppk file. A file that holds a public key only gives
// ErrPublicOnly, and one in ssh's own format ErrOpenSSH.
func Parse(b []byte) (*File, error) {
	text := strings.TrimLeft(string(b), "\ufeff \t\r\n")
	switch {
	case strings.HasPrefix(text, "---- BEGIN SSH2 PUBLIC KEY"), strings.HasPrefix(text, "ssh-"), strings.HasPrefix(text, "ecdsa-"):
		return nil, ErrPublicOnly
	case strings.HasPrefix(text, "-----BEGIN "):
		return nil, ErrOpenSSH
	case !strings.HasPrefix(text, "PuTTY-User-Key-File-"):
		return nil, errors.New("it is not a PuTTY key file")
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(nil, 1<<20)
	// header reads the next line, which must be name: value.
	header := func(name string) (string, error) {
		if !sc.Scan() {
			return "", fmt.Errorf("the file ends before %s", name)
		}
		n, v, ok := strings.Cut(strings.TrimRight(sc.Text(), "\r"), ": ")
		if !ok || n != name {
			return "", fmt.Errorf("expected %s, found %q", name, sc.Text())
		}
		return v, nil
	}
	// lines reads the base64 body of a Public-Lines or Private-Lines.
	lines := func(name string) ([]byte, error) {
		v, err := header(name)
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 1<<14 {
			return nil, fmt.Errorf("invalid %s %q", name, v)
		}
		var s strings.Builder
		for range n {
			if !sc.Scan() {
				return nil, fmt.Errorf("the file ends in the middle of %s", name)
			}
			s.WriteString(strings.TrimSpace(sc.Text()))
		}
		return base64.StdEncoding.DecodeString(s.String())
	}
	f := &File{}
	first, _, _ := strings.Cut(text, "\n")
	ver, alg, ok := strings.Cut(strings.TrimRight(first, "\r"), ": ")
	if !ok {
		return nil, errors.New("it is not a PuTTY key file")
	}
	sc.Scan()
	switch ver {
	case "PuTTY-User-Key-File-2":
		f.Version = 2
	case "PuTTY-User-Key-File-3":
		f.Version = 3
	case "PuTTY-User-Key-File-1":
		return nil, errors.New("it uses the first PuTTY key format; open it in PuTTYgen and save it again")
	default:
		return nil, errors.New("it uses a PuTTY key format that is newer than this GoRex supports")
	}
	f.Algorithm = alg
	var err error
	if f.Encryption, err = header("Encryption"); err != nil {
		return nil, err
	}
	if f.Encryption != "none" && f.Encryption != "aes256-cbc" {
		return nil, fmt.Errorf("it uses an unknown encryption, %q", f.Encryption)
	}
	if f.Comment, err = header("Comment"); err != nil {
		return nil, err
	}
	if f.Public, err = lines("Public-Lines"); err != nil {
		return nil, err
	}
	if f.Version == 3 && f.Encrypted() {
		if f.kdf, err = header("Key-Derivation"); err != nil {
			return nil, err
		}
		num := func(name string, max uint64) (uint64, error) {
			v, err := header(name)
			if err != nil {
				return 0, err
			}
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil || n == 0 || n > max {
				return 0, fmt.Errorf("invalid %s %q", name, v)
			}
			return n, nil
		}
		m, err := num("Argon2-Memory", 1<<22)
		if err != nil {
			return nil, err
		}
		p, err := num("Argon2-Passes", 1<<16)
		if err != nil {
			return nil, err
		}
		par, err := num("Argon2-Parallelism", 255)
		if err != nil {
			return nil, err
		}
		f.memory, f.passes, f.parallelism = uint32(m), uint32(p), uint8(par)
		s, err := header("Argon2-Salt")
		if err != nil {
			return nil, err
		}
		if f.salt, err = hex.DecodeString(s); err != nil {
			return nil, fmt.Errorf("invalid Argon2-Salt %q", s)
		}
	}
	if f.private, err = lines("Private-Lines"); err != nil {
		return nil, err
	}
	m, err := header("Private-MAC")
	if err != nil {
		return nil, err
	}
	if f.mac, err = hex.DecodeString(m); err != nil {
		return nil, fmt.Errorf("invalid Private-MAC %q", m)
	}
	return f, nil
}

// Encrypted tells whether the private key needs a passphrase.
func (f *File) Encrypted() bool { return f.Encryption != "none" }

// PublicKey is the key's public half, which needs no passphrase.
func (f *File) PublicKey() (ssh.PublicKey, error) { return ssh.ParsePublicKey(f.Public) }

// keys makes the cipher key, IV and MAC key of the passphrase, as
// ssh2_ppk_derive_keys does.
func (f *File) keys(passphrase []byte) (key, iv, macKey []byte, err error) {
	if f.Version == 2 {
		var k []byte
		for ctr := uint32(0); len(k) < 32; ctr++ {
			h := sha1.New()
			binary.Write(h, binary.BigEndian, ctr)
			h.Write(passphrase)
			k = h.Sum(k)
		}
		m := sha1.Sum(append([]byte("putty-private-key-file-mac-key"), passphrase...))
		return k[:32], make([]byte, aes.BlockSize), m[:], nil
	}
	if !f.Encrypted() {
		return nil, nil, nil, nil
	}
	const n = 32 + aes.BlockSize + 32
	var out []byte
	switch f.kdf {
	case "Argon2id":
		out = argon2.IDKey(passphrase, f.salt, f.passes, f.memory, f.parallelism, n)
	case "Argon2i":
		out = argon2.Key(passphrase, f.salt, f.passes, f.memory, f.parallelism, n)
	case "Argon2d":
		return nil, nil, nil, errors.New("its passphrase is hashed with Argon2d, which GoRex does not support; save it again in PuTTYgen with Argon2id (the PuTTYgen default)")
	default:
		return nil, nil, nil, fmt.Errorf("it uses an unknown key derivation, %q", f.kdf)
	}
	return out[:32], out[32 : 32+aes.BlockSize], out[32+aes.BlockSize:], nil
}

// Decrypt returns the private key, with passphrase when the file is
// encrypted: an *rsa.PrivateKey, *ecdsa.PrivateKey or ed25519.PrivateKey.
// A wrong passphrase gives ErrWrongPassphrase.
func (f *File) Decrypt(passphrase []byte) (crypto.PrivateKey, error) {
	if !f.Encrypted() {
		passphrase = nil
	}
	key, iv, macKey, err := f.keys(passphrase)
	if err != nil {
		return nil, err
	}
	priv := bytes.Clone(f.private)
	if f.Encrypted() {
		if len(priv)%aes.BlockSize != 0 {
			return nil, errors.New("its private key is damaged")
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(priv, priv)
	}
	newMAC := sha1.New
	if f.Version == 3 {
		newMAC = sha256.New
	}
	mac := hmac.New(newMAC, macKey)
	for _, s := range [][]byte{[]byte(f.Algorithm), []byte(f.Encryption), []byte(f.Comment), f.Public, priv} {
		binary.Write(mac, binary.BigEndian, uint32(len(s)))
		mac.Write(s)
	}
	if !hmac.Equal(mac.Sum(nil), f.mac) {
		if f.Encrypted() {
			return nil, ErrWrongPassphrase
		}
		return nil, errors.New("it is damaged (its MAC does not match)")
	}
	k, err := privateKey(f.Algorithm, f.Public, priv)
	if err != nil {
		return nil, err
	}
	// The private key must be the public one's.
	signer, err := ssh.NewSignerFromKey(k)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(signer.PublicKey().Marshal(), f.Public) {
		return nil, errors.New("its private key does not match its public key")
	}
	return k, nil
}

// privateKey makes the key of algorithm alg from its public and private
// halves, as PuTTY encodes them.
func privateKey(alg string, public, private []byte) (crypto.PrivateKey, error) {
	pub, priv := &reader{b: public}, &reader{b: private}
	pub.string() // the algorithm, again
	switch alg {
	case "ssh-rsa":
		e, n := pub.mpint(), pub.mpint()
		d, p, q := priv.mpint(), priv.mpint(), priv.mpint()
		if pub.err != nil || priv.err != nil || !e.IsInt64() {
			return nil, errors.New("its RSA key is damaged")
		}
		k := &rsa.PrivateKey{PublicKey: rsa.PublicKey{N: n, E: int(e.Int64())}, D: d, Primes: []*big.Int{p, q}}
		if err := k.Validate(); err != nil {
			return nil, fmt.Errorf("its RSA key is damaged: %w", err)
		}
		k.Precompute()
		return k, nil
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		curve := map[string]elliptic.Curve{
			"ecdsa-sha2-nistp256": elliptic.P256(), "ecdsa-sha2-nistp384": elliptic.P384(), "ecdsa-sha2-nistp521": elliptic.P521(),
		}[alg]
		d := priv.mpint()
		if priv.err != nil {
			return nil, errors.New("its ECDSA key is damaged")
		}
		size := (curve.Params().BitSize + 7) / 8
		if len(d.Bytes()) > size {
			return nil, errors.New("its ECDSA key is damaged")
		}
		return ecdsa.ParseRawPrivateKey(curve, d.FillBytes(make([]byte, size)))
	case "ssh-ed25519":
		// The private key is the seed, as OpenSSH's.
		seed := priv.string()
		if priv.err != nil || len(seed) != ed25519.SeedSize {
			return nil, errors.New("its Ed25519 key is damaged")
		}
		return ed25519.NewKeyFromSeed(seed), nil
	case "ssh-dss":
		return nil, errors.New("it is a DSA key, which ssh no longer accepts; make a new key in PuTTYgen")
	}
	return nil, fmt.Errorf("the ssh key format cannot store its key type, %s", alg)
}

// OpenSSH returns the private key in OpenSSH's format, encrypted with
// passphrase unless it is empty, as ssh-keygen writes it, and the public
// key's line, as authorized_keys takes it, with the comment.
func OpenSSH(k crypto.PrivateKey, comment string, passphrase []byte) (private, public []byte, err error) {
	var block *pem.Block
	if len(passphrase) > 0 {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(k, comment, passphrase)
	} else {
		block, err = ssh.MarshalPrivateKey(k, comment)
	}
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(k)
	if err != nil {
		return nil, nil, err
	}
	line := bytes.TrimSpace(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	if comment != "" {
		line = append(append(line, ' '), comment...)
	}
	return pem.EncodeToMemory(block), append(line, '\n'), nil
}

// reader reads ssh's encoding: strings, a length then bytes, and mpints,
// strings of a big-endian two's complement number.
type reader struct {
	b   []byte
	err error
}

func (r *reader) string() []byte {
	if r.err != nil || len(r.b) < 4 {
		r.err = errors.New("short")
		return nil
	}
	n := binary.BigEndian.Uint32(r.b)
	if uint64(n) > uint64(len(r.b)-4) {
		r.err = errors.New("short")
		return nil
	}
	s := r.b[4 : 4+n]
	r.b = r.b[4+n:]
	return s
}

func (r *reader) mpint() *big.Int {
	s := r.string()
	if len(s) > 0 && s[0]&0x80 != 0 {
		r.err = errors.New("negative")
	}
	return new(big.Int).SetBytes(s)
}
