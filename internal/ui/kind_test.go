// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// kindFixture parses one captured smartctl envelope.
func kindFixture(t *testing.T, name string) *smart.Report {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var rep smart.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return &rep
}

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
		r := kindFixture(t, c.fixture)
		if got := shortKind(r); got != c.short {
			t.Errorf("%s: shortKind = %q, want %q", c.fixture, got, c.short)
		}
		if got := driveKind(r); got != c.long {
			t.Errorf("%s: driveKind = %q, want %q", c.fixture, got, c.long)
		}
	}
}
