// SPDX-License-Identifier: GPL-3.0-or-later

// Package smart wraps the smartctl(8) JSON interface (smartmontools >= 7.0).
//
// The JSON schema is highly drive- and vendor-dependent: only device, smartctl
// and smart_status are reliably present. Every other field is modelled as a
// pointer or slice so an absent section decodes to nil rather than a zero value
// that the UI would misrender. Callers must nil-check before dereferencing.
package smart

// Report is the parsed result of `smartctl -j -x <device>`.
type Report struct {
	Smartctl        Smartctl     `json:"smartctl"`
	Device          Device       `json:"device"`
	ModelName       string       `json:"model_name"`
	ModelFamily     string       `json:"model_family"`
	SerialNumber    string       `json:"serial_number"`
	FirmwareVersion string       `json:"firmware_version"`
	UserCapacity    *Capacity    `json:"user_capacity"`
	RotationRate    *int         `json:"rotation_rate"` // absent/0 => SSD
	SmartStatus     SmartStatus  `json:"smart_status"`
	Temperature     *Temperature `json:"temperature"`
	PowerOnTime     *PowerOnTime `json:"power_on_time"`
	PowerCycleCount *int         `json:"power_cycle_count"`

	// ATA / SATA
	ATAAttributes         *ATAAttributes         `json:"ata_smart_attributes"`
	ATASelfTestLog        *ATASelfTestLog        `json:"ata_smart_self_test_log"`
	ATAErrorLog           *ATAErrorLog           `json:"ata_smart_error_log"`
	ATATemperatureHistory *ATATemperatureHistory `json:"ata_sct_temperature_history"`

	// NVMe
	NVMeHealth      *NVMeHealth      `json:"nvme_smart_health_information_log"`
	NVMeErrorLog    *NVMeErrorLog    `json:"nvme_error_information_log"`
	NVMeSelfTestLog *NVMeSelfTestLog `json:"nvme_self_test_log"`
}

// Smartctl holds metadata about the smartctl invocation itself.
type Smartctl struct {
	Version    []int     `json:"version"`
	ExitStatus int       `json:"exit_status"` // bitmask, NOT a success/fail flag
	Messages   []Message `json:"messages"`
}

// Message is a diagnostic line emitted by smartctl (e.g. permission errors).
type Message struct {
	String   string `json:"string"`
	Severity string `json:"severity"`
}

// Device identifies the drive. Name is round-tripped verbatim from --scan-open;
// never construct it (macOS uses IOService:/... paths, Linux uses /dev/...).
type Device struct {
	Name     string `json:"name"`
	InfoName string `json:"info_name"`
	Type     string `json:"type"`
	Protocol string `json:"protocol"` // "ATA", "NVMe", "SCSI"
}

// Capacity is the usable size of the drive.
type Capacity struct {
	Blocks int64 `json:"blocks"`
	Bytes  int64 `json:"bytes"`
}

// SmartStatus is the overall pass/fail verdict.
type SmartStatus struct {
	Passed bool `json:"passed"`
	NVMe   *struct {
		Value int `json:"value"` // critical_warning byte
	} `json:"nvme"`
}

// Temperature reports the current temperature in Celsius. NVMe drives populate
// only Current; ATA drives add the lifetime/cycle min-max block.
type Temperature struct {
	Current       *int `json:"current"`
	PowerCycleMin *int `json:"power_cycle_min"`
	PowerCycleMax *int `json:"power_cycle_max"`
	LifetimeMin   *int `json:"lifetime_min"`
	LifetimeMax   *int `json:"lifetime_max"`
}

// PowerOnTime is the accumulated power-on duration.
type PowerOnTime struct {
	Hours int `json:"hours"`
}

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

// ATATemperatureHistory is the SCT temperature log: up to ~128 recent samples,
// a ready-made source for a temperature sparkline.
type ATATemperatureHistory struct {
	LoggingIntervalMinutes int   `json:"logging_interval_minutes"`
	Table                  []int `json:"table"`
}

// ATASelfTestLog holds the extended self-test history.
type ATASelfTestLog struct {
	Extended *struct {
		Table []ATASelfTestEntry `json:"table"`
	} `json:"extended"`
}

// ATASelfTestEntry is one self-test run.
type ATASelfTestEntry struct {
	Type          StringValue `json:"type"`
	Status        StringValue `json:"status"`
	LifetimeHours int         `json:"lifetime_hours"`
}

// ATAErrorLog summarises logged ATA command errors.
type ATAErrorLog struct {
	Extended *struct {
		Count int `json:"count"`
	} `json:"extended"`
}

// NVMeHealth is the NVMe SMART/health information log.
type NVMeHealth struct {
	CriticalWarning         int   `json:"critical_warning"`
	Temperature             *int  `json:"temperature"`
	AvailableSpare          *int  `json:"available_spare"`
	AvailableSpareThreshold *int  `json:"available_spare_threshold"`
	PercentageUsed          *int  `json:"percentage_used"`
	DataUnitsRead           int64 `json:"data_units_read"`
	DataUnitsWritten        int64 `json:"data_units_written"`
	PowerOnHours            int   `json:"power_on_hours"`
	PowerCycles             int   `json:"power_cycles"`
	UnsafeShutdowns         int   `json:"unsafe_shutdowns"`
	MediaErrors             int   `json:"media_errors"`
	NumErrLogEntries        int   `json:"num_err_log_entries"`
	TemperatureSensors      []int `json:"temperature_sensors"`
}

// NVMeErrorLog reports the NVMe error information log occupancy.
type NVMeErrorLog struct {
	Size int `json:"size"`
	Read int `json:"read"`
}

// NVMeSelfTestLog holds NVMe device self-test history.
type NVMeSelfTestLog struct {
	CurrentSelfTestOperation *StringValue        `json:"current_self_test_operation"`
	Table                    []NVMeSelfTestEntry `json:"table"`
}

// NVMeSelfTestEntry is one NVMe self-test run.
type NVMeSelfTestEntry struct {
	SelfTestCode   StringValue `json:"self_test_code"`
	SelfTestResult StringValue `json:"self_test_result"`
	PowerOnHours   int         `json:"power_on_hours"`
}

// StringValue is smartmontools' common {value:int, string:"..."} enum encoding.
type StringValue struct {
	Value  int    `json:"value"`
	String string `json:"string"`
}

// IsNVMe reports whether the report describes an NVMe drive.
func (r *Report) IsNVMe() bool { return r.Device.Protocol == "NVMe" }

// IsATA reports whether the report describes an ATA/SATA drive.
func (r *Report) IsATA() bool { return r.Device.Protocol == "ATA" }

// CurrentTemp returns the current temperature in Celsius, preferring the
// generic temperature block and falling back to the NVMe health log.
func (r *Report) CurrentTemp() (int, bool) {
	if r.Temperature != nil && r.Temperature.Current != nil {
		return *r.Temperature.Current, true
	}
	if r.NVMeHealth != nil && r.NVMeHealth.Temperature != nil {
		return *r.NVMeHealth.Temperature, true
	}
	return 0, false
}
