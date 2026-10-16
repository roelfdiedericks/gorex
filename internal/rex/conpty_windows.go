package rex

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// conpty is the pseudo console sessions run in: Microsoft's own, the one
// Windows Terminal ships, when conpty.dll is beside the executable (with
// OpenConsole.exe in x64\), else Windows'. Windows' drops what it cannot
// model of what goes through it, as the OSC sequences a terminal writes
// to a program (the Kitty clipboard protocol's paste events) and the
// replies to a program's queries; Microsoft's passes them through.
var conpty struct {
	once                  sync.Once
	create, resize, close *windows.Proc
}

func loadConPTY() {
	conpty.once.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		path := filepath.Join(filepath.Dir(exe), "conpty.dll")
		if _, err := os.Stat(path); err != nil {
			return
		}
		dll, err := windows.LoadDLL(path)
		if err != nil {
			log.Printf("rex: %s: %v; using Windows' ConPTY", path, err)
			return
		}
		create, err1 := dll.FindProc("ConptyCreatePseudoConsole")
		resize, err2 := dll.FindProc("ConptyResizePseudoConsole")
		close, err3 := dll.FindProc("ConptyClosePseudoConsole")
		if err1 != nil || err2 != nil || err3 != nil {
			log.Printf("rex: %s lacks the ConPTY functions; using Windows' ConPTY", path)
			return
		}
		conpty.create, conpty.resize, conpty.close = create, resize, close
	})
}

// coordValue passes a COORD by value, as the ConPTY functions take it.
func coordValue(c windows.Coord) uintptr { return uintptr(*(*uint32)(unsafe.Pointer(&c))) }

func createPseudoConsole(size windows.Coord, in, out windows.Handle, flags uint32, console *windows.Handle) error {
	loadConPTY()
	if conpty.create == nil {
		return windows.CreatePseudoConsole(size, in, out, flags, console)
	}
	r, _, _ := conpty.create.Call(coordValue(size), uintptr(in), uintptr(out), uintptr(flags), uintptr(unsafe.Pointer(console)))
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

func resizePseudoConsole(console windows.Handle, size windows.Coord) error {
	loadConPTY()
	if conpty.resize == nil {
		return windows.ResizePseudoConsole(console, size)
	}
	if r, _, _ := conpty.resize.Call(uintptr(console), coordValue(size)); r != 0 {
		return windows.Errno(r)
	}
	return nil
}

func closePseudoConsole(console windows.Handle) {
	loadConPTY()
	if conpty.close == nil {
		windows.ClosePseudoConsole(console)
		return
	}
	conpty.close.Call(uintptr(console))
}
