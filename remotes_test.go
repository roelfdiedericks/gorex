package main

import (
	"strings"
	"testing"
)

// gorex ssh takes ssh's -A and -a before the destination, the last one
// deciding, and refuses other options, no destination, or more than one.
func TestSSHArgs(t *testing.T) {
	for _, c := range []struct {
		args        string
		agent, spec string
		err         bool
	}{
		{"me@box", "", "me@box", false},
		{"-A me@box", "yes", "me@box", false},
		{"-a me@box", "no", "me@box", false},
		{"-A -a me@box", "no", "me@box", false},
		{"-X me@box", "", "", true},
		{"-A", "", "", true},
		{"me@box other", "", "", true},
	} {
		agent, spec, err := sshArgs(strings.Fields(c.args))
		if agent != c.agent || spec != c.spec || (err != nil) != c.err {
			t.Errorf("%q: %q %q %v", c.args, agent, spec, err)
		}
	}
}
