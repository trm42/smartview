// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"
)

// The scaling is a pure function, so unlike the widgets it replaces it can be
// tested. These cases pin the behaviour the old charts got wrong.

// TestSeriesRowsScalesToRangeNotZero is the whole reason this file exists: the
// Seagate fixture's SCT history lives between 35°C and 40°C, and a zero-anchored
// renderer drew it as a solid block. Scaled to its own range it must use the
// full height and leave the top and bottom rows distinguishable.
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
