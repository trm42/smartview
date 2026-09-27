// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// farmView renders the FARM tab: a 2×2 grid of stat boxes above per-head bar
// charts, refreshing in place and relaying out when data or width changes.
type farmView struct {
	*scrollView
	// boxes are in reading order: the grid's top row, then its bottom row.
	boxes  [4]farmBox
	charts []tview.Primitive

	// Width the grid was last laid out for; -1 forces a rebuild.
	lastWidth int
}

// farmBox is one stat box with its text captured at refresh.
type farmBox struct {
	tv    *tview.TextView
	text  string
	write func(*strings.Builder, *smart.FARM)
}

func newFarmView(r *smart.Report) *farmView {
	box := func(title string) *tview.TextView {
		tv := tview.NewTextView().SetDynamicColors(true)
		// Pre-wrapped by hangingIndent.
		tv.SetWrap(false)
		titledBox(tv.Box, title)
		return tv
	}
	v := &farmView{
		scrollView: newScrollView(),
		boxes: [4]farmBox{
			{tv: box(" Drive "), write: writeFarmDriveInfo},
			{tv: box(" Error statistics "), write: writeFarmErrors},
			{tv: box(" Environment "), write: writeFarmEnvironment},
			{tv: box(" Workload "), write: writeFarmWorkload},
		},
	}
	v.refresh(r, nil)
	return v
}

// setFocused accents the four stat boxes' borders.
func (v *farmView) setFocused(focused bool) {
	c := borderColor(focused)
	for _, b := range v.boxes {
		b.tv.SetBorderColor(c)
	}
}

// refresh captures the box text and rebuilds the charts; the layout waits for the next Draw.
func (v *farmView) refresh(r *smart.Report, _ []float64) {
	f := r.FARM
	if f == nil {
		return
	}

	for i := range v.boxes {
		v.boxes[i].text = farmBoxText(v.boxes[i].write, f)
	}

	v.charts = v.charts[:0]
	if c := farmHeadChart(" Reallocated sectors / head ", f.Reliability.ReallocatedByHead, true); c != nil {
		v.charts = append(v.charts, c)
	}
	if c := farmHeadChart(" MR head resistance / head ", f.Reliability.MRHeadResistance, false); c != nil {
		v.charts = append(v.charts, c)
	}

	v.lastWidth = -1
}

// Draw relays out when the width changed or refresh invalidated it.
func (v *farmView) Draw(screen tcell.Screen) {
	if _, _, w, _ := v.GetInnerRect(); w != v.lastWidth {
		v.relayout(w)
		v.lastWidth = w
	}
	v.scrollView.Draw(screen)
}

// relayout builds the 2×2 or stacked boxes above the charts for width and hands the whole layout to the scroll container.
func (v *farmView) relayout(width int) {
	leftW := width / 2
	leftInner, rightInner := boxInner(leftW), boxInner(width-leftW)

	var grid tview.Primitive
	var gridHeight int
	if min(leftInner, rightInner) < farmColumnMin {
		grid, gridHeight = v.stackBoxes(boxInner(width))
	} else {
		topRowH, bottomRowH := v.wrapBoxes(leftInner, rightInner)
		b := v.boxes
		grid = buildFarmGrid(b[0].tv, b[2].tv, b[1].tv, b[3].tv, topRowH, bottomRowH)
		gridHeight = topRowH + bottomRowH
	}

	outer := tview.NewFlex().SetDirection(tview.FlexRow)
	outer.AddItem(grid, gridHeight, 0, false)
	total := gridHeight
	for _, c := range v.charts {
		h := farmChartHeight
		if _, isSummary := c.(*tview.TextView); isSummary {
			h = farmSummaryHeight
		}
		outer.AddItem(c, h, 0, false)
		total += h
	}

	v.setContent(outer, total)
}

// farmChartHeight is the fixed cell height of each per-head bar chart.
const farmChartHeight = 9

// farmSummaryHeight is the collapsed form of an all-zero per-head fault chart.
const farmSummaryHeight = 3

// wrap pre-wraps the box's text for an inner width, sets it, and returns the box height.
func (b farmBox) wrap(innerW int) int {
	wrapped := hangingIndent(b.text, farmWrap, innerW)
	b.tv.SetText(wrapped)
	return lineCount(wrapped) + 2
}

// wrapBoxes pre-wraps each box for its column and returns the shared row heights.
func (v *farmView) wrapBoxes(leftInner, rightInner int) (topRowH, bottomRowH int) {
	b := v.boxes
	return max(b[0].wrap(leftInner), b[1].wrap(rightInner)), max(b[2].wrap(leftInner), b[3].wrap(rightInner))
}

// stackBoxes lays the four boxes out in one full-width column.
func (v *farmView) stackBoxes(innerW int) (tview.Primitive, int) {
	col := tview.NewFlex().SetDirection(tview.FlexRow)
	total := 0
	for _, b := range v.boxes {
		h := b.wrap(innerW)
		col.AddItem(b.tv, h, 0, false)
		total += h
	}
	return col, total
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

	grid := tview.NewFlex()
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

// farmValueCol must match farmLabelWidth or hangingIndent cuts inside the label.
const (
	farmLabelWidth = 20
	farmValueCol   = farmLabelWidth + 1
)

// farmColumnMin is the narrowest paired-box inner width that still holds a reading like "12.29V now".
const farmColumnMin = farmValueCol + 13

// FARM values are short numbers, so they wrap down to one cell rather than clip.
var farmWrap = hangingWrap{valueCol: farmValueCol, minValueW: 1}

// farmRow writes an aligned key/value line.
func farmRow(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, "[::b]%-*s[-:-:-] %s\n", farmLabelWidth, k, v)
}

// farmCount writes a counter line, tinting by severity only when non-zero.
func farmCount(b *strings.Builder, k string, v int64, sevWhenSet smart.Severity) {
	val := strconv.FormatInt(v, 10)
	if v > 0 {
		val = sevText(sevWhenSet, val)
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

// farmHeadChart builds a per-head chart, nil when empty; an all-zero fault counter collapses to one line.
func farmHeadChart(title string, data []int, health bool) tview.Primitive {
	if len(data) == 0 {
		return nil
	}
	worst := slices.Max(data)

	color := activeTheme.BarHealthy
	if health {
		if worst == 0 {
			return farmHeadSummary(title, len(data))
		}
		color = activeTheme.Failing
	}
	vals := make([]float64, len(data))
	for i, v := range data {
		vals[i] = float64(v)
	}

	c := newRangeChart().
		setBars(vals, farmHeadPitch, farmHeadAxis).
		setColor(color)
	c.SetBorder(true)
	c.SetTitle(fmt.Sprintf("%s— %d–%d ", title, slices.Min(data), worst))
	return c
}

// farmHeadPitch is the widest per-head bar pitch: one cell of bar, one of gap.
const farmHeadPitch = 2

// farmHeadSummary states an all-zero fault chart's healthy answer in one line.
func farmHeadSummary(title string, heads int) tview.Primitive {
	tv := tview.NewTextView().SetDynamicColors(true)
	titledBox(tv.Box, title)
	tv.SetText(fmt.Sprintf("none on any of %d heads", heads))
	return tv
}

// farmHeadAxis labels the first head of every step-th bar, step being the
// fewest bars whose cells hold an index plus a space.
func farmHeadAxis(pitch, group, heads, width int) string {
	if heads <= 0 || pitch <= 0 || group <= 0 || width <= 0 {
		return ""
	}
	bars := (heads + group - 1) / group
	labelW := len(strconv.Itoa((bars - 1) * group))
	step := max(1, (labelW+pitch)/pitch)
	// Stop at the last whole index; a sliced one names the wrong head. Indices are ASCII, so b.Len() is the column.
	var b strings.Builder
	for i := 0; i < bars; i += step {
		lbl := strconv.Itoa(i * group)
		if b.Len()+len(lbl) > width {
			break
		}
		fmt.Fprintf(&b, "%-*s", pitch*step, lbl)
	}
	return strings.TrimRight(b.String(), " ")
}
