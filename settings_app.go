package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"

	"gorex/internal/remote"
	"gorex/internal/rex"
)

// loadSettings reads this PC's Defaults at startup and applies them,
// before any host's are known.
func (a *App) loadSettings() {
	a.defaults = loadDefaults()
	a.host = Settings{}
	a.applySettings()
}

// setHostSettings takes a host's overrides, as its server kept them, and
// applies them: on connecting, before the tabs show.
func (a *App) setHostSettings(b []byte) {
	a.host = decodeSettings(b)
	a.applySettings()
}

// applySettings makes the effective settings those of the app and the
// panes. What a terminal is made with and can't change later makes the
// panes' terminals anew, which replays their screens.
func (a *App) applySettings() {
	a.eff = effective(&a.defaults, &a.host)
	a.defEff = effective(&a.defaults, &Settings{})
	if t := &a.settings.themes; t.open && t.scope == scopeDefaults {
		// Previewing the Defaults' colors: the window shows them over the
		// host's own meanwhile.
		if t.light {
			a.eff.Appearance.ThemeLight = a.defEff.Appearance.ThemeLight
		} else {
			a.eff.Appearance.Theme = a.defEff.Appearance.Theme
		}
	}
	applyAppearance(&a.eff)
	termFont = termFontOf(&a.eff)
	light, dark := termThemes(&a.eff)
	opts := fmt.Sprint(a.eff.Appearance.Transparent != nil && *a.eff.Appearance.Transparent, a.eff.Terminal.Cursor,
		is(a.eff.Terminal.Blink), a.eff.Terminal.Scrollback, is(a.eff.Terminal.OptionAsAlt), is(a.eff.Terminal.CopyOnSelect))
	remake := a.termOpts != "" && opts != a.termOpts
	a.termOpts = opts
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.term == nil {
				continue
			}
			if remake {
				a.reattach(p)
				continue
			}
			p.term.SetFont(termFont)
			p.term.SetTheme(light, dark)
		}
	}
	a.services.Invalidate()
}

// reattach makes a pane's terminal anew, attached to the same session,
// which replays its screen.
func (a *App) reattach(p *Pane) {
	cols, rows := 0, 0
	if p.term != nil {
		cols, rows = p.term.Size()
		p.term.Close()
		p.term = nil
	}
	p.stream = nil
	a.attach(p, cols, rows)
}

// settingsChanged applies a change and has it saved: the Defaults to this
// PC's file at once, the host's overrides to its server soon.
func (a *App) settingsChanged(ofHost bool) {
	a.applySettings()
	if ofHost {
		a.settingsDue = true
		a.saveHostSettingsSoon()
	} else {
		saveDefaults(a.defaults)
	}
}

// saveHostSettingsSoon writes the host's overrides to its server after a
// moment, so that a run of changes writes once.
func (a *App) saveHostSettingsSoon() {
	time.AfterFunc(400*time.Millisecond, func() {
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			if a.settingsDue {
				a.saveHostSettings()
			}
		})
	})
}

// saveHostSettings writes the host's overrides to its server now.
func (a *App) saveHostSettings() {
	a.settingsDue = false
	client := a.client
	if client == nil {
		a.settingsErr = "Not connected: the host's settings were not saved."
		return
	}
	b := encodeHost(a.host)
	win := a.win
	go func() {
		err := client.Set("settings", b)
		if win == nil {
			return
		}
		win.Update(func() {
			if a.client != client {
				return
			}
			if err != nil {
				a.settingsErr = "Saving the host's settings failed: " + err.Error()
				log.Printf("saving settings: %v", err)
			} else {
				a.settingsErr = ""
			}
		})
	}()
}

// setAppearance sets the current host's appearance, as the menu and the
// palette do.
func (a *App) setAppearance(v string) {
	a.host.Appearance.Mode = v
	a.settingsChanged(true)
}

// setFontSize changes the size of the terminals' text, for the current
// host.
func (a *App) setFontSize(size float32) {
	a.host.Terminal.Size = min(max(size, 8), 32)
	a.settingsChanged(true)
}

// copyLook copies the look of one host's settings into another's.
func copyLook(from Settings, to *Settings) {
	src := lookOnly(from)
	for _, f := range fields {
		if f.kind == "look" {
			f.value(to).Set(f.value(&src))
		}
	}
}

// fetchSettings reads a host's overrides from its server.
func fetchSettings(client *rex.Client) (Settings, error) {
	b, err := client.Get("settings")
	if err != nil {
		return Settings{}, err
	}
	return decodeSettings(b), nil
}

// readHostSettings connects to a host, as the window would, reads its
// settings and disconnects: for copying a look from a host the window is
// not on. It logs what connecting prints, and shows no status.
func readHostSettings(spec string) (Settings, error) {
	t, err := remote.Resolve(spec)
	if err != nil {
		return Settings{}, err
	}
	client, link, _, err := dialBinding(binding{remote: true, target: t}, remote.Progress{
		Status: func(string) {}, Log: func(s string) { log.Print(s) },
	}, false)
	if err != nil {
		return Settings{}, err
	}
	defer client.Close()
	if link != nil {
		defer link.Close()
	}
	return fetchSettings(client)
}

// confirmClose asks before closing panes running a program, when the
// settings say so; it reports whether to go on.
func (a *App) confirmClose(panes []*Pane) bool {
	if !is(a.eff.General.ConfirmClose) {
		return true
	}
	var running []string
	for _, p := range panes {
		if !p.info.Idle && p.info.Program != "" && !p.info.Exited {
			running = append(running, programOf(p.info.Program).Name)
		}
	}
	if len(running) == 0 {
		return true
	}
	what := running[0] + " is running"
	if len(running) > 1 {
		what = fmt.Sprintf("%d programs are running", len(running))
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: a.win, Type: mygo.MessageWarning, Title: "Close the pane?",
		Message: what + " in it. Closing the pane ends it.",
		Buttons: []string{"Close", "Cancel"}, DefaultButton: 0, CancelButton: 1,
	})
	return err != nil || res.Button == 0
}
