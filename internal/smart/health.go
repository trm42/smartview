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
// protocol-specific health indicators.
func (r *Report) Overall() Severity {
	if !r.SmartStatus.Passed {
		return SeverityFailing
	}
	worst := SeverityOK
	if r.IsNVMe() && r.NVMeHealth != nil {
		if r.NVMeHealth.CriticalWarning != 0 {
			return SeverityFailing
		}
		worst = max(worst, nvmeSeverity(r.NVMeHealth))
	}
	if r.ATAAttributes != nil {
		for i := range r.ATAAttributes.Table {
			worst = max(worst, r.ATAAttributes.Table[i].Severity())
		}
	}
	return max(worst, r.logSeverity())
}

// logSeverity grades the drive's own error logs, which the attribute table can
// miss entirely: an uncorrectable read is written to the ATA error log without
// necessarily moving any normalized value, so a drive can log a UNC and still
// present every attribute above its threshold. A populated log is the drive
// reporting a fault about itself and is worth a Caution.
//
// NVMe is deliberately excluded. Its num_err_log_entries is a lifetime counter
// that accumulates for entirely benign reasons (a rejected Identify field is an
// "error"), so a non-zero value there is a count worth reading, not a verdict.
// The Logs tab decodes the entries; grading on the bare number would cry wolf.
func (r *Report) logSeverity() Severity {
	if r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil && r.ATAErrorLog.Extended.Count > 0 {
		return SeverityCaution
	}
	if r.ATAPendingDefects != nil && r.ATAPendingDefects.Count > 0 {
		return SeverityCaution
	}
	return SeverityOK
}

// Severity classifies a single ATA attribute. The pre-fail vs old-age
// distinction comes from the authoritative Flags.Prefailure bit rather than a
// heuristic on the attribute name.
func (a *ATAAttribute) Severity() Severity {
	switch a.WhenFailed {
	case "FAILING_NOW":
		return SeverityFailing
	case "in_the_past":
		return SeverityCaution
	}
	// thresh of 0 means "no threshold"; only meaningful when positive.
	if a.Thresh > 0 && a.Value <= a.Thresh {
		if a.Flags.Prefailure {
			return SeverityFailing
		}
		return SeverityCaution
	}
	return SeverityOK
}

// PctUsedSeverity grades NVMe endurance consumption (percentage_used): a drive
// at or past 90% of its rated writes is worth a Caution.
func PctUsedSeverity(percent int) Severity {
	if percent >= 90 {
		return SeverityCaution
	}
	return SeverityOK
}

// nvmeSeverity grades NVMe wear/spare/media indicators below the
// critical-warning threshold.
func nvmeSeverity(h *NVMeHealth) Severity {
	sev := SeverityOK
	if h.MediaErrors > 0 {
		sev = max(sev, SeverityCaution)
	}
	if h.AvailableSpare != nil && h.AvailableSpareThreshold != nil &&
		*h.AvailableSpare <= *h.AvailableSpareThreshold {
		sev = max(sev, SeverityFailing)
	}
	if h.PercentageUsed != nil && *h.PercentageUsed >= 100 {
		sev = max(sev, SeverityCaution)
	}
	return sev
}
