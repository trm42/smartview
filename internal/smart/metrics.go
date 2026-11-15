// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"slices"
	"strings"
)

// Cross-protocol accessors over the sparse schema; each reports presence rather than a zero.

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
	for _, a := range r.Attributes() {
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

// deviceStat returns a valid Device Statistics counter by name.
func (r *Report) deviceStat(name string) (int64, bool) {
	for e := range r.validStats() {
		if e.Name == name {
			return e.Value, true
		}
	}
	return 0, false
}

// DriveKind is what a drive physically is.
type DriveKind int

const (
	KindNVMe DriveKind = iota + 1
	KindHDD
	KindSSD
)

// Kind classifies the drive, with the spindle speed for an HDD; an ATA drive
// with no rotation rate is an SSD only if it reports flash wear, else unknown.
func (r *Report) Kind() (kind DriveKind, rpm int, ok bool) {
	switch {
	case r.IsNVMe():
		return KindNVMe, 0, true
	case !r.IsATA():
		return 0, 0, false
	case r.RotationRate == nil:
		if r.reportsSSDWear() {
			return KindSSD, 0, true
		}
		return 0, 0, false
	case *r.RotationRate > 0:
		return KindHDD, *r.RotationRate, true
	default:
		return KindSSD, 0, true
	}
}

// ssdWearAttrs are smartctl drive-database names that only flash wear reports.
var ssdWearAttrs = map[string]bool{
	"Wear_Leveling_Count": true, "SSD_Life_Left": true, "Media_Wearout_Indicator": true,
	"Percent_Lifetime_Remain": true, "Remaining_Lifetime_Perc": true, "Percent_Lifetime_Used": true,
}

// reportsSSDWear reports whether the drive logs flash wear, which no spinning disk does.
func (r *Report) reportsSSDWear() bool {
	if r.EnduranceUsed != nil {
		return true
	}
	if r.ATADeviceStatistics != nil {
		for _, p := range r.ATADeviceStatistics.Pages {
			if p.Name == "Solid State Device Statistics" {
				return true
			}
		}
	}
	return slices.ContainsFunc(r.Attributes(), func(a ATAAttribute) bool { return ssdWearAttrs[a.Name] })
}

// SectorBytes is the logical block size (default 512 B), the unit of the
// "Logical Sectors *" counters.
func (r *Report) SectorBytes() int64 {
	if r.LogicalBlockSize != nil && *r.LogicalBlockSize > 0 {
		return int64(*r.LogicalBlockSize)
	}
	return 512
}

// DataUnitBytes converts an NVMe data-units count (thousands of 512-byte units) to bytes.
func DataUnitBytes(units int64) int64 { return units * 512 * 1000 }

// CapacityBytes returns the drive's usable size: user_capacity, else the NVMe
// total capacity (Apple internal SSDs report only the latter).
func (r *Report) CapacityBytes() (int64, bool) {
	if r.UserCapacity != nil && r.UserCapacity.Bytes > 0 {
		return r.UserCapacity.Bytes, true
	}
	if r.NVMeTotalCapacity != nil && *r.NVMeTotalCapacity > 0 {
		return *r.NVMeTotalCapacity, true
	}
	return 0, false
}

// CurrentTemp returns the current Celsius reading, falling back from the
// generic block to the NVMe health log.
func (r *Report) CurrentTemp() (int, bool) {
	if r.Temperature != nil && r.Temperature.Current != nil {
		return *r.Temperature.Current, true
	}
	if r.HasNVMeHealth() && r.NVMeHealth.Temperature != nil {
		return *r.NVMeHealth.Temperature, true
	}
	return 0, false
}

// PowerOnHours returns the accumulated power-on time in hours.
func (r *Report) PowerOnHours() (int, bool) {
	if r.PowerOnTime != nil {
		return r.PowerOnTime.Hours, true
	}
	if r.HasNVMeHealth() {
		return r.NVMeHealth.PowerOnHours, true
	}
	return 0, false
}

// PowerCycles returns the drive's power-cycle count.
func (r *Report) PowerCycles() (int, bool) {
	if r.PowerCycleCount != nil {
		return *r.PowerCycleCount, true
	}
	if r.HasNVMeHealth() {
		return r.NVMeHealth.PowerCycles, true
	}
	return 0, false
}

// WearSource is where a wear reading came from; only the NVMe health log's own fields feed a gauge.
type WearSource int

const (
	// WearHealthLog is the NVMe health log's percentage_used or available_spare.
	WearHealthLog WearSource = iota + 1
	// WearFallback is a top-level or Device Statistics reading.
	WearFallback
)

// LifeUsedPercent returns the percentage of rated write endurance consumed and
// its source; spinning disks report absent.
func (r *Report) LifeUsedPercent() (int, WearSource, bool) {
	if r.HasNVMeHealth() && r.NVMeHealth.PercentageUsed != nil {
		return *r.NVMeHealth.PercentageUsed, WearHealthLog, true
	}
	if r.EnduranceUsed != nil {
		return r.EnduranceUsed.CurrentPercent, WearFallback, true
	}
	if v, ok := r.deviceStat(StatPercentageUsed); ok {
		return int(v), WearFallback, true
	}
	return 0, 0, false
}

// SparePercent returns the available spare percentage, its depletion
// threshold (0 when unreported) and its source.
func (r *Report) SparePercent() (pct, threshold int, src WearSource, ok bool) {
	if r.HasNVMeHealth() && r.NVMeHealth.AvailableSpare != nil {
		thr := 0
		if r.NVMeHealth.AvailableSpareThreshold != nil {
			thr = *r.NVMeHealth.AvailableSpareThreshold
		}
		return *r.NVMeHealth.AvailableSpare, thr, WearHealthLog, true
	}
	if r.SpareAvailable != nil {
		return r.SpareAvailable.CurrentPercent, r.SpareAvailable.ThresholdPercent, WearFallback, true
	}
	return 0, 0, 0, false
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
	if r.HasNVMeHealth() {
		return WriteTotal{Bytes: DataUnitBytes(r.NVMeHealth.DataUnitsWritten), Source: WriteSourceNVMe}, true
	}
	if v, ok := r.deviceStat(StatSectorsWritten); ok {
		return WriteTotal{Bytes: v * r.SectorBytes(), Source: WriteSourceDeviceStats}, true
	}
	if v, ok := r.attrRaw(241); ok {
		return WriteTotal{Bytes: AttrLBABytes(v), Source: WriteSourceAttribute}, true
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

	pick(&e.Reallocated, 5, StatReallocated, StatReallocatedLegacy)
	pick(&e.Pending, 197, StatReallocCandidates)
	if e.Pending == nil && r.ATAPendingDefects != nil {
		e.Pending = new(int64(r.ATAPendingDefects.Count))
	}
	pick(&e.Uncorrectable, 198, StatUncorrectable)
	pick(&e.CRCErrors, 199, StatInterfaceCRC)

	if h := r.NVMeHealth; h != nil {
		e.MediaErrors = new(int64(h.MediaErrors))
		e.ErrorLogEntries = new(int64(h.NumErrLogEntries))
		e.UnsafeShutdowns = new(int64(h.UnsafeShutdowns))
	} else if l := r.ATAErrors(); l != nil {
		e.ErrorLogEntries = new(int64(l.Count))
	}
	return e
}

// TempHistory picks the best temperature history, newest last, and reports whether it
// is the drive's own log; that log lags by its logging interval, so the live reading closes it.
func (r *Report) TempHistory(runtime []float64) ([]float64, bool) {
	sct, ok := sctSeries(r)
	if !ok {
		return runtime, false
	}
	if now, ok := r.CurrentTemp(); ok && plausibleTemp(now) {
		sct = append(sct, float64(now))
	}
	return sct, true
}

// plausibleTemp rejects a reading no drive survives, which is a sensor glitch or an absent-sensor marker.
func plausibleTemp(c int) bool { return c > -40 && c < 200 }

// sctSeries returns the drive's own SCT temperature log, ok only when it holds a trend.
func sctSeries(r *Report) ([]float64, bool) {
	if r.ATATemperatureHistory == nil {
		return nil, false
	}
	out := make([]float64, 0, len(r.ATATemperatureHistory.Table))
	for _, v := range r.ATATemperatureHistory.Table {
		// An empty slot is null; skip it and any implausible value.
		if v != nil && plausibleTemp(*v) {
			out = append(out, float64(*v))
		}
	}
	return out, len(out) > 1
}

// AttrLBABytes converts an ATA attribute 241/242 LBA count to bytes, assuming 512-byte LBAs.
func AttrLBABytes(lbas int64) int64 { return lbas * 512 }
