// SPDX-License-Identifier: GPL-3.0-or-later

package ui

// ataDesc maps common ATA SMART attribute IDs to a plain-language explanation,
// shown in the Attributes footer for the selected row. Unknown IDs fall back to
// the bare name and numbers.
var ataDesc = map[int]string{
	1:   "Raw Read Error Rate — rate of errors reading from the platters; vendor-encoded, watch for sudden drops.",
	3:   "Spin-Up Time — average time for the platters to reach full speed; rising values hint at motor wear.",
	4:   "Start/Stop Count — number of spindle start/stop cycles over the drive's life.",
	5:   "Reallocated Sector Count — sectors remapped after read/write errors; any rise signals a degrading disk.",
	7:   "Seek Error Rate — rate of head-positioning errors; vendor-encoded, a falling normalized value is concerning.",
	9:   "Power-On Hours — cumulative time the drive has been powered on.",
	10:  "Spin Retry Count — retries needed to spin the platters up; non-zero points to a struggling motor.",
	12:  "Power Cycle Count — number of full power-on/off cycles.",
	177: "Wear Leveling Count — remaining flash endurance headroom on SSDs; falls as the drive ages.",
	179: "Used Reserved Block Count — spare flash blocks already consumed; rising values mean wear.",
	181: "Program Fail Count — failed flash program (write) operations.",
	182: "Erase Fail Count — failed flash erase operations.",
	183: "Runtime Bad Block — bad blocks found during normal operation.",
	184: "End-to-End Error — data-path parity errors between cache and host; any value is serious.",
	187: "Reported Uncorrectable Errors — errors that ECC could not correct; should stay at zero.",
	188: "Command Timeout — operations that timed out; cabling/power issues or a failing drive.",
	190: "Airflow Temperature — drive temperature reported by the airflow sensor, in °C.",
	194: "Temperature — current drive temperature in °C.",
	195: "Hardware ECC Recovered — errors corrected by ECC; vendor-encoded, informational.",
	197: "Current Pending Sector — sectors waiting to be remapped; non-zero is an early warning.",
	198: "Offline Uncorrectable — sectors that could not be read even offline; should be zero.",
	199: "UDMA CRC Error Count — interface CRC errors, usually a bad SATA cable rather than the disk.",
	200: "Multi-Zone Error Rate — write errors across zones; vendor-encoded.",
	231: "SSD Life Left — estimated remaining endurance as a percentage.",
	235: "Power-Off Retract Count — emergency head retractions on unexpected power loss.",
	240: "Head Flying Hours — cumulative time the heads have spent flying over the platters.",
	241: "Total LBAs Written — lifetime data written to the drive.",
	242: "Total LBAs Read — lifetime data read from the drive.",
}

// nvmeDesc maps NVMe health-log field labels (as rendered in the table) to a
// plain-language explanation for the Attributes footer.
var nvmeDesc = map[string]string{
	"Critical warning":  "Critical Warning — bitmask of active alarms (spare low, read-only, overtemp); non-zero needs attention.",
	"Percentage used":   "Percentage Used — endurance consumed vs the drive's rated writes; can exceed 100%.",
	"Available spare":   "Available Spare — remaining spare flash capacity as a percentage.",
	"Spare threshold":   "Spare Threshold — spare level at which the drive raises a warning.",
	"Media errors":      "Media Errors — uncorrected data-integrity errors detected by the controller.",
	"Error log entries": "Error Log Entries — number of entries in the NVMe error information log.",
	"Power-on":          "Power-On Hours — cumulative time the drive has been powered on.",
	"Power cycles":      "Power Cycles — number of full power-on/off cycles.",
	"Unsafe shutdowns":  "Unsafe Shutdowns — power was lost without a clean shutdown handshake.",
	"Data read":         "Data Read — lifetime data read from the drive.",
	"Data written":      "Data Written — lifetime data written to the drive.",
	"Sensors":           "Temperature Sensors — per-sensor readings reported by the controller, in °C.",
}
