// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The modal overlay stack and its three consumers (confirm, key list, error).

// pushModal shows a modal overlay, suspending the normal key-handler logic.
func (a *App) pushModal(m tview.Primitive) {
	a.inModal = true
	a.app.SetRoot(m, false)
}

// popModal removes the modal overlay and restores the main layout, returning
// focus to the detail content so the Tests tab stays interactive.
func (a *App) popModal() {
	a.inModal = false
	a.app.SetRoot(a.root, true)
	if a.fleetMode {
		a.app.SetFocus(a.fleet.table)
	} else {
		a.app.SetFocus(a.detail.content())
	}
	a.refreshChrome()
}

// styleModal grounds a modal in the active theme. tview builds one from its own
// contrast palette, and Modal.SetBackgroundColor reaches the inner form and
// frame but not the surrounding box, so both are set here.
func styleModal(m *tview.Modal) *tview.Modal {
	m.SetBackgroundColor(activeTheme.Background)
	m.Box.SetBackgroundColor(activeTheme.Background)
	m.SetBorderColor(activeTheme.Accent)
	m.SetTextColor(activeTheme.Neutral)
	m.SetButtonBackgroundColor(activeTheme.SelectionBg)
	m.SetButtonTextColor(activeTheme.SelectionFg)
	// Accent, not OK: the affirmative may be destructive, and OK stays
	// reserved for healthy/go semantics. Bold carries the focus under mono,
	// where every colour collapses to the terminal default.
	m.SetButtonActivatedStyle(tcell.StyleDefault.
		Background(activeTheme.Accent).
		Foreground(activeTheme.Inverse).
		Attributes(tcell.AttrBold))
	return m
}

// confirm shows a two-button confirmation modal; onYes runs when the user picks
// the affirmative label.
func (a *App) confirm(text, yesLabel string, onYes func()) {
	modal := styleModal(tview.NewModal()).
		SetText(text).
		AddButtons([]string{yesLabel, "Back"}).
		SetDoneFunc(func(_ int, label string) {
			a.popModal()
			if label == yesLabel {
				onYes()
			}
		})
	a.pushModal(modal)
}

// keysText is the body of the '?' modal. Every line stays under 26 columns:
// tview.Modal wraps at a third of the screen width, so a longer line splits at
// the 80-column floor. keys_test.go fails if a bound rune is missing here.
const keysText = "Keys\n\n" +

	"↑/↓        select / scroll\n" +
	"←/→        prev / next tab\n" +
	"1-9        jump to tab\n" +
	"Click      switch tab\n" +
	"Tab        move focus\n" +
	"j / k      scroll content\n" +
	"PgUp/PgDn ^B/^F  page\n" +
	"g/G Home/End  top/bottom\n" +
	"s / f      sort / filter\n" +
	"t          Tests tab\n" +
	"Enter      start test\n" +
	"x          cancel test\n" +
	"c          fleet compare\n" +
	"Enter      open drive\n" +
	"Esc        back\n" +
	"r          refresh now\n" +
	"+ / -      refresh rate\n" +
	"T          colour theme\n" +
	"?          this list\n" +
	"q          quit"

// notice shows a modal with one dismissing button.
func (a *App) notice(text, button string) {
	a.pushModal(styleModal(tview.NewModal()).
		SetText(text).
		AddButtons([]string{button}).
		SetDoneFunc(func(int, string) { a.popModal() }))
}

// showKeys lists every binding in a dismissable modal; the narrow hint bar
// points here, so nothing is merely hidden.
func (a *App) showKeys() { a.notice(keysText, "Close") }

// showError displays a smartctl failure in a dismissable modal; action names
// the operation that failed.
func (a *App) showError(action string, err error) {
	a.notice(fmt.Sprintf("Could not %s:\n%s", action, err), "OK")
}
