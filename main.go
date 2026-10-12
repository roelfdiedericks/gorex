// GoRex is a terminal of tabs and split panes, after Superlogical's Rex,
// in MyGo's native UI. Its shells run in a session server of their own,
// so that they outlive the app: quit it, open it again, and every tab and
// pane is back as it was, its programs still running.
//
//	go tool mygo dev                 the app, rebuilt as the code changes
//	go run . -server                 the session server alone, in the foreground
//	GoRex ssh user@host              a window on the sessions of a server on host
//	go run ./tools/remotepayload     builds the Linux server ssh copies there
package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/remote"
	"gorex/internal/rex"
)

func main() {
	// -server runs the session server; other arguments are AppKit's, as
	// -NSWindowResizeTime, which it reads itself.
	if len(os.Args) > 1 && os.Args[1] == "-server" {
		log.SetPrefix("[server] ")
		if err := rex.Serve(); err != nil {
			log.Fatal(err)
		}
		return
	}
	// -bridge, run by another GoRex over ssh, connects it to the session
	// server of the profile named.
	if len(os.Args) > 2 && os.Args[1] == "-bridge" {
		runBridge(os.Args[2])
		return
	}
	// The state of the sessions lives in the app's data directory, which
	// the server it starts takes from GOREX_DIR: "GoRex", or "GoRex Dev"
	// under mygo dev, which keeps the installed app's sessions apart.
	if os.Getenv("GOREX_DIR") == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			os.Setenv("GOREX_DIR", dir)
		}
	}
	registerFonts()
	loadSettings()
	a := &App{stateKey: "main"}
	// ssh <destination> opens a window on the sessions of a server there.
	// Its window keeps a place of its own, and the process is the window's:
	// it quits when the window closes, on macOS too, as ssh itself ends
	// with its session.
	oneOff := len(os.Args) > 2 && os.Args[1] == "ssh"
	if oneOff {
		if t, err := remote.Resolve(os.Args[2]); err != nil {
			a.err = "Can't connect to " + os.Args[2] + ": " + err.Error()
		} else {
			a.bind = binding{remote: true, target: t}
			a.stateKey = "ssh/" + t.Key()
		}
	}
	mygo.App.SetMenu(a.menu())
	mygo.App.WhenReady(a.open)
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && a.win == nil {
			a.open()
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" || oneOff {
			mygo.App.Quit()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.saveNow()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// runBridge carries the connections of a GoRex at the other end of ssh to
// the server of profile, whose state lives in ~/.config/<profile>. Its
// standard output carries nothing else.
func runBridge(profile string) {
	if profile == "" || profile == "." || profile == ".." || strings.ContainsAny(profile, `/\`) {
		log.Fatalf("bad profile %q", profile)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	os.Setenv("GOREX_DIR", filepath.Join(home, ".config", profile))
	log.SetOutput(os.Stderr)
	log.SetPrefix("[bridge] ")
	if err := rex.Bridge(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// open opens the window, which connects to its server and shows the tabs
// it kept.
func (a *App) open() {
	a.reset()
	opts := mygo.WindowOptions{
		Title:                "GoRex",
		Width:                1000,
		Height:               620,
		MinWidth:             560,
		MinHeight:            340,
		StateKey:             a.stateKey,
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 16, Y: 15},
		BackgroundColor:      "light-dark(#efe1e6, #231e27)",
		Content:              ui.View(a.view),
	}
	if ownControls {
		// The title bar draws the window's controls (windowControls).
		opts.TitleBarStyle, opts.Frameless = mygo.TitleBarDefault, true
	}
	win := mygo.NewWindow(opts)
	a.win = win
	a.debugHook()
	win.OnClosed(func() {
		a.saveNow()
		a.quitting = true
		a.gen.Add(1) // stops connecting
		a.detachAll()
		a.win = nil
	})
	a.startConnecting(win, a.bind, "Connecting to "+a.bind.label()+"…")
}

// saveNow sends the layout to the server and waits for it to be kept.
func (a *App) saveNow() {
	if a.client == nil || len(a.tabs) == 0 && !a.quitting {
		return
	}
	b, err := json.Marshal(a.snapshot())
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		a.client.SetLayout(b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}

// reset forgets the tabs of the window, as when it closed or connects
// to another server.
func (a *App) reset() {
	a.tabs, a.active, a.focusReq = nil, 0, nil
	a.paletteOpen, a.hostOpen, a.renaming = false, false, nil
	a.saveDue, a.quitting, a.lastSnapshot, a.title = false, false, "", ""
	a.err, a.errAction, a.errRun = "", "", nil
}

// staleServer reports whether the server runs another build of the app
// than this one, as after mygo dev rebuilt it.
func staleServer(h rex.Hello) bool {
	exe, at := rex.Executable()
	return h.Exe != exe || !h.ExeTime.Equal(at)
}
