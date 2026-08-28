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

// run executes smartctl. A non-zero exit returns stdout alongside the error:
// smartctl emits valid JSON even with its exit-status bitmask set.
func run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, fmt.Errorf("smartctl exit %d: %w", ee.ExitCode(), ee)
		}
		return out, fmt.Errorf("run smartctl: %w", err)
	}
	return out, nil
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
