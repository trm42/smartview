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
}

// tabView is a detail sub-view that refreshes in place, preserving
// interaction state across polls.
type tabView interface {
	tview.Primitive
	refresh(r *smart.Report, tempHistory []float64)
}

// staticView adapts a plain primitive to tabView with a no-op refresh.
type staticView struct{ tview.Primitive }

func (staticView) refresh(*smart.Report, []float64) {}

// focusChromer is implemented by tab views that signal keyboard focus by
// accenting their border.
type focusChromer interface {
	setFocused(focused bool)
}

// tabSpan is a pill's column range within the tab bar's inner rect, half-open.
type tabSpan struct{ start, end int }

// inertTextView is a TextView that ignores the mouse: tview's default handler
// focuses the view on a left press, and these views handle no key.
type inertTextView struct{ *tview.TextView }

// newInertTextView builds a mouse-declining TextView with markup enabled —
// the shape every piece of keyless chrome wants. Wrap a new keyless widget
// this way or a click on it strands focus where no key is handled.
func newInertTextView() *inertTextView {
	return &inertTextView{tview.NewTextView().SetDynamicColors(true)}
}

// MouseHandler declines every mouse event, leaving focus where it was.
func (v *inertTextView) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
		return false, nil
	}
}

// tabBar is the detail's tab strip: it emits the pills and records their column
// spans in the same pass, so a click maps to a tab without a second model of the
// layout. render is the only writer of the text — a bare SetText would leave the
// spans describing a strip that is no longer drawn.
type tabBar struct {
	*tview.TextView
	tabs      []tab
	active    int
	spans     []tabSpan
	lastWidth int
	onClick   func(i int)
}

func newTabBar() *tabBar {
	// Wrapping is off because the strip is one row: a wrapped pill would be
	// pushed onto a line the box never shows.
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

// Draw relays out when the width changed, the pattern the other width-aware
// panels use; lastWidth is 0 before the first draw, which tabPills reads as
// unconstrained.
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
		if i == b.active {
			// activeTabTag falls back to black-on-white so the pill survives mono.
			fmt.Fprintf(&s, " %s%s[-:-:-] ", activeTabTag(), pill)
		} else {
			fmt.Fprintf(&s, " %s%s[-] ", accentTag(), pill)
		}
		// The span covers the separator spaces too, so pills are contiguous and
		// no column between them is dead.
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

// MouseHandler activates the tab a click lands on. The setFocus closure is
// discarded: focusing the bar would strand every key, since it handles none and
// tview routes keys only to the focused primitive.
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

// detail is the right-hand pane: a tab bar above a Pages content area. The
// tab set is recomputed from each report, so absent sections show no tab.
type detail struct {
	*tview.Flex
	bar *tabBar
	// barRow holds bar and spinner; it is built once, so repaintAll has to be
	// able to reach it.
	barRow  *tview.Flex
	spinner *inertTextView
	pages   *tview.Pages
	tabs    []tab
	active  int

	device string             // current drive name, to detect device switches
	views  map[string]tabView // live view per visible tab id
	// placeholder is the last message showPlaceholder rendered, so a theme
	// repaint can re-show it instead of guessing one.
	placeholder string

	selfTest selfTestActions // callbacks for the interactive Tests tab
	// onTabClick receives the tab a click landed on; the bar forwards the
	// intent and the App owns the focus move and the chrome resync.
	onTabClick func(i int)
}

func newDetail() *detail {
	d := &detail{
		Flex:    tview.NewFlex().SetDirection(tview.FlexRow),
		bar:     newTabBar(),
		spinner: newInertTextView(),
		pages:   tview.NewPages(),
	}
	d.spinner.SetTextAlign(tview.AlignRight)
	// The indirection keeps the wiring valid: build() assigns onTabClick later.
	d.bar.onClick = func(i int) {
		if d.onTabClick != nil {
			d.onTabClick(i)
		}
	}
	// Tab strip and refresh spinner share one row; the spinner gets a fixed
	// 2-col cell flush right.
	d.barRow = tview.NewFlex().
		AddItem(d.bar, 0, 1, false).
		AddItem(d.spinner, 2, 0, false)
	d.AddItem(d.barRow, 1, 0, false)
	d.AddItem(d.pages, 0, 1, true)
	d.showPlaceholder("Scanning for drives…")
	return d
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

// update applies a fresh report. Same drive + same tab set refreshes each
// view in place so selection/scroll/sort survive the poll; a device or
// tab-set change triggers a full rebuild.
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
		v := d.buildTabView(t.id, r, tempHistory)
		d.views[t.id] = v
		d.pages.AddPage(t.id, v, true, false)
	}

	// Keep the previously focused tab when still available.
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
	return slices.EqualFunc(a, b, func(x, y tab) bool { return x.id == y.id })
}

// visibleTabs returns the tabs applicable to the report, in display order.
func visibleTabs(r *smart.Report) []tab {
	tabs := []tab{{id: "overview", title: "Overview"}}
	if (r.IsNVMe() && r.NVMeHealth != nil) || r.ATAAttributes != nil {
		tabs = append(tabs, tab{id: "attributes", title: "Attributes"})
	}
	if r.HasDeviceStats() {
		tabs = append(tabs, tab{id: "statistics", title: "Statistics"})
	}
	if r.HasFARM() {
		tabs = append(tabs, tab{id: "farm", title: "FARM"})
	}
	if r.SupportsSelfTest() {
		tabs = append(tabs, tab{id: "tests", title: "Tests"})
	}
	if hasLogs(r) {
		tabs = append(tabs, tab{id: "logs", title: "Logs"})
	}
	return tabs
}

// buildTabView constructs the view for a tab id; visibleTabs guarantees the
// data each view needs is present.
func (d *detail) buildTabView(id string, r *smart.Report, tempHistory []float64) tabView {
	switch id {
	case "overview":
		return newOverviewView(r, tempHistory)
	case "attributes":
		if r.IsNVMe() && r.NVMeHealth != nil {
			return newNVMeAttributesView(r.NVMeHealth)
		}
		return newAttributesView(r.ATAAttributes.Table)
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

// stepTab moves the active tab by delta, clamped (no wrap), reporting whether
// it changed so the caller can fall through to the drive list at the edge.
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

// selectTabID activates the tab with the given id, reporting whether it was found.
func (d *detail) selectTabID(id string) bool {
	for i, t := range d.tabs {
		if t.id == id {
			d.active = i
			d.selectActive()
			return true
		}
	}
	return false
}

// activeView returns the live view backing the active tab, or nil when none is
// built (a placeholder is showing). Callers type-assert it to a concrete view
// to ask a question only that view can answer — the tabView interface stays at
// refresh alone, so a view that has nothing to say implements nothing.
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

// setContentFocus accents or dims the active tab body's border; no-op for a
// placeholder page.
func (d *detail) setContentFocus(focused bool) {
	if f, ok := d.content().(focusChromer); ok {
		f.setFocused(focused)
	}
}

// tabCount is the number of visible tabs, used to size the "1-N tab" hint.
func (d *detail) tabCount() int { return len(d.tabs) }

// testsRunning reports whether the Tests tab shows a running self-test, so
// the hint bar can offer cancel instead of start.
func (d *detail) testsRunning() bool {
	if v, ok := d.views["tests"].(*testsView); ok {
		return v.mode == modeRunning
	}
	return false
}
