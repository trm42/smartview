// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// statisticsView renders the Statistics tab: the ATA Device Statistics log (GP
// Log 0x04), a set of vendor-neutral counters grouped into named pages. It is a
// single scrollable TextView — the page sections vary in length and the whole
// log can outgrow the viewport — and refreshes its text in place, preserving the
// scroll offset across polls.
type statisticsView struct {
	*scrollTextView

	// Latest text and the width it was wrapped for. Values are laid out against
	// the live column width — the same lazy relayout farm.go and overview.go use
	// — because tview's own wrapping breaks a value back to column 0, splitting
	// "15.4 TB  (30003609491 sectors)" across two lines and the two-column grid
	// with it. -1 forces a rebuild.
	raw       string
	lastWidth int
}

func newStatisticsView(r *smart.Report) *statisticsView {
	v := &statisticsView{scrollTextView: newScrollTextView(), lastWidth: -1}
	v.SetDynamicColors(true).SetScrollable(true).SetWrap(false)
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
	v.raw = buildStatisticsText(r)
	v.lastWidth = -1 // data changed: re-wrap against the current width
}

// Draw re-wraps the body when the width changed (or a refresh invalidated it),
// then defers to the scrolling TextView.
func (v *statisticsView) Draw(screen tcell.Screen) {
	if _, _, w, _ := v.GetInnerRect(); w != v.lastWidth {
		row, col := v.GetScrollOffset()
		v.SetText(hangingIndentStats(v.raw, w))
		v.ScrollTo(row, col)
		v.lastWidth = w
	}
	v.scrollTextView.Draw(screen)
}

// statValueCol is the column the values start in: nestIndent plus the
// statLabelWidth label plus a space.
const statValueCol = len(nestIndent) + statLabelWidth + 1

// hangingIndentStats re-wraps any line too long for innerW so the overflow hangs
// under the value column instead of returning to the left margin. tview's own
// wrapping is left enabled for the odd line this cannot split.
func hangingIndentStats(text string, innerW int) string {
	valueW := innerW - statValueCol
	if valueW <= 8 || text == "" {
		return text
	}
	indent := strings.Repeat(" ", statValueCol)
	var out strings.Builder
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		if tview.TaggedStringWidth(line) <= innerW || len(line) <= statValueCol {
			out.WriteString(line)
			continue
		}
		out.WriteString(line[:statValueCol])
		col := 0
		for w, word := range strings.Fields(line[statValueCol:]) {
			width := tview.TaggedStringWidth(word)
			switch {
			case w == 0:
			case col+1+width <= valueW:
				out.WriteString(" ")
				col++
			default:
				out.WriteString("\n" + indent)
				col = 0
			}
			out.WriteString(word)
			col += width
		}
	}
	return out.String()
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
	// "Logical Sectors *" counts are in logical-block units, which are 4096 B on
	// a 4Kn drive — use the reported size, falling back to the 512 B common case.
	sectorBytes := int64(512)
	if r.LogicalBlockSize != nil && *r.LogicalBlockSize > 0 {
		sectorBytes = int64(*r.LogicalBlockSize)
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
			fmt.Fprintf(&b, nestIndent+"%-*s %s\n", statLabelWidth, esc(e.Name), statValue(p, e, sectorBytes))
		}
	}
	return b.String()
}

// statLabelWidth is the counter-name column. smartctl's names are long
// ("Number of Realloc. Candidate Logical Sectors" is 44), and this is the width
// that keeps their values aligned down the page.
const statLabelWidth = 46

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

// statValue formats one statistic, tinting health-relevant counters when set.
// Well-known counters lead with the human-readable form (a duration, capacity or
// temperature) and keep the exact raw counter in gray — this is the detailed
// view, so the precise number stays available without dominating the column.
func statValue(p smart.ATAStatPage, e smart.ATAStatEntry, sectorBytes int64) string {
	var val string
	switch {
	case strings.HasSuffix(e.Name, "Hours"):
		// Any "* Hours" counter (Power-on, Spindle Motor Power-on, Head Flying).
		val = fmt.Sprintf("%s  %s(%d h)[-]", humanDuration(int(e.Value)), mutedTag(), e.Value)
	case e.Name == "Logical Sectors Written" || e.Name == "Logical Sectors Read":
		val = fmt.Sprintf("%s  %s(%d sectors)[-]", humanBytes(e.Value*sectorBytes), mutedTag(), e.Value)
	case isTemperatureStat(p, e):
		val = fmt.Sprintf("%d°C", e.Value)
	default:
		val = fmt.Sprintf("%d", e.Value)
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
