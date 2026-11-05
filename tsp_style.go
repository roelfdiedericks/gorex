package main

import "math"

// tspStyle is how surfaces are drawn, by the OMP settings: made by
// applySettings, read through App.tspLook.
type tspStyle struct {
	// font is the text's families, with the icons of the Nerd Fonts where
	// they lack them; mono the code's, and monoFeatures its font features
	// (none but ligatures off).
	font, mono   string
	monoFeatures []string
	// size and monoSize are the text's and the code's base sizes, which
	// the sizes drawn keep their ratios to (App.tspText, App.tspCode);
	// msgSize is the messages' and the composer's (App.tspMsg); lineHeight
	// is the messages'.
	size, monoSize, msgSize, lineHeight float32
	// gap scales the room between blocks and in cards, by the spacing;
	// dockW is the composer's width at most, msgW the transcript's, 0 for
	// none.
	gap, dockW, msgW float32
	// motion animates the working indicators.
	motion bool
}

// tspStyleOf returns the style of surfaces by s: the code's font, size
// and ligatures the terminal's where the OMP settings leave them unset.
func tspStyleOf(s *Settings) tspStyle {
	o := &s.OMP
	st := tspStyle{size: o.Size, lineHeight: o.LineHeight, dockW: o.DockWidth, motion: is(o.Motion), gap: 1}
	st.msgSize = o.MessageSize
	if st.msgSize <= 0 {
		st.msgSize = tspMessageSize(o.Size)
	}
	family := o.Font
	if family == "" {
		family = "system-ui"
	}
	st.font = family + ", " + symbolsFont
	st.mono = termFamily(s.Terminal.Font)
	if o.CodeFont != "" {
		st.mono = termFamily(o.CodeFont)
	}
	st.monoSize = o.CodeSize
	if st.monoSize <= 0 {
		st.monoSize = s.Terminal.Size
	}
	ligatures := o.CodeLigatures
	if ligatures == nil {
		ligatures = s.Terminal.Ligatures
	}
	if !is(ligatures) {
		st.monoFeatures = []string{"calt=0", "liga=0"}
	}
	if o.MessageWidth > 0 {
		st.msgW = o.MessageWidth
	}
	switch o.Spacing {
	case "compact":
		st.gap = 0.6
	case "roomy":
		st.gap = 1.4
	}
	return st
}

// tspLook returns the style of surfaces: the builtin settings' before
// applySettings made one.
func (a *App) tspLook() *tspStyle {
	if a.tsp.size <= 0 {
		a.tsp = tspStyleOf(&builtin)
	}
	return &a.tsp
}

// tspText is a text size of surfaces, n at the builtin text size (13),
// in the ratio of the setting to it.
func (a *App) tspText(n float32) float32 {
	return n * a.tspLook().size / tspTextSize
}

// tspMessageSize is the messages' size that follows the text's size
// given: in the ratio of the builtin sizes (14 to 13), to the half point
// the setting steps by, so that what Settings shows is what is drawn.
func tspMessageSize(size float32) float32 {
	return float32(math.Round(float64(size*tspMsgSize/tspTextSize)*2)) / 2
}

// tspMsg is a size of the messages and the composer, n at the builtin
// message size (14), in the ratio of the setting to it.
func (a *App) tspMsg(n float32) float32 {
	return n * a.tspLook().msgSize / tspMsgSize
}

// tspCode is a code size of surfaces, n at the size of code before it
// followed the settings (12), in the ratio of the code's size to it.
func (a *App) tspCode(n float32) float32 {
	return n * a.tspLook().monoSize / tspCodeSize
}

// tspGap is a room of surfaces, n at normal spacing.
func (a *App) tspGap(n float32) float32 {
	return n * a.tspLook().gap
}

// tspMotion reports whether the working indicators move: by the setting
// and the system's reduced motion.
func (a *App) tspMotion() bool {
	return a.tspLook().motion && !a.reduceMotion
}

// The sizes the surfaces' text, messages and code were drawn at before
// they followed the settings, which their sizes are given at.
const (
	tspTextSize = 13
	tspMsgSize  = 14
	tspCodeSize = 12
)
