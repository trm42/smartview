// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "testing"

// TestKind pins that an SSD is a stated zero rotation rate, never an absent one.
func TestKind(t *testing.T) {
	cases := []struct {
		fixture string
		kind    DriveKind
		rpm     int
		ok      bool
	}{
		{"smart-sda.json", KindHDD, 7200, true},
		{"smart-sdc-failing.json", KindHDD, 7200, true},
		{"smart-sdb.json", KindSSD, 0, true},
		{"smart-nvme.json", KindNVMe, 0, true},
		{"smart-apple-nvme.json", KindNVMe, 0, true},
		{"smart-sde-nodata.json", 0, 0, false},
		{"smart-sdd-standby.json", 0, 0, false},
	}
	for _, c := range cases {
		kind, rpm, ok := parseFixture(t, c.fixture).Kind()
		if kind != c.kind || rpm != c.rpm || ok != c.ok {
			t.Errorf("%s: Kind = (%d,%d,%v), want (%d,%d,%v)", c.fixture, kind, rpm, ok, c.kind, c.rpm, c.ok)
		}
	}
	if _, _, ok := (&Report{Device: Device{Protocol: "SCSI"}}).Kind(); ok {
		t.Error("a protocol we do not classify reported a kind")
	}
}
