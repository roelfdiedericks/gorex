package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gorex ssh takes ssh's -A and -a, and --plain or --server, before the
// destination, the last of each deciding, and refuses other options, no
// destination, or more than one.
func TestSSHArgs(t *testing.T) {
	for _, c := range []struct {
		args               string
		agent, force, spec string
		err                bool
	}{
		{"me@box", "", "", "me@box", false},
		{"-A me@box", "yes", "", "me@box", false},
		{"-a me@box", "no", "", "me@box", false},
		{"-A -a me@box", "no", "", "me@box", false},
		{"--plain me@box", "", "plain", "me@box", false},
		{"-A --server me@box", "yes", "server", "me@box", false},
		{"--server --plain me@box", "", "plain", "me@box", false},
		{"-X me@box", "", "", "", true},
		{"-plain me@box", "", "", "", true},
		{"-A", "", "", "", true},
		{"--plain", "", "", "", true},
		{"me@box other", "", "", "", true},
	} {
		agent, force, spec, err := sshArgs(strings.Fields(c.args))
		if agent != c.agent || force != c.force || spec != c.spec || (err != nil) != c.err {
			t.Errorf("%q: %q %q %q %v", c.args, agent, force, spec, err)
		}
	}
}

// A host's ssh options split as a shell splits them: at spaces, but not
// within quotes, which go; an empty pair of quotes is an argument.
func TestSplitArgs(t *testing.T) {
	for in, want := range map[string][]string{
		"":                           nil,
		"  ":                         nil,
		"-p 2222":                    {"-p", "2222"},
		"-o  ProxyJump=bastion\t-4":  {"-o", "ProxyJump=bastion", "-4"},
		`-o "ProxyCommand=nc %h %p"`: {"-o", "ProxyCommand=nc %h %p"},
		`-i 'C:\Users\me\my key'`:    {"-i", `C:\Users\me\my key`},
		`-o 'User="x y"'`:            {"-o", `User="x y"`},
		`a"b c"d`:                    {"ab cd"},
		`-E ""`:                      {"-E", ""},
	} {
		if got := splitArgs(in); !slices.Equal(got, want) {
			t.Errorf("splitArgs(%q) = %q, want %q", in, got, want)
		}
	}
}

// A plain or serial host's server keeps its state in a folder of the
// app's named after the host's key, of characters every system takes in
// a name, one folder for each host.
func TestHostDir(t *testing.T) {
	keys := []string{"root@gear.cisp.co.za:22", "root@gear.cisp.co.za:2222", "me@[fe80::1%eth0]:22", "me@box:22", "serial-0a1b2c3d"}
	seen := map[string]string{}
	for _, key := range keys {
		name := filepath.Base(hostDir(key))
		if strings.ContainsAny(name, `:/\%[] `) {
			t.Errorf("hostDir(%q) is named %q", key, name)
		}
		if other, ok := seen[name]; ok {
			t.Errorf("%q and %q share the folder %q", key, other, name)
		}
		seen[name] = key
	}
}
