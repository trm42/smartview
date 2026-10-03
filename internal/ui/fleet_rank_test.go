// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// fixtureReport decodes one captured smartctl envelope from the data layer's testdata.
func fixtureReport(t *testing.T, name string) *smart.Report {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var r smart.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return &r
}

// TestFleetHealthSortsNoVerdictLast: a drive with no verdict cannot report the
// health metric, so it sorts after every graded drive whatever its device name.
func TestFleetHealthSortsNoVerdictLast(t *testing.T) {
	healthy := fixtureReport(t, "smart-sdb.json")
	noData := fixtureReport(t, "smart-sde-nodata.json")
	if !noVerdict(noData) || noVerdict(healthy) {
		t.Fatal("fixtures no longer give one no-verdict and one graded report")
	}

	// The healthy pair ties, and one of them names after the no-verdict drive.
	names := []string{"/dev/sda", "/dev/sdc", "/dev/sde", "/dev/sdz"}
	reports := map[string]*smart.Report{
		"/dev/sda": healthy,
		"/dev/sdc": fixtureReport(t, "smart-sdc-failing.json"),
		"/dev/sde": noData,
		"/dev/sdz": healthy,
	}
	devices := make([]smart.Device, len(names))
	for i, n := range names {
		devices[i] = smart.Device{Name: n, Protocol: "ATA"}
	}

	v := newFleetView(nil)
	v.activeID = "health"
	v.refresh(devices, reports, nil, nil)

	got := make([]string, 0, len(names))
	for row := 1; row < v.table.GetRowCount(); row++ {
		// The device cell leads with the health glyph; the name is its last field.
		fields := strings.Fields(v.table.GetCell(row, 0).Text)
		if len(fields) == 0 {
			t.Fatalf("row %d has an empty device cell", row)
		}
		got = append(got, fields[len(fields)-1])
	}
	want := []string{"/dev/sdc", "/dev/sda", "/dev/sdz", "/dev/sde"}
	if !slices.Equal(got, want) {
		t.Errorf("health-sorted fleet rows = %v, want %v", got, want)
	}

	if val, ok := healthSection().rank(fleetRow{dev: devices[2], rep: noData}); ok {
		t.Errorf("no-verdict rank = (%v, true), want ok=false", val)
	}
}
