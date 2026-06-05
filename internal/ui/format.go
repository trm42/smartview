// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/trm42/smartview/internal/smart"
)

// dash is rendered wherever a drive does not report a value.
const dash = "[gray]—[-]"

// leadingInt parses the leading integer of s (smartctl raw strings often read
// like "9201 (189 58 0)" or "37 (0 21 0 0 0)"), ignoring any trailing detail.
func leadingInt(s string) (int64, bool) {
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

// marginBar renders a severity-coloured headroom bar for a normalized SMART
// value above its threshold: a fuller bar means more margin before the
// attribute is considered failing. base is the smallest standard top value
// (100/200/253) at least as large as the observed value/worst.
func marginBar(value, worst, thresh int, sev smart.Severity) string {
	const width = 10
	base := 100
	for _, b := range []int{200, 253} {
		if max(value, worst) > base {
			base = b
		}
	}
	span := base - thresh
	frac := 0.0
	if span > 0 {
		frac = float64(value-thresh) / float64(span)
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(width) + 0.5)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("[%s]%s[-] %d", severityTag(sev), bar, value-thresh)
}

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

// selectedRowStyle is the per-cell highlight for the selected table row. tview's
// default selection inverts a cell's own fg/bg, which makes neutral rows
// (ColorDefault text) vanish into the background. We instead paint an explicit
// highlight background and keep the cell's foreground colour so the text — and
// any severity colour — stays legible while selected.
func selectedRowStyle(fg tcell.Color) tcell.Style {
	// ColorDefault would resolve to the terminal's default foreground, which is
	// dark on light themes and thus illegible on the dark highlight; pin it to
	// white so neutral rows stay readable everywhere.
	if fg == tcell.ColorDefault {
		fg = tcell.ColorWhite
	}
	return tcell.StyleDefault.
		Background(tcell.ColorDarkSlateGray).
		Foreground(fg).
		Attributes(tcell.AttrBold)
}

// attrTextColor colours attribute row text: neutral for healthy rows so the
// table is easy to scan, reserving yellow/red for rows that need attention.
func attrTextColor(s smart.Severity) tcell.Color {
	switch s {
	case smart.SeverityFailing:
		return tcell.ColorRed
	case smart.SeverityCaution:
		return tcell.ColorYellow
	default:
		return tcell.ColorDefault
	}
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

// humanMinutes renders a minute count compactly: raw minutes under 90, else
// approximate hours. E.g. 1 → "1 min", 1804 → "~30 h".
func humanMinutes(m int) string {
	if m < 90 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("~%d h", (m+30)/60)
}

// orDash renders s, falling back to the dash placeholder when empty.
func orDash(s string) string {
	if s == "" {
		return dash
	}
	return s
}

// capacityBytes returns the drive's usable size, preferring user_capacity and
// falling back to nvme_total_capacity (Apple/NVMe drives often omit the former).
func capacityBytes(r *smart.Report) (int64, bool) {
	if r.UserCapacity != nil && r.UserCapacity.Bytes > 0 {
		return r.UserCapacity.Bytes, true
	}
	if r.NVMeTotalCapacity != nil && *r.NVMeTotalCapacity > 0 {
		return *r.NVMeTotalCapacity, true
	}
	return 0, false
}

// capacityString formats a report's usable capacity, or a dash if unknown.
func capacityString(r *smart.Report) string {
	if b, ok := capacityBytes(r); ok {
		return humanBytes(b)
	}
	return dash
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
