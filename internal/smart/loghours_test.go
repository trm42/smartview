// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "testing"

// TestUnwrapLogHours pins the 16-bit stamp unwrap: lifted to the latest
// matching hour at or before now, left alone when it cannot have wrapped.
func TestUnwrapLogHours(t *testing.T) {
	for _, c := range []struct{ hours, now, want int }{
		{1000, 5000, 1000},                 // drive younger than one wrap
		{1000, 70000, 66536},               // one wrap: 1000 + 65536
		{60000, 70000, 60000},              // stamp still in the first lap
		{100, 3*65536 + 50, 2*65536 + 100}, // the lap before now's, since 100 > 50
		{-1, 70000, -1},                    // out of range stamps pass through
		{65536, 70000, 65536},
	} {
		if got := unwrapLogHours(c.hours, c.now); got != c.want {
			t.Errorf("unwrapLogHours(%d, %d) = %d, want %d", c.hours, c.now, got, c.want)
		}
	}
}

// TestLogEventHoursWithoutPowerOn: no current reading means no unwrap.
func TestLogEventHoursWithoutPowerOn(t *testing.T) {
	if got := (&Report{}).LogEventHours(1000); got != 1000 {
		t.Errorf("LogEventHours = %d, want the stamp unchanged", got)
	}
}
