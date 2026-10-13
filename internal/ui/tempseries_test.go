// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestSparseSCTLogFallsBackToRuntime: an SCT table is a trend only once two
// slots are filled; below that the runtime series is the history.
func TestSparseSCTLogFallsBackToRuntime(t *testing.T) {
	runtime := []float64{30, 31, 32}
	legend := func(r *smart.Report) string {
		series, onDevice := temperatureSeries(r, runtime)
		return temperatureSection().legend([]fleetRow{{rep: r, series: series, onDeviceLog: onDevice}})
	}
	const polls = "successive polls"

	for _, table := range []string{`[null,null,null,31]`, `[null,null]`, `[31]`, `[]`} {
		r := sctReport(t, table)
		if got, onDevice := temperatureSeries(r, runtime); !slices.Equal(got, runtime) || onDevice {
			t.Errorf("table %s: series = %v, want the runtime %v", table, got, runtime)
		}
		if buildTempSparkline(r, runtime) == nil {
			t.Errorf("table %s: no chart though %d runtime samples exist", table, len(runtime))
		}
		if !strings.Contains(legend(r), polls) {
			t.Errorf("table %s: legend does not say the trend builds over polls", table)
		}
	}

	r := sctReport(t, `[null,35,null,36]`)
	// The live 31°C closes the log as its newest point.
	if got, want := first(temperatureSeries(r, runtime)), []float64{35, 36, 31}; !slices.Equal(got, want) {
		t.Errorf("series = %v, want the SCT log and the live reading %v", got, want)
	}
	if strings.Contains(legend(r), polls) {
		t.Error("legend claims a runtime trend for a drive with a usable SCT log")
	}

	if got, _ := sctSeries(sctReport(t, `[35,-128,36,255]`)); !slices.Equal(got, []float64{35, 36}) {
		t.Errorf("series = %v, want the implausible slots dropped", got)
	}
}

// first drops a pair's second value.
func first[A, B any](a A, _ B) A { return a }

// TestImplausibleLiveTempIsNotPlotted: an absent-sensor 255 must not rescale the chart or set its range.
func TestImplausibleLiveTempIsNotPlotted(t *testing.T) {
	r := sctReport(t, `[35,40]`)
	bogus := 255
	r.Temperature.Current = &bogus
	if got := first(temperatureSeries(r, nil)); !slices.Equal(got, []float64{35, 40}) {
		t.Errorf("series = %v, want the log without the implausible live reading", got)
	}
}

// TestFleetTempRangeIncludesTheLiveReading: the fleet's Min/Max read the same series the Overview plots.
func TestFleetTempRangeIncludesTheLiveReading(t *testing.T) {
	r := sctReport(t, `[35,40]`)
	hot := 61
	r.Temperature.Current = &hot
	series, onDevice := temperatureSeries(r, nil)
	cells := temperatureSection().cells(fleetRow{rep: r, series: series, onDeviceLog: onDevice})
	if got := cells[2].text; got != "61" {
		t.Errorf("fleet Max = %q, want the live 61 above the log's 40", got)
	}
}
