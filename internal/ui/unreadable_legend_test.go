// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/smart"
)

// legendFixtures make every fleet section available.
var legendFixtures = []string{"smart-sda.json", "smart-sdb.json", "smart-nvme.json"}

// legendRows renders each available section at width and returns the legend's wrapped line count per section.
func legendRows(t *testing.T, width int, asleep, unreadable bool) map[string]int {
	t.Helper()
	a, _ := newSimApp(t, width, 40)
	v := a.fleet
	rows := make([]fleetRow, 0, len(legendFixtures))
	for _, f := range legendFixtures {
		rep := loadReport(t, f)
		rows = append(rows, fleetRow{dev: rep.Device, rep: rep, series: temperatureSeries(rep, nil)})
	}
	rows[0].asleep = asleep
	rows[1].unreadable = unreadable
	v.rows = rows
	v.lastWidth = width

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("simulation screen: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, 40)

	out := map[string]int{}
	for _, sec := range v.availableSections() {
		v.activeID = sec.id
		v.render()
		v.legend.SetRect(0, 0, width, 40)
		v.legend.Draw(screen)
		out[sec.id] = v.legend.GetWrappedLineCount()
	}
	if len(out) != len(v.sections) {
		t.Fatalf("%d of %d sections available; the fixtures must cover them all", len(out), len(v.sections))
	}
	return out
}

// TestStandbyLegendIsUnchanged pins the baseline the unreadable forms are measured against.
func TestStandbyLegendIsUnchanged(t *testing.T) {
	const want = "◌ spun down; values as of the last read"
	if got := staleLegend([]fleetRow{{asleep: true, rep: &smart.Report{}}}); got != want {
		t.Errorf("standby legend = %q, want %q", got, want)
	}
	if got := staleLegend([]fleetRow{{rep: &smart.Report{}}}); got != "" {
		t.Errorf("legend prefix with nothing stale = %q, want none", got)
	}
}

// TestUnreadableLegendIsNoTallerThanStandby: the legend is two rows, so a longer prefix clips the section's caveat.
func TestUnreadableLegendIsNoTallerThanStandby(t *testing.T) {
	for _, width := range []int{80, 100, 120, 140} {
		plain := legendRows(t, width, false, false)
		standby := legendRows(t, width, true, false)
		forms := []struct {
			name string
			rows map[string]int
		}{
			{"unreadable", legendRows(t, width, false, true)},
			{"asleep+unreadable", legendRows(t, width, true, true)},
		}
		for sec, bound := range standby {
			var got []string
			for _, f := range forms {
				if f.rows[sec] > bound {
					t.Errorf("%d columns, %s: %s legend wraps to %d rows, standby alone to %d",
						width, sec, f.name, f.rows[sec], bound)
				}
				got = append(got, f.name+"="+string(rune('0'+f.rows[sec])))
			}
			t.Logf("%3d cols %-12s none=%d standby=%d %s", width, sec, plain[sec], bound, strings.Join(got, " "))
		}
	}
}

// TestBothStaleMarksShareOneLegendLine: two prefixes stacked would cost the caveat a second clause of width.
func TestBothStaleMarksShareOneLegendLine(t *testing.T) {
	got := staleLegend([]fleetRow{{asleep: true}, {unreadable: true}})
	if !strings.Contains(got, standbyGlyph) || !strings.Contains(got, staleGlyph) {
		t.Errorf("merged legend %q does not explain both marks", got)
	}
	if strings.Contains(got, " · ") {
		t.Errorf("merged legend %q is not a single clause", got)
	}
}
