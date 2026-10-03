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
		return temperatureSection().legend([]fleetRow{{rep: r}})
	}
	const polls = "successive polls"

	for _, table := range []string{`[null,null,null,31]`, `[null,null]`, `[31]`, `[]`} {
		r := sctReport(t, table)
		if got := temperatureSeries(r, runtime); !slices.Equal(got, runtime) {
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
	if got, want := temperatureSeries(r, runtime), []float64{35, 36}; !slices.Equal(got, want) {
		t.Errorf("series = %v, want the SCT log %v", got, want)
	}
	if strings.Contains(legend(r), polls) {
		t.Error("legend claims a runtime trend for a drive with a usable SCT log")
	}

	if got, _ := sctSeries(sctReport(t, `[35,-128,36,255]`)); !slices.Equal(got, []float64{35, 36}) {
		t.Errorf("series = %v, want the implausible slots dropped", got)
	}
}
