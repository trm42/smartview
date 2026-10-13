// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "testing"

// TestKind pins that an absent rotation rate names an SSD only on flash-wear evidence.
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
	// An SSD that omits rotation_rate (identify word 217 = 0) is still named by its wear attribute.
	ssd := parseFixture(t, "smart-sdb.json")
	ssd.RotationRate = nil
	if kind, _, ok := ssd.Kind(); kind != KindSSD || !ok {
		t.Errorf("SSD without rotation rate: Kind = (%d,%v), want SSD", kind, ok)
	}
	hdd := parseFixture(t, "smart-sda.json")
	hdd.RotationRate = nil
	if _, _, ok := hdd.Kind(); ok {
		t.Error("HDD without rotation rate reported a kind")
	}
	if _, _, ok := (&Report{Device: Device{Protocol: "SCSI"}}).Kind(); ok {
		t.Error("a protocol we do not classify reported a kind")
	}
}
