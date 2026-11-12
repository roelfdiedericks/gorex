//go:build gorexserver

package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// The server for ssh hosts draws no surfaces, so it is built without
// mmdg (tsp_mermaid.go): a Mermaid block is its code.

type tspMermaid struct{}

func (a *App) tspMermaidBlock(c *ui.Context, k *colors, p *Pane, b *markdown.CodeBlockView) {
	markdown.DefaultCodeBlock(c, b)
}

func (a *App) sharpenDiagram(v *tspViewer) {}
