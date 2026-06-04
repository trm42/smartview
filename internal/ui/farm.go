// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"

	"smartview/internal/smart"
)

// buildFarm renders the FARM tab: a scrollable panel of Seagate Field Accessible
// Reliability Metrics (drive/wear summary, health-graded error counters,
// environment and workload totals) above per-head bar charts.
func buildFarm(r *smart.Report) tview.Primitive {
	f := r.FARM
	if f == nil {
		return centeredNote("No FARM log available.")
	}

	root := tview.NewFlex().SetDirection(tview.FlexRow)

	stats := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	stats.SetBorder(true).SetTitle(" Seagate FARM ")
	var b strings.Builder
	writeFarmDriveInfo(&b, f)
	b.WriteString("\n")
	writeFarmErrors(&b, f)
	b.WriteString("\n")
	writeFarmEnvironment(&b, f)
	b.WriteString("\n")
	writeFarmWorkload(&b, f)
	stats.SetText(b.String())
	root.AddItem(stats, 0, 1, true)

	// Per-head visualizations. Reallocated sectors per head is the health
	// red-flag (flat zero on a healthy drive); MR head resistance always varies
	// and surfaces an outlier head.
	if c := farmHeadChart(" Reallocated sectors / head ", f.Reliability.ReallocatedByHead, true); c != nil {
		root.AddItem(c, 9, 0, false)
	}
	if c := farmHeadChart(" MR head resistance / head ", f.Reliability.MRHeadResistance, false); c != nil {
		root.AddItem(c, 9, 0, false)
	}
	return root
}

// writeFarmDriveInfo renders the drive/wear summary block.
func writeFarmDriveInfo(b *strings.Builder, f *smart.FARM) {
	d := f.DriveInfo
	fmt.Fprintln(b, " [::b]Drive[-:-:-]")
	farmRow(b, "Recording", orDash(d.RecordingType))
	if d.RotationRate > 0 {
		farmRow(b, "Spindle", fmt.Sprintf("%d rpm", d.RotationRate))
	}
	farmRow(b, "Heads", fmt.Sprintf("%d", d.Heads))
	farmRow(b, "Power-on", fmt.Sprintf("%d h", d.POH))
	farmRow(b, "Head flight", fmt.Sprintf("%d h", d.HeadFlightHours))
	farmRow(b, "Head loads", fmt.Sprintf("%d", d.HeadLoadEvents))
	farmRow(b, "Power cycles", fmt.Sprintf("%d", d.PowerCycles))
}

// writeFarmErrors renders the health-graded error/reliability counters.
func writeFarmErrors(b *strings.Builder, f *smart.FARM) {
	e := f.Errors
	fmt.Fprintln(b, " [::b]Error statistics[-:-:-]")
	farmCount(b, "Unrecoverable read", e.UnrecoverableRead, smart.SeverityFailing)
	farmCount(b, "Unrecoverable write", e.UnrecoverableWrite, smart.SeverityFailing)
	farmCount(b, "Reallocated sectors", e.ReallocatedSectors, smart.SeverityFailing)
	farmCount(b, "Candidate sectors", e.CandidateSectors, smart.SeverityCaution)
	farmCount(b, "Mech start failures", e.MechStartFailures, smart.SeverityFailing)
	farmCount(b, "CRC errors", e.CRCErrors, smart.SeverityCaution)
	farmCount(b, "Command timeouts", e.CommandTimeouts, smart.SeverityCaution)
	farmCount(b, "Flash-LED events", e.TotalFlashLED, smart.SeverityCaution)
}

// writeFarmEnvironment renders temperatures and power-rail telemetry.
func writeFarmEnvironment(b *strings.Builder, f *smart.FARM) {
	e := f.Environment
	fmt.Fprintln(b, " [::b]Environment[-:-:-]")
	farmRow(b, "Temp now", fmt.Sprintf("%d°C", e.CurrentTemp))
	farmRow(b, "Temp avg", fmt.Sprintf("%d°C", e.AverageTemp))
	farmRow(b, "Temp range", fmt.Sprintf("%d–%d°C (life), spec %d–%d°C",
		e.LowestTemp, e.HighestTemp, e.MinTemp, e.MaxTemp))
	farmRow(b, "12V rail", fmt.Sprintf("%s now  (%s–%s)",
		millivolts(e.Current12V), millivolts(e.Min12V), millivolts(e.Max12V)))
	farmRow(b, "5V rail", fmt.Sprintf("%s now  (%s–%s)",
		millivolts(e.Current5V), millivolts(e.Min5V), millivolts(e.Max5V)))
}

// writeFarmWorkload renders lifetime command and data-transfer totals.
func writeFarmWorkload(b *strings.Builder, f *smart.FARM) {
	w := f.Workload
	sectorBytes := f.DriveInfo.LogicalSectorB
	if sectorBytes == 0 {
		sectorBytes = 512
	}
	fmt.Fprintln(b, " [::b]Workload[-:-:-]")
	farmRow(b, "Read cmds", fmt.Sprintf("%d  (%d random)", w.TotalReadCommands, w.RandomReads))
	farmRow(b, "Write cmds", fmt.Sprintf("%d  (%d random)", w.TotalWriteCommands, w.RandomWrites))
	farmRow(b, "Data read", humanBytes(w.LogicalSectorsRead*sectorBytes))
	farmRow(b, "Data written", humanBytes(w.LogicalSectorsWrite*sectorBytes))
}

// farmRow writes an aligned key/value line.
func farmRow(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, "   [::b]%-20s[-:-:-] %s\n", k, v)
}

// farmCount writes a counter line, tinting it by severity only when non-zero so
// healthy zeroes stay neutral.
func farmCount(b *strings.Builder, k string, v int64, sevWhenSet smart.Severity) {
	val := fmt.Sprintf("%d", v)
	if v > 0 {
		val = fmt.Sprintf("[%s]%d[-]", severityTag(sevWhenSet), v)
	}
	farmRow(b, k, val)
}

// millivolts renders a millivolt reading as volts, or a dash when unset.
func millivolts(mv int) string {
	if mv == 0 {
		return dash
	}
	return fmt.Sprintf("%.2fV", float64(mv)/1000)
}

// farmHeadChart builds a per-head bar chart, or nil when the series is empty.
// When health is true, non-zero bars are tinted red (a bad head stands out).
func farmHeadChart(title string, data []int, health bool) tview.Primitive {
	if len(data) == 0 {
		return nil
	}
	chart := tvxwidgets.NewBarChart()
	chart.SetBorder(true)
	chart.SetTitle(title)

	max := 0
	for _, v := range data {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		max = 1 // keep a flat baseline instead of a zero-height axis
	}
	chart.SetMaxValue(max)

	for i, v := range data {
		color := tcell.ColorTeal
		if health && v > 0 {
			color = tcell.ColorRed
		}
		chart.AddBar(fmt.Sprintf("%d", i), v, color)
	}
	return chart
}
