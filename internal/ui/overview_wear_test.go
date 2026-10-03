// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// wearFixture decodes a captured fixture with the named top-level sections removed.
func wearFixture(t *testing.T, name string, drop ...string) *smart.Report {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range drop {
		if _, ok := raw[k]; !ok {
			t.Fatalf("%s has no %q section to drop", name, k)
		}
		delete(raw, k)
	}
	return wearReport(t, raw)
}

// wearReport decodes v, marshalled to JSON, as a report.
func wearReport(t *testing.T, v any) *smart.Report {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var r smart.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// wearSinks counts where the Overview draws a wear reading: identity rows by
// key, gauges by title.
func wearSinks(r *smart.Report, row, gauge string) (rows, gauges int) {
	for _, sec := range identitySections(r) {
		for _, f := range sec.fields {
			if f.k == row {
				rows++
			}
		}
	}
	if col, ok := buildGauges(r).(*tview.Flex); ok {
		for i := range col.GetItemCount() {
			g, ok := col.GetItem(i).(interface{ GetTitle() string })
			if ok && strings.TrimSpace(g.GetTitle()) == gauge {
				gauges++
			}
		}
	}
	return rows, gauges
}

// ataWearReport is an ATA SSD whose only wear source is Device Statistics.
func ataWearReport(t *testing.T, used int) *smart.Report {
	t.Helper()
	return wearReport(t, map[string]any{
		"device": map[string]any{"name": "/dev/sdz", "type": "sat", "protocol": "ATA"},
		"ata_device_statistics": map[string]any{"pages": []any{map[string]any{
			"number": 7, "name": "Solid State Device Statistics", "revision": 1,
			"table": []any{map[string]any{
				"offset": 8, "name": "Percentage Used Endurance Indicator", "size": 1,
				"value": used, "flags": map[string]any{"value": 192, "string": "V---", "valid": true},
			}},
		}}},
	})
}

// spareOnlyReport is an NVMe drive reporting spare only through the top-level field.
func spareOnlyReport(t *testing.T, pct, threshold int) *smart.Report {
	t.Helper()
	return wearReport(t, map[string]any{
		"device":          map[string]any{"name": "/dev/nvme9", "type": "nvme", "protocol": "NVMe"},
		"spare_available": map[string]any{"current_percent": pct, "threshold_percent": threshold},
	})
}

// wearField returns the identity panel's value for key.
func wearField(t *testing.T, r *smart.Report, key string) string {
	t.Helper()
	for _, sec := range identitySections(r) {
		for _, f := range sec.fields {
			if f.k == key {
				return f.v
			}
		}
	}
	t.Fatalf("no %q row", key)
	return ""
}

// TestFallbackWearRowsAreGraded: a fallback row prints the resolved value,
// plain in band and tinted with the gauges' graders out of it.
func TestFallbackWearRowsAreGraded(t *testing.T) {
	cases := []struct {
		name, key, want string
		rep             *smart.Report
	}{
		{"life in band", "Life used", "7%", ataWearReport(t, 7)},
		{"life exhausted", "Life used", sevText(smart.SeverityFailing, "100%"), ataWearReport(t, 100)},
		{"spare in band", "Spare avail", "80%", spareOnlyReport(t, 80, 10)},
		{"spare near threshold", "Spare avail", sevText(smart.SeverityCaution, "15%"), spareOnlyReport(t, 15, 10)},
		{"spare below threshold", "Spare avail", sevText(smart.SeverityFailing, "5%"), spareOnlyReport(t, 5, 10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wearField(t, tc.rep, tc.key); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

// TestOverviewDrawsEveryResolvedWearReading: a wear reading the accessors
// resolve is drawn exactly once, whichever section it came from.
func TestOverviewDrawsEveryResolvedWearReading(t *testing.T) {
	const healthLog = "nvme_smart_health_information_log"
	cases := []struct {
		name            string
		rep             *smart.Report
		wantLife        bool
		wantSpare       bool
		wantLifeAsRow   bool
		wantSpareAsRow  bool
		wantLifeRowText string
	}{
		{name: "nvme health log", rep: wearFixture(t, "smart-nvme.json"), wantLife: true, wantSpare: true},
		{name: "apple nvme", rep: wearFixture(t, "smart-apple-nvme.json"), wantLife: true, wantSpare: true},
		{
			name: "apple top-level only", rep: wearFixture(t, "smart-apple-nvme.json", healthLog),
			wantLife: true, wantSpare: true, wantLifeAsRow: true, wantSpareAsRow: true, wantLifeRowText: "3%",
		},
		{
			name: "ata device statistics", rep: ataWearReport(t, 7),
			wantLife: true, wantLifeAsRow: true, wantLifeRowText: "7%",
		},
		{name: "ata hdd", rep: wearFixture(t, "smart-sda.json")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, lifeOK := tc.rep.LifeUsedPercent()
			_, _, spareOK := tc.rep.SparePercent()
			if lifeOK != tc.wantLife || spareOK != tc.wantSpare {
				t.Fatalf("accessors report life=%v spare=%v, want %v %v: the case no longer covers its source",
					lifeOK, spareOK, tc.wantLife, tc.wantSpare)
			}

			check := func(what, row, gauge string, reported, asRow bool) {
				rows, gauges := wearSinks(tc.rep, row, gauge)
				want := 0
				if reported {
					want = 1
				}
				if rows+gauges != want {
					t.Errorf("%s drawn %d times (%d rows, %d gauges), want %d", what, rows+gauges, rows, gauges, want)
				}
				if asRow && rows != 1 {
					t.Errorf("%s: %d rows, want the row since no gauge has a source", what, rows)
				}
			}
			check("life used", "Life used", "Life left", lifeOK, tc.wantLifeAsRow)
			check("spare", "Spare avail", "Spare avail", spareOK, tc.wantSpareAsRow)

			if tc.wantLifeRowText != "" {
				if text := identityText(tc.rep, 120); !strings.Contains(text, tc.wantLifeRowText) {
					t.Errorf("identity panel lacks %q:\n%s", tc.wantLifeRowText, text)
				}
			}
		})
	}
}
