// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/smart"
)

const railMarker = "▸"

// railSelection returns the rail name the selection marker sits on, failing unless there is exactly one marker.
func railSelection(t *testing.T, text string) string {
	t.Helper()
	if n := strings.Count(text, railMarker); n != 1 {
		t.Fatalf("rail carries %d selection markers, want 1: %q", n, text)
	}
	_, after, _ := strings.Cut(text, railMarker)
	// The marker is followed by the drive's glyph, then its name.
	fields := strings.Fields(after)
	if len(fields) < 2 {
		t.Fatalf("no drive name follows the selection marker: %q", text)
	}
	return fields[1]
}

// TestRailMarksTheSelectionWithoutAReport: the rail is the narrow layout's only
// selector, so the selected drive is marked whether or not it has been read.
func TestRailMarksTheSelectionWithoutAReport(t *testing.T) {
	devices := []smart.Device{{Name: "/dev/sda"}, {Name: "/dev/sdb"}, {Name: "/dev/sdc"}}
	cases := []struct {
		name     string
		reported []string
		asleep   []string
	}{
		{"cold start", nil, nil},
		{"selected drive parked", []string{"/dev/sda", "/dev/sdc"}, []string{"/dev/sdb"}},
		{"only the selection unread", []string{"/dev/sda", "/dev/sdc"}, nil},
		{"only the selection read", []string{"/dev/sdb"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := newSimApp(t, 80, 30)
			a.devices = devices
			a.app.SetFocus(a.detail.content())
			a.setNarrow(true)
			for _, name := range c.reported {
				a.reports[name] = healthyReport(name)
			}
			for _, name := range c.asleep {
				a.asleep[name] = true
			}
			a.populateList()

			for i, d := range devices {
				a.renderRail(i)
				if got, want := railSelection(t, a.rail.GetText(true)), railName(d); got != want {
					t.Errorf("selection at %d: marker sits on %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestRailSelectionFollowsTheArrows drives the real key path over drives that have never answered.
func TestRailSelectionFollowsTheArrows(t *testing.T) {
	a, screen := newSimApp(t, 80, 30)
	runSim(t, a, screen)

	onLoop(t, a, func() bool {
		a.devices = []smart.Device{{Name: "/dev/sda"}, {Name: "/dev/sdb"}}
		a.populateList()
		return true
	})
	railText := func() string { return a.rail.GetText(true) }
	if got := railSelection(t, onLoop(t, a, railText)); got != "sda" {
		t.Errorf("before any key: marker sits on %q, want sda", got)
	}

	screen.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	// The key and a queued update reach the loop on different channels, so poll for it.
	deadline := time.Now().Add(testTimeout)
	for onLoop(t, a, func() int { return a.list.GetCurrentItem() }) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("Down did not move the selection within the timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := railSelection(t, onLoop(t, a, railText)); got != "sdb" {
		t.Errorf("after Down: marker sits on %q, want sdb", got)
	}
	// A queued update runs after the draw of the one before it.
	drawn := strings.Join(onLoop(t, a, func() []string { return screenText(screen) }), "\n")
	if !strings.Contains(drawn, railMarker) {
		t.Errorf("no selection marker on the drawn rail:\n%s", drawn)
	}
}
