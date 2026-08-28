// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestNVMeErrorCountIsEntriesNotCapacity: the Logs tab must report len(Table)
// as the error count, never the log's slot capacity (Size).
func TestNVMeErrorCountIsEntriesNotCapacity(t *testing.T) {
	r := &smart.Report{
		Device: smart.Device{Protocol: "NVMe"},
		NVMeErrorLog: &smart.NVMeErrorLog{
			Size: 256, Read: 3, Unread: 0,
			Table: []smart.NVMeErrorLogEntry{{}, {}, {}},
		},
	}
	got := buildLogsText(r)
	if !strings.Contains(got, "3 errors logged") {
		t.Errorf("want %q in logs text, got:\n%s", "3 errors logged", got)
	}
	for _, unwanted := range []string{"256", "253"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("log capacity %s leaked into the error count:\n%s", unwanted, got)
		}
	}
}

// TestNVMeUnreadComesFromSmartctl checks the unread figure is smartctl's own and
// is not derived by subtracting Read from the capacity.
func TestNVMeUnreadComesFromSmartctl(t *testing.T) {
	r := &smart.Report{
		Device: smart.Device{Protocol: "NVMe"},
		NVMeErrorLog: &smart.NVMeErrorLog{
			Size: 256, Read: 2, Unread: 1,
			Table: []smart.NVMeErrorLogEntry{{}, {}},
		},
	}
	got := buildLogsText(r)
	if !strings.Contains(got, "2 errors logged") || !strings.Contains(got, "1 not read back") {
		t.Errorf("want 2 logged and 1 unread, got:\n%s", got)
	}
}

// TestErrorCountPlural checks the "error(s)" placeholder is gone on both paths.
func TestErrorCountPlural(t *testing.T) {
	one := &smart.Report{
		Device:       smart.Device{Protocol: "NVMe"},
		NVMeErrorLog: &smart.NVMeErrorLog{Size: 256, Table: []smart.NVMeErrorLogEntry{{}}},
	}
	got := buildLogsText(one)
	if !strings.Contains(got, "1 error logged") || strings.Contains(got, "error(s)") {
		t.Errorf("want singular %q and no placeholder plural, got:\n%s", "1 error logged", got)
	}
}

// TestTidyErrorDescription trims smartctl's phrasing for a line that already
// sits under an "Error log" heading: it prefixes every entry with "Error: " and
// gives the LBA twice, in hex and again in decimal.
func TestTidyErrorDescription(t *testing.T) {
	cases := map[string]string{
		"Error: UNC at LBA = 0x0011a034 = 1155124": "UNC at LBA 1155124",
		"Error: ABRT":           "ABRT",
		"UNC at LBA = 0x00 = 0": "UNC at LBA 0",
		"":                      "",
		"IDNF":                  "IDNF",
	}
	for in, want := range cases {
		if got := tidyErrorDescription(in); got != want {
			t.Errorf("tidyErrorDescription(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSelfTestPassedSharesColorResult: the summary counts failures with the same
// keyword test that colours the rows, so the two can never disagree about one
// drive-reported string.
func TestSelfTestPassedSharesColorResult(t *testing.T) {
	for _, s := range []string{"Completed without error", "Completed"} {
		if !selfTestPassed(s) {
			t.Errorf("selfTestPassed(%q) = false, want true", s)
		}
		if !strings.Contains(colorResult(s), okTag()) {
			t.Errorf("colorResult(%q) does not read as a pass", s)
		}
	}
	// "Completed" alone is not a pass: smartctl reports failures as
	// "Completed: read failure" and the like, which a bare keyword check painted
	// green — a failed self-test rendering as a healthy one.
	for _, s := range []string{
		"Completed: read failure",
		"Completed: electrical failure",
		"Completed: unknown failure",
		"Aborted by host",
		"Interrupted (host reset)",
	} {
		if selfTestPassed(s) {
			t.Errorf("selfTestPassed(%q) = true, want false", s)
		}
		if !strings.Contains(colorResult(s), failingTag()) {
			t.Errorf("colorResult(%q) does not read as a failure", s)
		}
	}
}

// TestNVMeErrorStatusEscapedOnce pins the colorResult contract at the NVMe
// error-log sink: the keyword test runs on the original string and colorResult
// does the escaping itself, so pre-escaping the argument double-escaped a
// drive-controlled status ("[red]failed" -> "[red[[]failed").
func TestNVMeErrorStatusEscapedOnce(t *testing.T) {
	r := &smart.Report{
		Device: smart.Device{Protocol: "NVMe"},
		NVMeErrorLog: &smart.NVMeErrorLog{
			Size: 256, Read: 1,
			Table: []smart.NVMeErrorLogEntry{
				{ErrorCount: 1, StatusField: smart.StringValue{String: "[red]failed"}},
			},
		},
	}
	got := buildLogsText(r)
	want := esc("[red]failed")
	if !strings.Contains(got, want) {
		t.Errorf("want singly-escaped %q in logs text, got:\n%s", want, got)
	}
	if strings.Contains(got, esc(want)) {
		t.Errorf("status was escaped twice:\n%s", got)
	}
	// The keyword test must have run on the original, not the escaped copy.
	if !strings.Contains(got, failingTag()) {
		t.Errorf("a %q status should read as a failure:\n%s", "failed", got)
	}
}

// TestAllClearLinesAreNotGreen: colour marks exceptions, not membership. The
// "nothing to report" lines render only when nothing is wrong, so tinting them
// green made a healthy Logs tab a wall of green and left nothing to notice.
func TestAllClearLinesAreNotGreen(t *testing.T) {
	r := &smart.Report{
		Device:            smart.Device{Protocol: "NVMe"},
		NVMeErrorLog:      &smart.NVMeErrorLog{Size: 256},
		ATAPendingDefects: &smart.ATAPendingDefects{Count: 0, Size: 32},
		SATAPhyEvents: &smart.SATAPhyEvents{Table: []smart.SATAPhyCounter{
			{ID: 1, Name: "Command failed due to ICRC error", Value: 0},
		}},
	}
	got := buildLogsText(r)
	for _, line := range strings.Split(got, "\n") {
		if !strings.Contains(line, okTag()) {
			continue
		}
		t.Errorf("all-clear line is tinted with the healthy colour: %q", line)
	}
	// The lines themselves must still be there — neutral, not dropped.
	for _, want := range []string{"No errors logged", "No pending defects", "No link events logged"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in logs text, got:\n%s", want, got)
		}
	}
}
