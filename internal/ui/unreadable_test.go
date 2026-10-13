// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/trm42/smartview/internal/smart"
)

// staleGlyph is spelled out so the tests pin the rendered mark, not a constant.
const staleGlyph = "⊘"

// goneUnreadable gives dev a good reading 12 minutes old, then applies fail.
func goneUnreadable(t *testing.T, width int, fail pollResult) (*App, smart.Device) {
	t.Helper()
	a, _ := newSimApp(t, width, 40)
	a.applyLayout(width < narrowBreakpoint)
	other := smart.Device{Name: "/dev/sda", Type: "sat"}
	d := smart.Device{Name: "/dev/sdb", Type: "sat"}
	a.devices = []smart.Device{other, d}
	a.fleet.lastWidth = width

	a.applyPoll(map[string]pollResult{
		other.Name: {rep: healthyReport(other.Name)},
		d.Name:     {rep: healthyReport(d.Name)},
	})
	a.list.SetCurrentItem(1)
	if got := surfaces(a, d); strings.Contains(got, staleGlyph) {
		t.Fatalf("a freshly read drive is marked unreadable: %q", got)
	}
	if got := a.detail.note.GetText(true); strings.TrimSpace(got) != "" {
		t.Fatalf("a freshly read drive carries a caveat note: %q", got)
	}

	a.lastRead[d.Name] = time.Now().Add(-12 * time.Minute)
	a.applyPoll(map[string]pollResult{
		other.Name: {rep: healthyReport(other.Name)},
		d.Name:     fail,
	})
	return a, d
}

// surfaces joins every place a drive's state is summarised outside the detail note.
func surfaces(a *App, d smart.Device) string {
	_, sec := a.listRow(d)
	return strings.Join([]string{
		sec, a.list.GetTitle(), a.rail.GetText(true), fleetText(a), a.fleet.legend.GetText(true),
	}, "\n")
}

// fleetText is the fleet table's identity column, one row per line.
func fleetText(a *App) string {
	var b strings.Builder
	for r := range a.fleet.table.GetRowCount() {
		if cell := a.fleet.table.GetCell(r, 0); cell != nil {
			b.WriteString(cell.Text + "\n")
		}
	}
	return b.String()
}

// assertMarkedStale checks every sink that would otherwise present the cached reading as current.
func assertMarkedStale(t *testing.T, a *App, d smart.Device) {
	t.Helper()
	if !a.reports[d.Name].HasHealth() {
		t.Fatal("the failed read replaced the last good report")
	}
	if _, sec := a.listRow(d); !strings.Contains(sec, staleGlyph) {
		t.Errorf("drive-list row has no unreadable mark: %q", sec)
	}
	if a.narrow {
		if got := a.rail.GetText(true); !strings.Contains(got, staleGlyph+" 1") {
			t.Errorf("rail does not count the unreadable drive: %q", got)
		}
	} else if got := a.list.GetTitle(); !strings.Contains(got, staleGlyph+" 1") {
		t.Errorf("list title does not count the unreadable drive: %q", got)
	}
	note := a.detail.note.GetText(true)
	if !strings.Contains(note, staleGlyph) || !strings.Contains(note, "12m") {
		t.Errorf("detail note %q does not mark and date the stale reading", note)
	}
	var marked int
	for line := range strings.SplitSeq(fleetText(a), "\n") {
		if strings.Contains(line, staleGlyph) {
			marked++
			if !strings.Contains(line, "sdb") {
				t.Errorf("fleet marks the wrong row: %q", line)
			}
		}
	}
	if marked != 1 {
		t.Errorf("%d fleet rows carry the unreadable mark, want exactly 1", marked)
	}
	if got := a.fleet.legend.GetText(true); !strings.Contains(got, staleGlyph) {
		t.Errorf("fleet legend %q does not explain the unreadable mark", got)
	}
}

// assertReadClears checks that a good read removes every mark.
func assertReadClears(t *testing.T, a *App, d smart.Device) {
	t.Helper()
	a.applyPoll(map[string]pollResult{d.Name: {rep: healthyReport(d.Name)}})
	if got := surfaces(a, d); strings.Contains(got, staleGlyph) {
		t.Errorf("a drive that answered again is still marked unreadable: %q", got)
	}
	if got := a.detail.note.GetText(true); strings.TrimSpace(got) != "" {
		t.Errorf("a drive that answered again still carries a caveat note: %q", got)
	}
}

var layoutWidths = map[string]int{"wide": 120, "narrow": 80}

// TestFailedOpenMarksTheReadingStale: a kept reading with no mark presents a dead drive as healthy.
func TestFailedOpenMarksTheReadingStale(t *testing.T) {
	for name, width := range layoutWidths {
		t.Run(name, func(t *testing.T) {
			a, d := goneUnreadable(t, width, pollResult{rep: noDataReport("/dev/sdb")})
			assertMarkedStale(t, a, d)
			assertReadClears(t, a, d)
		})
	}
}

// TestStandbyIsNotMarkedUnreadable: a parked drive is asleep, not failed.
func TestStandbyIsNotMarkedUnreadable(t *testing.T) {
	a, d := goneUnreadable(t, 120, pollResult{standby: true})
	if got := surfaces(a, d); strings.Contains(got, staleGlyph) {
		t.Errorf("a spun-down drive is marked unreadable: %q", got)
	}
}

// TestNeverReadableDriveIsNotStale: with no good reading cached there is nothing stale to flag.
func TestNeverReadableDriveIsNotStale(t *testing.T) {
	a, _ := newSimApp(t, 120, 40)
	d := smart.Device{Name: "/dev/sde", Type: "sat"}
	a.devices = []smart.Device{d}
	// The failed fetch sits between two envelopes: a mark there would flap.
	for i, res := range []pollResult{
		{rep: noDataReport(d.Name)}, {rep: noDataReport(d.Name)}, outcome(nil), {rep: noDataReport(d.Name)},
	} {
		a.applyPoll(map[string]pollResult{d.Name: res})
		if got := surfaces(a, d); strings.Contains(got, staleGlyph) {
			t.Errorf("poll %d: a drive that never had a verdict is marked stale: %q", i, got)
		}
		if got := a.detail.note.GetText(true); strings.TrimSpace(got) != "" {
			t.Errorf("poll %d: a drive that never had a verdict carries a caveat note: %q", i, got)
		}
	}
}
