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

// logSeverity grades the drive's own error logs: an uncorrectable read can be
// logged without moving any normalized attribute value. NVMe's
// num_err_log_entries is deliberately excluded — it accumulates for benign
// reasons, so it is a count to display, not a verdict.
func (r *Report) logSeverity() Severity {
	if r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil && r.ATAErrorLog.Extended.Count > 0 {
		return SeverityCaution
	}
	if r.ATAPendingDefects != nil && r.ATAPendingDefects.Count > 0 {
		return SeverityCaution
	}
	return SeverityOK
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
	// Thresh 0 means "no threshold".
	if a.Thresh > 0 && a.Value <= a.Thresh {
		if a.Flags.Prefailure {
			return SeverityFailing
		}
		return SeverityCaution
	}
	return SeverityOK
}

// PctUsedSeverity grades NVMe endurance consumption: Caution at >= 90%.
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
