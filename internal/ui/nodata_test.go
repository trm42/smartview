// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// noDataReport is the envelope smartctl prints when it cannot open the device.
func noDataReport(name string) *smart.Report {
	return &smart.Report{
		Device: smart.Device{Name: name, Type: "sat", Protocol: "ATA"},
		Smartctl: smart.Smartctl{Messages: []smart.Message{{
			String: "Smartctl open device: " + name + " failed: Permission denied", Severity: "error",
		}}},
	}
}

// TestNoVerdictIsNotFailing: an unreadable drive reads "No data", never the
// failing verdict or the healthy one.
func TestNoVerdictIsNotFailing(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sde"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}
	a.applyResults(map[string]pollResult{dev: {rep: noDataReport(dev)}})

	main, _ := a.listRow(a.devices[0])
	if !strings.Contains(main, noVerdictGlyph) {
		t.Errorf("list row %q lacks the no-data mark", main)
	}
	for _, g := range []string{"●", "▲", "■"} {
		if strings.Contains(main, g) {
			t.Errorf("list row %q carries severity glyph %s", main, g)
		}
	}
	if v := reportVerdict(a.reports[dev]); !strings.Contains(v, "No data") {
		t.Errorf("verdict = %q, want No data", v)
	}
	if n := a.alertCount(); n != 0 {
		t.Errorf("alertCount() = %d, want 0: an unread drive is not an alert", n)
	}
}

// TestFailedReadKeepsTheLastReading: one failed open between good polls must
// not blank the drive, the same contract standby keeps.
func TestFailedReadKeepsTheLastReading(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	const dev = "/dev/sdb"
	a.devices = []smart.Device{{Name: dev, Type: "sat"}}

	good := healthyReport(dev)
	a.applyResults(map[string]pollResult{dev: {rep: good}})
	first := a.lastRead[dev]

	a.applyResults(map[string]pollResult{dev: {rep: noDataReport(dev)}})

	if a.reports[dev] != good {
		t.Error("a failed read replaced the last good report")
	}
	if a.lastRead[dev] != first {
		t.Error("lastRead moved on a failed read")
	}
}

// TestNoVerdictStillShowsDegradation: data that is present still grades, so a
// partial read with a failing attribute is not hidden behind "?".
func TestNoVerdictStillShowsDegradation(t *testing.T) {
	r := noDataReport("/dev/sde")
	r.ATAAttributes = &smart.ATAAttributes{Table: []smart.ATAAttribute{{ID: 5, WhenFailed: "FAILING_NOW"}}}
	if noVerdict(r) {
		t.Error("noVerdict() = true for a report with a failing attribute")
	}
	if g := reportGlyph(r); !strings.Contains(g, "■") {
		t.Errorf("reportGlyph() = %q, want the failing mark", g)
	}
}
