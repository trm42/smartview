// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// esc escapes drive-controlled free text before it is inserted into a widget
// that interprets tview colour/region markup (every SetDynamicColors(true)
// TextView, and all table cells). SMART identity and log fields — model name,
// serial, firmware, self-test/error strings, PHY-counter and attribute names —
// originate from the device and are attacker-controllable (writable via vendor
// tooling, or fully forged by a hostile USB enclosure). Without escaping, a
// failing drive whose model_name is "Disk [green]HEALTHY[-]" could paint a fake
// healthy verdict or recolour the real severity. esc wraps only the data; the
// surrounding intentional tags stay literal.
func esc(s string) string {
	return tview.Escape(s)
}

// dash is defined in theme.go (it carries the active theme's muted colour and is
// recomputed in setTheme); its call sites here use it as a plain value.

// uiGutter is the standard horizontal inset (cells) between a box border and its
// text, applied via SetBorderPadding on every text/table/list box so the left
// margin is identical across tabs. Vertical padding stays 0 for density; graphical
// widgets (gauges, sparkline, bar charts) opt out to keep visualizations full-width.
const uiGutter = 1

// nestIndent is the leading whitespace for a line subordinate to an in-box header
// (e.g. self-test entries under "Self-test history").
const nestIndent = "  "

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

// pctBarWidth is the cell width of pctBar. Narrower than marginBar's ten: the
// fleet table shows two of these side by side and still needs room for the
// numbers.
const pctBarWidth = 8

// pctBar renders a plain percentage as a severity-coloured bar followed by the
// value — the fleet view's compact form for life-used and spare. marginBar is
// the sibling for normalized SMART values, which need a threshold-relative
// scale rather than a straight 0-100 one.
func pctBar(pct int, sev smart.Severity) string {
	filled := (clampPct(pct)*pctBarWidth + 50) / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", pctBarWidth-filled)
	return fmt.Sprintf("[%s]%s[-] %d%%", severityTag(sev), bar, pct)
}

// borderColor returns the accent border colour for a pane that holds keyboard
// focus and a dim one for an unfocused pane, so the active container is obvious.
// Driven from App.refreshFocusChrome on every focus/tab transition.
func borderColor(focused bool) tcell.Color {
	if focused {
		return activeTheme.Accent
	}
	return activeTheme.Muted
}

// tempSeverity grades a drive temperature for display colouring only. The data
// layer derives health from SMART status and attributes, never from raw
// temperature, so these thresholds live in the UI layer to keep internal/smart
// untouched.
func tempSeverity(celsius int) smart.Severity {
	switch {
	case celsius >= 65:
		return smart.SeverityFailing
	case celsius >= 55:
		return smart.SeverityCaution
	default:
		return smart.SeverityOK
	}
}

// severityColor maps a health severity to its display colour.
func severityColor(s smart.Severity) tcell.Color {
	switch s {
	case smart.SeverityFailing:
		return activeTheme.Failing
	case smart.SeverityCaution:
		return activeTheme.Caution
	default:
		return activeTheme.OK
	}
}

// severityTag returns a tview colour token (e.g. "#dc322f", or "-" under the
// mono theme) for inline markup. Every caller interpolates it into a "[%s]"
// bracket, so the bare token is what they need.
func severityTag(s smart.Severity) string {
	return tag(severityColor(s))
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
		fg = activeTheme.SelectionFg
	}
	return tcell.StyleDefault.
		Background(activeTheme.SelectionBg).
		Foreground(fg).
		Attributes(tcell.AttrBold)
}

// styleList applies the active theme to a List: the secondary-text colour and a
// selected-row highlight that matches the themed table selection (SelectionBg/Fg
// via selectedRowStyle) rather than tview's default white inverse, so list and
// table selections look the same. tview Lists default their secondary text to
// Styles.TertiaryTextColor (green), which would otherwise leak green into every
// theme; this pins it to ListSecondary. Re-call after a theme change.
func styleList(l *tview.List) {
	l.SetSecondaryTextColor(activeTheme.ListSecondary)
	l.SetSelectedStyle(selectedRowStyle(activeTheme.SelectionFg))
}

// attrTextColor colours attribute row text: neutral for healthy rows so the
// table is easy to scan, reserving yellow/red for rows that need attention.
func attrTextColor(s smart.Severity) tcell.Color {
	switch s {
	case smart.SeverityFailing:
		return activeTheme.Failing
	case smart.SeverityCaution:
		return activeTheme.Caution
	default:
		return activeTheme.Neutral
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

// tempMarkup formats a temperature and tints it once it leaves the OK band.
// Colour marks exceptions rather than membership: an in-band reading keeps the
// surrounding text colour, so a healthy fleet is not a wall of green and a hot
// drive is the only thing tinted. Callers must place it where a trailing style
// reset is harmless — the closing tag returns to the widget default, not to the
// caller's colour.
func tempMarkup(celsius int) string {
	s := fmt.Sprintf("%d°C", celsius)
	sev := tempSeverity(celsius)
	if sev == smart.SeverityOK {
		return s
	}
	return fmt.Sprintf("[%s::b]%s[-:-:-]", severityTag(sev), s)
}

// tempCell is tempMarkup for a whole report, falling back to the dash when the
// drive reports no temperature.
func tempCell(r *smart.Report) string {
	if t, ok := r.CurrentTemp(); ok {
		return tempMarkup(t)
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
