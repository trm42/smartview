// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// esc escapes drive-controlled free text (identity, log strings, attribute
// names) before it reaches markup-interpreting widgets — otherwise a hostile
// drive can inject colour tags and spoof the health display. Escape only the
// data, not the surrounding intentional tags.
//
// Control characters are folded to spaces as well: callers write the value into
// a line of its own, so an embedded newline would forge extra key/value rows.
func esc(s string) string {
	return tview.Escape(stripControl(s))
}

// stripControl replaces C0 controls, DEL and the C1 range with a space. A space
// is the benign substitution here: control characters carry no display meaning,
// and the widths every panel is laid out against stay right. strings.Map returns
// the original string when nothing changes, so the common case allocates nothing.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
}

// dash is defined in theme.go; it carries the active theme's muted colour.

// uiGutter is the standard horizontal inset between a box border and its
// text (SetBorderPadding on every text/table/list box). Vertical padding
// stays 0 for density; graphical widgets opt out to stay full-width.
const uiGutter = 1

// nestIndent is the leading whitespace for a line under an in-box header.
const nestIndent = "  "

// sectionHeader writes a top-level section heading. Bold, not accented: the
// accent marks focus and exceptions, and a heading is neither. Overview's
// panel headings are accented on purpose — they are sub-headings inside one
// box, a different thing from these.
func sectionHeader(b *strings.Builder, title string) {
	fmt.Fprintf(b, "[::b]%s[-:-:-]\n", title)
}

// titledBox applies the package's standard frame: a border, the uniform
// horizontal gutter, and a title. uiGutter's rule is "every text/table/list
// box"; this is where that is actually applied rather than re-typed.
func titledBox(b *tview.Box, title string) *tview.Box {
	return b.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(title)
}

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
	frac = min(max(frac, 0), 1)
	full, empty := barGlyphs(int(frac*float64(width)+0.5), width)
	return sevText(sev, full+empty)
}

// pctBarWidth is the cell width of every bar in the UI.
const pctBarWidth = 8

// barGlyphs is the one bar vocabulary: filled cells then empty ones, to a
// total of width. Returned as a pair so a caller that colours the two halves
// differently (progressBar) still spells the bar the same way — under mono the
// glyphs are all that survives.
func barGlyphs(filled, width int) (full, empty string) {
	filled = min(max(filled, 0), width)
	return strings.Repeat("█", filled), strings.Repeat("░", width-filled)
}

// pctBar renders a percentage as a severity-coloured bar plus the value.
// A FULLER BAR ALWAYS MEANS HEALTHIER; callers with a "consumed" percentage
// use pctBarUsed so opposite polarities never share a colour.
func pctBar(pct int, sev smart.Severity) string {
	full, empty := barGlyphs((clampPct(pct)*pctBarWidth+50)/100, pctBarWidth)
	return fmt.Sprintf("%s %d%%", sevText(sev, full+empty), pct)
}

// pctBarUsed renders a CONSUMED percentage: the bar drains as the drive wears
// (matching every other bar's polarity) while the number stays the "used" figure.
func pctBarUsed(pct int, sev smart.Severity) string {
	used := clampPct(pct)
	full, empty := barGlyphs(((100-used)*pctBarWidth+50)/100, pctBarWidth)
	return fmt.Sprintf("%s %d%%", sevText(sev, full+empty), used)
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

// healthGlyph is the coloured status dot shown beside each drive.
func healthGlyph(s smart.Severity) string {
	return sevText(s, "●")
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

// clampPct bounds a percentage into the 0..100 range every bar and gauge
// assumes.
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
// The fallback itself lives in smart.CapacityBytes.
func capacityString(r *smart.Report) string {
	if b, ok := r.CapacityBytes(); ok {
		return humanBytes(b)
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
	return sevBold(sev, s)
}

// tempCell is tempMarkup for a whole report, dash when unreported.
func tempCell(r *smart.Report) string {
	if t, ok := r.CurrentTemp(); ok {
		return tempMarkup(t)
	}
	return dash
}

// kindLabels names a drive kind at one verbosity. The classification itself is
// in kindLabel: two switches over the same four cases drift, and the fleet's
// short labels have to agree with the identity line's long ones.
type kindLabels struct{ nvme, hdd, ssd string }

var (
	longKindLabels  = kindLabels{nvme: "NVMe SSD", hdd: "HDD @ %d rpm", ssd: "SATA SSD"}
	shortKindLabels = kindLabels{nvme: "NVMe", hdd: "HDD", ssd: "SSD"}
)

// kindLabel classifies the drive and names it from the given label set. Only
// the HDD label may take the rotation rate, so it alone is a format string.
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
		// Device.Protocol is smartctl's own enum, not free text, but it reaches a
		// markup-interpreting sink either way.
		return esc(r.Device.Protocol)
	}
}

// driveKind classifies the drive for the identity line (SSD vs HDD vs NVMe).
func driveKind(r *smart.Report) string { return kindLabel(r, longKindLabels) }

// hangingWrap is one panel's key/value geometry. valueCol is the display
// column values start in — it must match the width that panel's rows pad the
// label to, or the cut lands inside the label. minValueW is the narrowest
// value column still worth wrapping into: a panel of short numbers can wrap
// into almost nothing, but one carrying a 150-character device path cannot,
// because WordWrap hard-splits a token with no break opportunity and the line
// count is what the panel is sized from.
type hangingWrap struct {
	valueCol  int
	minValueW int
}

// hangingIndent re-wraps over-long lines so overflow hangs under the value
// column; tview's own wrapping would break a value back to column 0, so
// callers disable it and pre-wrap here.
//
// valueCol is a display column, so the key is cut with splitAtWidth and the
// value re-wrapped by tview.WordWrap — both measure cells and treat style tags
// as zero-width.
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

// roundDuration renders an age at a sensible resolution: seconds under a
// minute, minutes under an hour, then hours. The coarse units are formatted
// rather than left to Duration.String, which would spell twelve minutes
// "12m0s". A cached reading's age only needs to say roughly how stale it is.
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
