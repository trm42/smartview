// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "strings"

// Cross-protocol metric accessors, each resolving a fallback chain across the
// sparse smartctl JSON. Every accessor reports presence rather than
// substituting a zero: "not reported" and "reported as zero" are different
// answers on this schema.

// LeadingInt parses the leading integer of s, ignoring trailing detail like
// the "(189 58 0)" in smartctl raw attribute strings.
func LeadingInt(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	var n int64
	for _, c := range s[:end] {
		n = n*10 + int64(c-'0')
	}
	return n, true
}

// attrRaw returns the raw value of ATA attribute id, preferring the
// pre-formatted string over the numeric field.
func (r *Report) attrRaw(id int) (int64, bool) {
	if r.ATAAttributes == nil {
		return 0, false
	}
	for _, a := range r.ATAAttributes.Table {
		if a.ID != id {
			continue
		}
		if n, ok := LeadingInt(a.Raw.String); ok {
			return n, true
		}
		return a.Raw.Value, true
	}
	return 0, false
}

// deviceStat returns a Device Statistics counter by name, skipping entries
// not flagged valid (smartctl emits placeholder rows).
func (r *Report) deviceStat(name string) (int64, bool) {
	if r.ATADeviceStatistics == nil {
		return 0, false
	}
	for _, p := range r.ATADeviceStatistics.Pages {
		for _, e := range p.Table {
			if e.Name == name && e.Flags.Valid {
				return e.Value, true
			}
		}
	}
	return 0, false
}

// sectorBytes is the logical block size (default 512 B), the unit of the
// "Logical Sectors *" counters.
func (r *Report) sectorBytes() int64 {
	if r.LogicalBlockSize != nil && *r.LogicalBlockSize > 0 {
		return int64(*r.LogicalBlockSize)
	}
	return 512
}

// PowerOnHours returns the accumulated power-on time in hours.
func (r *Report) PowerOnHours() (int, bool) {
	if r.PowerOnTime != nil {
		return r.PowerOnTime.Hours, true
	}
	if r.NVMeHealth != nil {
		return r.NVMeHealth.PowerOnHours, true
	}
	return 0, false
}

// PowerCycles returns the drive's power-cycle count.
func (r *Report) PowerCycles() (int, bool) {
	if r.PowerCycleCount != nil {
		return *r.PowerCycleCount, true
	}
	if r.NVMeHealth != nil {
		return r.NVMeHealth.PowerCycles, true
	}
	return 0, false
}

// LifeUsedPercent returns the percentage of rated write endurance consumed:
// NVMe percentage_used, Apple endurance_used, or the Device Statistics
// endurance indicator. Spinning disks correctly report absent.
func (r *Report) LifeUsedPercent() (int, bool) {
	if r.NVMeHealth != nil && r.NVMeHealth.PercentageUsed != nil {
		return *r.NVMeHealth.PercentageUsed, true
	}
	if r.EnduranceUsed != nil {
		return r.EnduranceUsed.CurrentPercent, true
	}
	if v, ok := r.deviceStat("Percentage Used Endurance Indicator"); ok {
		return int(v), true
	}
	return 0, false
}

// SparePercent returns the available spare percentage and its depletion
// threshold.
func (r *Report) SparePercent() (pct, threshold int, ok bool) {
	if r.NVMeHealth != nil && r.NVMeHealth.AvailableSpare != nil {
		thr := 0
		if r.NVMeHealth.AvailableSpareThreshold != nil {
			thr = *r.NVMeHealth.AvailableSpareThreshold
		}
		return *r.NVMeHealth.AvailableSpare, thr, true
	}
	if r.SpareAvailable != nil {
		return r.SpareAvailable.CurrentPercent, r.SpareAvailable.ThresholdPercent, true
	}
	return 0, 0, false
}

// TempRange returns the recorded temperature extremes in Celsius, preferring
// lifetime over power-cycle. ATA only; NVMe reports no range and yields false.
func (r *Report) TempRange() (lo, hi int, ok bool) {
	if r.Temperature == nil {
		return 0, 0, false
	}
	if r.Temperature.LifetimeMin != nil && r.Temperature.LifetimeMax != nil {
		return *r.Temperature.LifetimeMin, *r.Temperature.LifetimeMax, true
	}
	if r.Temperature.PowerCycleMin != nil && r.Temperature.PowerCycleMax != nil {
		return *r.Temperature.PowerCycleMin, *r.Temperature.PowerCycleMax, true
	}
	return 0, 0, false
}

// WriteSource identifies where a WriteTotal came from; only the attribute
// source has vendor-defined units.
type WriteSource int

const (
	// WriteSourceNVMe is data_units_written (thousands of 512-byte units).
	WriteSourceNVMe WriteSource = iota
	// WriteSourceDeviceStats is the "Logical Sectors Written" counter.
	WriteSourceDeviceStats
	// WriteSourceAttribute is ATA attribute 241; its unit is vendor-defined,
	// so the byte figure is an estimate.
	WriteSourceAttribute
)

// WriteTotal is a lifetime host-write total together with its provenance.
type WriteTotal struct {
	Bytes  int64
	Source WriteSource
}

// Approximate reports whether the total must be presented as an estimate.
func (w WriteTotal) Approximate() bool { return w.Source == WriteSourceAttribute }

// DataWritten returns the lifetime host writes, preferring exact sources;
// attribute 241 is the last resort and flagged approximate.
func (r *Report) DataWritten() (WriteTotal, bool) {
	if r.NVMeHealth != nil {
		return WriteTotal{Bytes: r.NVMeHealth.DataUnitsWritten * 512 * 1000, Source: WriteSourceNVMe}, true
	}
	if v, ok := r.deviceStat("Logical Sectors Written"); ok {
		return WriteTotal{Bytes: v * r.sectorBytes(), Source: WriteSourceDeviceStats}, true
	}
	if v, ok := r.attrRaw(241); ok {
		return WriteTotal{Bytes: v * 512, Source: WriteSourceAttribute}, true
	}
	return WriteTotal{}, false
}

// ErrorCounts is the comparable subset of a drive's error counters. Fields
// are pointers: nil ("not reported") and zero ("reports none") are different
// answers. The first four are ATA, the last three NVMe, ErrorLogEntries both.
type ErrorCounts struct {
	Reallocated     *int64
	Pending         *int64
	Uncorrectable   *int64
	CRCErrors       *int64
	MediaErrors     *int64
	ErrorLogEntries *int64
	UnsafeShutdowns *int64
}

// ErrorCounts collects the error counters, preferring SMART attributes and
// falling back to Device Statistics.
func (r *Report) ErrorCounts() ErrorCounts {
	var e ErrorCounts

	// A field is assigned only on a hit, so an unreported counter stays nil
	// rather than becoming a zero.
	pick := func(dst **int64, attrID int, statNames ...string) {
		if v, ok := r.attrRaw(attrID); ok {
			*dst = new(v)
			return
		}
		for _, n := range statNames {
			if v, ok := r.deviceStat(n); ok {
				*dst = new(v)
				return
			}
		}
	}

	pick(&e.Reallocated, 5, "Number of Reallocated Logical Sectors", "Number of Reallocated Sectors")
	pick(&e.Pending, 197, "Number of Realloc. Candidate Logical Sectors")
	if e.Pending == nil && r.ATAPendingDefects != nil {
		e.Pending = new(int64(r.ATAPendingDefects.Count))
	}
	pick(&e.Uncorrectable, 198, "Number of Reported Uncorrectable Errors")
	pick(&e.CRCErrors, 199, "Number of Interface CRC Errors")

	if h := r.NVMeHealth; h != nil {
		e.MediaErrors = new(int64(h.MediaErrors))
		e.ErrorLogEntries = new(int64(h.NumErrLogEntries))
		e.UnsafeShutdowns = new(int64(h.UnsafeShutdowns))
	} else if r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil {
		e.ErrorLogEntries = new(int64(r.ATAErrorLog.Extended.Count))
	}
	return e
}

// Worst grades the counters: any nonzero wear/damage signal is a Caution.
// CRC errors (cabling) and unsafe shutdowns (host-side) don't grade the drive.
func (e ErrorCounts) Worst() Severity {
	for _, c := range []*int64{e.Reallocated, e.Pending, e.Uncorrectable, e.MediaErrors} {
		if c != nil && *c > 0 {
			return SeverityCaution
		}
	}
	return SeverityOK
}
