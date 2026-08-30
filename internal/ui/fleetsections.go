// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"slices"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// What the fleet view compares. fleet.go owns the widget; a section here is
// pure data. Adding a comparison means adding a fleetSection, not touching
// the widget.

// fleetRow is one drive's input to a section; rep is nil while the first scan
// is still running.
type fleetRow struct {
	dev    smart.Device
	rep    *smart.Report
	series []float64
}

// fleetCell is one rendered cell; text may carry intentional markup, and
// drive-controlled data in it must already be escaped by the producer.
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
	// available: any drive supplies this section's data; hidden otherwise
	// (same capability rule as the detail tabs).
	available func(rows []fleetRow) bool
	// cells renders one drive's row, one cell per column.
	cells func(row fleetRow) []fleetCell
	// rank is the sort key, descending; ok=false sorts the drive last rather
	// than pretending to a zero.
	rank func(row fleetRow) (float64, bool)
	// legend explains this section's gaps and caveats.
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

// temperatureSection compares running temperature — the one metric every
// drive type reports, so it leads.
func temperatureSection() fleetSection {
	return fleetSection{
		id:      "temperature",
		title:   "Temperature",
		columns: []string{"Now", "Min", "Max", "Trend"},
		available: func(rows []fleetRow) bool {
			return anyRow(rows, func(r *smart.Report) bool { _, ok := r.CurrentTemp(); return ok })
		},
		rank: rankBy((*smart.Report).CurrentTemp),
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

// healthSection compares the verdict and the error counters behind it. ATA
// and NVMe expose different counters, so half of every row is legitimately
// blank; the legend says so.
func healthSection() fleetSection {
	return fleetSection{
		id:      "health",
		title:   "Health & errors",
		columns: []string{"State", "Realloc", "Pending", "Uncorr", "CRC", "Media", "Err log", "Unsafe"},
		available: func(rows []fleetRow) bool {
			return anyRow(rows, func(*smart.Report) bool { return true })
		},
		rank: func(row fleetRow) (float64, bool) {
			if row.rep == nil {
				return 0, false
			}
			// Worst verdict first, then by logged errors as the tiebreaker.
			return float64(row.rep.Overall())*1e9 + float64(totalErrors(row.rep)), true
		},
		cells: func(row fleetRow) []fleetCell {
			r := row.rep
			sev := r.Overall()
			verdict := fleetCell{
				text:  sevBold(sev, verdictWord(sev)),
				color: activeTheme.Neutral,
			}
			e := r.ErrorCounts()
			return []fleetCell{
				verdict,
				counterCell(e.Reallocated, smart.SeverityCaution),
				counterCell(e.Pending, smart.SeverityCaution),
				counterCell(e.Uncorrectable, smart.SeverityCaution),
				// CRC errors are cabling, unsafe shutdowns host-side; neither
				// grades the drive, so both stay neutral.
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

// counterCell renders an optional counter: absent stays a dash, zero reads
// neutral, nonzero takes the given severity.
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

// enduranceSection compares write wear; an all-HDD machine never sees it.
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
		rank: rankBy((*smart.Report).LifeUsedPercent),
		cells: func(row fleetRow) []fleetCell {
			r := row.rep
			life := fleetCell{text: dash, color: activeTheme.Neutral}
			if pct, ok := r.LifeUsedPercent(); ok {
				// pctBarUsed drains as the drive wears, matching the spare bar's polarity.
				life = fleetCell{text: pctBarUsed(pct, lifeUsedSeverity(pct)), color: activeTheme.Neutral}
			}
			spare := fleetCell{text: dash, color: activeTheme.Neutral}
			if pct, thr, ok := r.SparePercent(); ok {
				// Grade from the pair SparePercent resolved, not from NVMeHealth:
				// the non-NVMe source reports spare with NVMeHealth nil.
				spare = fleetCell{text: pctBar(pct, spareSeverityPct(pct, thr)), color: activeTheme.Neutral}
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

// writeCell renders a write total, marking an attribute-derived estimate with
// a caution-coloured tilde.
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

// ageSection compares service seen; the one section with no protocol gaps.
func ageSection() fleetSection {
	return fleetSection{
		id:      "age",
		title:   "Age & usage",
		columns: []string{"Kind", "Capacity", "Power-on", "Cycles", "Hrs/cycle"},
		available: func(rows []fleetRow) bool {
			return anyRow(rows, func(r *smart.Report) bool { _, ok := r.PowerOnHours(); return ok })
		},
		rank: rankBy((*smart.Report).PowerOnHours),
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

// cell is a plain left-aligned cell in the neutral text colour.
func cell(text string) fleetCell {
	return fleetCell{text: text, color: activeTheme.Neutral}
}

// numCell is a right-aligned cell, so figures line up down a column.
func numCell(text string) fleetCell {
	return fleetCell{text: text, color: activeTheme.Neutral, align: tview.AlignRight}
}

// sevCell is a right-aligned cell tinted by severity; healthy stays neutral.
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

// rankBy turns a metric accessor into a section rank: absent readings sort
// last rather than pretending to a zero, and the nil-report guard lives here
// instead of being repeated (and forgotten) per section.
func rankBy(f func(*smart.Report) (int, bool)) func(fleetRow) (float64, bool) {
	return func(row fleetRow) (float64, bool) {
		if row.rep == nil {
			return 0, false
		}
		v, ok := f(row.rep)
		return float64(v), ok
	}
}

// shortKind is driveKind compressed for a table column.
func shortKind(r *smart.Report) string { return kindLabel(r, shortKindLabels) }

// seriesRange returns the extremes of an observed series; a single sample has
// no range and reports absent until a second poll lands.
func seriesRange(series []float64) (lo, hi int, ok bool) {
	if len(series) < 2 {
		return 0, 0, false
	}
	f, g, _ := dataRange(series) // len >= 2, so ok is never false here
	return int(f), int(g), true
}

// sparkString renders a series as a text sparkline (so the comparison can be
// one selectable table), reducing through chart.go's downsample when there are
// more samples than cells. The scale is relative to the series' own range; the
// numeric columns carry the absolute values.
func sparkString(data []float64, width int) string {
	if len(data) < 2 || width <= 0 {
		return ""
	}
	buckets := downsample(data, width)
	n := len(buckets)

	lo, hi := slices.Min(buckets), slices.Max(buckets)
	out := make([]rune, n)
	span := hi - lo
	for i, v := range buckets {
		// A flat series draws mid-height so it reads as steady.
		level := len(blockRamp) / 2
		if span > 0 {
			level = int((v-lo)/span*float64(len(blockRamp)-1) + 0.5)
		}
		out[i] = blockRamp[level]
	}
	return string(out)
}
