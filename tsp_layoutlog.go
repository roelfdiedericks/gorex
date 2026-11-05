package main

import (
	"fmt"
	"log"
	"os"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// tspLayoutLog logs how live surfaces with a dock are laid out, while
// GOREX_TSPLOG names a file when GoRex starts: a line each time what
// places the transcript changes (the anchor, the rows the server gave,
// the dock's height, the content's), and each size sent to the server,
// to find what moves the transcript while a turn runs or a key is typed.
var tspLayoutLog = func() *log.Logger {
	path := os.Getenv("GOREX_TSPLOG")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	return log.New(f, "", log.Ltime|log.Lmicroseconds)
}()

// logSurfaceLayout logs a docked surface's layout in this frame when it
// differs from the one logged last for the surface.
func (a *App) logSurfaceLayout(p *Pane, sf *rex.Surface, pl terminal.AppPlacement, screenRows int, cut bool, dockH, contentH float32, box ui.Rect) {
	line := fmt.Sprintf("%s anchor=%d y=%.1f screen=%d cut=%v rows=%d dock=%d spare=%d most=%d dockH=%.1f contentH=%.1f boxY=%.1f boxH=%.1f",
		sf.ID, pl.Row, pl.Rect.Y, screenRows, cut, sf.Rows, sf.Dock, sf.Spare, sf.Most, dockH, contentH, box.Y, box.H)
	key := fmt.Sprintf("%p/%d", p, sf.Image)
	if a.tspLogged[key] == line {
		return
	}
	if a.tspLogged == nil {
		a.tspLogged = map[string]string{}
	}
	a.tspLogged[key] = line
	tspLayoutLog.Printf("f=%d %s", a.drawn, line)
}
