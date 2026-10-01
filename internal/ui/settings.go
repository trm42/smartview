// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"slices"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/config"
)

// settingsWidth is the modal's fixed outer width.
const settingsWidth = 56

// changedGlyph marks a row edited since the modal opened, in a two-column gutter left of the label.
const (
	changedGlyph = "•"
	rowGutter    = "  "
)

// settingsHelp is the focus-following help line per row.
var settingsHelp = []string{
	"Colour palette. t/T cycle it live.",
	"How often every drive is re-read.",
	"Leave parked drives asleep. ATA only.",
	"Draw all six tabs, muting empty ones.",
	"Which screen to open on. Next run.",
}

// settingsThemeApproxHelp replaces the theme help without truecolor, where the palette is approximated.
const settingsThemeApproxHelp = "Palette. Approximated — set COLORTERM=truecolor"

// settingsHelpLine is the help for row; truecolor is a parameter because the terminal's answer is process-global.
func settingsHelpLine(row int, truecolor bool) string {
	if row == 0 && !truecolor {
		return settingsThemeApproxHelp
	}
	return settingsHelp[row]
}

// settingsButtonHelp fills the help line while a button has focus.
const settingsButtonHelp = "Save writes the file. Cancel discards."

// settingsHeight is border, rows, blank, buttons and the two-line footer.
var settingsHeight = len(settingsRows) + 7

// settingsKeys is the modal's key hint line.
const settingsKeys = "↑↓ move   ⏎/→ change   ← back   Esc cancel"

// Checkbox state glyphs, escaped at the sink.
const (
	checkedGlyph   = "[x]"
	uncheckedGlyph = "[ ]"
)

// settingsRows names the form rows in order; its length sizes the modal.
var settingsRows = []string{
	"Theme", "Refresh", "Skip spun-down drives",
	"Show unavailable tabs", "Start view",
}

// currentConfig is derived from live state, never cached, so T and +/- cannot be reverted by Save.
func (a *App) currentConfig() config.Config {
	return config.Config{
		Theme:               a.themeName,
		RefreshInterval:     config.Duration(a.interval),
		StandbyAware:        a.standbyAware.Load(),
		ShowUnavailableTabs: a.detail.showAllTabs,
		StartView:           a.startView,
	}
}

// settingsForm builds the editor over a working copy of cfg, so Cancel discards for free.
func (a *App) settingsForm(cfg config.Config) *tview.Form {
	original := cfg
	a.settingsHelp = newInertTextView()
	a.settingsHelp.SetBorderPadding(0, 0, uiGutter+1, uiGutter)
	form := tview.NewForm()
	// AddDropDown fires its callback during construction; mark is reassigned below.
	mark := func() {}
	form.SetItemPadding(0)
	// NewForm defaults to padding 1, which pushes the buttons outside the form's clip.
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
	// A DropDown closing its list also routes Esc here while still open; decline then.
	form.SetCancelFunc(func() {
		if chooserOpen(form) {
			return
		}
		a.popModal()
	})

	// Explicit glyphs survive mono; escaped because Checkbox renders its state as markup and "[x]" is a tag.
	for i := range form.GetFormItemCount() {
		switch it := form.GetFormItem(i).(type) {
		case *tview.DropDown:
			it.SetTextOptions("", "", "‹ ", " ›", "")
		case *tview.Checkbox:
			it.SetCheckedString(tview.Escape(checkedGlyph)).
				SetUncheckedString(tview.Escape(uncheckedGlyph))
		}
	}
	a.installRowKeys(form)
	mark = func() { markChangedRows(form, original, cfg) }
	mark()
	return styleForm(form)
}

// markChangedRows puts a dot in each edited row's constant-width gutter.
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

// setItemLabel relabels a form item; SetLabel is not on the FormItem interface.
func setItemLabel(item tview.FormItem, label string) {
	switch v := item.(type) {
	case *tview.DropDown:
		v.SetLabel(label)
	case *tview.Checkbox:
		v.SetLabel(label)
	}
}

// boxed covers the Box hooks the FormItem interface omits.
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

// installRowKeys gives the form a list model: up/down move through settings
// and buttons, Enter or Right activates, Left closes an open chooser.
// Captures go per item: Form.Focus delegates to the child. They also stop a closed DropDown opening on Up/Down.
func (a *App) installRowKeys(form *tview.Form) {
	items, buttons := form.GetFormItemCount(), form.GetButtonCount()
	total := items + buttons
	focusAt := func(i int) {
		i = min(max(i, 0), total-1)
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
			// An open chooser owns the arrows: tview routes them to the DropDown, which forwards them to its list.
			if dd != nil && dd.IsOpen() {
				if ev.Key() == tcell.KeyLeft {
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
				focusAt(items - 1)
				return nil
			case tcell.KeyDown:
				return nil
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

// settingsModal builds the overlay: the form, then the help and key lines inside the same border.
func (a *App) settingsModal() (tview.Primitive, *tview.Form) {
	form := a.settingsForm(a.currentConfig())

	keys := newInertTextView()
	keys.SetBorderPadding(0, 0, uiGutter+1, uiGutter)
	keys.SetText(mutedTag() + settingsKeys + "[-]")

	box := modalBox(" Settings ", uiGutter)
	box.AddItem(form, len(settingsRows)+2, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(a.settingsHelp, 1, 0, false).
		AddItem(keys, 1, 0, false)
	return centeredModal(box, settingsWidth, settingsHeight), form
}

// showSettings rebuilds the modal on every open, so it is always in the current theme.
func (a *App) showSettings() {
	modal, form := a.settingsModal()
	a.pushModal(modal)
	a.app.SetFocus(form)
}

// applySettings makes cfg live and persists it; runs after popModal so rethemeTree walks the real tree.
func (a *App) applySettings(cfg config.Config) {
	old := a.currentConfig()

	// Every field lands before anything repaints: repaintAll rebuilds from showAllTabs.
	a.standbyAware.Store(cfg.StandbyAware)
	a.detail.showAllTabs = cfg.ShowUnavailableTabs
	a.startView = cfg.StartView

	switch {
	case cfg.Theme != old.Theme:
		a.themeName = cfg.Theme
		setTheme(themes[cfg.Theme])
		a.repaintAll()
	case cfg.ShowUnavailableTabs != old.ShowUnavailableTabs:
		a.rebuildDetail()
	}
	if d := cfg.RefreshInterval.Duration(); d != a.interval {
		a.setInterval(d)
	}

	// A write failure keeps the applied settings and reports separately.
	if a.save != nil {
		if err := a.save(cfg); err != nil {
			a.showError("save settings", err)
			return
		}
	}
	a.refreshChrome()
}

// intervalChoices is the +/- ladder with an off-ladder current value prepended.
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

// indexOr is slices.Index with a fallback.
func indexOr[S ~[]E, E comparable](s S, v E, fallback int) int {
	if i := slices.Index(s, v); i >= 0 {
		return i
	}
	return fallback
}

// centeredBox centres a fixed-size primitive, sizing itself from the screen in Draw.
type centeredBox struct {
	*tview.Flex
	column        *tview.Flex // the row holding p, resized to the screen
	inner         tview.Primitive
	width, height int
}

// Draw clamps to the screen: an oversized fixed item gives the gaps a negative share.
func (c *centeredBox) Draw(screen tcell.Screen) {
	w, h := screen.Size()
	c.SetRect(0, 0, w, h)
	c.ResizeItem(c.column, min(c.width, w), 1)
	c.column.ResizeItem(c.inner, min(c.height, h), 1)
	c.Flex.Draw(screen)
}

// centeredModal wraps p in a screen-filling, centring container.
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

// rebuildDetail forces the detail to rebuild its tab views from the cached report.
func (a *App) rebuildDetail() {
	// The rebuild destroys the focused page; re-home focus if it was on the detail.
	focused := a.detail.HasFocus()
	a.detail.device = "" // forces update's rebuild branch
	a.showSelected()
	if len(a.devices) == 0 {
		// showDevice returns early with no devices; re-show the last placeholder.
		a.detail.showPlaceholder(a.detail.placeholder)
	}
	if focused {
		a.app.SetFocus(a.detail.content())
	}
}
