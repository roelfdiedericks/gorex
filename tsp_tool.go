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
		// The file the card names (its href), its name a link to it.
		href, _ := n.Props["href"].(string)
		file := tspFileHref(href)
		opened := false
		if collapsible, _ := n.Props["collapsible"].(bool); collapsible {
			h.Unselectable()
		}
		h.Children(func() {
			a.tspChevron(c, k, n, collapsed)
			a.tspIcon(c, k, tspToolIcon(name))
			a.tspRich(c, k, n.Props["title"], 13, true).Shrink(0)
			if target := n.Props["target"]; target != nil {
				kind, _ := n.Props["targetKind"].(string)
				if file != "" {
					// The file's name, a link to it: in the links' color,
					// underlined while the pointer is on it, as a
					// message's links are.
					link := ui.Row(c.Key("target")).Shrink(1).MinWidth(0)
					hover := link.Hovered()
					link.Children(func() {
						a.tspToolTarget(c, k, target, kind, true, hover).Shrink(1).MinWidth(0)
					})
					opened = a.fileLink(link, p, file)
				} else {
					a.tspToolTarget(c, k, target, kind, false, false).Shrink(1).MinWidth(0)
				}
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
				ui.Text(c, fmt.Sprintf("exit %d", int(exit))).FontSize(a.tspText(11)).TextColor(k.attention).Padding(1, 6).Radius(4).Background(k.hover)
			}
			if took, ok := n.Props["took"].(float64); ok && status != "running" {
				ui.Text(c, elapsedText(took/1000)).FontSize(a.tspText(12)).TextColor(k.textMuted)
			} else if age, ok := n.Props["age"].(float64); ok {
				var stopped *float64
				if status != "running" && status != "pending" {
					stopped = &age
				}
				ui.Text(c, elapsedText(a.liveMS(c, p, sf, n.ID, age, stopped)/1000)).FontSize(a.tspText(12)).TextColor(k.textMuted)
			}
			if status != "" && status != "done" {
				a.tspStatus(c, k, status)
			} else if status == "done" && !flat {
				ui.Text(c, "✓").FontSize(a.tspText(12)).TextColor(k.busy)
			}
			a.tspActions(c, k, p, sf, n, n.Props["tools"])
		})
		if !opened {
			// A click on the file's name opened it, not the fold.
			a.tspFoldHead(h, p, sf, n, collapsed)
		}
		if intent != "" && status == "running" {
			ui.Text(c, intent).FontSize(a.tspText(12)).TextColor(k.textMuted).Italic()
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
// pattern or a query quoted in the accent color. A link (the file the
// card names) is in the links' color, underlined while hovered.
func (a *App) tspToolTarget(c *ui.Context, k *colors, v any, kind string, link, hover bool) ui.Element {
	mono := a.tspLook().mono
	ink := k.text
	if link {
		ink = k.busy
	}
	s, isString := v.(string)
	if !isString {
		spans, ok := v.([]any)
		if !ok || !link {
			return a.tspRich(c, k, v, 12, false).Font(mono).SingleLine()
		}
		sp, _ := a.tspSpans(k, spans, 12, false)
		for i := range sp {
			sp[i].Font, sp[i].Underline = mono, hover
			if sp[i].Color == k.text {
				sp[i].Color = ink
			}
		}
		return ui.RichText(c, sp...).SingleLine()
	}
	var t ui.Element
	switch kind {
	case "path":
		dir, file := path.Split(s)
		return ui.RichText(c,
			ui.Span{Text: dir, Font: mono, Size: a.tspCode(12), Color: k.textMuted, Underline: hover},
			ui.Span{Text: file, Font: mono, Size: a.tspCode(12), Color: ink, Weight: 600, Underline: hover},
		).SingleLine()
	case "pattern", "query":
		t = ui.Text(c, "“"+s+"”").Font(mono).FontSize(a.tspCode(12)).TextColor(k.busy).SingleLine()
	case "text":
		t = ui.Text(c, s).FontSize(a.tspText(12)).TextColor(ink).SingleLine()
	default:
		first, _, _ := strings.Cut(s, "\n")
		t = ui.Text(c, first).Font(mono).FontFeatures(a.tspLook().monoFeatures...).FontSize(a.tspCode(12)).TextColor(ink).SingleLine()
	}
	if hover {
		t.Underline()
	}
	return t
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
		chip := ui.Text(c.Key(fmt.Sprintf("chip%d", i)), text).FontSize(a.tspText(11)).TextColor(color).Padding(1, 6).Radius(4).Background(k.hover).Shrink(0)
		if title, _ := ch["title"].(string); title != "" {
			chip.Tooltip(title)
		}
	}
}

// tspActions draws buttons a program offers (a tool's head tools, a
// picker's action bar) as MyGo's buttons, as Tern's: the primary one as
// the primary button, one that is on as chosen, a danger one's label in
// the error color, a disabled one with its reason as its tooltip, the
// keys in the tooltip. A click sends the action with the button's id.
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
		key := c.Key(fmt.Sprintf("act-%d-%s", i, id))
		var btn ui.Element
		if primary {
			btn = ui.PrimaryButton(key, label)
		} else {
			btn = ui.Button(key, label)
		}
		btn.Shrink(0)
		if on {
			btn.Selected(true)
		}
		if danger {
			btn.TextColor(k.attention)
		}
		tip := label
		if keys, _ := ac["keys"].([]any); len(keys) > 0 {
			names := make([]string, 0, len(keys))
			for _, key := range keys {
				if s, ok := key.(string); ok {
					names = append(names, s)
				}
			}
			tip += "  " + strings.Join(names, " ")
		}
		if disabled {
			btn.Disabled(true)
			if why, ok := ac["disabled"].(string); ok {
				tip = why
			}
		}
		if tip != "" {
			btn.Tooltip(tip)
		}
		if btn.Clicked() && !disabled && !sf.Closed {
			// The keys stay the program's, not the button's.
			a.focusReq = p
			a.changed()
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
