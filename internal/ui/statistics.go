// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/trm42/smartview/internal/smart"
)

// statisticsView renders the Statistics tab: the ATA Device Statistics log (GP
// Log 0x04), a set of vendor-neutral counters grouped into named pages. It is a
// single scrollable TextView — the page sections vary in length and the whole
// log can outgrow the viewport — and refreshes its text in place, preserving the
// scroll offset across polls.
type statisticsView struct {
	*scrollTextView
}

func newStatisticsView(r *smart.Report) *statisticsView {
	v := &statisticsView{newScrollTextView()}
	v.SetDynamicColors(true).SetScrollable(true)
	v.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" Device statistics ")
	v.refresh(r, nil)
	return v
}

// setFocused accents the Statistics tab's border when it holds keyboard focus.
func (v *statisticsView) setFocused(focused bool) {
	v.SetBorderColor(borderColor(focused))
}

// refresh re-renders the statistics text, restoring the prior scroll offset so a
// poll does not jump the view back to the top.
func (v *statisticsView) refresh(r *smart.Report, _ []float64) {
	row, col := v.GetScrollOffset()
	v.SetText(buildStatisticsText(r))
	v.ScrollTo(row, col)
}

// buildStatisticsText assembles the Statistics tab body: one bold-headed section
// per page, listing only valid entries. Health-relevant counters tint when
// nonzero; a few well-known counters get a human-readable form alongside the raw
// value.
func buildStatisticsText(r *smart.Report) string {
	var b strings.Builder
	if r.ATADeviceStatistics == nil {
		return ""
	}
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
		fmt.Fprintf(&b, "[::b]%s[-:-:-]\n", orDash(esc(p.Name)))
		for _, e := range valid {
			fmt.Fprintf(&b, nestIndent+"%-46s %s\n", esc(e.Name), statValue(p, e))
		}
	}
	return b.String()
}

// validStatEntries returns the entries whose value is meaningful (smartctl marks
// placeholder rows such as timestamps with valid=false).
func validStatEntries(table []smart.ATAStatEntry) []smart.ATAStatEntry {
	out := make([]smart.ATAStatEntry, 0, len(table))
	for _, e := range table {
		if e.Flags.Valid {
			out = append(out, e)
		}
	}
	return out
}

// statValue formats one statistic, tinting health-relevant counters when set and
// appending a human-readable form for a few well-known names.
func statValue(p smart.ATAStatPage, e smart.ATAStatEntry) string {
	val := fmt.Sprintf("%d", e.Value)
	switch {
	case e.Name == "Power-on Hours":
		val += fmt.Sprintf("  [gray](%s)[-]", humanDuration(int(e.Value)))
	case e.Name == "Logical Sectors Written" || e.Name == "Logical Sectors Read":
		val += fmt.Sprintf("  [gray](%s)[-]", humanBytes(e.Value*512))
	case isTemperatureStat(p, e):
		val = fmt.Sprintf("%d°C", e.Value)
	}
	if sev := statSeverity(e); sev != smart.SeverityOK && e.Value > 0 {
		return fmt.Sprintf("[%s]%s[-]", severityTag(sev), val)
	}
	return val
}

// isTemperatureStat reports whether an entry on the Temperature Statistics page
// is a degrees-Celsius reading (vs a duration like "Time in Over-Temperature").
func isTemperatureStat(p smart.ATAStatPage, e smart.ATAStatEntry) bool {
	return p.Number == 5 && strings.Contains(e.Name, "Temperature") &&
		!strings.Contains(e.Name, "Time in")
}

// statSeverity grades the health-relevant Device Statistics counters: any
// nonzero reallocation, uncorrectable-error or interface-error count is a
// caution, and the SSD endurance indicator escalates as it nears its rated life.
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
