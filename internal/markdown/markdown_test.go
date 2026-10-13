package markdown

import (
	"strings"
	"testing"
)

const sample = "# Title\n\nSome *em* and **strong** text with `code` and ~~gone~~.\n\n" +
	"| a | b |\n|---|---:|\n| 1 | 2 |\n\n" +
	"- [x] done\n- [ ] todo\n\n" +
	"Note[^1].\n\n[^1]: the note\n\n" +
	"```go\nfunc main() {}\n```\n\n" +
	"```mermaid\ngraph TD; A-->B;\n```\n\n" +
	"<details><summary>More</summary>hidden</details>\n\n" +
	"<script>alert(1)</script>\n\n" +
	"<img src=\"x\" onerror=\"alert(1)\">\n\n" +
	"![pic](img.png)\n\n[sibling](docs/other.md) [site](https://example.com)\n\n" +
	"## Second heading\n"

func TestRender(t *testing.T) {
	out := Render(sample)
	for _, want := range []string{
		`<h1 id="title">Title</h1>`, "<em>em</em>", "<strong>strong</strong>", "<code>code</code>", "<del>gone</del>",
		`<table>`, `text-align: right`,
		`type="checkbox"`, `checked`,
		`class="footnote-ref"`,
		`class="ch-chroma"`, `<span class="ch-kd">func</span>`,
		`<pre class="mermaid">graph TD; A--&gt;B;`,
		`<details><summary>More</summary>hidden</details>`,
		`<img src="img.png" alt="pic"`,
		`<a href="docs/other.md">sibling</a>`, `<a href="https://example.com"`,
		`<h2 id="second-heading">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, bad := range []string{"<script", "onerror", "alert("} {
		if strings.Contains(out, bad) {
			t.Errorf("kept %q in:\n%s", bad, out)
		}
	}
}

func TestHeadings(t *testing.T) {
	hs := Headings(sample)
	if len(hs) != 2 || hs[0].ID != "title" || hs[0].Level != 1 || hs[0].Line != 0 || hs[1].ID != "second-heading" || hs[1].Level != 2 {
		t.Errorf("headings %+v", hs)
	}
	if hs[1].Line == 0 || !strings.HasPrefix(strings.Split(sample, "\n")[hs[1].Line], "## ") {
		t.Errorf("second heading's line %d", hs[1].Line)
	}
}

func TestCSS(t *testing.T) {
	if css := CSS("github"); !strings.Contains(css, ".ch-kd") {
		t.Errorf("no keyword class in %q", css[:min(200, len(css))])
	}
}
