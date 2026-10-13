// SPDX-License-Identifier: GPL-3.0-or-later

package smart

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
	if r.IsNVMe() && r.NVMeHealth != nil {
		if r.NVMeHealth.CriticalWarning != 0 {
			return SeverityFailing
		}
		if r.NVMeHealth.MediaErrors > 0 {
			worst = SeverityCaution
		}
	}
	worst = max(worst, r.wearSeverity())
	if r.ATAAttributes != nil {
		for i := range r.ATAAttributes.Table {
			worst = max(worst, r.ATAAttributes.Table[i].Severity())
		}
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

// Severity classifies one ATA attribute; pre-fail vs old-age comes from the
// authoritative Flags.Prefailure bit, not the attribute name.
func (a *ATAAttribute) Severity() Severity {
	switch a.WhenFailed {
	case "FAILING_NOW":
		return SeverityFailing
	case "in_the_past":
		return SeverityCaution
	}
	// Thresh 0 means no threshold.
	if a.Thresh > 0 && a.Value <= a.Thresh {
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
