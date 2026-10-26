package main

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// tspTool draws a tool call (kind tool): a head drawn from its data, its
// icon, verb and target, short facts, badges, a note, the exit code, the
// time it ran and its state, and the body of its children, folding as a
// card does. Nested cards and tools in the body are drawn bare, as
// sections of the one frame.
func (a *App) tspTool(c *ui.Context, key *ui.Context, k *colors, t *Tab, p *Pane, sf *rex.Surface, n *rex.TSPNode) ui.Element {
	name, _ := n.Props["name"].(string)
	status, _ := n.Props["status"].(string)
	frame, _ := n.Props["frame"].(string)
	intent, _ := n.Props["intent"].(string)
	_, body, collapsed := a.tspFold(p, sf, n)
	flat := frame == "inline" || a.tspFlat > 0
	tool := ui.Column(key).Gap(8).FillWidth()
	if !flat {
		tool.Radius(cardR).Background(k.card).Border(1, k.cardBorder).Padding(10, 14)
	}
	tool.Children(func() {
		h := ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth()
		if collapsible, _ := n.Props["collapsible"].(bool); collapsible {
			h.Unselectable()
		}
		if intent != "" && status != "running" {
			h.Tooltip(intent)
		}
		h.Children(func() {
			if collapsible, _ := n.Props["collapsible"].(bool); collapsible && flat {
				glyph := "▾"
				if collapsed {
					glyph = "▸"
				}
				ui.Text(c, glyph).FontSize(12).TextColor(k.textMuted)
			}
			a.tspIcon(c, k, tspToolIcon(name))
			a.tspRich(c, k, n.Props["title"], 13, true).Shrink(0)
			if target := n.Props["target"]; target != nil {
				kind, _ := n.Props["targetKind"].(string)
				a.tspToolTarget(c, k, target, kind).Shrink(1).MinWidth(0)
			}
			if meta, _ := n.Props["meta"].([]any); len(meta) > 0 {
				for i, m := range meta {
					a.tspRich(c.Key(fmt.Sprintf("meta%d", i)), k, m, 12, false).TextColor(k.textMuted).Shrink(0)
				}
			}
			a.tspChips(c, k, n.Props["badges"])
			ui.Box(c).Grow(1)
			if note := n.Props["note"]; note != nil {
				a.tspRich(c, k, note, 12, false).TextColor(k.textMuted)
			}
			if exit, _ := n.Props["exit"].(float64); exit != 0 {
				ui.Text(c, fmt.Sprintf("exit %d", int(exit))).FontSize(11).TextColor(k.attention).Padding(1, 6).Radius(4).Background(k.hover)
			}
			if took, ok := n.Props["took"].(float64); ok && status != "running" {
				ui.Text(c, elapsedText(took/1000)).FontSize(12).TextColor(k.textMuted)
			} else if age, ok := n.Props["age"].(float64); ok {
				var stopped *float64
				if status != "running" && status != "pending" {
					stopped = &age
				}
				ui.Text(c, elapsedText(a.liveMS(c, p, sf, n.ID, age, stopped)/1000)).FontSize(12).TextColor(k.textMuted)
			}
			if status != "" && status != "done" {
				a.tspStatus(c, k, status)
			} else if status == "done" && !flat {
				ui.Text(c, "✓").FontSize(12).TextColor(k.busy)
			}
			a.tspActions(c, k, p, sf, n, n.Props["tools"])
		})
		a.tspFoldHead(h, p, sf, n, collapsed)
		if intent != "" && status == "running" {
			ui.Text(c, intent).FontSize(12).TextColor(k.textMuted).Italic()
		}
		if len(body) == 0 {
			return
		}
		drawBody := func() {
			a.tspFlat++
			for _, ch := range body {
				a.tspNode(c, k, t, p, sf, ch)
			}
			a.tspFlat--
		}
		if !collapsed {
			ui.Column(c.Key("body")).Gap(8).FillWidth().Children(drawBody)
			return
		}
		// Collapsed: the body's head or its tail, as many lines as the
		// preview says; its children's own previews; or nothing.
		switch pv := n.Props["preview"].(type) {
		case string:
			if pv == "children" {
				ui.Column(c.Key("body")).Gap(8).FillWidth().Children(drawBody)
			}
		case map[string]any:
			lines, tail := pv["lines"].(float64)
			if !tail {
				lines, _ = pv["tail"].(float64)
			}
			if lines <= 0 {
				return
			}
			clip := ui.Column(c.Key("body")).Gap(8).FillWidth().MaxHeight(float32(lines) * 18).Clip()
			if tail {
				clip.Justify(ui.End)
			}
			clip.Children(drawBody)
		}
	})
	return tool
}

// tspToolTarget draws a tool's primary argument by its kind: a command in
// the mono face, a path with its directory dim and its name strong, a
// pattern or a query quoted in the accent color.
func (a *App) tspToolTarget(c *ui.Context, k *colors, v any, kind string) ui.Element {
	mono := a.editorFont().Family
	s, isString := v.(string)
	if !isString {
		return a.tspRich(c, k, v, 12, false).Font(mono).SingleLine()
	}
	switch kind {
	case "path":
		dir, file := path.Split(s)
		return ui.RichText(c,
			ui.Span{Text: dir, Font: mono, Size: 12, Color: k.textMuted},
			ui.Span{Text: file, Font: mono, Size: 12, Color: k.text, Weight: 600},
		).SingleLine()
	case "pattern", "query":
		return ui.Text(c, "“"+s+"”").Font(mono).FontSize(12).TextColor(k.busy).SingleLine()
	case "text":
		return ui.Text(c, s).FontSize(12).TextColor(k.text).SingleLine()
	}
	first, _, _ := strings.Cut(s, "\n")
	return ui.Text(c, first).Font(mono).FontSize(12).TextColor(k.text).SingleLine()
}

// tspToolIcon is the icon of a tool by its name.
func tspToolIcon(name string) string {
	switch {
	case name == "bash" || name == "shell" || strings.HasSuffix(name, "exec"):
		return "terminal"
	case name == "read" || name == "view":
		return "file"
	case name == "edit" || name == "write" || name == "patch" || name == "apply_patch":
		return "pencil"
	case name == "grep" || name == "find" || name == "search" || name == "glob" || strings.Contains(name, "search"):
		return "search"
	case name == "web_search" || name == "fetch" || name == "browser":
		return "globe"
	case name == "task" || name == "agent":
		return "bot"
	case name == "eval" || name == "python" || name == "js":
		return "file-code"
	case name == "todo" || name == "todo_write":
		return "check"
	case strings.HasPrefix(name, "mcp__"):
		return "server"
	}
	return "activity"
}

// tspChips draws chips: badges of text and tone, with their tooltips.
func (a *App) tspChips(c *ui.Context, k *colors, v any) {
	chips, _ := v.([]any)
	for i, ch := range chips {
		ch, _ := ch.(map[string]any)
		text, _ := ch["text"].(string)
		tone, _ := ch["tone"].(string)
		color := k.textMuted
		switch tone {
		case "accent", "info", "success":
			color = k.busy
		case "warning", "error":
			color = k.attention
		}
		chip := ui.Text(c.Key(fmt.Sprintf("chip%d", i)), text).FontSize(11).TextColor(color).Padding(1, 6).Radius(4).Background(k.hover).Shrink(0)
		if title, _ := ch["title"].(string); title != "" {
			chip.Tooltip(title)
		}
	}
}

// tspActions draws buttons a program offers (a tool's head tools, a
// picker's action bar): a click sends the action with the button's id.
func (a *App) tspActions(c *ui.Context, k *colors, p *Pane, sf *rex.Surface, n *rex.TSPNode, v any) {
	actions, _ := v.([]any)
	for i, ac := range actions {
		ac, _ := ac.(map[string]any)
		id, _ := ac["id"].(string)
		label, _ := ac["label"].(string)
		primary, _ := ac["primary"].(bool)
		danger, _ := ac["danger"].(bool)
		on, _ := ac["on"].(bool)
		disabled := ac["disabled"] != nil && ac["disabled"] != false
		keys, _ := ac["keys"].([]any)
		btn := ui.Row(c.Key(fmt.Sprintf("act-%d-%s", i, id))).Gap(5).AlignItems(ui.Center).Padding(2, 8).Radius(5).Unselectable().Shrink(0)
		color := k.text
		switch {
		case primary:
			btn.Background(k.busy.Alpha(0.25))
		case on:
			btn.Background(k.hover).Border(1, k.busy)
		default:
			btn.Background(k.hover)
		}
		if danger {
			color = k.attention
		}
		if disabled {
			btn.Opacity(0.5)
			if why, ok := ac["disabled"].(string); ok {
				btn.Tooltip(why)
			}
		}
		btn.Children(func() {
			if label != "" {
				ui.Text(c, label).FontSize(12).TextColor(color)
			}
			if len(keys) > 0 {
				a.tspKeys(c, k, keys)
			}
		})
		if btn.Clicked() && !disabled && !sf.Closed {
			a.tspSend(p, map[string]any{"ev": "action", "sf": sf.ID, "id": n.ID, "act": id})
		}
	}
}

// tspSend sends a program an event of its surface.
func (a *App) tspSend(p *Pane, ev map[string]any) {
	stream := p.stream
	go func() {
		if err := stream.TSPEvent(ev); err != nil {
			a.post(func() {
				a.toast(ui.Toast{Title: "The program did not get the event", Description: err.Error(), Type: "error"})
			})
		}
	}()
}

// liveMS is a live timer's milliseconds: the age the program gave, plus
// the time since the window first heard it, redrawn every second; with
// stopped, that. id names the timer among the surface's.
func (a *App) liveMS(c *ui.Context, p *Pane, sf *rex.Surface, id string, age float64, stopped *float64) float64 {
	key := fmt.Sprintf("%d/%s", sf.Image, id)
	if stopped != nil {
		delete(p.elapsed, key)
		return *stopped
	}
	if sf.Closed {
		return age
	}
	st, ok := p.elapsed[key]
	if !ok || st.age != age {
		st = elapsedStart{age: age, at: time.Now()}
		if p.elapsed == nil {
			p.elapsed = map[string]elapsedStart{}
		}
		p.elapsed[key] = st
	}
	if !a.elapsedDue {
		a.elapsedDue = true
		time.AfterFunc(time.Second, func() {
			a.elapsedDue = false
			c.Invalidate()
		})
	}
	return age + float64(time.Since(st.at)/time.Millisecond)
}
