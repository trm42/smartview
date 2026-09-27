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
	"time"
)

// binary is the smartctl executable, resolved via PATH; a var so tests can stub it.
var binary = "smartctl"

// smartctlWrapper is the envelope every smartctl JSON response carries.
type smartctlWrapper struct {
	Smartctl Smartctl `json:"smartctl"`
}

// farmWrapper is the envelope `smartctl -l farm -j` returns.
type farmWrapper struct {
	FARM *FARM `json:"seagate_farm_log"`
}

// scanResult mirrors `smartctl --scan-open -j`.
type scanResult struct {
	smartctlWrapper
	Devices []Device `json:"devices"`
}

// minSmartctlVersion is the oldest supported smartmontools: 7.0 added the -j output every
// parser here assumes.
var minSmartctlVersion = [2]int{7, 0}

// ErrNoSmartctl reports that smartctl is not on PATH; callers attach the install hint.
var ErrNoSmartctl = errors.New("smartctl not found on PATH")

// ErrOldSmartctl reports a smartctl too old for the JSON schema; callers attach the upgrade hint.
var ErrOldSmartctl = fmt.Errorf("smartctl is too old: smartview needs smartmontools %d.%d or newer",
	minSmartctlVersion[0], minSmartctlVersion[1])

// Available reports whether the smartctl binary is resolvable on PATH.
func Available() bool {
	_, err := exec.LookPath(binary)
	return err == nil
}

// Version returns smartctl's version from `smartctl -j -V`; a build too old for -j fails instead.
func Version(ctx context.Context) ([]int, error) {
	res, err := runJSON[smartctlWrapper](ctx, "smartctl version", "-j", "-V")
	if err != nil {
		return nil, err
	}
	return res.Smartctl.Version, nil
}

// Preflight checks smartctl is on PATH and at least minSmartctlVersion; a no-op in fixture mode.
func Preflight(ctx context.Context) error {
	if fixtureActive() {
		return nil
	}
	if !Available() {
		return ErrNoSmartctl
	}
	v, err := Version(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("smartctl version check: %w", err)
		}
		// A pre-7.0 build rejects -j instead of stating a version, so the failed probe is the verdict.
		return fmt.Errorf("%w (version check failed: %v)", ErrOldSmartctl, err)
	}
	if !versionAtLeast(v, minSmartctlVersion) {
		return fmt.Errorf("%w (found %s)", ErrOldSmartctl, formatVersion(v))
	}
	return nil
}

// versionAtLeast compares v to a [major, minor] floor; an undeterminable version (empty or
// major-only) passes.
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

// Scan enumerates drives via `smartctl --scan-open -j`; Device names must reach Info unmodified.
func Scan(ctx context.Context) ([]Device, error) {
	if fixtureActive() {
		return fixtureScan()
	}
	res, err := runJSON[scanResult](ctx, "scan output", "--scan-open", "-j")
	if err != nil {
		return nil, err
	}
	return res.Devices, nil
}

// PowerPolicy decides whether a spun-down drive may be woken to be read.
type PowerPolicy int

const (
	// WakeDrive reads the drive, spinning it up if it is parked.
	WakeDrive PowerPolicy = iota
	// SkipStandby returns an empty report rather than wake a parked drive.
	SkipStandby
)

// standbyExit is the -n exit status: smartctl's default 2 is ambiguous with "device open
// failed", and 129 (bits 0+7) cannot occur on a real run.
const standbyExit = 129

// InStandby reports that smartctl declined to wake a spun-down drive, so the report carries no
// drive data.
func (r *Report) InStandby() bool { return r.Smartctl.ExitStatus == standbyExit }

// powerArgs is the SkipStandby guard: -d because autodetection can spin the drive up, and no
// STATUS2 so a drive without a power-mode check is still read.
func powerArgs(d Device, policy PowerPolicy) []string {
	if policy != SkipStandby {
		return nil
	}
	args := []string{"-n", "standby," + strconv.Itoa(standbyExit)}
	if d.Type != "" {
		args = append(args, "-d", d.Type)
	}
	return args
}

// Info runs `smartctl -j -x <name>`, parsing stdout regardless of the exit bitmask.
// Under SkipStandby a parked drive returns a valid report and nil error; see [Report.InStandby].
func Info(ctx context.Context, d Device, policy PowerPolicy) (*Report, error) {
	if fixtureActive() {
		return fixtureInfo(d.Name)
	}
	args := append(powerArgs(d, policy), "-j", "-x", d.Name)
	return runJSON[Report](ctx, "report for "+d.Name, args...)
}

// FarmLog runs `smartctl -l farm -j`; an unsupported drive yields (nil, nil).
// It takes Info's power policy because both run every poll.
func FarmLog(ctx context.Context, d Device, policy PowerPolicy) (*FARM, error) {
	if fixtureActive() {
		return fixtureFarm(d.Name)
	}
	args := append(powerArgs(d, policy), "-l", "farm", "-j", d.Name)
	w, err := runJSON[farmWrapper](ctx, "FARM log for "+d.Name, args...)
	if err != nil {
		return nil, err
	}
	return supportedFarm(w.FARM), nil
}

// RunSelfTest queues a short or long self-test; progress arrives via later Info polls. Usually
// requires root.
func RunSelfTest(ctx context.Context, name string, testType SelfTestType) error {
	switch testType {
	case SelfTestShort, SelfTestLong:
	default:
		return fmt.Errorf("unsupported self-test type %q (want %q or %q)",
			testType, SelfTestShort, SelfTestLong)
	}
	return runSelfTestCommand(ctx, name, "start", "-t", string(testType))
}

// AbortSelfTest cancels the running self-test (`smartctl -X`); a no-op if
// none is running.
func AbortSelfTest(ctx context.Context, name string) error {
	return runSelfTestCommand(ctx, name, "abort", "-X")
}

// runSelfTestCommand runs a self-test control command, returning the first error-severity
// smartctl message as an error.
func runSelfTestCommand(ctx context.Context, name, action string, flags ...string) error {
	args := append(flags, "-j", name)
	w, err := runJSON[smartctlWrapper](ctx,
		fmt.Sprintf("self-test %s response for %s", action, name), args...)
	if err != nil {
		return err
	}
	for _, m := range w.Smartctl.Messages {
		if m.Severity == "error" {
			return fmt.Errorf("self-test %s for %s: %s", action, name, m.String)
		}
	}
	return nil
}

// runJSON runs smartctl and decodes stdout into T; empty stdout is an error since smartctl
// prints JSON even with exit bits set.
func runJSON[T any](ctx context.Context, what string, args ...string) (*T, error) {
	out, err := run(ctx, args...)
	if len(out) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("smartctl produced no output")
	}
	var v T
	if jerr := json.Unmarshal(out, &v); jerr != nil {
		return nil, fmt.Errorf("parse %s: %w", what, jerr)
	}
	return &v, nil
}

// maxStderrDetail bounds how much of smartctl's stderr is folded into an error.
const maxStderrDetail = 200

// waitDelay bounds how long a cancelled command may outlive its deadline; a var for tests.
var waitDelay = 2 * time.Second

// run executes smartctl, returning stdout alongside any error.
func run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	// CommandContext kills only the child; WaitDelay bounds a descendant still holding stdout.
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	if err != nil {
		// exec reports "signal: killed" on cancel; return the cause so errors.Is matches.
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

// isBenignLogReadFailure matches the error-log read failure Apple internal NVMe emits on every
// poll, a platform limitation.
func isBenignLogReadFailure(msg string) bool {
	return strings.Contains(msg, "Error Information Log failed: GetLogPage failed")
}
