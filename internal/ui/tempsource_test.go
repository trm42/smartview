// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// sctReport is an ATA report whose SCT history table is the given JSON array.
func sctReport(t *testing.T, table string) *smart.Report {
	t.Helper()
	var r smart.Report
	raw := `{"temperature":{"current":31},"ata_sct_temperature_history":{"table":` + table + `}}`
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// TestTempChartReadsTheCurrentTemperature: the SCT log lags by its logging
// interval, so "now" and the grade come from the live reading, not its last slot.
func TestTempChartReadsTheCurrentTemperature(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current int
		sev     smart.Severity
	}{
		{"hotter than the log", 61, smart.SeverityCaution},
		{"failing", 70, smart.SeverityFailing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := loadReport(t, "smart-sda.json")
			r.Temperature.Current = &tc.current

			c, ok := buildTempSparkline(r, nil).(*rangeChart)
			if !ok {
				t.Fatal("no temperature chart for a drive with an SCT history")
			}
			if title, want := c.GetTitle(), "now "+tempText(tc.current); !strings.Contains(title, want) {
				t.Errorf("title %q lacks %q", title, want)
			}
			if want := severityColor(tc.sev); c.color != want {
				t.Errorf("chart colour = %v, want %v", c.color, want)
			}
		})
	}

	t.Run("cooler than the log", func(t *testing.T) {
		cool := 30
		r := sctReport(t, `[40,50,60]`)
		r.Temperature.Current = &cool
		c := buildTempSparkline(r, nil).(*rangeChart)
		if c.color != activeTheme.BarHealthy {
			t.Errorf("chart colour = %v, want the in-band %v", c.color, activeTheme.BarHealthy)
		}
	})

	t.Run("no live reading", func(t *testing.T) {
		r := sctReport(t, `[40,50,60]`)
		r.Temperature = nil
		c := buildTempSparkline(r, nil).(*rangeChart)
		if title := c.GetTitle(); !strings.Contains(title, "now "+tempText(60)) {
			t.Errorf("title %q does not fall back to the last sample", title)
		}
	})
}

// tempText is the title's rendering of a temperature.
func tempText(c int) string { return strconv.Itoa(c) + "°C" }
