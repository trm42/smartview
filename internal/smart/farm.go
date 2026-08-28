// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"encoding/json"
	"strconv"
	"strings"
)

// SupportsFARM reports whether a FARM fetch is worth attempting (Seagate
// only); actual support is confirmed by FARM.Supported once fetched.
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
// log; only the fields smartview visualizes are modelled.
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

// FARMReliability is FARM page 5. smartctl flattens per-head arrays into
// numbered keys, so a custom unmarshal gathers each family into a slice.
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
