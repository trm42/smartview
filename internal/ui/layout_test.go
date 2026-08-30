// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/config"
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
	a, screen := newSimAppCfg(t, width, height, config.Default())
	return a, screen
}

// newSimAppCfg is newSimApp with explicit settings. The saver only records:
// no test may write a config file.
func newSimAppCfg(t *testing.T, width, height int, cfg config.Config) (*App, tcell.SimulationScreen) {
	t.Helper()
	a := New(cfg, func(config.Config) error { return nil })
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

// TestThemeCycleRegroundsPersistentWidgets guards the same class of miss
// CLAUDE.md names for the banner: tview bakes the ground into a widget at
// construction, and repaintAll rebuilds only the detail's tab views, so every
// widget that outlives a theme change has to be re-grounded.
//
// repaintAll walks the tree for this; the list below stays hand-written on
// purpose. If both sides derived from the walk the test would assert nothing,
// so production walks and the test enumerates — that is what catches a widget
// the walk cannot reach and nobody grounded (the rail, in the narrow layout).
func TestThemeCycleRegroundsPersistentWidgets(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	for range themeCycle {
		a.cycleTheme()
		if activeTheme.Background != dark.Background {
			break
		}
	}
	if activeTheme.Background == dark.Background {
		t.Fatal("no theme in the cycle grounds differently from dark; nothing is under test")
	}

	type grounded interface{ GetBackgroundColor() tcell.Color }
	want := activeTheme.Background
	for _, c := range []struct {
		name string
		w    grounded
	}{
		{"list", a.list},
		{"status", a.status},
		{"banner", a.banner},
		{"rail", a.rail},
		{"body", a.body},
		{"bodyPages", a.bodyPages},
		{"root", a.root},
		{"detail", a.detail},
		{"detail.barRow", a.detail.barRow},
		{"detail.bar", a.detail.bar},
		{"detail.spinner", a.detail.spinner},
		{"detail.pages", a.detail.pages},
		{"fleet", a.fleet},
		{"fleet.bar", a.fleet.bar},
		{"fleet.table", a.fleet.table},
		{"fleet.legend", a.fleet.legend},
	} {
		if got := c.w.GetBackgroundColor(); got != want {
			t.Errorf("%s ground = %v after a theme cycle, want %v", c.name, got, want)
		}
	}
}

// The root-warning banner is mounted only when euid != 0, so under sudo it is
// not in the widget tree at all and groundTree cannot reach it. Tests run
// non-root, where it *is* mounted — which is exactly why a walk-only repaint
// looked correct here while leaving the banner stale for anyone running under
// sudo. Unmount it to stand in for that layout and pin that repaintAll grounds
// it anyway. The same holds for whichever of list/rail the layout left out.
func TestThemeCycleRegroundsWidgetsTheWalkCannotReach(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)

	// Stand in for the euid == 0 layout: build() would never have added it.
	a.root.RemoveItem(a.banner)

	offTree := []struct {
		name string
		w    interface {
			GetBackgroundColor() tcell.Color
			SetBackgroundColor(tcell.Color) *tview.Box
		}
	}{
		{"banner", a.banner},
		{"rail", a.rail}, // never mounted in the wide layout
	}
	// A ground nothing in the theme uses, so "still stale" is unmistakable.
	for _, c := range offTree {
		c.w.SetBackgroundColor(tcell.ColorFuchsia)
	}

	for range themeCycle {
		a.cycleTheme()
		if activeTheme.Background != dark.Background {
			break
		}
	}
	if activeTheme.Background == dark.Background {
		t.Fatal("no theme in the cycle grounds differently from dark; nothing is under test")
	}

	for _, c := range offTree {
		if got := c.w.GetBackgroundColor(); got != activeTheme.Background {
			t.Errorf("%s ground = %v after a theme cycle, want %v — it is not in the "+
				"widget tree, so repaintAll has to ground it explicitly",
				c.name, got, activeTheme.Background)
		}
	}
}

// TestThemeCycleKeepsThePlaceholderMessage: with no drives the detail holds a
// placeholder, and repaintAll has to rebuild it in the new theme without
// changing what it says — "No drives found" is the actionable one and nothing
// ever sets it a second time.
func TestThemeCycleKeepsThePlaceholderMessage(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const msg = "No drives found. Try running with sudo."
	a.detail.showPlaceholder(msg)

	a.cycleTheme()

	name, page := a.detail.pages.GetFrontPage()
	if name != "placeholder" {
		t.Fatalf("front page after a theme cycle = %q, want %q", name, "placeholder")
	}
	tv, ok := page.(*tview.TextView)
	if !ok {
		t.Fatalf("placeholder page is %T, want *tview.TextView", page)
	}
	if got := tv.GetText(true); !strings.Contains(got, msg) {
		t.Errorf("placeholder after a theme cycle = %q, want it to still say %q", got, msg)
	}
}
