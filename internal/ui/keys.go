// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
)

// Key dispatch and the navigation it drives: the global handler, the fleet
// view's overrides, focus movement and the poll-interval ladder.

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
		// Wide leaves the arrows to the focused widget (the drive list, or the
		// detail body it scrolls). Narrow has no list on screen for them to
		// reach, so the drive selection is stepped from here — the narrow hint
		// bar advertises "↑/↓ drive" and it has to be true. Line-scrolling the
		// detail there is j/k, paging is PgUp/PgDn. Fleet mode is exempt: its
		// table is focused and owns them (see onFleetKey).
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
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			a.openTab(int(r - '1'))
			return nil
		}
	}
	return ev
}

// onFleetKey handles the keys the fleet view claims, reporting whether it
// consumed the event. Up/Down, Enter and 's' are deliberately absent — they
// belong to the focused table itself.
func (a *App) onFleetKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		// Esc is "back" here, not "quit"; q still quits.
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
	// Render from the cache so entry doesn't wait for the next poll.
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
	// Narrow keeps focus on the detail either way: the list is not in that
	// layout, and focus on an off-tree primitive reaches nothing.
	if toDetail || a.narrow {
		a.app.SetFocus(a.detail.content())
	} else {
		a.app.SetFocus(a.list)
	}
	a.refreshChrome()
}

// openDrive selects a drive by device name and leaves the fleet view for its
// detail.
func (a *App) openDrive(name string) {
	for i, d := range a.devices {
		if d.Name == name {
			a.list.SetCurrentItem(i)
			break
		}
	}
	// SetCurrentItem does not fire the changed-func when the index is unchanged;
	// render unconditionally.
	a.showSelected()
	a.exitFleet(true)
}

// focusDetail moves focus to the detail body and resyncs the chrome, the pair
// almost every focus move wants. The sites that instead call refreshChrome
// after a branch (exitFleet, toggleFocus's wide arm, popModal, poll.go) may
// focus the list rather than the detail, so they cannot use this — and now
// look different because they are, not by accident.
func (a *App) focusDetail() {
	a.app.SetFocus(a.detail.content())
	a.refreshChrome()
}

// openTab activates a tab and moves focus to its body; the 1-9 keys and a tab
// click share it. From a mouse handler the event loop holds no draw lock, so
// SetFocus is safe here where QueueUpdateDraw would deadlock.
func (a *App) openTab(i int) {
	if !a.detail.selectTab(i) {
		return
	}
	a.focusDetail()
}

// toggleFocus moves focus between the drive list and the detail content. In the
// narrow layout there is nothing to toggle — the list is not in the widget tree,
// so focusing it would park focus off-tree and tview would forward no key at all.
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

// focusLeft is the reverse of focusRight, falling through to the drive list.
// The narrow layout has no list to fall through to, so it stops at the first tab.
func (a *App) focusLeft() {
	if a.list.HasFocus() {
		return
	}
	// Ask stepTab rather than test active == 0: with show_unavailable_tabs
	// there may be muted tabs to the left that it steps over, and only it
	// knows whether any reachable one remains.
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

// triggerRefresh asks the poll loop to fetch immediately (non-blocking). It
// honours the standby policy: 'r' must not spin a parked drive up.
func (a *App) triggerRefresh() {
	select {
	case a.refreshCh <- struct{}{}:
	default:
	}
}

// forceRefresh asks the poll loop to fetch immediately and wake any parked
// drive. This is the only path that overrides standby_aware — without it, a
// cold start with every drive asleep could never show a reading.
func (a *App) forceRefresh() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}

// setInterval changes the poll cadence at runtime: updates a.interval,
// signals the poll loop to reset its ticker, refreshes the status bar.
func (a *App) setInterval(d time.Duration) {
	a.interval = d
	select {
	case a.intervalCh <- d:
	default:
	}
	a.refreshChrome()
}

// stepDrive moves the drive selection by delta, clamped at both ends. It is the
// narrow layout's stand-in for arrowing the drive list, which is not on screen
// there; the wide layout never needs it because the list handles its own keys.
func (a *App) stepDrive(delta int) {
	cur := a.list.GetCurrentItem()
	next := cur + delta
	if next < 0 || next >= a.list.GetItemCount() {
		return
	}
	a.list.SetCurrentItem(next)
	// SetCurrentItem does not fire the changed-func when the index is unchanged;
	// render unconditionally.
	a.showSelected()
	a.refreshChrome()
}
