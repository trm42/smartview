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

// fleetView is the full-screen drive comparison: a section strip, a table sorted by the section's focus metric, and a legend.
type fleetView struct {
	*tview.Flex
	bar    *inertTextView
	table  *scrollTable
	legend *inertTextView

	sections []fleetSection // every section, in display order
	shown    []fleetSection // those the current fleet can actually fill
	activeID string         // selected section, kept by id so it survives a rebuild

	// shownCols/dropped: section columns that fit; dropped ones are announced in the legend.
	shownCols, dropped int
	identityCols       int
	lastWidth          int

	sortByName bool // false: sort by the focus metric; true: by device name

	rows     []fleetRow // latest data, in scan order
	ordered  []fleetRow // rows as currently displayed; row i+1 → ordered[i]
	selected string     // device name of the selected row, kept across re-sorts
	renderer bool       // true while rebuilding, to ignore transient selection events

	onOpen func(device string)
}

// fleetLegendHeight fits the longest caveat wrapped to two lines.
const fleetLegendHeight = 2

func newFleetView(onOpen func(device string)) *fleetView {
	v := &fleetView{
		Flex:     tview.NewFlex().SetDirection(tview.FlexRow),
		bar:      newInertTextView(),
		table:    newScrollTable(),
		legend:   newInertTextView(),
		sections: fleetSections(),
		onOpen:   onOpen,
	}
	v.activeID = v.sections[0].id

	v.legend.SetWrap(true)
	v.bar.SetBorderPadding(0, 0, uiGutter, uiGutter)
	v.legend.SetBorderPadding(0, 0, uiGutter, uiGutter)
	v.table.SetBorders(false).SetFixed(1, 0)
	v.table.SetSelectable(true, false)
	titledBox(v.table.Box, "")

	v.table.SetSelectionChangedFunc(func(row, _ int) {
		if v.renderer {
			return
		}
		if i := row - 1; i >= 0 && i < len(v.ordered) {
			v.selected = v.ordered[i].dev.Name
		}
	})
	v.table.SetSelectedFunc(func(row, _ int) {
		if i := row - 1; i >= 0 && i < len(v.ordered) && v.onOpen != nil {
			v.onOpen(v.ordered[i].dev.Name)
		}
	})
	v.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Rune() == 's' {
			v.sortByName = !v.sortByName
			v.render()
			return nil
		}
		return ev
	})

	v.AddItem(v.bar, 1, 0, false)
	v.AddItem(v.table, 0, 1, true)
	v.AddItem(v.legend, fleetLegendHeight, 0, false)
	v.render()
	return v
}

// setFocused accents the table's border when the fleet view holds focus.
func (v *fleetView) setFocused(focused bool) {
	v.table.SetBorderColor(borderColor(focused))
}

// Draw re-renders on width change, measuring again after Flex.Draw because Flex assigns the table's rect there.
func (v *fleetView) Draw(screen tcell.Screen) {
	v.syncWidth()
	v.Flex.Draw(screen)
	if v.syncWidth() {
		v.Flex.Draw(screen)
	}
}

// syncWidth re-renders against the table's inner width, reporting whether it changed.
func (v *fleetView) syncWidth() bool {
	_, _, w, _ := v.table.GetInnerRect()
	if w == v.lastWidth {
		return false
	}
	v.lastWidth = w
	v.render()
	return true
}

// refresh applies the latest poll; called even when hidden so the view is current on open.
func (v *fleetView) refresh(devices []smart.Device, reports map[string]*smart.Report,
	history map[string][]float64, asleep map[string]bool) {
	rows := make([]fleetRow, 0, len(devices))
	for _, d := range devices {
		row := fleetRow{dev: d, rep: reports[d.Name], asleep: asleep[d.Name]}
		if row.rep != nil {
			row.series = temperatureSeries(row.rep, history[d.Name])
		}
		rows = append(rows, row)
	}
	v.rows = rows
	v.render()
}

// noDrivesText is the empty-scan message the detail pane and the fleet share.
const noDrivesText = "No drives found. Try running with sudo."

// standbyPrefix marks a spun-down drive in the fleet's identity cell.
func standbyPrefix(row fleetRow) string {
	if !row.asleep {
		return ""
	}
	return standbyGlyph + " "
}

// render rebuilds strip, table and legend, restoring selection by device name.
func (v *fleetView) render() {
	v.renderer = true
	defer func() { v.renderer = false }()

	v.shown = v.availableSections()
	v.renderBar()
	if len(v.shown) == 0 {
		v.table.Clear()
		v.table.SetTitle(" Fleet ")
		v.table.SetCell(0, 0, tview.NewTableCell(" "+noDrivesText+" ").
			SetTextColor(activeTheme.Muted).SetSelectable(false))
		v.legend.SetText("")
		v.ordered = nil
		return
	}

	sec := v.shown[v.activeIndex()]
	v.ordered = v.sortRows(sec)
	v.renderTable(sec)
	legend := fleetLegend(v.rows, sec)
	if v.dropped > 0 {
		legend = fmt.Sprintf("%s%s at a wider terminal[-] · %s",
			cautionTag(), plural(v.dropped, "more column", "more columns"), legend)
	}
	v.legend.SetText(mutedTag() + legend + "[-]")
	v.restoreSelection()
}

// fleetLegend prefixes the section's legend with what a spun-down or unread row means; a fleet with no report at all omits the section's own caveats.
func fleetLegend(rows []fleetRow, sec fleetSection) string {
	var parts []string
	switch {
	case slices.ContainsFunc(rows, func(r fleetRow) bool { return r.asleep && r.rep != nil }):
		parts = append(parts, standbyGlyph+" spun down; values as of the last read")
	case slices.ContainsFunc(rows, func(r fleetRow) bool { return r.asleep }):
		parts = append(parts, standbyGlyph+" spun down")
	}
	if slices.ContainsFunc(rows, func(r fleetRow) bool { return r.rep == nil }) {
		parts = append(parts, "a row of "+dash+" has not been read yet")
	}
	if slices.ContainsFunc(rows, func(r fleetRow) bool { return r.rep != nil }) {
		parts = append(parts, sec.legend(rows))
	}
	return strings.Join(parts, " · ")
}

// availableSections filters out sections no drive in this fleet can fill; empty only when there are no rows.
func (v *fleetView) availableSections() []fleetSection {
	if len(v.rows) == 0 {
		return nil
	}
	out := make([]fleetSection, 0, len(v.sections))
	for _, s := range v.sections {
		if s.available(v.rows) {
			out = append(out, s)
		}
	}
	return out
}

// activeIndex is the active section's position among the shown ones, else 0.
func (v *fleetView) activeIndex() int {
	for i, s := range v.shown {
		if s.id == v.activeID {
			return i
		}
	}
	return 0
}

// sortRows orders rows by the focus metric descending, or by device name when
// toggled. Drives that can't report the metric sort last; ties break on name.
func (v *fleetView) sortRows(sec fleetSection) []fleetRow {
	out := slices.Clone(v.rows)
	if v.sortByName {
		slices.SortStableFunc(out, func(x, y fleetRow) int { return cmp.Compare(x.dev.Name, y.dev.Name) })
		return out
	}
	type ranked struct {
		row fleetRow
		v   float64
		ok  bool
	}
	rs := make([]ranked, len(out))
	for i, r := range out {
		val, ok := sec.rank(r)
		rs[i] = ranked{r, val, ok}
	}
	slices.SortStableFunc(rs, func(x, y ranked) int {
		switch {
		case x.ok != y.ok:
			if x.ok {
				return -1
			}
			return 1
		case !x.ok || x.v == y.v:
			return cmp.Compare(x.row.dev.Name, y.row.dev.Name)
		default:
			return cmp.Compare(y.v, x.v)
		}
	})
	for i, r := range rs {
		out[i] = r.row
	}
	return out
}

// renderTable fills the table with the identity columns plus the section's own.
func (v *fleetView) renderTable(sec fleetSection) {
	v.table.Clear()
	sortLabel := strings.ToLower(sec.title)
	if v.sortByName {
		sortLabel = "device"
	}
	v.table.SetTitle(fmt.Sprintf(" Fleet — %s · sorted by %s  %s[s][-] ",
		plural(len(v.ordered), "drive", "drives"), sortLabel, accentTag()))

	// Identity narrows first: a cramped terminal spends its width on the metrics.
	identity := fleetIdentityColumns
	identityW := fleetDeviceWidth + 4 + fleetModelWidth + 3 + fleetSerialWidth + 3
	if v.lastWidth > 0 && v.lastWidth < narrowBreakpoint {
		identity = identity[:1]
		identityW = fleetDeviceWidth + 4
	}
	v.identityCols = len(identity)
	cells := make([][]fleetCell, len(v.ordered))
	for i, row := range v.ordered {
		if row.rep != nil {
			cells[i] = sec.cells(row)
		}
	}
	v.shownCols, v.dropped = fittingColumns(sec.columns, cells, identityW, v.lastWidth)
	headers := append(append([]string{}, identity...), sec.columns[:v.shownCols]...)
	// Headers adopt the alignment of the first reporting row's cells.
	var aligns []int
	for i, row := range v.ordered {
		if row.rep != nil {
			for _, cl := range cells[i][:v.shownCols] {
				aligns = append(aligns, cl.align)
			}
			break
		}
	}
	for c, h := range headers {
		align := tview.AlignLeft
		if i := c - v.identityCols; i >= 0 && i < len(aligns) {
			align = aligns[i]
		}
		v.table.SetCell(0, c, headerCellAligned(h, align))
	}

	for i, row := range v.ordered {
		v.setRow(i+1, row, cells[i], v.shownCols)
	}
}

// setRow fills one row: identity cells, then the section's; a drive still scanning gets a row too.
func (v *fleetView) setRow(rowIdx int, row fleetRow, secCells []fleetCell, n int) {
	var cells []fleetCell
	if row.rep == nil {
		waiting := "scanning…"
		if row.asleep {
			waiting = "asleep"
		}
		cells = []fleetCell{
			{text: mutedTag() + "●[-] " + standbyPrefix(row) + esc(fleetDevice(row.dev)),
				color: activeTheme.Muted},
			{text: waiting, color: activeTheme.Muted},
			{text: dash, color: activeTheme.Muted},
		}[:v.identityCols]
		for range n {
			cells = append(cells, numCell(dash))
		}
	} else {
		model := truncateRunes(row.rep.ModelName, fleetModelWidth)
		if model == "" {
			model = shortName(row.dev)
		}
		if n < len(secCells) {
			secCells = secCells[:n]
		}
		identity := []fleetCell{
			{text: reportGlyph(row.rep) + " " + standbyPrefix(row) + esc(fleetDevice(row.dev)),
				color: activeTheme.Neutral},
			{text: esc(model), color: activeTheme.Neutral},
			{text: orDash(esc(truncateRunes(row.rep.SerialNumber, fleetSerialWidth))),
				color: activeTheme.Muted},
		}
		cells = append(identity[:v.identityCols], secCells...)
	}

	for c, cl := range cells {
		v.table.SetCell(rowIdx, c, bodyCell(cl.text, cl.color, cl.align))
	}
}

// restoreSelection re-selects by device name, since the metric sort reorders rows.
func (v *fleetView) restoreSelection() {
	if len(v.ordered) == 0 {
		return
	}
	target := 1
	for i, row := range v.ordered {
		if row.dev.Name == v.selected {
			target = i + 1
			break
		}
	}
	v.table.Select(target, 0)
	v.selected = v.ordered[target-1].dev.Name
}

// renderBar draws the section strip in the detail tab bar's pill idiom.
func (v *fleetView) renderBar() {
	active := v.activeIndex()
	s := ""
	for i, sec := range v.shown {
		if i == active {
			s += fmt.Sprintf(" %s %d %s [-:-:-] ", activeTabTag(), i+1, sec.title)
		} else {
			s += fmt.Sprintf(" %s %d %s [-] ", accentTag(), i+1, sec.title)
		}
	}
	v.bar.SetText(s)
}

// selectSection activates a section by zero-based index if it exists.
func (v *fleetView) selectSection(i int) {
	if i < 0 || i >= len(v.shown) {
		return
	}
	v.activeID = v.shown[i].id
	v.render()
}

// stepSection moves the active section by delta without wrapping, reporting whether it moved.
func (v *fleetView) stepSection(delta int) bool {
	next := v.activeIndex() + delta
	if next < 0 || next >= len(v.shown) {
		return false
	}
	v.activeID = v.shown[next].id
	v.render()
	return true
}

// sectionCount is the number of selectable sections, for the "1-N section" hint.
func (v *fleetView) sectionCount() int { return len(v.shown) }

// fittingColumns reports how many whole columns fit in width, measured from the rendered cells.
func fittingColumns(columns []string, cells [][]fleetCell, identityWidth, width int) (shown, dropped int) {
	n := len(columns)
	if width <= 0 {
		return n, 0
	}
	need := make([]int, n)
	for i, h := range columns {
		need[i] = len(h) + 2
	}
	for _, row := range cells {
		for i, cl := range row {
			if i < n {
				need[i] = max(need[i], tview.TaggedStringWidth(cl.text)+2)
			}
		}
	}
	avail := width - identityWidth
	for i, w := range need {
		if avail-w < 0 {
			return i, n - i
		}
		avail -= w
	}
	return n, 0
}

// fleetIdentityColumns are the columns every section carries.
var fleetIdentityColumns = []string{"Drive", "Model", "Serial"}

// fleetModelWidth caps the model column so a long name can't squeeze out the comparison.
const fleetModelWidth = 20

// fleetDeviceWidth covers every /dev/... name; an IOService path is truncated.
const fleetDeviceWidth = 11

// fleetSerialWidth is long enough to tell two of the same model apart.
const fleetSerialWidth = 10

// fleetDevice renders a device name for the comparison table's Drive column.
func fleetDevice(d smart.Device) string {
	return shortDevice(d.Name, fleetDeviceWidth)
}

// truncateRunes shortens s to n runes with an ellipsis; apply before esc, or a tag could be severed.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
