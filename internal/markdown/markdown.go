// Package markdown renders Markdown to HTML for a preview: GitHub's
// dialect, code highlighted by Chroma with classes a stylesheet colors,
// Mermaid blocks left for the page to draw, and raw HTML kept but
// sanitised, as the text may be anyone's.
package markdown

import (
	"bytes"
	"regexp"
	"strings"
	"sync"

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
			),
			&mermaidKeeper{},
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
		b.Parent().ReplaceChild(b.Parent(), b, m)
	}
}

type mermaidRenderer struct{}

func (r *mermaidRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMermaid, r.render)
}

func (r *mermaidRenderer) render(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		w.WriteString(`<pre class="mermaid">`)
		w.Write(util.EscapeHTML(n.(*mermaidNode).source))
		w.WriteString("</pre>\n")
	}
	return ast.WalkSkipChildren, nil
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

// CSS is the stylesheet of the highlighted code, for the Chroma style
// name, with the prefix Render's classes carry.
func CSS(style string) string {
	st := styles.Get(style)
	if st == nil {
		st = styles.Fallback
	}
	var buf bytes.Buffer
	f := chromahtml.New(chromahtml.WithClasses(true), chromahtml.ClassPrefix("ch-"))
	f.WriteCSS(&buf, st)
	return buf.String()
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
