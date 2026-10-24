// tspdemo tries the Tern Surface Protocol in a GoRex pane: it opens an
// inline surface with a card whose status changes every second, a few
// lines of Markdown, a badge that counts clicks, and one of each of the
// other kinds GoRex draws (facts, a list, a table, code, progress,
// keys), through the official SDK (github.com/stencil-hq/tern-sdk).
// Escape ends it. Where the terminal does not speak TSP, it prints the
// view as text and exits.
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
- **Escape** closes the surface and ends the program.
- Links open: [the SDK](https://github.com/stencil-hq/tern-sdk).`

const code = `func main() {
	s, err := tern.Connect(ctx, tern.Options{App: "tspdemo"})
	if errors.Is(err, tern.ErrUnsupported) {
		fmt.Print(tern.Plain(view(), 0)) // a terminal without TSP
	}
}`

func main() {
	ctx := context.Background()
	clicks, ticks := 0, 0
	view := func() tern.View {
		status := ui.Running
		if ticks%2 == 1 {
			status = ui.Done
		}
		progress := float64(ticks%10) / 10
		return tern.Main(ui.Card{
			Common: ui.Common{Key: "card"},
			Head:   ui.T("GoRex TSP demo"),
			Status: status,
			Children: ui.Nodes(
				ui.Md{Common: ui.Common{Key: "body"}, Text: body},
				ui.Row{Common: ui.Common{Key: "row"}, Children: ui.Nodes(
					ui.Badge{Common: ui.Common{Key: "button", OnClick: func(*tern.Action) { clicks++ }}, Text: "Click me"},
					ui.Text{Common: ui.Common{Key: "count"}, Text: fmt.Sprintf("clicked %d times, tick %d", clicks, ticks)},
					ui.Kbd{Common: ui.Common{Key: "esc"}, Keys: []string{"Esc"}},
				)},
				ui.Rule{Common: ui.Common{Key: "rule"}, Label: ui.T("the other kinds")},
				ui.Section{Common: ui.Common{Key: "facts"}, Head: ui.T("Facts"), Children: ui.Nodes(
					ui.KV{Common: ui.Common{Key: "kv"}, Items: []ui.Pair{
						{K: ui.T("Program"), V: ui.T("tspdemo")},
						{K: ui.T("Ticks"), V: ui.Spans(ui.Span{T: fmt.Sprint(ticks), S: "num"}, ui.Span{T: " so far", S: "muted"})},
						{K: ui.T("Styles"), V: ui.Spans(ui.Span{T: "strong ", S: "strong"}, ui.Span{T: "em ", S: "em"}, ui.Span{T: "code ", S: "code"}, ui.Span{T: "accent ", S: "accent"}, ui.Span{T: "warning", S: "warning"})},
					}},
				)},
				ui.Section{Common: ui.Common{Key: "work"}, Head: ui.T("Work"), Children: ui.Nodes(
					ui.Progress{Common: ui.Common{Key: "progress"}, Value: &progress, Label: ui.T(fmt.Sprintf("%d%% of the way", int(progress*100)))},
					ui.Spinner{Common: ui.Common{Key: "spinner"}, Label: ui.T("still ticking")},
					ui.Status{Common: ui.Common{Key: "status"}, Children: ui.Nodes(
						ui.Icon{Common: ui.Common{Key: "i"}, Name: ui.IconCheck},
						ui.Text{Common: ui.Common{Key: "t"}, Text: "a status bar with an icon"},
					)},
				)},
				ui.List{CommonWithoutMax: ui.CommonWithoutMax{Key: "list"}, Children: ui.Nodes(
					ui.Item{Common: ui.Common{Key: "one"}, Icon: ui.IconFolder, Label: ui.T("An item with a label"), Detail: ui.T("and a detail line under it"), Hint: []string{"Enter"}},
					ui.Item{Common: ui.Common{Key: "two"}, Icon: ui.IconFile, Label: ui.T("Another, with a value"), Value: ui.T("42")},
					ui.Item{Common: ui.Common{Key: "three"}, Label: ui.T("A disabled one"), Disabled: true},
				)},
				ui.Table{Common: ui.Common{Key: "table"},
					Cols: []ui.Column{{ID: "name", Head: ui.T("Name"), Grow: 1}, {ID: "kind", Head: ui.T("Kind")}, {ID: "load", Head: ui.T("Load")}},
					Rows: []ui.TableRow{
						{ID: "r1", Cells: map[string]ui.Cell{"name": ui.T("debian-vpp"), "kind": ui.T("host"), "load": ui.MeterCell{Value: &progress}}},
						{ID: "r2", Cells: map[string]ui.Cell{"name": ui.T("mac-mini"), "kind": ui.T("host"), "load": ui.T("idle")}},
					}},
				ui.Code{Common: ui.Common{Key: "code"}, Text: code, Lang: "go", Path: "tools/tspdemo/main.go"},
				ui.Spacer{Common: ui.Common{Key: "sp"}, Size: ui.GapMD},
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
