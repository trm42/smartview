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

// fleetView is the full-screen drive comparison: a section strip over a table
// (one row per drive) plus a legend. A pure renderer over the App's cached
// reports — it never issues a smartctl call. Rows sort by the active
// section's focus metric, so the question asked is answered at the top.
type fleetView struct {
	*tview.Flex
	bar    *inertTextView // section strip, same pill idiom as the detail tab bar
	table  *scrollTable
	legend *inertTextView

	sections []fleetSection // every section, in display order
	shown    []fleetSection // those the current fleet can actually fill
	activeID string         // selected section, kept by id so it survives a rebuild

	// shownCols/dropped: how many of the section's columns fit the width. A
	// narrow terminal drops whole columns and says so in the legend rather
	// than clipping silently.
	shownCols, dropped int
	identityCols       int
	lastWidth          int

	sortByName bool // false: sort by the focus metric; true: by device name

	rows     []fleetRow // latest data, in scan order
	ordered  []fleetRow // rows as currently displayed; row i+1 → ordered[i]
	selected string     // device name of the selected row, kept across re-sorts
	renderer bool       // true while rebuilding, to ignore transient selection events

	onOpen func(device string) // Enter: leave the fleet view for this drive's detail
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
	// Enter opens the highlighted drive's detail view.
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

// Draw re-renders on width change; width is only known here. The budget is the
// table's inner width, which a Flex assigns only inside Flex.Draw, so it is
// measured again afterwards — the first visible frame would otherwise render
// with no width and drop every comparison column.
func (v *fleetView) Draw(screen tcell.Screen) {
	v.syncWidth()
	v.Flex.Draw(screen)
	if v.syncWidth() {
		v.Flex.Draw(screen)
	}
}

// syncWidth re-renders against the table's current inner width, reporting
// whether the width had in fact changed.
func (v *fleetView) syncWidth() bool {
	_, _, w, _ := v.table.GetInnerRect()
	if w == v.lastWidth {
		return false
	}
	v.lastWidth = w
	v.render()
	return true
}

// refresh applies the latest poll. Called on every poll, visible or not, so
// the comparison is current the moment it is opened; event-loop goroutine only.
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

// standbyPrefix marks a spun-down drive in the fleet's identity cell, matching
// the drive list's mark.
func standbyPrefix(row fleetRow) string {
	if !row.asleep {
		return ""
	}
	return standbyGlyph + " "
}

// render rebuilds the strip, table and legend. The table primitive is
// preserved so focus survives; selection is restored by device name because
// sorting reorders rows on every poll.
func (v *fleetView) render() {
	v.renderer = true
	defer func() { v.renderer = false }()

	v.shown = v.availableSections()
	v.renderBar()
	if len(v.shown) == 0 {
		v.table.Clear()
		v.table.SetTitle(" Fleet ")
		v.table.SetCell(0, 0, tview.NewTableCell(" Scanning for drives… ").
			SetTextColor(activeTheme.Muted).SetSelectable(false))
		v.legend.SetText("")
		v.ordered = nil
		return
	}

	sec := v.shown[v.activeIndex()]
	v.ordered = v.sortRows(sec)
	v.renderTable(sec)
	// The legend is our own prose with intentional markup; nothing
	// drive-controlled reaches it, so it is not escaped.
	legend := sec.legend(v.rows)
	if slices.ContainsFunc(v.rows, func(r fleetRow) bool { return r.asleep }) {
		legend = standbyGlyph + " spun down; values as of the last read · " + legend
	}
	if v.dropped > 0 {
		legend = fmt.Sprintf("%s%d more column%s at a wider terminal[-] · %s",
			cautionTag(), v.dropped, map[bool]string{true: "", false: "s"}[v.dropped == 1], legend)
	}
	v.legend.SetText(mutedTag() + legend + "[-]")
	v.restoreSelection()
}

// availableSections filters out sections no drive in this fleet can fill.
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

// activeIndex is the active section's position among the shown sections,
// falling back to the first.
func (v *fleetView) activeIndex() int {
	for i, s := range v.shown {
		if s.id == v.activeID {
			return i
		}
	}
	return 0
}

// sortRows orders rows by the focus metric descending (or device name when
// toggled). Drives that can't report the metric sort last, not as zero; ties
// break on name so the order is stable between polls.
func (v *fleetView) sortRows(sec fleetSection) []fleetRow {
	out := slices.Clone(v.rows)
	if v.sortByName {
		slices.SortStableFunc(out, func(x, y fleetRow) int { return cmp.Compare(x.dev.Name, y.dev.Name) })
		return out
	}
	slices.SortStableFunc(out, func(x, y fleetRow) int {
		a, aok := sec.rank(x)
		b, bok := sec.rank(y)
		switch {
		case aok != bok: // a drive that cannot report the metric sorts last
			if aok {
				return -1
			}
			return 1
		case !aok || a == b:
			return cmp.Compare(x.dev.Name, y.dev.Name)
		default:
			return cmp.Compare(b, a) // focus metric, descending
		}
	})
	return out
}

// renderTable fills the table with the identity columns plus the section's own.
func (v *fleetView) renderTable(sec fleetSection) {
	v.table.Clear()
	sortLabel := strings.ToLower(sec.title)
	if v.sortByName {
		sortLabel = "device"
	}
	drives := "drives"
	if len(v.ordered) == 1 {
		drives = "drive"
	}
	v.table.SetTitle(fmt.Sprintf(" Fleet — %d %s · sorted by %s  %s[s][-] ",
		len(v.ordered), drives, sortLabel, accentTag()))

	// Identity narrows before the comparison does: a cramped terminal spends
	// its width on the metric columns.
	identity := fleetIdentityColumns
	identityW := fleetDeviceWidth + 4 + fleetModelWidth + 3 + fleetSerialWidth + 3
	if v.lastWidth > 0 && v.lastWidth < narrowBreakpoint {
		identity = identity[:1]
		identityW = fleetDeviceWidth + 4
	}
	v.identityCols = len(identity)
	v.shownCols, v.dropped = fittingColumns(sec, v.ordered, identityW, v.lastWidth)
	headers := append(append([]string{}, identity...), sec.columns[:v.shownCols]...)
	// Headers adopt the alignment of the cells below them, read off the first
	// row that has a report.
	var aligns []int
	for _, row := range v.ordered {
		if row.rep != nil {
			for _, cl := range sec.cells(row)[:v.shownCols] {
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
		v.setRow(i+1, row, sec, v.shownCols)
	}
}

// setRow fills one row: identity cells, then the section's. A drive still
// scanning gets a row too, so the fleet size is honest from the start.
func (v *fleetView) setRow(rowIdx int, row fleetRow, sec fleetSection, n int) {
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
		// Model and device name are drive-controlled; esc() blocks markup injection.
		model := truncateRunes(row.rep.ModelName, fleetModelWidth)
		if model == "" {
			model = shortName(row.dev)
		}
		secCells := sec.cells(row)
		if n < len(secCells) {
			secCells = secCells[:n]
		}
		identity := []fleetCell{
			{text: healthGlyph(row.rep.Overall()) + " " + standbyPrefix(row) + esc(fleetDevice(row.dev)),
				color: activeTheme.Neutral},
			{text: esc(model), color: activeTheme.Neutral},
			// Serial disambiguates two drives of the same model.
			{text: esc(truncateRunes(orDash(row.rep.SerialNumber), fleetSerialWidth)),
				color: activeTheme.Muted},
		}
		cells = append(identity[:v.identityCols], secCells...)
	}

	// No column expands: the comparison reads best packed left.
	for c, cl := range cells {
		v.table.SetCell(rowIdx, c, bodyCell(cl.text, cl.color, cl.align))
	}
}

// restoreSelection re-selects by device name, not row index — the metric sort
// reorders rows on every poll.
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

// renderBar draws the section strip, mirroring the detail tab bar's pill idiom
// (tabBar.layout).
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

// stepSection moves the active section by delta, clamped (no wrap), reporting
// whether it changed.
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

// fittingColumns reports how many of a section's columns fit in width, and
// how many are left over — measured from the cells actually rendered, so
// whole columns drop (and are announced) instead of clipping silently.
func fittingColumns(sec fleetSection, rows []fleetRow, identityWidth, width int) (shown, dropped int) {
	n := len(sec.columns)
	if width <= 0 {
		return n, 0
	}
	// Each column is as wide as its widest cell plus padding.
	need := make([]int, n)
	for i, h := range sec.columns {
		need[i] = len(h) + 2
	}
	for _, row := range rows {
		if row.rep == nil {
			continue
		}
		for i, cl := range sec.cells(row) {
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

// fleetModelWidth caps the model column so a long name can't squeeze the
// comparison columns off a narrow terminal.
const fleetModelWidth = 20

// fleetDeviceWidth caps the device column: 11 covers every /dev/... name, and
// an uncapped macOS IOService path would set the column width for the table.
const fleetDeviceWidth = 11

// fleetSerialWidth caps the serial column — long enough to tell two of the
// same model apart.
const fleetSerialWidth = 10

// fleetDevice renders a device name for the comparison table's Drive column.
func fleetDevice(d smart.Device) string {
	return shortDevice(d.Name, fleetDeviceWidth)
}

// truncateRunes shortens s to n runes with an ellipsis. Applied before esc:
// truncating already-escaped text could sever a tag.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
