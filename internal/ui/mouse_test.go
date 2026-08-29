// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// allTabsJSON is a sparse ATA report shaped to show every detail tab, so the
// strip is wide enough to overflow a narrow terminal.
const allTabsJSON = `{
  "device": {"name": "/dev/sda", "protocol": "ATA"},
  "model_name": "TEST DRIVE",
  "smart_status": {"passed": true},
  "ata_smart_attributes": {"table": [
    {"id": 5, "name": "Reallocated_Sector_Ct", "value": 100, "worst": 100,
     "thresh": 10, "flags": {"prefailure": true}, "raw": {"value": 0, "string": "0"}}
  ]},
  "ata_device_statistics": {"pages": [
    {"number": 1, "name": "General Statistics", "table": [
      {"offset": 8, "name": "Power-on Hours", "size": 4, "value": 100,
       "flags": {"valid": true}}
    ]}
  ]},
  "ata_smart_data": {"capabilities": {"self_tests_supported": true}},
  "ata_smart_error_log": {"extended": {"count": 0}}
}`

// allTabsReport parses allTabsJSON and attaches a FARM log, giving six tabs.
func allTabsReport(t *testing.T) *smart.Report {
	t.Helper()
	var r smart.Report
	if err := json.Unmarshal([]byte(allTabsJSON), &r); err != nil {
		t.Fatalf("parse the test report: %v", err)
	}
	r.FARM = &smart.FARM{Supported: true}
	return &r
}

// installReport applies a report to the detail pane and returns the tab count.
func installReport(t *testing.T, a *App, r *smart.Report) int {
	t.Helper()
	n := onLoop(t, a, func() int {
		a.detail.update(r, nil)
		return len(a.detail.tabs)
	})
	if n < 3 {
		t.Fatalf("test report yields %d tabs, want at least 3", n)
	}
	return n
}

// tabBarCell returns the screen cell at the middle of tab i's pill, derived
// from the bar's own spans so a retitled or compacted tab moves the click with
// it.
func tabBarCell(t *testing.T, a *App, i int) (int, int) {
	t.Helper()
	type cell struct{ x, y, spans int }
	c := onLoop(t, a, func() cell {
		x, y, _, _ := a.detail.bar.GetInnerRect()
		if i >= len(a.detail.bar.spans) {
			return cell{spans: len(a.detail.bar.spans)}
		}
		s := a.detail.bar.spans[i]
		return cell{x: x + (s.start+s.end)/2, y: y, spans: len(a.detail.bar.spans)}
	})
	if i >= c.spans {
		t.Fatalf("tab %d has no span: the bar recorded %d", i, c.spans)
	}
	return c.x, c.y
}

// clickAt sends a complete left click: tview synthesises MouseLeftClick from a
// press and a release in the same cell, and a lone press is the focus-stealing
// half.
func clickAt(screen tcell.SimulationScreen, x, y int) {
	screen.InjectMouse(x, y, tcell.ButtonPrimary, tcell.ModNone)
	screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
}

// waitFor polls cond on the event loop until it holds. Waiting on a single
// drawn frame would not do: the press is consumed and drawn before the click
// that follows it is processed.
func waitFor(t *testing.T, a *App, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if onLoop(t, a, cond) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// spanEndsMatchTheText cross-checks the recorded spans against the width of the
// text actually emitted, so a bug in the renderer cannot make a click test
// agree with itself.
func spanEndsMatchTheText(t *testing.T, a *App) {
	t.Helper()
	type shape struct {
		last, width int
	}
	s := onLoop(t, a, func() shape {
		spans := a.detail.bar.spans
		if len(spans) == 0 {
			return shape{}
		}
		return shape{spans[len(spans)-1].end, tview.TaggedStringWidth(a.detail.bar.GetText(false))}
	})
	if s.last != s.width {
		t.Errorf("last span ends at %d but the strip is %d columns wide", s.last, s.width)
	}
}

// TestTabBarClickSwitchesTabsWithoutStealingFocus is the core of mouse tab
// switching: the click must land on the tab under the pointer and end in the
// same state as the 1-9 keys, with focus on the tab body and the chrome resynced.
func TestTabBarClickSwitchesTabsWithoutStealingFocus(t *testing.T) {
	a, screen := newSimApp(t, 120, 40)
	runSim(t, a, screen)
	n := installReport(t, a, allTabsReport(t))
	spanEndsMatchTheText(t, a)

	last := n - 1
	wantID := onLoop(t, a, func() string { return a.detail.tabs[last].id })
	x, y := tabBarCell(t, a, last)
	clickAt(screen, x, y)
	waitFor(t, a, "the clicked tab to activate", func() bool {
		return a.detail.activeID() == wantID
	})

	if onLoop(t, a, func() bool { return a.detail.bar.HasFocus() }) {
		t.Error("the tab bar took focus; it handles no key, so every binding would be stranded")
	}
	if !onLoop(t, a, func() bool { return a.detail.content().HasFocus() }) {
		t.Error("focus did not move to the tab body")
	}
	if !onLoop(t, a, func() bool { return a.status.GetText(true) == stripTags(a.statusText()) }) {
		t.Error("the hint bar is stale: refreshChrome did not run after the click")
	}
}

// TestTabBarDoubleClickSwitchesTabs covers the second click of a rapid pair,
// which tview delivers as MouseLeftDoubleClick rather than MouseLeftClick.
func TestTabBarDoubleClickSwitchesTabs(t *testing.T) {
	a, screen := newSimApp(t, 120, 40)
	runSim(t, a, screen)
	n := installReport(t, a, allTabsReport(t))

	last := n - 1
	lastID := onLoop(t, a, func() string { return a.detail.tabs[last].id })
	x, y := tabBarCell(t, a, last)
	clickAt(screen, x, y)
	waitFor(t, a, "the first click to land", func() bool { return a.detail.activeID() == lastID })

	// No sleep: inside DoubleClickInterval the second press is a double click.
	firstID := onLoop(t, a, func() string { return a.detail.tabs[0].id })
	x, y = tabBarCell(t, a, 0)
	clickAt(screen, x, y)
	waitFor(t, a, "the immediate second click to land", func() bool {
		return a.detail.activeID() == firstID
	})
}

// fence forces the mouse events injected before it through the event loop:
// tcell delivers screen events in order, so a key whose effect can be waited on
// lands after them. A declined mouse event changes nothing to wait on, which is
// what makes a negative assertion race without this. The interval keys are the
// fence because they touch neither focus nor the active tab.
func fence(t *testing.T, a *App, screen tcell.SimulationScreen) {
	t.Helper()
	before := onLoop(t, a, func() time.Duration { return a.interval })
	key, want := '-', nextInterval(before, true)
	if want == before {
		key, want = '+', nextInterval(before, false)
	}
	screen.InjectKey(tcell.KeyRune, key, tcell.ModNone)
	waitFor(t, a, "the fence key to be processed", func() bool { return a.interval == want })
}

// TestTabBarIgnoresMissesAndTheWheel pins what the bar must not do on screen:
// snap a click past the last pill onto a tab, or take focus from either that or
// the wheel. Both assertions are fenced — read without one they pass whether the
// handler is right or the event has simply not been processed yet.
func TestTabBarIgnoresMissesAndTheWheel(t *testing.T) {
	a, screen := newSimApp(t, 120, 40)
	runSim(t, a, screen)
	n := installReport(t, a, allTabsReport(t))

	type state struct {
		id, text string
		listFocus,
		barFocus bool
	}
	read := func() state {
		return onLoop(t, a, func() state {
			return state{a.detail.activeID(), a.detail.bar.GetText(false),
				a.list.HasFocus(), a.detail.bar.HasFocus()}
		})
	}
	before := read()

	// One column past the last pill, still inside the bar.
	x, y := tabBarCell(t, a, n-1)
	end := onLoop(t, a, func() int {
		ix, _, _, _ := a.detail.bar.GetInnerRect()
		return ix + a.detail.bar.spans[n-1].end
	})
	clickAt(screen, end, y)
	fence(t, a, screen)
	if got := read(); got != before {
		t.Errorf("a click past the last pill changed state: %+v, want %+v", got, before)
	}

	screen.InjectMouse(x, y, tcell.WheelDown, tcell.ModNone)
	screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	fence(t, a, screen)
	if got := read(); got != before {
		t.Errorf("the wheel over the tab bar changed state: %+v, want %+v", got, before)
	}
}

// TestTabBarMouseHandlerDeclinesUnownedEvents drives the handler directly, where
// what it returns is visible: a hit is consumed and reports its tab, and a miss,
// the wheel and the focus-stealing press are all declined outright.
func TestTabBarMouseHandlerDeclinesUnownedEvents(t *testing.T) {
	b := newTabBar()
	b.SetRect(0, 0, 60, 1)
	tabs := []tab{{id: "overview", title: "Overview"}, {id: "logs", title: "Logs"}}
	b.render(tabs, 0)
	clicked := -1
	b.onClick = func(i int) { clicked = i }

	ix, iy, _, _ := b.GetInnerRect()
	hit := ix + (b.spans[1].start+b.spans[1].end)/2
	miss := ix + b.spans[len(b.spans)-1].end
	focused := false
	setFocus := func(tview.Primitive) { focused = true }

	cases := []struct {
		name     string
		action   tview.MouseAction
		x        int
		consumed bool
		want     int
	}{
		{"click on a pill", tview.MouseLeftClick, hit, true, 1},
		{"double click on a pill", tview.MouseLeftDoubleClick, hit, true, 1},
		{"click past the last pill", tview.MouseLeftClick, miss, false, -1},
		{"press on a pill", tview.MouseLeftDown, hit, false, -1},
		{"wheel over a pill", tview.MouseScrollDown, hit, false, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clicked, focused = -1, false
			ev := tcell.NewEventMouse(c.x, iy, tcell.ButtonPrimary, tcell.ModNone)
			consumed, capture := b.MouseHandler()(c.action, ev, setFocus)
			if consumed != c.consumed {
				t.Errorf("consumed = %v, want %v", consumed, c.consumed)
			}
			if capture != nil {
				t.Error("returned a capture primitive, which would hijack every later mouse event")
			}
			if clicked != c.want {
				t.Errorf("reported tab %d, want %d", clicked, c.want)
			}
			if focused {
				t.Error("the bar took focus; it handles no key, so every binding would be stranded")
			}
		})
	}
}

// TestChromeDeclinesTheMouse covers the widgets that carry no keys of their own:
// tview's TextView focuses itself on a left press, and focus parked on one of
// these reaches no handler at all. The rail, banner and status bar sit outside
// a.detail, so the poll loop's re-focus never rescues them.
func TestChromeDeclinesTheMouse(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	widgets := map[string]tview.Primitive{
		"rail":                a.rail,
		"banner":              a.banner,
		"status bar":          a.status,
		"refresh spinner":     a.detail.spinner,
		"fleet section strip": a.fleet.bar,
		"fleet legend":        a.fleet.legend,
	}
	actions := []tview.MouseAction{tview.MouseLeftDown, tview.MouseLeftClick,
		tview.MouseLeftDoubleClick, tview.MouseScrollDown}
	for name, w := range widgets {
		t.Run(name, func(t *testing.T) {
			w.SetRect(0, 0, 40, 1)
			ev := tcell.NewEventMouse(1, 0, tcell.ButtonPrimary, tcell.ModNone)
			for _, action := range actions {
				focused := false
				consumed, capture := w.MouseHandler()(action, ev, func(tview.Primitive) { focused = true })
				if focused {
					t.Errorf("action %v focused it, stranding every key binding", action)
				}
				if consumed || capture != nil {
					t.Errorf("action %v: consumed = %v, capture = %v, want false and nil",
						action, consumed, capture)
				}
			}
		})
	}
}

// TestSpinnerClickKeepsFocus covers the two cells the spinner occupies at the
// right of the tab row: they sit inside a strip advertised as clickable and
// must not park focus on a widget that handles no key.
func TestSpinnerClickKeepsFocus(t *testing.T) {
	a, screen := newSimApp(t, 120, 40)
	runSim(t, a, screen)
	installReport(t, a, allTabsReport(t))

	cell := onLoop(t, a, func() [2]int {
		x, y, _, _ := a.detail.spinner.GetInnerRect()
		return [2]int{x, y}
	})

	before := onLoop(t, a, func() bool { return a.list.HasFocus() })
	clickAt(screen, cell[0], cell[1])
	if got := onLoop(t, a, func() bool { return a.list.HasFocus() }); got != before {
		t.Error("clicking the spinner moved focus")
	}
	if onLoop(t, a, func() bool { return a.detail.spinner.HasFocus() }) {
		t.Error("the spinner took focus; it handles no key")
	}
}

// TestTabBarEveryTabIsClickable holds the strip to CLAUDE.md's "nothing may
// truncate silently": with a span-based hit test, a pill drawn past the right
// edge is also unclickable, and the full six-tab strip needs 119 columns.
func TestTabBarEveryTabIsClickable(t *testing.T) {
	for _, width := range []int{120, 80} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			a, screen := newSimApp(t, width, 40)
			runSim(t, a, screen)
			n := installReport(t, a, allTabsReport(t))

			for i := range n {
				wantID := onLoop(t, a, func() string { return a.detail.tabs[i].id })
				fits := onLoop(t, a, func() bool {
					_, _, w, _ := a.detail.bar.GetInnerRect()
					spans := a.detail.bar.spans
					return spans[len(spans)-1].end <= w
				})
				if !fits {
					t.Fatalf("the tab strip runs past the bar at %d columns: "+
						"the pills beyond the edge are neither visible nor clickable", width)
				}
				x, y := tabBarCell(t, a, i)
				clickAt(screen, x, y)
				waitFor(t, a, "tab "+strconv.Itoa(i)+" to activate", func() bool {
					return a.detail.activeID() == wantID
				})
				if onLoop(t, a, func() bool { return a.detail.bar.HasFocus() }) {
					t.Fatalf("the tab bar took focus at %d columns", width)
				}
			}
		})
	}
}

// TestTabPills pins the compaction rule: inactive titles are what gives way, and
// every tab keeps a pill so the 1-9 affordance survives.
func TestTabPills(t *testing.T) {
	tabs := []tab{{id: "overview", title: "Overview"}, {id: "farm", title: "FARM"},
		{id: "logs", title: "Logs"}}
	cases := []struct {
		name   string
		active int
		width  int
		want   []string
	}{
		{"unconstrained", 0, 0, []string{" 1 Overview ", " 2 FARM ", " 3 Logs "}},
		{"roomy", 1, 200, []string{" 1 Overview ", " 2 FARM ", " 3 Logs "}},
		{"tight", 1, 20, []string{" 1 ", " 2 FARM ", " 3 "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tabPills(tabs, c.active, c.width)
			if !slices.Equal(got, c.want) {
				t.Errorf("tabPills(_, %d, %d) = %q, want %q", c.active, c.width, got, c.want)
			}
		})
	}
	if got := tabPills(nil, 0, 80); len(got) != 0 {
		t.Errorf("tabPills(nil, …) = %q, want empty", got)
	}
}

// TestDriveListClickShowsTheClickedDrive covers tview's List, which fires its
// changed-func before storing the new index: a handler that reads
// GetCurrentItem() there renders the previously selected drive, and the wrong
// drive stays on screen until the next poll.
func TestDriveListClickShowsTheClickedDrive(t *testing.T) {
	a, screen := newSimApp(t, 120, 40)
	runSim(t, a, screen)

	devices := []smart.Device{{Name: "/dev/sda", Protocol: "ATA"}, {Name: "/dev/sdb", Protocol: "ATA"}}
	reports := map[string]*smart.Report{}
	for _, d := range devices {
		r := allTabsReport(t)
		r.Device.Name = d.Name
		reports[d.Name] = r
	}
	onLoop(t, a, func() bool {
		a.devices = devices
		a.reports = reports
		a.populateList()
		return true
	})

	// Rows are two cells tall: the list shows secondary text.
	cell := onLoop(t, a, func() [2]int {
		x, y, _, _ := a.list.GetInnerRect()
		return [2]int{x, y + 2}
	})
	clickAt(screen, cell[0], cell[1])
	waitFor(t, a, "the second drive to be shown", func() bool {
		return a.detail.device == "/dev/sdb"
	})
	if !onLoop(t, a, func() bool { return a.status.GetText(true) == stripTags(a.statusText()) }) {
		t.Error("the hint bar is stale: refreshChrome did not run after the drive click")
	}
}
