// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/config"
	"github.com/trm42/smartview/internal/smart"
)

// fleetTableText joins every cell the fleet table holds, one line per row; event-loop only.
func fleetTableText(v *fleetView) string {
	var b strings.Builder
	for r := range v.table.GetRowCount() {
		for c := range v.table.GetColumnCount() {
			b.WriteString(v.table.GetCell(r, c).Text)
			b.WriteByte('|')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// fleetFrame is what the fleet put on one frame.
type fleetFrame struct {
	table, legend string
	legendWidth   int
}

// startFleetSim mirrors Run after the scan under start_view = "fleet" and returns the first visible fleet frame.
func startFleetSim(t *testing.T, width int, devices []smart.Device, asleep ...string) (*App, fleetFrame) {
	t.Helper()
	cfg := config.Default()
	cfg.StartView = config.StartFleet
	a, screen := newSimAppCfg(t, width, 40, cfg)
	a.devices = devices
	for _, name := range asleep {
		a.asleep[name] = true
	}
	a.populateList()
	a.applyStartView()

	frame := make(chan fleetFrame, 1)
	a.app.SetAfterDrawFunc(func(tcell.Screen) {
		if !a.fleetMode {
			return
		}
		_, _, w, _ := a.fleet.legend.GetInnerRect()
		select {
		case frame <- fleetFrame{fleetTableText(a.fleet), a.fleet.legend.GetText(true), w}:
		default: // only the first visible frame is under test
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
	case got := <-frame:
		return a, got
	case <-time.After(testTimeout):
		t.Fatal("the fleet page never drew within the timeout")
		return nil, fleetFrame{}
	}
}

// TestFleetListsDrivesThatHaveNoReport: a fleet where no drive has reported yet, or every drive is parked, still shows its rows.
func TestFleetListsDrivesThatHaveNoReport(t *testing.T) {
	devs := []smart.Device{{Name: "/dev/sda"}, {Name: "/dev/sdb"}}
	a, frame := startFleetSim(t, 120, devs)
	first := frame.table

	for _, d := range devs {
		if !strings.Contains(first, d.Name) {
			t.Errorf("first fleet frame has no row for %s before any report:\n%s", d.Name, first)
		}
	}
	if strings.Contains(first, "Scanning for drives") {
		t.Errorf("first fleet frame claims to be scanning for drives it already lists:\n%s", first)
	}

	parked := onLoop(t, a, func() string {
		a.applyPoll(map[string]pollResult{
			"/dev/sda": {standby: true},
			"/dev/sdb": {standby: true},
		})
		return fleetTableText(a.fleet)
	})
	if got := strings.Count(parked, "asleep"); got != len(devs) {
		t.Errorf("fleet with every drive parked marks %d rows asleep, want %d:\n%s", got, len(devs), parked)
	}
	for _, d := range devs {
		if !strings.Contains(parked, standbyGlyph+" "+d.Name) {
			t.Errorf("parked fleet has no standby-marked row for %s:\n%s", d.Name, parked)
		}
	}
	if n := onLoop(t, a, a.fleet.sectionCount); n == 0 {
		t.Error("parked fleet offers no section, so its rows have nothing to render under")
	}
}

// TestFleetSaysWhenTheScanFoundNothing: an empty scan result is stated, not shown as a scan still running.
func TestFleetSaysWhenTheScanFoundNothing(t *testing.T) {
	_, frame := startFleetSim(t, 120, nil)
	first := frame.table
	if !strings.Contains(first, "No drives found") {
		t.Errorf("fleet after an empty scan = %q, want it to say %q", first, "No drives found")
	}
	if strings.Contains(first, "Scanning for drives") {
		t.Errorf("fleet after an empty scan still claims to be scanning: %q", first)
	}
}

// TestFleetLegendIsTrueForUnreadDrives: with no report there is no last read and no unreported counter, and below 100 columns the legend is the only explanation on screen.
func TestFleetLegendIsTrueForUnreadDrives(t *testing.T) {
	devs := []smart.Device{{Name: "/dev/sda"}, {Name: "/dev/sdb"}}
	for _, width := range []int{80, 100, 120} {
		_, frame := startFleetSim(t, width, devs, "/dev/sda", "/dev/sdb")
		got := frame.legend
		for _, want := range []string{standbyGlyph + " spun down", "has not been read yet"} {
			if !strings.Contains(got, want) {
				t.Errorf("%d columns: legend for parked, never-read drives = %q, want it to say %q", width, got, want)
			}
		}
		for _, lie := range []string{"last read", "does not report"} {
			if strings.Contains(got, lie) {
				t.Errorf("%d columns: legend for parked, never-read drives claims %q: %q", width, lie, got)
			}
		}
		if lines := tview.WordWrap(got, frame.legendWidth); len(lines) > fleetLegendHeight {
			t.Errorf("%d columns: legend wraps to %d rows at %d cells, want at most %d: %q",
				width, len(lines), frame.legendWidth, fleetLegendHeight, got)
		}
	}
}

// TestFleetLegendQualifiesTheDash: beside drives that have reported, an unread row's dashes are explained and a read standby row keeps its caveat.
func TestFleetLegendQualifiesTheDash(t *testing.T) {
	sec := healthSection()
	read := fleetRow{dev: smart.Device{Name: "/dev/sda"}, rep: &smart.Report{}}
	unread := fleetRow{dev: smart.Device{Name: "/dev/sdb"}}
	parked := fleetRow{dev: smart.Device{Name: "/dev/sdc"}, rep: &smart.Report{}, asleep: true}

	if got := fleetLegend([]fleetRow{read}, sec); got != sec.legend(nil) {
		t.Errorf("legend for a fully read, awake fleet = %q, want the section's own %q", got, sec.legend(nil))
	}
	mixed := fleetLegend([]fleetRow{read, unread}, sec)
	if !strings.Contains(mixed, "has not been read yet") || !strings.Contains(mixed, sec.legend(nil)) {
		t.Errorf("legend with one unread drive = %q, want the unread caveat and the section's own", mixed)
	}
	if strings.Contains(mixed, standbyGlyph) {
		t.Errorf("legend with no parked drive mentions standby: %q", mixed)
	}
	if got := fleetLegend([]fleetRow{read, parked}, sec); !strings.Contains(got, "values as of the last read") ||
		strings.Contains(got, "has not been read yet") {
		t.Errorf("legend with a read, parked drive = %q, want only the last-read caveat", got)
	}
}
