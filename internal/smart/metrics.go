// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "strings"

// This file holds the cross-protocol metric accessors: the handful of readings
// that mean the same thing on an ATA drive and an NVMe drive, resolved from
// whichever section of the sparse smartctl JSON actually carries them. They
// exist so callers comparing several drives at once (the fleet view) need not
// branch on protocol per metric, and so the fallback chains are unit-testable
// against the captured fixtures rather than buried in rendering code.
//
// Every accessor reports presence rather than substituting a zero: on this
// schema "not reported" and "reported as zero" are genuinely different answers,
// and a comparison that conflates them is misleading.

// LeadingInt parses the leading integer of s (smartctl raw attribute strings
// often read like "9201 (189 58 0)" or "37 (0 21 0 0 0)"), ignoring any
// trailing detail.
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

// attrRaw returns the raw value of the ATA attribute with the given id. The
// pre-formatted string is preferred (it is what smartctl considers the raw
// reading) with the numeric field as the fallback.
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

// deviceStat returns a Device Statistics counter by its smartctl name. Only
// entries flagged valid are considered; smartctl emits placeholder rows.
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

// sectorBytes is the logical block size, defaulting to the 512 B common case.
// "Logical Sectors *" counters are in these units, which are 4096 B on a 4Kn
// drive.
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

// LifeUsedPercent returns the fraction of the drive's rated write endurance
// consumed, as a percentage. NVMe reports it directly; Apple internal SSDs can
// omit the standard field and report endurance_used instead; SATA SSDs carry it
// in the (vendor-neutral) Device Statistics endurance indicator. Spinning disks
// have no endurance indicator at all and correctly report absent.
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

// SparePercent returns the available spare capacity and the threshold below
// which the drive considers it depleted, both as percentages. Named for the
// reading rather than the field, since Report.SpareAvailable is one of the two
// sources it resolves.
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

// TempRange returns the drive's recorded temperature extremes in Celsius,
// preferring the lifetime range over the current power cycle. This is an ATA
// block: NVMe drives report no such range and yield ok=false, leaving the
// caller to derive extremes from an observed series if it has one.
func (r *Report) TempRange() (min, max int, ok bool) {
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

// WriteSource identifies where a WriteTotal came from, because the sources are
// not equally trustworthy: only the attribute source has vendor-defined units.
type WriteSource int

const (
	// WriteSourceNVMe is the NVMe health log's data_units_written, defined by
	// the spec as thousands of 512-byte units.
	WriteSourceNVMe WriteSource = iota
	// WriteSourceDeviceStats is the ATA Device Statistics "Logical Sectors
	// Written" counter, a vendor-neutral count of logical blocks.
	WriteSourceDeviceStats
	// WriteSourceAttribute is ATA attribute 241 (Total_LBAs_Written). Its unit
	// is vendor-defined — some firmware counts 512-byte LBAs, others 32 MiB
	// chunks or GB — so the byte figure is an estimate, not a comparable total.
	WriteSourceAttribute
)

// WriteTotal is a lifetime host-write total together with its provenance.
type WriteTotal struct {
	Bytes  int64
	Source WriteSource
}

// Approximate reports whether the total was derived from a vendor-defined
// attribute unit and so must be presented as an estimate.
func (w WriteTotal) Approximate() bool { return w.Source == WriteSourceAttribute }

// DataWritten returns the lifetime host writes, preferring exact sources. The
// Device Statistics log is preferred over ATA attribute 241 because it is
// vendor-neutral and the log is explicitly the more reliable of the two (see
// ATADeviceStatistics); attribute 241 is used only as a last resort and is
// flagged approximate so callers can mark it.
func (r *Report) DataWritten() (WriteTotal, bool) {
	if r.NVMeHealth != nil {
		// data_units_written counts thousands of 512-byte units.
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

// ErrorCounts is the comparable subset of a drive's error counters. Fields are
// pointers because a nil counter ("this drive does not report it") and a zero
// counter ("this drive reports none") are different answers, and a fleet
// comparison that shows 0 for the former would be a lie.
//
// The first four are ATA readings, the last three NVMe — except ErrorLogEntries,
// which both protocols provide.
type ErrorCounts struct {
	Reallocated     *int64
	Pending         *int64
	Uncorrectable   *int64
	CRCErrors       *int64
	MediaErrors     *int64
	ErrorLogEntries *int64
	UnsafeShutdowns *int64
}

// ErrorCounts collects the drive's error counters from whichever sections
// report them, preferring SMART attributes and falling back to the vendor-neutral
// Device Statistics counters for drives that omit the attribute.
func (r *Report) ErrorCounts() ErrorCounts {
	var e ErrorCounts
	set := func(dst **int64, v int64) { n := v; *dst = &n }

	pick := func(dst **int64, attrID int, statNames ...string) {
		if v, ok := r.attrRaw(attrID); ok {
			set(dst, v)
			return
		}
		for _, n := range statNames {
			if v, ok := r.deviceStat(n); ok {
				set(dst, v)
				return
			}
		}
	}

	pick(&e.Reallocated, 5, "Number of Reallocated Logical Sectors", "Number of Reallocated Sectors")
	pick(&e.Pending, 197, "Number of Realloc. Candidate Logical Sectors")
	if e.Pending == nil && r.ATAPendingDefects != nil {
		set(&e.Pending, int64(r.ATAPendingDefects.Count))
	}
	pick(&e.Uncorrectable, 198, "Number of Reported Uncorrectable Errors")
	pick(&e.CRCErrors, 199, "Number of Interface CRC Errors")

	if h := r.NVMeHealth; h != nil {
		set(&e.MediaErrors, int64(h.MediaErrors))
		set(&e.ErrorLogEntries, int64(h.NumErrLogEntries))
		set(&e.UnsafeShutdowns, int64(h.UnsafeShutdowns))
	} else if r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil {
		set(&e.ErrorLogEntries, int64(r.ATAErrorLog.Extended.Count))
	}
	return e
}

// Worst returns the highest severity implied by the counters. Any nonzero
// reallocation, pending sector, uncorrectable error or media error is a caution:
// these are wear/damage signals rather than an immediate failure verdict, which
// remains the business of Overall(). Interface CRC errors are cabling faults and
// unsafe shutdowns are host-side, so neither grades the drive.
func (e ErrorCounts) Worst() Severity {
	for _, c := range []*int64{e.Reallocated, e.Pending, e.Uncorrectable, e.MediaErrors} {
		if c != nil && *c > 0 {
			return SeverityCaution
		}
	}
	return SeverityOK
}
