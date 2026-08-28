// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/smart"
)

// fleetBudget is what the fleet decided to render on a given frame.
type fleetBudget struct{ shown, identity, width int }

// TestFleetMeasuresItsTableOnTheFirstVisibleFrame is the regression test for a
// fleet view that opened empty. The column budget is measured from the table's
// inner rect, and a Flex assigns its children's rects inside Flex.Draw — so a
// measurement taken before that call reads the previous frame's width, which is
// zero on the frame the fleet first becomes visible. Every comparison column was
// dropped, the identity narrowed to the device name alone, and nothing scheduled
// another draw: the view stayed a bare "Drive" list until the next key press.
//
// The budget is therefore read in the after-draw hook, on the very frame the
// user first sees. Reading it from a queued update instead would hide the bug —
// queuing an update draws again, and the second frame was always correct.
func TestFleetMeasuresItsTableOnTheFirstVisibleFrame(t *testing.T) {
	a, screen := newSimApp(t, 120, 40)

	drawn := make(chan struct{}, 1)
	budget := make(chan fleetBudget, 1)
	a.app.SetAfterDrawFunc(func(tcell.Screen) {
		if a.fleetMode {
			select {
			case budget <- fleetBudget{a.fleet.shownCols, a.fleet.identityCols, a.fleet.lastWidth}:
			default: // only the first visible frame is under test
			}
		}
		select {
		case drawn <- struct{}{}:
		default:
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.app.Run() }()
	t.Cleanup(func() {
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
		t.Fatal("no initial draw within the timeout")
	}

	devs := []smart.Device{{Name: "/dev/sda"}, {Name: "/dev/sdb"}}
	a.devices = devs
	a.reports = map[string]*smart.Report{
		"/dev/sda": {
			Device:      smart.Device{Name: "/dev/sda", Protocol: "ATA"},
			ModelName:   "ST22000NT001-3LS101",
			Temperature: &smart.Temperature{Current: ptr(37)},
		},
		"/dev/sdb": {
			Device:      smart.Device{Name: "/dev/sdb", Protocol: "ATA"},
			ModelName:   "Samsung SSD 850 EVO",
			Temperature: &smart.Temperature{Current: ptr(29)},
		},
	}
	go a.app.QueueUpdateDraw(func() { a.toggleFleet() })

	var got fleetBudget
	select {
	case got = <-budget:
	case <-time.After(testTimeout):
		t.Fatal("the fleet page never drew within the timeout")
	}

	if got.shown == 0 {
		t.Errorf("the first visible fleet frame dropped every comparison column "+
			"(shownCols=0, measured width %d): the table was measured before "+
			"Flex.Draw assigned its rect", got.width)
	}
	if got.identity != len(fleetIdentityColumns) {
		t.Errorf("the first visible fleet frame showed %d identity columns, want %d: "+
			"a 120-column terminal is not narrow", got.identity, len(fleetIdentityColumns))
	}
	if got.width <= narrowBreakpoint {
		t.Errorf("the first visible fleet frame measured its table at %d cells "+
			"in a 120-column terminal", got.width)
	}
}

// ptr is the one-liner the sparse schema needs: nearly every reading is a
// pointer so "absent" and "reported as zero" stay distinguishable.
func ptr[T any](v T) *T { return &v }
