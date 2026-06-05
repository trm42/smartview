// SPDX-License-Identifier: GPL-3.0-or-later

package smart

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

// NVMeErrorLog reports the NVMe error information log occupancy.
type NVMeErrorLog struct {
	Size int `json:"size"`
	Read int `json:"read"`
}

// NVMeSelfTestLog holds NVMe device self-test history. While a test runs,
// CurrentSelfTestOperation.Value is non-zero and CurrentCompletionPercent
// tracks progress.
type NVMeSelfTestLog struct {
	CurrentSelfTestOperation *StringValue        `json:"current_self_test_operation"`
	CurrentCompletionPercent *int                `json:"current_self_test_completion_percent"`
	Table                    []NVMeSelfTestEntry `json:"table"`
}

// NVMeOptAdmin mirrors nvme_optional_admin_commands; its SelfTest bit reports
// whether the controller implements the Device Self-test admin command.
type NVMeOptAdmin struct {
	SelfTest bool `json:"self_test"`
}

// NVMeSelfTestEntry is one NVMe self-test run.
type NVMeSelfTestEntry struct {
	SelfTestCode   StringValue `json:"self_test_code"`
	SelfTestResult StringValue `json:"self_test_result"`
	PowerOnHours   int         `json:"power_on_hours"`
}
