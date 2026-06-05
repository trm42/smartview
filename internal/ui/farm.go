// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/navidys/tvxwidgets"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// farmView renders the FARM tab: four separately-bordered panels of Seagate
// Field Accessible Reliability Metrics (drive/wear summary, health-graded error
// counters, environment and workload totals) laid out as a 2×2 grid above the
// per-head bar charts. It refreshes in place, rebuilding the grid each poll.
type farmView struct {
	*scrollView
	drive    *tview.TextView
	errors   *tview.TextView
	env      *tview.TextView
	workload *tview.TextView
}

func newFarmView(r *smart.Report) *farmView {
	box := func(title string) *tview.TextView {
		tv := tview.NewTextView().SetDynamicColors(true)
		tv.SetBorder(true).SetTitle(title)
		return tv
	}
	v := &farmView{
		scrollView: newScrollView(),
		drive:      box(" Drive "),
		errors:     box(" Error statistics "),
		env:        box(" Environment "),
		workload:   box(" Workload "),
	}
	v.refresh(r, nil)
	return v
}

// boxHeight returns the fixed height for a bordered box holding text: one row
// per line plus two for the top and bottom border.
func boxHeight(text string) int {
	return strings.Count(text, "\n") + 2
}

// refresh rebuilds the four stat boxes and lays them out as a 2×2 grid (a left
// column of drive then env, a right column of errors then workload) above the
// per-head charts. Each box is fixed to its content height and top-anchored by a
// trailing flexible spacer. The whole layout is handed to the scroll container
// at its full height so the bottom charts stay reachable when the terminal is
// shorter than the content; focus stays on the scrollView, so inner items are
// added non-focusable.
func (v *farmView) refresh(r *smart.Report, _ []float64) {
	f := r.FARM
	if f == nil {
		return
	}

	driveText := farmBoxText(writeFarmDriveInfo, f)
	errorsText := farmBoxText(writeFarmErrors, f)
	envText := farmBoxText(writeFarmEnvironment, f)
	workloadText := farmBoxText(writeFarmWorkload, f)
	v.drive.SetText(driveText)
	v.errors.SetText(errorsText)
	v.env.SetText(envText)
	v.workload.SetText(workloadText)

	left := tview.NewFlex().SetDirection(tview.FlexRow)
	left.AddItem(v.drive, boxHeight(driveText), 0, false)
	left.AddItem(v.env, boxHeight(envText), 0, false)
	left.AddItem(nil, 0, 1, false)
	leftTotal := boxHeight(driveText) + boxHeight(envText)

	right := tview.NewFlex().SetDirection(tview.FlexRow)
	right.AddItem(v.errors, boxHeight(errorsText), 0, false)
	right.AddItem(v.workload, boxHeight(workloadText), 0, false)
	right.AddItem(nil, 0, 1, false)
	rightTotal := boxHeight(errorsText) + boxHeight(workloadText)

	grid := tview.NewFlex() // horizontal: left column | right column
	grid.AddItem(left, 0, 1, false)
	grid.AddItem(right, 0, 1, false)

	gridHeight := max(leftTotal, rightTotal)
	outer := tview.NewFlex().SetDirection(tview.FlexRow)
	outer.AddItem(grid, gridHeight, 0, false)
	total := gridHeight
	// Per-head visualizations. Reallocated sectors per head is the health
	// red-flag (flat zero on a healthy drive); MR head resistance always varies
	// and surfaces an outlier head.
	if c := farmHeadChart(" Reallocated sectors / head ", f.Reliability.ReallocatedByHead, true); c != nil {
		outer.AddItem(c, 9, 0, false)
		total += 9
	}
	if c := farmHeadChart(" MR head resistance / head ", f.Reliability.MRHeadResistance, false); c != nil {
		outer.AddItem(c, 9, 0, false)
		total += 9
	}

	v.setContent(outer, total)
}

// farmBoxText builds a single box's text via the matching writeFarm* helper.
func farmBoxText(write func(*strings.Builder, *smart.FARM), f *smart.FARM) string {
	var b strings.Builder
	write(&b, f)
	return b.String()
}

// writeFarmDriveInfo renders the drive/wear summary block.
func writeFarmDriveInfo(b *strings.Builder, f *smart.FARM) {
	d := f.DriveInfo
	farmRow(b, "Recording", orDash(d.RecordingType))
	if d.RotationRate > 0 {
		farmRow(b, "Spindle", fmt.Sprintf("%d rpm", d.RotationRate))
	}
	farmRow(b, "Heads", fmt.Sprintf("%d", d.Heads))
	farmRow(b, "Power-on", humanDuration(d.POH))
	farmRow(b, "Head flight", humanDuration(d.HeadFlightHours))
	farmRow(b, "Head loads", fmt.Sprintf("%d", d.HeadLoadEvents))
	farmRow(b, "Power cycles", fmt.Sprintf("%d", d.PowerCycles))
}

// writeFarmErrors renders the health-graded error/reliability counters.
func writeFarmErrors(b *strings.Builder, f *smart.FARM) {
	e := f.Errors
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
	farmRow(b, "Read cmds", fmt.Sprintf("%d  (%d random)", w.TotalReadCommands, w.RandomReads))
	farmRow(b, "Write cmds", fmt.Sprintf("%d  (%d random)", w.TotalWriteCommands, w.RandomWrites))
	farmRow(b, "Data read", humanBytes(w.LogicalSectorsRead*sectorBytes))
	farmRow(b, "Data written", humanBytes(w.LogicalSectorsWrite*sectorBytes))
}

// farmRow writes an aligned key/value line.
func farmRow(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, " [::b]%-20s[-:-:-] %s\n", k, v)
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
