// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// smartview's chart renderer: charts scale to their data, never to zero.
// tvxwidgets' Sparkline and BarChart both anchor to zero and expose no
// baseline, which flattens any series whose variation sits high in its range
// (a 35–40°C history, per-head resistances of 350–495). The scaling here is
// pure and unit-tested.

// blockRamp sub-divides a cell vertically in eighths. []rune deliberately:
// each glyph is three bytes, so indexing the string would slice mid-character.
var blockRamp = []rune("▁▂▃▄▅▆▇█")

// dataRange returns the data's min and max; ok is false for an empty series.
func dataRange(data []float64) (lo, hi float64, ok bool) {
	if len(data) == 0 {
		return 0, 0, false
	}
	lo, hi = data[0], data[0]
	for _, v := range data[1:] {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	return lo, hi, true
}

// padRange widens a degenerate range so a flat series draws along the
// baseline instead of dividing by zero.
func padRange(lo, hi float64) (float64, float64) {
	if hi > lo {
		return lo, hi
	}
	return lo, lo + 1
}

// downsample reduces data to width points by bucket MAXIMUM, not mean, so a
// spike is never averaged away. Shorter series return unchanged.
func downsample(data []float64, width int) []float64 {
	if width <= 0 || len(data) <= width {
		return data
	}
	out := make([]float64, width)
	for i := range out {
		start := i * len(data) / width
		end := (i + 1) * len(data) / width
		if end <= start {
			end = start + 1
		}
		peak := data[start]
		for _, v := range data[start+1 : end] {
			peak = max(peak, v)
		}
		out[i] = peak
	}
	return out
}

// seriesRows traces the TOP EDGE of the series scaled to [lo, hi] — a line,
// not a filled area, which would reproduce the solid-block failure. Row 0 is
// the top of the chart.
func seriesRows(data []float64, width, rows int, lo, hi float64) []string {
	if width <= 0 || rows <= 0 {
		return nil
	}
	lo, hi = padRange(lo, hi)
	grid := make([][]rune, rows)
	for r := range grid {
		grid[r] = []rune(strings.Repeat(" ", width))
	}
	for x, v := range downsample(data, width) {
		if x >= width {
			break
		}
		frac := (v - lo) / (hi - lo)
		frac = min(max(frac, 0), 1)
		eighths := frac * float64(rows) * 8
		if eighths >= float64(rows*8) {
			grid[0][x] = '█'
			continue
		}
		row := rows - 1 - int(eighths)/8
		grid[row][x] = blockRamp[int(eighths)%8]
	}
	out := make([]string, rows)
	for r, g := range grid {
		out[r] = string(g)
	}
	return out
}

// barRows renders categorical values as vertical bars scaled to [lo, hi],
// filled from the baseline (comparison between neighbours is the point).
func barRows(values []float64, barWidth, rows int, lo, hi float64) []string {
	if barWidth <= 0 || rows <= 0 {
		return nil
	}
	lo, hi = padRange(lo, hi)
	cols := make([][]rune, rows)
	for _, v := range values {
		frac := (v - lo) / (hi - lo)
		frac = min(max(frac, 0), 1)
		eighths := frac * float64(rows) * 8
		for r := range rows {
			// This cell's fill in eighths, counting up from the baseline row.
			cell := eighths - float64((rows-1-r)*8)
			g := ' '
			switch {
			case cell >= 8:
				g = '█'
			case cell > 0:
				g = blockRamp[int(cell)]
			case r == rows-1:
				// The smallest value would draw nothing and read as a missing
				// category; give the baseline row a minimum mark.
				g = blockRamp[0]
			}
			cols[r] = append(cols[r], g)
			for range barWidth - 1 {
				cols[r] = append(cols[r], ' ') // gap, so neighbours stay separate
			}
		}
	}
	out := make([]string, rows)
	for r, c := range cols {
		out[r] = string(c)
	}
	return out
}

// axisLabels labels each row with the value at the TOP of its band; the
// baseline goes on the axis line. Integer rounding can repeat a label across
// rows, so only the first occurrence is printed.
func axisLabels(rows int, lo, hi float64) []string {
	lo, hi = padRange(lo, hi)
	out := make([]string, rows)
	step := (hi - lo) / float64(rows)
	prev := ""
	for r := range rows {
		lbl := fmt.Sprintf("%.0f", hi-step*float64(r))
		if lbl == prev {
			lbl = ""
		} else {
			prev = lbl
		}
		out[r] = lbl
	}
	return out
}

// rangeChart is a bordered chart that scales to its data rather than zero:
// a traced series (setSeries) or categorical bars (setBars), with the
// baseline value stated on the axis.
type rangeChart struct {
	*tview.Box
	data    []float64
	bars    bool
	tick    int    // bar pitch in cells; 1 for a traced series
	unit    string // appended to the axis labels and the range caption
	caption string // one line under the axis: what the x axis is
	// axis builds the caption for a bar chart instead of caption. The pitch and
	// how many bars fit are only known at draw time, so the labels cannot be
	// baked in with the data.
	axis    func(pitch, shown, width int) string
	color   tcell.Color
	focused bool
}

// newRangeChart returns an empty chart. Call setSeries or setBars before use.
func newRangeChart() *rangeChart {
	return &rangeChart{Box: tview.NewBox(), tick: 1, color: activeTheme.BarHealthy}
}

// setSeries plots data as a traced line, downsampled to the available width.
func (c *rangeChart) setSeries(data []float64, unit, caption string) *rangeChart {
	c.data, c.bars, c.tick, c.unit, c.caption = data, false, 1, unit, caption
	return c
}

// setBars plots data as categorical bars. pitch is the widest bar cell plus
// gap to use; Draw narrows it toward 1 rather than let bars fall off the edge.
// axis builds the caption once the pitch and bar count are known.
func (c *rangeChart) setBars(data []float64, pitch int, unit string, axis func(pitch, shown, width int) string) *rangeChart {
	c.data, c.bars, c.tick, c.unit, c.axis = data, true, max(pitch, 1), unit, axis
	c.caption = ""
	return c
}

// barFit picks the bar pitch and count for a plot of plotW cells: the widest
// pitch up to c.tick that seats every bar, narrowing to 1 before dropping any.
// shown is short of the data only when even one cell each does not fit.
func (c *rangeChart) barFit(plotW int) (pitch, shown int) {
	n := len(c.data)
	if n == 0 || plotW <= 0 {
		return c.tick, 0
	}
	pitch = min(c.tick, max(plotW/n, 1))
	return pitch, min(n, plotW/pitch)
}

// barCaption is the axis labels plus, when bars had to be dropped, how many —
// the same contract as the fleet's dropped columns: nothing goes missing
// silently. The note is measured first so the labels are built around it, in
// cells rather than bytes: "·" is two bytes and the screen clips in cells.
func (c *rangeChart) barCaption(pitch, shown, plotW int) string {
	note := ""
	if dropped := len(c.data) - shown; dropped > 0 {
		note = fmt.Sprintf(" · %d more", dropped)
	}
	labels := ""
	if c.axis != nil {
		labels = c.axis(pitch, shown, plotW-utf8.RuneCountInString(note))
	}
	return labels + note
}

func (c *rangeChart) setColor(col tcell.Color) *rangeChart { c.color = col; return c }

// setFocused accents the border when the chart holds keyboard focus.
func (c *rangeChart) setFocused(focused bool) {
	c.focused = focused
	c.SetBorderColor(borderColor(focused))
}

// chartMinHeight is one plot row, an axis line and a caption; below this the
// chart draws nothing rather than a misleading stub.
const chartMinHeight = 3

// Draw paints the axis, plot and caption; the y-axis gutter is sized to the
// widest label so the plot never shifts.
func (c *rangeChart) Draw(screen tcell.Screen) {
	c.DrawForSubclass(screen, c)
	x, y, w, h := c.GetInnerRect()
	if w <= 0 || h < chartMinHeight || len(c.data) == 0 {
		return
	}
	lo, hi, ok := dataRange(c.data)
	if !ok {
		return
	}

	plotRows := h - 2 // one axis line, one caption line
	// Cap resolution to the integer span: values are whole numbers, and finer
	// rows could never be landed in.
	if span := int(hi-lo) + 1; span > 0 && span < plotRows {
		plotRows = span
	}
	top := y + (h - 2 - plotRows)
	labels := axisLabels(plotRows, lo, hi)
	gutter := len(fmt.Sprintf("%.0f", lo))
	for _, l := range labels {
		gutter = max(gutter, len(l))
	}
	gutter += 3 // a space either side of the label, then the axis glyph
	plotW := w - gutter
	if plotW <= 0 {
		return
	}

	var rows []string
	caption := c.caption
	if c.bars {
		pitch, shown := c.barFit(plotW)
		// The scale stays the whole drive's range even when bars are dropped, so
		// the axis labels keep agreeing with the title; the caption says what is
		// missing rather than quietly rescaling around it.
		rows = barRows(c.data[:shown], pitch, plotRows, lo, hi)
		caption = c.barCaption(pitch, shown, plotW)
	} else {
		rows = seriesRows(c.data, plotW, plotRows, lo, hi)
	}

	muted := activeTheme.Muted
	for r, line := range rows {
		lbl := fmt.Sprintf("%*s ", gutter-2, labels[r])
		tview.Print(screen, esc(lbl), x, top+r, gutter, tview.AlignLeft, muted)
		tview.Print(screen, "┤", x+gutter-1, top+r, 1, tview.AlignLeft, activeTheme.Accent)
		// Clip by RUNES, not bytes: the block glyphs are three bytes each.
		cells := []rune(line)
		if len(cells) > plotW {
			cells = cells[:plotW]
		}
		tview.Print(screen, esc(string(cells)), x+gutter, top+r, plotW, tview.AlignLeft, c.color)
	}

	// The axis line carries the baseline value, so a non-zero start is obvious.
	base := fmt.Sprintf("%*s ", gutter-2, fmt.Sprintf("%.0f", lo))
	tview.Print(screen, esc(base), x, top+plotRows, gutter, tview.AlignLeft, muted)
	tview.Print(screen, "└"+strings.Repeat("─", plotW-1), x+gutter-1, top+plotRows, plotW, tview.AlignLeft, muted)
	if caption != "" {
		tview.Print(screen, esc(caption), x+gutter, top+plotRows+1, plotW, tview.AlignLeft, muted)
	}
}
