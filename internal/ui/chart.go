// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// This file is smartview's chart renderer. It exists because every chart widget
// we had anchored its axis to zero, which destroys the signal whenever the
// interesting variation sits in the top few percent of the range:
//
//   - tvxwidgets.Sparkline computes int((data/maxVal) * barHeight), so a drive
//     that lived between 35°C and 40°C rendered as a solid rectangle.
//   - tvxwidgets.BarChart offers only SetMaxValue and draws its own axis labels
//     from the values it is handed, so per-head resistances of 350–495 drew as
//     twenty bars of identical height. Offsetting the data before handing it
//     over would fix the shape under an axis that then lied about the numbers.
//
// Neither exposes a baseline, so the renderer is ours. The scaling is a pure
// function over a slice — the first chart code here that can be unit-tested.

// blockRamp is the eighths ramp used to sub-divide a cell vertically. Index 0 is
// the shortest visible mark, index 7 a full cell. It is []rune deliberately:
// each glyph is three bytes, so indexing the string would slice mid-character.
var blockRamp = []rune("▁▂▃▄▅▆▇█")

// dataRange returns the smallest and largest value in data. ok is false for an
// empty series, so callers can decline to draw rather than inventing a range.
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

// padRange widens a degenerate range so a perfectly flat series still draws a
// line rather than dividing by zero. A flat series is a real answer ("this never
// moved"), so it renders along the baseline instead of being suppressed.
func padRange(lo, hi float64) (float64, float64) {
	if hi > lo {
		return lo, hi
	}
	return lo, lo + 1
}

// downsample reduces data to at most width points by taking the MAXIMUM of each
// bucket rather than the mean. Averaging would smooth away exactly the thing a
// health tool is looking for — a single hot sample or a lone bad head — so a
// spike always survives the reduction. Series shorter than width are returned
// unchanged; the caller decides how to place a short series.
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

// seriesRows renders data as rows lines of width cells, tracing the TOP EDGE of
// the series scaled to [lo, hi] — a line, not a filled area. Filling from the
// baseline reproduces the solid-block failure whenever the series sits high in
// its own range, which is the common case for drive temperatures.
//
// Row 0 is the top of the chart. Values at or above hi occupy the top row as a
// full cell; values at lo sit on the baseline row.
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

// barRows renders values as vertical bars of barWidth cells each, scaled to
// [lo, hi]. Unlike seriesRows this fills from the baseline: the values are
// categorical (one per head), so the comparison between neighbours is the point
// and a filled bar reads better than a traced edge.
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
			// How much of this cell the bar fills, in eighths, counting up from
			// the baseline row.
			cell := eighths - float64((rows-1-r)*8)
			g := ' '
			switch {
			case cell >= 8:
				g = '█'
			case cell > 0:
				g = blockRamp[int(cell)]
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

// axisLabels returns the label for each chart row, top first: the value at the
// TOP of that row's band, so a mark in a row means "at most this". The baseline
// (lo) is labelled separately, on the axis line beneath the plot.
func axisLabels(rows int, lo, hi float64) []string {
	lo, hi = padRange(lo, hi)
	out := make([]string, rows)
	step := (hi - lo) / float64(rows)
	for r := range rows {
		out[r] = fmt.Sprintf("%.0f", hi-step*float64(r))
	}
	return out
}

// rangeChart is a bordered chart primitive that scales to its data rather than
// to zero. It renders either a traced series (setSeries) or categorical bars
// (setBars), with a labelled y axis whose baseline is the smallest value in the
// data — stated on the axis so the reader knows the plot does not start at zero.
type rangeChart struct {
	*tview.Box
	data    []float64
	bars    bool
	tick    int    // bar pitch in cells; 1 for a traced series
	unit    string // appended to the axis labels and the range caption
	caption string // one line under the axis: what the x axis is
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

// setBars plots data as categorical bars at the given pitch (bar cell plus
// gap), which keeps neighbouring bars readable as separate values.
func (c *rangeChart) setBars(data []float64, pitch int, unit, caption string) *rangeChart {
	c.data, c.bars, c.tick, c.unit, c.caption = data, true, max(pitch, 1), unit, caption
	return c
}

func (c *rangeChart) setColor(col tcell.Color) *rangeChart { c.color = col; return c }

// setFocused accents the border when the chart holds keyboard focus, matching
// every other focusable body in the UI.
func (c *rangeChart) setFocused(focused bool) {
	c.focused = focused
	c.SetBorderColor(borderColor(focused))
}

// chartMinHeight is the smallest useful chart: one plot row, an axis line and a
// caption. Below this the chart draws nothing rather than a misleading stub.
const chartMinHeight = 3

// Draw paints the axis, the plot and the caption. The y-axis gutter is sized to
// the widest label so the plot never shifts as values change magnitude.
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
	labels := axisLabels(plotRows, lo, hi)
	gutter := len(fmt.Sprintf("%.0f", lo))
	for _, l := range labels {
		gutter = max(gutter, len(l))
	}
	gutter += 2 // a space and the axis glyph
	plotW := w - gutter
	if plotW <= 0 {
		return
	}

	var rows []string
	if c.bars {
		rows = barRows(c.data, c.tick, plotRows, lo, hi)
	} else {
		rows = seriesRows(c.data, plotW, plotRows, lo, hi)
	}

	muted := activeTheme.Muted
	for r, line := range rows {
		lbl := fmt.Sprintf("%*s ", gutter-1, labels[r])
		tview.Print(screen, esc(lbl), x, y+r, gutter, tview.AlignLeft, muted)
		tview.Print(screen, "┤", x+gutter-1, y+r, 1, tview.AlignLeft, activeTheme.Accent)
		if len(line) > plotW {
			line = string([]rune(line)[:plotW])
		}
		tview.Print(screen, esc(line), x+gutter, y+r, plotW, tview.AlignLeft, c.color)
	}

	// The axis line carries the baseline value, so it is always obvious that the
	// plot does not start at zero.
	base := fmt.Sprintf("%*s ", gutter-1, fmt.Sprintf("%.0f", lo))
	tview.Print(screen, esc(base), x, y+plotRows, gutter, tview.AlignLeft, muted)
	tview.Print(screen, "└"+strings.Repeat("─", plotW-1), x+gutter-1, y+plotRows, plotW, tview.AlignLeft, muted)
	if c.caption != "" {
		tview.Print(screen, esc(c.caption), x+gutter, y+plotRows+1, plotW, tview.AlignLeft, muted)
	}
}
