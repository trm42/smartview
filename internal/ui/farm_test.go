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

// Every rendered farm line must start its value at exactly farmValueCol. The
// panel pre-wraps with hangingIndent, which cuts each line at that display
// column; a label wider than farmLabelWidth would push the value past the cut
// and the wrap would land inside the label instead of under the value. This is
// the invariant that lets farm.go share format.go's implementation.
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
// than returning it to the left margin, and must not drop any of it. This is
// the behaviour farm.go previously kept its own copy of the algorithm for.
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
