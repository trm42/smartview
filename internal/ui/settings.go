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
const settingsWidth = 56

// changedGlyph marks a row edited since the modal opened. It sits in a
// two-column gutter left of every label rather than at the right edge,
// because a tview Form row is label plus field with nothing after it.
const (
	changedGlyph = "•"
	rowGutter    = "  "
)

// settingsHelp is the one-line description shown under the form for whichever
// row has focus. It is the only place a caveat like "ATA drives only" reaches
// someone actually editing the setting — the config file and README do not.
var settingsHelp = []string{
	"Colour palette. T cycles it live.",
	"How often every drive is re-read.",
	"Leave parked drives asleep. ATA only.",
	"Draw all six tabs, muting empty ones.",
	"Which screen to open on. Next run.",
}

// settingsThemeApproxHelp replaces the theme row's help line on a terminal
// without 24-bit colour, where the palette the user is choosing is not the one
// they will see. The Settings modal is where someone picking a theme is
// looking, so it is where the caveat belongs.
const settingsThemeApproxHelp = "Palette. Approximated — set COLORTERM=truecolor"

// settingsHelpLine is the description for row, with that one substitution.
// truecolor is passed rather than read, because the terminal's answer is
// process-global and a test could not vary it otherwise.
func settingsHelpLine(row int, truecolor bool) string {
	if row == 0 && !truecolor {
		return settingsThemeApproxHelp
	}
	return settingsHelp[row]
}

// settingsButtonHelp fills the description line while a button has focus, so
// the row does not go blank when navigation leaves the settings.
const settingsButtonHelp = "Save writes the file. Cancel discards."

// settingsHeight is the border, one row per setting, a blank, the button row,
// and the two-line footer (help + keys). Derived from settingsRows so adding a
// setting cannot leave the box too short to show the buttons.
var settingsHeight = len(settingsRows) + 7

// settingsKeys is the modal's hint line, in the status bar's vocabulary. The
// modal was the one surface in the app that taught no keys.
const settingsKeys = "↑↓ move   ⏎/→ change   ← back   Esc cancel"

// Checkbox state glyphs. Written unescaped here and escaped at the sink, so
// what is intended is legible; see settingsForm for why escaping is required.
const (
	checkedGlyph   = "[x]"
	uncheckedGlyph = "[ ]"
)

// settingsRows names the form's rows, in order. Its length sizes the modal, so
// adding a setting cannot leave the box too short to show the buttons.
var settingsRows = []string{
	"Theme", "Refresh", "Skip spun-down drives",
	"Show unavailable tabs", "Start view",
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
	original := cfg
	a.settingsHelp = newInertTextView()
	a.settingsHelp.SetBorderPadding(0, 0, uiGutter+1, uiGutter)
	form := tview.NewForm()
	// AddDropDown fires its callback during construction, so mark starts as a
	// no-op and is reassigned once form exists for it to relabel.
	mark := func() {}
	form.SetItemPadding(0) // density, matching every other box in the UI
	// NewForm defaults to SetBorderPadding(1, 1, 1, 1). The wrapper supplies
	// the gutter, and a row of vertical padding pushes the button row outside
	// the form's clip — which is exactly how the buttons went missing.
	form.SetBorderPadding(0, 0, 0, 0)
	intervals := intervalChoices(cfg.RefreshInterval.Duration())
	views := []string{config.StartDrives, config.StartFleet}

	form.AddDropDown(rowGutter+settingsRows[0], themeCycle, indexOr(themeCycle, cfg.Theme, 0),
		func(opt string, _ int) { cfg.Theme = opt; mark() })
	form.AddDropDown(rowGutter+settingsRows[1], labelDurations(intervals),
		indexOr(intervals, cfg.RefreshInterval.Duration(), 0),
		func(_ string, i int) {
			if i >= 0 && i < len(intervals) {
				cfg.RefreshInterval = config.Duration(intervals[i])
			}
			mark()
		})
	form.AddCheckbox(rowGutter+settingsRows[2], cfg.StandbyAware,
		func(v bool) { cfg.StandbyAware = v; mark() })
	form.AddCheckbox(rowGutter+settingsRows[3], cfg.ShowUnavailableTabs,
		func(v bool) { cfg.ShowUnavailableTabs = v; mark() })
	form.AddDropDown(rowGutter+settingsRows[4], views, indexOr(views, cfg.StartView, 0),
		func(opt string, _ int) { cfg.StartView = opt; mark() })

	form.AddButton("Save", func() {
		a.popModal()
		a.applySettings(cfg)
	})
	form.AddButton("Cancel", a.popModal)
	// Esc arrives here from two places. A DropDown closing its list routes it
	// through the Form's finished handler first, while d.open is still set, so
	// declining then is what stops leaving a chooser from taking the whole
	// modal with it.
	form.SetCancelFunc(func() {
		if chooserOpen(form) {
			return
		}
		a.popModal()
	})

	// A chooser has to look pressable. currentPrefix/currentSuffix wrap only
	// the value on the row, not the entries in the open list.
	for i := range form.GetFormItemCount() {
		if dd, ok := form.GetFormItem(i).(*tview.DropDown); ok {
			dd.SetTextOptions("", "", "‹ ", " ›", "")
		}
	}
	a.installRowKeys(form)

	// tview draws an unchecked box as a bare space — a one-cell background
	// tint that disappears under mono, where colour is all we would have.
	// Explicit glyphs survive it, like the ● that carries severity.
	//
	// Both strings go through tview.Escape: a Checkbox renders its state
	// string as markup, so a bare "[x]" parses as a colour tag and is
	// swallowed, leaving a checked box showing nothing at all. "[ ]" happens
	// to survive unescaped because the space does not match the tag pattern —
	// which is exactly the asymmetry that hid the bug.
	for i := range form.GetFormItemCount() {
		if cb, ok := form.GetFormItem(i).(*tview.Checkbox); ok {
			cb.SetCheckedString(tview.Escape(checkedGlyph)).
				SetUncheckedString(tview.Escape(uncheckedGlyph))
		}
	}
	mark = func() { markChangedRows(form, original, cfg) }
	mark()
	return styleForm(form)
}

// markChangedRows puts a dot in each edited row's gutter. Labels keep a
// constant two-column prefix so the form's label column cannot jump as rows
// are marked.
func markChangedRows(form *tview.Form, original, cur config.Config) {
	changed := []bool{
		cur.Theme != original.Theme,
		cur.RefreshInterval != original.RefreshInterval,
		cur.StandbyAware != original.StandbyAware,
		cur.ShowUnavailableTabs != original.ShowUnavailableTabs,
		cur.StartView != original.StartView,
	}
	for i, dirty := range changed {
		gutter := rowGutter
		if dirty {
			gutter = changedGlyph + " "
		}
		setItemLabel(form.GetFormItem(i), gutter+settingsRows[i])
	}
}

// setItemLabel relabels a form item. SetLabel is not on the FormItem
// interface: each widget returns its own concrete type, so they cannot share
// one.
func setItemLabel(item tview.FormItem, label string) {
	switch v := item.(type) {
	case *tview.DropDown:
		v.SetLabel(label)
	case *tview.Checkbox:
		v.SetLabel(label)
	}
}

// boxed is the part of every form item the FormItem interface omits: both
// hooks live on the embedded *tview.Box and are promoted, so one interface
// covers DropDown and Checkbox alike.
type boxed interface {
	SetInputCapture(func(*tcell.EventKey) *tcell.EventKey) *tview.Box
	SetFocusFunc(func()) *tview.Box
}

// chooserOpen reports whether any chooser has its list open.
func chooserOpen(form *tview.Form) bool {
	for i := range form.GetFormItemCount() {
		if dd, ok := form.GetFormItem(i).(*tview.DropDown); ok && dd.IsOpen() {
			return true
		}
	}
	return false
}

// installRowKeys gives the form a list model: up/down move through the
// settings and on to the buttons, Enter or Right activates the focused row,
// and Left backs out of an open chooser.
//
// The captures go per item, not on the Form: Form.Focus delegates to the
// child, so the application focuses the item and a capture on the Form is
// never in the chain. They are also what stops tview opening a closed
// DropDown on Up/Down/Home/End/PgUp/PgDn, which would swallow row navigation
// on the very first keypress.
func (a *App) installRowKeys(form *tview.Form) {
	items, buttons := form.GetFormItemCount(), form.GetButtonCount()
	total := items + buttons
	// One index space over the settings and then the buttons, so the arrows
	// do not dead-end at the last setting.
	focusAt := func(i int) {
		i = min(max(i, 0), total-1) // clamp, no wrap — the rule stepTab follows
		form.SetFocus(i)
		a.app.SetFocus(form)
	}

	for i := range items {
		item, ok := form.GetFormItem(i).(boxed)
		if !ok {
			continue
		}
		row := i
		dd, _ := form.GetFormItem(i).(*tview.DropDown)
		item.SetFocusFunc(func() { a.settingsHelp.SetText(mutedTag() + settingsHelpLine(row, hasTruecolor()) + "[-]") })
		item.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			// An open chooser owns the arrows. tview delivers Up/Down to the
			// DropDown even while its list holds focus — the DropDown
			// forwards them on — so without this the capture steals them and
			// the highlight can never move.
			if dd != nil && dd.IsOpen() {
				if ev.Key() == tcell.KeyLeft {
					// Escape is how the list closes; SetCancelFunc keeps it
					// from cancelling the modal on the way out.
					return tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
				}
				return ev
			}
			switch ev.Key() {
			case tcell.KeyUp:
				focusAt(row - 1)
				return nil
			case tcell.KeyDown:
				focusAt(row + 1)
				return nil
			case tcell.KeyRight:
				// The widgets already do the right thing on Enter: a checkbox
				// toggles, a closed chooser opens. One verb, two controls.
				return tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
			}
			return ev
		})
	}

	for i := range buttons {
		btn, ok := tview.Primitive(form.GetButton(i)).(boxed)
		if !ok {
			continue
		}
		at := items + i
		btn.SetFocusFunc(func() { a.settingsHelp.SetText(mutedTag() + settingsButtonHelp + "[-]") })
		btn.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			switch ev.Key() {
			case tcell.KeyUp:
				focusAt(items - 1) // back to the last setting
				return nil
			case tcell.KeyDown:
				return nil // nothing below; clamp rather than wrap
			case tcell.KeyLeft:
				focusAt(at - 1)
				return nil
			case tcell.KeyRight:
				focusAt(at + 1)
				return nil
			}
			return ev
		})
	}
}

// settingsModal builds the whole overlay: the form, then the focus-following
// help line and the key hints under it. The border lives on the wrapper rather
// than the form so the footer sits inside the box.
func (a *App) settingsModal() (tview.Primitive, *tview.Form) {
	form := a.settingsForm(a.currentConfig())

	keys := newInertTextView()
	keys.SetBorderPadding(0, 0, uiGutter+1, uiGutter)
	keys.SetText(mutedTag() + settingsKeys + "[-]")

	box := newOpaqueFlex()
	box.SetDirection(tview.FlexRow).
		AddItem(form, len(settingsRows)+2, 0, true). // rows + blank + buttons
		AddItem(nil, 1, 0, false).                   // breathing room above the footer
		AddItem(a.settingsHelp, 1, 0, false).
		AddItem(keys, 1, 0, false)
	box.SetBackgroundColor(activeTheme.Background)
	titledBox(box.Box, " Settings ")
	box.SetBorderColor(activeTheme.Accent)
	box.SetTitleColor(activeTheme.Neutral)
	return centeredModal(box, settingsWidth, settingsHeight), form
}

// showSettings opens the editor. It is rebuilt on every open rather than
// cached on App, so it is always born in the current theme — the whole
// repaint-miss class, dodged rather than handled.
func (a *App) showSettings() {
	modal, form := a.settingsModal()
	a.pushModal(modal)
	a.app.SetFocus(form)
}

// applySettings makes cfg live and persists it. It runs after popModal has
// restored a.root, so repaintAll's groundTree walks the real tree.
func (a *App) applySettings(cfg config.Config) {
	old := a.currentConfig()

	// Every field lands before anything repaints: repaintAll rebuilds the
	// detail from showAllTabs, so setting the flag afterwards would leave the
	// old tab set on screen until the next poll.
	a.standbyAware.Store(cfg.StandbyAware)
	a.detail.showAllTabs = cfg.ShowUnavailableTabs
	a.startView = cfg.StartView // consulted only by Run

	switch {
	case cfg.Theme != old.Theme:
		a.themeName = cfg.Theme
		setTheme(themes[cfg.Theme])
		a.repaintAll() // also rebuilds the detail, so the tab set follows
	case cfg.ShowUnavailableTabs != old.ShowUnavailableTabs:
		a.rebuildDetail()
	}
	if d := cfg.RefreshInterval.Duration(); d != a.interval {
		a.setInterval(d) // signals intervalCh; the ticker resets live
	}

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
	column        *tview.Flex // the row holding p, resized to the screen
	inner         tview.Primitive
	width, height int
}

// Draw claims the whole screen, then lets the Flex centre its child in it. The
// box is clamped to the screen first: a Flex asked for a fixed size larger than
// it has gives its gap items a negative share, which walks the box off the left
// edge and clips the labels rather than the margin.
func (c *centeredBox) Draw(screen tcell.Screen) {
	w, h := screen.Size()
	c.SetRect(0, 0, w, h)
	c.ResizeItem(c.column, min(c.width, w), 1)
	c.column.ResizeItem(c.inner, min(c.height, h), 1)
	c.Flex.Draw(screen)
}

// centeredModal wraps p in a screen-filling, centring container. It shrinks to
// the screen when the terminal is smaller than the box, so a small terminal
// clips the margin rather than the content.
func centeredModal(p tview.Primitive, width, height int) tview.Primitive {
	column := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(p, height, 1, true).
		AddItem(nil, 0, 1, false)
	return &centeredBox{
		Flex: tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(column, width, 1, true).
			AddItem(nil, 0, 1, false),
		column: column,
		inner:  p,
		width:  width,
		height: height,
	}
}

// rebuildDetail forces the detail to rebuild its tab views from the cached
// report. Shared with repaintAll so there is one rebuild idiom.
func (a *App) rebuildDetail() {
	// The rebuild destroys the page primitive focus points at, and nothing
	// else re-homes it: the poll loop only restores focus it still sees on the
	// detail. Without this every focused-content key lands on an off-tree
	// widget until the user presses Tab.
	focused := a.detail.HasFocus()
	a.detail.device = "" // invalidate the cache so update() takes the rebuild branch
	a.showSelected()
	if len(a.devices) == 0 {
		// showDevice returns before the placeholder when there is nothing to
		// show, and the message in place is the one the app last chose.
		a.detail.showPlaceholder(a.detail.placeholder)
	}
	if focused {
		a.app.SetFocus(a.detail.content())
	}
}
