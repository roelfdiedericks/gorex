package main

import (
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// settingsUI is the state of the settings dialogue: a sidebar of pages in
// two scopes, the current host's and this PC's Defaults, and a search
// over every row of both.
type settingsUI struct {
	open  bool
	page  string // "host/Terminal", "defaults/General", "defaults/Hosts"
	query string
	// reveal is the key of a row to scroll into view in the next frame,
	// and flashed when each row was last revealed, to flash it.
	reveal  string
	flashed map[string]time.Time
	scroll  ui.ScrollState
	// lastHit is the search result chosen last, kept marked.
	lastHit string
	// copyFrom is the source chosen on the Hosts page, and copyMsg how
	// the last copy went.
	copyFrom, copyMsg string
	themes            themesUI
}

// scope is where a page's values live.
const (
	scopeHost     = "host"
	scopeDefaults = "defaults"
)

var sections = []string{"Appearance", "Terminal", "Shell", "General"}

// pages lists the sidebar's pages of a scope: the sections with rows in
// that scope, and Hosts under the Defaults.
func pagesOf(scope string) []string {
	var ps []string
	for _, sec := range sections {
		for _, f := range fields {
			if f.section == sec && f.inScope(scope) {
				ps = append(ps, sec)
				break
			}
		}
	}
	if scope == scopeDefaults {
		ps = append(ps, "Hosts")
	}
	return ps
}

// inScope reports whether a field is edited in a scope: look fields in
// both, host fields on the host, pc fields in the Defaults.
func (f field) inScope(scope string) bool {
	if f.os != "" && f.os != runtime.GOOS {
		return false
	}
	switch f.kind {
	case "host":
		return scope == scopeHost
	case "pc":
		return scope == scopeDefaults
	}
	return true
}

func (a *App) openSettings() {
	s := &a.settings
	s.open, s.query = true, ""
	if s.page == "" {
		s.page = scopeHost + "/Appearance"
	}
	s.copyMsg = ""
	a.remotes = loadRemotes()
	a.focusReq = nil
}

// showSetting opens the settings at a row, revealed and flashed.
func (a *App) showSetting(scope string, f field) {
	s := &a.settings
	s.page = scope + "/" + f.section
	s.reveal = scope + "/" + f.key
	if s.flashed == nil {
		s.flashed = map[string]time.Time{}
	}
	s.flashed[s.reveal] = time.Now()
}

// settingsDialog draws the settings over the window.
func (a *App) settingsDialog(c *ui.Context, k *colors) {
	s := &a.settings
	if !s.open {
		if s.themes.open {
			a.endThemePreview(false)
		}
		return
	}
	w, h := float32(760), float32(520)
	if r := c.Root().Bounds(); r.W > 0 {
		w, h = min(w, r.W-24), min(h, r.H-24)
	}
	ui.DialogBase(c, &s.open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(w).Height(h).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Clip()
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.settingsSidebar(c, k)
			if s.themes.open {
				a.themePicker(c, k)
			} else {
				a.settingsPage(c, k)
			}
		})
	})
}

// settingsSidebar is the search field and the pages, or the search's
// results.
func (a *App) settingsSidebar(c *ui.Context, k *colors) {
	s := &a.settings
	ui.Column(c).Width(210).FillHeight().Padding(12, 10).Gap(8).Background(k.hover).Shrink(0).Children(func() {
		sf := ui.SearchField(c, &s.query).Placeholder("Search settings")
		if !s.themes.open && !a.focusedOnce(c, "settings-search") {
			sf.Focus()
		}
		hostLabel := a.hostName()
		if q := strings.TrimSpace(strings.ToLower(s.query)); q != "" {
			a.searchResults(c, k, q, hostLabel)
			return
		}
		sidebar := ui.Sidebar(c, &s.page, func() {
			ui.SidebarSection(c, "This window · "+hostLabel, nil, func() {
				for _, p := range pagesOf(scopeHost) {
					ui.SidebarItem(c, scopeHost+"/"+p, icon(pageGlyph(p)), p)
				}
			})
			ui.SidebarSection(c, "Defaults · "+thisMachine(), nil, func() {
				for _, p := range pagesOf(scopeDefaults) {
					ui.SidebarItem(c, scopeDefaults+"/"+p, icon(pageGlyph(p)), p)
				}
			})
		}).Grow(1).Label("Settings pages")
		if sidebar.Changed() {
			s.themes.open = false
		}
	})
}

func pageGlyph(page string) string {
	switch page {
	case "Appearance":
		return "sparkles"
	case "Terminal":
		return "square-terminal"
	case "Shell":
		return "terminal"
	case "General":
		return "settings-2"
	case "Hosts":
		return "server"
	}
	return "circle-dot"
}

// focusedOnce reports whether something was focused for key already, and
// records that it is now: AutoFocus once per opening.
func (a *App) focusedOnce(c *ui.Context, key string) bool {
	s := &a.settings
	if s.flashed == nil {
		s.flashed = map[string]time.Time{}
	}
	if _, ok := s.flashed["focus/"+key]; ok {
		return true
	}
	s.flashed["focus/"+key] = time.Now()
	return false
}

// searchResults lists the rows matching q, page by page, in both scopes.
func (a *App) searchResults(c *ui.Context, k *colors, q, hostLabel string) {
	s := &a.settings
	n := 0
	ui.Scroll(c).Grow(1).Children(func() {
		for _, scope := range []string{scopeHost, scopeDefaults} {
			label := "This window · " + hostLabel
			if scope == scopeDefaults {
				label = "Defaults · " + thisMachine()
			}
			for _, page := range pagesOf(scope) {
				var hits []field
				for _, f := range fields {
					if f.section == page && f.inScope(scope) && fuzzy(strings.ToLower(f.label+" "+f.group+" "+page), q) {
						hits = append(hits, f)
					}
				}
				if len(hits) == 0 {
					continue
				}
				if n == 0 {
					ui.Text(c, strings.ToUpper(label)).FontSize(10.5).FontWeight(700).TextColor(k.textFaint).Padding(6, 8, 2, 8)
				} else {
					ui.Text(c, strings.ToUpper(label)).FontSize(10.5).FontWeight(700).TextColor(k.textFaint).Padding(10, 8, 2, 8)
				}
				ui.Text(c, page).FontSize(13).FontWeight(600).TextColor(k.text).Padding(4, 8)
				for _, f := range hits {
					n++
					key := scope + "/" + f.key
					row := ui.Row(c.Key(key)).Height(26).Padding(0, 8, 0, 20).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(f.label)
					if s.reveal == key || s.page == scope+"/"+page && s.lastHit == key {
						row.Background(k.panelSel)
					} else if row.Hovered() {
						row.Background(k.pressed)
					}
					if row.Clicked() {
						s.lastHit = key
						s.themes.open = false
						a.showSetting(scope, f)
					}
					row.Children(func() {
						ui.Text(c, f.label).FontSize(12.5).TextColor(k.text).SingleLine().Ellipsis("…")
					})
				}
			}
		}
		if n == 0 {
			ui.Text(c, "No settings match.").FontSize(12.5).TextColor(k.textFaint).Padding(8)
		}
	})
}

// settingsPage draws the page the sidebar chose: a header saying the
// scope, then the rows in cards by group.
func (a *App) settingsPage(c *ui.Context, k *colors) {
	s := &a.settings
	scope, page, _ := strings.Cut(s.page, "/")
	hostLabel := a.hostName()
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Column(c).Padding(18, 22, 12, 22).Gap(4).Shrink(0).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, page).FontSize(16).FontWeight(700).TextColor(k.text).Grow(1)
				if iconButton(c, k, "x", "Close", 26, 14).Clicked() {
					s.open = false
				}
			})
			where := "Defaults · " + thisMachine() + " · hosts without a value of their own follow these"
			if scope == scopeHost {
				where = "This window · " + hostLabel + " · set here, applies to this host from any PC"
				if a.client == nil {
					where = "This window · " + hostLabel + " · not connected: its settings can't be changed now"
				}
			}
			ui.Text(c, where).FontSize(12).TextColor(k.textFaint).SingleLine().Ellipsis("…")
			if a.settingsErr != "" {
				ui.Text(c, a.settingsErr).FontSize(12).TextColor(ui.Hex("#ef4444"))
			}
		})
		ui.Divider(c)
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(14, 22, 18, 22).TrackScroll(&s.scroll).Children(func() {
			if page == "Hosts" {
				a.hostsPage(c, k)
				return
			}
			a.sectionRows(c, k, scope, page)
		})
	})
}

// sectionRows draws the rows of a section in a scope, grouped in cards.
func (a *App) sectionRows(c *ui.Context, k *colors, scope, section string) {
	var groups []string
	byGroup := map[string][]field{}
	for _, f := range fields {
		if f.section != section || !f.inScope(scope) {
			continue
		}
		if _, ok := byGroup[f.group]; !ok {
			groups = append(groups, f.group)
		}
		byGroup[f.group] = append(byGroup[f.group], f)
	}
	ui.Column(c).Gap(18).Children(func() {
		for _, g := range groups {
			ui.Column(c.Key(g)).Gap(8).Children(func() {
				ui.Text(c, g).FontSize(13).FontWeight(700).TextColor(k.text)
				ui.Column(c).Radius(10).Background(k.hover).Clip().Children(func() {
					for i, f := range byGroup[g] {
						if i > 0 {
							ui.Divider(c).Margin(0, 14)
						}
						a.settingRow(c, k, scope, f)
					}
				})
				if note := a.groupNote(scope, byGroup[g]); note != "" {
					ui.Text(c, note).FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
				}
			})
		}
	})
}

// groupNote says, under a card on a host page, which look rows the host
// sets and which it inherits.
func (a *App) groupNote(scope string, fs []field) string {
	if scope != scopeHost {
		return ""
	}
	var set, inherited, only []string
	for _, f := range fs {
		switch {
		case f.kind == "host":
			only = append(only, strings.ToLower(f.label))
		case f.isSet(&a.host):
			set = append(set, strings.ToLower(f.label))
		default:
			inherited = append(inherited, strings.ToLower(f.label))
		}
	}
	var parts []string
	if len(set) > 0 {
		parts = append(parts, "Set for this host: "+strings.Join(set, ", ")+".")
	}
	if len(inherited) > 0 {
		parts = append(parts, "Inherited from Defaults: "+strings.Join(inherited, ", ")+".")
	}
	if len(only) > 0 {
		parts = append(parts, "This host only: "+strings.Join(only, ", ")+".")
	}
	return strings.Join(parts, " ")
}

// settingRow draws one row: the label, the control, and on a host page
// the row's state with Reset when the host sets a look field.
func (a *App) settingRow(c *ui.Context, k *colors, scope string, f field) {
	s := &a.settings
	key := scope + "/" + f.key
	row := ui.Row(c.Key(f.key)).MinHeight(40).Padding(6, 14).Gap(12).AlignItems(ui.Center).
		Transition(ui.ElementTransition{Colors: true, Duration: 900 * time.Millisecond})
	if at, ok := s.flashed[key]; ok && time.Since(at) < 1500*time.Millisecond {
		if time.Since(at) < 120*time.Millisecond {
			row.Background(ui.Hex("#2e6fd0").Alpha(0.35))
		}
		c.Invalidate()
	}
	if s.reveal == key {
		row.ScrollIntoView()
		s.reveal = ""
	}
	disabled := scope == scopeHost && a.client == nil
	row.Children(func() {
		ui.Text(c, f.label).FontSize(13).TextColor(k.text).Grow(1).MinWidth(0)
		if disabled {
			ui.Text(c, a.rowValue(scope, f)).FontSize(12.5).TextColor(k.textFaint)
			return
		}
		a.settingControl(c, k, scope, f)
		if scope == scopeHost && f.kind == "look" {
			if f.isSet(&a.host) {
				if textButton(c, k, "Reset").Tooltip("Back to the Defaults' value").Clicked() {
					f.clear(&a.host)
					a.settingsChanged(true)
				}
			} else {
				ui.Text(c, "inherited").FontSize(11.5).TextColor(k.textFaint).Shrink(0)
			}
		}
	})
}

// rowValue shows a field's effective value as text, for a page that can't
// be edited.
func (a *App) rowValue(scope string, f field) string {
	v := f.value(&a.eff)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "off"
		}
		if v.Elem().Bool() {
			return "on"
		}
		return "off"
	}
	return fmt.Sprint(v.Interface())
}

// settingControl draws the control editing f in the scope's layer; a
// change applies and saves. Look rows on a host page show the effective
// value until edited, which makes the value the host's.
func (a *App) settingControl(c *ui.Context, k *colors, scope string, f field) {
	layer := &a.defaults
	if scope == scopeHost {
		layer = &a.host
	}
	changed := func() { a.settingsChanged(scope == scopeHost) }
	effv := f.value(&a.eff)
	target := f.value(layer)
	switch f.key {
	case "appearance.mode":
		choices(c, &target, effv, []string{"", "light", "dark"}, []string{"System", "Light", "Dark"}, changed)
	case "appearance.theme", "appearance.themeLight":
		cur := effv.String()
		ui.Text(c, cur).FontSize(12.5).TextColor(k.textMuted).MaxWidth(200).SingleLine().Ellipsis("…")
		if textButton(c, k, "Choose…").Clicked() {
			a.startThemePreview(scope, f.key == "appearance.themeLight")
		}
	case "appearance.tint":
		tintControl(c, k, &target, effv, changed)
	case "terminal.cursor":
		choices(c, &target, effv, []string{"block", "bar", "underline"}, []string{"Block", "Bar", "Underline"}, changed)
	case "terminal.bell":
		choices(c, &target, effv, []string{"none", "mark", "sound"}, []string{"None", "Mark the pane", "Sound"}, changed)
	case "shell.startIn":
		choices(c, &target, effv, []string{"current", "home"}, []string{"The current pane's directory", "Home"}, changed)
	case "general.startWith":
		choices(c, &target, effv, []string{"local", "lastHost"}, []string{thisMachine(), "The last host"}, changed)
	case "general.windowTitle":
		choices(c, &target, effv, []string{"host", "hostAndTab", "tab"}, []string{"Host", "Host and tab", "Tab"}, changed)
	case "terminal.font":
		v := effv.String()
		if ui.Combobox(c, &v, fontChoices).Width(220).Changed() {
			target.SetString(v)
			changed()
		}
	case "terminal.weight":
		v := strconv.Itoa(int(effv.Int()))
		if ui.Select(c, &v, []string{"300", "400", "500", "600", "700"}).Width(90).Changed() {
			n, _ := strconv.Atoi(v)
			target.SetInt(int64(n))
			changed()
		}
	case "terminal.size":
		stepper(c, k, &target, effv, 8, 32, 0.5, changed)
	case "terminal.lineHeight":
		stepper(c, k, &target, effv, 0.8, 1.6, 0.05, changed)
	case "terminal.scrollback":
		stepper(c, k, &target, effv, 500, 200000, 500, changed)
	case "general.notifyAfter":
		stepper(c, k, &target, effv, 1, 600, 1, changed)
	case "general.reconnectAttempts":
		stepper(c, k, &target, effv, 1, 10, 1, changed)
	case "shell.env":
		v := target.String()
		if ui.TextArea(c, &v).Width(300).Height(64).Placeholder("KEY=value, one per line").Changed() {
			target.SetString(v)
			changed()
		}
	default:
		switch target.Kind() {
		case reflect.Pointer: // *bool
			val := is(effv.Interface().(*bool))
			if ui.Switch(c, &val).Changed() {
				target.Set(reflect.ValueOf(&val))
				changed()
			}
		case reflect.String:
			v := target.String()
			in := ui.TextInput(c, &v).Width(260)
			if ph := a.placeholder(f); ph != "" {
				in.Placeholder(ph)
			}
			if in.Changed() {
				target.SetString(v)
				changed()
			}
		default:
			ui.Text(c, fmt.Sprint(effv.Interface())).FontSize(12.5).TextColor(k.textMuted)
		}
	}
}

// placeholder is what a host row shows when empty: the host's own.
func (a *App) placeholder(f field) string {
	switch f.key {
	case "shell.command":
		if a.hello.Host.GOOS == "windows" {
			return "PowerShell, or $SHELL"
		}
		return "the login shell, $SHELL"
	case "appearance.name":
		return a.hello.Host.Name
	case "terminal.font":
		return "JetBrains Mono"
	}
	return ""
}

var fontChoices = []string{"JetBrains Mono", "Cascadia Code", "Cascadia Mono", "Consolas", "Fira Code", "Hack", "Menlo", "SF Mono", "Source Code Pro", "monospace"}

// choices is a segmented control over values, showing labels; the
// effective value shows until a choice sets the layer's.
func choices(c *ui.Context, target *reflect.Value, eff reflect.Value, values, labels []string, changed func()) {
	sel := 0
	for i, v := range values {
		if v == eff.String() {
			sel = i
		}
	}
	if ui.Segmented(c, &sel, labels...).Changed() {
		target.SetString(values[sel])
		changed()
	}
}

// stepper edits a number, as a stepper beside its value: the effective
// value, until a step sets the layer's.
func stepper(c *ui.Context, k *colors, target *reflect.Value, eff reflect.Value, lo, hi, step float64, changed func()) {
	v := numberOf(eff)
	decimals := 0
	for s := step; s < 1 && decimals < 4; s *= 10 {
		decimals++
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, strconv.FormatFloat(v, 'f', decimals, 64)).FontSize(13).TextColor(k.text).Width(56).SingleLine()
		if ui.Stepper(c, &v, lo, hi, step).Changed() {
			setNumber(target, roundTo(v, step))
			changed()
		}
	})
}

// numberOf reads an int or float field as float64.
func numberOf(v reflect.Value) float64 {
	switch v.Kind() {
	case reflect.Int, reflect.Int64:
		return float64(v.Int())
	case reflect.Float32, reflect.Float64:
		return v.Float()
	}
	return 0
}

func setNumber(v *reflect.Value, n float64) {
	switch v.Kind() {
	case reflect.Int, reflect.Int64:
		v.SetInt(int64(n))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(n)
	}
}

func roundTo(v, step float64) float64 {
	n := v / step
	return float64(int64(n+0.5*sign(n))) * step
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// tintControl is a color well and a way to have no tint.
func tintControl(c *ui.Context, k *colors, target *reflect.Value, eff reflect.Value, changed func()) {
	col := ui.Hex("#2e6fd0")
	if hex := eff.String(); hex != "" {
		col = ui.Hex(hex)
	}
	if ui.ColorWell(c, &col).Changed() {
		target.SetString(hexOf(col))
		changed()
	}
	if eff.String() == "" {
		ui.Text(c, "none").FontSize(12.5).TextColor(k.textFaint)
	} else if textButton(c, k, "None").Clicked() {
		target.SetString("none")
		changed()
	}
}

// hexOf formats a color as #rrggbb.
func hexOf(col ui.Color) string {
	s := col.SRGB()
	return fmt.Sprintf("#%02x%02x%02x", s.R, s.G, s.B)
}

// hostsPage lists the known hosts, with Forget, and copies a look between
// hosts and the Defaults.
func (a *App) hostsPage(c *ui.Context, k *colors) {
	s := &a.settings
	ui.Column(c).Gap(18).Children(func() {
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, "Known hosts").FontSize(13).FontWeight(700).TextColor(k.text)
			ui.Column(c).Radius(10).Background(k.hover).Clip().Children(func() {
				if len(a.remotes) == 0 {
					ui.Text(c, "None yet: connect to one with ssh user@host in the palette, or GoRex ssh user@host.").
						FontSize(12.5).TextColor(k.textFaint).Padding(12, 14)
				}
				for i, r := range a.remotes {
					if i > 0 {
						ui.Divider(c).Margin(0, 14)
					}
					ui.Row(c.Key(r.Key)).MinHeight(40).Padding(6, 14).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, r.title()).FontSize(13).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…")
							ui.Text(c, r.Target+" · "+roundDur(time.Since(r.LastUsed))+" ago").FontSize(11.5).TextColor(k.textFaint).SingleLine().Ellipsis("…")
						})
						if "ssh/"+r.Key == a.bind.key() {
							ui.Text(c, "this window").FontSize(11.5).TextColor(k.textFaint)
						} else if textButton(c, k, "Forget").Clicked() {
							forgetRemote(r.Key)
							a.remotes = loadRemotes()
						}
					})
				}
			})
		})
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, "Copy a look").FontSize(13).FontWeight(700).TextColor(k.text)
			ui.Column(c).Radius(10).Background(k.hover).Clip().Padding(10, 14).Gap(8).Children(func() {
				sources := []string{"Defaults · " + thisMachine()}
				for _, r := range a.remotes {
					if "ssh/"+r.Key != a.bind.key() {
						sources = append(sources, r.title()+" ("+r.Target+")")
					}
				}
				if s.copyFrom == "" {
					s.copyFrom = sources[0]
				}
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "From").FontSize(13).TextColor(k.text)
					ui.Select(c, &s.copyFrom, sources).Width(260)
					ui.Text(c, "to this window's host").FontSize(13).TextColor(k.text).Grow(1)
					if textButton(c, k, "Copy").Clicked() {
						a.copyLookFrom(s.copyFrom)
					}
				})
				if s.copyMsg != "" {
					ui.Text(c, s.copyMsg).FontSize(11.5).TextColor(k.textFaint)
				}
			})
			ui.Text(c, "Copies the appearance and terminal rows into this window's host, as its own values. The source host must be reachable.").
				FontSize(11.5).TextColor(k.textFaint).LineHeight(1.35)
		})
	})
}

// copyLookFrom copies the look of the source named, as the Hosts page
// lists it, into the current host's settings.
func (a *App) copyLookFrom(source string) {
	s := &a.settings
	if a.client == nil {
		s.copyMsg = "Not connected to this window's host."
		return
	}
	if strings.HasPrefix(source, "Defaults") {
		copyLook(a.defaults, &a.host)
		a.settingsChanged(true)
		s.copyMsg = "Copied the Defaults' look."
		return
	}
	var from *savedRemote
	for i := range a.remotes {
		if source == a.remotes[i].title()+" ("+a.remotes[i].Target+")" {
			from = &a.remotes[i]
		}
	}
	if from == nil {
		return
	}
	s.copyMsg = "Reading " + from.title() + "'s settings…"
	win, target := a.win, from.Target
	go func() {
		src, err := readHostSettings(target)
		if win == nil {
			return
		}
		win.Update(func() {
			if err != nil {
				s.copyMsg = "Couldn't read " + target + ": " + err.Error()
				return
			}
			copyLook(src, &a.host)
			a.settingsChanged(true)
			s.copyMsg = "Copied " + target + "'s look."
		})
	}()
}

// themesUI is the theme picker's state: a grid of every theme, filtered
// by a search, previewing in the window as a card is chosen.
type themesUI struct {
	open    bool
	scope   string
	light   bool // which slot is being chosen: light or dark windows
	query   string
	grid    ui.GridState
	sel     int
	orig    string // the layer's own value before previewing, for Esc
	origSet bool
	names   []string
	cache   map[string]*terminal.Theme
	picked  string
}

func (a *App) startThemePreview(scope string, light bool) {
	t := &a.settings.themes
	layer := &a.host
	if scope == scopeDefaults {
		layer = &a.defaults
	}
	t.open, t.scope, t.light, t.query = true, scope, light, ""
	t.orig, t.picked = layer.Appearance.Theme, a.eff.Appearance.Theme
	if light {
		t.orig, t.picked = layer.Appearance.ThemeLight, a.eff.Appearance.ThemeLight
	}
	t.origSet = t.orig != ""
	t.names = append([]string{"GoRex"}, terminal.GhosttyThemes()...)
	if t.cache == nil {
		t.cache = map[string]*terminal.Theme{}
	}
	t.sel = 0
	for i, n := range t.names {
		if strings.EqualFold(n, t.picked) {
			t.sel = i
		}
	}
	t.grid.Selected = &t.sel
}

// previewTheme shows a theme in the window without saving it.
func (a *App) previewTheme(name string) {
	t := &a.settings.themes
	t.picked = name
	layer := &a.host
	if t.scope == scopeDefaults {
		layer = &a.defaults
	}
	if t.light {
		layer.Appearance.ThemeLight = name
	} else {
		layer.Appearance.Theme = name
	}
	a.applySettings()
}

// endThemePreview keeps the theme previewed, or puts the one before back.
func (a *App) endThemePreview(keep bool) {
	t := &a.settings.themes
	if !t.open {
		return
	}
	t.open = false
	if keep {
		a.settingsChanged(t.scope == scopeHost)
		return
	}
	layer := &a.host
	if t.scope == scopeDefaults {
		layer = &a.defaults
	}
	if t.light {
		layer.Appearance.ThemeLight = t.orig
	} else {
		layer.Appearance.Theme = t.orig
	}
	a.applySettings()
}

// themePicker draws the theme grid over the page.
func (a *App) themePicker(c *ui.Context, k *colors) {
	t := &a.settings.themes
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Column(c).Padding(18, 22, 12, 22).Gap(8).Shrink(0).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, "Terminal colors").FontSize(16).FontWeight(700).TextColor(k.text).Grow(1)
				slot := 0
				if t.light {
					slot = 1
				}
				if ui.Segmented(c, &slot, "Dark windows", "Light windows").Changed() {
					a.endThemePreview(true)
					a.startThemePreview(t.scope, slot == 1)
				}
			})
			sf := ui.SearchField(c, &t.query).Placeholder("Search themes")
			if !a.focusedOnce(c, "themes-search") {
				sf.Focus()
			}
			if sf.Shortcut(0, ui.KeyDown) {
				t.sel = min(t.sel+1, len(t.filtered())-1)
				a.previewTheme(t.filtered()[t.sel])
			}
			if sf.Shortcut(0, ui.KeyUp) {
				t.sel = max(t.sel-1, 0)
				a.previewTheme(t.filtered()[t.sel])
			}
			if sf.Submitted() {
				a.endThemePreview(true)
			}
		})
		ui.Divider(c)
		names := t.filtered()
		if t.sel >= len(names) {
			t.sel = max(len(names)-1, 0)
		}
		grid := ui.GridView(c, &t.grid, len(names), 150, 96, func(i int) {
			a.themeCard(c, k, names[i], names[i] == t.picked)
		}).Grow(1).MinHeight(0).Padding(12, 16)
		if grid.Changed() && t.sel >= 0 && t.sel < len(names) {
			a.previewTheme(names[t.sel])
		}
		if grid.Submitted() {
			a.endThemePreview(true)
		}
		ui.Divider(c)
		ui.Row(c).Padding(10, 22).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprintf("%d of %d · previewing in this window · ↑↓ or click, Enter keeps, Esc goes back", len(names), len(t.names))).
				FontSize(11.5).TextColor(k.textFaint).Grow(1).SingleLine().Ellipsis("…")
			if textButton(c, k, "Back").Clicked() {
				a.endThemePreview(false)
			}
			if textButton(c, k, "Apply").Clicked() {
				a.endThemePreview(true)
			}
		})
	})
}

// filtered returns the theme names containing the search, those starting
// with it first.
func (t *themesUI) filtered() []string {
	q := strings.ToLower(strings.TrimSpace(t.query))
	if q == "" {
		return t.names
	}
	var first, rest []string
	for _, n := range t.names {
		l := strings.ToLower(n)
		if strings.HasPrefix(l, q) {
			first = append(first, n)
		} else if strings.Contains(l, q) {
			rest = append(rest, n)
		}
	}
	return append(first, rest...)
}

// theme returns a theme by name, the app's own for "GoRex".
func (t *themesUI) theme(name string) *terminal.Theme {
	if th, ok := t.cache[name]; ok {
		return th
	}
	var th *terminal.Theme
	if strings.EqualFold(name, "GoRex") {
		if t.light {
			th = lightTerm
		} else {
			th = darkTerm
		}
	} else if g, err := terminal.GhosttyTheme(name); err == nil {
		th = g
	} else {
		th = darkTerm
	}
	t.cache[name] = th
	return th
}

// themeCard draws a theme: a prompt line in its colors, its palette, and
// its name.
func (a *App) themeCard(c *ui.Context, k *colors, name string, current bool) {
	th := a.settings.themes.theme(name)
	card := ui.Column(c).Grow(1).Padding(8).Gap(4).Radius(8).Background(th.Background).Border(1, k.panelBorder).Clip()
	if current {
		card.Border(2, ui.Hex("#2e6fd0"))
	}
	card.Children(func() {
		ui.Text(c, "user@host:~$ ls").Font(termFont.Family).FontSize(10).TextColor(th.Foreground).SingleLine()
		ui.Row(c).Gap(2).Children(func() {
			for i := 1; i < 8; i++ {
				ui.Box(c).Size(10, 10).Radius(2).Background(th.Palette[i])
			}
		})
		ui.Text(c, name).FontSize(11).TextColor(th.Foreground).SingleLine().Ellipsis("…")
		if luminance(th.Background) > 0.5 {
			ui.Text(c, "light").FontSize(9.5).TextColor(th.Foreground.Alpha(0.6))
		}
	})
}

func luminance(col ui.Color) float32 {
	s := col.SRGB()
	return (0.2126*float32(s.R) + 0.7152*float32(s.G) + 0.0722*float32(s.B)) / 255
}
