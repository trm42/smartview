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

// TestTempHistoryFromFixtures: a captured SCT log is the series, closed by the live reading; NVMe falls back to runtime.
func TestTempHistoryFromFixtures(t *testing.T) {
	for _, c := range []struct {
		file string
		live float64
	}{{"smart-sda.json", 37}, {"smart-sdb.json", 29}} {
		r := parseFixture(t, c.file)
		got, own := r.TempHistory([]float64{1, 2})
		if !own || len(got) != len(r.ATATemperatureHistory.Table)+1 || got[len(got)-1] != c.live {
			t.Errorf("%s: own=%v len=%d last=%v, want the %d-slot log closed by %v",
				c.file, own, len(got), got[len(got)-1], len(r.ATATemperatureHistory.Table), c.live)
		}
	}
	runtime := []float64{40, 41}
	if got, own := parseFixture(t, "smart-nvme.json").TempHistory(runtime); own || !slices.Equal(got, runtime) {
		t.Errorf("nvme: own=%v series=%v, want the runtime buffer", own, got)
	}
}
