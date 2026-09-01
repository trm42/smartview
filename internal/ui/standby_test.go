// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/trm42/smartview/internal/smart"
)

func healthyReport(name string) *smart.Report {
	return &smart.Report{
		Device:      smart.Device{Name: name, Type: "sat", Protocol: "ATA"},
		ModelName:   "ACME SpinRight 8TB",
		SmartStatus: smart.SmartStatus{Passed: true},
		Temperature: &smart.Temperature{Current: new(38)},
	}
}

// TestStandbyKeepsTheLastReading is the core contract: an empty standby report
// must never replace a good one, or a drive going to sleep would blank every
// value smartview had already read.
func TestStandbyKeepsTheLastReading(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}

	good := healthyReport(dev)
	a.applyResults(map[string]pollResult{dev: {rep: good}})
	first := a.lastRead[dev]

	a.applyResults(map[string]pollResult{dev: {standby: true}})

	if a.reports[dev] != good {
		t.Error("a standby poll replaced the last good report")
	}
	if !a.asleep[dev] {
		t.Error("asleep[dev] = false after a standby poll")
	}
	if a.lastRead[dev] != first {
		t.Error("lastRead moved on a standby poll; the reading is no newer than it was")
	}
}

// TestWakingClearsTheStandbyMark: the drive must stop reading as asleep once
// it answers again.
func TestWakingClearsTheStandbyMark(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}

	a.applyResults(map[string]pollResult{dev: {standby: true}})
	a.applyResults(map[string]pollResult{dev: {rep: healthyReport(dev)}})

	if a.asleep[dev] {
		t.Error("asleep[dev] = true after the drive answered")
	}
	if a.reports[dev] == nil {
		t.Error("the waking report was not stored")
	}
}

// TestStandbyIsMarkedInTheDriveList: the mark is the whole reason stale values
// are safe to show.
func TestStandbyIsMarkedInTheDriveList(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	d := smart.Device{Name: dev, Type: "sat"}
	a.devices = []smart.Device{d}

	a.applyResults(map[string]pollResult{dev: {rep: healthyReport(dev)}})
	_, awake := a.listRow(d)
	a.applyResults(map[string]pollResult{dev: {standby: true}})
	_, asleep := a.listRow(d)

	if strings.Contains(awake, standbyGlyph) {
		t.Errorf("an awake drive carries the standby glyph: %q", awake)
	}
	if !strings.Contains(asleep, standbyGlyph) {
		t.Errorf("a spun-down drive has no standby glyph: %q", asleep)
	}
}

// TestNeverReadStandbyDriveExplainsItself: a cold start with every drive
// asleep must say so and name the way out, not sit on "Loading…" forever.
func TestNeverReadStandbyDriveExplainsItself(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}
	a.populateList()

	a.applyResults(map[string]pollResult{dev: {standby: true}})
	a.showDevice(0)

	got := a.detail.placeholder
	for _, want := range []string{"spun down", "R"} {
		if !strings.Contains(got, want) {
			t.Errorf("placeholder %q does not mention %q", got, want)
		}
	}
}

// TestDetailNoteDatesAStaleReading: showing last week's temperature with no
// indication of its age would be worse than showing nothing.
func TestDetailNoteDatesAStaleReading(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}

	a.applyResults(map[string]pollResult{dev: {rep: healthyReport(dev)}})
	a.showDevice(0)
	if got := a.detail.note.GetText(true); strings.TrimSpace(got) != "" {
		t.Errorf("a fresh drive carries a caveat note: %q", got)
	}

	a.lastRead[dev] = time.Now().Add(-12 * time.Minute)
	a.applyResults(map[string]pollResult{dev: {standby: true}})
	a.showDevice(0)

	got := a.detail.note.GetText(true)
	if !strings.Contains(got, "Spun down") {
		t.Errorf("note %q does not say the drive is spun down", got)
	}
	if !strings.Contains(got, "12m") {
		t.Errorf("note %q does not date the reading", got)
	}
}

// TestForceRefreshSignalsTheWakeChannel: 'r' and 'R' must reach different
// channels, or the gentle refresh would wake the drive too.
func TestForceRefreshSignalsTheWakeChannel(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)

	a.triggerRefresh()
	select {
	case <-a.refreshCh:
	default:
		t.Error("triggerRefresh did not signal refreshCh")
	}
	select {
	case <-a.wakeCh:
		t.Error("triggerRefresh signalled wakeCh; 'r' must respect standby")
	default:
	}

	a.forceRefresh()
	select {
	case <-a.wakeCh:
	default:
		t.Error("forceRefresh did not signal wakeCh")
	}
}

// TestFleetMarksStandbyDrives: the fleet is a whole-fleet comparison, so a
// stale row there is exactly as misleading as a stale drive-list row.
func TestFleetMarksStandbyDrives(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	awake, parked := "/dev/sda", "/dev/sdb"
	a.devices = []smart.Device{{Name: awake, Type: "sat"}, {Name: parked, Type: "sat"}}

	a.applyResults(map[string]pollResult{
		awake:  {rep: healthyReport(awake)},
		parked: {rep: healthyReport(parked)},
	})
	a.applyResults(map[string]pollResult{parked: {standby: true}})
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)

	var marked, plain int
	for r := range a.fleet.table.GetRowCount() {
		cell := a.fleet.table.GetCell(r, 0)
		if cell == nil {
			continue
		}
		if strings.Contains(cell.Text, standbyGlyph) {
			marked++
		} else if strings.Contains(cell.Text, "sd") {
			plain++
		}
	}
	if marked != 1 {
		t.Errorf("%d rows carry the standby glyph, want exactly 1", marked)
	}
	if plain != 1 {
		t.Errorf("%d awake rows, want exactly 1", plain)
	}
}

// TestFleetLegendExplainsTheGlyph: a glyph with no legend is a puzzle.
func TestFleetLegendExplainsTheGlyph(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}

	a.applyResults(map[string]pollResult{dev: {rep: healthyReport(dev)}})
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)
	if got := a.fleet.legend.GetText(true); strings.Contains(got, standbyGlyph) {
		t.Errorf("legend explains standby with no drive asleep: %q", got)
	}

	a.applyResults(map[string]pollResult{dev: {standby: true}})
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)
	if got := a.fleet.legend.GetText(true); !strings.Contains(got, standbyGlyph) {
		t.Errorf("legend %q does not explain the standby glyph", got)
	}
}
