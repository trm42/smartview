// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "testing"

// legacyLogsJSON is synthetic: the layout smartmontools' PrintSmartErrorlog and
// ataPrintSmartSelfTestlog (ataprint.cpp) emit when GP logs 0x03/0x07 are absent.
const legacyLogsJSON = `{"device":{"protocol":"ATA"},"smart_status":{"passed":true},
	"power_on_time":{"hours":9000},
	"ata_smart_error_log":{"summary":{"revision":1,"count":7,"logged_count":5,"table":[
		{"error_number":7,"lifetime_hours":8100,
		 "completion_registers":{"error":64,"status":81,"count":0,"lba":1193046,"device":224},
		 "error_description":"Error: UNC at LBA = 0x00123456 = 1193046"},
		{"error_number":6,"lifetime_hours":8099,
		 "completion_registers":{"error":4,"status":81,"count":0,"lba":0,"device":160}}]}},
	"ata_smart_self_test_log":{"standard":{"revision":1,"table":[
		{"type":{"value":2,"string":"Extended offline"},
		 "status":{"value":112,"string":"Completed: read failure","passed":false},
		 "lifetime_hours":8200,"lba":1193046},
		{"type":{"value":1,"string":"Short offline"},
		 "status":{"value":0,"string":"Completed without error","passed":true},
		 "lifetime_hours":8000}],
		"count":2,"error_count_total":1,"error_count_outdated":0}}}`

// TestLegacyErrorLogGradesTheDrive: errors logged in the legacy summary log are
// graded and counted exactly as the same count under "extended".
func TestLegacyErrorLogGradesTheDrive(t *testing.T) {
	r := decode(t, legacyLogsJSON)
	if got := r.Overall(); got != SeverityCaution {
		t.Errorf("Overall = %v, want %v", got, SeverityCaution)
	}
	n := r.ErrorCounts().ErrorLogEntries
	if n == nil {
		t.Fatal("ErrorLogEntries absent, want 7")
	}
	if *n != 7 {
		t.Errorf("ErrorLogEntries = %d, want 7", *n)
	}
}

// TestLegacyEmptyErrorLogIsClean: an empty legacy log is a reported zero, not an absent reading.
func TestLegacyEmptyErrorLogIsClean(t *testing.T) {
	r := decode(t, `{"device":{"protocol":"ATA"},"smart_status":{"passed":true},
		"ata_smart_error_log":{"summary":{"revision":1,"count":0}}}`)
	if got := r.Overall(); got != SeverityOK {
		t.Errorf("Overall = %v, want %v", got, SeverityOK)
	}
	if n := r.ErrorCounts().ErrorLogEntries; n == nil || *n != 0 {
		t.Errorf("ErrorLogEntries = %v, want a reported 0", n)
	}
}

// TestExtendedErrorLogWinsOverLegacy: with both shapes present the GP log answers.
func TestExtendedErrorLogWinsOverLegacy(t *testing.T) {
	r := decode(t, `{"device":{"protocol":"ATA"},
		"ata_smart_error_log":{"extended":{"count":3},"summary":{"count":5}}}`)
	if n := r.ErrorCounts().ErrorLogEntries; n == nil || *n != 3 {
		t.Errorf("ErrorLogEntries = %v, want 3", n)
	}
}

// TestATASelfTestsResolvesEitherShape pins the (table, ok) pair for each way the section can arrive.
func TestATASelfTestsResolvesEitherShape(t *testing.T) {
	const ext = `"extended":{"table":[{"type":{"string":"Short offline"},"lifetime_hours":2}]}`
	const std = `"standard":{"table":[{"type":{"string":"Extended offline"},"lifetime_hours":1}]}`
	cases := []struct {
		name, section string
		wantOK        bool
		wantType      string // newest entry's type; "" for an empty table
	}{
		{"no section", ``, false, ""},
		{"neither shape", `,"ata_smart_self_test_log":{}`, false, ""},
		{"standard only", `,"ata_smart_self_test_log":{` + std + `}`, true, "Extended offline"},
		{"extended only", `,"ata_smart_self_test_log":{` + ext + `}`, true, "Short offline"},
		{"both", `,"ata_smart_self_test_log":{` + std + `,` + ext + `}`, true, "Short offline"},
		{"standard, nothing logged", `,"ata_smart_self_test_log":{"standard":{"revision":1,"count":0}}`, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tbl, ok := decode(t, `{"device":{"protocol":"ATA"}`+c.section+`}`).ATASelfTests()
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			var got string
			if len(tbl) > 0 {
				got = tbl[0].Type.String
			}
			if got != c.wantType || (c.wantType != "" && len(tbl) != 1) {
				t.Errorf("table = %+v, want one %q entry", tbl, c.wantType)
			}
		})
	}
}
