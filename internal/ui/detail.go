// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"

	"github.com/rivo/tview"

	"smartview/internal/smart"
)

// tab identifies a detail sub-view.
type tab struct {
	id    string
	title string
}

// tabView is a detail sub-view that can refresh its data in place, preserving
// interaction state (table selection, scroll offset, sort/filter) across polls.
type tabView interface {
	tview.Primitive
	refresh(r *smart.Report, tempHistory []float64)
}

// staticView adapts a plain primitive to tabView with a no-op refresh.
type staticView struct{ tview.Primitive }

func (staticView) refresh(*smart.Report, []float64) {}

// detail is the right-hand pane: a tab bar above a Pages content area. Which
// tabs exist is recomputed from each report, so drives that omit a section
// (e.g. the Apple NVMe with no logs) simply don't show that tab.
type detail struct {
	*tview.Flex
	bar    *tview.TextView
	pages  *tview.Pages
	tabs   []tab
	active int

	device string             // current drive name, to detect device switches
	views  map[string]tabView // live view per visible tab id
}

func newDetail() *detail {
	d := &detail{
		Flex:  tview.NewFlex().SetDirection(tview.FlexRow),
		bar:   tview.NewTextView().SetDynamicColors(true).SetRegions(true),
		pages: tview.NewPages(),
	}
	d.AddItem(d.bar, 1, 0, false)
	d.AddItem(d.pages, 0, 1, true)
	d.showPlaceholder("Scanning for drives…")
	return d
}

// showPlaceholder displays a message when no drive is selected yet.
func (d *detail) showPlaceholder(msg string) {
	d.tabs = nil
	d.device = ""
	d.views = nil
	d.bar.SetText("")
	d.pages.RemovePage("placeholder")
	d.pages.AddPage("placeholder", centeredNote(msg), true, true)
}

// update applies a fresh report. For the same drive with the same set of tabs it
// refreshes each view's data in place — the table/scroll widgets are not
// recreated, so the selected row, scroll position and sort/filter survive the
// poll. A device switch or a change in which tabs are available triggers a full
// rebuild (which legitimately resets selection: it is a different drive).
func (d *detail) update(r *smart.Report, tempHistory []float64) {
	newTabs := visibleTabs(r)
	if d.device == r.Device.Name && d.device != "" && sameTabIDs(newTabs, d.tabs) {
		for _, t := range newTabs {
			if v := d.views[t.id]; v != nil {
				v.refresh(r, tempHistory)
			}
		}
		return
	}

	prev := d.activeID()
	for _, t := range d.tabs {
		d.pages.RemovePage(t.id)
	}
	d.pages.RemovePage("placeholder")

	d.device = r.Device.Name
	d.tabs = newTabs
	d.views = make(map[string]tabView, len(newTabs))
	for _, t := range d.tabs {
		v := buildTabView(t.id, r, tempHistory)
		d.views[t.id] = v
		d.pages.AddPage(t.id, v, true, false)
	}

	// Preserve the previously focused tab when still available.
	d.active = 0
	for i, t := range d.tabs {
		if t.id == prev {
			d.active = i
			break
		}
	}
	d.selectActive()
}

// sameTabIDs reports whether two tab slices have identical ids in the same order.
func sameTabIDs(a, b []tab) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].id != b[i].id {
			return false
		}
	}
	return true
}

// visibleTabs returns the tabs applicable to the report, in display order.
func visibleTabs(r *smart.Report) []tab {
	tabs := []tab{{"overview", "Overview"}}
	if (r.IsNVMe() && r.NVMeHealth != nil) || r.ATAAttributes != nil {
		tabs = append(tabs, tab{"attributes", "Attributes"})
	}
	if r.HasFARM() {
		tabs = append(tabs, tab{"farm", "FARM"})
	}
	if hasLogs(r) {
		tabs = append(tabs, tab{"logs", "Logs"})
	}
	return tabs
}

// buildTabView constructs the refreshable view for a tab id. The tab set is
// derived from visibleTabs, so the data each view needs is guaranteed present.
func buildTabView(id string, r *smart.Report, tempHistory []float64) tabView {
	switch id {
	case "overview":
		return newOverviewView(r, tempHistory)
	case "attributes":
		if r.IsNVMe() && r.NVMeHealth != nil {
			return newNVMeAttributesView(r.NVMeHealth)
		}
		return newAttributesView(r.ATAAttributes.Table)
	case "farm":
		return newFarmView(r)
	case "logs":
		return newLogsView(r)
	default:
		return staticView{centeredNote("unknown tab")}
	}
}

// activeID returns the id of the active tab, or "" if none.
func (d *detail) activeID() string {
	if d.active >= 0 && d.active < len(d.tabs) {
		return d.tabs[d.active].id
	}
	return ""
}

// selectActive switches the Pages view and repaints the tab bar.
func (d *detail) selectActive() {
	if len(d.tabs) == 0 {
		return
	}
	d.pages.SwitchToPage(d.tabs[d.active].id)
	d.renderBar()
}

// renderBar draws the tab strip with the active tab highlighted.
func (d *detail) renderBar() {
	s := ""
	for i, t := range d.tabs {
		if i == d.active {
			s += fmt.Sprintf(" [black:aqua] %d %s [-:-] ", i+1, t.title)
		} else {
			s += fmt.Sprintf(" [aqua] %d %s [-] ", i+1, t.title)
		}
	}
	d.bar.SetText(s)
}

// stepTab moves the active tab by delta, clamped to the visible tabs (no wrap).
// It reports whether the active tab actually changed, so the caller can fall
// through to the drive list at the left edge.
func (d *detail) stepTab(delta int) bool {
	n := len(d.tabs)
	if n == 0 {
		return false
	}
	next := d.active + delta
	if next < 0 || next >= n {
		return false
	}
	d.active = next
	d.selectActive()
	return true
}

// selectTab activates a tab by zero-based index if it exists.
func (d *detail) selectTab(i int) {
	if i < 0 || i >= len(d.tabs) {
		return
	}
	d.active = i
	d.selectActive()
}

// content returns the currently visible tab primitive, for focus handling.
func (d *detail) content() tview.Primitive {
	if name, prim := d.pages.GetFrontPage(); name != "" {
		return prim
	}
	return d.pages
}
