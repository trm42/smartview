// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "iter"

// ATAAttributes is the classic vendor SMART attribute table.
type ATAAttributes struct {
	Table []ATAAttribute `json:"table"`
}

// ATAAttribute is one row of the SMART attribute table.
type ATAAttribute struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	Value      int      `json:"value"`
	Worst      int      `json:"worst"`
	Thresh     int      `json:"thresh"`
	WhenFailed string   `json:"when_failed"` // "", "FAILING_NOW", "in_the_past"
	Flags      ATAFlags `json:"flags"`
	Raw        ATARaw   `json:"raw"`
}

// ATAFlags carries the attribute's classification bits. Prefailure is the
// authoritative pre-fail vs old-age signal.
type ATAFlags struct {
	Prefailure bool   `json:"prefailure"`
	Value      int    `json:"value"`
	String     string `json:"string"`
}

// ATARaw is the vendor-specific raw value, with a pre-formatted string form.
type ATARaw struct {
	Value  int64  `json:"value"`
	String string `json:"string"`
}

// ATATemperatureHistory is the SCT temperature log (~128 recent samples).
type ATATemperatureHistory struct {
	LoggingIntervalMinutes int    `json:"logging_interval_minutes"`
	Table                  []*int `json:"table"` // nil where smartctl wrote null for an empty slot
}

// ATASelfTestLog is the self-test history: GP log 0x07, or the legacy SMART log without it.
type ATASelfTestLog struct {
	Extended *ATASelfTestTable `json:"extended"`
	Standard *ATASelfTestTable `json:"standard"`
}

// ATASelfTestTable is one self-test log; both shapes share the entry layout.
type ATASelfTestTable struct {
	Table []ATASelfTestEntry `json:"table"`
}

// ATASelfTests returns the self-test history, newest first, and whether the drive reported a log.
func (r *Report) ATASelfTests() ([]ATASelfTestEntry, bool) {
	l := r.ATASelfTestLog
	switch {
	case l == nil:
		return nil, false
	case l.Extended != nil:
		return l.Extended.Table, true
	case l.Standard != nil:
		return l.Standard.Table, true
	}
	return nil, false
}

// ATASelfTestEntry is one self-test run.
type ATASelfTestEntry struct {
	Type          StringValue `json:"type"`
	Status        StringValue `json:"status"`
	LifetimeHours int         `json:"lifetime_hours"`
}

// ATAErrorLog is the logged ATA command errors: GP log 0x03, or the legacy SMART log without it.
type ATAErrorLog struct {
	Extended *ATAErrorLogExtended `json:"extended"`
	Summary  *ATAErrorLogExtended `json:"summary"`
}

// ATAErrorLogExtended is one error log of either shape; Table is empty on a healthy drive.
type ATAErrorLogExtended struct {
	Count int                `json:"count"`
	Table []ATAErrorLogEntry `json:"table"`
}

// ATAErrorLogEntry is one logged error; only the fields both shapes share are modelled.
type ATAErrorLogEntry struct {
	ErrorNumber      int    `json:"error_number"`
	LifetimeHours    int    `json:"lifetime_hours"`
	ErrorDescription string `json:"error_description"`
}

// ATAErrors returns the drive's error log, preferring the extended shape; nil when neither is reported.
func (r *Report) ATAErrors() *ATAErrorLogExtended {
	l := r.ATAErrorLog
	switch {
	case l == nil:
		return nil
	case l.Extended != nil:
		return l.Extended
	}
	return l.Summary
}

// WWN is the drive's World Wide Name.
type WWN struct {
	NAA int   `json:"naa"`
	OUI int   `json:"oui"`
	ID  int64 `json:"id"`
}

// ATAPendingDefects is the Pending Defects log: sectors awaiting reallocation.
type ATAPendingDefects struct {
	Count int `json:"count"`
	Size  int `json:"size"`
}

// ATASCTErc is the SCT Error Recovery Control (TLER/ERC/CCTL) time limits.
type ATASCTErc struct {
	Read  *ERCTimer `json:"read"`
	Write *ERCTimer `json:"write"`
}

// ERCTimer is one ERC time limit in deciseconds; Enabled false means the
// firmware default applies.
type ERCTimer struct {
	Enabled     bool `json:"enabled"`
	Deciseconds int  `json:"deciseconds"`
}

// ATADeviceStatistics is the Device Statistics log (GP Log 0x04):
// vendor-neutral counters, more reliable than attribute raw values.
type ATADeviceStatistics struct {
	Pages []ATAStatPage `json:"pages"`
}

// ATAStatPage is one named page of the Device Statistics log.
type ATAStatPage struct {
	Number int            `json:"number"`
	Name   string         `json:"name"`
	Table  []ATAStatEntry `json:"table"`
}

// ATAStatEntry is one statistic; Value is only meaningful when Flags.Valid.
type ATAStatEntry struct {
	Offset int          `json:"offset"`
	Name   string       `json:"name"`
	Size   int          `json:"size"`
	Value  int64        `json:"value"`
	Flags  ATAStatFlags `json:"flags"`
}

// ATAStatFlags carries the per-statistic flag bits; only Valid is consumed.
type ATAStatFlags struct {
	Valid bool `json:"valid"`
}

// HasDeviceStats reports whether the Device Statistics log holds any valid entry.
func (r *Report) HasDeviceStats() bool {
	for range r.validStats() {
		return true
	}
	return false
}

// validStats yields the Device Statistics entries flagged valid; smartctl also emits placeholder rows.
func (r *Report) validStats() iter.Seq[ATAStatEntry] {
	return func(yield func(ATAStatEntry) bool) {
		if r.ATADeviceStatistics == nil {
			return
		}
		for _, p := range r.ATADeviceStatistics.Pages {
			for _, e := range p.Table {
				if e.Flags.Valid && !yield(e) {
					return
				}
			}
		}
	}
}

// ATASmartData carries SMART capability metadata: self-test durations, live
// self-test status, and the self-tests-supported capability bit.
type ATASmartData struct {
	SelfTest *struct {
		Status         *ATASelfTestStatus `json:"status"`
		PollingMinutes *SelfTestPolling   `json:"polling_minutes"`
	} `json:"self_test"`
	Capabilities *struct {
		SelfTestsSupported bool `json:"self_tests_supported"`
	} `json:"capabilities"`
}

// ATASelfTestStatus is the live self-test status. RemainingPercent is present
// only while a test runs; idle, String/Passed describe the last run.
type ATASelfTestStatus struct {
	Value            int    `json:"value"`
	String           string `json:"string"`
	Passed           bool   `json:"passed"`
	RemainingPercent *int   `json:"remaining_percent"`
}

// SelfTestPolling is how long each self-test type takes, in minutes.
type SelfTestPolling struct {
	Short      int `json:"short"`
	Extended   int `json:"extended"`
	Conveyance int `json:"conveyance"`
}

// SATAPhyEvents is the SATA PHY event counter log (flaky cable/connection signal).
type SATAPhyEvents struct {
	Table []SATAPhyCounter `json:"table"`
}

// SATAPhyCounter is one PHY event counter.
type SATAPhyCounter struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Value int64  `json:"value"`
}
