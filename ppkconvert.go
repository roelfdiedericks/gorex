package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/openssh"
	"gorex/internal/ppk"
)

// PuTTY's key files (.ppk), which ssh can't read: GoRex tells when the key
// is in the agent ssh uses already (Pageant, as it holds the keys PuTTY
// logs in with), else converts the file into the user's .ssh folder in
// OpenSSH's format, with its passphrase. The host dialog does so for a host
// imported with one, or an identity file chosen as one; the palette for any
// (Convert PuTTY Key…).

// What a PuTTY key file is for ssh (ppkInfo.kind).
const (
	ppkConvert    = iota // to convert
	ppkInAgent           // in the agent ssh uses: nothing to do
	ppkPublicOnly        // a public key only
	ppkOpenSSH           // in ssh's own format already
	ppkBad               // not readable as a key
)

// ppkInfo is what checkPPK made of a key file: its kind, the agent that
// holds it, whether it needs a passphrase, or why it can't be used.
type ppkInfo struct {
	kind      int
	agent     string
	encrypted bool
	err       string
}

// isPPK tells a path of a PuTTY key file, by its extension.
func isPPK(path string) bool { return strings.EqualFold(filepath.Ext(strings.TrimSpace(path)), ".ppk") }

// checkPPK reads the key file at path and asks the agent ssh uses whether
// it holds its key, which takes a moment: not on the UI's thread.
func checkPPK(path string) ppkInfo {
	b, err := os.ReadFile(path)
	if err != nil {
		return ppkInfo{kind: ppkBad, err: err.Error()}
	}
	f, err := ppk.Parse(b)
	switch {
	case errors.Is(err, ppk.ErrPublicOnly):
		return ppkInfo{kind: ppkPublicOnly}
	case errors.Is(err, ppk.ErrOpenSSH):
		return ppkInfo{kind: ppkOpenSSH}
	case err != nil:
		return ppkInfo{kind: ppkBad, err: err.Error()}
	}
	if name, sock := openssh.AgentInUse(); sock != "" {
		if keys, err := agentKeys(sock); err == nil {
			for _, k := range keys {
				if bytes.Equal(k.Blob, f.Public) {
					return ppkInfo{kind: ppkInAgent, agent: name}
				}
			}
		}
	}
	return ppkInfo{kind: ppkConvert, encrypted: f.Encrypted()}
}

// agentKeys lists the keys of the agent at sock, giving up on one that
// doesn't answer in 5 seconds.
func agentKeys(sock string) ([]ppkAgentKey, error) {
	type result struct {
		keys []ppkAgentKey
		err  error
	}
	done := make(chan result, 1)
	go func() {
		keys, err := openssh.AgentKeys(sock)
		var out []ppkAgentKey
		for _, k := range keys {
			out = append(out, ppkAgentKey{Blob: k.Blob})
		}
		done <- result{out, err}
	}()
	select {
	case r := <-done:
		return r.keys, r.err
	case <-time.After(5 * time.Second):
		return nil, errors.New("the agent doesn't answer")
	}
}

// ppkAgentKey is a key an agent holds, in ssh's encoding.
type ppkAgentKey struct{ Blob []byte }

// convertPPK converts the PuTTY key file at path into the user's .ssh
// folder, in OpenSSH's format with the same passphrase, beside its public
// key (.pub), named as the file without .ppk, or with -2, -3… where that
// is taken: ssh refuses a key file other users can read, as one in a
// folder like C:\utils may be. It returns the private key's path.
func convertPPK(path, passphrase string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	f, err := ppk.Parse(b)
	if err != nil {
		return "", err
	}
	k, err := f.Decrypt([]byte(passphrase))
	if err != nil {
		return "", err
	}
	if !f.Encrypted() {
		passphrase = ""
	}
	priv, pub, err := ppk.OpenSSH(k, f.Comment, []byte(passphrase))
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	out := filepath.Join(dir, base)
	for n := 2; exists(out) || exists(out+".pub"); n++ {
		out = filepath.Join(dir, base+"-"+strconv.Itoa(n))
	}
	if err := os.WriteFile(out, priv, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(out+".pub", pub, 0o644); err != nil {
		os.Remove(out)
		return "", err
	}
	return out, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// ppkForm is the state of a PuTTY key's panel: its file, what checkPPK
// made of it (nil while it reads), the passphrase typed, and how the
// conversion goes: busy, why it failed, or the file it wrote.
type ppkForm struct {
	path string
	info *ppkInfo
	pass string
	busy bool
	err  string
	done string
}

// newPPKForm starts reading the key file at path for its panel.
func (a *App) newPPKForm(path string) *ppkForm {
	f := &ppkForm{path: path}
	win := a.win
	go func() {
		info := checkPPK(path)
		set := func() {
			f.info = &info
			a.changed()
		}
		if win == nil {
			set()
			return
		}
		win.Update(set)
	}()
	return f
}

// ppkPanel draws what a PuTTY key file is for ssh, with what to do: to
// convert it (its passphrase first, when it has one), or to use it as it
// is; use takes the file ssh is to use. pad indents its lines, as the host
// dialog's help.
func (a *App) ppkPanel(c *ui.Context, k *colors, f *ppkForm, pad float32, use func(string)) {
	text := func(s string, col ui.Color) {
		ui.Text(c, s).FontSize(11.5).TextColor(col).LineHeight(1.35).Selectable()
	}
	ui.Column(c).Gap(6).Padding(0, 0, 0, pad).Children(func() {
		switch {
		case f.done != "":
			text("Converted to "+f.done+" (plus its .pub file). ssh uses the converted key.", k.textFaint)
			return
		case f.info == nil:
			text("Reading "+f.path+"…", k.textFaint)
			return
		}
		switch f.info.kind {
		case ppkInAgent:
			text("PuTTY key "+f.path+" is already in "+f.info.agent+". ssh uses it from there, so there is nothing to convert.", k.textFaint)
		case ppkPublicOnly:
			text(f.path+" contains only a public key. ssh needs the private key, which is the .ppk file that PuTTYgen saved.", k.attention)
		case ppkOpenSSH:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				text(f.path+" is already in ssh's format.", k.textFaint)
				if textButton(c, k, "Use It").Clicked() {
					use(f.path)
				}
			})
		case ppkBad:
			text("Can't use "+f.path+": "+f.info.err, k.attention)
		case ppkConvert:
			msg := "PuTTY key " + f.path + " isn't in ssh's format. GoRex can convert it and save it in your .ssh folder"
			if f.info.encrypted {
				msg += ", with the same passphrase."
			} else {
				msg += "."
			}
			text(msg, k.textFaint)
			convert := func() {
				if f.busy || (f.info.encrypted && f.pass == "") {
					return
				}
				f.busy, f.err = true, ""
				win, path, pass := a.win, f.path, f.pass
				go func() {
					out, err := convertPPK(path, pass)
					set := func() {
						f.busy = false
						if err != nil {
							f.err = err.Error()
						} else {
							f.done, f.pass = out, ""
							use(out)
						}
						a.changed()
					}
					if win == nil {
						set()
						return
					}
					win.Update(set)
				}()
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if f.info.encrypted {
					in := ui.TextInput(c, &f.pass).Password().Width(220).FontSize(12.5).Placeholder("Key passphrase")
					if in.Submitted() {
						convert()
					}
				}
				label := "Convert"
				if f.busy {
					label = "Converting…"
				}
				if textButton(c, k, label).Clicked() {
					convert()
				}
			})
			if f.err != "" {
				text("Can't convert the key: "+f.err, ui.Hex("#ef4444"))
			}
		}
	})
}

// convertKeyDialog is the palette's Convert PuTTY Key… dialog.
func (a *App) convertKeyDialog(c *ui.Context, k *colors) {
	f := a.convertKey
	if f == nil {
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(560).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Padding(18, 20).Gap(12)
		ui.Text(c, "Convert a PuTTY key").FontSize(14).FontWeight(700).TextColor(k.text)
		a.ppkPanel(c, k, f, 0, func(string) {})
		ui.Row(c).Children(func() {
			ui.Box(c).Grow(1)
			label := "Cancel"
			if f.done != "" || (f.info != nil && f.info.kind != ppkConvert) {
				label = "Done"
			}
			if textButton(c, k, label).Clicked() {
				a.convertKey = nil
			}
		})
	})
	if !open {
		a.convertKey = nil
	}
}

// chooseKeyToConvert asks for a PuTTY key file, and opens the dialog that
// converts it.
func (a *App) chooseKeyToConvert() {
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "PuTTY key to convert",
			Filters: []mygo.FileFilter{{Name: "PuTTY keys", Extensions: []string{"ppk"}}}})
		if err != nil || len(paths) == 0 || win == nil {
			return
		}
		win.Update(func() {
			a.convertKey = a.newPPKForm(paths[0])
			a.changed()
		})
	}()
}
