package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"

	"gorex/internal/rex"
)

// Settings are the user's choices, in two layers: the Defaults of this PC,
// in defaults.json beside the local server's state, and the overrides of
// each host, kept with that host's server as its "settings" document, so
// that a host looks the same from any PC. A field's kind, in its tag, says
// how the layers combine:
//
//   - look: the host's value when set, else the Defaults'. Appearance, fonts.
//   - host: the host's value only; the host's own default when unset. The
//     shell, scrollback: nothing of this PC's applies to another machine.
//   - pc: the Defaults' value only, never sent to a host. Notifications,
//     reconnecting.
//
// Unset is the zero value: "" for strings, 0 for numbers, nil for *bool.
// The sections are the pages of the settings dialogue, in that order.
type Settings struct {
	V          int        `json:"v,omitempty"`
	Appearance Appearance `json:"appearance,omitzero"`
	Terminal   Terminal   `json:"terminal,omitzero"`
	Shell      Shell      `json:"shell,omitzero"`
	General    General    `json:"general,omitzero"`
}

// settingsVersion is the document's version: for migrating a field whose
// meaning changed, on read.
const settingsVersion = 1

type Appearance struct {
	// Mode is "light", "dark", or "" to follow the system.
	Mode string `json:"mode,omitempty" kind:"look" label:"Appearance" group:"Window"`
	// Theme and ThemeLight name the terminal's colors, in dark and light
	// windows, among Ghostty's themes; "GoRex" is the app's own.
	Theme      string `json:"theme,omitempty" kind:"look" label:"Colors in dark windows" group:"Terminal colors"`
	ThemeLight string `json:"themeLight,omitempty" kind:"look" label:"Colors in light windows" group:"Terminal colors"`
	// Tint colors the window's gradient and tab strip, as #rrggbb.
	Tint string `json:"tint,omitempty" kind:"look" label:"Tint" group:"Window"`
	// Name is shown for the host instead of its own name.
	Name        string `json:"name,omitempty" kind:"look" label:"Host name" group:"Window"`
	Transparent *bool  `json:"transparent,omitempty" kind:"look" label:"Transparent terminal background" group:"Window"`
}

type Terminal struct {
	Font       string  `json:"font,omitempty" kind:"look" label:"Font" group:"Text"`
	Size       float32 `json:"size,omitempty" kind:"look" label:"Size" group:"Text"`
	Weight     int     `json:"weight,omitempty" kind:"look" label:"Weight" group:"Text"`
	LineHeight float32 `json:"lineHeight,omitempty" kind:"look" label:"Line height" group:"Text"`
	Ligatures  *bool   `json:"ligatures,omitempty" kind:"look" label:"Ligatures" group:"Text"`
	Thicken    *bool   `json:"thicken,omitempty" kind:"look" label:"Thicker text" os:"darwin" group:"Text"`
	// Cursor is "block", "bar" or "underline".
	Cursor string `json:"cursor,omitempty" kind:"look" label:"Cursor" group:"Cursor"`
	Blink  *bool  `json:"blink,omitempty" kind:"look" label:"Cursor blinks" group:"Cursor"`
	// Scrollback is in lines, for the session's buffer on the server and
	// the window's.
	Scrollback   int   `json:"scrollback,omitempty" kind:"host" label:"Scrollback" group:"Buffer"`
	CopyOnSelect *bool `json:"copyOnSelect,omitempty" kind:"pc" label:"Copy on select" group:"Input and bell"`
	OptionAsAlt  *bool `json:"optionAsAlt,omitempty" kind:"pc" label:"Option as Alt" os:"darwin" group:"Input and bell"`
	// Bell is "none", "mark" (the pane asks for attention) or "sound".
	Bell string `json:"bell,omitempty" kind:"pc" label:"Bell" group:"Input and bell"`
}

type Shell struct {
	// Command is the program of new sessions, "" for the user's shell, and
	// Args its arguments, as on a command line.
	Command string `json:"command,omitempty" kind:"host" label:"Command" group:"Shell"`
	Args    string `json:"args,omitempty" kind:"host" label:"Arguments" group:"Shell"`
	Login   *bool  `json:"login,omitempty" kind:"host" label:"Login shell" group:"Shell"`
	// StartIn is "current" (the focused pane's directory) or "home".
	StartIn string `json:"startIn,omitempty" kind:"host" label:"New tabs start in" group:"Shell"`
	// Env adds to the environment of new sessions, one KEY=value per line.
	Env string `json:"env,omitempty" kind:"host" label:"Environment" group:"Shell"`
}

type General struct {
	ConfirmClose *bool `json:"confirmClose,omitempty" kind:"pc" label:"Confirm closing a pane running a program" group:"Panes"`
	Notify       *bool `json:"notify,omitempty" kind:"pc" label:"Notify when a long command finishes" group:"Notifications"`
	// NotifyAfter is how many seconds a command must have run for.
	NotifyAfter        int   `json:"notifyAfter,omitempty" kind:"pc" label:"After" group:"Notifications"`
	Attention          *bool `json:"attention,omitempty" kind:"pc" label:"Mark panes that need attention" group:"Panes"`
	ReconnectAttempts  int   `json:"reconnectAttempts,omitempty" kind:"pc" label:"Reconnect attempts" group:"Hosts"`
	LogWhileConnecting *bool `json:"logWhileConnecting,omitempty" kind:"pc" label:"Show the Connection Log while connecting" group:"Hosts"`
	// StartWith is "local" or "lastHost": what a window with no host named
	// on the command line opens.
	StartWith string `json:"startWith,omitempty" kind:"pc" label:"Open at start" group:"Hosts"`
	// WindowTitle is "host", "hostAndTab" or "tab": what the window's
	// title names.
	WindowTitle string `json:"windowTitle,omitempty" kind:"pc" label:"Window title" group:"Window"`
}

const defaultFontSize = 11.6

// builtin are the values of fields set in neither layer.
var builtin = Settings{
	V:          settingsVersion,
	Appearance: Appearance{Theme: "GoRex", ThemeLight: "GoRex", Transparent: on},
	Terminal: Terminal{
		Font: "JetBrains Mono", Size: defaultFontSize, Weight: 400, LineHeight: 1, Ligatures: on, Thicken: off,
		Cursor: "block", Blink: on, Scrollback: 10000, CopyOnSelect: off, OptionAsAlt: off, Bell: "mark",
	},
	Shell: Shell{Login: on, StartIn: "current"},
	General: General{
		ConfirmClose: off, Notify: on, NotifyAfter: 8, Attention: on, ReconnectAttempts: 5,
		LogWhileConnecting: on, StartWith: "local", WindowTitle: "host",
	},
}

var on, off = new(true), new(false)

// field is one setting: where it is in Settings, and what its tag says.
type field struct {
	section, name string // Go names, as Settings.Appearance.Mode
	key           string // the JSON path, as appearance.mode
	kind          string // look, host or pc
	label, group  string // the row's label, and the card it is in
	os            string // "" or the GOOS the field is for
	index         []int  // into Settings, for reflect
}

// fields are every setting, in the order of the sections and their
// fields, from the tags.
var fields = func() []field {
	var fs []field
	t := reflect.TypeOf(Settings{})
	for i := range t.NumField() {
		sec := t.Field(i)
		if sec.Type.Kind() != reflect.Struct {
			continue
		}
		secKey, _, _ := strings.Cut(sec.Tag.Get("json"), ",")
		for j := range sec.Type.NumField() {
			f := sec.Type.Field(j)
			key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			fs = append(fs, field{
				section: sec.Name, name: f.Name, key: secKey + "." + key,
				kind: f.Tag.Get("kind"), label: f.Tag.Get("label"), group: f.Tag.Get("group"), os: f.Tag.Get("os"),
				index: []int{i, j},
			})
		}
	}
	return fs
}()

// value returns the field in s.
func (f field) value(s *Settings) reflect.Value {
	return reflect.ValueOf(s).Elem().FieldByIndex(f.index)
}

// isSet reports whether the field has a value in s.
func (f field) isSet(s *Settings) bool { return !f.value(s).IsZero() }

// clear unsets the field in s.
func (f field) clear(s *Settings) { f.value(s).SetZero() }

// effective combines the layers: builtin, then the Defaults' look and pc
// fields, then the host's look and host fields.
func effective(defaults, host *Settings) Settings {
	eff := builtin
	for _, f := range fields {
		if f.kind != "host" && f.isSet(defaults) {
			f.value(&eff).Set(f.value(defaults))
		}
		if f.kind != "pc" && f.isSet(host) {
			f.value(&eff).Set(f.value(host))
		}
	}
	eff.V = settingsVersion
	return eff
}

// inherited reports whether the host's value of a look field is the
// Defaults', i.e. the host does not set it.
func (f field) inherited(host *Settings) bool { return f.kind == "look" && !f.isSet(host) }

// decodeSettings reads a document, leniently: unknown fields are ignored,
// missing ones unset. Migrations by V would go here.
func decodeSettings(b []byte) (s Settings) {
	if len(b) > 0 {
		json.Unmarshal(b, &s)
	}
	return s
}

func defaultsPath() string { return filepath.Join(rex.Dir(), "defaults.json") }

// loadDefaults reads this PC's Defaults.
func loadDefaults() Settings {
	b, _ := os.ReadFile(defaultsPath())
	return decodeSettings(b)
}

func saveDefaults(s Settings) {
	s.V = settingsVersion
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(rex.Dir(), 0o700); err == nil {
		err = os.WriteFile(defaultsPath()+".tmp", b, 0o600)
	}
	if err == nil {
		err = os.Rename(defaultsPath()+".tmp", defaultsPath())
	}
	if err != nil {
		log.Printf("saving defaults: %v", err)
	}
}

// encodeHost encodes a host's overrides for its server: look and host
// fields only.
func encodeHost(s Settings) []byte {
	for _, f := range fields {
		if f.kind == "pc" {
			f.clear(&s)
		}
	}
	s.V = settingsVersion
	b, _ := json.Marshal(s)
	return b
}

// lookOnly returns the look fields of s, for copying a host's look.
func lookOnly(s Settings) Settings {
	for _, f := range fields {
		if f.kind != "look" {
			f.clear(&s)
		}
	}
	return s
}

// termThemes returns the terminal's colors, for light and dark windows,
// by the names in s: the app's own for "GoRex" or a name it has no theme
// for.
func termThemes(s *Settings) (light, dark *terminal.Theme) {
	light, dark = lightTerm, darkTerm
	if th, err := terminal.GhosttyTheme(s.Appearance.ThemeLight); err == nil && !strings.EqualFold(s.Appearance.ThemeLight, "GoRex") {
		light = th
	}
	if th, err := terminal.GhosttyTheme(s.Appearance.Theme); err == nil && !strings.EqualFold(s.Appearance.Theme, "GoRex") {
		dark = th
	}
	return light, dark
}

// termFontOf returns the terminal's font by s.
func termFontOf(s *Settings) terminal.Font {
	f := terminal.Font{
		Family: s.Terminal.Font + ", SF Mono, Menlo, monospace", Size: s.Terminal.Size,
		Weight: s.Terminal.Weight, LineHeight: s.Terminal.LineHeight, Thicken: is(s.Terminal.Thicken),
	}
	if !is(s.Terminal.Ligatures) {
		f.Features = []string{"-calt", "-liga"}
	}
	return f
}

func cursorOf(s *Settings) terminal.CursorStyle {
	switch s.Terminal.Cursor {
	case "bar":
		return terminal.CursorBar
	case "underline":
		return terminal.CursorUnderline
	}
	return terminal.CursorBlock
}

// is reads a *bool, false when nil.
func is(b *bool) bool { return b != nil && *b }

// applyAppearance sets the app's theme by s.
func applyAppearance(s *Settings) {
	switch s.Appearance.Mode {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// shellOptions returns what new sessions of the host run, by s.
func shellOptions(s *Settings) rex.CreateOptions {
	o := rex.CreateOptions{Scrollback: s.Terminal.Scrollback}
	if cmd := strings.TrimSpace(s.Shell.Command); cmd != "" {
		o.Command = append([]string{cmd}, strings.Fields(s.Shell.Args)...)
		o.Login = is(s.Shell.Login)
	}
	for l := range strings.SplitSeq(s.Shell.Env, "\n") {
		if l = strings.TrimSpace(l); l != "" && strings.Contains(l, "=") {
			o.Env = append(o.Env, l)
		}
	}
	return o
}
