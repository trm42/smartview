// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// binary is the smartctl executable name; resolved via PATH.
const binary = "smartctl"

// scanResult mirrors `smartctl --scan-open -j`.
type scanResult struct {
	Smartctl Smartctl `json:"smartctl"`
	Devices  []Device `json:"devices"`
}

// Available reports whether the smartctl binary is resolvable on PATH.
func Available() bool {
	_, err := exec.LookPath(binary)
	return err == nil
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

// Info runs `smartctl -j -x <name>` and parses the full report.
//
// smartctl's process exit status is a bitmask (e.g. 4 on a perfectly healthy
// drive), so a non-zero exit is NOT treated as failure: we parse stdout
// regardless and only error out when the JSON is unusable. Real problems
// (permission denied, device not found) surface as smartctl.messages, which the
// caller can inspect via Report.Errorf / FatalMessage.
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
//
// Like Info, it ignores smartctl's exit-status bitmask and parses stdout
// regardless. FARM is Seagate-only: on a drive that does not support it the
// section is absent (or supported=false), which is reported as (nil, nil) — an
// expected condition, not an error, so the caller simply omits the FARM tab.
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

// RunSelfTest starts a SMART self-test on the named device. testType must be
// SelfTestShort or SelfTestLong (extended) — smartview deliberately does not
// expose conveyance/selective tests. It returns nil once smartctl confirms the
// test has been queued; it does NOT wait for completion (progress is observed
// via later Info polls). Starting a test usually requires root.
func RunSelfTest(ctx context.Context, name string, testType SelfTestType) error {
	switch testType {
	case SelfTestShort, SelfTestLong:
	default:
		return fmt.Errorf("unsupported self-test type %q (want %q or %q)",
			testType, SelfTestShort, SelfTestLong)
	}
	return runSelfTestCommand(ctx, name, "start", "-t", string(testType), "-j", name)
}

// AbortSelfTest cancels the self-test currently running on the named device
// (`smartctl -X`). It is a no-op on the drive if no test is running.
func AbortSelfTest(ctx context.Context, name string) error {
	return runSelfTestCommand(ctx, name, "abort", "-X", "-j", name)
}

// runSelfTestCommand runs a self-test control command and surfaces any
// error-severity smartctl message as the returned error, mirroring how Info
// treats smartctl.messages as the authoritative failure channel.
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

// run executes smartctl and returns its stdout. A non-zero exit code yields a
// non-nil error AND the captured stdout, because smartctl emits valid JSON even
// when its exit status bitmask is set. Callers decide whether the error matters.
func run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// Exit-status bitmask set: stdout is still valid JSON.
			return out, fmt.Errorf("smartctl exit %d: %w", ee.ExitCode(), ee)
		}
		return out, fmt.Errorf("run smartctl: %w", err)
	}
	return out, nil
}

// FatalMessage returns the first error-severity smartctl message, if any. It is
// how permission/open failures are surfaced despite a parseable report.
// Known-benign messages are skipped (see isBenignLogReadFailure).
func (r *Report) FatalMessage() (string, bool) {
	for _, m := range r.Smartctl.Messages {
		if m.Severity == "error" && !isBenignLogReadFailure(m.String) {
			return m.String, true
		}
	}
	return "", false
}

// isBenignLogReadFailure reports whether msg is the error-log read failure that
// Apple internal NVMe emits on every poll ("Read N entries from Error
// Information Log failed: GetLogPage failed: ..."): macOS reads these drives
// through Apple's private NVMeSMARTLib, which rejects that log page, so the
// message is a permanent platform limitation, not an actionable fault. The
// missing log is already conveyed by the Logs tab hiding itself.
func isBenignLogReadFailure(msg string) bool {
	return strings.Contains(msg, "Error Information Log failed: GetLogPage failed")
}
