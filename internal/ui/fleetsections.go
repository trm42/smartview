// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// This file defines what the fleet view compares. fleet.go owns the widget and
// the interaction; a section here is pure data — which columns exist, how one
// drive's row renders, how the rows sort, and what caveat the legend must carry.
// Adding a comparison means adding a fleetSection, not touching the widget.

// fleetRow is one drive's input to a section: the device, its latest report
// (nil while the first scan is still running) and its temperature series.
type fleetRow struct {
	dev    smart.Device
	rep    *smart.Report
	series []float64
}

// fleetCell is one rendered table cell. Text may carry intentional tview markup
// (a severity glyph, a coloured bar); drive-controlled data inside it must
// already be escaped by the producer.
type fleetCell struct {
	text  string
	color tcell.Color
	align int
}

// fleetSection is one comparison: a focus metric plus the columns that support
// reading it across drives.
type fleetSection struct {
	id, title string
	columns   []string
	// available reports whether any drive supplies this section's data. A
	// section that no drive can fill is hidden entirely, the same
	// capability-driven rule the detail tabs follow (see visibleTabs).
	available func(rows []fleetRow) bool
	// cells renders one drive's row; it must return one cell per column.
	cells func(row fleetRow) []fleetCell
	// rank is the sort key, descending. ok=false sorts the drive last, so
	// drives that cannot report the focus metric fall to the bottom rather
	// than pretending to a zero.
	rank func(row fleetRow) (float64, bool)
	// legend explains this section's gaps and caveats, given the actual rows.
	legend func(rows []fleetRow) string
}

// fleetSections returns the comparison sections in display order.
func fleetSections() []fleetSection {
	return []fleetSection{
		temperatureSection(),
		healthSection(),
		enduranceSection(),
		ageSection(),
	}
}

// --- Temperature -------------------------------------------------------------

// temperatureSection compares running temperature. It is the one metric every
// drive type reports, which is why it leads.
func temperatureSection() fleetSection {
	return fleetSection{
		id:      "temperature",
		title:   "Temperature",
		columns: []string{"Now", "Min", "Max", "Trend"},
		available: func(rows []fleetRow) bool {
			return anyRow(rows, func(r *smart.Report) bool { _, ok := r.CurrentTemp(); return ok })
		},
		rank: func(row fleetRow) (float64, bool) {
			if row.rep == nil {
				return 0, false
			}
			t, ok := row.rep.CurrentTemp()
			return float64(t), ok
		},
		cells: func(row fleetRow) []fleetCell {
			r := row.rep
			now := fleetCell{text: dash, color: activeTheme.Neutral, align: tview.AlignRight}
			sev := smart.SeverityOK
			if t, ok := r.CurrentTemp(); ok {
				sev = tempSeverity(t)
				now = sevCell(fmt.Sprintf("%d°C", t), sev)
			}

			lo, hi, ok := r.TempRange()
			if !ok {
				// NVMe reports no range; derive one from the trend window.
				lo, hi, ok = seriesRange(row.series)
			}
			minCell, maxCell := numCell(dash), numCell(dash)
			if ok {
				minCell = numCell(fmt.Sprintf("%d", lo))
				maxCell = numCell(fmt.Sprintf("%d", hi))
			}

			trend := fleetCell{text: dash, color: activeTheme.Neutral}
			if s := sparkString(row.series, sparkWidth); s != "" {
				trend = fleetCell{text: s, color: severityColor(sev)}
			}
			return []fleetCell{now, minCell, maxCell, trend}
		},
		legend: func(rows []fleetRow) string {
			s := "min/max is the drive's lifetime range where it reports one (ATA); " +
				"otherwise the range observed in the trend window"
			if anyRow(rows, func(r *smart.Report) bool {
				return r.ATATemperatureHistory == nil || len(r.ATATemperatureHistory.Table) < 2
			}) {
				s += " · drives without an on-device log build their trend over successive polls"
			}
			return s
		},
	}
}

// --- Health & errors ---------------------------------------------------------

// healthSection compares the verdict and the error counters behind it. ATA and
// NVMe expose different counters, so roughly half of every row is legitimately
// blank; the legend says so rather than leaving it to look broken.
func healthSection() fleetSection {
	return fleetSection{
		id:      "health",
		title:   "Health & errors",
		columns: []string{"Verdict", "Realloc", "Pending", "Uncorr", "CRC", "Media", "Err log", "Unsafe"},
		available: func(rows []fleetRow) bool {
			return anyRow(rows, func(*smart.Report) bool { return true })
		},
		rank: func(row fleetRow) (float64, bool) {
			if row.rep == nil {
				return 0, false
			}
			// Worst verdict first, then by how many errors are logged, so two
			// drives at the same verdict still order meaningfully.
			return float64(row.rep.Overall())*1e9 + float64(totalErrors(row.rep)), true
		},
		cells: func(row fleetRow) []fleetCell {
			r := row.rep
			sev := r.Overall()
			verdict := fleetCell{
				text:  fmt.Sprintf("[%s::b]%s[-:-:-]", severityTag(sev), verdictWord(sev)),
				color: activeTheme.Neutral,
			}
			e := r.ErrorCounts()
			return []fleetCell{
				verdict,
				counterCell(e.Reallocated, smart.SeverityCaution),
				counterCell(e.Pending, smart.SeverityCaution),
				counterCell(e.Uncorrectable, smart.SeverityCaution),
				// Interface CRC errors are a cabling fault, not drive wear, and
				// unsafe shutdowns are host-side — neither grades the drive, so
				// both stay neutral even when nonzero.
				counterCell(e.CRCErrors, smart.SeverityOK),
				counterCell(e.MediaErrors, smart.SeverityCaution),
				counterCell(e.ErrorLogEntries, smart.SeverityOK),
				counterCell(e.UnsafeShutdowns, smart.SeverityOK),
			}
		},
		legend: func([]fleetRow) string {
			return "Realloc/Pending/Uncorr/CRC are ATA counters, Media/Unsafe are NVMe; " +
				dash + " means this drive does not report that counter (not zero)"
		},
	}
}

// counterCell renders an optional error counter: absent stays a dash, zero reads
// neutral, and a nonzero count takes the given severity.
func counterCell(v *int64, nonzero smart.Severity) fleetCell {
	if v == nil {
		return numCell(dash)
	}
	if *v == 0 {
		return numCell("0")
	}
	return sevCell(fmt.Sprintf("%d", *v), nonzero)
}

// totalErrors sums the counters a drive actually reports, for sort tie-breaking.
func totalErrors(r *smart.Report) int64 {
	e := r.ErrorCounts()
	var n int64
	for _, c := range []*int64{e.Reallocated, e.Pending, e.Uncorrectable, e.MediaErrors, e.ErrorLogEntries} {
		if c != nil {
			n += *c
		}
	}
	return n
}

// --- Endurance & wear --------------------------------------------------------

// enduranceSection compares write wear. Spinning disks have no endurance
// indicator, so an all-HDD machine never sees this section at all.
func enduranceSection() fleetSection {
	return fleetSection{
		id:      "endurance",
		title:   "Endurance & wear",
		columns: []string{"Kind", "Life left", "Spare", "Written", "Per day"},
		available: func(rows []fleetRow) bool {
			return anyRow(rows, func(r *smart.Report) bool {
				if _, ok := r.LifeUsedPercent(); ok {
					return true
				}
				if _, _, ok := r.SparePercent(); ok {
					return true
				}
				_, ok := r.DataWritten()
				return ok
			})
		},
		rank: func(row fleetRow) (float64, bool) {
			if row.rep == nil {
				return 0, false
			}
			pct, ok := row.rep.LifeUsedPercent()
			return float64(pct), ok
		},
		cells: func(row fleetRow) []fleetCell {
			r := row.rep
			life := fleetCell{text: dash, color: activeTheme.Neutral}
			if pct, ok := r.LifeUsedPercent(); ok {
				// pctBarUsed drains as the drive wears, so this bar and the spare
				// bar beside it fill in the same direction; the number stays the
				// "percentage used" figure the drive reports.
				life = fleetCell{text: pctBarUsed(pct, lifeUsedSeverity(pct)), color: activeTheme.Neutral}
			}
			spare := fleetCell{text: dash, color: activeTheme.Neutral}
			if pct, _, ok := r.SparePercent(); ok {
				spare = fleetCell{text: pctBar(pct, spareSeverity(r.NVMeHealth)), color: activeTheme.Neutral}
			}

			written, perDay := numCell(dash), numCell(dash)
			if w, ok := r.DataWritten(); ok {
				written = writeCell(humanBytes(w.Bytes), w.Approximate())
				if hours, ok := r.PowerOnHours(); ok && hours >= 24 {
					bytesPerDay := int64(float64(w.Bytes) / (float64(hours) / 24))
					perDay = writeCell(humanBytes(bytesPerDay), w.Approximate())
				}
			}
			return []fleetCell{cell(shortKind(r)), life, spare, written, perDay}
		},
		legend: func(rows []fleetRow) string {
			s := "bars fill toward healthy · " + dash +
				" means the drive reports no endurance indicator (normal for spinning disks)"
			if anyRow(rows, func(r *smart.Report) bool {
				w, ok := r.DataWritten()
				return ok && w.Approximate()
			}) {
				s += " · " + cautionTag() + "~[-] estimated from vendor attribute 241 " +
					"(Total_LBAs_Written), whose unit is vendor-defined and may be wrong"
			}
			return s
		},
	}
}

// writeCell renders a write total, marking an attribute-derived estimate with a
// tilde and the caution colour so it is never mistaken for a comparable figure.
func writeCell(text string, approximate bool) fleetCell {
	if approximate {
		return fleetCell{
			text:  cautionTag() + "~" + text + "[-]",
			color: activeTheme.Neutral,
			align: tview.AlignRight,
		}
	}
	return numCell(text)
}

// --- Age & usage -------------------------------------------------------------

// ageSection compares how much service each drive has seen. Power-on time and
// power cycles are present on every drive, making this the one section with no
// protocol gaps.
func ageSection() fleetSection {
	return fleetSection{
		id:      "age",
		title:   "Age & usage",
		columns: []string{"Kind", "Capacity", "Power-on", "Cycles", "Hrs/cycle"},
		available: func(rows []fleetRow) bool {
			return anyRow(rows, func(r *smart.Report) bool { _, ok := r.PowerOnHours(); return ok })
		},
		rank: func(row fleetRow) (float64, bool) {
			if row.rep == nil {
				return 0, false
			}
			h, ok := row.rep.PowerOnHours()
			return float64(h), ok
		},
		cells: func(row fleetRow) []fleetCell {
			r := row.rep
			powerOn, cycles, perCycle := numCell(dash), numCell(dash), numCell(dash)
			hours, hasHours := r.PowerOnHours()
			if hasHours {
				powerOn = numCell(humanDuration(hours))
			}
			if n, ok := r.PowerCycles(); ok {
				cycles = numCell(fmt.Sprintf("%d", n))
				if hasHours && n > 0 {
					perCycle = numCell(fmt.Sprintf("%d h", hours/n))
				}
			}
			return []fleetCell{
				cell(shortKind(r)),
				numCell(capacityString(r)),
				powerOn,
				cycles,
				perCycle,
			}
		},
		legend: func([]fleetRow) string {
			return "Hrs/cycle is average power-on time per power cycle — a low value on a " +
				"high-cycle drive suggests frequent sleep/wake rather than sustained use"
		},
	}
}

// --- shared helpers ----------------------------------------------------------

// sparkWidth is the cell width of the temperature trend column.
const sparkWidth = 16

// sparkLevels are the eighth-block glyphs the trend column is drawn from.
var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// cell is a plain left-aligned cell in the neutral text colour.
func cell(text string) fleetCell {
	return fleetCell{text: text, color: activeTheme.Neutral}
}

// numCell is a right-aligned cell, so figures line up down a column.
func numCell(text string) fleetCell {
	return fleetCell{text: text, color: activeTheme.Neutral, align: tview.AlignRight}
}

// sevCell is a right-aligned cell tinted by severity, healthy rows staying
// neutral so only the readings needing attention draw the eye.
func sevCell(text string, sev smart.Severity) fleetCell {
	return fleetCell{text: text, color: attrTextColor(sev), align: tview.AlignRight}
}

// anyRow reports whether any drive with a report satisfies pred.
func anyRow(rows []fleetRow, pred func(*smart.Report) bool) bool {
	for _, row := range rows {
		if row.rep != nil && pred(row.rep) {
			return true
		}
	}
	return false
}

// shortKind is driveKind compressed for a table column ("HDD" rather than
// "HDD @ 7200 rpm"), since the fleet table trades detail for width.
func shortKind(r *smart.Report) string {
	switch {
	case r.IsNVMe():
		return "NVMe"
	case r.RotationRate != nil && *r.RotationRate > 0:
		return "HDD"
	case r.IsATA():
		return "SSD"
	default:
		return esc(r.Device.Protocol)
	}
}

// seriesRange returns the extremes of an observed temperature series. A single
// sample has no range to report — showing min = max = the current reading would
// look like a lifetime range the drive never gave us — so it reports absent
// until a second poll lands.
func seriesRange(series []float64) (min, max int, ok bool) {
	if len(series) < 2 {
		return 0, 0, false
	}
	lo, hi := series[0], series[0]
	for _, v := range series {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return int(lo), int(hi), true
}

// sparkString renders a series as a text sparkline of at most width glyphs,
// bucket-averaging when there are more samples than cells. A text sparkline (as
// opposed to a sparkline widget per drive) is what lets the whole comparison be
// one selectable, scrollable table.
//
// The scale is relative to the series' own range, so the shape of each drive's
// trend is readable even when drives sit at very different temperatures; the
// numeric columns beside it carry the absolute values.
func sparkString(data []float64, width int) string {
	if len(data) < 2 || width <= 0 {
		return ""
	}
	n := min(width, len(data))
	buckets := make([]float64, n)
	for i := range buckets {
		lo := i * len(data) / n
		hi := (i + 1) * len(data) / n
		if hi <= lo {
			hi = lo + 1
		}
		var sum float64
		for _, v := range data[lo:hi] {
			sum += v
		}
		buckets[i] = sum / float64(hi-lo)
	}

	lo, hi := buckets[0], buckets[0]
	for _, v := range buckets {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	out := make([]rune, n)
	span := hi - lo
	for i, v := range buckets {
		// A flat series has no range to scale against; draw it mid-height so it
		// reads as steady rather than as an arbitrary floor or ceiling.
		level := len(sparkLevels) / 2
		if span > 0 {
			level = int((v-lo)/span*float64(len(sparkLevels)-1) + 0.5)
		}
		out[i] = sparkLevels[level]
	}
	return string(out)
}
