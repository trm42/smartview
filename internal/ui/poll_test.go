// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestPollRepaintsTheDriveList is the regression that made the whole wide
// layout lie: the rows carry the health glyph, model, capacity and
// temperature, so a poll that folds reports into App state without
// repopulating the list leaves every drive on "scanning…" for the session.
// The narrow rail hides it, because showDevice redraws that one.
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
