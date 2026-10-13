// Package markdown renders Markdown to HTML for a preview: GitHub's
// dialect, code highlighted by Chroma with classes a stylesheet colors,
// Mermaid blocks left for the page to draw, and raw HTML kept but
// sanitised, as the text may be anyone's.
package markdown

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	once   sync.Once
	md     goldmark.Markdown
	policy *bluemonday.Policy
)

func setup() {
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM, extension.Footnote, extension.DefinitionList, extension.Typographer,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithFormatOptions(chromahtml.WithClasses(true), chromahtml.ClassPrefix("ch-")),
				highlighting.WithGuessLanguage(false),
				highlighting.WithWrapperRenderer(codeWrapper),
			),
			&mermaidKeeper{},
			&lineMarker{},
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	policy = bluemonday.UGCPolicy()
	policy.AllowElements("details", "summary", "kbd", "sup", "sub", "mark", "ins", "del", "abbr", "figure", "figcaption", "picture", "source", "video", "audio")
	policy.AllowAttrs("open").OnElements("details")
	policy.AllowAttrs("align").OnElements("p", "div", "td", "th", "table", "img", "tr", "h1", "h2", "h3", "h4", "h5", "h6")
	policy.AllowAttrs("width", "height").OnElements("img", "video", "source", "table", "td", "th")
	policy.AllowAttrs("class").Matching(regexp.MustCompile(`^[a-zA-Z0-9 _-]+$`)).Globally()
	policy.AllowAttrs("id").Matching(regexp.MustCompile(`^[a-zA-Z0-9 _:.-]+$`)).Globally()
	policy.AllowAttrs("type", "checked", "disabled").OnElements("input")
	policy.AllowAttrs("start").OnElements("ol")
	policy.AllowAttrs("src", "controls", "poster", "loop", "muted").OnElements("video", "audio", "source")
	policy.AllowAttrs("title", "lang", "dir").Globally()
	policy.AllowAttrs("role", "aria-hidden", "aria-label").Globally()
	policy.AllowAttrs("data-line").Matching(regexp.MustCompile(`^[0-9]+$`)).Globally()
	// The page's own scheme for the host's files, beside the web's.
	policy.AllowURLSchemes("http", "https", "mailto", "gorex-file", "data")
	policy.AllowRelativeURLs(true)
	policy.RequireNoFollowOnLinks(false)
	policy.AllowStyles("text-align", "width", "height", "max-width", "float", "margin", "margin-left", "margin-right").Globally()
	policy.AllowDataURIImages()
}

// mermaidKeeper turns ```mermaid blocks into mermaid nodes, rendered as
// <pre class="mermaid"> with the source as text for the page's Mermaid,
// before the highlighter sees them as code.
type mermaidKeeper struct{}

func (m *mermaidKeeper) Extend(g goldmark.Markdown) {
	g.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(&mermaidTransformer{}, 100)))
	g.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(&mermaidRenderer{}, 100)))
}

var kindMermaid = ast.NewNodeKind("Mermaid")

type mermaidNode struct {
	ast.BaseBlock
	source []byte
}

func (n *mermaidNode) Kind() ast.NodeKind            { return kindMermaid }
func (n *mermaidNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type mermaidTransformer struct{}

func (t *mermaidTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	var found []*ast.FencedCodeBlock
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if b, ok := n.(*ast.FencedCodeBlock); ok && entering && string(b.Language(source)) == "mermaid" {
			found = append(found, b)
		}
		return ast.WalkContinue, nil
	})
	for _, b := range found {
		var src []byte
		lines := b.Lines()
		for i := range lines.Len() {
			seg := lines.At(i)
			src = append(src, seg.Value(source)...)
		}
		m := &mermaidNode{source: src}
		m.SetLines(lines)
		b.Parent().ReplaceChild(b.Parent(), b, m)
	}
}

type mermaidRenderer struct{}

func (r *mermaidRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMermaid, r.render)
}

func (r *mermaidRenderer) render(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		w.WriteString(`<pre class="mermaid"`)
		html.RenderAttributes(w, n, nil)
		w.WriteString(`>`)
		w.Write(util.EscapeHTML(n.(*mermaidNode).source))
		w.WriteString("</pre>\n")
	}
	return ast.WalkSkipChildren, nil
}

// codeWrapper wraps a highlighted code block in a div carrying its source
// line, which Chroma's own markup can't.
func codeWrapper(w util.BufWriter, ctx highlighting.CodeBlockContext, entering bool) {
	if !entering {
		w.WriteString("</div>")
		return
	}
	w.WriteString("<div")
	if v, ok := ctx.Attributes().Get([]byte("data-line")); ok {
		if line, ok := v.(string); ok {
			w.WriteString(` data-line="` + line + `"`)
		}
	}
	w.WriteString(">")
}

// lineMarker gives every block node a data-line attribute: the source
// line it starts on, from 0, which the page maps scroll positions to
// lines by, both ways. A container's line is its first leaf's.
type lineMarker struct{}

func (m *lineMarker) Extend(g goldmark.Markdown) {
	g.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(&lineTransformer{}, 200)))
}

type lineTransformer struct{}

func (t *lineTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	// Line starts, for a binary search by offset.
	starts := []int{0}
	for i, c := range source {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	lineOf := func(off int) int {
		return sort.Search(len(starts), func(i int) bool { return starts[i] > off }) - 1
	}
	var mark func(n ast.Node) int
	mark = func(n ast.Node) int {
		first := -1
		if n.Type() == ast.TypeBlock && n.Lines() != nil && n.Lines().Len() > 0 {
			first = lineOf(n.Lines().At(0).Start)
			if k := n.Kind(); (k == ast.KindFencedCodeBlock || k == kindMermaid) && first > 0 {
				first-- // the lines are the content's; the fence is above
			}
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if l := mark(c); l >= 0 && (first < 0 || l < first) {
				first = l
			}
		}
		if first >= 0 && n.Type() == ast.TypeBlock && n.Kind() != ast.KindDocument {
			n.SetAttributeString("data-line", strconv.Itoa(first))
		}
		return first
	}
	mark(doc)
}

// Render turns Markdown into an HTML fragment for the preview's page.
func Render(src string) string {
	once.Do(setup)
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "<p>" + string(util.EscapeHTML([]byte(err.Error()))) + "</p>"
	}
	return policy.Sanitize(buf.String())
}

// CSS is the stylesheet of the highlighted code, with the prefix Render's
// classes carry: the light Chroma style, and the dark one under
// [data-theme="dark"], which the page sets on its root.
func CSS(light, dark string) string {
	var buf bytes.Buffer
	f := chromahtml.New(chromahtml.WithClasses(true), chromahtml.ClassPrefix("ch-"))
	f.WriteCSS(&buf, styleOf(light))
	var d bytes.Buffer
	f.WriteCSS(&d, styleOf(dark))
	// Chroma writes a rule a line: /* Name */ .ch-x { … }.
	for line := range strings.Lines(d.String()) {
		if _, rule, ok := strings.Cut(line, "*/ "); ok {
			buf.WriteString(`[data-theme="dark"] ` + rule)
		}
	}
	return buf.String()
}

func styleOf(name string) *chroma.Style {
	if st := styles.Get(name); st != nil {
		return st
	}
	return styles.Fallback
}

// Headings lists the headings of a text in order, as anchors: the
// level, the text, and the id goldmark gives it.
type Heading struct {
	Level int
	Text  string
	ID    string
	Line  int // the line it starts on, from 0
}

func Headings(src string) []Heading {
	once.Do(setup)
	source := []byte(src)
	ctx := parser.NewContext()
	doc := md.Parser().Parse(text.NewReader(source), parser.WithContext(ctx))
	var out []Heading
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok {
			id, _ := h.AttributeString("id")
			line := 0
			if h.Lines().Len() > 0 {
				line = bytes.Count(source[:h.Lines().At(0).Start], []byte("\n"))
			}
			out = append(out, Heading{Level: h.Level, Text: string(h.Text(source)), ID: strings.TrimSpace(string(idBytes(id))), Line: line})
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return out
}

func idBytes(v any) []byte {
	switch id := v.(type) {
	case []byte:
		return id
	case string:
		return []byte(id)
	}
	return nil
}
