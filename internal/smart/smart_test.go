// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// parseFixture decodes a captured smartctl report from testdata.
func parseFixture(t *testing.T, name string) *Report {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var rep Report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return &rep
}

func TestParseATA(t *testing.T) {
	r := parseFixture(t, "smart-sda.json")
	if !r.IsATA() {
		t.Fatalf("protocol = %q, want ATA", r.Device.Protocol)
	}
	if r.ModelName != "ST22000NT001-3LS101" {
		t.Errorf("model = %q", r.ModelName)
	}
	if !r.SmartStatus.Passed {
		t.Error("expected SMART status passed")
	}
	if r.ATAAttributes == nil || len(r.ATAAttributes.Table) == 0 {
		t.Fatal("expected ATA attribute table")
	}
	// The Raw_Read_Error_Rate attribute is pre-failure on Seagate drives.
	var found bool
	for _, a := range r.ATAAttributes.Table {
		if a.ID == 1 {
			found = true
			if !a.Flags.Prefailure {
				t.Error("attribute 1 should be flagged prefailure")
			}
		}
	}
	if !found {
		t.Error("attribute ID 1 not found")
	}
	if r.ATATemperatureHistory == nil || len(r.ATATemperatureHistory.Table) == 0 {
		t.Error("expected SCT temperature history for sparkline")
	}
	if temp, ok := r.CurrentTemp(); !ok || temp <= 0 {
		t.Errorf("CurrentTemp = %d, %v", temp, ok)
	}
}

func TestParseNVMe(t *testing.T) {
	r := parseFixture(t, "smart-nvme.json")
	if !r.IsNVMe() {
		t.Fatalf("protocol = %q, want NVMe", r.Device.Protocol)
	}
	if r.NVMeHealth == nil {
		t.Fatal("expected NVMe health log")
	}
	if r.ATAAttributes != nil {
		t.Error("NVMe drive should not have ATA attributes")
	}
	if temp, ok := r.CurrentTemp(); !ok || temp <= 0 {
		t.Errorf("CurrentTemp = %d, %v", temp, ok)
	}
	if r.Overall() != SeverityOK {
		t.Errorf("healthy WD drive Overall = %v", r.Overall())
	}
}

// TestParseSparseAppleNVMe is the graceful-degradation guard: the Apple drive
// omits the error and self-test logs and reports a non-zero exit_status bitmask,
// yet must still parse cleanly with absent sections decoding to nil.
func TestParseSparseAppleNVMe(t *testing.T) {
	r := parseFixture(t, "smart-apple-nvme.json")
	if !r.IsNVMe() {
		t.Fatalf("protocol = %q, want NVMe", r.Device.Protocol)
	}
	if r.Smartctl.ExitStatus == 0 {
		t.Error("Apple fixture expected to carry a non-zero exit_status bitmask")
	}
	if r.NVMeErrorLog != nil {
		t.Error("Apple drive should have nil NVMe error log")
	}
	if r.NVMeSelfTestLog != nil {
		t.Error("Apple drive should have nil NVMe self-test log")
	}
	if r.NVMeHealth == nil {
		t.Fatal("expected NVMe health log even on sparse Apple drive")
	}
	if _, ok := r.CurrentTemp(); !ok {
		t.Error("expected a temperature reading")
	}
}

func TestSeverityClassification(t *testing.T) {
	cases := []struct {
		name string
		attr ATAAttribute
		want Severity
	}{
		{"healthy", ATAAttribute{Value: 100, Thresh: 10, Flags: ATAFlags{Prefailure: true}}, SeverityOK},
		{"failing-now", ATAAttribute{WhenFailed: "FAILING_NOW"}, SeverityFailing},
		{"failed-past", ATAAttribute{WhenFailed: "in_the_past"}, SeverityCaution},
		{"prefail-below-thresh", ATAAttribute{Value: 5, Thresh: 10, Flags: ATAFlags{Prefailure: true}}, SeverityFailing},
		{"oldage-below-thresh", ATAAttribute{Value: 5, Thresh: 10, Flags: ATAFlags{Prefailure: false}}, SeverityCaution},
		{"no-threshold", ATAAttribute{Value: 0, Thresh: 0}, SeverityOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.attr.Severity(); got != c.want {
				t.Errorf("Severity() = %v, want %v", got, c.want)
			}
		})
	}
}
