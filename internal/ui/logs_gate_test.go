// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// An ata_smart_data section admits the Logs tab only when it adds a line to the body.
func TestLogsTabGatedOnRenderedSmartData(t *testing.T) {
	empty := buildLogsText(&smart.Report{})
	cases := []struct {
		name, envelope string
		want           bool
	}{
		{"empty section", `{"ata_smart_data":{}}`, false},
		{"empty self_test", `{"ata_smart_data":{"self_test":{}}}`, false},
		{"status only", `{"ata_smart_data":{"self_test":{"status":{"value":0,"string":"completed without error"}}}}`, false},
		{"capabilities only", `{"ata_smart_data":{"capabilities":{"self_tests_supported":true}}}`, false},
		{"polling minutes", `{"ata_smart_data":{"self_test":{"polling_minutes":{"short":2,"extended":90}}}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := decodeReport(t, c.envelope)
			got := hasLogs(r)
			if got != c.want {
				t.Errorf("hasLogs = %v, want %v", got, c.want)
			}
			if renders := buildLogsText(r) != empty; got != renders {
				t.Errorf("hasLogs = %v, but the section adds to the body = %v", got, renders)
			}
		})
	}
}

// Every committed fixture keeps the Logs tab it has content for.
func TestLogsTabAvailabilityPerFixture(t *testing.T) {
	want := map[string]bool{
		"smart-apple-nvme.json":       false,
		"smart-nvme-errors.json":      true,
		"smart-nvme.json":             true,
		"smart-sda-errors.json":       true,
		"smart-sda.json":              true,
		"smart-sdb.json":              true,
		"smart-sdc-failing.json":      true,
		"smart-sdd-standby.json":      false,
		"smart-sde-nodata.json":       false,
		"smart-seagate-farm-log.json": false,
	}
	for name, w := range want {
		data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got := hasLogs(decodeReport(t, string(data))); got != w {
			t.Errorf("%s: hasLogs = %v, want %v", name, got, w)
		}
	}
}
