// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// standbyEnvelope is what smartctl prints when -n standby declines to wake a
// drive: the envelope, and none of the data sections.
const standbyEnvelope = `{"json_format_version":[1,0],
"smartctl":{"version":[7,5],"exit_status":129},
"device":{"name":"/dev/sdb","info_name":"/dev/sdb","type":"sat","protocol":"ATA"}}`

// recordArgv points the package at a stub that appends its argv to a file and
// prints body; it returns a func reading back the recorded arguments.
func recordArgv(t *testing.T, body string) func() []string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "argv")
	fakeSmartctlScript(t, "printf '%s\\n' \"$@\" >> "+log+"\ncat <<'EOF'\n"+body+"\nEOF\n")
	return func() []string {
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("stub recorded no argv: %v", err)
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

// TestInfoReportsStandby: smartctl exits non-zero but prints a full envelope,
// so runJSON parses it and returns no error. InStandby is the only signal.
func TestInfoReportsStandby(t *testing.T) {
	fakeSmartctlScript(t, "cat <<'EOF'\n"+standbyEnvelope+"\nEOF\nexit 129\n")
	rep, err := Info(t.Context(), Device{Name: "/dev/sdb", Type: "sat"}, SkipStandby)
	if err != nil {
		t.Fatalf("Info on a standby drive = %v, want no error: standby is a state, not a failure", err)
	}
	if rep == nil {
		t.Fatal("Info returned no report for a standby drive")
	}
	if !rep.InStandby() {
		t.Errorf("InStandby() = false for exit_status %d", rep.Smartctl.ExitStatus)
	}
}

// TestInfoPassesTheStandbyGuard covers both halves. The negative half is the
// one that protects today's default argv: the guard must appear only when the
// caller asked to skip standby drives.
func TestInfoPassesTheStandbyGuard(t *testing.T) {
	dev := Device{Name: "/dev/sdb", Type: "sat"}

	t.Run("SkipStandby", func(t *testing.T) {
		argv := recordArgv(t, `{"smartctl":{"exit_status":0}}`)
		if _, err := Info(t.Context(), dev, SkipStandby); err != nil {
			t.Fatal(err)
		}
		got := argv()
		if !slices.Contains(got, "-n") || !slices.Contains(got, "standby,129") {
			t.Errorf("argv %q is missing the -n standby guard", got)
		}
		// Without -d, autodetection itself can spin the drive up.
		if !slices.Contains(got, "-d") || !slices.Contains(got, "sat") {
			t.Errorf("argv %q is missing -d sat", got)
		}
		// STATUS2 would turn smartctl's benign "power mode check unsupported"
		// fall-through into an early exit carrying no data.
		for _, a := range got {
			if strings.HasPrefix(a, "standby,") && strings.Count(a, ",") > 1 {
				t.Errorf("argv passes STATUS2 (%q); that blinds us on drives whose power-mode check is unsupported", a)
			}
		}
	})

	t.Run("WakeDrive leaves the argv alone", func(t *testing.T) {
		argv := recordArgv(t, `{"smartctl":{"exit_status":0}}`)
		if _, err := Info(t.Context(), dev, WakeDrive); err != nil {
			t.Fatal(err)
		}
		got := argv()
		for _, unwanted := range []string{"-n", "-d"} {
			if slices.Contains(got, unwanted) {
				t.Errorf("argv %q carries %s under WakeDrive; the default path must be unchanged", got, unwanted)
			}
		}
	})
}

// TestFarmLogPassesTheStandbyGuard: FARM is a second per-poll smartctl call,
// so guarding only Info would still wake the drive every poll.
func TestFarmLogPassesTheStandbyGuard(t *testing.T) {
	argv := recordArgv(t, `{"smartctl":{"exit_status":0}}`)
	if _, err := FarmLog(t.Context(), Device{Name: "/dev/sdb", Type: "sat"}, SkipStandby); err != nil {
		t.Fatal(err)
	}
	if got := argv(); !slices.Contains(got, "standby,129") {
		t.Errorf("FARM argv %q is missing the standby guard", got)
	}
}

// TestInfoOmitsTypeWhenUnknown: a scan that reported no type must not produce
// a bare "-d".
func TestInfoOmitsTypeWhenUnknown(t *testing.T) {
	argv := recordArgv(t, `{"smartctl":{"exit_status":0}}`)
	if _, err := Info(t.Context(), Device{Name: "/dev/sdb"}, SkipStandby); err != nil {
		t.Fatal(err)
	}
	if got := argv(); slices.Contains(got, "-d") {
		t.Errorf("argv %q carries -d with no device type", got)
	}
}

// TestInStandbyOnlyMatchesTheSentinel: every other exit status is a real
// reading, including the bitmask values a healthy drive routinely returns.
func TestInStandbyOnlyMatchesTheSentinel(t *testing.T) {
	for _, status := range []int{0, 2, 4, 8, 128, 132} {
		r := &Report{Smartctl: Smartctl{ExitStatus: status}}
		if r.InStandby() {
			t.Errorf("InStandby() = true for exit_status %d", status)
		}
	}
	r := &Report{Smartctl: Smartctl{ExitStatus: 129}}
	if !r.InStandby() {
		t.Error("InStandby() = false for the sentinel")
	}
}
