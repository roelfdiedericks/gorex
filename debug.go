package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"
)

// debugHook lets a script drive the app while GOREX_DEBUG names a
// directory: each line of its file "do" is run, and "shot" captures the
// window into shot.png.
func (a *App) debugHook() {
	dir := os.Getenv("GOREX_DEBUG")
	if dir == "" {
		return
	}
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	go func() {
		for range time.Tick(150 * time.Millisecond) {
			req := filepath.Join(dir, "do")
			b, err := os.ReadFile(req)
			if err != nil {
				continue
			}
			os.Remove(req)
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				a.debug(dir, strings.TrimSpace(line))
			}
		}
	}()
}

func (a *App) debug(dir, line string) {
	verb, arg, _ := strings.Cut(line, " ")
	win := a.win
	if win == nil {
		return
	}
	switch verb {
	case "shot":
		time.Sleep(100 * time.Millisecond)
		if png, err := win.CapturePage(); err == nil {
			name := "shot.png"
			if arg != "" {
				name = arg
			}
			os.WriteFile(filepath.Join(dir, name), png, 0o644)
		}
	case "sleep":
		d, _ := time.ParseDuration(arg)
		time.Sleep(d)
	case "type":
		arg = strings.ReplaceAll(arg, `\r`, "\r")
		arg = strings.ReplaceAll(arg, `\e`, "\x1b")
		win.Update(func() {
			if t := a.tab(); t != nil && t.Focus != nil && t.Focus.term != nil {
				t.Focus.term.Send([]byte(arg))
			}
		})
	case "size":
		var w, h int
		if _, err := fmtSscan(arg, &w, &h); err == nil {
			win.SetSize(w, h)
		}
	case "root":
		// root <path>: the active editor tab's browser moves there.
		win.Update(func() {
			if p := a.focusedBrowser(); p != nil {
				a.setRoot(p, arg)
			}
		})
	case "open":
		// open <path>: opens a file in the active editor tab, as a click
		// in its browser does.
		win.Update(func() {
			if t := a.tab(); t != nil && t.Kind == paneEditor {
				a.openFile(t, arg, false)
			}
		})
	case "scroll":
		// scroll <by> [n] [ms]: scrolls the focused pane's terminal as
		// its keys do (scrollTerm), n times, ms apart (16 by default).
		by, rest, _ := strings.Cut(arg, " ")
		ns, ms, _ := strings.Cut(rest, " ")
		n, gap := max(atoi(ns), 1), 16*time.Millisecond
		if ms != "" {
			gap = time.Duration(atoi(ms)) * time.Millisecond
		}
		for range n {
			win.Update(func() {
				if t := a.tab(); t != nil && t.Focus != nil && t.Focus.term != nil {
					a.scrollTerm(t.Focus, by)
					a.changed()
				}
			})
			time.Sleep(gap)
		}
	case "prof":
		// prof <file>: starts a CPU profile of the app, which profstop
		// ends.
		if f, err := os.Create(filepath.Join(dir, arg)); err == nil {
			if pprof.StartCPUProfile(f) != nil {
				f.Close()
			} else {
				profFile = f
			}
		}
	case "profstop":
		if profFile != nil {
			pprof.StopCPUProfile()
			profFile.Close()
			profFile = nil
		}
	case "log":
		// log <text>: a mark in the log, between measurements.
		log.Print("debug: ", arg)
	case "cull":
		// cull on|off: whether long lists of surfaces are culled.
		win.Update(func() { tspCullOff = arg == "off"; a.changed() })
	default:
		for _, cmd := range commands {
			if strings.EqualFold(strings.ReplaceAll(cmd.Title, " ", "-"), verb) {
				a.run(cmd)
			}
		}
		switch verb {
		case "tab":
			win.Update(func() { a.selectTab(atoi(arg) - 1) })
		case "focus":
			win.Update(func() {
				if t := a.tab(); t != nil {
					if ps := t.panes(); atoi(arg)-1 < len(ps) {
						t.setFocus(ps[atoi(arg)-1])
						a.focusReq = t.Focus
					}
				}
			})
		case "name":
			win.Update(func() {
				if t := a.tab(); t != nil {
					t.Name = arg
				}
			})
		case "close":
			win.Close()
		case "zoom":
			win.Update(func() {
				if win.IsMaximized() {
					win.Unmaximize()
				} else {
					win.Maximize()
				}
			})
		}
	}
}

// profFile is the CPU profile the debug verb prof writes.
var profFile *os.File

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}

func fmtSscan(s string, w, h *int) (int, error) {
	a, b, _ := strings.Cut(s, " ")
	*w, *h = atoi(a), atoi(b)
	return 2, nil
}
