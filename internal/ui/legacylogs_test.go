// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestLogsTabRendersLegacyLogs: the Logs tab shows the legacy summary error log
// and standard self-test log a drive without GP logs 0x03/0x07 reports.
func TestLogsTabRendersLegacyLogs(t *testing.T) {
	// Synthetic: the layout of smartmontools' PrintSmartErrorlog and ataPrintSmartSelfTestlog.
	const raw = `{"device":{"protocol":"ATA"},"power_on_time":{"hours":9000},
		"ata_smart_error_log":{"summary":{"revision":1,"count":7,"logged_count":5,"table":[
			{"error_number":7,"lifetime_hours":8100,
			 "error_description":"Error: UNC at LBA = 0x00123456 = 1193046"}]}},
		"ata_smart_self_test_log":{"standard":{"revision":1,"table":[
			{"type":{"value":2,"string":"Extended offline"},
			 "status":{"value":112,"string":"Completed: read failure","passed":false},
			 "lifetime_hours":8200,"lba":1193046}],
			"count":1,"error_count_total":1,"error_count_outdated":0}}}`
	var r smart.Report
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	got := buildLogsText(&r)
	for _, want := range []string{"7 errors logged", "#7", "UNC", "Extended offline", "Completed: read failure", "1 run failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in logs text, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "no self-test log") {
		t.Errorf("legacy self-test log rendered as absent:\n%s", got)
	}
}

// TestLogsTabPrefersExtendedSelfTestLog: with both shapes present the GP log is the one drawn.
func TestLogsTabPrefersExtendedSelfTestLog(t *testing.T) {
	const raw = `{"device":{"protocol":"ATA"},
		"ata_smart_self_test_log":{
			"extended":{"table":[{"type":{"string":"Short offline"},"status":{"string":"Completed without error"}}]},
			"standard":{"table":[{"type":{"string":"Conveyance offline"},"status":{"string":"Completed without error"}}]}}}`
	var r smart.Report
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	got := buildLogsText(&r)
	if !strings.Contains(got, "Short offline") || strings.Contains(got, "Conveyance offline") {
		t.Errorf("want the extended table only, got:\n%s", got)
	}
}
