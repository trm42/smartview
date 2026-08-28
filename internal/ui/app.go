// SPDX-License-Identifier: GPL-3.0-or-later

// Package ui implements the tview-based terminal interface for smartview.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// App is the smartview terminal application.
type App struct {
	app    *tview.Application
	root   tview.Primitive // main layout, restored when a modal closes
	list   *tview.List
	detail *detail
	status *tview.TextView
	banner *tview.TextView

	// bodyPages swaps the body between the per-drive view and the fleet
	// comparison; it sits inside root so banner/status/modals are shared.
	bodyPages *tview.Pages
	fleet     *fleetView

	// rail is the narrow-layout drive selector; narrow/lastWidth make the
	// layout swap happen only when the width crosses the breakpoint.
	rail      *tview.TextView
	body      *tview.Flex
	narrow    bool
	lastWidth int

	interval   time.Duration
	themeName  string // active colour theme; cycled by the 'T' key
	refreshCh  chan struct{}
	intervalCh chan time.Duration // runtime interval changes → poll-loop ticker reset
	// rootCtx is the application context; interactive smartctl calls derive
	// from it so they are cancelled on shutdown.
	rootCtx context.Context

	// refreshing crosses the poll and animation goroutines, hence atomic —
	// the only field that does; the maps below stay event-loop-only.
	refreshing atomic.Bool
	spinFrame  int // animation frame; mutated only inside QueueUpdateDraw

	// All fields below are touched only on the main (event-loop) goroutine,
	// either directly or inside QueueUpdateDraw callbacks, so need no locking.
	// One carve-out like refreshing above: devices is written once in Run,
	// before the poll goroutine is started, and only read afterwards — so the
	// poll loop may range over it off the event loop without synchronisation.
	devices []smart.Device
	reports map[string]*smart.Report
	history map[string][]float64 // runtime temperature series per device
	// startedTests remembers, per device, the self-test type smartview itself
	// started. The drive reports a running test's progress but never its type
	// (ATA's status string is "in progress, N% remaining"), so this is the only
	// source for the Tests tab's time estimate — a test smartview did not start
	// stays absent and gets none. Entries age out in observeSelfTest.
	startedTests map[string]startedTest
	inModal      bool // true while a modal overlay is shown
	fleetMode    bool // true while the fleet comparison is on screen

	// bannerShown: the root-warning banner is in the layout. Its text is set
	// once at build, so theme cycles must call refreshBanner explicitly.
	bannerShown bool
}

// startedTest records the self-test type smartview asked a drive to run, plus
// whether the drive has since been seen running it (see observeSelfTest).
type startedTest struct {
	typ  smart.SelfTestType
	seen bool
}

// maxHistory bounds the temperature ring buffer backing the NVMe sparkline
// (~60 min at the default 30s poll; a rough trend, not a fixed-time axis).
const maxHistory = 120

// spinnerInterval is the refresh spinner animation cadence.
const spinnerInterval = 120 * time.Millisecond

// intervalPresets is the ladder the +/- keys walk to change poll cadence.
var intervalPresets = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
}

// nextInterval returns the adjacent preset, clamped at the ends. It walks by
// value, not index, so an off-ladder --interval snaps to a neighbour.
func nextInterval(cur time.Duration, faster bool) time.Duration {
	if faster {
		next := intervalPresets[0]
		for _, p := range intervalPresets {
			if p < cur {
				next = p
			}
		}
		return next
	}
	next := intervalPresets[len(intervalPresets)-1]
	for i := len(intervalPresets) - 1; i >= 0; i-- {
		if intervalPresets[i] > cur {
			next = intervalPresets[i]
		}
	}
	return next
}

// New constructs the application. themeName must be valid (caller checks
// HasTheme); the theme installs before build so widgets get its colours.
func New(interval time.Duration, themeName string) *App {
	setTheme(themes[themeName])
	a := &App{
		app:          tview.NewApplication(),
		list:         tview.NewList(),
		detail:       newDetail(),
		status:       tview.NewTextView().SetDynamicColors(true),
		banner:       tview.NewTextView().SetDynamicColors(true),
		rail:         tview.NewTextView().SetDynamicColors(true),
		lastWidth:    -1,
		interval:     interval,
		themeName:    themeName,
		refreshCh:    make(chan struct{}, 1),
		intervalCh:   make(chan time.Duration, 1),
		reports:      map[string]*smart.Report{},
		history:      map[string][]float64{},
		startedTests: map[string]startedTest{},
	}
	a.build()
	return a
}

// build assembles the widget tree and installs key bindings.
func (a *App) build() {
	a.list.ShowSecondaryText(true).SetHighlightFullLine(true)
	styleList(a.list)
	a.list.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" Drives ")
	a.list.SetChangedFunc(func(int, string, string, rune) { a.showSelected() })

	a.banner.SetBorderPadding(0, 0, uiGutter, uiGutter)
	a.status.SetBorderPadding(0, 0, uiGutter, uiGutter)
	a.status.SetText(a.statusText())

	a.rail.SetBorderPadding(0, 0, uiGutter, uiGutter)
	a.body = tview.NewFlex()
	a.applyLayout(false)

	a.fleet = newFleetView(a.openDrive)
	a.bodyPages = tview.NewPages().
		AddPage(pageDrives, a.body, true, true).
		AddPage(pageFleet, a.fleet, true, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	// Full SMART access usually requires root; warn when we lack it.
	if os.Geteuid() != 0 {
		a.bannerShown = true
		a.refreshBanner()
		root.AddItem(a.banner, 1, 0, false)
	}
	root.AddItem(a.bodyPages, 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.detail.selfTest = selfTestActions{
		run:     a.onSelfTestRun,
		cancel:  a.onSelfTestCancel,
		started: a.selfTestStarted,
	}

	a.root = root
	a.app.SetRoot(root, true).EnableMouse(true)
	a.app.SetInputCapture(a.onKey)
	// Width is only known at draw time, so the layout choice lives here.
	a.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		w, _ := screen.Size()
		if w != a.lastWidth {
			a.lastWidth = w
			a.setNarrow(w < narrowBreakpoint)
		}
		return false
	})
	// The drive list is the initial focus; accent its border from the start.
	a.refreshFocusChrome()
}

// narrowBreakpoint is the width below which the drive list and the detail
// pane cannot both be useful.
const narrowBreakpoint = 100

// setNarrow switches between the two-pane and narrow layouts when the choice
// changed. Runs on the event-loop goroutine (from the draw hook).
func (a *App) setNarrow(narrow bool) {
	if narrow == a.narrow && a.body.GetItemCount() > 0 {
		return
	}
	a.applyLayout(narrow)
	a.refreshBanner()
	a.refreshChrome()
	// The list is not in the narrow layout, so focus cannot rest on it there:
	// tview would then forward no key to anything, since the focused primitive
	// is off-tree. The move has to be queued rather than done here — setNarrow
	// runs from the before-draw hook, which tview calls with the application
	// mutex held, and SetFocus takes that same (non-reentrant) mutex. Calling it
	// inline self-deadlocks the event loop on the very first draw. The goroutine
	// is required too: QueueUpdate blocks until the event loop runs the closure,
	// which cannot happen until this draw returns.
	if narrow && a.list.HasFocus() {
		go a.app.QueueUpdateDraw(func() {
			a.app.SetFocus(a.detail.content())
			a.refreshChrome()
		})
	}
}

// applyLayout installs the arrangement: wide is list beside detail; narrow
// collapses the list to a one-row rail and gives the detail the full width.
func (a *App) applyLayout(narrow bool) {
	a.narrow = narrow
	a.body.Clear().SetDirection(tview.FlexColumn)
	if narrow {
		a.body.SetDirection(tview.FlexRow).
			AddItem(a.rail, 1, 0, false).
			AddItem(a.detail, 0, 1, true)
		a.renderRail()
		return
	}
	a.body.AddItem(a.list, driveListWidth, 0, true).
		AddItem(a.detail, 0, 1, false)
}

// driveListWidth is the drive list's fixed column width in the wide layout.
const driveListWidth = 38

// renderRail draws the narrow drive selector: one row of severity glyphs and
// short names, selection highlighted, plus an attention count.
func (a *App) renderRail() {
	if !a.narrow {
		return
	}
	cur := a.list.GetCurrentItem()
	var b strings.Builder
	fmt.Fprintf(&b, "%sDrives[-] ", mutedTag())
	alerts := 0
	for i, d := range a.devices {
		name := railName(d)
		rep, ok := a.reports[d.Name]
		if !ok {
			fmt.Fprintf(&b, " %s●[-] %s%s[-]", mutedTag(), mutedTag(), esc(name))
			continue
		}
		sev := rep.Overall()
		if sev != smart.SeverityOK {
			alerts++
		}
		if i == cur {
			// The ▸ marker keeps the selection visible under mono.
			fmt.Fprintf(&b, " %s▸%s %s[-:-:-]",
				fgbgTag(severityColor(sev), activeTheme.SelectionBg), healthGlyph(sev), esc(name))
			continue
		}
		fmt.Fprintf(&b, "  %s %s", healthGlyph(sev), esc(name))
	}
	if alerts > 0 {
		fmt.Fprintf(&b, "  %s▲ %d[-]", cautionTag(), alerts)
	}
	a.rail.SetText(b.String())
}

// railName is the shortest identifying form of a device name (drops /dev/).
func railName(d smart.Device) string {
	n := shortDevice(d.Name, railDeviceWidth)
	return strings.TrimPrefix(n, "/dev/")
}

// railDeviceWidth bounds a name on the rail.
const railDeviceWidth = 10

// statusText renders the bottom key-hint bar: global keys, then a context
// segment for the focused tab, then the refresh cadence.
func (a *App) statusText() string {
	aq := accentTag()
	// Narrow terminals get a deliberately shorter bar, never a truncated one.
	if a.narrow {
		hint := aq + "↑/↓[-] drive   " + aq + "←/→[-] nav"
		if n := a.detail.tabCount(); n >= 2 {
			hint += fmt.Sprintf("   %s1-%d[-] tab", aq, n)
		}
		if a.fleetMode {
			hint = aq + "↑/↓[-] drive   " + aq + "←/→[-] section"
		}
		hint += "   " + aq + "c[-] compare   " + aq + "q[-] quit   " + aq + "?[-] keys"
		return hint + fmt.Sprintf("   %s%s[-]", mutedTag(), a.interval)
	}

	var hint string
	if a.fleetMode {
		hint = a.fleetHints()
	} else {
		hint = aq + "↑/↓[-] drive   " + aq + "←/→[-] nav"
		if n := a.detail.tabCount(); n >= 2 {
			hint += fmt.Sprintf("   %s1-%d[-] tab", aq, n)
		}
		hint += "   " + aq + "Tab[-] focus   " + aq + "c[-] compare   " +
			aq + "r[-] refresh   " + aq + "q[-] quit"
		hint += a.contextHints()
	}
	hint += "   " + aq + "+/-[-] rate   " + aq + "T[-] theme"
	return hint + fmt.Sprintf("      %s · refresh every %s", a.themeName, a.interval)
}

// fleetHints is the fleet comparison's key-hint bar, swapped in wholesale.
func (a *App) fleetHints() string {
	aq := accentTag()
	hint := aq + "↑/↓[-] drive"
	if n := a.fleet.sectionCount(); n >= 2 {
		hint += fmt.Sprintf("   %s←/→ 1-%d[-] section", aq, n)
	}
	return hint + "   " + aq + "s[-] sort   " + aq + "Enter[-] open drive   " +
		aq + "c/Esc[-] back   " + aq + "r[-] refresh   " + aq + "q[-] quit"
}

// contextHints returns the hints for the focused detail tab; empty while the
// list holds focus, so the bar only advertises keys that currently work.
func (a *App) contextHints() string {
	if a.list.HasFocus() {
		return ""
	}
	aq := accentTag()
	switch a.detail.activeID() {
	case "attributes":
		return "   " + aq + "s[-] sort   " + aq + "f[-] filter"
	case "tests":
		if a.detail.testsRunning() {
			return "   " + aq + "x[-] cancel test"
		}
		return "   " + aq + "Enter[-] start test"
	}
	return ""
}

// refreshChrome resyncs focus-border accents and the hint bar; call after any
// focus change, tab change, or poll.
func (a *App) refreshChrome() {
	a.refreshFocusChrome()
	a.status.SetText(a.statusText())
}

// refreshFocusChrome accents the focused pane's border and dims the other.
// Must be called after focus has actually moved.
func (a *App) refreshFocusChrome() {
	a.fleet.setFocused(a.fleetMode)
	if a.fleetMode {
		a.list.SetBorderColor(borderColor(false))
		a.detail.setContentFocus(false)
		return
	}
	listFocused := !a.narrow && a.list.HasFocus()
	a.list.SetBorderColor(borderColor(listFocused))
	a.detail.setContentFocus(!listFocused)
}

// refreshBanner re-renders the root-warning banner in the active theme — its
// text is set once at build, so theme cycles would otherwise miss it.
func (a *App) refreshBanner() {
	if !a.bannerShown {
		return
	}
	// Must fit an 80-column terminal without losing the sudo hint.
	const text = " ⚠ Without root some drives report limited data — re-run with sudo. "
	if activeTheme.BannerBg == tcell.ColorDefault {
		// Under mono the background disappears; a left bar + bold survive.
		a.banner.SetText("[::b]▌" + text + "[-:-:-]")
		return
	}
	a.banner.SetText(fgbgTag(activeTheme.Inverse, activeTheme.BannerBg) + text + "[-:-]")
}

// cycleTheme advances to the next theme and repaints. Runs on the UI
// goroutine (from onKey), so no QueueUpdateDraw is needed.
func (a *App) cycleTheme() {
	a.themeName = nextThemeName(a.themeName)
	setTheme(themes[a.themeName])
	a.repaintAll()
}

// repaintAll re-applies the theme everywhere colour was baked in at build
// time: force a detail rebuild, then re-render list, fleet, chrome and
// banner. Trade-off: the rebuild resets Attributes selection/scroll.
func (a *App) repaintAll() {
	a.detail.device = "" // invalidate cache so update() takes the rebuild branch
	a.showSelected()
	styleList(a.list)
	a.populateList()
	// The fleet table bakes a colour into every cell, same miss as the banner.
	a.fleet.refresh(a.devices, a.reports, a.history)
	a.refreshChrome()
	a.refreshBanner()
}

// onKey is the global key handler.
func (a *App) onKey(ev *tcell.EventKey) *tcell.EventKey {
	if a.inModal {
		return ev // let the modal handle all input
	}
	// Keys the fleet view doesn't claim fall through to the shared bindings.
	if a.fleetMode && a.onFleetKey(ev) {
		return nil
	}
	switch ev.Key() {
	case tcell.KeyEscape:
		a.app.Stop()
		return nil
	case tcell.KeyTab:
		a.toggleFocus()
		return nil
	case tcell.KeyUp, tcell.KeyDown:
		// Wide leaves the arrows to the focused widget (the drive list, or the
		// detail body it scrolls). Narrow has no list on screen for them to
		// reach, so the drive selection is stepped from here — the narrow hint
		// bar advertises "↑/↓ drive" and it has to be true. Line-scrolling the
		// detail there is j/k, paging is PgUp/PgDn. Fleet mode is exempt: its
		// table is focused and owns them (see onFleetKey).
		if !a.narrow || a.fleetMode {
			return ev
		}
		delta := 1
		if ev.Key() == tcell.KeyUp {
			delta = -1
		}
		a.stepDrive(delta)
		return nil
	case tcell.KeyLeft:
		a.focusLeft()
		return nil
	case tcell.KeyRight:
		a.focusRight()
		return nil
	case tcell.KeyRune:
		switch r := ev.Rune(); r {
		case 'q':
			a.app.Stop()
			return nil
		case 'r':
			a.triggerRefresh()
			return nil
		case '+', '-':
			a.setInterval(nextInterval(a.interval, r == '-'))
			return nil
		case 't':
			if a.detail.selectTabID("tests") {
				a.app.SetFocus(a.detail.content())
				a.refreshChrome()
			}
			return nil
		case 'c':
			a.toggleFleet()
			return nil
		case '?':
			a.showKeys()
			return nil
		case 'T':
			// Uppercase cycles the theme; lowercase t (above) is the Tests tab.
			a.cycleTheme()
			return nil
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			a.detail.selectTab(int(r - '1'))
			a.app.SetFocus(a.detail.content())
			a.refreshChrome()
			return nil
		}
	}
	return ev
}

// Page names for bodyPages.
const (
	pageDrives = "drives"
	pageFleet  = "fleet"
)

// onFleetKey handles the keys the fleet view claims, reporting whether it
// consumed the event. Up/Down, Enter and 's' are deliberately absent — they
// belong to the focused table itself.
func (a *App) onFleetKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		// Esc is "back" here, not "quit"; q still quits.
		a.exitFleet(false)
		return true
	case tcell.KeyTab:
		return true // swallow rather than orphan focus
	case tcell.KeyLeft:
		a.stepFleetSection(-1)
		return true
	case tcell.KeyRight:
		a.stepFleetSection(1)
		return true
	case tcell.KeyRune:
		switch r := ev.Rune(); {
		case r == 't':
			return true // no drive on screen for the Tests tab to address
		case r >= '1' && r <= '9':
			a.fleet.selectSection(int(r - '1'))
			a.refreshChrome()
			return true
		}
	}
	return false
}

// stepFleetSection moves the focus metric and resyncs the hint bar.
func (a *App) stepFleetSection(delta int) {
	if a.fleet.stepSection(delta) {
		a.refreshChrome()
	}
}

// toggleFleet switches between the per-drive view and the fleet comparison.
func (a *App) toggleFleet() {
	if a.fleetMode {
		a.exitFleet(false)
		return
	}
	a.fleetMode = true
	// Render from the cache so entry doesn't wait for the next poll.
	a.fleet.refresh(a.devices, a.reports, a.history)
	a.bodyPages.SwitchToPage(pageFleet)
	a.app.SetFocus(a.fleet.table)
	a.refreshChrome()
}

// exitFleet returns to the per-drive view; focusDetail focuses the detail
// pane (opening a drive) instead of the list (plain "back").
func (a *App) exitFleet(focusDetail bool) {
	a.fleetMode = false
	a.bodyPages.SwitchToPage(pageDrives)
	// Narrow keeps focus on the detail either way: the list is not in that
	// layout, and focus on an off-tree primitive reaches nothing.
	if focusDetail || a.narrow {
		a.app.SetFocus(a.detail.content())
	} else {
		a.app.SetFocus(a.list)
	}
	a.refreshChrome()
}

// openDrive selects a drive by device name and leaves the fleet view for its
// detail.
func (a *App) openDrive(name string) {
	for i, d := range a.devices {
		if d.Name == name {
			a.list.SetCurrentItem(i)
			break
		}
	}
	// SetCurrentItem fires the changed-func *before* storing the new index, so
	// its showSelected rendered the previous drive; render again here.
	a.showSelected()
	a.exitFleet(true)
}

// toggleFocus moves focus between the drive list and the detail content. In the
// narrow layout there is nothing to toggle — the list is not in the widget tree,
// so focusing it would park focus off-tree and tview would forward no key at all.
func (a *App) toggleFocus() {
	if a.narrow {
		a.app.SetFocus(a.detail.content())
		a.refreshChrome()
		return
	}
	if a.list.HasFocus() {
		a.app.SetFocus(a.detail.content())
	} else {
		a.app.SetFocus(a.list)
	}
	a.refreshChrome()
}

// focusRight advances along the chain list → tab0 → … → tabN (no wrap).
func (a *App) focusRight() {
	if a.list.HasFocus() {
		a.app.SetFocus(a.detail.content())
		a.refreshChrome()
		return
	}
	if a.detail.stepTab(1) {
		a.app.SetFocus(a.detail.content())
		a.refreshChrome()
	}
}

// focusLeft is the reverse of focusRight, falling through to the drive list.
// The narrow layout has no list to fall through to, so it stops at the first tab.
func (a *App) focusLeft() {
	if a.list.HasFocus() {
		return
	}
	if a.detail.active == 0 {
		if a.narrow {
			return
		}
		a.app.SetFocus(a.list)
		a.refreshChrome()
		return
	}
	a.detail.stepTab(-1)
	a.app.SetFocus(a.detail.content())
	a.refreshChrome()
}

// triggerRefresh asks the poll loop to fetch immediately (non-blocking).
func (a *App) triggerRefresh() {
	select {
	case a.refreshCh <- struct{}{}:
	default:
	}
}

// setInterval changes the poll cadence at runtime: updates a.interval,
// signals the poll loop to reset its ticker, refreshes the status bar.
func (a *App) setInterval(d time.Duration) {
	a.interval = d
	select {
	case a.intervalCh <- d:
	default:
	}
	a.refreshChrome()
}

// stepDrive moves the drive selection by delta, clamped at both ends. It is the
// narrow layout's stand-in for arrowing the drive list, which is not on screen
// there; the wide layout never needs it because the list handles its own keys.
func (a *App) stepDrive(delta int) {
	cur := a.list.GetCurrentItem()
	next := cur + delta
	if next < 0 || next >= a.list.GetItemCount() {
		return
	}
	a.list.SetCurrentItem(next)
	// SetCurrentItem fires the changed-func *before* storing the new index (see
	// openDrive), so render again now that it is current.
	a.showSelected()
	a.refreshChrome()
}

// showSelected renders the cached report for the highlighted drive.
func (a *App) showSelected() {
	// The rail is the narrow layout's drive list, and the selection marker it
	// carries has to follow the selection like the list's highlight does.
	a.renderRail()
	dev, ok := a.selectedDevice()
	if !ok {
		return
	}
	if rep, ok := a.reports[dev.Name]; ok {
		a.observeSelfTest(dev.Name, rep)
		a.detail.update(rep, a.history[dev.Name])
	} else {
		a.detail.showPlaceholder("Loading " + dev.Name + " …")
	}
}

// selectedDevice returns the currently highlighted device.
func (a *App) selectedDevice() (smart.Device, bool) {
	i := a.list.GetCurrentItem()
	if i < 0 || i >= len(a.devices) {
		return smart.Device{}, false
	}
	return a.devices[i], true
}

// selfTestStarted reports the self-test type smartview started on the selected
// drive, or "" when unknown: the drive reports progress but not what is running.
func (a *App) selfTestStarted() smart.SelfTestType {
	dev, ok := a.selectedDevice()
	if !ok {
		return ""
	}
	return a.startedTests[dev.Name].typ
}

// observeSelfTest ages out the recorded type for a device. The record is dropped
// only after the drive has been seen running the test: dropping it on the first
// idle report would race the refresh that follows a start, and never dropping it
// would let a stale type label a test another tool began.
func (a *App) observeSelfTest(name string, rep *smart.Report) {
	st, ok := a.startedTests[name]
	if !ok {
		return
	}
	if _, _, running := rep.SelfTestProgress(); running {
		if !st.seen {
			st.seen = true
			a.startedTests[name] = st
		}
		return
	}
	if st.seen {
		delete(a.startedTests, name)
	}
}

// populateList fills the drive list from cached reports. Existing rows are
// updated in place with SetItemText: Clear()+AddItem() fires SetChangedFunc,
// which would flip the detail to another drive and back, rebuilding every tab.
func (a *App) populateList() {
	// The rail renders the same rows in one line, so it is repainted wherever
	// the list is — otherwise its glyphs, alert count and theme colours freeze
	// at the moment the narrow layout was installed. No-op when not narrow.
	defer a.renderRail()
	if a.list.GetItemCount() != len(a.devices) {
		cur := a.list.GetCurrentItem()
		a.list.Clear()
		for _, d := range a.devices {
			main, sec := a.listRow(d)
			a.list.AddItem(main, sec, 0, nil)
		}
		if cur >= 0 && cur < len(a.devices) {
			a.list.SetCurrentItem(cur)
		}
		return
	}
	for i, d := range a.devices {
		main, sec := a.listRow(d)
		a.list.SetItemText(i, main, sec)
	}
}

// listRow renders the main/secondary text for a drive row.
func (a *App) listRow(d smart.Device) (string, string) {
	rep, ok := a.reports[d.Name]
	if !ok {
		return fmt.Sprintf("%s●[-] %s", mutedTag(), esc(shortName(d))), "scanning…"
	}
	// Model and device name are drive-controlled; esc() blocks markup injection.
	model := esc(rep.ModelName)
	if rep.ModelName == "" {
		model = esc(shortName(d))
	}
	main := fmt.Sprintf("%s %s", healthGlyph(rep.Overall()), model)
	// Temperature goes last: tempCell ends with a style reset that would drop
	// the secondary colour for anything after it.
	sec := fmt.Sprintf("%s · %s · %s",
		esc(shortName(d)), capacityString(rep), tempCell(rep))
	return main, sec
}

// Run performs the initial scan and starts the event loop and poll goroutine.
func (a *App) Run(ctx context.Context) error {
	a.rootCtx = ctx
	devices, err := smart.Scan(ctx)
	if err != nil && len(devices) == 0 {
		return fmt.Errorf("scan drives: %w (try running with sudo)", err)
	}
	a.devices = devices
	a.populateList()
	if len(devices) == 0 {
		a.detail.showPlaceholder("No drives found. Try running with sudo.")
	}

	// Stop the UI on context cancellation (SIGINT/SIGTERM).
	go func() {
		<-ctx.Done()
		a.app.Stop()
	}()

	go a.pollLoop(ctx, a.interval)
	go a.animateSpinner(ctx)
	return a.app.Run()
}

// spinnerFrames are single-width braille glyphs; swap for ASCII (`|/-\`) if a
// target terminal can't render braille.
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// renderSpinner paints the top-right spinner cell; event-loop goroutine only.
func (a *App) renderSpinner() {
	if a.refreshing.Load() {
		a.detail.spinner.SetText(fmt.Sprintf("%s%c[-] ",
			accentTag(), spinnerFrames[a.spinFrame%len(spinnerFrames)]))
	} else {
		a.detail.spinner.SetText("")
	}
}

// animateSpinner advances the spinner while a refresh is underway; idle it
// queues no redraws.
func (a *App) animateSpinner(ctx context.Context) {
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !a.refreshing.Load() {
				continue
			}
			a.app.QueueUpdateDraw(func() {
				a.spinFrame++
				a.renderSpinner()
			})
		}
	}
}

// shortName trims an over-long device name for display.
func shortName(d smart.Device) string {
	return shortDevice(d.Name, listDeviceWidth)
}

// listDeviceWidth is the display budget for a device name in the drive list.
const listDeviceWidth = 30

// shortDevice trims a device name to n runes — display only; names stay
// verbatim everywhere else. Whole trailing path components are kept, since a
// character cut makes every macOS IOService path look alike.
func shortDevice(name string, n int) string {
	if len([]rune(name)) <= n {
		return name
	}
	if parts := strings.Split(name, "/"); len(parts) > 1 {
		out := ""
		for i := len(parts) - 1; i >= 0; i-- {
			cand := parts[i]
			if out != "" {
				cand += "/" + out
			}
			if len([]rune(cand))+2 > n { // +2 for the leading "…/"
				break
			}
			out = cand
		}
		if out != "" {
			return "…/" + out
		}
	}
	r := []rune(name)
	return "…" + string(r[len(r)-(n-1):])
}

// pushModal shows a modal overlay, suspending the normal key-handler logic.
func (a *App) pushModal(m tview.Primitive) {
	a.inModal = true
	a.app.SetRoot(m, false)
}

// popModal removes the modal overlay and restores the main layout, returning
// focus to the detail content so the Tests tab stays interactive.
func (a *App) popModal() {
	a.inModal = false
	a.app.SetRoot(a.root, true)
	if a.fleetMode {
		a.app.SetFocus(a.fleet.table)
	} else {
		a.app.SetFocus(a.detail.content())
	}
	a.refreshChrome()
}

// testLabel renders a friendly self-test name for prompts.
func testLabel(testType smart.SelfTestType) string {
	if testType == smart.SelfTestLong {
		return "long (extended)"
	}
	return string(testType)
}

// onSelfTestRun confirms, then starts a self-test on the selected drive.
func (a *App) onSelfTestRun(testType smart.SelfTestType) {
	dev, ok := a.selectedDevice()
	if !ok {
		return
	}
	a.confirm(
		fmt.Sprintf("Run %s self-test on %s?\n(Requires root; the drive stays usable.)",
			testLabel(testType), shortName(dev)),
		"Run",
		func() {
			a.status.SetText(cautionTag() + "⟳[-] Starting " + testLabel(testType) +
				" self-test on " + shortName(dev) + "…")
			a.runSmartctl(
				fmt.Sprintf("start the %s self-test on %s", testLabel(testType), shortName(dev)),
				func(ctx context.Context) error {
					return smart.RunSelfTest(ctx, dev.Name, testType)
				},
				// Recorded only on success, and only here: the type is what the
				// Tests tab times the run against, and the drive never reports it.
				func() { a.startedTests[dev.Name] = startedTest{typ: testType} })
		},
	)
}

// onSelfTestCancel confirms, then aborts the running self-test on the selected
// drive.
func (a *App) onSelfTestCancel() {
	dev, ok := a.selectedDevice()
	if !ok {
		return
	}
	a.confirm(
		fmt.Sprintf("Cancel the running self-test on %s?", shortName(dev)),
		"Cancel test",
		func() {
			a.status.SetText(cautionTag() + "⟳[-] Cancelling self-test on " + shortName(dev) + "…")
			a.runSmartctl(
				fmt.Sprintf("cancel the self-test on %s", shortName(dev)),
				func(ctx context.Context) error {
					return smart.AbortSelfTest(ctx, dev.Name)
				},
				func() { delete(a.startedTests, dev.Name) })
		},
	)
}

// runSmartctl runs a self-test control call off the event loop, then either
// surfaces the error or triggers an immediate refresh. onSuccess (optional) runs
// on the event loop only after a call the drive accepted.
func (a *App) runSmartctl(action string, fn func(context.Context) error, onSuccess func()) {
	parent := a.rootCtx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, fetchTimeout)
		defer cancel()
		err := fn(ctx)
		a.app.QueueUpdateDraw(func() {
			a.status.SetText(a.statusText())
			if err != nil {
				a.showError(action, err)
				return
			}
			if onSuccess != nil {
				onSuccess()
			}
			a.triggerRefresh()
		})
	}()
}

// confirm shows a two-button confirmation modal; onYes runs when the user picks
// the affirmative label.
func (a *App) confirm(text, yesLabel string, onYes func()) {
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{yesLabel, "Back"}).
		// Accent, not OK: the affirmative may be destructive, and OK stays
		// reserved for healthy/go semantics.
		SetButtonActivatedStyle(tcell.StyleDefault.
			Background(activeTheme.Accent).Foreground(activeTheme.Inverse)).
		SetDoneFunc(func(_ int, label string) {
			a.popModal()
			if label == yesLabel {
				onYes()
			}
		})
	a.pushModal(modal)
}

// showKeys lists every binding in a dismissable modal; the narrow hint bar
// points here, so nothing is merely hidden.
func (a *App) showKeys() {
	modal := tview.NewModal().
		// One binding per line — tview.Modal wraps, splitting two-column lines.
		SetText("Keys\n\n" +
			"↑/↓    select drive\n" +
			"←/→    previous / next tab\n" +
			"1-9    jump to tab\n" +
			"Tab    move focus\n" +
			"t      Tests tab\n" +
			"s / f  sort / filter attributes\n" +
			"c      fleet comparison\n" +
			"Enter  open drive from the fleet\n" +
			"Esc    back\n" +
			"r      refresh now\n" +
			"+ / -  refresh interval\n" +
			"T      cycle colour theme\n" +
			"q      quit").
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(int, string) { a.popModal() })
	a.pushModal(modal)
}

// showError displays a smartctl failure in a dismissable modal; action names
// the operation that failed.
func (a *App) showError(action string, err error) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Could not %s:\n%s", action, err)).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(int, string) { a.popModal() })
	a.pushModal(modal)
}
