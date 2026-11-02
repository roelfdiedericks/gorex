package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A session's host is as PuTTY makes it: a user@ in HostName over
// UserName, the Default Settings' user for a session without one (but one
// that asks for the system's), a :port after the host dropped, the port
// PortNumber's; ssh sessions only, of a host.
func TestSessionHost(t *testing.T) {
	defaults := map[string]string{"UserName": "root"}
	sessions := map[string]puttySession{
		"bastion": {Name: "bastion", Values: map[string]string{"HostName": "jump.example.com", "PortNumber": "2200", "UserName": "ops"}},
	}
	for _, c := range []struct {
		situation string
		values    map[string]string
		ok        bool
		spec      string
		options   []string
		agent     bool
		identity  string
		puttyKey  string
		notes     int
	}{
		{"user and port", map[string]string{"HostName": "192.168.0.11", "Protocol": "ssh", "PortNumber": "2222", "UserName": "me", "AgentFwd": "1"},
			true, "me@192.168.0.11", []string{"-p", "2222"}, true, "", "", 0},
		{"user in the host name wins, its port dropped", map[string]string{"HostName": " admin@box:2200 ", "UserName": "me", "PortNumber": "22"},
			true, "admin@box", nil, false, "", "", 0},
		{"no user: the Default Settings'", map[string]string{"HostName": "box", "Protocol": "ssh"},
			true, "root@box", nil, false, "", "", 1},
		{"no user, the system's asked for", map[string]string{"HostName": "box", "UserNameFromEnvironment": "1"},
			true, "box", nil, false, "", "", 0},
		{"an IPv6 address", map[string]string{"HostName": "fe80::1", "UserName": "me"},
			true, "me@fe80::1", nil, false, "", "", 0},
		{"a bracketed IPv6 address with a port", map[string]string{"HostName": "[fe80::1]:22", "UserName": "me"},
			true, "me@fe80::1", nil, false, "", "", 0},
		{"jump host by session", map[string]string{"HostName": "inner", "UserName": "me", "ProxyMethod": "6", "ProxyHost": "bastion"},
			true, "me@inner", []string{"-J", "ops@jump.example.com:2200"}, false, "", "", 0},
		{"jump host by name, its user", map[string]string{"HostName": "inner", "UserName": "me", "ProxyMethod": "6", "ProxyHost": "gw", "ProxyPort": "22", "ProxyUsername": "u"},
			true, "me@inner", []string{"-J", "u@gw"}, false, "", "", 0},
		{"a SOCKS proxy, noted", map[string]string{"HostName": "box", "UserName": "me", "ProxyMethod": "2", "ProxyHost": "p"},
			true, "me@box", nil, false, "", "", 1},
		{"a PuTTY key, noted", map[string]string{"HostName": "box", "UserName": "me", "PublicKeyFile": `C:\keys\id.ppk`},
			true, "me@box", nil, false, "", `C:\keys\id.ppk`, 1},
		{"an OpenSSH key", map[string]string{"HostName": "box", "UserName": "me", "PublicKeyFile": `C:\keys\id_ed25519`},
			true, "me@box", nil, false, `C:\keys\id_ed25519`, "", 0},
		{"compression and IPv6 only", map[string]string{"HostName": "box", "UserName": "me", "Compression": "1", "AddressFamily": "2"},
			true, "me@box", []string{"-C", "-6"}, false, "", "", 0},
		{"telnet", map[string]string{"HostName": "box", "Protocol": "telnet"}, false, "", nil, false, "", "", 0},
		{"serial", map[string]string{"Protocol": "serial", "SerialLine": "COM1"}, false, "", nil, false, "", "", 0},
		{"no host", map[string]string{"Protocol": "ssh", "UserName": "me"}, false, "", nil, false, "", "", 0},
	} {
		got, ok := sessionHost(puttySession{Name: "s", Values: c.values}, defaults, sessions)
		if ok != c.ok {
			t.Errorf("%s: ok %v, want %v", c.situation, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.spec != c.spec || !slices.Equal(got.options, c.options) || got.agent != c.agent ||
			got.identity != c.identity || got.puttyKey != c.puttyKey || len(got.notes) != c.notes {
			t.Errorf("%s: %q %q agent %v identity %q key %q notes %q, want %q %q %v %q %q %d notes",
				c.situation, got.spec, got.options, got.agent, got.identity, got.puttyKey, got.notes,
				c.spec, c.options, c.agent, c.identity, c.puttyKey, c.notes)
		}
	}
	if _, ok := sessionHost(puttySession{Name: importDefaultSettings, Values: map[string]string{"HostName": "box"}}, nil, nil); ok {
		t.Error("Default Settings became a host")
	}
}

// A KiTTY folder's session file is a line a value, Key\value\, its bytes
// %XX escaped, a value's last backslash missing at times.
func TestKittyFile(t *testing.T) {
	b := []byte("HostName\\192.168.0.11\\\nPortNumber\\22\\\r\nPublicKeyFile\\C%3A%5Ckeys%5Cid.ppk\\\nUserName\\root\nnot a value\n")
	v := kittyFile(b, func(b []byte) string { return string(b) })
	want := map[string]string{"HostName": "192.168.0.11", "PortNumber": "22", "PublicKeyFile": `C:\keys\id.ppk`, "UserName": "root"}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
	if len(v) != len(want) {
		t.Errorf("%d values, want %d: %q", len(v), len(want), v)
	}
}

// kitty.ini says where sessions are in [KiTTY]: savemode and configdir.
func TestKittyIni(t *testing.T) {
	ini := "[Agent]\nsavemode=registry\n[KiTTY]\n; savemode: specify the save mode\n savemode = Dir \nconfigdir=conf\n[Print]\n"
	if mode, dir := kittyIni([]byte(ini)); mode != "dir" || dir != "conf" {
		t.Errorf("kittyIni = %q %q, want dir conf", mode, dir)
	}
	if mode, _ := kittyIni(nil); mode != "" {
		t.Errorf("no ini: %q", mode)
	}
}

// A KiTTY folder's sessions are its files, in its subfolders too, named
// as escaped; each source's Default Settings gives its sessions their
// user, and sessions of one source only.
func TestKittyFolder(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("Default%20Settings", "HostName\\\\\nUserName\\admin\\\n")
	write("spider", "HostName\\192.168.0.11\\\nProtocol\\ssh\\\n")
	write("Lab/my%20router", "HostName\\10.0.0.1\\\nProtocol\\ssh\\\nUserName\\cisco\\\n")
	write("Lab/console", "Protocol\\serial\\\nSerialLine\\COM1\\\n")
	sessions := kittyFolder(dir, "KiTTY folder", func(b []byte) string { return string(b) })
	sessions = append(sessions, puttySession{Name: "elsewhere", Source: "PuTTY", Values: map[string]string{"HostName": "e"}})
	var got []string
	for _, c := range importCandidates(sessions) {
		got = append(got, c.s.Source+"|"+c.s.Folder+"|"+c.s.Name+"|"+c.spec)
	}
	want := []string{"KiTTY folder||spider|admin@192.168.0.11", "KiTTY folder|Lab|my router|cisco@10.0.0.1", "PuTTY||elsewhere|e"}
	if !slices.Equal(got, want) {
		t.Errorf("candidates %q, want %q", got, want)
	}
}
