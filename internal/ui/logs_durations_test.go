// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// durationLine returns the Logs tab's "Estimated duration" line, or "".
func durationLine(r *smart.Report) string {
	for line := range strings.SplitSeq(buildLogsText(r), "\n") {
		if strings.Contains(line, "Estimated duration") {
			return line
		}
	}
	return ""
}

// pollingReport is an ATA report advertising only the given polling minutes.
func pollingReport(t *testing.T, polling string) *smart.Report {
	t.Helper()
	var r smart.Report
	doc := `{"device":{"protocol":"ATA"},"ata_smart_data":{"self_test":{"polling_minutes":` + polling + `}}}`
	if err := json.Unmarshal([]byte(doc), &r); err != nil {
		t.Fatalf("parse %s: %v", polling, err)
	}
	return &r
}

// TestSelfTestDurationsOmitUnreported: a polling time the drive does not
// report is absent from the line, and only a reported zero renders as 0 min.
func TestSelfTestDurationsOmitUnreported(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "smart", "testdata", "smart-sdb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var samsung smart.Report
	if err := json.Unmarshal(data, &samsung); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		report *smart.Report
		want   string
	}{
		{"samsung fixture has no conveyance", &samsung, "Estimated duration: short 2 min · extended ~4 h"},
		{"all three", pollingReport(t, `{"short":1,"extended":1804,"conveyance":2}`),
			"Estimated duration: short 1 min · extended ~30 h · conveyance 2 min"},
		{"extended only", pollingReport(t, `{"extended":120}`), "Estimated duration: extended ~2 h"},
		{"reported zero is kept", pollingReport(t, `{"short":0,"extended":0,"conveyance":5}`),
			"Estimated duration: short 0 min · extended 0 min · conveyance 5 min"},
		{"none reported", pollingReport(t, `{}`), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.TrimSpace(durationLine(tt.report)); got != tt.want {
				t.Errorf("duration line = %q, want %q", got, tt.want)
			}
		})
	}
}
