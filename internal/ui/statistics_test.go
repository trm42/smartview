// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"
)

// TestHangingIndent pins the fix for a broken two-column grid: tview's own
// wrapping returned an overflowing value to column 0, so "15.4 TB (30003609491
// sectors)" split with "sectors)" against the left border, between two rows that
// were still aligned. The overflow now hangs under the value column.
func TestHangingIndent(t *testing.T) {
	line := "  " + padRight("Logical Sectors Read", statLabelWidth) + " 193.6 TB (378186418521 sectors)"
	got := hangingIndent(line, statValueCol, 60)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected the long value to wrap, got:\n%s", got)
	}
	for _, l := range lines[1:] {
		if strings.TrimLeft(l, " ") == l {
			t.Errorf("continuation is not indented: %q", l)
		}
		if n := len(l) - len(strings.TrimLeft(l, " ")); n != statValueCol {
			t.Errorf("continuation indent = %d, want the value column %d", n, statValueCol)
		}
	}
	// A line that fits is returned untouched.
	short := "  " + padRight("Power-on Hours", statLabelWidth) + " 1 y 28 d"
	if got := hangingIndent(short, statValueCol, 120); got != short {
		t.Errorf("short line was rewritten:\n%q\n%q", short, got)
	}
	// An implausibly narrow pane gives up rather than indenting into nothing.
	if got := hangingIndent(line, statValueCol, 20); got != line {
		t.Error("a pane too narrow to hang-indent should return the text unchanged")
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
