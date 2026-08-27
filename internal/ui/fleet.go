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
// with one row per drive, plus a legend explaining that section's gaps. It is a
// pure renderer over the App's cached reports — the comparison never issues a
// smartctl call of its own.
//
// Only one section is in focus at a time (the "focus metric"), and the rows sort
// by it, so the question being asked — which drive runs hottest, which is most
// worn, which is oldest — is answered by the top of the table.
type fleetView struct {
	*tview.Flex
	bar    *tview.TextView // section strip, same pill idiom as the detail tab bar
	table  *scrollTable
	legend *tview.TextView

	sections []fleetSection // every section, in display order
	shown    []fleetSection // those the current fleet can actually fill
	activeID string         // selected section, kept by id so it survives a rebuild

	sortByName bool // false: sort by the focus metric; true: by device name

	rows     []fleetRow // latest data, in scan order
	ordered  []fleetRow // rows as currently displayed; row i+1 → ordered[i]
	selected string     // device name of the selected row, kept across re-sorts
	renderer bool       // true while rebuilding, to ignore transient selection events

	onOpen func(device string) // Enter: leave the fleet view for this drive's detail
}

// fleetLegendHeight is the legend's fixed height. Two wrapped lines is enough
// for the longest caveat (the attribute-241 estimate note) at a usual width.
const fleetLegendHeight = 2

func newFleetView(onOpen func(device string)) *fleetView {
	v := &fleetView{
		Flex:     tview.NewFlex().SetDirection(tview.FlexRow),
		bar:      tview.NewTextView().SetDynamicColors(true),
		table:    newScrollTable(),
		legend:   tview.NewTextView().SetDynamicColors(true).SetWrap(true),
		sections: fleetSections(),
		onOpen:   onOpen,
	}
	v.activeID = v.sections[0].id

	v.bar.SetBorderPadding(0, 0, uiGutter, uiGutter)
	v.legend.SetBorderPadding(0, 0, uiGutter, uiGutter)
	v.table.SetBorders(false).SetFixed(1, 0)
	v.table.SetSelectable(true, false)
	v.table.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter)

	v.table.SetSelectionChangedFunc(func(row, _ int) {
		if v.renderer {
			return
		}
		if i := row - 1; i >= 0 && i < len(v.ordered) {
			v.selected = v.ordered[i].dev.Name
		}
	})
	// Enter opens the highlighted drive's detail view: the fleet view answers
	// "which drive", and the natural next step is "show me that one".
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

// refresh applies the latest poll. Called on every poll (not only while the
// view is visible) so the comparison is current the moment it is opened, and
// always from the event-loop goroutine like every other UI mutation.
func (v *fleetView) refresh(devices []smart.Device, reports map[string]*smart.Report,
	history map[string][]float64) {
	rows := make([]fleetRow, 0, len(devices))
	for _, d := range devices {
		row := fleetRow{dev: d, rep: reports[d.Name]}
		if row.rep != nil {
			// temperatureSeries prefers the drive's own SCT log and falls back
			// to the runtime ring buffer, exactly as the Overview sparkline does.
			row.series = temperatureSeries(row.rep, history[d.Name])
		}
		rows = append(rows, row)
	}
	v.rows = rows
	v.render()
}

// render rebuilds the section strip, table and legend from the current rows.
// The table primitive itself is preserved so focus survives; selection is
// restored by device name because sorting reorders rows on every poll.
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
	// The legend is our own prose and carries intentional markup (the dash
	// placeholder, the caution-coloured tilde), so it is not escaped — nothing
	// drive-controlled reaches it.
	v.legend.SetText(mutedTag() + sec.legend(v.rows) + "[-]")
	v.restoreSelection()
}

// availableSections filters out sections no drive in this fleet can fill — an
// all-HDD machine has nothing to say about endurance, so it is not offered.
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

// activeIndex is the position of the active section id among the shown
// sections, falling back to the first when it is no longer available.
func (v *fleetView) activeIndex() int {
	for i, s := range v.shown {
		if s.id == v.activeID {
			return i
		}
	}
	return 0
}

// sortRows orders the rows for the section: by its focus metric descending, or
// by device name when the user has toggled name order. Drives that cannot
// report the metric sort last rather than ranking as zero, and ties break on
// device name so the order is stable between polls.
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

	headers := append([]string{"Drive", "Model"}, sec.columns...)
	for c, h := range headers {
		v.table.SetCell(0, c, headerCell(h))
	}

	for i, row := range v.ordered {
		v.setRow(i+1, row, sec, len(sec.columns))
	}
}

// setRow fills one table row: the shared identity cells, then the section's.
// A drive whose first scan has not landed yet still gets a row, so the fleet
// size is honest from the start.
func (v *fleetView) setRow(rowIdx int, row fleetRow, sec fleetSection, n int) {
	var cells []fleetCell
	if row.rep == nil {
		cells = []fleetCell{
			{text: mutedTag() + "●[-] " + esc(shortName(row.dev)), color: activeTheme.Muted},
			{text: "scanning…", color: activeTheme.Muted},
		}
		for range n {
			cells = append(cells, numCell(dash))
		}
	} else {
		// Model and device name are drive-controlled and table cells interpret
		// markup, so escape them (see esc): a hostile drive must not be able to
		// paint a fake verdict into the row beside it.
		model := truncateRunes(row.rep.ModelName, fleetModelWidth)
		if model == "" {
			model = shortName(row.dev)
		}
		cells = append([]fleetCell{
			{text: healthGlyph(row.rep.Overall()) + " " + esc(shortName(row.dev)),
				color: activeTheme.Neutral},
			{text: esc(model), color: activeTheme.Neutral},
		}, sec.cells(row)...)
	}

	// No column expands: the comparison reads best packed left, with the
	// metric columns adjacent rather than pushed to the far edge by a wide
	// identity column.
	for c, cl := range cells {
		v.table.SetCell(rowIdx, c, tview.NewTableCell(" "+cl.text+" ").
			SetTextColor(cl.color).
			SetAlign(cl.align).
			SetSelectedStyle(selectedRowStyle(cl.color)))
	}
}

// restoreSelection re-selects the previously selected drive after a re-sort,
// falling back to the first row. Selection follows the drive, not the row
// index — the metric sort reorders rows on every poll.
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

// renderBar draws the section strip, highlighting the active section. It
// deliberately mirrors detail.renderBar so the fleet view reads as part of the
// same application rather than a separate tool.
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

// stepSection moves the active section by delta, clamped (no wrap). It reports
// whether the section actually changed.
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

// fleetModelWidth bounds the model column so a long model name cannot squeeze
// the comparison columns off a narrow terminal.
const fleetModelWidth = 22

// truncateRunes shortens s to at most n runes, marking the cut with an ellipsis.
// Applied before esc, since truncating already-escaped text could sever a tag.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
