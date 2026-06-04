// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// binary is the smartctl executable name; resolved via PATH.
const binary = "smartctl"

// commandTimeout-free: callers pass a context to bound execution.

// ScanResult mirrors `smartctl --scan-open -j`.
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
func (r *Report) FatalMessage() (string, bool) {
	for _, m := range r.Smartctl.Messages {
		if m.Severity == "error" {
			return m.String, true
		}
	}
	return "", false
}
