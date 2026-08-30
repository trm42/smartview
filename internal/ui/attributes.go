// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// The Attributes tab pairs a selectable table with a description footer:
// attributesView for ATA, nvmeAttributesView for NVMe. Both refresh in place
// so the selected row survives polls.

// sortMode orders the ATA attribute rows.
type sortMode int

const (
	sortSeverity sortMode = iota // worst first, then by ID
	sortID                       // ascending ID (smartctl's native order)
	sortMargin                   // least threshold headroom first
)

func (m sortMode) String() string {
	switch m {
	case sortID:
		return "id"
	case sortMargin:
		return "margin"
	default:
		return "severity"
	}
}

// filterMode hides rows that are not currently of interest.
type filterMode int

const (
	filterAll        filterMode = iota // every attribute
	filterPrefail                      // only pre-fail attributes
	filterConcerning                   // only caution/failing attributes
)

func (m filterMode) String() string {
	switch m {
	case filterPrefail:
		return "pre-fail"
	case filterConcerning:
		return "concerning"
	default:
		return "all"
	}
}

// attrFooterHeight fits two borders plus three text rows: numbers, verdict,
// description (which wraps to two lines at usual widths).
const attrFooterHeight = 5

// attrNameWidth caps the attribute-name column so one long drive-supplied
// name can't set the whole table's width.
const attrNameWidth = 22

// attributesView is the ATA attribute table plus footer, with sort (s) and
// filter (f); rows rebuild in place so selection and focus survive.
// attrScaffold is the frame both attribute tables share: a selectable table
// over a description footer. Only the frame — the two views differ in how they
// restore selection (by attribute ID vs by row index) and in which keys they
// claim, so that stays with each.
type attrScaffold struct {
	*tview.Flex
	table  *scrollTable
	footer *tview.TextView
}

// newAttrScaffold assembles the frame; onSelect is handed the table row.
func newAttrScaffold(onSelect func(row int)) attrScaffold {
	s := attrScaffold{
		Flex:   tview.NewFlex().SetDirection(tview.FlexRow),
		table:  newScrollTable(),
		footer: tview.NewTextView().SetDynamicColors(true).SetWrap(true),
	}
	s.table.SetBorders(false).SetFixed(1, 0)
	s.table.SetSelectable(true, false)
	titledBox(s.footer.Box, "")
	s.table.SetSelectionChangedFunc(func(row, _ int) { onSelect(row) })
	s.AddItem(s.table, 0, 1, true)
	s.AddItem(s.footer, attrFooterHeight, 0, false)
	return s
}

// setFocused accents the table's border when the Attributes tab holds focus.
func (s attrScaffold) setFocused(focused bool) {
	s.table.SetBorderColor(borderColor(focused))
}

// footerRow maps a table row to an index into a rows slice of length n,
// clearing the footer and reporting false for the header row or past the end.
func (s attrScaffold) footerRow(row, n int) (int, bool) {
	i := row - 1
	if i < 0 || i >= n {
		s.footer.SetText("")
		return 0, false
	}
	return i, true
}

type attributesView struct {
	attrScaffold
	attrs  []smart.ATAAttribute
	shown  []smart.ATAAttribute // rows currently displayed; row i+1 → shown[i]
	sortBy sortMode
	filter filterMode
}

func newAttributesView(attrs []smart.ATAAttribute) *attributesView {
	v := &attributesView{attrs: attrs}
	v.attrScaffold = newAttrScaffold(func(row int) { v.updateFooter(row) })
	v.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Rune() {
		case 's':
			sel := v.selectedID()
			v.sortBy = (v.sortBy + 1) % 3
			v.renderRows()
			v.selectByID(sel)
			return nil
		case 'f':
			sel := v.selectedID()
			v.filter = (v.filter + 1) % 3
			v.renderRows()
			v.selectByID(sel)
			return nil
		}
		return ev
	})

	v.renderRows()
	v.selectByID(-1)
	return v
}

// refresh re-applies the latest data, keeping selection (by ID) and sort/filter.
func (v *attributesView) refresh(r *smart.Report, _ []float64) {
	if r.ATAAttributes == nil {
		return
	}
	sel := v.selectedID()
	v.attrs = r.ATAAttributes.Table
	v.renderRows()
	v.selectByID(sel)
}

// selectedID is the ID of the currently selected attribute, or -1 if none.
func (v *attributesView) selectedID() int {
	if row, _ := v.table.GetSelection(); row-1 >= 0 && row-1 < len(v.shown) {
		return v.shown[row-1].ID
	}
	return -1
}

// selectByID selects the row for attribute id (first row when absent/-1) and
// refreshes the footer.
func (v *attributesView) selectByID(id int) {
	if len(v.shown) == 0 {
		v.footer.SetText("")
		return
	}
	target := 1
	if id >= 0 {
		for i, a := range v.shown {
			if a.ID == id {
				target = i + 1
				break
			}
		}
	}
	v.table.Select(target, 0)
	v.updateFooter(target)
}

// renderRows rebuilds the table body for the current sort/filter, preserving
// the table primitive so focus is not lost; the caller applies selection.
func (v *attributesView) renderRows() {
	v.table.Clear()
	titledBox(v.table.Box, fmt.Sprintf(
		" SMART attributes — sort: %s · filter: %s  %s[s/f][-] ", v.sortBy, v.filter, accentTag()))

	headers := []string{"ID", "Attribute", "Kind", "State", "Margin", "Reading"}
	for c, h := range headers {
		v.table.SetCell(0, c, headerCell(h))
	}

	v.shown = v.visibleRows()
	if len(v.shown) == 0 {
		v.table.SetCell(1, 0, tview.NewTableCell(" No attributes match — all healthy ").
			SetTextColor(activeTheme.Neutral).SetSelectable(false))
		v.footer.SetText("")
		return
	}

	for i, a := range v.shown {
		v.setAttrRow(i+1, a)
	}
}

// setAttrRow fills table row (1-based) for attribute a; healthy rows render
// neutral.
func (v *attributesView) setAttrRow(row int, a smart.ATAAttribute) {
	color := attrTextColor(a.Severity())
	put := func(col int, text string, align int) {
		v.table.SetCell(row, col, bodyCell(text, color, align))
	}
	// Name and reading are drive-controlled: esc() blocks markup injection.
	put(0, fmt.Sprintf("%3d", a.ID), tview.AlignLeft)
	put(1, esc(truncateRunes(humanAttrName(a.Name), attrNameWidth)), tview.AlignLeft)
	put(2, attrKind(a), tview.AlignLeft)
	put(3, attrState(a), tview.AlignLeft)
	// Margin carries its own colour tags (a green bar even on a neutral row), so
	// it takes no text colour of its own.
	v.table.SetCell(row, 4, tview.NewTableCell(cellPad(marginCell(a), tview.AlignLeft)).
		SetSelectedStyle(selectedRowStyle(color)))
	// decodeReading returns "" when the drive reports no raw value; the themed
	// dash is substituted here, after escaping, as marginCell's cell does.
	put(5, orDash(esc(decodeReading(a))), tview.AlignRight)
}

// attrKind names pre-fail vs old-age from the authoritative flags bit.
func attrKind(a smart.ATAAttribute) string {
	if a.Flags.Prefailure {
		return "pre-fail"
	}
	return "old-age"
}

// attrState renders the attribute's condition as a word.
func attrState(a smart.ATAAttribute) string {
	switch a.Severity() {
	case smart.SeverityFailing:
		return "FAILING"
	case smart.SeverityCaution:
		if a.WhenFailed == "in_the_past" {
			return "failed once"
		}
		return "watch"
	default:
		return "ok"
	}
}

// attrLimits renders value/threshold for the footer; no threshold gets the
// dash, not a fabricated 0.
func attrLimits(a smart.ATAAttribute) string {
	if a.Thresh <= 0 {
		return fmt.Sprintf("%d/%s", a.Value, dash)
	}
	return fmt.Sprintf("%d/%d", a.Value, a.Thresh)
}

// visibleRows applies the current filter then sort.
func (v *attributesView) visibleRows() []smart.ATAAttribute {
	out := make([]smart.ATAAttribute, 0, len(v.attrs))
	for _, a := range v.attrs {
		switch v.filter {
		case filterPrefail:
			if !a.Flags.Prefailure {
				continue
			}
		case filterConcerning:
			if a.Severity() == smart.SeverityOK {
				continue
			}
		}
		out = append(out, a)
	}
	slices.SortStableFunc(out, func(x, y smart.ATAAttribute) int {
		switch v.sortBy {
		case sortID:
			return cmp.Compare(x.ID, y.ID)
		case sortMargin:
			return cmp.Compare(attrMargin(x), attrMargin(y))
		default: // severity: worst first, then by ID
			return cmp.Or(cmp.Compare(y.Severity(), x.Severity()), cmp.Compare(x.ID, y.ID))
		}
	})
	return out
}

// updateFooter writes the description and precise numbers for the selected row.
func (v *attributesView) updateFooter(row int) {
	i, ok := v.footerRow(row, len(v.shown))
	if !ok {
		return
	}
	a := v.shown[i]
	desc := ataDesc[a.ID]
	if desc == "" {
		desc = esc(humanAttrName(a.Name)) // drive-controlled fallback; escape markup
	}
	// Numbers first, verdict, then description: the description is what wraps,
	// so a narrow terminal runs out of prose rather than data.
	var b strings.Builder
	fmt.Fprintf(&b, "%s%d · %s[-]  %s%s · now/thr %s · worst %d · raw %s[-]\n",
		accentTag(), a.ID, esc(humanAttrName(a.Name)),
		mutedTag(), attrKind(a), attrLimits(a), a.Worst, esc(a.Raw.String))
	if verdict := attrVerdict(a); verdict != "" {
		fmt.Fprintf(&b, "%s\n", sevText(a.Severity(), verdict))
	}
	fmt.Fprintf(&b, "%s", desc)
	v.footer.SetText(b.String())
}

// attrVerdict states what the condition means for the reader; healthy
// attributes get nothing.
func attrVerdict(a smart.ATAAttribute) string {
	switch a.Severity() {
	case smart.SeverityFailing:
		return "▲ Below the drive's own threshold on a pre-fail attribute. " +
			"Back up now; replacement is the expected outcome."
	case smart.SeverityCaution:
		if a.WhenFailed == "in_the_past" {
			return "▲ This attribute failed at some point in the drive's life and has since " +
				"recovered. Worth watching for a repeat."
		}
		return "▲ Below the drive's own threshold. Old-age attributes wear out by design, " +
			"but a fresh drive reaching this is worth investigating."
	default:
		return ""
	}
}

// attrMargin is the threshold headroom; attributes without a threshold sort last.
func attrMargin(a smart.ATAAttribute) int {
	if a.Thresh <= 0 {
		return 1 << 30
	}
	return a.Value - a.Thresh
}

// marginCell renders the threshold headroom bar, or the dash when the drive
// sets no threshold.
func marginCell(a smart.ATAAttribute) string {
	if a.Thresh <= 0 {
		return dash
	}
	return marginBar(a.Value, a.Worst, a.Thresh, a.Severity())
}

// humanAttrName turns smartctl's snake_case attribute name into spaced words.
func humanAttrName(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}

// decodeReading converts well-known raw values into real-world figures,
// falling back to the raw string. An unreported value returns "": the themed
// dash is substituted at the sink rather than escaped along with the data.
func decodeReading(a smart.ATAAttribute) string {
	switch a.ID {
	case 9, 240: // power-on hours, head flying hours
		if n, ok := smart.LeadingInt(a.Raw.String); ok {
			return humanDuration(int(n))
		}
	case 241, 242: // total LBAs written / read (512-byte LBAs)
		if n, ok := smart.LeadingInt(a.Raw.String); ok {
			return humanBytes(n * 512)
		}
	case 190, 194: // airflow / drive temperature
		if n, ok := smart.LeadingInt(a.Raw.String); ok {
			return fmt.Sprintf("%d°C", n)
		}
	}
	return a.Raw.String
}

// attrKV is one row of the NVMe health table.
type attrKV struct {
	k   string
	v   string
	sev smart.Severity
}

// nvmeAttributesView renders the NVMe health log as a key/value table with a
// description footer, refreshing in place.
type nvmeAttributesView struct {
	attrScaffold
	rows []attrKV
}

func newNVMeAttributesView(h *smart.NVMeHealth) *nvmeAttributesView {
	v := &nvmeAttributesView{}
	v.attrScaffold = newAttrScaffold(func(row int) { v.setFooter(row) })
	titledBox(v.table.Box, " NVMe health log ")

	v.setRows(h)
	v.table.Select(1, 0)
	v.setFooter(1)
	return v
}

// refresh re-applies the latest data, keeping the selected row (field order
// is stable).
func (v *nvmeAttributesView) refresh(r *smart.Report, _ []float64) {
	if r.NVMeHealth == nil {
		return
	}
	row, _ := v.table.GetSelection()
	v.setRows(r.NVMeHealth)
	if row < 1 {
		row = 1
	}
	if row > len(v.rows) {
		row = len(v.rows)
	}
	if len(v.rows) > 0 {
		v.table.Select(row, 0)
	}
	v.setFooter(row)
}

// setRows rebuilds the table body from the health log.
func (v *nvmeAttributesView) setRows(h *smart.NVMeHealth) {
	v.rows = nvmeRows(h)
	v.table.Clear()
	v.table.SetCell(0, 0, headerCell("Field"))
	v.table.SetCell(0, 1, headerCell("Value"))
	for i, r := range v.rows {
		v.table.SetCell(i+1, 0, bodyCell(r.k, activeTheme.Neutral, tview.AlignLeft))
		v.table.SetCell(i+1, 1, bodyCell(r.v, severityColor(r.sev), tview.AlignLeft))
	}
}

// setFooter writes the description for the selected field.
func (v *nvmeAttributesView) setFooter(row int) {
	i, ok := v.footerRow(row, len(v.rows))
	if !ok {
		return
	}
	desc := nvmeDesc[v.rows[i].k]
	if desc == "" {
		desc = v.rows[i].k
	}
	v.footer.SetText(fmt.Sprintf("%s\n%s%s: %s[-]", desc, mutedTag(), v.rows[i].k, v.rows[i].v))
}

// nvmeRows builds the NVMe health key/value rows with per-row severity.
func nvmeRows(h *smart.NVMeHealth) []attrKV {
	var rows []attrKV
	add := func(k, v string, sev smart.Severity) { rows = append(rows, attrKV{k, v, sev}) }

	warnSev := smart.SeverityOK
	if h.CriticalWarning != 0 {
		warnSev = smart.SeverityFailing
	}
	add("Critical warning", fmt.Sprintf("0x%02x", h.CriticalWarning), warnSev)
	if h.PercentageUsed != nil {
		add("Percentage used", fmt.Sprintf("%d%%", *h.PercentageUsed), smart.PctUsedSeverity(*h.PercentageUsed))
	}
	if h.AvailableSpare != nil {
		sev := smart.SeverityOK
		if h.AvailableSpareThreshold != nil && *h.AvailableSpare <= *h.AvailableSpareThreshold {
			sev = smart.SeverityFailing
		}
		add("Available spare", fmt.Sprintf("%d%%", *h.AvailableSpare), sev)
	}
	if h.AvailableSpareThreshold != nil {
		add("Spare threshold", fmt.Sprintf("%d%%", *h.AvailableSpareThreshold), smart.SeverityOK)
	}
	mediaSev := smart.SeverityOK
	if h.MediaErrors > 0 {
		mediaSev = smart.SeverityCaution
	}
	add("Media errors", fmt.Sprintf("%d", h.MediaErrors), mediaSev)
	add("Error log entries", fmt.Sprintf("%d", h.NumErrLogEntries), smart.SeverityOK)
	add("Power-on", humanDuration(h.PowerOnHours), smart.SeverityOK)
	add("Power cycles", fmt.Sprintf("%d", h.PowerCycles), smart.SeverityOK)
	add("Unsafe shutdowns", fmt.Sprintf("%d", h.UnsafeShutdowns), smart.SeverityOK)
	add("Data read", humanBytes(smart.DataUnitBytes(h.DataUnitsRead)), smart.SeverityOK)
	add("Data written", humanBytes(smart.DataUnitBytes(h.DataUnitsWritten)), smart.SeverityOK)
	if h.HostReads > 0 || h.HostWrites > 0 {
		add("Read commands", fmt.Sprintf("%d", h.HostReads), smart.SeverityOK)
		add("Write commands", fmt.Sprintf("%d", h.HostWrites), smart.SeverityOK)
	}
	if h.ControllerBusyTime > 0 {
		add("Controller busy", humanMinutes(int(h.ControllerBusyTime)), smart.SeverityOK)
	}
	warnSevTemp := smart.SeverityOK
	if h.WarningTempTime > 0 {
		warnSevTemp = smart.SeverityCaution
	}
	add("Warn temp time", humanMinutes(h.WarningTempTime), warnSevTemp)
	critSevTemp := smart.SeverityOK
	if h.CriticalCompTime > 0 {
		critSevTemp = smart.SeverityCaution
	}
	add("Crit temp time", humanMinutes(h.CriticalCompTime), critSevTemp)
	if len(h.TemperatureSensors) > 0 {
		// Grade each sensor and the row on the hottest: the composite can sit
		// in range while one sensor is past the failing threshold.
		parts := make([]string, len(h.TemperatureSensors))
		rowSev := smart.SeverityOK
		for i, t := range h.TemperatureSensors {
			parts[i] = tempMarkup(t)
			rowSev = max(rowSev, tempSeverity(t))
		}
		add("Sensors", strings.Join(parts, ", "), rowSev)
	}
	return rows
}

// headerCell builds a non-selectable bold header cell.
func headerCell(s string) *tview.TableCell {
	return headerCellAligned(s, tview.AlignLeft)
}

// cellPad applies the table's one padding rule: a cell is padded both sides,
// except a right-aligned one, which takes a leading pad only. tview already
// spaces columns, and a trailing pad on a right-aligned cell pushes the value
// off its own edge — and costs real width across eight fleet columns.
func cellPad(s string, align int) string {
	if align == tview.AlignRight {
		return " " + s
	}
	return " " + s + " "
}

// headerCellAligned is headerCell with an explicit alignment.
func headerCellAligned(s string, align int) *tview.TableCell {
	return tview.NewTableCell(cellPad(s, align)).
		SetTextColor(activeTheme.Accent).
		SetAttributes(tcell.AttrBold).
		SetAlign(align).
		SetSelectable(false)
}

// bodyCell is headerCellAligned's counterpart for data rows: same padding
// rule, the row's own colour, and a selection style that keeps that colour.
func bodyCell(text string, color tcell.Color, align int) *tview.TableCell {
	return tview.NewTableCell(cellPad(text, align)).
		SetTextColor(color).
		SetAlign(align).
		SetSelectedStyle(selectedRowStyle(color))
}

// centeredNote is a placeholder primitive for empty/unsupported sections.
func centeredNote(msg string) tview.Primitive {
	tv := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetText("\n" + msg)
	titledBox(tv.Box, "")
	return tv
}
