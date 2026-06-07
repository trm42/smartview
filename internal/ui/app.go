// SPDX-License-Identifier: GPL-3.0-or-later

// Package ui implements the tview-based terminal interface for smartview.
package ui

import (
	"context"
	"fmt"
	"os"
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

	interval  time.Duration
	refreshCh chan struct{}
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
	devices []smart.Device
	reports map[string]*smart.Report
	history map[string][]float64 // runtime temperature series per device
	inModal bool                 // true while a modal overlay is shown
}

// maxHistory bounds the runtime temperature ring buffer that backs the NVMe
// sparkline (NVMe has no on-device temperature log). 120 samples is ~10 minutes
// of history at the default 5s poll interval — enough to show a trend without
// growing unbounded.
const maxHistory = 120

// spinnerInterval is the cadence of the refresh spinner animation: fast enough
// to read as motion, slow enough not to spam the event loop with redraws.
const spinnerInterval = 120 * time.Millisecond

// New constructs the application with the given poll interval.
func New(interval time.Duration) *App {
	a := &App{
		app:       tview.NewApplication(),
		list:      tview.NewList(),
		detail:    newDetail(),
		status:    tview.NewTextView().SetDynamicColors(true),
		banner:    tview.NewTextView().SetDynamicColors(true),
		interval:  interval,
		refreshCh: make(chan struct{}, 1),
		reports:   map[string]*smart.Report{},
		history:   map[string][]float64{},
	}
	a.build()
	return a
}

// build assembles the widget tree and installs key bindings.
func (a *App) build() {
	a.list.ShowSecondaryText(true).SetHighlightFullLine(true)
	a.list.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" Drives ")
	a.list.SetChangedFunc(func(int, string, string, rune) { a.showSelected() })

	a.banner.SetBorderPadding(0, 0, uiGutter, uiGutter)
	a.status.SetBorderPadding(0, 0, uiGutter, uiGutter)
	a.status.SetText(a.statusText())

	body := tview.NewFlex().
		AddItem(a.list, 38, 0, true).
		AddItem(a.detail, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	// Full SMART access usually requires root; warn when we lack it.
	if os.Geteuid() != 0 {
		a.banner.SetText("[black:yellow] ⚠ Running without root — some drives may " +
			"report limited data; re-run with sudo for full access. [-:-]")
		root.AddItem(a.banner, 1, 0, false)
	}
	root.AddItem(body, 0, 1, true).
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
	hint := "[aqua]↑/↓[-] drive   [aqua]←/→[-] nav"
	if n := a.detail.tabCount(); n >= 2 {
		hint += fmt.Sprintf("   [aqua]1-%d[-] tab", n)
	}
	hint += "   [aqua]Tab[-] focus   [aqua]r[-] refresh   [aqua]q[-] quit"
	hint += a.contextHints()
	return hint + fmt.Sprintf("      refresh every %s", a.interval)
}

// contextHints returns the key hints specific to the focused detail tab. They
// apply only while the detail pane holds focus (the keys reach the focused tab
// body, not the drive list), so the bar stays honest about what works right now.
func (a *App) contextHints() string {
	if a.list.HasFocus() {
		return ""
	}
	switch a.detail.activeID() {
	case "attributes":
		return "   [aqua]s[-] sort   [aqua]f[-] filter"
	case "tests":
		if a.detail.testsRunning() {
			return "   [aqua]x[-] cancel test"
		}
		return "   [aqua]Enter[-] start test"
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
	listFocused := a.list.HasFocus()
	a.list.SetBorderColor(borderColor(listFocused))
	a.detail.setContentFocus(!listFocused)
}

// onKey is the global key handler.
func (a *App) onKey(ev *tcell.EventKey) *tcell.EventKey {
	if a.inModal {
		return ev // let the modal handle all input
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
		case 't':
			if a.detail.selectTabID("tests") {
				a.app.SetFocus(a.detail.content())
				a.refreshChrome()
			}
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
		return fmt.Sprintf("[gray]●[-] %s", shortName(d)), "scanning…"
	}
	model := rep.ModelName
	if model == "" {
		model = shortName(d)
	}
	main := fmt.Sprintf("%s %s", healthGlyph(rep.Overall()), model)
	sec := fmt.Sprintf("%s · %s · %s",
		shortName(d), capacityString(rep), tempString(rep))
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
		a.detail.spinner.SetText(fmt.Sprintf("[aqua]%c[-] ",
			spinnerFrames[a.spinFrame%len(spinnerFrames)]))
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
	n := d.Name
	if len(n) > 30 {
		return "…" + n[len(n)-29:]
	}
	return n
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
	a.app.SetFocus(a.detail.content())
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
			a.status.SetText("[yellow]⟳[-] Starting " + testLabel(testType) +
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
			a.status.SetText("[yellow]⟳[-] Cancelling self-test on " + shortName(dev) + "…")
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
		// Aqua (not green) activated button: the affirmative may be a destructive
		// "Cancel test", so green stays reserved for healthy/go semantics.
		SetButtonActivatedStyle(tcell.StyleDefault.
			Background(tcell.ColorAqua).Foreground(tcell.ColorBlack)).
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
