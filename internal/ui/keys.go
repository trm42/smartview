// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
)

// intervalPresets is the ladder the +/- keys walk to change poll cadence.
var intervalPresets = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
}

// nextInterval returns the adjacent preset, clamped at the ends. It walks by
// value, not index, so an off-ladder --interval snaps to a neighbour.
func nextInterval(cur time.Duration, faster bool) time.Duration {
	if faster {
		next := intervalPresets[0]
		for _, p := range intervalPresets {
			if p < cur {
				next = p
			}
		}
		return next
	}
	next := intervalPresets[len(intervalPresets)-1]
	for i := len(intervalPresets) - 1; i >= 0; i-- {
		if intervalPresets[i] > cur {
			next = intervalPresets[i]
		}
	}
	return next
}

// onKey is the global key handler.
func (a *App) onKey(ev *tcell.EventKey) *tcell.EventKey {
	if a.inModal {
		return ev // let the modal handle all input
	}
	// Keys the fleet view doesn't claim fall through to the shared bindings.
	if a.fleetMode && a.onFleetKey(ev) {
		return nil
	}
	switch ev.Key() {
	case tcell.KeyEscape:
		a.app.Stop()
		return nil
	case tcell.KeyTab:
		a.toggleFocus()
		return nil
	case tcell.KeyUp, tcell.KeyDown:
		// Wide: the focused widget owns the arrows. Narrow has no list, so step the drive here; fleet's table owns them.
		if !a.narrow || a.fleetMode {
			return ev
		}
		delta := 1
		if ev.Key() == tcell.KeyUp {
			delta = -1
		}
		a.stepDrive(delta)
		return nil
	case tcell.KeyLeft:
		a.focusLeft()
		return nil
	case tcell.KeyRight:
		a.focusRight()
		return nil
	case tcell.KeyRune:
		switch r := ev.Rune(); r {
		case 'q':
			a.app.Stop()
			return nil
		case 'r':
			a.triggerRefresh()
			return nil
		case 'R':
			a.forceRefresh()
			return nil
		case '+', '-':
			a.setInterval(nextInterval(a.interval, r == '-'))
			return nil
		case 't':
			if a.detail.selectTabID("tests") {
				a.focusDetail()
			}
			return nil
		case 'c':
			a.toggleFleet()
			return nil
		case '?':
			a.showKeys()
			return nil
		case 'T':
			// Uppercase cycles the theme; lowercase t (above) is the Tests tab.
			a.cycleTheme()
			return nil
		case 'S':
			// Uppercase opens Settings; lowercase s (attributes.go) sorts.
			a.showSettings()
			return nil
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			a.openTab(int(r - '1'))
			return nil
		}
	}
	return ev
}

// onFleetKey handles the keys the fleet view claims; Up/Down, Enter and 's' belong to the table.
func (a *App) onFleetKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		a.exitFleet(false)
		return true
	case tcell.KeyTab:
		return true // swallow rather than orphan focus
	case tcell.KeyLeft:
		a.stepFleetSection(-1)
		return true
	case tcell.KeyRight:
		a.stepFleetSection(1)
		return true
	case tcell.KeyRune:
		switch r := ev.Rune(); {
		case r == 't':
			return true // no drive on screen for the Tests tab to address
		case r >= '1' && r <= '9':
			a.fleet.selectSection(int(r - '1'))
			a.refreshChrome()
			return true
		}
	}
	return false
}

// stepFleetSection moves the focus metric and resyncs the hint bar.
func (a *App) stepFleetSection(delta int) {
	if a.fleet.stepSection(delta) {
		a.refreshChrome()
	}
}

// toggleFleet switches between the per-drive view and the fleet comparison.
func (a *App) toggleFleet() {
	if a.fleetMode {
		a.exitFleet(false)
		return
	}
	a.fleetMode = true
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)
	a.bodyPages.SwitchToPage(pageFleet)
	a.app.SetFocus(a.fleet.table)
	a.refreshChrome()
}

// exitFleet returns to the per-drive view; toDetail focuses the detail
// pane (opening a drive) instead of the list (plain "back").
func (a *App) exitFleet(toDetail bool) {
	a.fleetMode = false
	a.bodyPages.SwitchToPage(pageDrives)
	// Narrow has no list on-tree to focus.
	if toDetail || a.narrow {
		a.app.SetFocus(a.detail.content())
	} else {
		a.app.SetFocus(a.list)
	}
	a.refreshChrome()
}

// openDrive selects a drive by device name and leaves the fleet view for its detail.
func (a *App) openDrive(name string) {
	cur := a.list.GetCurrentItem()
	for i, d := range a.devices {
		if d.Name == name {
			if i == cur {
				// SetCurrentItem fires no changed-func for the same index.
				a.showSelected()
			} else {
				a.list.SetCurrentItem(i)
			}
			break
		}
	}
	a.exitFleet(true)
}

// focusDetail moves focus to the detail body and resyncs the chrome.
func (a *App) focusDetail() {
	a.app.SetFocus(a.detail.content())
	a.refreshChrome()
}

// openTab activates a tab and focuses its body; shared by the 1-9 keys and tab clicks.
func (a *App) openTab(i int) {
	if !a.detail.selectTab(i) {
		return
	}
	a.focusDetail()
}

// toggleFocus swaps focus between list and detail; narrow has no list, so it only focuses the detail.
func (a *App) toggleFocus() {
	if a.narrow {
		a.focusDetail()
		return
	}
	if a.list.HasFocus() {
		a.app.SetFocus(a.detail.content())
	} else {
		a.app.SetFocus(a.list)
	}
	a.refreshChrome()
}

// focusRight advances along the chain list → tab0 → … → tabN (no wrap).
func (a *App) focusRight() {
	if a.list.HasFocus() {
		a.focusDetail()
		return
	}
	if a.detail.stepTab(1) {
		a.focusDetail()
	}
}

// focusLeft is the reverse of focusRight; narrow stops at the first tab.
func (a *App) focusLeft() {
	if a.list.HasFocus() {
		return
	}
	// stepTab knows whether any available tab remains to the left.
	if a.detail.stepTab(-1) {
		a.focusDetail()
		return
	}
	if a.narrow {
		return
	}
	a.app.SetFocus(a.list)
	a.refreshChrome()
}

// triggerRefresh asks the poll loop to fetch now, honouring the standby policy.
func (a *App) triggerRefresh() {
	select {
	case a.refreshCh <- struct{}{}:
	default:
	}
}

// forceRefresh asks the poll loop to fetch now, waking parked drives; the only override of standby_aware.
func (a *App) forceRefresh() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}

// setInterval changes the poll cadence live and signals the poll loop's ticker.
func (a *App) setInterval(d time.Duration) {
	a.interval = d
	// Drain first: the poll loop does not read while fetching, so a stale value
	// would otherwise win over this one.
	select {
	case <-a.intervalCh:
	default:
	}
	select {
	case a.intervalCh <- d:
	default:
	}
	a.refreshChrome()
}

// stepDrive moves the selection by delta, clamped; the narrow layout's stand-in
// for the list. The changed-func renders, since next always differs from the current index.
func (a *App) stepDrive(delta int) {
	next := a.list.GetCurrentItem() + delta
	if next < 0 || next >= a.list.GetItemCount() {
		return
	}
	a.list.SetCurrentItem(next)
}
