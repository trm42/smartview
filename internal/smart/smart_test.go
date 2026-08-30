// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		name string
		v    []int
		want bool
	}{
		{"exact-floor", []int{7, 0}, true},
		{"newer-minor", []int{7, 5, 2025}, true},
		{"newer-major", []int{8, 0}, true},
		{"older-minor", []int{6, 6}, false},
		{"older-major", []int{5, 43}, false},
		// An undeterminable version must not block startup.
		{"empty", nil, true},
		{"major-only", []int{7}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := versionAtLeast(c.v, minSmartctlVersion); got != c.want {
				t.Errorf("versionAtLeast(%v) = %v, want %v", c.v, got, c.want)
			}
		})
	}
}

func TestFormatVersion(t *testing.T) {
	if got := formatVersion([]int{7, 4}); got != "7.4" {
		t.Errorf("formatVersion = %q, want 7.4", got)
	}
	if got := formatVersion(nil); got == "" {
		t.Error("formatVersion(nil) = empty, want a placeholder")
	}
}

// smartctl's stderr is the only place the real complaint appears (ExitError
// says just "exit status N"), so it is folded into the error — bounded, and on
// one line.
func TestStderrDetail(t *testing.T) {
	if got := stderrDetail(nil); got != "" {
		t.Errorf("empty stderr = %q, want \"\"", got)
	}
	if got := stderrDetail([]byte("  \n\t ")); got != "" {
		t.Errorf("blank stderr = %q, want \"\"", got)
	}
	got := stderrDetail([]byte("Unknown option\nSmartctl: please specify a device\n"))
	want := ": Unknown option Smartctl: please specify a device"
	if got != want {
		t.Errorf("stderrDetail = %q, want %q", got, want)
	}
	long := stderrDetail([]byte(strings.Repeat("x", 4096)))
	if len(long) > maxStderrDetail+8 {
		t.Errorf("long stderr not truncated: %d bytes", len(long))
	}
}

// A cancelled or expired context kills the child process; run must report the
// context cause rather than exec's "signal: killed", so callers can match with
// errors.Is.
func TestRunReportsContextCause(t *testing.T) {
	if !Available() {
		t.Skip("smartctl not installed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := run(ctx, "-j", "-V"); !errors.Is(err, context.Canceled) {
		t.Errorf("run on cancelled context = %v, want context.Canceled", err)
	}
}

func TestVersionAndPreflight(t *testing.T) {
	if !Available() {
		t.Skip("smartctl not installed")
	}
	v, err := Version(t.Context())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if len(v) == 0 {
		t.Fatal("Version returned no components")
	}
	if err := Preflight(t.Context()); err != nil {
		t.Errorf("Preflight: %v", err)
	}
}

// smartctl exiting 0 with no output must say so plainly. Every entry point
// funnels through runJSON for this; before it did, Scan alone fell through to
// json.Unmarshal(nil) and reported "unexpected end of JSON input" instead.
func TestEmptyOutputIsReportedPlainly(t *testing.T) {
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("no true(1) to stand in for smartctl")
	}
	orig := binary
	binary = "true" // exits 0, prints nothing
	t.Cleanup(func() { binary = orig })

	const want = "smartctl produced no output"
	for _, c := range []struct {
		name string
		call func() error
	}{
		{"Scan", func() error { _, err := Scan(t.Context()); return err }},
		{"Info", func() error { _, err := Info(t.Context(), "/dev/sda"); return err }},
		{"Version", func() error { _, err := Version(t.Context()); return err }},
		{"FarmLog", func() error { _, err := FarmLog(t.Context(), "/dev/sda"); return err }},
		{"RunSelfTest", func() error { return RunSelfTest(t.Context(), "/dev/sda", SelfTestShort) }},
		{"AbortSelfTest", func() error { return AbortSelfTest(t.Context(), "/dev/sda") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s on empty output = nil, want %q", c.name, want)
			}
			if err.Error() != want {
				t.Errorf("%s on empty output = %q, want %q", c.name, err, want)
			}
		})
	}
}

// fakeSmartctl writes an executable stub that prints body on stdout, and
// points the package at it for the duration of the test.
func fakeSmartctl(t *testing.T, body string) {
	t.Helper()
	fakeSmartctlScript(t, "cat <<'EOF'\n"+body+"\nEOF\n")
}

// fakeSmartctlScript is fakeSmartctl for a stub that must do more than print:
// body is the shell source, so it can fail the way a real smartctl does.
func fakeSmartctlScript(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "smartctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := binary
	binary = path
	t.Cleanup(func() { binary = orig })
}

// Preflight is the startup gate, so the message it produces is what a user
// with the wrong smartmontools actually sees. Before it was wired in, an old
// build got a JSON parse error from the first real query instead.
func TestPreflightNamesAnOldSmartctl(t *testing.T) {
	fakeSmartctl(t, `{"smartctl":{"version":[6,6]}}`)
	err := Preflight(t.Context())
	if err == nil {
		t.Fatal("Preflight accepted smartctl 6.6, below the 7.0 floor")
	}
	if !errors.Is(err, ErrOldSmartctl) {
		t.Errorf("Preflight on smartctl 6.6 = %v, want ErrOldSmartctl", err)
	}
	for _, want := range []string{"6.6", "too old", "7.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err, want)
		}
	}
}

// A build at or above the floor passes, so the gate does not lock anyone out.
func TestPreflightAcceptsASupportedSmartctl(t *testing.T) {
	for _, v := range []string{`[7,0]`, `[7,4]`, `[8,1]`} {
		fakeSmartctl(t, `{"smartctl":{"version":`+v+`}}`)
		if err := Preflight(t.Context()); err != nil {
			t.Errorf("Preflight rejected smartctl %s: %v", v, err)
		}
	}
}

// A missing binary is reported as ErrNoSmartctl so main can attach the
// platform's install hint without matching on message text.
func TestPreflightMissingBinaryIsMatchable(t *testing.T) {
	orig := binary
	binary = "smartview-no-such-binary"
	t.Cleanup(func() { binary = orig })

	err := Preflight(t.Context())
	if !errors.Is(err, ErrNoSmartctl) {
		t.Errorf("Preflight with no smartctl = %v, want ErrNoSmartctl", err)
	}
}

// A version smartctl would not state is not evidence of an old build, and
// refusing to start would be the worse error.
func TestPreflightAcceptsAnUnstatedVersion(t *testing.T) {
	fakeSmartctl(t, `{"smartctl":{}}`)
	if err := Preflight(t.Context()); err != nil {
		t.Errorf("Preflight rejected a build that states no version: %v", err)
	}
}

// The version probe itself needs -j, which is what 7.0 added, so a build below
// the floor rejects the flag rather than reporting a version it could be judged
// on. That path is the only one a real old smartctl takes: if it does not reach
// the floor message, nothing does.
func TestPreflightNamesASmartctlTooOldForJSON(t *testing.T) {
	fakeSmartctlScript(t, "echo \"smartctl: Unrecognized option '-j'\" >&2\nexit 1\n")
	err := Preflight(t.Context())
	if !errors.Is(err, ErrOldSmartctl) {
		t.Fatalf("Preflight on a pre-JSON smartctl = %v, want ErrOldSmartctl", err)
	}
	for _, want := range []string{"too old", "7.0", "Unrecognized option"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err, want)
		}
	}
}

// A probe cut short by the caller says so: reporting an interrupted or timed-out
// check as an old smartctl would send the user to fix the wrong thing, and main
// branches on the context error to abort quietly.
func TestPreflightReportsAnAbortedProbeAsSuch(t *testing.T) {
	fakeSmartctl(t, `{"smartctl":{"version":[7,4]}}`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := Preflight(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Preflight on a cancelled context = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrOldSmartctl) {
		t.Errorf("cancelled probe reported as an old smartctl: %v", err)
	}
}

// A deadline has to bound the call, not just describe it. Output waits for the
// stdout pipe to close and CommandContext signals only the direct child, so a
// descendant that inherited the pipe keeps the read alive after the child is
// killed -- and startup hangs long past the timeout that was supposed to stop it.
func TestRunDoesNotOutliveItsDeadline(t *testing.T) {
	fakeSmartctlScript(t, "sleep 3 &\nsleep 3\n")
	orig := waitDelay
	waitDelay = 50 * time.Millisecond
	t.Cleanup(func() { waitDelay = orig })

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := run(ctx, "-j", "-V")
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("run past its deadline = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run still blocked a second after a 50ms deadline")
	}
}
