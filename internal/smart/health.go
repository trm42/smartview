// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"slices"
	"strings"
)

// Severity is a tri-state health classification used for colouring and sorting.
type Severity int

const (
	// SeverityOK indicates a value within normal bounds.
	SeverityOK Severity = iota
	// SeverityCaution indicates degradation or a past failure worth watching.
	SeverityCaution
	// SeverityFailing indicates an active pre-fail condition.
	SeverityFailing
)

// String renders the severity for display/debugging.
func (s Severity) String() string {
	switch s {
	case SeverityFailing:
		return "FAILING"
	case SeverityCaution:
		return "CAUTION"
	default:
		return "OK"
	}
}

// Overall derives a drive-level severity from the SMART status and the
// protocol-specific health indicators; with no status it grades what is present.
func (r *Report) Overall() Severity {
	if r.SmartStatus != nil && !r.SmartStatus.Passed {
		return SeverityFailing
	}
	worst := SeverityOK
	if r.HasNVMeHealth() {
		if r.NVMeHealth.CriticalWarning != 0 {
			return SeverityFailing
		}
		if r.NVMeHealth.MediaErrors > 0 {
			worst = SeverityCaution
		}
	}
	worst = max(worst, r.wearSeverity())
	for _, a := range r.Attributes() {
		worst = max(worst, a.Severity())
	}
	return max(worst, r.logSeverity())
}

// logSeverity grades the drive's own logs, which can record an uncorrectable read
// no attribute reflects. NVMe num_err_log_entries is excluded: it grows for benign reasons.
func (r *Report) logSeverity() Severity {
	if l := r.ATAErrors(); l != nil && l.Count > 0 {
		return SeverityCaution
	}
	if r.lastSelfTestFailed() {
		return SeverityCaution
	}
	if r.ATAPendingDefects != nil && r.ATAPendingDefects.Count > 0 {
		return SeverityCaution
	}
	return SeverityOK
}

// lastSelfTestFailed reports whether the newest finished ATA self-test failed; a later pass clears it.
func (r *Report) lastSelfTestFailed() bool {
	tbl, _ := r.ATASelfTests()
	for _, e := range tbl {
		if e.Status.Passed != nil {
			return !*e.Status.Passed
		}
	}
	return false
}

// HasThreshold reports whether the attribute carries a threshold; 0 means none.
func (a *ATAAttribute) HasThreshold() bool { return a.Thresh > 0 }

// FailedInPast reports whether the value once crossed its threshold but no longer does.
func (a *ATAAttribute) FailedInPast() bool { return a.WhenFailed == "in_the_past" }

// Severity classifies one ATA attribute; pre-fail vs old-age comes from the
// authoritative Flags.Prefailure bit, not the attribute name.
func (a *ATAAttribute) Severity() Severity {
	switch {
	case a.WhenFailed == "FAILING_NOW":
		return SeverityFailing
	case a.FailedInPast():
		return SeverityCaution
	}
	if a.HasThreshold() && a.Value <= a.Thresh {
		if a.Flags.Prefailure {
			return SeverityFailing
		}
		return SeverityCaution
	}
	return SeverityOK
}

// PctUsedSeverity grades endurance consumption: Caution at >= 90%, since a
// drive past its rated writes is worn, not failing.
func PctUsedSeverity(percent int) Severity {
	if percent >= 90 {
		return SeverityCaution
	}
	return SeverityOK
}

// SpareSeverity grades available spare against its depletion threshold:
// Failing at or below it, Caution within 10 points short of a full spare.
func SpareSeverity(pct, threshold int) Severity {
	switch {
	case pct <= threshold:
		return SeverityFailing
	case pct <= threshold+10 && pct < 100:
		return SeverityCaution
	default:
		return SeverityOK
	}
}

// wearSeverity grades the wear accessors' readings, the ones every gauge, row and bar shows.
func (r *Report) wearSeverity() Severity {
	sev := SeverityOK
	if pct, _, ok := r.LifeUsedPercent(); ok {
		sev = max(sev, PctUsedSeverity(pct))
	}
	if pct, thr, _, ok := r.SparePercent(); ok {
		sev = max(sev, SpareSeverity(pct, thr))
	}
	return sev
}

// SelfTestVerdict classifies a drive-reported self-test result string.
type SelfTestVerdict int

const (
	// SelfTestNeutral is a run that says nothing about the drive: in progress, or stopped by the host.
	SelfTestNeutral SelfTestVerdict = iota
	// SelfTestPassed is a run that completed without error.
	SelfTestPassed
	// SelfTestFailed is a run that reported a failure.
	SelfTestFailed
)

// SelfTestOutcome classifies a self-test result string by keyword; "without error"
// is checked before "error", so a bare "Completed" passes only when nothing else matched.
func SelfTestOutcome(s string) SelfTestVerdict {
	low := strings.ToLower(s)
	if strings.Contains(low, "without error") {
		return SelfTestPassed
	}
	if containsAny(low, "fail", "error", "fatal", "damage") {
		return SelfTestFailed
	}
	if containsAny(low, "in progress", "aborted", "interrupted") {
		return SelfTestNeutral
	}
	if strings.Contains(low, "completed") {
		return SelfTestPassed
	}
	return SelfTestNeutral
}

// containsAny reports whether s contains any of the keywords.
func containsAny(s string, kws ...string) bool {
	return slices.ContainsFunc(kws, func(kw string) bool { return strings.Contains(s, kw) })
}

// Severity grades the health-relevant Device Statistics counters.
func (e ATAStatEntry) Severity() Severity {
	switch e.Name {
	case StatPercentageUsed:
		return PctUsedSeverity(int(e.Value))
	case StatReallocatedLegacy,
		StatReallocated,
		StatReallocCandidates,
		StatUncorrectable,
		StatInterfaceCRC,
		StatMechStartFailures:
		return SeverityCaution
	}
	return SeverityOK
}

// Concerning reports whether a PHY counter indicates a link fault:
// CRC/decode errors point at a cable, resets accumulate on any healthy link.
func (c SATAPhyCounter) Concerning() bool {
	return containsAny(strings.ToLower(c.Name), "crc", "non-crc", "decode", "disparity", "handshake")
}
