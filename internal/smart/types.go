// SPDX-License-Identifier: GPL-3.0-or-later

// Package smart wraps the smartctl(8) JSON interface (smartmontools >= 7.0).
//
// The JSON schema is highly drive- and vendor-dependent: only device, smartctl
// and smart_status are reliably present. Every other field is modelled as a
// pointer or slice so an absent section decodes to nil rather than a zero value
// that the UI would misrender. Callers must nil-check before dereferencing.
package smart

import (
	"encoding/json"
	"strconv"
	"strings"
)

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

	// Drive geometry and interface (mostly ATA; absent fields decode to nil).
	LogicalBlockSize  *int            `json:"logical_block_size"`
	PhysicalBlockSize *int            `json:"physical_block_size"`
	FormFactor        *NamedValue     `json:"form_factor"`
	InterfaceSpeed    *InterfaceSpeed `json:"interface_speed"`
	SATAVersion       *StringValue    `json:"sata_version"`
	ATAVersion        *StringValue    `json:"ata_version"`
	Trim              *Trim           `json:"trim"`

	// ATA / SATA
	ATAAttributes         *ATAAttributes         `json:"ata_smart_attributes"`
	ATASmartData          *ATASmartData          `json:"ata_smart_data"`
	ATASelfTestLog        *ATASelfTestLog        `json:"ata_smart_self_test_log"`
	ATAErrorLog           *ATAErrorLog           `json:"ata_smart_error_log"`
	ATATemperatureHistory *ATATemperatureHistory `json:"ata_sct_temperature_history"`
	SATAPhyEvents         *SATAPhyEvents         `json:"sata_phy_event_counters"`

	// NVMe
	NVMeHealth        *NVMeHealth      `json:"nvme_smart_health_information_log"`
	NVMeErrorLog      *NVMeErrorLog    `json:"nvme_error_information_log"`
	NVMeSelfTestLog   *NVMeSelfTestLog `json:"nvme_self_test_log"`
	NVMeTotalCapacity *int64           `json:"nvme_total_capacity"`

	// Seagate FARM. Not part of `-j -x`; fetched separately via FarmLog and
	// attached by the caller, so it is never populated by Info's unmarshal.
	FARM *FARM `json:"-"`
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
	HostReads               int64 `json:"host_reads"`
	HostWrites              int64 `json:"host_writes"`
	ControllerBusyTime      int64 `json:"controller_busy_time"` // minutes
	WarningTempTime         int   `json:"warning_temp_time"`    // minutes
	CriticalCompTime        int   `json:"critical_comp_time"`   // minutes
}

// NamedValue is smartmontools' common {name:"...", ...} encoding (e.g. form_factor).
type NamedValue struct {
	Name string `json:"name"`
}

// InterfaceSpeed reports the negotiated vs maximum SATA link speed.
type InterfaceSpeed struct {
	Max     *LinkSpeed `json:"max"`
	Current *LinkSpeed `json:"current"`
}

// LinkSpeed is one interface-speed reading (e.g. "6.0 Gb/s").
type LinkSpeed struct {
	String string `json:"string"`
}

// Trim reports SSD TRIM/UNMAP support.
type Trim struct {
	Supported bool `json:"supported"`
}

// ATASmartData carries SMART capability metadata, notably self-test durations.
type ATASmartData struct {
	SelfTest *struct {
		PollingMinutes *SelfTestPolling `json:"polling_minutes"`
	} `json:"self_test"`
}

// SelfTestPolling is how long each self-test type takes, in minutes.
type SelfTestPolling struct {
	Short      int `json:"short"`
	Extended   int `json:"extended"`
	Conveyance int `json:"conveyance"`
}

// SATAPhyEvents is the SATA PHY event counter log — the best signal for a flaky
// cable or connection (CRC/COMRESET/handshake errors).
type SATAPhyEvents struct {
	Table []SATAPhyCounter `json:"table"`
}

// SATAPhyCounter is one PHY event counter.
type SATAPhyCounter struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Value int64  `json:"value"`
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

// SupportsFARM reports whether a FARM fetch is worth attempting for this drive.
// FARM is a Seagate ATA/SAS feature; gating on it avoids a wasted smartctl call
// on every non-Seagate SATA drive each poll. The actual support is confirmed by
// FARM.Supported once fetched.
func (r *Report) SupportsFARM() bool {
	if !r.IsATA() {
		return false
	}
	if strings.Contains(strings.ToLower(r.ModelFamily), "seagate") {
		return true
	}
	// Seagate model numbers conventionally start with "ST".
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(r.ModelName)), "ST")
}

// HasFARM reports whether a parsed, supported FARM log is attached.
func (r *Report) HasFARM() bool { return r.FARM != nil && r.FARM.Supported }

// FARM is a curated view of the Seagate Field Accessible Reliability Metrics
// log (`smartctl -l farm <dev> -j`). Only the fields smartview visualizes are
// modelled; the log carries far more.
type FARM struct {
	Supported   bool            `json:"supported"`
	DriveInfo   FARMDriveInfo   `json:"page_1_drive_information"`
	Workload    FARMWorkload    `json:"page_2_workload_statistics"`
	Errors      FARMErrors      `json:"page_3_error_statistics"`
	Environment FARMEnvironment `json:"page_4_environment_statistics"`
	Reliability FARMReliability `json:"page_5_reliability_statistics"`
}

// FARMDriveInfo is FARM page 1: identity, wear and recording-technology summary.
type FARMDriveInfo struct {
	Heads            int    `json:"number_of_heads"`
	POH              int    `json:"poh"` // power-on hours
	HeadFlightHours  int    `json:"head_flight_hours"`
	HeadLoadEvents   int    `json:"head_load_events"`
	PowerCycles      int    `json:"power_cycle_count"`
	ResetCount       int    `json:"reset_count"`
	RotationRate     int    `json:"rotation_rate"`
	RecordingType    string `json:"drive_recording_type"` // CMR / SMR
	LogicalSectorB   int64  `json:"logical_sector_size"`
	PhysicalSectorB  int64  `json:"physical_sector_size"`
	CapacityInSector int64  `json:"device_capacity_in_sectors"`
}

// FARMWorkload is FARM page 2: lifetime command and sector counters.
type FARMWorkload struct {
	TotalReadCommands   int64 `json:"total_read_commands"`
	TotalWriteCommands  int64 `json:"total_write_commands"`
	RandomReads         int64 `json:"total_random_reads"`
	RandomWrites        int64 `json:"total_random_writes"`
	OtherCommands       int64 `json:"total_other_commands"`
	LogicalSectorsRead  int64 `json:"logical_sectors_read"`
	LogicalSectorsWrite int64 `json:"logical_sectors_written"`
}

// FARMErrors is FARM page 3: lifetime error and reliability-event counters.
type FARMErrors struct {
	UnrecoverableRead  int64 `json:"number_of_unrecoverable_read_errors"`
	UnrecoverableWrite int64 `json:"number_of_unrecoverable_write_errors"`
	ReallocatedSectors int64 `json:"number_of_reallocated_sectors"`
	CandidateSectors   int64 `json:"number_of_reallocated_candidate_sectors"`
	MechStartFailures  int64 `json:"number_of_mechanical_start_failures"`
	CRCErrors          int64 `json:"total_crc_errors"`
	IOEDCErrors        int64 `json:"number_of_ioedc_errors"`
	CommandTimeouts    int64 `json:"command_time_out_count_total"`
	TotalASREvents     int64 `json:"total_asr_events"`
	TotalFlashLED      int64 `json:"total_flash_led"`
	Uncorrectables     int64 `json:"uncorrectables"`
}

// FARMEnvironment is FARM page 4: temperatures and power-rail telemetry.
type FARMEnvironment struct {
	CurrentTemp int `json:"curent_temp"` // smartctl spelling (sic)
	HighestTemp int `json:"highest_temp"`
	LowestTemp  int `json:"lowest_temp"`
	AverageTemp int `json:"average_temp"`
	MaxTemp     int `json:"max_temp"`
	MinTemp     int `json:"min_temp"`
	Current12V  int `json:"current_12v_in_mv"`
	Min12V      int `json:"minimum_12v_in_mv"`
	Max12V      int `json:"maximum_12v_in_mv"`
	Current5V   int `json:"current_5v_in_mv"`
	Min5V       int `json:"minimum_5v_in_mv"`
	Max5V       int `json:"maximum_5v_in_mv"`
}

// FARMReliability is FARM page 5. smartctl flattens the per-head arrays into
// numbered keys (e.g. mr_head_resistance_from_head_0..N), so a custom unmarshal
// gathers each family into an index-ordered slice.
type FARMReliability struct {
	ErrorRateNormalized     int
	SeekErrorRateNormalized int
	HighPriorityUnloads     int

	MRHeadResistance  []int // mr_head_resistance_from_head_K
	ReallocatedByHead []int // number_of_reallocated_sectors_by_head_K
	CandidateByHead   []int // number_of_reallocation_candidate_sectors_by_head_K
	WriteWorkloadPOH  []int // write_workload_power_on_time_by_head_K
}

// UnmarshalJSON decodes page 5, collecting the flat per-head keys into slices.
func (p *FARMReliability) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	intOf := func(key string) int {
		var v int
		if raw, ok := m[key]; ok {
			_ = json.Unmarshal(raw, &v) // absent/malformed scalar defaults to 0
		}
		return v
	}
	p.ErrorRateNormalized = intOf("error_rate_normalized")
	p.SeekErrorRateNormalized = intOf("seek_error_rate_normalized")
	p.HighPriorityUnloads = intOf("high_priority_unload_events")
	p.MRHeadResistance = collectByHead(m, "mr_head_resistance_from_head_")
	p.ReallocatedByHead = collectByHead(m, "number_of_reallocated_sectors_by_head_")
	p.CandidateByHead = collectByHead(m, "number_of_reallocation_candidate_sectors_by_head_")
	p.WriteWorkloadPOH = collectByHead(m, "write_workload_power_on_time_by_head_")
	return nil
}

// collectByHead gathers keys of the form "<prefix><N>" into an index-ordered
// slice [0..maxN], filling gaps with zero. Returns nil when none are present.
func collectByHead(m map[string]json.RawMessage, prefix string) []int {
	vals := map[int]int{}
	maxIdx := -1
	for k, raw := range m {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		idx, err := strconv.Atoi(k[len(prefix):])
		if err != nil {
			continue
		}
		var v int
		if json.Unmarshal(raw, &v) == nil {
			vals[idx] = v
			if idx > maxIdx {
				maxIdx = idx
			}
		}
	}
	if maxIdx < 0 {
		return nil
	}
	out := make([]int, maxIdx+1)
	for i := range out {
		out[i] = vals[i]
	}
	return out
}
