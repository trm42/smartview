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

	// bodyPages swaps the main body between the per-drive view (list + detail)
	// and the fleet comparison. It sits inside root, so the banner and status
	// bar are shared by both and the modal push/pop machinery is unaffected.
	bodyPages *tview.Pages
	fleet     *fleetView

	interval   time.Duration
	themeName  string // active colour theme; cycled by the 'T' key
	refreshCh  chan struct{}
	intervalCh chan time.Duration // runtime interval changes → poll-loop ticker reset
	// rootCtx is the application context (set in Run); interactive smartctl
	// calls derive their timeout from it so they are cancelled on shutdown
	// rather than lingering until the per-call timeout elapses.
	rootCtx context.Context

	// refreshing is set on the poll goroutine and read on the animation
	// goroutine, so it is an atomic. This does not violate the event-loop-only
	// invariant below — the reports/history maps stay event-loop-only; only this
	// flag crosses goroutines.
	refreshing atomic.Bool
	spinFrame  int // animation frame; mutated only inside QueueUpdateDraw

	// All fields below are touched only on the main (event-loop) goroutine,
	// either directly or inside QueueUpdateDraw callbacks, so need no locking.
	devices   []smart.Device
	reports   map[string]*smart.Report
	history   map[string][]float64 // runtime temperature series per device
	inModal   bool                 // true while a modal overlay is shown
	fleetMode bool                 // true while the fleet comparison is on screen

	// bannerShown is true when the root-warning banner is part of the layout (we
	// lack root). refreshBanner re-renders its theme-coloured text on a theme
	// cycle; the banner is set once at build, so it is the easy repaint miss.
	bannerShown bool
}

// maxHistory bounds the runtime temperature ring buffer that backs the NVMe
// sparkline (NVMe has no on-device temperature log). 120 samples is a ~60-minute
// trend window at the default 30s poll interval; since the interval is now
// adjustable at runtime (+/- keys) the spanned duration varies — the sparkline
// is only ever a rough trend, not a fixed-time axis. Enough to show movement
// without growing unbounded.
const maxHistory = 120

// spinnerInterval is the cadence of the refresh spinner animation: fast enough
// to read as motion, slow enough not to spam the event loop with redraws.
const spinnerInterval = 120 * time.Millisecond

// intervalPresets is the ladder the +/- keys walk to change the poll cadence at
// runtime. The slowest end keeps background load gentle; the fastest is for a
// close watch. nextInterval walks this relative to the current value, so an
// arbitrary --interval still snaps to a neighbouring preset on the first press.
var intervalPresets = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
}

// nextInterval returns the adjacent preset relative to cur: the first preset
// strictly greater (slower) or strictly less (faster), clamped at the ends. It
// walks by value rather than a stored index so an off-ladder --interval (e.g.
// 7s) still resolves to a sensible neighbour on the first keypress.
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

// New constructs the application with the given poll interval and colour theme.
// themeName must be a known theme (validated by the caller via HasTheme); the
// theme is installed before build so every widget is created with its colours.
func New(interval time.Duration, themeName string) *App {
	setTheme(themes[themeName])
	a := &App{
		app:        tview.NewApplication(),
		list:       tview.NewList(),
		detail:     newDetail(),
		status:     tview.NewTextView().SetDynamicColors(true),
		banner:     tview.NewTextView().SetDynamicColors(true),
		interval:   interval,
		themeName:  themeName,
		refreshCh:  make(chan struct{}, 1),
		intervalCh: make(chan time.Duration, 1),
		reports:    map[string]*smart.Report{},
		history:    map[string][]float64{},
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

	body := tview.NewFlex().
		AddItem(a.list, 38, 0, true).
		AddItem(a.detail, 0, 1, false)

	a.fleet = newFleetView(a.openDrive)
	a.bodyPages = tview.NewPages().
		AddPage(pageDrives, body, true, true).
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
		run:    a.onSelfTestRun,
		cancel: a.onSelfTestCancel,
	}

	a.root = root
	a.app.SetRoot(root, true).EnableMouse(true)
	a.app.SetInputCapture(a.onKey)
	// The drive list is the initial focus; accent its border from the start.
	a.refreshFocusChrome()
}

// statusText renders the bottom key-hint bar. A stable global prefix is followed
// by a context segment for the focused tab (the sort/filter and self-test keys
// are otherwise undiscoverable), then the refresh cadence.
func (a *App) statusText() string {
	aq := accentTag()
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

// fleetHints is the key-hint segment for the fleet comparison. Its keys differ
// enough from the per-drive view (sections rather than tabs, Enter to open a
// drive) that the bar swaps wholesale rather than appending a context segment.
func (a *App) fleetHints() string {
	aq := accentTag()
	hint := aq + "↑/↓[-] drive"
	if n := a.fleet.sectionCount(); n >= 2 {
		hint += fmt.Sprintf("   %s←/→ 1-%d[-] section", aq, n)
	}
	return hint + "   " + aq + "s[-] sort   " + aq + "Enter[-] open drive   " +
		aq + "c/Esc[-] back   " + aq + "r[-] refresh   " + aq + "q[-] quit"
}

// contextHints returns the key hints specific to the focused detail tab. They
// apply only while the detail pane holds focus (the keys reach the focused tab
// body, not the drive list), so the bar stays honest about what works right now.
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

// refreshChrome resyncs the focus-border accents and the context-aware hint bar.
// Call it after any focus change, tab change, or poll (the Tests tab can flip
// idle↔running, and the available tab set can change).
func (a *App) refreshChrome() {
	a.refreshFocusChrome()
	a.status.SetText(a.statusText())
}

// refreshFocusChrome accents the border of whichever pane (drive list or detail
// content) holds keyboard focus and dims the other, so the focused container is
// always obvious. Must be called after focus has actually moved.
func (a *App) refreshFocusChrome() {
	// In fleet mode the comparison table is the only thing on screen, so it
	// holds focus unconditionally and the hidden panes are dimmed.
	a.fleet.setFocused(a.fleetMode)
	if a.fleetMode {
		a.list.SetBorderColor(borderColor(false))
		a.detail.setContentFocus(false)
		return
	}
	listFocused := a.list.HasFocus()
	a.list.SetBorderColor(borderColor(listFocused))
	a.detail.setContentFocus(!listFocused)
}

// refreshBanner re-renders the root-warning banner text in the active theme.
// The banner is set once at build, so it is the one surface a theme cycle would
// otherwise leave at the old colours; no-op when no banner is shown.
func (a *App) refreshBanner() {
	if !a.bannerShown {
		return
	}
	a.banner.SetText(fgbgTag(activeTheme.Inverse, activeTheme.BannerBg) +
		" ⚠ Running without root — some drives may report limited data; " +
		"re-run with sudo for full access. [-:-]")
}

// cycleTheme advances to the next built-in theme and repaints everything. Runs
// on the UI goroutine (called from onKey), so it mutates activeTheme and widgets
// directly without QueueUpdateDraw — the same reasoning as setInterval.
func (a *App) cycleTheme() {
	a.themeName = nextThemeName(a.themeName)
	setTheme(themes[a.themeName])
	a.repaintAll()
}

// repaintAll re-applies the active theme everywhere a colour was baked in at
// build time. Forcing a detail rebuild (by invalidating the cached device) is
// the robust hammer: it recreates every tab view with current-theme borders and
// selected-row styles. The list rows, focus chrome/hint bar and the one-shot
// banner are then re-rendered explicitly. Accepted trade-off: the rebuild resets
// the Attributes table selection/scroll — fine for an explicit user action.
func (a *App) repaintAll() {
	a.detail.device = "" // invalidate cache → next update() takes the rebuild branch
	a.showSelected()     // rebuild + re-render all tabs (preserves active tab index)
	styleList(a.list)
	a.populateList() // re-render list rows (scanning + severity glyphs)
	// The fleet table bakes a colour into every cell at build time, so it is a
	// repaint miss of the same kind as the banner; re-render it from the cache.
	a.fleet.refresh(a.devices, a.reports, a.history)
	a.refreshChrome() // status hints + focus borders
	a.refreshBanner() // re-set the root-warning banner text if shown
}

// onKey is the global key handler.
func (a *App) onKey(ev *tcell.EventKey) *tcell.EventKey {
	if a.inModal {
		return ev // let the modal handle all input
	}
	// The fleet view rebinds a handful of keys (sections rather than tabs) and
	// swallows the ones that address the hidden per-drive panes; everything it
	// does not claim falls through to the shared bindings below.
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
		case 'T':
			// Uppercase T cycles the colour theme; lowercase t (above) jumps to the
			// Tests tab. ev.Rune() distinguishes the case.
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
// consumed the event. Keys it declines (q, r, +/-, T) fall through to the
// shared bindings, so the global chrome keeps working in both modes.
//
// Up/Down and Enter are deliberately absent: they reach the focused table
// directly, which scrolls and fires its selected-func. 's' likewise belongs to
// the table's own input capture, next to the sort state it toggles.
func (a *App) onFleetKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		// Esc is "back" here rather than "quit": leaving a full-screen view is
		// what it means everywhere else, and q still quits.
		a.exitFleet(false)
		return true
	case tcell.KeyTab:
		return true // nothing to move focus between; swallow rather than orphan it
	case tcell.KeyLeft:
		a.stepFleetSection(-1)
		return true
	case tcell.KeyRight:
		a.stepFleetSection(1)
		return true
	case tcell.KeyRune:
		switch r := ev.Rune(); {
		case r == 't':
			return true // the Tests tab belongs to a drive, and none is on screen
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
	// Render from the cache on entry so the comparison is current even if the
	// next poll is half a minute away.
	a.fleet.refresh(a.devices, a.reports, a.history)
	a.bodyPages.SwitchToPage(pageFleet)
	a.app.SetFocus(a.fleet.table)
	a.refreshChrome()
}

// exitFleet returns to the per-drive view. focusDetail puts focus on the detail
// pane rather than the drive list, which is what opening a drive from the fleet
// table should do; plain "back" restores the list.
func (a *App) exitFleet(focusDetail bool) {
	a.fleetMode = false
	a.bodyPages.SwitchToPage(pageDrives)
	if focusDetail {
		a.app.SetFocus(a.detail.content())
	} else {
		a.app.SetFocus(a.list)
	}
	a.refreshChrome()
}

// openDrive selects a drive by device name and leaves the fleet view for its
// detail. Setting the list item fires its changed-func, which renders the
// drive — the same path an arrow-key selection takes.
func (a *App) openDrive(name string) {
	for i, d := range a.devices {
		if d.Name == name {
			a.list.SetCurrentItem(i)
			break
		}
	}
	// SetCurrentItem fires the list's changed-func *before* it stores the new
	// index, so the showSelected it triggers still reads the previous drive and
	// renders that one. Render again here, once the index is actually current.
	a.showSelected()
	a.exitFleet(true)
}

// toggleFocus moves focus between the drive list and the detail content.
func (a *App) toggleFocus() {
	if a.list.HasFocus() {
		a.app.SetFocus(a.detail.content())
	} else {
		a.app.SetFocus(a.list)
	}
	a.refreshChrome()
}

// focusRight advances along the focus chain list → tab0 → tab1 … → tabN. From
// the list it enters the detail at the current tab; within the detail it steps
// to the next tab, stopping at the last (no wrap).
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

// focusLeft is the reverse of focusRight: it steps to the previous tab, and on
// the first tab falls through to the drive list.
func (a *App) focusLeft() {
	if a.list.HasFocus() {
		return
	}
	if a.detail.active == 0 {
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

// setInterval changes the poll cadence at runtime. Runs on the UI goroutine: it
// updates a.interval (read by statusText), signals the poll loop to reset its
// ticker (non-blocking, like triggerRefresh), and refreshes the status bar so
// the displayed cadence updates immediately.
func (a *App) setInterval(d time.Duration) {
	a.interval = d
	select {
	case a.intervalCh <- d:
	default:
	}
	a.refreshChrome()
}

// showSelected renders the cached report for the highlighted drive.
func (a *App) showSelected() {
	dev, ok := a.selectedDevice()
	if !ok {
		return
	}
	if rep, ok := a.reports[dev.Name]; ok {
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

// populateList fills the drive list rows from cached reports. The device set is
// fixed after the initial scan, so once the rows exist they are updated in place
// with SetItemText. This is deliberate: Clear()+AddItem() fires the list's
// SetChangedFunc (on the first re-added item, and again via SetCurrentItem),
// which would momentarily switch the detail to another drive and back — a device
// switch that rebuilds every tab, discarding the Attributes selection and
// orphaning focus on the interactive Tests tab. SetItemText fires nothing.
func (a *App) populateList() {
	if a.list.GetItemCount() != len(a.devices) {
		// Initial build (or a device-count change): create the rows once.
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
	// model and the device name are drive-controlled; escape markup (see esc) so
	// the always-visible sidebar can't be made to paint a fake health verdict.
	model := esc(rep.ModelName)
	if rep.ModelName == "" {
		model = esc(shortName(d))
	}
	main := fmt.Sprintf("%s %s", healthGlyph(rep.Overall()), model)
	// Temperature is last on the line because tempCell's tint ends with a style
	// reset, which would otherwise drop the secondary colour for what follows.
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

	// Stop the UI when the context is cancelled (SIGINT/SIGTERM), so signal
	// handling quits cleanly rather than leaving the event loop running.
	go func() {
		<-ctx.Done()
		a.app.Stop()
	}()

	go a.pollLoop(ctx)
	go a.animateSpinner(ctx)
	return a.app.Run()
}

// spinnerFrames are the single-width braille glyphs cycled by the refresh
// spinner. Swap for ASCII (`|/-\`) if a target terminal can't render braille.
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// renderSpinner paints the top-right spinner cell from the current refresh
// state. Called only on the event-loop goroutine.
func (a *App) renderSpinner() {
	if a.refreshing.Load() {
		a.detail.spinner.SetText(fmt.Sprintf("%s%c[-] ",
			accentTag(), spinnerFrames[a.spinFrame%len(spinnerFrames)]))
	} else {
		a.detail.spinner.SetText("")
	}
}

// animateSpinner advances the spinner animation while a refresh is underway.
// When idle it queues no redraws, so it does not busy-spin the event loop.
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

// shortDevice trims a device name to at most n runes for display. Device names
// are round-tripped verbatim everywhere else (see CLAUDE.md); this is display
// only, and the full name is shown on the Overview.
//
// A macOS IOService path is long and its distinguishing part is a path
// component, not a character offset, so keeping the last n characters
// ("…pleANS3CGv2Controller/NS_01@1") cut mid-word and made every Apple drive
// look alike. Prefer whole trailing components, adding as many as fit.
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

// onSelfTestRun confirms, then starts a self-test on the selected drive. It is
// wired into the Tests view via selfTestActions and runs on the event loop.
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
				})
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
				})
		},
	)
}

// runSmartctl runs a self-test control call off the event loop, then restores
// the status bar and either surfaces the error or triggers an immediate refresh
// so the Tests view reflects the new state.
func (a *App) runSmartctl(action string, fn func(context.Context) error) {
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
		// Accent (not the OK colour) activated button: the affirmative may be a
		// destructive "Cancel test", so the OK colour stays reserved for healthy/go
		// semantics.
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

// showError displays a smartctl failure (commonly a permission error) in a
// dismissable modal. action names the operation that failed (e.g. "start the
// short self-test on disk0") so the message says which drive and command.
func (a *App) showError(action string, err error) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Could not %s:\n%s", action, err)).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(int, string) { a.popModal() })
	a.pushModal(modal)
}
