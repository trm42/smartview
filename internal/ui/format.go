// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"smartview/internal/smart"
)

// dash is rendered wherever a drive does not report a value.
const dash = "[gray]—[-]"

// severityColor maps a health severity to its display colour.
func severityColor(s smart.Severity) tcell.Color {
	switch s {
	case smart.SeverityFailing:
		return tcell.ColorRed
	case smart.SeverityCaution:
		return tcell.ColorYellow
	default:
		return tcell.ColorGreen
	}
}

// severityTag returns a tview colour tag for inline markup.
func severityTag(s smart.Severity) string {
	switch s {
	case smart.SeverityFailing:
		return "red"
	case smart.SeverityCaution:
		return "yellow"
	default:
		return "green"
	}
}

// healthGlyph is the coloured status dot shown beside each drive.
func healthGlyph(s smart.Severity) string {
	return fmt.Sprintf("[%s]●[-]", severityTag(s))
}

// humanBytes renders a byte count as a human-readable capacity.
func humanBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// humanDuration renders an hour count as years+days, falling back to days, or
// raw hours under a day. E.g. 9439h → "1 y 28 d", 100h → "4 d", 9h → "9 h".
func humanDuration(hours int) string {
	if hours < 24 {
		return fmt.Sprintf("%d h", hours)
	}
	days := hours / 24
	if days < 365 {
		return fmt.Sprintf("%d d", days)
	}
	return fmt.Sprintf("%d y %d d", days/365, days%365)
}

// orDash renders s, falling back to the dash placeholder when empty.
func orDash(s string) string {
	if s == "" {
		return dash
	}
	return s
}

// capacityString formats a drive's usable capacity, or a dash if unknown.
func capacityString(c *smart.Capacity) string {
	if c == nil || c.Bytes == 0 {
		return dash
	}
	return humanBytes(c.Bytes)
}

// tempString formats the current temperature in Celsius, or a dash.
func tempString(r *smart.Report) string {
	if t, ok := r.CurrentTemp(); ok {
		return fmt.Sprintf("%d°C", t)
	}
	return dash
}

// driveKind classifies the drive for the identity line (SSD vs HDD vs NVMe).
func driveKind(r *smart.Report) string {
	switch {
	case r.IsNVMe():
		return "NVMe SSD"
	case r.RotationRate != nil && *r.RotationRate > 0:
		return fmt.Sprintf("HDD @ %d rpm", *r.RotationRate)
	case r.IsATA():
		return "SATA SSD"
	default:
		return r.Device.Protocol
	}
}
