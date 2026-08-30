// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The scaling is a pure function, so unlike the widgets it replaces it can be
// tested. These cases pin the behaviour the old charts got wrong.

// TestSeriesRowsScalesToRangeNotZero: a 35–40°C series scaled to its own
// range must use the full height, not draw as a zero-anchored solid block.
func TestSeriesRowsScalesToRangeNotZero(t *testing.T) {
	data := []float64{35, 36, 37, 38, 39, 40}
	rows := seriesRows(data, 6, 4, 35, 40)
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	if strings.TrimSpace(rows[0]) == "" {
		t.Errorf("top row is empty; the maximum should reach it:\n%s", strings.Join(rows, "\n"))
	}
	if strings.TrimSpace(rows[len(rows)-1]) == "" {
		t.Errorf("baseline row is empty; the minimum should sit on it:\n%s", strings.Join(rows, "\n"))
	}
	// A block-filled row on every line would be the old failure mode.
	full := 0
	for _, r := range rows {
		if !strings.Contains(r, " ") {
			full++
		}
	}
	if full == len(rows) {
		t.Errorf("every row is solid — the series was not scaled:\n%s", strings.Join(rows, "\n"))
	}
}

// TestSeriesRowsTracesTopEdge checks we draw a line, not a filled area: a single
// sample must mark exactly one cell in one row, not a column down to the floor.
func TestSeriesRowsTracesTopEdge(t *testing.T) {
	rows := seriesRows([]float64{10}, 1, 4, 0, 10)
	marked := 0
	for _, r := range rows {
		if strings.TrimSpace(r) != "" {
			marked++
		}
	}
	if marked != 1 {
		t.Errorf("want the top edge only (1 marked row), got %d:\n%s", marked, strings.Join(rows, "\n"))
	}
}

// TestDownsampleKeepsSpikes guards the choice of max-per-bucket over a mean: a
// lone hot sample is exactly what a health tool must not smooth away.
func TestDownsampleKeepsSpikes(t *testing.T) {
	data := make([]float64, 100)
	for i := range data {
		data[i] = 30
	}
	data[57] = 70
	got := downsample(data, 10)
	if len(got) != 10 {
		t.Fatalf("got %d points, want 10", len(got))
	}
	peak := 0.0
	for _, v := range got {
		peak = max(peak, v)
	}
	if peak != 70 {
		t.Errorf("spike lost in downsampling: peak %v, want 70", peak)
	}
}

// TestDownsampleShortSeriesUnchanged: a series that already fits is not touched.
func TestDownsampleShortSeriesUnchanged(t *testing.T) {
	data := []float64{1, 2, 3}
	if got := downsample(data, 10); len(got) != 3 || got[2] != 3 {
		t.Errorf("short series should pass through unchanged, got %v", got)
	}
}

// TestFlatSeriesStillDraws: "this never moved" is a real answer, and a flat
// series must not divide by zero or vanish.
func TestFlatSeriesStillDraws(t *testing.T) {
	rows := seriesRows([]float64{40, 40, 40}, 3, 3, 40, 40)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if strings.TrimSpace(strings.Join(rows, "")) == "" {
		t.Error("a flat series should still draw a baseline")
	}
}

// TestBarRowsSeparatesNeighbours checks the categorical form leaves a gap
// between bars, so twenty heads read as twenty values rather than a wall.
func TestBarRowsSeparatesNeighbours(t *testing.T) {
	rows := barRows([]float64{350, 495, 350}, 2, 3, 340, 500)
	// Cells are runes, not bytes: the block glyphs are three bytes each.
	bottom := []rune(rows[len(rows)-1])
	if len(bottom) != 6 {
		t.Fatalf("3 bars at pitch 2 should be 6 cells, got %d (%q)", len(bottom), string(bottom))
	}
	if bottom[1] != ' ' || bottom[3] != ' ' {
		t.Errorf("bars should be separated by a gap, got %q", string(bottom))
	}
}

// TestBarRowsShowsSpread is the FARM failure: resistances of 350-495 on a
// zero-anchored axis drew as identical bars. Scaled to the data, the tallest and
// shortest must differ.
func TestBarRowsShowsSpread(t *testing.T) {
	rows := barRows([]float64{350, 495}, 1, 3, 340, 500)
	top := []rune(rows[0])
	if top[0] == top[1] {
		t.Errorf("350 and 495 render identically in the top row (%q) — not scaled to the data", string(top))
	}
}

// TestDataRange reports absent for an empty series rather than inventing zero.
func TestDataRange(t *testing.T) {
	if _, _, ok := dataRange(nil); ok {
		t.Error("empty series should report no range")
	}
	lo, hi, ok := dataRange([]float64{37, 35, 40})
	if !ok || lo != 35 || hi != 40 {
		t.Errorf("dataRange = %v,%v,%v want 35,40,true", lo, hi, ok)
	}
}

// TestAxisLabelsDescendFromMax: labels run top-first and end above the baseline,
// which is printed separately on the axis line.
func TestAxisLabelsDescendFromMax(t *testing.T) {
	got := axisLabels(4, 35, 40)
	want := []string{"40", "39", "38", "36"}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("axisLabels = %v, want %v", got, want)
			break
		}
	}
}

// TestAxisLabelsDeduplicate: integer labels repeat when the range is narrow
// relative to the row count, and a repeated number reads as a rendering fault.
// Only the first row of each value is labelled.
func TestAxisLabelsDeduplicate(t *testing.T) {
	got := axisLabels(6, 35, 40)
	seen := map[string]bool{}
	for _, l := range got {
		if l == "" {
			continue
		}
		if seen[l] {
			t.Errorf("label %q repeats in %v", l, got)
		}
		seen[l] = true
	}
	if got[0] != "40" {
		t.Errorf("top label = %q, want the maximum 40", got[0])
	}
}

// drawChart renders a primitive on a simulation screen and returns its rows.
func drawChart(t *testing.T, p tview.Primitive, w, h int) []string {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(w, h)
	p.SetRect(0, 0, w, h)
	p.Draw(screen)
	screen.Show()
	cells, cw, _ := screen.GetContents()
	rows := make([]string, h)
	for y := range h {
		var b strings.Builder
		for x := range cw {
			b.WriteRune(cells[y*cw+x].Runes[0])
		}
		rows[y] = strings.TrimRight(b.String(), " ")
	}
	return rows
}

// countBars counts painted bar cells (full block or any partial).
func countBars(line string) int {
	n := 0
	for _, r := range line {
		if r == '█' || (r >= '▁' && r <= '▇') {
			n++
		}
	}
	return n
}

func headSeries(n int) []int {
	d := make([]int, n)
	for i := range d {
		d[i] = 350 + i*5
	}
	return d
}

// A chart must never drop bars off its right edge just because the default
// pitch does not fit: the pitch narrows toward one cell first. Before this,
// a 30-head drive at 60 columns painted 26 heads and said nothing -- and the
// title's range named a maximum that was never drawn, which on a health
// display is worse than a cramped chart.
func TestChartNarrowsPitchRatherThanDropBars(t *testing.T) {
	const w, h = 60, 12
	heads := headSeries(30)
	rows := drawChart(t, farmHeadChart(" heads ", heads, false), w, h)

	baseline := rows[h-4] // last plot row, above the axis line
	if got := countBars(baseline); got != len(heads) {
		t.Errorf("painted %d of %d heads at width %d:\n%s",
			got, len(heads), w, strings.Join(rows, "\n"))
	}
	// Nothing was dropped, so nothing should claim otherwise.
	if caption := rows[h-2]; strings.Contains(caption, "more") {
		t.Errorf("caption reports dropped bars when all fit: %q", caption)
	}
}

// The gap between bars is worth keeping when there is room for it; narrowing
// the pitch is a concession to width, not the new default.
func TestChartKeepsItsPitchWhenThereIsRoom(t *testing.T) {
	const w, h = 60, 12
	rows := drawChart(t, farmHeadChart(" heads ", headSeries(10), false), w, h)
	baseline := rows[h-4]
	i := strings.IndexAny(baseline, "▁▂▃▄▅▆▇█")
	if i < 0 {
		t.Fatalf("no bars drawn:\n%s", strings.Join(rows, "\n"))
	}
	// At the default pitch of 2 the cell after the first bar is a gap.
	if r := []rune(baseline[i:]); len(r) < 2 || r[1] != ' ' {
		t.Errorf("bars are touching at a width that affords a gap: %q", baseline)
	}
}

// When even one cell per bar will not fit, the chart shows what it can and
// says how many it could not -- the same contract as the fleet's dropped
// columns. Silently painting a subset would hide a failing head.
func TestChartSaysHowManyBarsItDropped(t *testing.T) {
	const w, h = 40, 10
	heads := headSeries(60)
	rows := drawChart(t, farmHeadChart(" heads ", heads, false), w, h)

	painted := countBars(rows[h-4])
	if painted >= len(heads) {
		t.Fatalf("width %d should not fit %d heads; painted %d", w, len(heads), painted)
	}
	caption := rows[h-2]
	want := fmt.Sprintf("%d more", len(heads)-painted)
	if !strings.Contains(caption, want) {
		t.Errorf("caption %q does not say %q:\n%s", caption, want, strings.Join(rows, "\n"))
	}
}

// The note about dropped bars must survive whatever width forced it; a note
// that is itself clipped would be the same failure one level up. The labels
// are built around the note for exactly this reason, so the check is that the
// count is complete and agrees with what was painted.
func TestChartDroppedNoteIsNeverItselfClipped(t *testing.T) {
	const h = 10
	heads := headSeries(60)
	sawDrop := false
	for _, w := range []int{30, 36, 40, 48, 60} {
		rows := drawChart(t, farmHeadChart(" heads ", heads, false), w, h)
		painted, caption := countBars(rows[h-4]), rows[h-2]
		if painted >= len(heads) {
			continue // wide enough for all of them; nothing to report
		}
		sawDrop = true
		want := fmt.Sprintf("· %d more", len(heads)-painted)
		if !strings.Contains(caption, want) {
			t.Errorf("width %d: painted %d of %d, caption %q lacks %q",
				w, painted, len(heads), caption, want)
		}
	}
	if !sawDrop {
		t.Fatal("no width dropped a bar; the test proves nothing")
	}
}
