// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// loadFARM decodes the captured Seagate FARM fixture.
func loadFARM(t *testing.T) *smart.FARM {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", "smart-seagate-farm-log.json"))
	if err != nil {
		t.Fatalf("read FARM fixture: %v", err)
	}
	var w struct {
		FARM *smart.FARM `json:"seagate_farm_log"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		t.Fatalf("parse FARM fixture: %v", err)
	}
	if w.FARM == nil {
		t.Fatal("FARM fixture decoded to nil")
	}
	return w.FARM
}

// Every rendered farm line must start its value at exactly farmValueCol, or
// hangingIndent's cut lands inside the label.
func TestFarmValuesStartAtTheValueColumn(t *testing.T) {
	f := loadFARM(t)
	const marker = "[-:-:-] "
	checked := 0
	for _, box := range []struct {
		name  string
		write func(*strings.Builder, *smart.FARM)
	}{
		{"drive", writeFarmDriveInfo},
		{"errors", writeFarmErrors},
		{"environment", writeFarmEnvironment},
		{"workload", writeFarmWorkload},
	} {
		for _, line := range strings.Split(strings.TrimRight(farmBoxText(box.write, f), "\n"), "\n") {
			head, _, found := strings.Cut(line, marker)
			if !found {
				t.Errorf("%s: line is not a farmRow: %q", box.name, line)
				continue
			}
			checked++
			// The label plus the marker's trailing space is where the value begins.
			if got := tview.TaggedStringWidth(head + marker); got != farmValueCol {
				t.Errorf("%s: value starts at column %d, want farmValueCol (%d): %q",
					box.name, got, farmValueCol, line)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no farm rows rendered; the fixture or the writers have changed")
	}
	t.Logf("checked %d rendered rows against farmValueCol=%d", checked, farmValueCol)
}

// hangingIndent must hang an over-long farm value under its own column rather
// than returning it to the left margin, and must not drop any of it.
func TestFarmValuesHangUnderTheValueColumn(t *testing.T) {
	var b strings.Builder
	farmRow(&b, "Device", "aaaa bbbb cccc dddd eeee ffff")

	const innerW = 40 // valueW = 19, comfortably over hangingIndent's floor
	got := hangingIndent(b.String(), farmWrap, innerW)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("value did not wrap at innerW=%d:\n%s", innerW, got)
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, strings.Repeat(" ", farmValueCol)) {
			t.Errorf("continuation does not hang under the value column: %q", l)
		}
	}
	for _, word := range []string{"aaaa", "bbbb", "cccc", "dddd", "eeee", "ffff"} {
		if !strings.Contains(got, word) {
			t.Errorf("rewrap lost %q:\n%s", word, got)
		}
	}
}

// Below farmColumnMin the four boxes stack into one full-width column, which
// puts every row back on a single line.
func TestFarmStacksWhenTooNarrowToPair(t *testing.T) {
	f := loadFARM(t)
	v := newFarmView(&smart.Report{FARM: f})

	for _, c := range []struct {
		width   int
		stacked bool
	}{
		{60, true},   // per-column inner 26: values would shred
		{70, true},   // per-column inner 31, still under the floor
		{80, false},  // per-column inner 36: pairs, long rows wrap once
		{120, false}, // comfortable
	} {
		v.relayout(c.width)
		// The stacked column mounts all four boxes, the grid mounts two columns.
		outer, ok := v.inner.(*tview.Flex)
		if !ok {
			t.Fatalf("width %d: scroll content is %T, want *tview.Flex", c.width, v.inner)
		}
		arrangement, ok := outer.GetItem(0).(*tview.Flex)
		if !ok {
			t.Fatalf("width %d: first item is %T, want *tview.Flex", c.width, outer.GetItem(0))
		}
		if stacked := arrangement.GetItemCount() == 4; stacked != c.stacked {
			t.Errorf("width %d: stacked = %v (%d items), want %v",
				c.width, stacked, arrangement.GetItemCount(), c.stacked)
		}
		if v.contentHeight <= 0 {
			t.Errorf("width %d: content height %d", c.width, v.contentHeight)
		}
	}

	// Stacked at 60 columns nothing wraps; paired, the boxes swell to 10/8/23/17 lines.
	v.relayout(60)
	for _, b := range v.boxes {
		raw := lineCount(b.text)
		if got := lineCount(b.tv.GetText(false)); got != raw {
			t.Errorf("stacked at 60: box wrapped to %d lines, want its %d rows intact:\n%s",
				got, raw, b.tv.GetText(false))
		}
	}
}

// The boxes are kept in the grid's reading order, which the stacked column follows.
func TestFarmStackKeepsReadingOrder(t *testing.T) {
	v := newFarmView(&smart.Report{FARM: loadFARM(t)})
	want := []string{" Drive ", " Error statistics ", " Environment ", " Workload "}
	for i, b := range v.boxes {
		if got := b.tv.GetTitle(); got != want[i] {
			t.Errorf("box %d is %q, want %q", i, got, want[i])
		}
	}
}
