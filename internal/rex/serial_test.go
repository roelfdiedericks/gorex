//go:build linux || darwin

package rex

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// device is a pseudo-terminal pair standing in for a serial device: the
// tty opens its slave as the port, and the test is the device at the
// master.
func device(t *testing.T) (*os.File, string) {
	t.Helper()
	master, slave, err := openMaster()
	if err != nil {
		t.Skip(err)
	}
	return master, slave
}

// reader gathers what a serialTTY reads, as the session's reader does.
type reader struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func readAll(t *testing.T, tt *serialTTY) *reader {
	r := &reader{mu: make(chan struct{}, 1)}
	r.mu <- struct{}{}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := tt.read(b)
			<-r.mu
			r.buf.Write(b[:n])
			r.mu <- struct{}{}
			if err != nil {
				return
			}
		}
	}()
	return r
}

// until waits for the tty to have read s.
func (r *reader) until(t *testing.T, s string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		<-r.mu
		got := r.buf.String()
		r.mu <- struct{}{}
		if strings.Contains(got, s) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("read %q, want %q in it", got, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// fromDevice waits for the device to have read s.
func fromDevice(t *testing.T, master *os.File, s string) {
	t.Helper()
	master.SetReadDeadline(time.Now().Add(5 * time.Second))
	var got []byte
	b := make([]byte, 256)
	for !bytes.Contains(got, []byte(s)) {
		n, err := master.Read(b)
		if err != nil {
			t.Fatalf("the device read %q, want %q: %v", got, s, err)
		}
		got = append(got, b[:n]...)
	}
}

// A serial session's tty says it connected, reads what the device sends,
// sends what is typed with Enter as its options say, and the terminal's
// answers as they are. Released, it sends nothing and says so, and Enter
// opens the port again. When the device goes, it says it waits for it.
func TestSerialTTY(t *testing.T) {
	master, slave := device(t)
	defer master.Close()
	tt := openSerial(SerialOptions{Path: slave, Baud: 115200, DataBits: 8, Parity: "N", StopBits: "1", Enter: "crlf"})
	defer tt.close()
	if path, state, _ := tt.status(); path != slave || state != "open" {
		t.Fatalf("status %q %q, want %q open", path, state, slave)
	}
	r := readAll(t, tt)
	r.until(t, "Connected to "+slave+" at 115200 8N1")

	master.Write([]byte("switch> "))
	r.until(t, "switch> ")

	tt.input([]byte("show ver\r"))
	fromDevice(t, master, "show ver\r\n")
	tt.write([]byte("\x1b[?1;2c"))
	fromDevice(t, master, "\x1b[?1;2c")

	tt.release()
	r.until(t, slave+" was released. Press Enter to connect again.")
	if _, state, _ := tt.status(); state != "released" {
		t.Fatalf("released, state %q", state)
	}
	tt.input([]byte("x"))
	tt.input([]byte("\r"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, state, _ := tt.status(); state == "open" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Enter did not open the port again")
		}
		time.Sleep(20 * time.Millisecond)
	}
	tt.input([]byte("y\r"))
	fromDevice(t, master, "y\r\n")

	master.Close()
	r.until(t, slave+" was disconnected. Waiting for it to come back.")
}

// Echo shows what is typed, and Backspace as BS goes as BS but shows as
// a rubout.
func TestSerialTTYEcho(t *testing.T) {
	master, slave := device(t)
	defer master.Close()
	tt := openSerial(SerialOptions{Path: slave, Baud: 9600, DataBits: 7, Parity: "E", StopBits: "1", Backspace: "bs", Echo: true})
	defer tt.close()
	r := readAll(t, tt)
	r.until(t, "at 9600 7E1")
	tt.input([]byte("ab\x7f\r"))
	fromDevice(t, master, "ab\b\r")
	r.until(t, "ab\b \b\r\n")
}
