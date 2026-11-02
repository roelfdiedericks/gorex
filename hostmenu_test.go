package main

import (
	"testing"

	"gorex/internal/remote"
)

// The address copied runs after "ssh ": no port on 22, ssh:// with the
// port otherwise, an IPv6 host in brackets there.
func TestSSHAddress(t *testing.T) {
	for _, c := range []struct {
		t    remote.Target
		want string
	}{
		{remote.Target{User: "root", Host: "192.168.0.180", Port: "22"}, "root@192.168.0.180"},
		{remote.Target{User: "me", Host: "box", Port: "2222"}, "ssh://me@box:2222"},
		{remote.Target{User: "me", Host: "fe80::1", Port: "2222"}, "ssh://me@[fe80::1]:2222"},
	} {
		if got := sshAddress(c.t); got != c.want {
			t.Errorf("sshAddress(%+v) = %q, want %q", c.t, got, c.want)
		}
	}
}

// The command copied reaches the host as GoRex does: its options without
// a port of their own, the port ssh resolved when not 22, -A when the
// agent goes along, and words with spaces quoted.
func TestSSHCommand(t *testing.T) {
	for _, c := range []struct {
		t    remote.Target
		want string
	}{
		{remote.Target{User: "root", Host: "192.168.0.180", Port: "22", Agent: "no"}, "ssh root@192.168.0.180"},
		{remote.Target{User: "me", Host: "box", Port: "2222", Agent: "yes", Opts: []string{"-p", "2222", "-i", "~/.ssh/id_box"}},
			"ssh -i ~/.ssh/id_box -p 2222 -A me@box"},
		{remote.Target{User: "me", Host: "box", Port: "2200", Agent: "no", Opts: []string{"-p2200", "-o", "ProxyJump=bastion"}},
			"ssh -o ProxyJump=bastion -p 2200 me@box"},
		{remote.Target{User: "me", Host: "box", Port: "22", Agent: "no", Opts: []string{"-i", `C:\Users\Me\My Keys\id`}},
			`ssh -i 'C:\Users\Me\My Keys\id' me@box`},
	} {
		if got := sshCommand(c.t); got != c.want {
			t.Errorf("sshCommand(%+v) = %q, want %q", c.t, got, c.want)
		}
	}
}
