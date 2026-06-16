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

	// World Wide Name.
	if r.WWN == nil {
		t.Error("expected a WWN")
	} else if r.WWN.NAA != 5 {
		t.Errorf("WWN.NAA = %d, want 5", r.WWN.NAA)
	}

	// Device Statistics log (GP Log 0x04): the richest unused source.
	if !r.HasDeviceStats() {
		t.Fatal("expected Device Statistics with valid entries")
	}
	if got := statEntry(r, "Power-on Hours"); got != 9438 {
		t.Errorf("Power-on Hours stat = %d, want 9438", got)
	}
	if got := statEntry(r, "Number of Reallocated Logical Sectors"); got != 0 {
		t.Errorf("Reallocated stat = %d, want 0 (healthy drive)", got)
	}

	// Pending Defects log.
	if r.ATAPendingDefects == nil {
		t.Error("expected a pending defects log")
	} else if r.ATAPendingDefects.Count != 0 {
		t.Errorf("PendingDefects.Count = %d, want 0", r.ATAPendingDefects.Count)
	}

	// SCT Error Recovery Control (TLER).
	if r.ATASCTErc == nil || r.ATASCTErc.Read == nil {
		t.Fatal("expected SCT ERC read/write timers")
	}
	if !r.ATASCTErc.Read.Enabled || r.ATASCTErc.Read.Deciseconds != 70 {
		t.Errorf("ERC read = %+v, want enabled 70 ds", *r.ATASCTErc.Read)
	}
}

// statEntry returns the value of the first valid Device Statistics entry with
// the given name, or -1 if absent.
func statEntry(r *Report, name string) int64 {
	if r.ATADeviceStatistics == nil {
		return -1
	}
	for _, p := range r.ATADeviceStatistics.Pages {
		for _, e := range p.Table {
			if e.Flags.Valid && e.Name == name {
				return e.Value
			}
		}
	}
	return -1
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

	// NVMe identity enrichment.
	if r.NVMeVersion == nil || r.NVMeVersion.String != "1.4" {
		t.Errorf("NVMeVersion = %+v, want string 1.4", r.NVMeVersion)
	}
	if r.NVMeNumberOfNamespaces == nil || *r.NVMeNumberOfNamespaces != 1 {
		t.Errorf("NVMeNumberOfNamespaces = %v, want 1", r.NVMeNumberOfNamespaces)
	}
	if r.NVMePCIVendor == nil || r.NVMePCIVendor.ID == 0 {
		t.Errorf("NVMePCIVendor = %+v, want a nonzero id", r.NVMePCIVendor)
	}
	if r.HasDeviceStats() {
		t.Error("NVMe drive should not report ATA Device Statistics")
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

	// Apple's own wear metrics, the fallback for drives that report them.
	if r.EnduranceUsed == nil || r.EnduranceUsed.CurrentPercent != 3 {
		t.Errorf("EnduranceUsed = %+v, want current_percent 3", r.EnduranceUsed)
	}
	if r.SpareAvailable == nil || r.SpareAvailable.CurrentPercent != 100 {
		t.Errorf("SpareAvailable = %+v, want current_percent 100", r.SpareAvailable)
	}
}

// TestParseErrorLogEntries exercises the populated-error-log renderer inputs:
// the committed healthy fixtures all carry empty error tables, so these
// hand-crafted fixtures are the only ones that decode real entries.
func TestParseErrorLogEntries(t *testing.T) {
	t.Run("nvme", func(t *testing.T) {
		r := parseFixture(t, "smart-nvme-errors.json")
		if r.NVMeErrorLog == nil {
			t.Fatal("expected an NVMe error log")
		}
		if len(r.NVMeErrorLog.Table) != 3 {
			t.Fatalf("NVMe error entries = %d, want 3", len(r.NVMeErrorLog.Table))
		}
		e := r.NVMeErrorLog.Table[0]
		if e.ErrorCount != 3 || e.StatusField.String != "Unrecovered Read Error" {
			t.Errorf("first NVMe error entry = %+v", e)
		}
	})
	t.Run("ata", func(t *testing.T) {
		r := parseFixture(t, "smart-sda-errors.json")
		if r.ATAErrorLog == nil || r.ATAErrorLog.Extended == nil {
			t.Fatal("expected an ATA extended error log")
		}
		if r.ATAErrorLog.Extended.Count != 2 {
			t.Errorf("ATA error count = %d, want 2", r.ATAErrorLog.Extended.Count)
		}
		if len(r.ATAErrorLog.Extended.Table) != 2 {
			t.Fatalf("ATA error entries = %d, want 2", len(r.ATAErrorLog.Extended.Table))
		}
		e := r.ATAErrorLog.Extended.Table[0]
		if e.LifetimeHours != 9421 || e.ErrorDescription == "" {
			t.Errorf("first ATA error entry = %+v", e)
		}
	})
}

// TestParseFARM exercises the Seagate FARM parse path used by FarmLog, including
// the custom per-head unmarshal that gathers smartctl's flat *_by_head_N keys
// into index-ordered slices.
func TestParseFARM(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "smart-seagate-farm-log.json"))
	if err != nil {
		t.Fatalf("read FARM fixture: %v", err)
	}
	var wrapper struct {
		FARM *FARM `json:"seagate_farm_log"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("parse FARM fixture: %v", err)
	}
	f := wrapper.FARM
	if f == nil || !f.Supported {
		t.Fatal("expected a supported FARM log")
	}
	if f.DriveInfo.RecordingType != "CMR" {
		t.Errorf("RecordingType = %q, want CMR", f.DriveInfo.RecordingType)
	}
	if f.DriveInfo.Heads != 20 {
		t.Errorf("Heads = %d, want 20", f.DriveInfo.Heads)
	}
	if f.Environment.CurrentTemp != 37 {
		t.Errorf("CurrentTemp = %d, want 37", f.Environment.CurrentTemp)
	}
	if got := len(f.Reliability.MRHeadResistance); got != 20 {
		t.Fatalf("MRHeadResistance len = %d, want 20", got)
	}
	if f.Reliability.MRHeadResistance[0] != 465 {
		t.Errorf("MRHeadResistance[0] = %d, want 465", f.Reliability.MRHeadResistance[0])
	}
	if got := len(f.Reliability.ReallocatedByHead); got != 20 {
		t.Fatalf("ReallocatedByHead len = %d, want 20", got)
	}
	for i, v := range f.Reliability.ReallocatedByHead {
		if v != 0 {
			t.Errorf("ReallocatedByHead[%d] = %d, want 0 (healthy drive)", i, v)
		}
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
