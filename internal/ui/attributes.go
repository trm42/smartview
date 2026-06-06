// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"sort"
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
	v.AddItem(v.footer, 4, 0, false)
	v.renderRows()
	v.selectByID(-1)
	return v
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
		" SMART attributes — sort: %s · filter: %s  [aqua][s/f][-] ", v.sortBy, v.filter))

	// No ID column: the numeric ID is shown in the footer for the selected row.
	headers := []string{"Attribute", "Kind", "Health", "Reading", "When"}
	for c, h := range headers {
		v.table.SetCell(0, c, headerCell(h))
	}

	v.shown = v.visibleRows()
	if len(v.shown) == 0 {
		v.table.SetCell(1, 0, tview.NewTableCell(" No attributes match — all healthy ").
			SetTextColor(tcell.ColorGreen).SetSelectable(false))
		v.footer.SetText("")
		return
	}

	for i, a := range v.shown {
		// Healthy rows render neutral so the table is easy to scan; yellow/red
		// is reserved for attributes that need attention.
		color := attrTextColor(a.Severity())
		kind := "old-age"
		if a.Flags.Prefailure {
			kind = "pre-fail"
		}
		when := a.WhenFailed
		if when == "" {
			when = "-"
		}
		sel := selectedRowStyle(color)
		v.table.SetCell(i+1, 0, tview.NewTableCell(" "+humanAttrName(a.Name)+" ").
			SetTextColor(color).SetExpansion(1).SetSelectedStyle(sel))
		v.table.SetCell(i+1, 1, tview.NewTableCell(" "+kind+" ").
			SetTextColor(color).SetSelectedStyle(sel))
		// Health uses inline colour tags (keeps a green bar on healthy rows).
		v.table.SetCell(i+1, 2, tview.NewTableCell(" "+healthCell(a)+" ").
			SetSelectedStyle(sel))
		v.table.SetCell(i+1, 3, tview.NewTableCell(" "+decodeReading(a)+" ").
			SetTextColor(color).SetAlign(tview.AlignRight).SetSelectedStyle(sel))
		v.table.SetCell(i+1, 4, tview.NewTableCell(" "+when+" ").
			SetTextColor(color).SetSelectedStyle(sel))
	}
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
	sort.SliceStable(out, func(i, j int) bool {
		switch v.sortBy {
		case sortID:
			return out[i].ID < out[j].ID
		case sortMargin:
			return attrMargin(out[i]) < attrMargin(out[j])
		default: // severity: worst first, then by ID
			if si, sj := out[i].Severity(), out[j].Severity(); si != sj {
				return si > sj
			}
			return out[i].ID < out[j].ID
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
	kind := "old-age"
	if a.Flags.Prefailure {
		kind = "pre-fail"
	}
	desc := ataDesc[a.ID]
	if desc == "" {
		desc = humanAttrName(a.Name)
	}
	v.footer.SetText(fmt.Sprintf("%s\n[gray](id %d, %s)  norm %d/%d · thresh %d · raw %s[-]",
		desc, a.ID, kind, a.Value, a.Worst, a.Thresh, a.Raw.String))
}

// attrMargin is the threshold headroom; attributes without a threshold sort last.
func attrMargin(a smart.ATAAttribute) int {
	if a.Thresh <= 0 {
		return 1 << 30
	}
	return a.Value - a.Thresh
}

// healthCell renders the per-attribute health indicator: a headroom bar for
// attributes with a real threshold, or a status dot for those without one.
func healthCell(a smart.ATAAttribute) string {
	sev := a.Severity()
	if a.Thresh > 0 {
		return marginBar(a.Value, a.Worst, a.Thresh, sev)
	}
	return fmt.Sprintf("[%s]●[-]", severityTag(sev))
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
		if n, ok := leadingInt(a.Raw.String); ok {
			return humanDuration(int(n))
		}
	case 241, 242: // total LBAs written / read (512-byte LBAs)
		if n, ok := leadingInt(a.Raw.String); ok {
			return humanBytes(n * 512)
		}
	case 190, 194: // airflow / drive temperature
		if n, ok := leadingInt(a.Raw.String); ok {
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
	v.AddItem(v.footer, 4, 0, false)

	v.setRows(h)
	v.table.Select(1, 0)
	v.setFooter(1)
	return v
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
			SetTextColor(tcell.ColorWhite).SetSelectedStyle(selectedRowStyle(tcell.ColorWhite)))
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
	v.footer.SetText(fmt.Sprintf("%s\n[gray]%s: %s[-]", desc, v.rows[i].k, v.rows[i].v))
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
		parts := make([]string, len(h.TemperatureSensors))
		for i, t := range h.TemperatureSensors {
			parts[i] = fmt.Sprintf("%d°C", t)
		}
		add("Sensors", strings.Join(parts, ", "), smart.SeverityOK)
	}
	return rows
}

// headerCell builds a non-selectable bold header cell.
func headerCell(s string) *tview.TableCell {
	return tview.NewTableCell(" " + s + " ").
		SetTextColor(tcell.ColorAqua).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false)
}

// centeredNote is a placeholder primitive for empty/unsupported sections.
func centeredNote(msg string) tview.Primitive {
	tv := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetText("\n" + msg)
	tv.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter)
	return tv
}
