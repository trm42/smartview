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

// detail is the right-hand pane: a tab bar above a Pages content area. Which
// tabs exist is recomputed from each report, so drives that omit a section
// (e.g. the Apple NVMe with no logs) simply don't show that tab.
type detail struct {
	*tview.Flex
	bar    *tview.TextView
	pages  *tview.Pages
	tabs   []tab
	active int
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
	d.bar.SetText("")
	d.pages.RemovePage("placeholder")
	d.pages.AddPage("placeholder", centeredNote(msg), true, true)
}

// update rebuilds the tabs and their content for the given report.
func (d *detail) update(r *smart.Report, tempHistory []float64) {
	prev := d.activeID()

	// Tear down previous pages.
	for _, t := range d.tabs {
		d.pages.RemovePage(t.id)
	}
	d.pages.RemovePage("placeholder")

	d.tabs = visibleTabs(r)
	for _, t := range d.tabs {
		d.pages.AddPage(t.id, buildTabContent(t.id, r, tempHistory), true, false)
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

// buildTabContent constructs the primitive for a tab id.
func buildTabContent(id string, r *smart.Report, tempHistory []float64) tview.Primitive {
	switch id {
	case "overview":
		return buildOverview(r, tempHistory)
	case "attributes":
		return buildAttributes(r)
	case "farm":
		return buildFarm(r)
	case "logs":
		return buildLogs(r)
	default:
		return centeredNote("unknown tab")
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
