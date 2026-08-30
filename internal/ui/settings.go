// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"slices"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/config"
)

// The settings modal: the only thing in smartview that writes the config file.
// The T and +/- keys stay session-only, exactly as the README documents.

// Modal size: a border, one row per setting, a blank, and the button row.
// Vertical padding is 0 for density, as everywhere else in the UI.
const settingsWidth = 52

// settingsHeight is the border, one row per setting, a blank and the button
// row. Derived from settingsRows so adding a setting cannot leave the box too
// short to show the buttons.
var settingsHeight = len(settingsRows) + 4

// settingsRows names the form's rows, in order. Its length sizes the modal, so
// adding a setting cannot leave the box too short to show the buttons.
var settingsRows = []string{
	"Theme", "Refresh", "Skip spun-down drives",
	"Show unavailable tabs", "Start view (next run)",
}

// currentConfig snapshots the live settings. Derived rather than stored: a
// stored copy would drift the moment T or +/- changed the session outside the
// modal, and Save would then silently revert the theme the user is looking at.
func (a *App) currentConfig() config.Config {
	return config.Config{
		Theme:               a.themeName,
		RefreshInterval:     config.Duration(a.interval),
		StandbyAware:        a.standbyAware.Load(),
		ShowUnavailableTabs: a.detail.showAllTabs,
		StartView:           a.startView,
	}
}

// settingsForm builds the editor over a working copy of cfg. Every callback
// writes to that copy, which is what makes Cancel free: nothing outside the
// closure changes until Save runs.
func (a *App) settingsForm(cfg config.Config) *tview.Form {
	form := tview.NewForm()
	form.SetItemPadding(0) // density, matching every other box in the UI
	intervals := intervalChoices(cfg.RefreshInterval.Duration())
	views := []string{config.StartDrives, config.StartFleet}

	form.AddDropDown(settingsRows[0], themeCycle, indexOr(themeCycle, cfg.Theme, 0),
		func(opt string, _ int) { cfg.Theme = opt })
	form.AddDropDown(settingsRows[1], labelDurations(intervals),
		indexOr(intervals, cfg.RefreshInterval.Duration(), 0),
		func(_ string, i int) {
			if i >= 0 && i < len(intervals) {
				cfg.RefreshInterval = config.Duration(intervals[i])
			}
		})
	form.AddCheckbox(settingsRows[2], cfg.StandbyAware,
		func(v bool) { cfg.StandbyAware = v })
	form.AddCheckbox(settingsRows[3], cfg.ShowUnavailableTabs,
		func(v bool) { cfg.ShowUnavailableTabs = v })
	form.AddDropDown(settingsRows[4], views, indexOr(views, cfg.StartView, 0),
		func(opt string, _ int) { cfg.StartView = opt })

	form.AddButton("Save", func() {
		a.popModal()
		a.applySettings(cfg)
	})
	form.AddButton("Cancel", a.popModal)
	form.SetCancelFunc(a.popModal) // Esc

	// tview draws an unchecked box as a bare space — a one-cell background
	// tint that disappears under mono, where colour is all we would have.
	// Explicit glyphs survive it, like the ● that carries severity.
	for i := range form.GetFormItemCount() {
		if cb, ok := form.GetFormItem(i).(*tview.Checkbox); ok {
			cb.SetCheckedString("[x]").SetUncheckedString("[ ]")
		}
	}
	titledBox(form.Box, " Settings ")
	return styleForm(form)
}

// showSettings opens the editor. The form is rebuilt on every open rather than
// cached on App, so it is always born in the current theme — the whole
// repaint-miss class, dodged rather than handled.
func (a *App) showSettings() {
	form := a.settingsForm(a.currentConfig())
	a.pushModal(centeredModal(form, settingsWidth, settingsHeight))
	a.app.SetFocus(form)
}

// applySettings makes cfg live and persists it. It runs after popModal has
// restored a.root, so repaintAll's groundTree walks the real tree.
func (a *App) applySettings(cfg config.Config) {
	old := a.currentConfig()

	if cfg.Theme != old.Theme {
		a.themeName = cfg.Theme
		setTheme(themes[cfg.Theme])
		a.repaintAll() // also rebuilds the detail
	}
	if d := cfg.RefreshInterval.Duration(); d != a.interval {
		a.setInterval(d) // signals intervalCh; the ticker resets live
	}
	a.standbyAware.Store(cfg.StandbyAware)
	if cfg.ShowUnavailableTabs != old.ShowUnavailableTabs {
		a.detail.showAllTabs = cfg.ShowUnavailableTabs
		if cfg.Theme == old.Theme {
			a.rebuildDetail() // repaintAll already did it otherwise
		}
	}
	a.startView = cfg.StartView // consulted only by Run

	// A write failure does not discard the settings: honouring the intent and
	// reporting the disk problem separately beats losing both.
	if a.save != nil {
		if err := a.save(cfg); err != nil {
			a.showError("save settings", err)
			return
		}
	}
	a.refreshChrome()
}

// intervalChoices is the +/- ladder, with an off-ladder current value
// prepended so the form can always represent what is in effect.
func intervalChoices(current time.Duration) []time.Duration {
	if slices.Contains(intervalPresets, current) {
		return intervalPresets
	}
	return append([]time.Duration{current}, intervalPresets...)
}

// labelDurations renders durations for a dropdown.
func labelDurations(ds []time.Duration) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.String()
	}
	return out
}

// indexOr is slices.Index with a fallback, so an unknown value cannot leave a
// dropdown with nothing selected.
func indexOr[S ~[]E, E comparable](s S, v E, fallback int) int {
	if i := slices.Index(s, v); i >= 0 {
		return i
	}
	return fallback
}

// centeredBox centres a fixed-size primitive on the screen. pushModal calls
// SetRoot(_, false), which never assigns the root a rect, so a plain Flex
// would draw at 0x0 in the corner with its left edge clipped. tview.Modal
// solves this by sizing itself from the screen in Draw; this is the same trick
// for an arbitrary primitive.
//
// The nil Flex items are gaps: the before-draw hook fills the screen, so the
// margin around the box is already grounded.
type centeredBox struct {
	*tview.Flex
}

// Draw claims the whole screen, then lets the Flex centre its child in it.
func (c *centeredBox) Draw(screen tcell.Screen) {
	w, h := screen.Size()
	c.SetRect(0, 0, w, h)
	c.Flex.Draw(screen)
}

// centeredModal wraps p in a screen-filling, centring container. It shrinks to
// the screen when the terminal is smaller than the box, so a small terminal
// clips nothing.
func centeredModal(p tview.Primitive, width, height int) tview.Primitive {
	return &centeredBox{
		Flex: tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 1, true).
				AddItem(nil, 0, 1, false), width, 1, true).
			AddItem(nil, 0, 1, false),
	}
}

// rebuildDetail forces the detail to rebuild its tab views from the cached
// report. Shared with repaintAll so there is one rebuild idiom.
func (a *App) rebuildDetail() {
	a.detail.device = "" // invalidate the cache so update() takes the rebuild branch
	a.showSelected()
	if len(a.devices) == 0 {
		// showDevice returns before the placeholder when there is nothing to
		// show, and the message in place is the one the app last chose.
		a.detail.showPlaceholder(a.detail.placeholder)
	}
}
