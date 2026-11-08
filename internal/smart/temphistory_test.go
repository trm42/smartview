// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"encoding/json"
	"slices"
	"testing"
)

// sctReport is an ATA report reading 31°C live whose SCT history table is the given JSON array.
func sctReport(t *testing.T, table string) *Report {
	t.Helper()
	var r Report
	raw := `{"temperature":{"current":31},"ata_sct_temperature_history":{"table":` + table + `}}`
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// TestSCTSeriesDropsImplausibleSlots: absent-sensor markers never reach the series.
func TestSCTSeriesDropsImplausibleSlots(t *testing.T) {
	if got, _ := sctSeries(sctReport(t, `[35,-128,36,255]`)); !slices.Equal(got, []float64{35, 36}) {
		t.Errorf("series = %v, want the implausible slots dropped", got)
	}
}

// TestImplausibleLiveTempIsNotPlotted: an absent-sensor 255 must not rescale the chart or set its range.
func TestImplausibleLiveTempIsNotPlotted(t *testing.T) {
	r := sctReport(t, `[35,40]`)
	bogus := 255
	r.Temperature.Current = &bogus
	if got, _ := r.TempHistory(nil); !slices.Equal(got, []float64{35, 40}) {
		t.Errorf("series = %v, want the log without the implausible live reading", got)
	}
}
