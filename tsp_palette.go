package main

import (
	"math"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A program's palette (verb t, feature program-palette) colors its
// surface: the variant for the window's appearance, its tokens by name
// as the protocol reads them. Text tokens color spans and the surface's
// body text, muted and dim text, the accent; fills (tokens ending in
// Bg) tint cards and selections, 60% over GoRex's own glass, so that
// its hairlines and translucency stay. Every text color is checked
// against the surface for contrast, 4.5:1 for body text and 3:1 for
// quiet tokens, and moved toward the window's text color until it
// passes: a program's dark palette in a light window stays readable.

// tspPalette is a palette's variant, parsed.
type tspPalette map[string]ui.Color

// surfacePalette returns the variant of a surface's palette for the
// window's appearance, parsed once; nil when the program sent none.
func (a *App) surfacePalette(sf *rex.Surface) tspPalette {
	if sf.Palette == nil {
		return nil
	}
	key := tspPaletteKey{sf.Palette, a.dark}
	if pal, ok := a.tspPalettes[key]; ok {
		return pal
	}
	src := sf.Palette.Light
	if a.dark || len(src) == 0 {
		src = sf.Palette.Dark
	}
	if len(src) == 0 {
		src = sf.Palette.Light
	}
	pal := tspPalette{}
	for name, v := range src {
		if c, err := parseHexColor(v); err == nil {
			c.A = 255 // alpha is ignored
			pal[name] = c
		}
	}
	if a.tspPalettes == nil || len(a.tspPalettes) >= 64 {
		a.tspPalettes = map[tspPaletteKey]tspPalette{}
	}
	a.tspPalettes[key] = pal
	return pal
}

// tspPaletteKey names a parsed palette: the palette the surface has and
// the window's appearance.
type tspPaletteKey struct {
	pal  *rex.TSPPalette
	dark bool
}

// surfaceColors are the colors a surface draws in: GoRex's, with its
// program's palette over them. It also makes the palette the one spans
// resolve their tokens through (spanColor) while the surface draws.
func (a *App) surfaceColors(k *colors, sf *rex.Surface) *colors {
	pal := a.surfacePalette(sf)
	a.tspPal = pal
	if len(pal) == 0 {
		return k
	}
	kk := *k
	bg := a.surfaceBg(k)
	if c, ok := pal["text"]; ok {
		kk.text = legible(c, bg, kk.text, 4.5)
	}
	if c, ok := pal["muted"]; ok {
		kk.textMuted = legible(c, bg, kk.text, 3)
	}
	if c, ok := pal["dim"]; ok {
		kk.textFaint = legible(c, bg, kk.text, 3)
	}
	if c, ok := pal["accent"]; ok {
		kk.busy = legible(c, bg, kk.text, 3)
	}
	if c, ok := pal["warning"]; ok {
		kk.attention = legible(c, bg, kk.text, 3)
	}
	if c, ok := pal["cardBg"]; ok {
		kk.card = tint(k.card, c)
	}
	if c, ok := pal["selectedBg"]; ok {
		kk.panelSel = tint(k.panelSel, c)
	}
	return &kk
}

// surfaceBg is about the color a surface's text lands on: the window's
// background under its glass.
func (a *App) surfaceBg(k *colors) ui.Color {
	return k.card.Over(k.bgMid)
}

// tint is a program's fill 60% over one of GoRex's, keeping its alpha.
func tint(base, fill ui.Color) ui.Color {
	t := base.Mix(fill, 0.6)
	t.A = max(base.A, 0x99)
	return t
}

// toneBg is the fill of a card of a tone, from the program's palette:
// its tool and message backgrounds; ok is false without one.
func (a *App) toneBg(k *colors, tone string) (ui.Color, bool) {
	name := map[string]string{
		"success": "toolSuccessBg", "error": "toolErrorBg", "pending": "toolPendingBg",
		"user": "userMessageBg", "info": "infoBg",
	}[tone]
	if c, ok := a.tspPal[name]; ok && name != "" {
		return tint(k.card, c), true
	}
	return ui.Color{}, false
}

// tspTokenChains are where the semantic span tokens and tones take their
// color from in a program's palette, the first token present winning.
var tspTokenChains = map[string][]string{
	"accent": {"accent"}, "success": {"success"}, "warning": {"warning"}, "error": {"error"},
	"muted": {"muted"}, "dim": {"dim"},
	"ins": {"toolDiffAdded", "success"}, "del": {"toolDiffRemoved", "error"},
	"link": {"mdLink", "accent"}, "path": {"statusLinePath", "mdLinkUrl"},
	"code": {"mdCode"}, "num": {"syntaxNumber"}, "info": {"syntaxType"},
}

// spanColor is the color of a span's style token: through the program's
// palette for a semantic token or one of its own token names
// (statusLineModel, thinkingHigh), else GoRex's; ok is false for a token
// that names no color (strong, em, icon).
func (a *App) spanColor(k *colors, tok string) (ui.Color, bool) {
	bg := a.surfaceBg(k)
	ratio := 4.5
	if tok == "muted" || tok == "dim" {
		ratio = 3
	}
	if chain, ok := tspTokenChains[tok]; ok {
		for _, name := range chain {
			if c, ok := a.tspPal[name]; ok {
				return legible(c, bg, k.text, ratio), true
			}
		}
	} else if c, ok := a.tspPal[tok]; ok {
		return legible(c, bg, k.text, ratio), true
	}
	switch tok {
	case "muted", "dim":
		return k.textMuted, true
	case "accent", "info", "link", "success":
		return k.busy, true
	case "warning", "error":
		return k.attention, true
	}
	return ui.Color{}, false
}

// legible is c, moved toward fg until it has at least ratio of contrast
// against bg.
func legible(c, bg, fg ui.Color, ratio float64) ui.Color {
	for i := 0; i < 10 && contrast(c, bg) < ratio; i++ {
		c = c.Mix(fg, 0.25)
	}
	return c
}

// contrast is the WCAG contrast ratio of two opaque colors.
func contrast(a, b ui.Color) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// relLuminance is a color's relative luminance (WCAG).
func relLuminance(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}
