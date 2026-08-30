// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/smart"
)

// statisticsView renders the ATA Device Statistics log as one scrollable
// TextView, refreshing in place so the scroll offset survives polls.
type statisticsView struct {
	*scrollTextView

	// Width-aware lazy relayout, same pattern as farm.go/overview.go: values
	// are pre-wrapped for the live width. lastWidth -1 forces a rebuild.
	raw       string
	lastWidth int
}

func newStatisticsView(r *smart.Report) *statisticsView {
	v := &statisticsView{scrollTextView: newScrollTextView(), lastWidth: -1}
	v.SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	titledBox(v.Box, " Device statistics ")
	v.refresh(r, nil)
	return v
}

// setFocused accents the Statistics tab's border when it holds keyboard focus.
func (v *statisticsView) setFocused(focused bool) {
	v.SetBorderColor(borderColor(focused))
}

// refresh re-renders the text and invalidates the width for a re-wrap.
func (v *statisticsView) refresh(r *smart.Report, _ []float64) {
	v.raw = buildStatisticsText(r)
	v.lastWidth = -1 // data changed: re-wrap against the current width
}

// Draw re-wraps when the width changed (or a refresh invalidated it).
func (v *statisticsView) Draw(screen tcell.Screen) {
	if _, _, w, _ := v.GetInnerRect(); w != v.lastWidth {
		v.setTextKeepingScroll(hangingIndent(v.raw, statWrap, w))
		v.lastWidth = w
	}
	v.scrollTextView.Draw(screen)
}

// buildStatisticsText assembles the body: one section per page, valid entries
// only, health-relevant counters tinted when nonzero.
func buildStatisticsText(r *smart.Report) string {
	var b strings.Builder
	if r.ATADeviceStatistics == nil {
		return ""
	}
	// "Logical Sectors *" counts are in logical-block units (4096 B on 4Kn).
	sectorBytes := r.SectorBytes()
	first := true
	for _, p := range r.ATADeviceStatistics.Pages {
		valid := validStatEntries(p.Table)
		if len(valid) == 0 {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		sectionHeader(&b, orDash(esc(p.Name)))
		for _, e := range valid {
			fmt.Fprintf(&b, nestIndent+"%-*s %s\n", statLabelWidth, esc(e.Name), statValue(p, e, sectorBytes))
		}
	}
	return b.String()
}

// statValueCol is the column values start in.
const statValueCol = len(nestIndent) + statLabelWidth + 1

// statWrap: counter names are long and the values short, so a value column
// under nine cells means the panel is already too narrow to read.
var statWrap = hangingWrap{valueCol: statValueCol, minValueW: 9}

// statLabelWidth fits smartctl's longest counter names (up to 44 chars).
const statLabelWidth = 46

// validStatEntries drops placeholder rows (valid=false).
func validStatEntries(table []smart.ATAStatEntry) []smart.ATAStatEntry {
	out := make([]smart.ATAStatEntry, 0, len(table))
	for _, e := range table {
		if e.Flags.Valid {
			out = append(out, e)
		}
	}
	return out
}

// statValue formats one statistic: well-known counters lead with the
// human-readable form and keep the raw counter in gray; health-relevant ones
// tint when set.
func statValue(p smart.ATAStatPage, e smart.ATAStatEntry, sectorBytes int64) string {
	var val string
	switch {
	case strings.HasSuffix(e.Name, "Hours"):
		val = fmt.Sprintf("%s  %s(%d h)[-]", humanDuration(int(e.Value)), mutedTag(), e.Value)
	case e.Name == "Logical Sectors Written" || e.Name == "Logical Sectors Read":
		val = fmt.Sprintf("%s  %s(%d sectors)[-]", humanBytes(e.Value*sectorBytes), mutedTag(), e.Value)
	case isTemperatureStat(p, e):
		val = fmt.Sprintf("%d°C", e.Value)
	default:
		val = fmt.Sprintf("%d", e.Value)
	}
	if sev := statSeverity(e); sev != smart.SeverityOK && e.Value > 0 {
		return sevText(sev, val)
	}
	return val
}

// isTemperatureStat reports whether an entry is a Celsius reading (vs a
// duration like "Time in Over-Temperature").
func isTemperatureStat(p smart.ATAStatPage, e smart.ATAStatEntry) bool {
	return p.Number == 5 && strings.Contains(e.Name, "Temperature") &&
		!strings.Contains(e.Name, "Time in")
}

// statSeverity grades the health-relevant counters.
func statSeverity(e smart.ATAStatEntry) smart.Severity {
	switch e.Name {
	case "Percentage Used Endurance Indicator":
		return smart.PctUsedSeverity(int(e.Value))
	case "Number of Reallocated Sectors",
		"Number of Reallocated Logical Sectors",
		"Number of Realloc. Candidate Logical Sectors",
		"Number of Reported Uncorrectable Errors",
		"Number of Interface CRC Errors",
		"Number of Mechanical Start Failures":
		return smart.SeverityCaution
	}
	return smart.SeverityOK
}
