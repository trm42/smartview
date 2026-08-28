// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// farmView renders the FARM tab: a 2×2 grid of stat boxes above per-head bar
// charts, refreshing in place and relaying out when data or width changes.
type farmView struct {
	*scrollView
	drive    *tview.TextView
	errors   *tview.TextView
	env      *tview.TextView
	workload *tview.TextView

	// Box contents and charts captured at refresh; the layout is rebuilt at
	// draw time once the true column width is known.
	driveText, errorsText, envText, workloadText string
	charts                                       []tview.Primitive

	// Width the grid was last laid out for; -1 forces a rebuild.
	lastWidth int
}

func newFarmView(r *smart.Report) *farmView {
	box := func(title string) *tview.TextView {
		tv := tview.NewTextView().SetDynamicColors(true)
		// Wrapping is pre-computed (hangingIndentValues); tview's own wrap
		// would re-break the text back to the left margin.
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

// setFocused accents the four stat boxes' borders (the scroll container is
// borderless).
func (v *farmView) setFocused(focused bool) {
	c := borderColor(focused)
	v.drive.SetBorderColor(c)
	v.errors.SetBorderColor(c)
	v.env.SetBorderColor(c)
	v.workload.SetBorderColor(c)
}

// hangingIndentValues rewraps each farmRow line so an over-long value hangs
// under the value column. Unchanged when innerW leaves no room for values.
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

// refresh captures the box text and rebuilds the charts, deferring the grid
// layout to the next Draw by invalidating lastWidth.
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

	// Per-head charts: reallocated sectors is the health red-flag, MR head
	// resistance surfaces an outlier head.
	v.charts = v.charts[:0]
	if c := farmHeadChart(" Reallocated sectors / head ", f.Reliability.ReallocatedByHead, true); c != nil {
		v.charts = append(v.charts, c)
	}
	if c := farmHeadChart(" MR head resistance / head ", f.Reliability.MRHeadResistance, false); c != nil {
		v.charts = append(v.charts, c)
	}

	v.lastWidth = -1 // content changed: relayout against the current width
}

// Draw relays out when the width changed (or refresh invalidated it); box
// heights depend on wrapping at the live width, which is only known here.
func (v *farmView) Draw(screen tcell.Screen) {
	if _, _, w, _ := v.GetInnerRect(); w != v.lastWidth {
		v.relayout(w)
		v.lastWidth = w
	}
	v.scrollView.Draw(screen)
}

// relayout builds the 2×2 grid above the charts for the given width and hands
// the whole layout to the scroll container at its total height, so the bottom
// charts stay reachable on a short terminal. Inner items are non-focusable —
// focus stays on the scrollView.
func (v *farmView) relayout(width int) {
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
		h := farmChartHeight
		if _, isSummary := c.(*tview.TextView); isSummary {
			h = farmSummaryHeight // one line of prose, not a plot
		}
		outer.AddItem(c, h, 0, false)
		total += h
	}

	v.setContent(outer, total)
}

// farmChartHeight is the fixed cell height of each per-head bar chart.
const farmChartHeight = 9

// farmSummaryHeight is the collapsed form of an all-zero per-head fault chart:
// a bordered line, not a plot.
const farmSummaryHeight = 3

// wrapBoxes pre-wraps each box's text for its column width, sets the boxes,
// and returns the shared top/bottom row heights (paired boxes grow to a
// common height so the columns end level).
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

// buildFarmGrid arranges the four boxes into the 2×2 grid: drive over env,
// beside errors over workload.
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
	// RecordingType is drive-controlled; esc() blocks markup injection.
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
	// Readings carry severity: this is where an out-of-spec environment shows.
	farmRow(b, "Temp now", tempMarkup(e.CurrentTemp))
	farmRow(b, "Temp avg", tempMarkup(e.AverageTemp))
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

// farmCount writes a counter line, tinting by severity only when non-zero.
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

// farmHeadChart builds a per-head chart, nil for an empty series. A fault
// counter (health) that is all zero collapses to a one-line summary. Uses
// rangeChart, not tvxwidgets.BarChart, which anchors to zero (see chart.go).
func farmHeadChart(title string, data []int, health bool) tview.Primitive {
	if len(data) == 0 {
		return nil
	}
	vals := make([]float64, len(data))
	worst := 0
	for i, v := range data {
		vals[i] = float64(v)
		worst = max(worst, v)
	}

	color := activeTheme.BarHealthy
	if health {
		if worst == 0 {
			return farmHeadSummary(title, len(data))
		}
		color = activeTheme.Failing
	}

	c := newRangeChart().
		setBars(vals, farmHeadPitch, "", farmHeadAxis(len(data))).
		setColor(color)
	c.SetBorder(true)
	c.SetBorderPadding(0, 0, uiGutter, uiGutter)
	c.SetTitle(fmt.Sprintf("%s— %d–%d ", title, minInts(data), worst))
	return c
}

// farmHeadPitch is the per-head bar pitch: one cell of bar, one of gap.
const farmHeadPitch = 2

// farmHeadSummary states an all-zero fault chart's healthy answer in one line.
func farmHeadSummary(title string, heads int) tview.Primitive {
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(title)
	tv.SetText(fmt.Sprintf("%snone on any of %d heads[-]", okTag(), heads))
	return tv
}

// farmHeadAxis labels head indices under the bars; past ten heads two-digit
// indices no longer fit the pitch, so every other one is labelled.
func farmHeadAxis(heads int) string {
	step := 1
	if heads > 10 {
		step = 2
	}
	var b strings.Builder
	for i := 0; i < heads; i += step {
		fmt.Fprintf(&b, "%-*d", farmHeadPitch*step, i)
	}
	return strings.TrimRight(b.String(), " ")
}

// minInts is the smallest value in data, or 0 for an empty series.
func minInts(data []int) int {
	if len(data) == 0 {
		return 0
	}
	lo := data[0]
	for _, v := range data[1:] {
		lo = min(lo, v)
	}
	return lo
}
