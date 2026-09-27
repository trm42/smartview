// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/trm42/smartview/internal/smart"
)

// TestPollRepaintsTheDriveList: a poll must repopulate the list, or every row stays on "scanning…".
func TestPollRepaintsTheDriveList(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	d := smart.Device{Name: dev, Type: "sat"}
	a.devices = []smart.Device{d}
	a.populateList() // as Run does, before any report has landed

	if _, sec := a.list.GetItemText(0); !strings.Contains(sec, "scanning") {
		t.Fatalf("the pre-poll row is not the scanning state: %q", sec)
	}

	a.applyPoll(map[string]pollResult{dev: {rep: healthyReport(dev)}})

	main, sec := a.list.GetItemText(0)
	if strings.Contains(sec, "scanning") {
		t.Errorf("the drive list still says scanning after a poll: %q", sec)
	}
	if !strings.Contains(main, healthyReport(dev).ModelName) {
		t.Errorf("row main text %q does not carry the model from the poll", main)
	}
}

// TestPollMarksAndUnmarksStandbyInTheList pairs with the above: the standby
// glyph reaches the list only through the poll's repaint.
func TestPollMarksAndUnmarksStandbyInTheList(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}
	a.populateList()

	a.applyPoll(map[string]pollResult{dev: {rep: healthyReport(dev)}})
	if _, sec := a.list.GetItemText(0); strings.Contains(sec, standbyGlyph) {
		t.Errorf("an awake drive carries the standby glyph: %q", sec)
	}
	a.applyPoll(map[string]pollResult{dev: {standby: true}})
	if _, sec := a.list.GetItemText(0); !strings.Contains(sec, standbyGlyph) {
		t.Errorf("a spun-down drive has no standby glyph in the list: %q", sec)
	}
}

// TestLatestIntervalWins: two changes while the poll loop is busy fetching
// must leave the second one queued, not the first.
func TestLatestIntervalWins(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	a.setInterval(10 * time.Second)
	a.setInterval(5 * time.Second)
	select {
	case d := <-a.intervalCh:
		if d != 5*time.Second {
			t.Errorf("intervalCh carried %s, want 5s", d)
		}
	default:
		t.Fatal("intervalCh is empty")
	}
}
