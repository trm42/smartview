// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "testing"

func TestPctUsedSeverity(t *testing.T) {
	cases := map[int]Severity{0: SeverityOK, 89: SeverityOK, 90: SeverityCaution, 100: SeverityCaution}
	for v, want := range cases {
		if got := PctUsedSeverity(v); got != want {
			t.Errorf("PctUsedSeverity(%d) = %v, want %v", v, got, want)
		}
	}
}

func TestNvmeSeverity(t *testing.T) {
	cases := []struct {
		name string
		h    NVMeHealth
		want Severity
	}{
		{"healthy", NVMeHealth{}, SeverityOK},
		{"media errors", NVMeHealth{MediaErrors: 1}, SeverityCaution},
		{"spare above thresh", NVMeHealth{AvailableSpare: new(20), AvailableSpareThreshold: new(10)}, SeverityOK},
		{"spare at thresh", NVMeHealth{AvailableSpare: new(10), AvailableSpareThreshold: new(10)}, SeverityFailing},
		{"spare below thresh", NVMeHealth{AvailableSpare: new(5), AvailableSpareThreshold: new(10)}, SeverityFailing},
		{"pct used 100", NVMeHealth{PercentageUsed: new(100)}, SeverityCaution},
		{"spare failing wins over media", NVMeHealth{MediaErrors: 3, AvailableSpare: new(1), AvailableSpareThreshold: new(10)}, SeverityFailing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nvmeSeverity(&c.h); got != c.want {
				t.Errorf("nvmeSeverity = %v, want %v", got, c.want)
			}
		})
	}
}

func TestOverall(t *testing.T) {
	failingAttr := ATAAttributes{Table: []ATAAttribute{{ID: 5, WhenFailed: "FAILING_NOW"}}}
	cautionAttr := ATAAttributes{Table: []ATAAttribute{{ID: 5, Value: 5, Thresh: 10, Flags: ATAFlags{Prefailure: false}}}}
	cases := []struct {
		name string
		r    Report
		want Severity
	}{
		{"smart status failed", Report{SmartStatus: SmartStatus{Passed: false}}, SeverityFailing},
		{"nvme critical warning", Report{
			Device:      Device{Protocol: "NVMe"},
			SmartStatus: SmartStatus{Passed: true},
			NVMeHealth:  &NVMeHealth{CriticalWarning: 0x04},
		}, SeverityFailing},
		{"ata failing attr", Report{
			Device: Device{Protocol: "ATA"}, SmartStatus: SmartStatus{Passed: true}, ATAAttributes: &failingAttr,
		}, SeverityFailing},
		{"ata old-age caution", Report{
			Device: Device{Protocol: "ATA"}, SmartStatus: SmartStatus{Passed: true}, ATAAttributes: &cautionAttr,
		}, SeverityCaution},
		{"healthy nvme", Report{
			Device: Device{Protocol: "NVMe"}, SmartStatus: SmartStatus{Passed: true}, NVMeHealth: &NVMeHealth{},
		}, SeverityOK},
		// The attribute table can stay in range while the error log is populated.
		{"ata logged error", Report{
			Device: Device{Protocol: "ATA"}, SmartStatus: SmartStatus{Passed: true},
			ATAErrorLog: &ATAErrorLog{Extended: &ATAErrorLogExtended{Count: 2}},
		}, SeverityCaution},
		{"ata empty error log", Report{
			Device: Device{Protocol: "ATA"}, SmartStatus: SmartStatus{Passed: true},
			ATAErrorLog: &ATAErrorLog{Extended: &ATAErrorLogExtended{Count: 0}},
		}, SeverityOK},
		{"ata pending defects", Report{
			Device: Device{Protocol: "ATA"}, SmartStatus: SmartStatus{Passed: true},
			ATAPendingDefects: &ATAPendingDefects{Count: 1},
		}, SeverityCaution},
		// NVMe error-log entries accumulate benignly and must not grade the drive.
		{"nvme error log entries do not alarm", Report{
			Device: Device{Protocol: "NVMe"}, SmartStatus: SmartStatus{Passed: true},
			NVMeHealth: &NVMeHealth{NumErrLogEntries: 3},
		}, SeverityOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.Overall(); got != c.want {
				t.Errorf("Overall = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCurrentTemp(t *testing.T) {
	if v, ok := (&Report{Temperature: &Temperature{Current: new(42)}}).CurrentTemp(); !ok || v != 42 {
		t.Errorf("primary temp = %d,%v want 42,true", v, ok)
	}
	if v, ok := (&Report{NVMeHealth: &NVMeHealth{Temperature: new(50)}}).CurrentTemp(); !ok || v != 50 {
		t.Errorf("nvme fallback temp = %d,%v want 50,true", v, ok)
	}
	if _, ok := (&Report{}).CurrentTemp(); ok {
		t.Error("no temp should report ok=false")
	}
}

func TestSupportsFARM(t *testing.T) {
	cases := []struct {
		name, proto, model, family string
		want                       bool
	}{
		{"nvme", "NVMe", "ST500", "Seagate", false},
		{"ata seagate family", "ATA", "WhateverModel", "Seagate BarraCuda", true},
		{"ata ST prefix", "ATA", "ST22000NT001", "", true},
		{"ata ST lower trimmed", "ATA", "  st2000 ", "", true},
		{"ata non-seagate", "ATA", "WDC WD40", "Western Digital", false},
		{"ata empty", "ATA", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Report{Device: Device{Protocol: c.proto}, ModelName: c.model, ModelFamily: c.family}
			if got := r.SupportsFARM(); got != c.want {
				t.Errorf("SupportsFARM = %v, want %v", got, c.want)
			}
		})
	}
}

func TestHasFARM(t *testing.T) {
	if (&Report{}).HasFARM() {
		t.Error("nil FARM should be false")
	}
	if (&Report{FARM: &FARM{Supported: false}}).HasFARM() {
		t.Error("unsupported FARM should be false")
	}
	if !(&Report{FARM: &FARM{Supported: true}}).HasFARM() {
		t.Error("supported FARM should be true")
	}
}

func TestFatalMessage(t *testing.T) {
	none := &Report{}
	if _, ok := none.FatalMessage(); ok {
		t.Error("no messages should be false")
	}
	mixed := &Report{Smartctl: Smartctl{Messages: []Message{
		{String: "info note", Severity: "information"},
		{String: "permission denied", Severity: "error"},
	}}}
	if msg, ok := mixed.FatalMessage(); !ok || msg != "permission denied" {
		t.Errorf("FatalMessage = %q,%v want permission denied,true", msg, ok)
	}
	// Apple internal NVMe's log-read failure is a platform limitation and must be filtered.
	apple := &Report{Smartctl: Smartctl{Messages: []Message{
		{String: "Read 1 entries from Error Information Log failed: GetLogPage failed: system=0x38, sub=0x0, code=745", Severity: "error"},
	}}}
	if msg, ok := apple.FatalMessage(); ok {
		t.Errorf("benign GetLogPage message should be filtered, got %q", msg)
	}
}

// TestFailingFixture exercises the severity path end to end against
// smart-sdc-failing.json, the one unhealthy fixture in testdata.
func TestFailingFixture(t *testing.T) {
	r := parseFixture(t, "smart-sdc-failing.json")

	if got := r.Overall(); got != SeverityFailing {
		t.Errorf("Overall = %v, want Failing", got)
	}
	if r.SmartStatus.Passed {
		t.Error("fixture should carry a failed SMART self-assessment")
	}

	byID := map[int]*ATAAttribute{}
	for i := range r.ATAAttributes.Table {
		a := &r.ATAAttributes.Table[i]
		byID[a.ID] = a
	}
	// A pre-fail attribute below its threshold is the failing case.
	if got := byID[5].Severity(); got != SeverityFailing {
		t.Errorf("attribute 5 severity = %v, want Failing", got)
	}
	// A past threshold dip is a caution.
	if got := byID[197].Severity(); got != SeverityCaution {
		t.Errorf("attribute 197 severity = %v, want Caution", got)
	}
	// Severity is per-attribute, so a healthy one on the same drive stays OK.
	if got := byID[12].Severity(); got != SeverityOK {
		t.Errorf("attribute 12 severity = %v, want OK", got)
	}

	e := r.ErrorCounts()
	for name, got := range map[string]*int64{
		"Reallocated": e.Reallocated, "Pending": e.Pending,
		"Uncorrectable": e.Uncorrectable, "CRCErrors": e.CRCErrors,
	} {
		if got == nil {
			t.Errorf("%s should be reported by this drive", name)
			continue
		}
		if *got == 0 {
			t.Errorf("%s = 0, want a nonzero counter", name)
		}
	}
	if e.Worst() != SeverityCaution {
		t.Errorf("ErrorCounts.Worst = %v, want Caution", e.Worst())
	}
}

// TestFailingFixtureLogsAlone checks the error log escalates a drive whose attributes are all in range.
func TestFailingFixtureLogsAlone(t *testing.T) {
	r := parseFixture(t, "smart-sda-errors.json")
	if !r.SmartStatus.Passed {
		t.Fatal("this fixture is meant to pass its self-assessment")
	}
	for i := range r.ATAAttributes.Table {
		if a := &r.ATAAttributes.Table[i]; a.Severity() != SeverityOK {
			t.Fatalf("attribute %d is not OK; this fixture should isolate the log", a.ID)
		}
	}
	if got := r.Overall(); got != SeverityCaution {
		t.Errorf("Overall = %v, want Caution from the error log alone", got)
	}
}
