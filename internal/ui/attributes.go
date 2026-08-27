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

// The Attributes tab pairs a selectable table with a description footer for the
// highlighted row: a rich, decoded ATA attribute table for ATA drives
// (attributesView), or the NVMe health-log table for NVMe drives
// (nvmeAttributesView). Both refresh their data in place so the selected row
// survives polls. The views are constructed from detail.buildTabView.

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

// attrFooterHeight is the footer's fixed height: two borders and three text
// rows — the numbers, the description, and what the state means for the reader.
// It was four (two text rows), and the description alone wraps to two at any
// usual width, so the numbers fell out of the box with no scroll cue.
const attrFooterHeight = 5

// attrNameWidth bounds the attribute-name column. Names come from the drive, so
// without a cap a single long one sets the width for the whole table.
const attrNameWidth = 22

// attributesView is the ATA attribute table plus a description footer, with
// interactive sort (s) and filter (f). It rebuilds rows in place so selection
// and focus survive a re-render.
type attributesView struct {
	*tview.Flex
	table  *scrollTable
	footer *tview.TextView
	attrs  []smart.ATAAttribute
	shown  []smart.ATAAttribute // rows currently displayed; row i+1 → shown[i]
	sortBy sortMode
	filter filterMode
}

func newAttributesView(attrs []smart.ATAAttribute) *attributesView {
	v := &attributesView{
		Flex:   tview.NewFlex().SetDirection(tview.FlexRow),
		table:  newScrollTable(),
		footer: tview.NewTextView().SetDynamicColors(true).SetWrap(true),
		attrs:  attrs,
	}
	v.table.SetBorders(false).SetFixed(1, 0)
	v.table.SetSelectable(true, false)
	v.footer.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter)

	v.table.SetSelectionChangedFunc(func(row, _ int) { v.updateFooter(row) })
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

	v.AddItem(v.table, 0, 1, true)
	v.AddItem(v.footer, attrFooterHeight, 0, false)
	v.renderRows()
	v.selectByID(-1)
	return v
}

// setFocused accents the table's border when the Attributes tab holds focus.
func (v *attributesView) setFocused(focused bool) {
	v.table.SetBorderColor(borderColor(focused))
}

// refresh re-applies the latest attribute data, keeping the selected attribute
// (by ID) and the current sort/filter so a poll never disturbs the user.
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

// selectByID selects the row showing attribute id (or the first row when id is
// not present / -1), and refreshes the footer.
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

// renderRows rebuilds the table body for the current sort/filter, preserving the
// table primitive so focus is not lost. Selection is applied separately by the
// caller (selectByID) so it can be retained across re-renders.
func (v *attributesView) renderRows() {
	v.table.Clear()
	v.table.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(fmt.Sprintf(
		" SMART attributes — sort: %s · filter: %s  %s[s/f][-] ", v.sortBy, v.filter, accentTag()))

	// The ID earns its column: every SMART reference is indexed by it, and an
	// unnamed vendor attribute ("Unknown Attribute") has no other handle. It used
	// to live only in the footer, which clips (see updateFooter).
	//
	// "Health" is split into State and Margin because one column cannot carry two
	// encodings: it held a headroom bar for thresholded attributes and a bare dot
	// for the rest, with no key, and the bar's number was an unlabelled
	// value-minus-threshold that printed negative on the one row that mattered.
	//
	// "When" is gone: it was 22 rows of "-" that truncated to "FAILING_NO…"
	// exactly when it filled. State says the same thing in words, and the exact
	// normalized/threshold/raw numbers are in the footer, which no longer clips.
	headers := []string{"ID", "Attribute", "Kind", "State", "Margin", "Reading"}
	for c, h := range headers {
		v.table.SetCell(0, c, headerCell(h))
	}

	v.shown = v.visibleRows()
	if len(v.shown) == 0 {
		v.table.SetCell(1, 0, tview.NewTableCell(" No attributes match — all healthy ").
			SetTextColor(activeTheme.OK).SetSelectable(false))
		v.footer.SetText("")
		return
	}

	for i, a := range v.shown {
		v.setAttrRow(i+1, a)
	}
}

// setAttrRow fills table row (1-based) with the cells for attribute a. Healthy
// rows render neutral so the table is easy to scan; yellow/red is reserved for
// attributes that need attention.
func (v *attributesView) setAttrRow(row int, a smart.ATAAttribute) {
	color := attrTextColor(a.Severity())
	sel := selectedRowStyle(color)
	put := func(col int, text string, align int) {
		v.table.SetCell(row, col, tview.NewTableCell(" "+text+" ").
			SetTextColor(color).SetAlign(align).SetSelectedStyle(sel))
	}
	// Name and reading are drive-controlled; table cells interpret markup, so
	// escape them (see esc) to keep a hostile drive from injecting tags.
	put(0, fmt.Sprintf("%3d", a.ID), tview.AlignLeft)
	// Capped like fleet.go's model column: one long vendor name must not squeeze
	// the reading off the right edge of a narrow detail pane.
	v.table.SetCell(row, 1, tview.NewTableCell(" "+esc(truncateRunes(humanAttrName(a.Name), attrNameWidth))+" ").
		SetTextColor(color).SetSelectedStyle(sel))
	put(2, attrKind(a), tview.AlignLeft)
	put(3, attrState(a), tview.AlignLeft)
	// Margin carries its own colour tags, so it keeps a green bar on a healthy
	// row rather than inheriting the neutral row colour.
	v.table.SetCell(row, 4, tview.NewTableCell(" "+marginCell(a)+" ").
		SetSelectedStyle(sel))
	put(5, esc(decodeReading(a)), tview.AlignRight)
}

// attrKind names the pre-fail/old-age distinction, taken from the authoritative
// flags bit rather than the attribute name.
func attrKind(a smart.ATAAttribute) string {
	if a.Flags.Prefailure {
		return "pre-fail"
	}
	return "old-age"
}

// attrState renders the attribute's condition as a word. smartctl's when_failed
// enums (FAILING_NOW, in_the_past) were previously printed raw in a column
// narrow enough to truncate them to "FAILING_NO…" and "in_the_pas…" — exactly
// when they finally carried a value, after twenty rows of "-".
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

// attrLimits renders the pair of numbers a state is derived from — the current
// normalized value against its threshold — for the footer. A drive that sets no
// threshold gets the not-reported dash rather than a fabricated 0.
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
	i := row - 1
	if i < 0 || i >= len(v.shown) {
		v.footer.SetText("")
		return
	}
	a := v.shown[i]
	desc := ataDesc[a.ID]
	if desc == "" {
		desc = esc(humanAttrName(a.Name)) // drive-controlled fallback; escape markup
	}
	// Ordered by what is scarce: the numbers, then what the state means, then the
	// description. The box is a fixed height and the description is what wraps,
	// so a narrow terminal runs out of prose rather than out of data or advice.
	// The numbers line used to be SECOND, and a two-line description pushed the
	// id, threshold and raw value out of the box with no scroll cue — leaving the
	// id with no home at all, since the table did not carry it either.
	var b strings.Builder
	fmt.Fprintf(&b, "%s%d · %s[-]  %s%s · now/thr %s · worst %d · raw %s[-]\n",
		accentTag(), a.ID, esc(humanAttrName(a.Name)),
		mutedTag(), attrKind(a), attrLimits(a), a.Worst, esc(a.Raw.String))
	if verdict := attrVerdict(a); verdict != "" {
		fmt.Fprintf(&b, "[%s]%s[-]\n", severityTag(a.Severity()), verdict)
	}
	fmt.Fprintf(&b, "%s", desc)
	v.footer.SetText(b.String())
}

// attrVerdict states what the attribute's condition means for the reader, so the
// footer ends with a consequence rather than leaving one to be inferred from a
// normalized number. Healthy attributes get nothing: silence is the message.
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

// marginCell renders the threshold headroom as a bar, or the not-reported dash
// where the drive sets no threshold. One encoding per column: the bar always
// means the same thing and always fills toward healthy, and an attribute without
// a threshold says so with the same dash the fleet legend already teaches,
// rather than borrowing a dot that also stood in for a bar elsewhere.
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

// decodeReading converts the vendor raw value of well-known attributes into a
// human-readable real-world figure, falling back to the raw string otherwise.
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
	if a.Raw.String == "" {
		return "—"
	}
	return a.Raw.String
}

// attrKV is one row of the NVMe health table.
type attrKV struct {
	k   string
	v   string
	sev smart.Severity
}

// nvmeAttributesView renders the NVMe SMART/health log as a key/value table with
// a description footer, refreshing rows in place so the selection is preserved.
type nvmeAttributesView struct {
	*tview.Flex
	table  *scrollTable
	footer *tview.TextView
	rows   []attrKV
}

func newNVMeAttributesView(h *smart.NVMeHealth) *nvmeAttributesView {
	v := &nvmeAttributesView{
		Flex:   tview.NewFlex().SetDirection(tview.FlexRow),
		table:  newScrollTable(),
		footer: tview.NewTextView().SetDynamicColors(true).SetWrap(true),
	}
	v.table.SetBorders(false).SetFixed(1, 0)
	v.table.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" NVMe health log ")
	v.table.SetSelectable(true, false)
	v.footer.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter)
	v.table.SetSelectionChangedFunc(func(row, _ int) { v.setFooter(row) })

	v.AddItem(v.table, 0, 1, true)
	v.AddItem(v.footer, attrFooterHeight, 0, false)

	v.setRows(h)
	v.table.Select(1, 0)
	v.setFooter(1)
	return v
}

// setFocused accents the table's border when the Attributes tab holds focus.
func (v *nvmeAttributesView) setFocused(focused bool) {
	v.table.SetBorderColor(borderColor(focused))
}

// refresh re-applies the latest health data, keeping the selected row (field
// order is stable, so the row index maps to the same field).
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
		v.table.SetCell(i+1, 0, tview.NewTableCell(" "+r.k+" ").
			SetTextColor(activeTheme.SelectionFg).SetSelectedStyle(selectedRowStyle(activeTheme.SelectionFg)))
		v.table.SetCell(i+1, 1, tview.NewTableCell(" "+r.v+" ").
			SetTextColor(severityColor(r.sev)).SetSelectedStyle(selectedRowStyle(severityColor(r.sev))))
	}
}

// setFooter writes the description for the selected field.
func (v *nvmeAttributesView) setFooter(row int) {
	i := row - 1
	if i < 0 || i >= len(v.rows) {
		v.footer.SetText("")
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
	add("Data read", humanBytes(h.DataUnitsRead*512*1000), smart.SeverityOK)
	add("Data written", humanBytes(h.DataUnitsWritten*512*1000), smart.SeverityOK)
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
		// Grade each sensor on its own reading and the row on the hottest of
		// them. The composite temperature on the Overview can sit comfortably in
		// range while one sensor is well past the failing threshold, so printing
		// these as plain text hid the only place that shows up.
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
	return tview.NewTableCell(" " + s + " ").
		SetTextColor(activeTheme.Accent).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false)
}

// centeredNote is a placeholder primitive for empty/unsupported sections.
func centeredNote(msg string) tview.Primitive {
	tv := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetText("\n" + msg)
	tv.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter)
	return tv
}
