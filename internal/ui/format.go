// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// esc escapes drive-controlled free text (identity, log strings, attribute
// names) before it reaches markup-interpreting widgets — otherwise a hostile
// drive can inject colour tags and spoof the health display. Escape only the
// data, not the surrounding intentional tags.
func esc(s string) string {
	return tview.Escape(s)
}

// dash is defined in theme.go; it carries the active theme's muted colour.

// uiGutter is the standard horizontal inset between a box border and its
// text (SetBorderPadding on every text/table/list box). Vertical padding
// stays 0 for density; graphical widgets opt out to stay full-width.
const uiGutter = 1

// nestIndent is the leading whitespace for a line under an in-box header.
const nestIndent = "  "

// marginBar renders a severity-coloured headroom bar for a normalized SMART
// value above its threshold: fuller means more margin, same polarity as every
// other bar. base is the smallest standard top value (100/200/253) covering
// value/worst.
func marginBar(value, worst, thresh int, sev smart.Severity) string {
	const width = pctBarWidth
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
	return fmt.Sprintf("[%s]%s[-]", severityTag(sev), bar)
}

// pctBarWidth is the cell width of every bar in the UI.
const pctBarWidth = 8

// pctBar renders a percentage as a severity-coloured bar plus the value.
// A FULLER BAR ALWAYS MEANS HEALTHIER; callers with a "consumed" percentage
// use pctBarUsed so opposite polarities never share a colour.
func pctBar(pct int, sev smart.Severity) string {
	filled := (clampPct(pct)*pctBarWidth + 50) / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", pctBarWidth-filled)
	return fmt.Sprintf("[%s]%s[-] %d%%", severityTag(sev), bar, pct)
}

// pctBarUsed renders a CONSUMED percentage: the bar drains as the drive wears
// (matching every other bar's polarity) while the number stays the "used" figure.
func pctBarUsed(pct int, sev smart.Severity) string {
	used := clampPct(pct)
	filled := ((100-used)*pctBarWidth + 50) / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", pctBarWidth-filled)
	return fmt.Sprintf("[%s]%s[-] %d%%", severityTag(sev), bar, used)
}

// borderColor returns the accent colour for a focused pane, muted otherwise.
func borderColor(focused bool) tcell.Color {
	if focused {
		return activeTheme.Accent
	}
	return activeTheme.Muted
}

// tempSeverity grades a temperature for display colouring only; health never
// derives from raw temperature, so these thresholds stay in the UI layer.
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

// severityTag returns the bare colour token, for callers interpolating into "[%s]".
func severityTag(s smart.Severity) string {
	return tag(severityColor(s))
}

// healthGlyph is the coloured status dot shown beside each drive.
func healthGlyph(s smart.Severity) string {
	return fmt.Sprintf("[%s]●[-]", severityTag(s))
}

// selectedRowStyle is the selected-row highlight: an explicit background that
// keeps the cell's own foreground (tview's default inversion makes neutral
// rows vanish).
func selectedRowStyle(fg tcell.Color) tcell.Style {
	// Pin ColorDefault to SelectionFg — the terminal default can be illegible
	// on the highlight.
	if fg == tcell.ColorDefault {
		fg = activeTheme.SelectionFg
	}
	return tcell.StyleDefault.
		Background(activeTheme.SelectionBg).
		Foreground(fg).
		Attributes(tcell.AttrBold)
}

// styleList applies the theme to a List: pins the secondary-text colour
// (tview defaults it to a green that leaks into every theme) and routes
// selection through selectedRowStyle so list and table selections match.
// Re-call after a theme change.
func styleList(l *tview.List) {
	l.SetSecondaryTextColor(activeTheme.ListSecondary)
	l.SetSelectedStyle(selectedRowStyle(activeTheme.SelectionFg))
}

// attrTextColor colours attribute row text: neutral when healthy, so only
// rows needing attention are tinted.
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

// humanDuration renders hours as "1 y 28 d" / "4 d" / "9 h".
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

// humanMinutes renders minutes under 90 raw, else approximate hours ("~30 h").
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

// capacityBytes returns the usable size: user_capacity, else nvme_total_capacity.
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

// tempMarkup tints a temperature only once it leaves the OK band (colour
// marks exceptions, not membership). Its trailing style reset returns to the
// widget default, so callers must place it where that is harmless.
func tempMarkup(celsius int) string {
	s := fmt.Sprintf("%d°C", celsius)
	sev := tempSeverity(celsius)
	if sev == smart.SeverityOK {
		return s
	}
	return fmt.Sprintf("[%s::b]%s[-:-:-]", severityTag(sev), s)
}

// tempCell is tempMarkup for a whole report, dash when unreported.
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

// hangingIndent re-wraps over-long lines so overflow hangs under the value
// column; tview's own wrapping would break a value back to column 0, so
// callers disable it and pre-wrap here.
//
// valueCol is a display column, so the key is cut with splitAtWidth and the
// value re-wrapped by tview.WordWrap — both measure cells and treat style tags
// as zero-width.
func hangingIndent(text string, valueCol, innerW int) string {
	valueW := innerW - valueCol
	if valueW <= 8 || text == "" {
		return text
	}
	indent := strings.Repeat(" ", valueCol)
	var out strings.Builder
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		if tview.TaggedStringWidth(line) <= innerW {
			out.WriteString(line)
			continue
		}
		key, value := splitAtWidth(line, valueCol)
		if value == "" { // narrower than the value column: nothing to hang
			out.WriteString(line)
			continue
		}
		out.WriteString(key)
		// WordWrap breaks at the last opportunity that fits and hard-splits a
		// token with none — a macOS IOService path is 150+ characters, and the
		// caller's SetWrap(false) would simply cut it at the border.
		for w, seg := range tview.WordWrap(value, valueW) {
			if w > 0 {
				out.WriteString("\n" + indent)
			}
			out.WriteString(seg)
		}
	}
	return out.String()
}

// splitAtWidth cuts s at display column col: head is the shortest leading run
// whose rendered width reaches col (zero-width style tags ride along with it),
// tail is the remainder, byte-for-byte untouched. A string narrower than col
// comes back whole with an empty tail.
//
// The cut has to land on a display column rather than a byte offset: "[::b]Model
// [-:-:-] " is 27 bytes of which 12 are markup worth no cells at all, so a byte
// slice at column 15 lands in the middle of the key. tview's width function is
// the authority on what a tag is worth, so candidates are measured with it and
// one that lands inside a tag (or inside an escaped "[[]" sequence) is rejected:
// the halves then measure wider than the whole, because the severed fragment
// stops being markup and starts counting as literal text.
func splitAtWidth(s string, col int) (head, tail string) {
	total := tview.TaggedStringWidth(s)
	for i := range s {
		w := tview.TaggedStringWidth(s[:i])
		if w+tview.TaggedStringWidth(s[i:]) != total {
			continue // the cut falls inside a tag
		}
		if w >= col {
			return s[:i], s[i:]
		}
	}
	return s, ""
}
