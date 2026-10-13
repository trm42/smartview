// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import "testing"

// TestUnreadDriveHasNoKind: an envelope with no rotation rate names no kind,
// in the fleet column or the Overview Type row.
func TestUnreadDriveHasNoKind(t *testing.T) {
	cases := []struct {
		fixture     string
		short, long string
	}{
		{"smart-sde-nodata.json", dash, dash},
		{"smart-sdd-standby.json", dash, dash},
		{"smart-sdb.json", "SSD", "SATA SSD"},
		{"smart-sda.json", "HDD", "HDD @ 7200 rpm"},
		{"smart-nvme.json", "NVMe", "NVMe SSD"},
		{"smart-apple-nvme.json", "NVMe", "NVMe SSD"},
	}
	for _, c := range cases {
		r := fixtureReport(t, c.fixture)
		if got := shortKind(r); got != c.short {
			t.Errorf("%s: shortKind = %q, want %q", c.fixture, got, c.short)
		}
		if got := driveKind(r); got != c.long {
			t.Errorf("%s: driveKind = %q, want %q", c.fixture, got, c.long)
		}
	}
}
