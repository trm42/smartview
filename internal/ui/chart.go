// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Charts scale to their data, never to zero; tvxwidgets offers no baseline.

// blockRamp sub-divides a cell vertically in eighths; []rune because each glyph is three bytes.
var blockRamp = []rune("▁▂▃▄▅▆▇█")

// dataRange returns the data's min and max; ok is false for an empty series.
func dataRange(data []float64) (lo, hi float64, ok bool) {
	if len(data) == 0 {
		return 0, 0, false
	}
	return slices.Min(data), slices.Max(data), true
}

// padRange widens a degenerate range so a flat series does not divide by zero.
func padRange(lo, hi float64) (float64, float64) {
	if hi > lo {
		return lo, hi
	}
	return lo, lo + 1
}

// downsample reduces data to width points by bucket maximum, so a spike is never averaged away.
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
		out[i] = slices.Max(data[start:end])
	}
	return out
}

// bucketMax groups values into uniform bars by maximum, so a failing head in the tail survives.
func bucketMax(data []float64, group int) []float64 {
	if group <= 1 {
		return data
	}
	out := make([]float64, 0, (len(data)+group-1)/group)
	for i := 0; i < len(data); i += group {
		out = append(out, slices.Max(data[i:min(i+group, len(data))]))
	}
	return out
}

// fillEighths scales v within [lo, hi] to a column height in eighths of a
// cell; it pads the range itself so a flat series cannot yield NaN.
func fillEighths(v, lo, hi float64, rows int) float64 {
	lo, hi = padRange(lo, hi)
	frac := (v - lo) / (hi - lo)
	frac = min(max(frac, 0), 1)
	return frac * float64(rows) * 8
}

// fillGlyph is the glyph for row r of a column filled to eighths; row rows-1
// is the baseline, which always gets a mark so the smallest value is not read as missing.
func fillGlyph(eighths float64, rows, r int) rune {
	cell := eighths - float64((rows-1-r)*8)
	switch {
	case cell >= 8:
		return '█'
	case cell > 0:
		return blockRamp[int(cell)]
	case r == rows-1:
		return blockRamp[0]
	}
	return ' '
}

// seriesRows plots the series scaled to [lo, hi] as a filled area; row 0 is the top.
func seriesRows(data []float64, width, rows int, lo, hi float64) []string {
	if width <= 0 || rows <= 0 {
		return nil
	}
	grid := make([][]rune, rows)
	for r := range grid {
		grid[r] = []rune(strings.Repeat(" ", width))
	}
	for x, v := range downsample(data, width) {
		eighths := fillEighths(v, lo, hi, rows)
		for r := range rows {
			grid[r][x] = fillGlyph(eighths, rows, r)
		}
	}
	out := make([]string, rows)
	for r, g := range grid {
		out[r] = string(g)
	}
	return out
}

// barRows renders categorical values as vertical bars scaled to [lo, hi].
func barRows(values []float64, barWidth, rows int, lo, hi float64) []string {
	if barWidth <= 0 || rows <= 0 {
		return nil
	}
	cols := make([][]rune, rows)
	for _, v := range values {
		eighths := fillEighths(v, lo, hi, rows)
		for r := range rows {
			cols[r] = append(cols[r], fillGlyph(eighths, rows, r))
			for range barWidth - 1 {
				cols[r] = append(cols[r], ' ')
			}
		}
	}
	out := make([]string, rows)
	for r, c := range cols {
		out[r] = string(c)
	}
	return out
}

// axisLabels labels each row with the value at the top of its band, printing a
// label repeated by integer rounding only once.
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

// rangeChart is a bordered chart, a filled series or categorical bars, that
// scales to its data and states the baseline on the axis.
type rangeChart struct {
	*tview.Box
	data    []float64
	bars    bool
	tick    int    // bar pitch in cells; 1 for a filled series
	caption string // one line under the axis: what the x axis is
	// axis builds a bar chart's caption at draw time, once pitch and grouping are known.
	axis  func(pitch, group, count, width int) string
	color tcell.Color
}

// newRangeChart returns an empty chart. Call setSeries or setBars before use.
func newRangeChart() *rangeChart {
	return &rangeChart{Box: tview.NewBox(), tick: 1, color: activeTheme.BarHealthy}
}

// setSeries plots data as a filled area, downsampled to the available width.
func (c *rangeChart) setSeries(data []float64, caption string) *rangeChart {
	c.data, c.bars, c.tick, c.caption = data, false, 1, caption
	return c
}

// setBars plots data as categorical bars. pitch is the widest bar cell plus
// gap to use; Draw narrows it toward 1, then groups values into shared bars.
func (c *rangeChart) setBars(data []float64, pitch int, axis func(pitch, group, count, width int) string) *rangeChart {
	c.data, c.bars, c.tick, c.axis, c.caption = data, true, max(pitch, 1), axis, ""
	return c
}

// barFit picks the bar pitch and grouping for a plot of plotW cells: the
// widest pitch up to c.tick that seats every bar, narrowing to 1 before it
// groups. group exceeds 1 only when even one cell each does not fit.
func (c *rangeChart) barFit(plotW int) (pitch, group int) {
	n := len(c.data)
	if n == 0 || plotW <= 0 {
		return c.tick, 1
	}
	if per := plotW / n; per >= 1 {
		return min(c.tick, per), 1
	}
	return 1, (n + plotW - 1) / plotW
}

// barCaption is the axis labels plus a "N per bar" note, measured in cells so the note is never clipped.
func (c *rangeChart) barCaption(pitch, group, plotW int) string {
	note := ""
	if group > 1 {
		note = fmt.Sprintf(" · %d per bar", group)
	}
	labels := ""
	if c.axis != nil {
		labels = c.axis(pitch, group, len(c.data), plotW-utf8.RuneCountInString(note))
	}
	return labels + note
}

func (c *rangeChart) setColor(col tcell.Color) *rangeChart { c.color = col; return c }

// chartMinHeight is one plot row, an axis line and a caption; below it nothing is drawn.
const chartMinHeight = 3

// Draw paints the axis, plot and caption; the y-axis gutter is sized to the widest label.
func (c *rangeChart) Draw(screen tcell.Screen) {
	c.DrawForSubclass(screen, c)
	x, y, w, h := c.GetInnerRect()
	if w <= 0 || h < chartMinHeight || len(c.data) == 0 {
		return
	}
	lo, hi, _ := dataRange(c.data)

	plotRows := h - 2 // one axis line, one caption line
	labels := axisLabels(plotRows, lo, hi)
	baseline := fmt.Sprintf("%.0f", lo)
	// The axis line prints the baseline, so blank a last row that rounds to it.
	if n := len(labels); n > 0 && labels[n-1] == baseline {
		labels[n-1] = ""
	}
	gutter := len(baseline)
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
		pitch, group := c.barFit(plotW)
		// The scale stays the full range; the caption reports grouping.
		rows = barRows(bucketMax(c.data, group), pitch, plotRows, lo, hi)
		caption = c.barCaption(pitch, group, plotW)
	} else {
		rows = seriesRows(c.data, plotW, plotRows, lo, hi)
	}

	muted := activeTheme.Muted
	for r, line := range rows {
		lbl := fmt.Sprintf("%*s ", gutter-2, labels[r])
		tview.Print(screen, lbl, x, y+r, gutter, tview.AlignLeft, muted)
		tview.Print(screen, "┤", x+gutter-1, y+r, 1, tview.AlignLeft, activeTheme.Accent)
		// Clip by runes: the block glyphs are three bytes each.
		cells := []rune(line)
		if len(cells) > plotW {
			cells = cells[:plotW]
		}
		tview.Print(screen, string(cells), x+gutter, y+r, plotW, tview.AlignLeft, c.color)
	}

	base := fmt.Sprintf("%*s ", gutter-2, baseline)
	tview.Print(screen, base, x, y+plotRows, gutter, tview.AlignLeft, muted)
	tview.Print(screen, "└"+strings.Repeat("─", plotW-1), x+gutter-1, y+plotRows, plotW, tview.AlignLeft, muted)
	if caption != "" {
		tview.Print(screen, caption, x+gutter, y+plotRows+1, plotW, tview.AlignLeft, muted)
	}
}
