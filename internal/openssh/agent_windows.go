//go:build windows

package openssh

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os/user"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procCryptProtectMemory = windows.NewLazySystemDLL("crypt32.dll").NewProc("CryptProtectMemory")

// pageantName is the name of the pipe Pageant listens at, as PuTTY makes
// it (agent_named_pipe_name, capi_obfuscate_string): its user's name and a
// SHA-256 of "Pageant" encrypted with the user's key (CryptProtectMemory,
// across processes), which only a program of the user can tell.
var pageantName = sync.OnceValue(func() string {
	u := userName()
	if u == "" {
		return ""
	}
	const block = 16 // CRYPTPROTECTMEMORY_BLOCK_SIZE
	data := make([]byte, block)
	copy(data, "Pageant")
	if procCryptProtectMemory.Find() == nil {
		// PuTTY hashes the plain name when it can't encrypt it.
		const crossProcess = 1 // CRYPTPROTECTMEMORY_CROSS_PROCESS
		procCryptProtectMemory.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), crossProcess)
	}
	h := sha256.New()
	binary.Write(h, binary.BigEndian, uint32(len(data)))
	h.Write(data)
	return `\\.\pipe\pageant.` + u + "." + hex.EncodeToString(h.Sum(nil))
})

// userName is the user's name as PuTTY tells it (get_username): the user
// part of the principal name, else the account's name.
func userName() string {
	n := uint32(256)
	buf := make([]uint16, n)
	if windows.GetUserNameEx(windows.NameUserPrincipal, &buf[0], &n) == nil {
		u := windows.UTF16ToString(buf[:n])
		if i := strings.Index(u, "@"); i >= 0 {
			u = u[:i]
		}
		return u
	}
	cur, err := user.Current()
	if err != nil {
		return ""
	}
	u := cur.Username
	if i := strings.LastIndex(u, `\`); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// PageantPipe returns Pageant's pipe, and whether Pageant (or KiTTY's
// Kageant, which listens at the same) runs: whether the pipe is there.
func PageantPipe() (string, bool) {
	name := pageantName()
	if name == "" {
		return "", false
	}
	var fd windows.Win32finddata
	h, err := windows.FindFirstFile(windows.StringToUTF16Ptr(name), &fd)
	if err != nil {
		return name, false
	}
	windows.FindClose(h)
	return name, true
}
