// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// tab identifies a detail sub-view.
type tab struct {
	id    string
	title string
	// available is false when the drive has no data; with show_unavailable_tabs it is drawn muted.
	available bool
}

// tabView is a detail sub-view that refreshes in place, preserving interaction state.
type tabView interface {
	tview.Primitive
	refresh(r *smart.Report, tempHistory []float64)
}

// staticView adapts a plain primitive to tabView with a no-op refresh.
type staticView struct{ tview.Primitive }

func (staticView) refresh(*smart.Report, []float64) {}

// focusChromer is implemented by tab views that accent their border on focus.
type focusChromer interface {
	setFocused(focused bool)
}

// tabSpan is a pill's column range within the tab bar's inner rect, half-open.
type tabSpan struct{ start, end int }

// inertTextView is a TextView that ignores the mouse: tview's default handler
// focuses the view on a left press, and these views handle no key.
type inertTextView struct{ *tview.TextView }

// newInertTextView builds a mouse-declining TextView with markup enabled.
func newInertTextView() *inertTextView {
	return &inertTextView{tview.NewTextView().SetDynamicColors(true)}
}

// MouseHandler declines every mouse event, leaving focus where it was.
func (v *inertTextView) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
		return false, nil
	}
}

// tabBar is the tab strip; render records each pill's span in the same pass, so it is the only writer of the text.
type tabBar struct {
	*tview.TextView
	tabs      []tab
	active    int
	spans     []tabSpan
	lastWidth int
	onClick   func(i int)
}

func newTabBar() *tabBar {
	// One row, so no wrap.
	b := &tabBar{TextView: tview.NewTextView().SetDynamicColors(true).SetWrap(false)}
	b.SetBorderPadding(0, 0, uiGutter, uiGutter)
	return b
}

// render draws the strip for a tab set and records where each pill landed.
func (b *tabBar) render(tabs []tab, active int) {
	b.tabs = tabs
	b.active = active
	b.layout()
}

// Draw relays out on width change; lastWidth 0 means unconstrained.
func (b *tabBar) Draw(screen tcell.Screen) {
	if _, _, w, _ := b.GetInnerRect(); w != b.lastWidth {
		b.lastWidth = w
		b.layout()
	}
	b.TextView.Draw(screen)
}

// layout emits the pills and the spans together.
func (b *tabBar) layout() {
	var s strings.Builder
	pills := tabPills(b.tabs, b.active, b.lastWidth)
	spans := make([]tabSpan, 0, len(pills))
	col := 0
	for i, pill := range pills {
		switch {
		case i == b.active:
			fmt.Fprintf(&s, " %s%s[-:-:-] ", activeTabTag(), pill)
		case !b.tabs[i].available:
			fmt.Fprintf(&s, " %s%s[-:-:-] ", unavailableTabTag(), pill)
		default:
			fmt.Fprintf(&s, " %s%s[-] ", accentTag(), pill)
		}
		// Spans include separators so no column is dead.
		w := 2 + tview.TaggedStringWidth(pill)
		spans = append(spans, tabSpan{col, col + w})
		col += w
	}
	b.spans = spans
	b.SetText(s.String())
}

// tabPills returns the plain text core of each pill, dropping the titles of
// inactive tabs when the full strip would not fit; width <= 0 is unconstrained.
func tabPills(tabs []tab, active, width int) []string {
	if len(tabs) == 0 {
		return nil
	}
	pills := make([]string, len(tabs))
	total := 0
	for i, t := range tabs {
		pills[i] = fmt.Sprintf(" %d %s ", i+1, t.title)
		total += 2 + tview.TaggedStringWidth(pills[i])
	}
	if width <= 0 || total <= width {
		return pills
	}
	// The number stays on every tab, so the 1-9 keys remain discoverable.
	for i := range pills {
		if i != active {
			pills[i] = fmt.Sprintf(" %d ", i+1)
		}
	}
	return pills
}

// tabAt returns the tab under a screen cell.
func (b *tabBar) tabAt(x, y int) (int, bool) {
	if !b.InInnerRect(x, y) {
		return 0, false
	}
	ix, _, _, _ := b.GetInnerRect()
	for i, s := range b.spans {
		if x-ix >= s.start && x-ix < s.end {
			return i, true
		}
	}
	return 0, false
}

// MouseHandler activates the clicked tab and drops setFocus: the bar handles no key.
func (b *tabBar) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return b.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, _ func(tview.Primitive)) (bool, tview.Primitive) {
		switch action {
		// A second click within DoubleClickInterval arrives as a double click.
		case tview.MouseLeftClick, tview.MouseLeftDoubleClick:
		default:
			return false, nil
		}
		i, ok := b.tabAt(event.Position())
		if !ok {
			return false, nil
		}
		if b.onClick != nil {
			b.onClick(i)
		}
		return true, nil
	})
}

// detail is the right-hand pane: a tab bar above a Pages content area, with
// the tab set recomputed from each report.
type detail struct {
	*tview.Flex
	bar     *tabBar
	barRow  *tview.Flex
	spinner *inertTextView
	// note is a one-row caveat strip, height 0 when empty.
	note   *inertTextView
	pages  *tview.Pages
	tabs   []tab
	active int

	showAllTabs bool

	device string             // current drive name, to detect device switches
	views  map[string]tabView // live view per visible tab id
	// placeholder is the last message shown, re-shown on repaint.
	placeholder string

	selfTest selfTestActions
}

func newDetail() *detail {
	d := &detail{
		Flex:    tview.NewFlex().SetDirection(tview.FlexRow),
		bar:     newTabBar(),
		spinner: newInertTextView(),
		note:    newInertTextView(),
		pages:   tview.NewPages(),
	}
	d.spinner.SetTextAlign(tview.AlignRight)
	d.barRow = tview.NewFlex().
		AddItem(d.bar, 0, 1, false).
		AddItem(d.spinner, 2, 0, false)
	d.note.SetBorderPadding(0, 0, uiGutter, uiGutter)
	d.AddItem(d.barRow, 1, 0, false)
	d.AddItem(d.note, 0, 0, false)
	d.AddItem(d.pages, 0, 1, true)
	d.showPlaceholder("Scanning for drives…")
	return d
}

// setNote shows a one-line caveat above the tab body, or hides the row when s is empty.
func (d *detail) setNote(s string) {
	d.note.SetText(s)
	height := 0
	if s != "" {
		height = 1
	}
	d.ResizeItem(d.note, height, 0)
}

// showPlaceholder displays a message when no drive is selected yet.
func (d *detail) showPlaceholder(msg string) {
	d.placeholder = msg
	d.tabs = nil
	d.device = ""
	d.views = nil
	d.bar.render(nil, 0)
	d.pages.RemovePage("placeholder")
	d.pages.AddPage("placeholder", centeredNote(msg), true, true)
}

// update applies a fresh report, in place for the same drive and tab set, else by rebuilding.
func (d *detail) update(r *smart.Report, tempHistory []float64) {
	newTabs := visibleTabs(r, d.showAllTabs)
	if d.device == r.Device.Name && d.device != "" && sameTabs(newTabs, d.tabs) {
		for _, t := range newTabs {
			if v := d.views[t.id]; v != nil && t.available {
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
		v := d.buildTabView(t, r, tempHistory)
		d.views[t.id] = v
		d.pages.AddPage(t.id, v, true, false)
	}

	// Keep the previous tab when still available; index 0 always is.
	d.active = 0
	for i, t := range d.tabs {
		if t.id == prev && t.available {
			d.active = i
			break
		}
	}
	d.selectActive()
}

// sameTabs compares availability too: with show_unavailable_tabs the ids never change.
func sameTabs(a, b []tab) bool { return slices.Equal(a, b) }

// allTabs is the full strip in display order, each with its data-presence predicate.
var allTabs = []struct {
	id, title string
	available func(*smart.Report) bool
}{
	{"overview", "Overview", func(*smart.Report) bool { return true }},
	{"attributes", "Attributes", hasAttributes},
	{"statistics", "Statistics", (*smart.Report).HasDeviceStats},
	{"farm", "FARM", (*smart.Report).HasFARM},
	{"tests", "Tests", (*smart.Report).SupportsSelfTest},
	{"logs", "Logs", hasLogs},
}

// hasAttributes reports whether either protocol's attribute view has data.
func hasAttributes(r *smart.Report) bool {
	return (r.IsNVMe() && r.NVMeHealth != nil) || len(ataAttributes(r)) > 0
}

// ataAttributes returns the ATA attribute rows, nil when the section is absent or empty.
func ataAttributes(r *smart.Report) []smart.ATAAttribute {
	if r.ATAAttributes == nil {
		return nil
	}
	return r.ATAAttributes.Table
}

// visibleTabs returns the tabs to draw; showAll keeps unavailable ones, marked.
func visibleTabs(r *smart.Report, showAll bool) []tab {
	tabs := make([]tab, 0, len(allTabs))
	for _, t := range allTabs {
		ok := t.available(r)
		if !ok && !showAll {
			continue
		}
		tabs = append(tabs, tab{id: t.id, title: t.title, available: ok})
	}
	return tabs
}

// buildTabView constructs a tab's view; an unavailable tab gets a note page.
func (d *detail) buildTabView(t tab, r *smart.Report, tempHistory []float64) tabView {
	if !t.available {
		return staticView{centeredNote(t.title + " — not reported by this drive")}
	}
	switch t.id {
	case "overview":
		return newOverviewView(r, tempHistory)
	case "attributes":
		if r.IsNVMe() && r.NVMeHealth != nil {
			return newNVMeAttributesView(r.NVMeHealth)
		}
		return newAttributesView(ataAttributes(r))
	case "statistics":
		return newStatisticsView(r)
	case "farm":
		return newFarmView(r)
	case "tests":
		return newTestsView(r, d.selfTest)
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
	d.bar.render(d.tabs, d.active)
}

// stepTab moves to the next available tab in direction delta without wrapping, reporting whether it moved.
func (d *detail) stepTab(delta int) bool {
	for i := d.active + delta; i >= 0 && i < len(d.tabs); i += delta {
		if !d.tabs[i].available {
			continue
		}
		d.active = i
		d.selectActive()
		return true
	}
	return false
}

// selectTab activates the tab at a displayed position unless it is unavailable.
func (d *detail) selectTab(i int) bool {
	if i < 0 || i >= len(d.tabs) || !d.tabs[i].available {
		return false
	}
	d.active = i
	d.selectActive()
	return true
}

// selectTabID activates the tab with the given id, reporting whether it was found.
func (d *detail) selectTabID(id string) bool {
	for i, t := range d.tabs {
		if t.id == id {
			return d.selectTab(i)
		}
	}
	return false
}

// activeView returns the active tab's live view, or nil under a placeholder.
func (d *detail) activeView() tabView {
	return d.views[d.activeID()]
}

// content returns the currently visible tab primitive, for focus handling.
func (d *detail) content() tview.Primitive {
	if name, prim := d.pages.GetFrontPage(); name != "" {
		return prim
	}
	return d.pages
}

// setContentFocus accents or dims the active tab body's border.
func (d *detail) setContentFocus(focused bool) {
	if f, ok := d.content().(focusChromer); ok {
		f.setFocused(focused)
	}
}

// tabCount is the number of visible tabs, used to size the "1-N tab" hint.
func (d *detail) tabCount() int { return len(d.tabs) }

// testsRunning reports whether the Tests tab shows a running self-test.
func (d *detail) testsRunning() bool {
	if v, ok := d.views["tests"].(*testsView); ok {
		return v.mode == modeRunning
	}
	return false
}
