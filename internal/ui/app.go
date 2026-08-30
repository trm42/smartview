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

	"github.com/trm42/smartview/internal/config"
	"github.com/trm42/smartview/internal/smart"
)

// App is the smartview terminal application.
type App struct {
	app    *tview.Application
	root   *tview.Flex // main layout, restored when a modal closes
	list   *tview.List
	detail *detail
	// status, banner and the rail below carry no key binding, so they decline
	// the mouse rather than let a click strand focus on them (inertTextView).
	status *inertTextView
	banner *inertTextView

	// bodyPages swaps the body between the per-drive view and the fleet
	// comparison; it sits inside root so banner/status/modals are shared.
	bodyPages *tview.Pages
	fleet     *fleetView

	// rail is the narrow-layout drive selector; narrow/lastWidth make the
	// layout swap happen only when the width crosses the breakpoint.
	rail      *inertTextView
	body      *tview.Flex
	narrow    bool
	lastWidth int

	interval   time.Duration
	themeName  string // active colour theme; cycled by the 'T' key
	startView  string // config.StartDrives/StartFleet; consulted once, in Run
	refreshCh  chan struct{}
	wakeCh     chan struct{}      // 'R': refresh, waking spun-down drives
	intervalCh chan time.Duration // runtime interval changes → poll-loop ticker reset
	// save persists the settings modal's result. Injected so the UI never
	// touches the filesystem and tests can assert on what would be written.
	save func(config.Config) error
	// rootCtx is the application context; interactive smartctl calls derive
	// from it so they are cancelled on shutdown.
	rootCtx context.Context

	// refreshing and standbyAware cross the poll goroutine, hence atomic —
	// the only fields that do; the maps below stay event-loop-only.
	refreshing atomic.Bool
	// standbyAware is read by the poll goroutine when it picks the power
	// policy, and written on the event loop when Settings applies.
	standbyAware atomic.Bool
	spinFrame    int // animation frame; mutated only inside QueueUpdateDraw

	// All fields below are touched only on the main (event-loop) goroutine,
	// either directly or inside QueueUpdateDraw callbacks, so need no locking.
	// One carve-out like refreshing above: devices is written once in Run,
	// before the poll goroutine is started, and only read afterwards — so the
	// poll loop may range over it off the event loop without synchronisation.
	devices []smart.Device
	reports map[string]*smart.Report
	history map[string][]float64 // runtime temperature series per device
	// asleep and lastRead back the standby marks: which drives smartctl
	// declined to wake, and when reports[name] was actually read.
	asleep   map[string]bool
	lastRead map[string]time.Time
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

// maxHistory bounds the temperature ring buffer backing the NVMe sparkline
// (~60 min at the default 30s poll; a rough trend, not a fixed-time axis).
const maxHistory = 120

// spinnerInterval is the refresh spinner animation cadence.
const spinnerInterval = 120 * time.Millisecond

// New constructs the application from validated settings (main runs
// config.Validate); the theme installs before build so widgets get its
// colours. save persists what the settings modal produces.
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

// applyStartView opens the screen start_view names. It runs once, from Run:
// afterwards the 'c' key owns which screen is up.
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
	a.detail.onTabClick = a.openTab

	a.root = root
	a.app.SetRoot(root, true).EnableMouse(true)
	a.app.SetInputCapture(a.onKey)
	// Width is only known at draw time, so the layout choice lives here.
	a.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		// tview clears the screen with its default style before this hook runs,
		// so SetStyle alone would first show one frame of the old ground, and a
		// non-fullscreen modal root leaves the cleared area unpainted.
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

// renderRail draws the narrow drive selector: one row of severity glyphs and
// short names, the drive at cur highlighted, plus an attention count. cur is
// passed in because the list's changed-func runs before the new index is stored.
func (a *App) renderRail(cur int) {
	if !a.narrow {
		return
	}
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
	// The rail has no room for a per-drive mark at railDeviceWidth, so the
	// standby drives are counted instead.
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
		// Keyed on the live view, not the tab id: both protocols use the id
		// "attributes", but only the ATA table binds s/f. The NVMe health view
		// installs no input capture, so those keys fall through to onKey and are
		// dropped — advertising a binding that does nothing is worse than silence.
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
	a.rebuildDetail()
	styleList(a.list)
	a.populateList()
	// Widgets built once are not recreated by the rebuild, so they keep the
	// ground tview baked in at construction until they are told again.
	groundTree(a.root)
	// The off-tree half: applyLayout mounts the list or the rail, never both, so
	// the walk above can only have reached one of them, and build() mounts the
	// banner only when we are not root.
	applyBackground(a.list, a.rail, a.banner)
	// The fleet table bakes a colour into every cell, same miss as the banner.
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)
	a.refreshChrome()
	a.refreshBanner()
}

// Page names for bodyPages.
const (
	pageDrives = "drives"
	pageFleet  = "fleet"
)

// showSelected renders the cached report for the highlighted drive.
func (a *App) showSelected() { a.showDevice(a.list.GetCurrentItem()) }

// showDevice renders the cached report for the drive at index i.
func (a *App) showDevice(i int) {
	// The rail is the narrow layout's drive list, and the selection marker it
	// carries has to follow the selection like the list's highlight does.
	a.renderRail(i)
	if i < 0 || i >= len(a.devices) {
		return
	}
	dev := a.devices[i]
	a.detail.setNote(a.standbyNote(dev.Name))
	if rep, ok := a.reports[dev.Name]; ok {
		a.observeSelfTest(dev.Name, rep)
		a.detail.update(rep, a.history[dev.Name])
		return
	}
	if a.asleep[dev.Name] {
		// Never read and spun down: say so and name the way out, rather than
		// sit on "Loading…" for a drive that will never answer on its own.
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

// populateList fills the drive list from cached reports. Existing rows are
// updated in place with SetItemText: Clear()+AddItem() fires SetChangedFunc,
// which would flip the detail to another drive and back, rebuilding every tab.
func (a *App) populateList() {
	// The rail renders the same rows in one line, so it is repainted wherever
	// the list is — otherwise its glyphs, alert count and theme colours freeze
	// at the moment the narrow layout was installed. No-op when not narrow.
	defer func() { a.renderRail(a.list.GetCurrentItem()) }()
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

// standbyGlyph marks a drive smartctl declined to wake. Like the ~ on an
// approximate write total, it is a glyph plus a legend rather than a silent
// substitution: the values beside it are real, just not current.
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
	// Model and device name are drive-controlled; esc() blocks markup injection.
	model := esc(rep.ModelName)
	if rep.ModelName == "" {
		model = esc(shortName(d))
	}
	main := fmt.Sprintf("%s %s", healthGlyph(rep.Overall()), model)
	// Temperature goes last: tempCell ends with a style reset that would drop
	// the secondary colour for anything after it. The standby mark therefore
	// goes first, and the health glyph is deliberately left undimmed: a
	// failing drive must not lose its colour for being asleep.
	sec := fmt.Sprintf("%s%s · %s · %s",
		a.standbyMark(d.Name), esc(shortName(d)), capacityString(rep), tempCell(rep))
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
