// SPDX-License-Identifier: GPL-3.0-or-later

// Package smart wraps the smartctl(8) JSON interface (smartmontools >= 7.0).
// The schema is drive-dependent: only device, smartctl and smart_status are
// reliably present, so every other field is a pointer or slice — nil-check
// before dereferencing.
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
	WWN             *WWN         `json:"wwn"`           // ATA World Wide Name
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
	ATADeviceStatistics   *ATADeviceStatistics   `json:"ata_device_statistics"`
	ATAPendingDefects     *ATAPendingDefects     `json:"ata_pending_defects_log"`
	ATASCTErc             *ATASCTErc             `json:"ata_sct_erc"`
	SATAPhyEvents         *SATAPhyEvents         `json:"sata_phy_event_counters"`

	// NVMe
	NVMeHealth        *NVMeHealth      `json:"nvme_smart_health_information_log"`
	NVMeErrorLog      *NVMeErrorLog    `json:"nvme_error_information_log"`
	NVMeSelfTestLog   *NVMeSelfTestLog `json:"nvme_self_test_log"`
	NVMeOptAdmin      *NVMeOptAdmin    `json:"nvme_optional_admin_commands"`
	NVMeTotalCapacity *int64           `json:"nvme_total_capacity"`

	// NVMe identity (cheap one-liners; each absent on drives that omit it).
	NVMeVersion            *StringValue   `json:"nvme_version"`
	NVMeNumberOfNamespaces *int           `json:"nvme_number_of_namespaces"`
	NVMeControllerID       *int           `json:"nvme_controller_id"`
	NVMePCIVendor          *NVMePCIVendor `json:"nvme_pci_vendor"`

	// Apple internal-SSD wear metrics, the endurance/spare fallback when the
	// standard NVMe health-log fields are absent.
	EnduranceUsed  *PercentValue   `json:"endurance_used"`
	SpareAvailable *SpareAvailable `json:"spare_available"`

	// Seagate FARM, fetched separately via FarmLog and attached by the caller.
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

// Temperature in Celsius. NVMe populates only Current; ATA adds the min-max block.
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

// StringValue is smartmontools' common {value:int, string:"..."} enum encoding.
type StringValue struct {
	Value  int    `json:"value"`
	String string `json:"string"`
}

// NVMePCIVendor identifies the controller's PCI vendor (and subsystem vendor).
type NVMePCIVendor struct {
	ID          int `json:"id"`
	SubsystemID int `json:"subsystem_id"`
}

// PercentValue is smartmontools' {current_percent:int} encoding.
type PercentValue struct {
	CurrentPercent int `json:"current_percent"`
}

// SpareAvailable is Apple's spare-capacity report with its depletion threshold.
type SpareAvailable struct {
	CurrentPercent   int `json:"current_percent"`
	ThresholdPercent int `json:"threshold_percent"`
}

// IsNVMe reports whether the report describes an NVMe drive.
func (r *Report) IsNVMe() bool { return r.Device.Protocol == "NVMe" }

// IsATA reports whether the report describes an ATA/SATA drive.
func (r *Report) IsATA() bool { return r.Device.Protocol == "ATA" }

// CurrentTemp returns the current Celsius reading, falling back from the
// generic block to the NVMe health log.
func (r *Report) CurrentTemp() (int, bool) {
	if r.Temperature != nil && r.Temperature.Current != nil {
		return *r.Temperature.Current, true
	}
	if r.NVMeHealth != nil && r.NVMeHealth.Temperature != nil {
		return *r.NVMeHealth.Temperature, true
	}
	return 0, false
}
