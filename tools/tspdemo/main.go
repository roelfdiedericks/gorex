// tspdemo tries the Tern Surface Protocol in a GoRex pane: it opens a
// screen surface with a card whose status changes every second, a few
// lines of Markdown and a badge that counts clicks, through the official
// SDK (github.com/stencil-hq/tern-sdk). Escape ends it. Where the
// terminal does not speak TSP, it prints the view as text and exits.
//
//	GOOS=linux GOARCH=amd64 go build -o tspdemo ./tools/tspdemo
//	TERN_TSP_RECORD=tsp.jsonl ./tspdemo    # records every message
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/stencil-hq/tern-sdk/go/tern"
	"github.com/stencil-hq/tern-sdk/go/tern/ui"
)

const body = `GoRex draws this surface natively, over the terminal.

- The status above flips every second: frames and credits.
- The badge counts clicks: events back to the program.
- **Escape** closes the surface and ends the program.`

func main() {
	ctx := context.Background()
	clicks, ticks := 0, 0
	view := func() tern.View {
		status := ui.Running
		if ticks%2 == 1 {
			status = ui.Done
		}
		return tern.Main(ui.Card{
			Common: ui.Common{Key: "card"},
			Head:   ui.T("GoRex TSP demo"),
			Status: status,
			Children: ui.Nodes(
				ui.Md{Common: ui.Common{Key: "body"}, Text: body},
				ui.Row{Common: ui.Common{Key: "row"}, Children: ui.Nodes(
					ui.Badge{Common: ui.Common{Key: "button", OnClick: func(*tern.Action) { clicks++ }}, Text: "Click me"},
					ui.Text{Common: ui.Common{Key: "count"}, Text: fmt.Sprintf("clicked %d times, tick %d", clicks, ticks)},
				)},
			),
		})
	}
	s, err := tern.Connect(ctx, tern.Options{App: "tspdemo", Version: "0.1"})
	if errors.Is(err, tern.ErrUnsupported) {
		fmt.Print(tern.Plain(view(), 0))
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tspdemo:", err)
		os.Exit(1)
	}
	defer s.Close()
	sf, err := s.Open(tern.SurfaceOptions{Mode: tern.Inline, Title: "tspdemo"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tspdemo:", err)
		os.Exit(1)
	}
	render := func() {
		if err := sf.Render(view()); err != nil {
			fmt.Fprintln(os.Stderr, "tspdemo:", err)
		}
	}
	render()
	// The clock ticks on its own goroutine; Run's goroutine renders, so the
	// tick is handed over as input.
	stop := make(chan struct{})
	defer close(stop)
	tick := make(chan struct{}, 1)
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				select {
				case tick <- struct{}{}:
				default:
				}
			case <-stop:
				return
			}
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	inputs := make(chan tern.Input, 16)
	errs := make(chan error, 1)
	go func() {
		errs <- s.Run(ctx, func(in tern.Input) error {
			inputs <- in
			return nil
		})
	}()
	for {
		select {
		case <-tick:
			ticks++
			render()
		case in := <-inputs:
			if k, ok := in.(tern.Key); ok && k.Is("escape") {
				return
			}
			// A click ran its handler on Run's goroutine already; redraw.
			render()
		case err := <-errs:
			if err != nil && !errors.Is(err, tern.ErrClosed) {
				fmt.Fprintln(os.Stderr, "tspdemo:", err)
			}
			return
		}
	}
}
