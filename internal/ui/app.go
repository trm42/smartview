// SPDX-License-Identifier: GPL-3.0-or-later

// Package ui implements the tview-based terminal interface for smartview.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/config"
	"github.com/trm42/smartview/internal/smart"
)

// App is the smartview terminal application.
type App struct {
	app *tview.Application
	// rootPages holds the main layout on pageMain and any modal on pageModal above it.
	rootPages *tview.Pages
	root      *tview.Flex
	list      *tview.List
	detail    *detail
	// Inert: no key binding, so a click must not strand focus on them.
	status *inertTextView
	banner *inertTextView

	// bodyPages swaps the body between the per-drive view and the fleet comparison.
	bodyPages *tview.Pages
	fleet     *fleetView

	// rail is the narrow-layout drive selector.
	rail      *inertTextView
	body      *tview.Flex
	narrow    bool
	lastWidth int

	interval   time.Duration
	themeName  string
	startView  string // consulted once, in Run
	refreshCh  chan struct{}
	wakeCh     chan struct{} // refresh that wakes spun-down drives
	intervalCh chan time.Duration
	// settingsHelp is the settings modal's focus-following help line; rebuilt per open.
	settingsHelp *inertTextView

	// save persists the settings modal's result; injected so the UI never touches the filesystem.
	save    func(config.Config) error
	rootCtx context.Context

	// refreshing and standbyAware cross the poll goroutine, hence atomic.
	refreshing   atomic.Bool
	standbyAware atomic.Bool
	spinFrame    int

	// Event-loop only, except devices: written once in Run before the poll goroutine starts.
	devices []smart.Device
	reports map[string]*smart.Report
	history map[string][]float64 // runtime temperature series per device
	// asleep: drives smartctl declined to wake; lastRead: when reports[name] was read.
	asleep   map[string]bool
	lastRead map[string]time.Time
	// startedTests is the self-test type smartview started per device; drives never report it. Aged out in observeSelfTest.
	startedTests map[string]startedTest
	// sharedModels marks model names more than one drive reports; rebuilt with the list.
	sharedModels map[string]bool
	inModal      bool
	// pendingNotices wait for the open modal to close; pushModal would replace it.
	pendingNotices []string
	fleetMode      bool

	// bannerShown: its text is set once, so theme cycles must call refreshBanner.
	bannerShown bool
}

// maxHistory bounds the NVMe temperature ring buffer (~60 min at 30s).
const maxHistory = 120

const spinnerInterval = 120 * time.Millisecond

// New constructs the application from validated settings; save persists what the settings modal produces.
func New(cfg config.Config, save func(config.Config) error) *App {
	setTheme(themes[cfg.Theme])
	a := &App{
		app:          tview.NewApplication(),
		list:         tview.NewList(),
		detail:       newDetail(),
		status:       newInertTextView(),
		banner:       newInertTextView(),
		rail:         newInertTextView(),
		lastWidth:    -1,
		interval:     cfg.RefreshInterval.Duration(),
		themeName:    cfg.Theme,
		startView:    cfg.StartView,
		save:         save,
		refreshCh:    make(chan struct{}, 1),
		wakeCh:       make(chan struct{}, 1),
		intervalCh:   make(chan time.Duration, 1),
		reports:      map[string]*smart.Report{},
		history:      map[string][]float64{},
		asleep:       map[string]bool{},
		lastRead:     map[string]time.Time{},
		startedTests: map[string]startedTest{},
	}
	a.standbyAware.Store(cfg.StandbyAware)
	a.detail.showAllTabs = cfg.ShowUnavailableTabs
	a.build()
	return a
}

// applyStartView opens the screen start_view names; runs once, from Run.
func (a *App) applyStartView() {
	if a.startView == config.StartFleet && !a.fleetMode {
		a.toggleFleet()
	}
}

// build assembles the widget tree and installs key bindings.
func (a *App) build() {
	a.list.ShowSecondaryText(true).SetHighlightFullLine(true)
	styleList(a.list)
	titledBox(a.list.Box, " Drives ")
	// The index tview passes is the new one; GetCurrentItem() is not yet.
	a.list.SetChangedFunc(func(i int, _, _ string, _ rune) {
		a.showDevice(i)
		a.refreshChrome()
	})

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
	a.detail.bar.onClick = a.openTab

	a.root = root
	a.rootPages = tview.NewPages().AddPage(pageMain, root, true, true)
	a.app.SetRoot(a.rootPages, true).EnableMouse(true)
	a.app.SetInputCapture(a.onKey)
	// Width is only known at draw time, so the layout choice lives here.
	a.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		// tview has already cleared with its default style; SetStyle alone shows one frame of the old ground.
		ground := tcell.StyleDefault.Background(activeTheme.Background)
		screen.SetStyle(ground)
		screen.Fill(' ', ground)
		w, _ := screen.Size()
		if w != a.lastWidth {
			a.lastWidth = w
			a.setNarrow(w < narrowBreakpoint)
		}
		return false
	})
	a.refreshFocusChrome()
}

// narrowBreakpoint is the width below which list and detail cannot both be useful.
const narrowBreakpoint = 100

// setNarrow switches layouts when the choice changed; runs from the draw hook.
func (a *App) setNarrow(narrow bool) {
	if narrow == a.narrow {
		return
	}
	a.applyLayout(narrow)
	a.refreshChrome()
	// The list is off-tree when narrow. SetFocus would deadlock inside the draw
	// hook (mutex held), and QueueUpdate blocks until the draw returns, hence the goroutine.
	if narrow && a.list.HasFocus() {
		go a.app.QueueUpdateDraw(a.focusDetail)
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
		a.renderRail(a.list.GetCurrentItem())
		return
	}
	a.body.AddItem(a.list, driveListWidth, 0, true).
		AddItem(a.detail, 0, 1, false)
}

// driveListWidth is the drive list's fixed column width in the wide layout.
const driveListWidth = 38

// alertCount is how many drives are not healthy.
func (a *App) alertCount() int {
	n := 0
	for _, d := range a.devices {
		if rep, ok := a.reports[d.Name]; ok && rep.Overall() != smart.SeverityOK {
			n++
		}
	}
	return n
}

// driveListTitle is the wide list's title, carrying the rail's attention and standby counts.
func (a *App) driveListTitle() string {
	title := " Drives "
	if n := a.alertCount(); n > 0 {
		title += fmt.Sprintf("%s▲ %d[-] ", cautionTag(), n)
	}
	if n := a.asleepCount(); n > 0 {
		title += fmt.Sprintf("%s%s %d[-] ", mutedTag(), standbyGlyph, n)
	}
	return title
}

// asleepCount is how many of the current drives are spun down.
func (a *App) asleepCount() int {
	n := 0
	for _, d := range a.devices {
		if a.asleep[d.Name] {
			n++
		}
	}
	return n
}

// renderRail draws the narrow drive selector with the drive at cur highlighted.
// cur is passed in because the list's changed-func runs before the new index is stored.
func (a *App) renderRail(cur int) {
	if !a.narrow {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%sDrives[-] ", mutedTag())
	for i, d := range a.devices {
		name := railName(d)
		rep, ok := a.reports[d.Name]
		if !ok {
			fmt.Fprintf(&b, " %s●[-] %s%s[-]", mutedTag(), mutedTag(), esc(name))
			continue
		}
		if i == cur {
			markColor := severityColor(rep.Overall())
			if noVerdict(rep) {
				markColor = activeTheme.Muted
			}
			// The ▸ marker keeps the selection visible under mono.
			fmt.Fprintf(&b, " %s▸%s %s[-:-:-]",
				fgbgTag(markColor, activeTheme.SelectionBg), reportGlyph(rep), esc(name))
			continue
		}
		fmt.Fprintf(&b, "  %s %s", reportGlyph(rep), esc(name))
	}
	if n := a.alertCount(); n > 0 {
		fmt.Fprintf(&b, "  %s▲ %d[-]", cautionTag(), n)
	}
	// No room for a per-drive standby mark, so count them.
	if n := a.asleepCount(); n > 0 {
		fmt.Fprintf(&b, "  %s%s %d[-]", mutedTag(), standbyGlyph, n)
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

// statusText renders the bottom key-hint bar: global keys, the focused tab's
// keys, then the theme and cadence.
func (a *App) statusText() string {
	aq := accentTag()
	// Narrow terminals get a deliberately shorter bar, never a truncated one.
	if a.narrow {
		hint := a.driveNavHints()
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
		hint = a.driveNavHints() + "   " + aq + "Tab[-] focus   " + aq + "c[-] compare   " +
			aq + "r[-] refresh   " + aq + "q[-] quit"
		hint += a.contextHints()
	}
	hint += "   " + aq + "+/-[-] rate   " + aq + "t/T[-] theme   " + aq + "S[-] settings"
	return hint + fmt.Sprintf("      %s · %s", a.themeName, a.interval)
}

// driveNavHints is the drive/nav/tab prefix of the per-drive hint bar.
func (a *App) driveNavHints() string {
	aq := accentTag()
	hint := aq + "↑/↓[-] drive   " + aq + "←/→[-] nav"
	if n := a.detail.tabCount(); n >= 2 {
		hint += fmt.Sprintf("   %s1-%d[-] tab", aq, n)
	}
	return hint
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
		// Both protocols use this id, but only the ATA table binds s/f.
		if _, ok := a.detail.activeView().(*attributesView); !ok {
			return ""
		}
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

// refreshFocusChrome accents the focused pane's border; call after focus has moved.
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

// refreshBanner re-renders the root-warning banner in the active theme.
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

// cycleTheme steps delta themes along the cycle and repaints.
func (a *App) cycleTheme(delta int) {
	a.themeName = stepThemeName(a.themeName, delta)
	setTheme(themes[a.themeName])
	a.repaintAll()
}

// repaintAll re-applies the theme everywhere colour was baked in; the detail
// rebuild resets Attributes selection and scroll.
func (a *App) repaintAll() {
	a.rebuildDetail()
	styleList(a.list)
	a.populateList()
	rethemeTree(a.rootPages)
	// rethemeTree reaches only mounted widgets; the list/rail and banner may be off-tree.
	applyBackground(a.list, a.rail, a.banner)
	applyTextColor(a.status, a.rail, a.banner)
	// The fleet table bakes a colour into every cell.
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)
	a.refreshChrome()
	a.refreshBanner()
}

// Page names for bodyPages, and for the two rootPages layers.
const (
	pageDrives = "drives"
	pageFleet  = "fleet"
	pageMain   = "main"
	pageModal  = "modal"
)

// showSelected renders the cached report for the highlighted drive.
func (a *App) showSelected() { a.showDevice(a.list.GetCurrentItem()) }

// showDevice renders the cached report for the drive at index i.
func (a *App) showDevice(i int) {
	a.renderRail(i)
	if i < 0 || i >= len(a.devices) {
		return
	}
	dev := a.devices[i]
	if rep, ok := a.reports[dev.Name]; ok {
		a.detail.setNote(a.standbyNote(dev.Name))
		a.observeSelfTest(dev.Name, rep)
		a.detail.update(rep, a.history[dev.Name])
		return
	}
	a.detail.setNote("")
	if a.asleep[dev.Name] {
		a.detail.showPlaceholder(dev.Name + " is spun down.\n\n" +
			"Press R to wake it and read, or turn off standby_aware in Settings.")
		return
	}
	a.detail.showPlaceholder("Loading " + dev.Name + " …")
}

// selectedDevice returns the currently highlighted device.
func (a *App) selectedDevice() (smart.Device, bool) {
	i := a.list.GetCurrentItem()
	if i < 0 || i >= len(a.devices) {
		return smart.Device{}, false
	}
	return a.devices[i], true
}

// populateList fills the drive list from cached reports, updating rows in
// place: Clear()+AddItem() fires the changed-func and rebuilds every tab.
func (a *App) populateList() {
	a.sharedModels = a.duplicateModels()
	// The rail and the list title go stale with the rows; repaint both.
	defer func() {
		a.renderRail(a.list.GetCurrentItem())
		a.list.SetTitle(a.driveListTitle())
	}()
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

// duplicateModels reports which non-empty model names more than one reporting drive shares.
func (a *App) duplicateModels() map[string]bool {
	seen, dup := map[string]bool{}, map[string]bool{}
	for _, d := range a.devices {
		rep, ok := a.reports[d.Name]
		if !ok || rep.ModelName == "" {
			continue
		}
		if seen[rep.ModelName] {
			dup[rep.ModelName] = true
		}
		seen[rep.ModelName] = true
	}
	return dup
}

// standbyGlyph marks a drive smartctl declined to wake: its values are real but not current.
const standbyGlyph = "◌"

// standbyMark returns the standby prefix for a drive, or "" when it is awake.
func (a *App) standbyMark(name string) string {
	if !a.asleep[name] {
		return ""
	}
	return mutedTag() + standbyGlyph + "[-] "
}

// standbyNote dates a spun-down drive's cached values for the caveat row.
func (a *App) standbyNote(name string) string {
	if !a.asleep[name] {
		return ""
	}
	read, ok := a.lastRead[name]
	if !ok {
		return fmt.Sprintf("%s%s Spun down — no reading yet; press R to wake and read.[-]",
			mutedTag(), standbyGlyph)
	}
	return fmt.Sprintf("%s%s Spun down — values as of %s, %s ago.[-]",
		mutedTag(), standbyGlyph, read.Format("15:04"), roundDuration(time.Since(read)))
}

// listRow renders the main/secondary text for a drive row.
func (a *App) listRow(d smart.Device) (string, string) {
	rep, ok := a.reports[d.Name]
	if !ok {
		sec := "scanning…"
		if a.asleep[d.Name] {
			sec = a.standbyMark(d.Name) + "asleep · R to read"
		}
		return fmt.Sprintf("%s●[-] %s", mutedTag(), esc(shortName(d))), sec
	}
	model := esc(rep.ModelName)
	if rep.ModelName == "" {
		model = esc(shortName(d))
	}
	main := fmt.Sprintf("%s %s", reportGlyph(rep), model)
	// Identical models are common; name the device when the model alone is ambiguous.
	if a.sharedModels[rep.ModelName] {
		main += mutedTag() + " · " + esc(railName(d)) + "[-]"
	}
	// tempCell ends with a style reset, so the temperature goes last; the health glyph stays undimmed while asleep.
	sec := fmt.Sprintf("%s%s · %s · %s",
		a.standbyMark(d.Name), esc(shortName(d)), capacityString(rep), tempCell(rep))
	return main, sec
}

// Run performs the initial scan and starts the event loop and poll goroutine.
func (a *App) Run(ctx context.Context) error {
	a.rootCtx = ctx
	// Bounded like every poll: a wedged device would otherwise hang startup on a blank terminal.
	scanCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	devices, err := smart.Scan(scanCtx)
	cancel()
	if err != nil && len(devices) == 0 {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("scan drives: smartctl --scan-open did not respond within %s", fetchTimeout)
		}
		return fmt.Errorf("scan drives: %w (try running with sudo)", err)
	}
	a.devices = devices
	a.populateList()
	if len(devices) == 0 {
		a.detail.showPlaceholder(noDrivesText)
	}
	a.applyStartView()

	// Stop the UI on context cancellation (SIGINT/SIGTERM).
	go func() {
		<-ctx.Done()
		a.app.Stop()
	}()

	go a.pollLoop(ctx, a.interval)
	go a.animateSpinner(ctx)
	return a.app.Run()
}

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

// animateSpinner advances the spinner while a refresh is underway.
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

// shortDevice trims a device name to n runes for display, keeping whole
// trailing path components since a character cut makes IOService paths look alike.
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
