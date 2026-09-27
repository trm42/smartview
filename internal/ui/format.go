// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// esc escapes drive-controlled text for markup sinks and folds control characters to spaces, so a hostile drive cannot inject tags or forge rows.
func esc(s string) string {
	return tview.Escape(stripControl(s))
}

// stripControl replaces C0, DEL and C1 controls with a space.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
}

// uiGutter is the horizontal inset inside every text/table/list box.
const uiGutter = 1

// nestIndent is the leading whitespace for a line under an in-box header.
const nestIndent = "  "

// sectionHeader writes a bold top-level heading.
func sectionHeader(b *strings.Builder, title string) {
	fmt.Fprintf(b, "[::b]%s[-:-:-]\n", title)
}

// titledBox applies the standard border, gutter and title.
func titledBox(b *tview.Box, title string) *tview.Box {
	return b.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(title)
}

// boxInner is the text width inside a titledBox of the given outer width.
func boxInner(outerW int) int { return outerW - 2 - 2*uiGutter }

// lineCount counts s's lines, ignoring a trailing newline.
func lineCount(s string) int { return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1 }

// marginBar renders a severity-coloured headroom bar for a normalized value
// above its threshold; base is the smallest standard top (100/200/253) covering value/worst.
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
	frac = min(max(frac, 0), 1)
	full, empty := barGlyphs(int(frac*float64(width)+0.5), width)
	return sevText(sev, full+empty)
}

// pctBarWidth is the cell width of every bar in the UI.
const pctBarWidth = 8

// barGlyphs spells a bar as filled then empty cells.
func barGlyphs(filled, width int) (full, empty string) {
	filled = min(max(filled, 0), width)
	return strings.Repeat("█", filled), strings.Repeat("░", width-filled)
}

// pctBar renders a percentage as a severity-coloured bar plus the value; a fuller bar is healthier.
func pctBar(pct int, sev smart.Severity) string {
	full, empty := barGlyphs((clampPct(pct)*pctBarWidth+50)/100, pctBarWidth)
	return fmt.Sprintf("%s %d%%", sevText(sev, full+empty), pct)
}

// pctBarUsed renders a consumed percentage: the bar drains while the number stays "used".
func pctBarUsed(pct int, sev smart.Severity) string {
	used := clampPct(pct)
	full, empty := barGlyphs(((100-used)*pctBarWidth+50)/100, pctBarWidth)
	return fmt.Sprintf("%s %d%%", sevText(sev, full+empty), used)
}

// tempSeverity grades a temperature for display colouring only; health never derives from it.
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

// healthGlyph is the tinted severity mark; the shape carries severity where colour cannot.
func healthGlyph(s smart.Severity) string {
	return sevText(s, severityGlyph(s))
}

// severityGlyph is the bare mark for a severity, escalating by weight.
func severityGlyph(s smart.Severity) string {
	switch s {
	case smart.SeverityFailing:
		return "■"
	case smart.SeverityCaution:
		return "▲"
	default:
		return "●"
	}
}

// sevVerdict renders a verdict word; failing takes an inverse chip because
// red is darker than yellow on a dark ground, so tint alone cannot make it loudest.
func sevVerdict(sev smart.Severity, word string) string {
	if sev != smart.SeverityFailing {
		return sevBold(sev, word)
	}
	if activeTheme.Failing == tcell.ColorDefault {
		return "[::rb]" + word + "[-:-:-]"
	}
	return fmt.Sprintf("[%s:%s:b]%s[-:-:-]",
		tag(activeTheme.Inverse), tag(activeTheme.Failing), word)
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

// clampPct bounds a percentage into 0..100.
func clampPct(v int) int {
	return min(max(v, 0), 100)
}

// plural renders a count with the right noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// orDash renders s, falling back to the dash placeholder when empty.
func orDash(s string) string {
	if s == "" {
		return dash
	}
	return s
}

// capacityString formats a report's usable capacity, or a dash if unknown.
func capacityString(r *smart.Report) string {
	if b, ok := r.CapacityBytes(); ok {
		return humanBytes(b)
	}
	return dash
}

// tempMarkup tints a temperature only outside the OK band; its trailing reset
// returns to the widget default, so place it where that is harmless.
func tempMarkup(celsius int) string {
	s := fmt.Sprintf("%d°C", celsius)
	sev := tempSeverity(celsius)
	if sev == smart.SeverityOK {
		return s
	}
	return sevBold(sev, s)
}

// tempCell is tempMarkup for a whole report, dash when unreported.
func tempCell(r *smart.Report) string {
	if t, ok := r.CurrentTemp(); ok {
		return tempMarkup(t)
	}
	return dash
}

// kindLabels names a drive kind at one verbosity.
type kindLabels struct{ nvme, hdd, ssd string }

var (
	longKindLabels  = kindLabels{nvme: "NVMe SSD", hdd: "HDD @ %d rpm", ssd: "SATA SSD"}
	shortKindLabels = kindLabels{nvme: "NVMe", hdd: "HDD", ssd: "SSD"}
)

// kindLabel classifies the drive and names it from l; only the HDD label may carry %d.
func kindLabel(r *smart.Report, l kindLabels) string {
	switch {
	case r.IsNVMe():
		return l.nvme
	case r.RotationRate != nil && *r.RotationRate > 0:
		if strings.Contains(l.hdd, "%d") {
			return fmt.Sprintf(l.hdd, *r.RotationRate)
		}
		return l.hdd
	case r.IsATA():
		return l.ssd
	default:
		return esc(r.Device.Protocol)
	}
}

// driveKind classifies the drive for the identity line (SSD vs HDD vs NVMe).
func driveKind(r *smart.Report) string { return kindLabel(r, longKindLabels) }

// hangingWrap: valueCol must equal the padded label width; minValueW is the
// narrowest value column still worth wrapping into.
type hangingWrap struct {
	valueCol  int
	minValueW int
}

// hangingIndent re-wraps long lines so overflow hangs under the value column; callers disable tview wrapping.
func hangingIndent(text string, w hangingWrap, innerW int) string {
	valueCol := w.valueCol
	valueW := innerW - valueCol
	if valueW < w.minValueW || text == "" {
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
		for w, seg := range tview.WordWrap(value, valueW) {
			if w > 0 {
				out.WriteString("\n" + indent)
			}
			out.WriteString(seg)
		}
	}
	return out.String()
}

// splitAtWidth cuts s at display column col, never inside a style tag or escape
// sequence; a string narrower than col comes back whole.
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

// roundDuration renders an age at second, minute or hour resolution.
func roundDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute).Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Round(time.Hour).Hours()))
	}
}
