// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// binary is the smartctl executable name; resolved via PATH.
const binary = "smartctl"

// scanResult mirrors `smartctl --scan-open -j`.
type scanResult struct {
	Smartctl Smartctl `json:"smartctl"`
	Devices  []Device `json:"devices"`
}

// minSmartctlVersion is the oldest smartmontools release smartview supports.
// 7.0 is where smartctl's JSON output (`-j`) landed, and every parser in this
// package assumes that schema; the README states the same floor.
var minSmartctlVersion = [2]int{7, 0}

// Available reports whether the smartctl binary is resolvable on PATH.
func Available() bool {
	_, err := exec.LookPath(binary)
	return err == nil
}

// Version reports smartctl's own version as the [major, minor, ...] list it
// prints in `smartctl -j -V`. A build too old to understand -j emits no JSON at
// all, which surfaces here as a parse error rather than a version.
func Version(ctx context.Context) ([]int, error) {
	out, err := run(ctx, "-j", "-V")
	if len(out) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("smartctl produced no output")
	}
	var res struct {
		Smartctl Smartctl `json:"smartctl"`
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		return nil, fmt.Errorf("parse smartctl version: %w", jerr)
	}
	return res.Smartctl.Version, nil
}

// Preflight checks that smartctl is on PATH and new enough to speak the JSON
// schema this package parses; it is a no-op when the fixture source is active.
// Not yet wired into startup — main.go still gates on Available() alone.
func Preflight(ctx context.Context) error {
	if fixtureActive() {
		return nil
	}
	if !Available() {
		return errors.New("smartctl not found on PATH")
	}
	v, err := Version(ctx)
	if err != nil {
		return fmt.Errorf("smartctl version check: %w", err)
	}
	if !versionAtLeast(v, minSmartctlVersion) {
		return fmt.Errorf("smartctl %s is too old: smartview needs smartmontools %d.%d or newer",
			formatVersion(v), minSmartctlVersion[0], minSmartctlVersion[1])
	}
	return nil
}

// versionAtLeast compares a smartctl version list against a [major, minor]
// floor. A version we could not determine (empty, or major-only) passes: it is
// not evidence of an old build, and refusing to start would be the worse error.
func versionAtLeast(v []int, minimum [2]int) bool {
	if len(v) == 0 {
		return true
	}
	if v[0] != minimum[0] {
		return v[0] > minimum[0]
	}
	if len(v) < 2 {
		return true
	}
	return v[1] >= minimum[1]
}

// formatVersion renders a version list for display ([7 4] -> "7.4").
func formatVersion(v []int) string {
	if len(v) == 0 {
		return "(unknown version)"
	}
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// Scan enumerates drives via `smartctl --scan-open -j`. The returned Device
// names are round-tripped verbatim into Info; they must not be modified.
func Scan(ctx context.Context) ([]Device, error) {
	if fixtureActive() {
		return fixtureScan()
	}
	out, err := run(ctx, "--scan-open", "-j")
	if err != nil && len(out) == 0 {
		return nil, err
	}
	var res scanResult
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		return nil, fmt.Errorf("parse scan output: %w", jerr)
	}
	return res.Devices, nil
}

// Info runs `smartctl -j -x <name>` and parses the full report. smartctl's
// exit status is a bitmask, often non-zero on healthy drives, so stdout is
// parsed regardless; real failures surface via smartctl.messages (FatalMessage).
func Info(ctx context.Context, name string) (*Report, error) {
	if fixtureActive() {
		return fixtureInfo(name)
	}
	out, err := run(ctx, "-j", "-x", name)
	if len(out) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("smartctl produced no output")
	}
	var rep Report
	if jerr := json.Unmarshal(out, &rep); jerr != nil {
		return nil, fmt.Errorf("parse report for %s: %w", name, jerr)
	}
	return &rep, nil
}

// FarmLog runs `smartctl -l farm -j <name>` and parses the Seagate FARM log.
// An unsupported drive yields (nil, nil): expected, not an error.
func FarmLog(ctx context.Context, name string) (*FARM, error) {
	if fixtureActive() {
		return fixtureFarm(name)
	}
	out, err := run(ctx, "-l", "farm", "-j", name)
	if len(out) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("smartctl produced no output")
	}
	var wrapper struct {
		FARM *FARM `json:"seagate_farm_log"`
	}
	if jerr := json.Unmarshal(out, &wrapper); jerr != nil {
		return nil, fmt.Errorf("parse FARM log for %s: %w", name, jerr)
	}
	if wrapper.FARM == nil || !wrapper.FARM.Supported {
		return nil, nil
	}
	return wrapper.FARM, nil
}

// RunSelfTest starts a short or long SMART self-test (other types are
// rejected). It returns once the test is queued; progress arrives via later
// Info polls. Usually requires root.
func RunSelfTest(ctx context.Context, name string, testType SelfTestType) error {
	switch testType {
	case SelfTestShort, SelfTestLong:
	default:
		return fmt.Errorf("unsupported self-test type %q (want %q or %q)",
			testType, SelfTestShort, SelfTestLong)
	}
	return runSelfTestCommand(ctx, name, "start", "-t", string(testType), "-j", name)
}

// AbortSelfTest cancels the running self-test (`smartctl -X`); a no-op if
// none is running.
func AbortSelfTest(ctx context.Context, name string) error {
	return runSelfTestCommand(ctx, name, "abort", "-X", "-j", name)
}

// runSelfTestCommand runs a self-test control command; error-severity
// smartctl messages become the returned error.
func runSelfTestCommand(ctx context.Context, name, action string, args ...string) error {
	out, err := run(ctx, args...)
	if len(out) == 0 {
		if err != nil {
			return err
		}
		return errors.New("smartctl produced no output")
	}
	var wrapper struct {
		Smartctl Smartctl `json:"smartctl"`
	}
	if jerr := json.Unmarshal(out, &wrapper); jerr != nil {
		return fmt.Errorf("parse self-test %s response for %s: %w", action, name, jerr)
	}
	for _, m := range wrapper.Smartctl.Messages {
		if m.Severity == "error" {
			return fmt.Errorf("self-test %s for %s: %s", action, name, m.String)
		}
	}
	return nil
}

// maxStderrDetail bounds how much of smartctl's stderr is folded into an error.
const maxStderrDetail = 200

// run executes smartctl. A non-zero exit returns stdout alongside the error:
// smartctl emits valid JSON even with its exit-status bitmask set.
func run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.Output()
	if err != nil {
		// A cancelled context kills the child, so exec reports "signal: killed"
		// and loses the cause; report the context error so errors.Is matches.
		if ctx.Err() != nil {
			return out, fmt.Errorf("run smartctl: %w", context.Cause(ctx))
		}
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			return out, fmt.Errorf("smartctl exit %d: %w%s", ee.ExitCode(), ee, stderrDetail(ee.Stderr))
		}
		return out, fmt.Errorf("run smartctl: %w", err)
	}
	return out, nil
}

// stderrDetail renders captured stderr as a bounded single-line error suffix.
// ExitError.Error() is only "exit status N", so the real complaint is otherwise lost.
func stderrDetail(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if s == "" {
		return ""
	}
	if len(s) > maxStderrDetail {
		s = strings.ToValidUTF8(s[:maxStderrDetail], "") + "..."
	}
	return ": " + s
}

// FatalMessage returns the first error-severity smartctl message (permission
// and open failures), skipping known-benign ones.
func (r *Report) FatalMessage() (string, bool) {
	for _, m := range r.Smartctl.Messages {
		if m.Severity == "error" && !isBenignLogReadFailure(m.String) {
			return m.String, true
		}
	}
	return "", false
}

// isBenignLogReadFailure matches the error-log read failure Apple internal
// NVMe emits on every poll: NVMeSMARTLib rejects that log page, a platform
// limitation rather than a fault.
func isBenignLogReadFailure(msg string) bool {
	return strings.Contains(msg, "Error Information Log failed: GetLogPage failed")
}
