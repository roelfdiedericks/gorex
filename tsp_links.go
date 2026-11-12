package main

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// Links and the actions a terminal does itself, as Tern does: a node's
// copy and open actions act on its href here, in the window (omp's sign-in
// link and its Copy button, Open dashboard), and still reach the program;
// a span with an href is a link; a node's menu actions make its context
// menu; and text cut in the middle or at its start keeps both ends, or
// its end, in sight.

// tspLocalAct says what the window does itself for act on n: the text to
// put on the clipboard (copy), or the address to open in the browser
// (open). Both are n's href; nothing for a node without one, or an
// address that is not a web page or an email.
func tspLocalAct(n *rex.TSPNode, act string) (copyText, openURL string) {
	href, _ := n.Props["href"].(string)
	if href == "" {
		return "", ""
	}
	switch act {
	case "copy":
		return href, ""
	case "open":
		if webURL(href) {
			return "", href
		}
	}
	return "", ""
}

// webURL tells an address the browser opens: http, https or mailto.
func webURL(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// tspLocal does what the window does itself for act on n (tspLocalAct).
func (a *App) tspLocal(n *rex.TSPNode, act string) {
	copyText, openURL := tspLocalAct(n, act)
	switch {
	case copyText != "":
		a.copyLink(copyText)
	case openURL != "":
		mygo.Shell.OpenExternal(openURL)
	}
}

// copyLink puts text on the clipboard and says so.
func (a *App) copyLink(text string) {
	a.services.WriteClipboard(text)
	a.toast(ui.Toast{ID: "tsp-copied", Title: "Copied to the clipboard", Timeout: 1500 * time.Millisecond})
}

// tspMenu gives el the context menu of n's menu actions, each sent with
// send, after what the window does itself for it (tspLocal).
func (a *App) tspMenu(el ui.Element, n *rex.TSPNode, send func(act string)) {
	actions, _ := n.Props["actions"].(map[string]any)
	menu, _ := actions["menu"].([]any)
	if len(menu) == 0 {
		return
	}
	el.ContextMenu(func(m *ui.Menu) {
		for _, v := range menu {
			act, _ := v.(string)
			if act == "" {
				continue
			}
			if m.Item(tspActLabel(act)).Chosen() {
				a.tspLocal(n, act)
				send(act)
			}
		}
	})
}

// tspActLabel names an action in a menu: its words, the first capitalized
// (copy-path is Copy path).
func tspActLabel(act string) string {
	name, _, _ := strings.Cut(act, "=")
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// tspLinkMenu is the context menu of a link: open it, or copy it.
func (a *App) tspLinkMenu(el ui.Element, href string) {
	el.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open Link").Chosen() {
			mygo.Shell.OpenExternal(href)
		}
		if m.Item("Copy Link").Chosen() {
			a.copyLink(href)
		}
	})
}

// tspCutText draws spans on one line cut as truncate says, "middle"
// keeping both ends and "start" the end, with "…" for what is cut; a
// click on it opens its link, when one of its spans has one, and its
// context menu opens or copies the link. A file's link opens as a click
// on a file does, underlined while hovered.
func (a *App) tspCutText(c *ui.Context, spans []ui.Span, hrefs []string, truncate string) ui.Element {
	w, h := c.MeasureText(0, spans...)
	box := ui.Box(c).Width(w).Height(h).Shrink(1).MinWidth(0)
	link := ""
	if i := slices.IndexFunc(hrefs, a.tspOpenable); i >= 0 {
		link = hrefs[i]
	}
	if file := a.tspFileLink(link); file != "" {
		if box.Hovered() {
			spans = slices.Clone(spans)
			for i := range spans {
				spans[i].Underline = spans[i].Underline || hrefs[i] == link
			}
		}
		if a.fileLink(box, a.tspPane, file) {
			a.tspLinkClicked = true
		}
	} else if link != "" {
		box.Cursor(ui.CursorPointer)
		if box.Clicked() {
			mygo.Shell.OpenExternal(link)
		}
		a.tspLinkMenu(box, link)
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		key := fmt.Sprintf("%s|%.0f|%v", truncate, r.W, spans)
		cut, ok := cuts[key]
		if !ok {
			cut = cutSpans(p, r.W, spans, truncate == "start")
			if len(cuts) > 512 {
				clear(cuts)
			}
			cuts[key] = cut
		}
		p.RichText(r.X, r.Y+(r.H-h)/2, 0, cut...)
	})
	return box
}

// cuts caches the spans of cut texts, by how they are cut, their width
// and spans.
var cuts = map[string][]ui.Span{}

// cutSpans returns spans fitted into width with "…" for what does not
// fit: the middle of the text, or its start when start is set. The
// ellipsis takes the style of the span it stands in.
func cutSpans(p *ui.Painter, width float32, spans []ui.Span, start bool) []ui.Span {
	if w, _ := p.MeasureText(0, spans...); w <= width {
		return spans
	}
	type ch struct {
		s    int
		text string
	}
	var chars []ch
	for i, s := range spans {
		for _, r := range s.Text {
			chars = append(chars, ch{i, string(r)})
		}
	}
	// build keeps head characters from the start and tail from the end.
	build := func(head, tail int) []ui.Span {
		var out []ui.Span
		add := func(si int, text string) {
			if n := len(out); n > 0 && out[n-1].Text != "…" && sameStyle(out[n-1], spans[si]) {
				out[n-1].Text += text
				return
			}
			sp := spans[si]
			sp.Text = text
			out = append(out, sp)
		}
		for _, c := range chars[:head] {
			add(c.s, c.text)
		}
		at := min(head, len(chars)-1)
		ell := spans[chars[at].s]
		ell.Text = "…"
		out = append(out, ell)
		for _, c := range chars[len(chars)-tail:] {
			add(c.s, c.text)
		}
		return out
	}
	split := func(keep int) (int, int) {
		if start {
			return 0, keep
		}
		return keep - keep/2, keep / 2
	}
	// The most characters that fit, by halving.
	lo, hi := 0, len(chars)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if w, _ := p.MeasureText(0, build(split(mid))...); w <= width {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return build(split(lo))
}

// sameStyle tells spans that differ in text only.
func sameStyle(a, b ui.Span) bool {
	a.Text, b.Text = "", ""
	return fmt.Sprint(a) == fmt.Sprint(b)
}
