// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strconv"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/smart"
)

// testTimeout bounds every wait in this file. The narrow-layout bugs these tests
// guard against hang the event loop rather than failing it, so nothing here may
// wait forever: a regression has to fail loudly instead of stalling CI.
const testTimeout = 5 * time.Second

// newSimApp builds an App backed by a tcell simulation screen of the given size.
// It deliberately skips Run: Run's initial smart.Scan shells out to smartctl,
// and the layout branch under test depends only on the terminal width.
func newSimApp(t *testing.T, width, height int) (*App, tcell.SimulationScreen) {
	t.Helper()
	a := New(30*time.Second, "dark")
	// New installs the theme globally; put it back so test order cannot matter.
	t.Cleanup(func() { setTheme(themes["dark"]) })
	screen := tcell.NewSimulationScreen("UTF-8")
	a.app.SetScreen(screen) // SetScreen initialises the screen itself
	screen.SetSize(width, height)
	return a, screen
}

// runSim starts the event loop and waits for one complete draw, which is what
// tells us the before-draw hook returned. It registers a cleanup that quits the
// application and waits for Run to return.
func runSim(t *testing.T, a *App, screen tcell.SimulationScreen) {
	t.Helper()
	drawn := make(chan struct{}, 1)
	a.app.SetAfterDrawFunc(func(tcell.Screen) {
		select {
		case drawn <- struct{}{}:
		default:
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.app.Run() }()

	t.Cleanup(func() {
		// 'q' goes through the real input capture, the same path a user takes.
		screen.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)
		select {
		case <-done:
		case <-time.After(testTimeout):
			t.Error("application did not stop within the timeout")
		}
	})

	select {
	case <-drawn:
	case <-time.After(testTimeout):
		t.Fatalf("no draw completed at %dx%d within %s: the before-draw hook "+
			"very likely deadlocked the event loop (it must not call a locking "+
			"tview.Application method)", widthOf(screen), heightOf(screen), testTimeout)
	}
}

func widthOf(s tcell.SimulationScreen) int  { w, _ := s.Size(); return w }
func heightOf(s tcell.SimulationScreen) int { _, h := s.Size(); return h }

// onLoop runs fn on the event-loop goroutine and returns its result, so widget
// state is read where every other read of it happens.
func onLoop[T any](t *testing.T, a *App, fn func() T) T {
	t.Helper()
	out := make(chan T, 1)
	// QueueUpdateDraw blocks until the event loop runs the closure, so it has to
	// be queued from a goroutine of its own.
	go a.app.QueueUpdateDraw(func() { out <- fn() })
	select {
	case v := <-out:
		return v
	case <-time.After(testTimeout):
		t.Fatal("event loop did not run a queued update within the timeout")
		var zero T
		return zero
	}
}

// TestNarrowStartupDrawsAndQuits is the regression test for the startup
// deadlock: below narrowBreakpoint the before-draw hook switched layouts and
// called Application.SetFocus, which takes the mutex draw() already holds, so
// the first draw never returned and only SIGKILL ended the process.
func TestNarrowStartupDrawsAndQuits(t *testing.T) {
	a, screen := newSimApp(t, 80, 30)
	runSim(t, a, screen)

	if !onLoop(t, a, func() bool { return a.narrow }) {
		t.Errorf("80 columns: narrow = false, want true (breakpoint is %d)", narrowBreakpoint)
	}
	// Focus must have been moved off the drive list, which the narrow layout
	// leaves out of the widget tree; parked there, tview forwards no key at all.
	if onLoop(t, a, func() bool { return a.list.HasFocus() }) {
		t.Error("focus rests on the drive list, which is absent from the narrow widget tree")
	}
	if !onLoop(t, a, func() bool { return a.root.HasFocus() }) {
		t.Error("focus is outside the widget tree: root.HasFocus() is false")
	}
}

// TestLayoutSelectedByWidth pins the breakpoint itself, at the two widths either
// side of it, and doubles as a hang guard at each one.
func TestLayoutSelectedByWidth(t *testing.T) {
	cases := []struct {
		width int
		want  bool
	}{
		{narrowBreakpoint - 1, true},
		{narrowBreakpoint, false},
		{120, false},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.width), func(t *testing.T) {
			a, screen := newSimApp(t, c.width, 30)
			runSim(t, a, screen)
			if got := onLoop(t, a, func() bool { return a.narrow }); got != c.want {
				t.Errorf("%d columns: narrow = %v, want %v", c.width, got, c.want)
			}
		})
	}
}

// TestNarrowLayoutOmitsDriveList documents what the narrow tree contains: the
// one-row rail and the detail, and not the drive list.
func TestNarrowLayoutOmitsDriveList(t *testing.T) {
	a, _ := newSimApp(t, 80, 30)
	a.app.SetFocus(a.detail.content()) // as the layout switch does, off the list
	a.setNarrow(true)

	if got := a.body.GetItemCount(); got != 2 {
		t.Fatalf("narrow body items = %d, want 2 (rail + detail)", got)
	}
	if a.body.GetItem(0) != a.rail {
		t.Error("narrow body does not start with the rail")
	}
	for i := 0; i < a.body.GetItemCount(); i++ {
		if a.body.GetItem(i) == a.list {
			t.Fatal("the drive list is in the narrow widget tree")
		}
	}

	a.setNarrow(false)
	if got := a.body.GetItemCount(); got != 2 {
		t.Fatalf("wide body items = %d, want 2 (list + detail)", got)
	}
	if a.body.GetItem(0) != a.list {
		t.Error("wide body does not start with the drive list")
	}
}

// TestNarrowFocusStaysInTree covers the three call sites that used to focus the
// off-tree drive list below the breakpoint. Each must leave focus somewhere the
// root can still reach, or the application stops responding to keys entirely.
func TestNarrowFocusStaysInTree(t *testing.T) {
	moves := []struct {
		name string
		fn   func(a *App)
	}{
		{"toggleFocus", func(a *App) { a.toggleFocus() }},
		{"focusLeft", func(a *App) { a.focusLeft() }},
		{"exitFleet", func(a *App) { a.exitFleet(false) }},
	}
	for _, m := range moves {
		t.Run(m.name, func(t *testing.T) {
			a, _ := newSimApp(t, 80, 30)
			a.app.SetFocus(a.detail.content())
			a.setNarrow(true)
			m.fn(a)
			if a.list.HasFocus() {
				t.Errorf("%s focused the drive list, which the narrow layout omits", m.name)
			}
			if !a.root.HasFocus() {
				t.Errorf("%s left focus outside the widget tree", m.name)
			}
		})
	}
}

// TestNarrowArrowsStepDrives holds the narrow hint bar ("↑/↓ drive") to its
// word: with no drive list on screen the arrows have to be handled by onKey.
// Wide leaves them alone so the focused widget keeps them.
func TestNarrowArrowsStepDrives(t *testing.T) {
	a, _ := newSimApp(t, 80, 30)
	a.devices = []smart.Device{{Name: "/dev/sda"}, {Name: "/dev/sdb"}, {Name: "/dev/sdc"}}
	a.populateList()
	a.app.SetFocus(a.detail.content())
	a.setNarrow(true)

	down := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	up := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)

	if ev := a.onKey(down); ev != nil {
		t.Error("narrow: Down was passed through instead of stepping the drive list")
	}
	if got := a.list.GetCurrentItem(); got != 1 {
		t.Errorf("narrow: after Down current item = %d, want 1", got)
	}
	if ev := a.onKey(up); ev != nil {
		t.Error("narrow: Up was passed through instead of stepping the drive list")
	}
	if got := a.list.GetCurrentItem(); got != 0 {
		t.Errorf("narrow: after Up current item = %d, want 0", got)
	}
	// Clamped at the ends rather than wrapping.
	a.onKey(up)
	if got := a.list.GetCurrentItem(); got != 0 {
		t.Errorf("narrow: Up at the first drive moved to %d, want 0", got)
	}

	// Wide: the list (or the focused detail body) owns the arrows again.
	a.setNarrow(false)
	if ev := a.onKey(down); ev != down {
		t.Error("wide: Down must fall through to the focused widget")
	}
}

// TestRailRepaintsAfterUpdate guards the rail's staleness bug: it was rendered
// once by applyLayout and never again, so severity glyphs, the alert count and
// the theme colours froze at the moment the narrow layout was installed.
func TestRailRepaintsAfterUpdate(t *testing.T) {
	a, _ := newSimApp(t, 80, 30)
	a.devices = []smart.Device{{Name: "/dev/sda"}}
	a.app.SetFocus(a.detail.content())
	a.setNarrow(true)
	a.populateList() // no reports yet: the muted "scanning" state
	before := a.rail.GetText(true)

	a.reports["/dev/sda"] = &smart.Report{
		Device:      smart.Device{Name: "/dev/sda", Protocol: "ATA"},
		SmartStatus: smart.SmartStatus{Passed: true},
		ModelName:   "TEST DRIVE",
	}
	a.populateList()
	if got := a.rail.GetText(true); got == before {
		t.Errorf("rail text unchanged after a report arrived: %q", got)
	}

	// A theme cycle must reach it too: renderRail bakes in the active colours.
	themed := a.rail.GetText(false)
	a.cycleTheme()
	if got := a.rail.GetText(false); got == themed {
		t.Errorf("rail markup unchanged after a theme cycle: %q", got)
	}
}
