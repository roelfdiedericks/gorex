package remote

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestReadHandshake(t *testing.T) {
	const binary = "\x00\x01yamux"
	r := bufio.NewReader(strings.NewReader("motd line\r\nwelcome\nGOREX-BRIDGE /root/.cache/gorex/ab/gorex\n" + binary))
	kind, value, err := readHandshake(r)
	if err != nil || kind != "bridge" || value != "/root/.cache/gorex/ab/gorex" {
		t.Fatalf("got %q %q %v", kind, value, err)
	}
	// What follows the ready line is the multiplexer's, left unread.
	if rest, _ := io.ReadAll(r); string(rest) != binary {
		t.Errorf("left %q", rest)
	}

	kind, value, err = readHandshake(bufio.NewReader(strings.NewReader("GOREX-DEPLOY Linux x86_64\n")))
	if err != nil || kind != "deploy" || value != "Linux x86_64" {
		t.Errorf("got %q %q %v", kind, value, err)
	}

	if kind, value, err = readHandshake(bufio.NewReader(strings.NewReader("only noise\n"))); err == nil {
		t.Errorf("noise read as %q %q", kind, value)
	}
}
