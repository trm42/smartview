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
// per-head bar charts. It refreshes in place, and relays out (resizing each box
// to its content) whenever the data or the available width changes.
type farmView struct {
	*scrollView
	drive    *tview.TextView
	errors   *tview.TextView
	env      *tview.TextView
	workload *tview.TextView

	// Latest box contents and the per-head charts, captured at refresh so the
	// layout can be rebuilt at draw time once the true column width is known.
	driveText, errorsText, envText, workloadText string
	charts                                       []tview.Primitive

	// Width the grid was last laid out for; -1 forces a rebuild (set whenever the
	// data changes) so a fresh poll's longer/shorter values resize their box.
	lastWidth int
}

func newFarmView(r *smart.Report) *farmView {
	box := func(title string) *tview.TextView {
		tv := tview.NewTextView().SetDynamicColors(true)
		// Wrapping is pre-computed in relayout (hangingIndentValues) so values that
		// overflow hang-indent under the value column; disable tview's own wrap so it
		// does not re-break the already-wrapped text back to the left margin.
		tv.SetWrap(false)
		tv.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(title)
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

// setFocused accents the FARM tab's borders when it holds keyboard focus. The
// scroll container is borderless (inner content supplies the borders), so the
// focus cue is applied to the four stat boxes.
func (v *farmView) setFocused(focused bool) {
	c := borderColor(focused)
	v.drive.SetBorderColor(c)
	v.errors.SetBorderColor(c)
	v.env.SetBorderColor(c)
	v.workload.SetBorderColor(c)
}

// hangingIndentValues rewraps each farmRow "label  value" line so a value too
// long for the box wraps with a hanging indent aligned under the value column,
// instead of tview wrapping it back to the left margin. innerW is the box's
// wrappable inner width (outer width minus borders and gutters). Returns the
// text unchanged when innerW leaves no room for the value column.
func hangingIndentValues(text string, innerW int) string {
	const valueCol = 21 // 20-char %-20s label + one space, per farmRow
	valueW := innerW - valueCol
	if valueW <= 0 {
		return text
	}
	const marker = "[-:-:-] "
	indent := strings.Repeat(" ", valueCol)

	var out strings.Builder
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		prefix, value, found := strings.Cut(line, marker)
		if !found {
			out.WriteString(line)
			continue
		}
		prefix += marker
		wrapped := tview.WordWrap(value, valueW)
		if len(wrapped) == 0 {
			out.WriteString(prefix)
			continue
		}
		out.WriteString(prefix + wrapped[0])
		for _, cont := range wrapped[1:] {
			out.WriteByte('\n')
			out.WriteString(indent + cont)
		}
	}
	return out.String()
}

// refresh captures the four stat boxes' text and rebuilds the per-head charts,
// then defers the grid layout (which needs the column width) to the next Draw by
// invalidating lastWidth.
func (v *farmView) refresh(r *smart.Report, _ []float64) {
	f := r.FARM
	if f == nil {
		return
	}

	v.driveText = farmBoxText(writeFarmDriveInfo, f)
	v.errorsText = farmBoxText(writeFarmErrors, f)
	v.envText = farmBoxText(writeFarmEnvironment, f)
	v.workloadText = farmBoxText(writeFarmWorkload, f)
	v.drive.SetText(v.driveText)
	v.errors.SetText(v.errorsText)
	v.env.SetText(v.envText)
	v.workload.SetText(v.workloadText)

	// Per-head visualizations. Reallocated sectors per head is the health
	// red-flag (flat zero on a healthy drive); MR head resistance always varies
	// and surfaces an outlier head.
	v.charts = v.charts[:0]
	if c := farmHeadChart(" Reallocated sectors / head ", f.Reliability.ReallocatedByHead, true); c != nil {
		v.charts = append(v.charts, c)
	}
	if c := farmHeadChart(" MR head resistance / head ", f.Reliability.MRHeadResistance, false); c != nil {
		v.charts = append(v.charts, c)
	}

	v.lastWidth = -1 // content changed: relayout against the current width
}

// Draw relays out the grid when the width changed (or refresh invalidated it),
// then defers to the scroll container. Box heights depend on word-wrapping at
// the live column width, which is only known here, so the layout is rebuilt
// lazily rather than in refresh.
func (v *farmView) Draw(screen tcell.Screen) {
	if _, _, w, _ := v.GetInnerRect(); w != v.lastWidth {
		v.relayout(w)
		v.lastWidth = w
	}
	v.scrollView.Draw(screen)
}

// relayout builds the 2×2 grid (a left column of drive then env, a right column
// of errors then workload) above the per-head charts for a content area of the
// given width. Each box's text is pre-wrapped with a hanging indent so overflowing
// values stay aligned under the value column, and paired side-by-side boxes are
// grown to a common row height so the two columns end level. The full layout is
// handed to the scroll container at its total height so the bottom charts stay
// reachable when the terminal is shorter than the content; focus stays on the
// scrollView, so inner items are added non-focusable.
func (v *farmView) relayout(width int) {
	// A horizontal Flex of two equal-weight columns splits the width as below.
	leftW := width / 2
	rightW := width - leftW
	leftInner := leftW - 2 - 2*uiGutter // outer width minus borders and gutters
	rightInner := rightW - 2 - 2*uiGutter

	topRowH, bottomRowH := v.wrapBoxes(leftInner, rightInner)
	grid := buildFarmGrid(v.drive, v.env, v.errors, v.workload, topRowH, bottomRowH)

	gridHeight := topRowH + bottomRowH
	outer := tview.NewFlex().SetDirection(tview.FlexRow)
	outer.AddItem(grid, gridHeight, 0, false)
	total := gridHeight
	for _, c := range v.charts {
		outer.AddItem(c, farmChartHeight, 0, false)
		total += farmChartHeight
	}

	v.setContent(outer, total)
}

// farmChartHeight is the fixed cell height of each per-head bar chart (border +
// labelled bars); they stack below the 2×2 stat grid inside the scroll viewport.
const farmChartHeight = 9

// wrapBoxes pre-wraps each stat box's text for its column's inner width so long
// values hang-indent under the value column rather than wrapping to the left
// margin (SetWrap(false) on the boxes keeps tview from re-breaking it), sets the
// boxes, and returns the shared heights for the top (drive|errors) and bottom
// (env|workload) rows — each paired box grown to a common height so the two
// columns end level.
func (v *farmView) wrapBoxes(leftInner, rightInner int) (topRowH, bottomRowH int) {
	driveText := hangingIndentValues(v.driveText, leftInner)
	envText := hangingIndentValues(v.envText, leftInner)
	errorsText := hangingIndentValues(v.errorsText, rightInner)
	workloadText := hangingIndentValues(v.workloadText, rightInner)
	v.drive.SetText(driveText)
	v.env.SetText(envText)
	v.errors.SetText(errorsText)
	v.workload.SetText(workloadText)

	// Height is just the pre-wrapped line count plus the two borders.
	boxHeight := func(text string) int {
		return strings.Count(strings.TrimRight(text, "\n"), "\n") + 1 + 2
	}
	topRowH = max(boxHeight(driveText), boxHeight(errorsText))
	bottomRowH = max(boxHeight(envText), boxHeight(workloadText))
	return topRowH, bottomRowH
}

// buildFarmGrid arranges the four stat boxes into the 2×2 grid: a left column of
// drive over env beside a right column of errors over workload, paired rows
// sharing a height so the columns stay level.
func buildFarmGrid(drive, env, errors, workload tview.Primitive, topRowH, bottomRowH int) tview.Primitive {
	left := tview.NewFlex().SetDirection(tview.FlexRow)
	left.AddItem(drive, topRowH, 0, false)
	left.AddItem(env, bottomRowH, 0, false)

	right := tview.NewFlex().SetDirection(tview.FlexRow)
	right.AddItem(errors, topRowH, 0, false)
	right.AddItem(workload, bottomRowH, 0, false)

	grid := tview.NewFlex() // horizontal: left column | right column
	grid.AddItem(left, 0, 1, false)
	grid.AddItem(right, 0, 1, false)
	return grid
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
	// RecordingType is drive-controlled free text; escape markup (see esc).
	farmRow(b, "Recording", orDash(esc(d.RecordingType)))
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
	fmt.Fprintf(b, "[::b]%-20s[-:-:-] %s\n", k, v)
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
