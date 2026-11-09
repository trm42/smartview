// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "testing"

// TestSelfTestOutcome pins the keyword rules the Logs summary and row colours share.
func TestSelfTestOutcome(t *testing.T) {
	for want, cases := range map[SelfTestVerdict][]string{
		SelfTestPassed: {"Completed without error", "Completed"},
		// "Completed" alone is not a pass: smartctl reports failures as "Completed: read failure" and the like.
		SelfTestFailed: {
			"Completed: read failure",
			"Completed: electrical failure",
			"Completed: unknown failure",
			"Completed: handling damage??",
			"Fatal or unknown error",
			"Aborted: Self-test failed segment",
		},
		// A run still going, or one the host stopped, says nothing about the drive.
		SelfTestNeutral: {
			"Self-test routine in progress",
			"Aborted by host",
			"Interrupted (host reset)",
			"Aborted: Controller Reset",
		},
	} {
		for _, s := range cases {
			if got := SelfTestOutcome(s); got != want {
				t.Errorf("SelfTestOutcome(%q) = %d, want %d", s, got, want)
			}
		}
	}
}
