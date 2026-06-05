// SPDX-License-Identifier: GPL-3.0-or-later

// Package ui implements the tview-based terminal interface for smartview.
package ui

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// App is the smartview terminal application.
type App struct {
	app    *tview.Application
	list   *tview.List
	detail *detail
	status *tview.TextView
	banner *tview.TextView

	interval  time.Duration
	refreshCh chan struct{}

	// All fields below are touched only on the main (event-loop) goroutine,
	// either directly or inside QueueUpdateDraw callbacks, so need no locking.
	devices []smart.Device
	reports map[string]*smart.Report
	history map[string][]float64 // runtime temperature series per device
}

// maxHistory bounds the runtime temperature ring buffer (NVMe sparkline).
const maxHistory = 120

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
	a.list.SetBorder(true).SetTitle(" Drives ")
	a.list.SetChangedFunc(func(int, string, string, rune) { a.showSelected() })

	a.status.SetText(a.statusText())

	body := tview.NewFlex().
		AddItem(a.list, 38, 0, true).
		AddItem(a.detail, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	// Full SMART access usually requires root; warn when we lack it.
	if os.Geteuid() != 0 {
		a.banner.SetText("  [black:yellow] ⚠ Running without root — some drives may " +
			"report limited data; re-run with sudo for full access. [-:-]")
		root.AddItem(a.banner, 1, 0, false)
	}
	root.AddItem(body, 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.app.SetRoot(root, true).EnableMouse(true)
	a.app.SetInputCapture(a.onKey)
}

// statusText renders the bottom key-hint bar.
func (a *App) statusText() string {
	return fmt.Sprintf("  [aqua]↑/↓[-] drive   [aqua]←/→[-] nav   [aqua]1-3[-] tab   "+
		"[aqua]Tab[-] focus   [aqua]r[-] refresh   [aqua]Esc/q[-] quit      refresh every %s", a.interval)
}

// onKey is the global key handler.
func (a *App) onKey(ev *tcell.EventKey) *tcell.EventKey {
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
		case '1', '2', '3', '4', '5':
			a.detail.selectTab(int(r - '1'))
			a.app.SetFocus(a.detail.content())
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
}

// focusRight advances along the focus chain list → tab0 → tab1 … → tabN. From
// the list it enters the detail at the current tab; within the detail it steps
// to the next tab, stopping at the last (no wrap).
func (a *App) focusRight() {
	if a.list.HasFocus() {
		a.app.SetFocus(a.detail.content())
		return
	}
	if a.detail.stepTab(1) {
		a.app.SetFocus(a.detail.content())
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
		return
	}
	a.detail.stepTab(-1)
	a.app.SetFocus(a.detail.content())
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

// populateList rebuilds the drive list rows from cached reports.
func (a *App) populateList() {
	cur := a.list.GetCurrentItem()
	a.list.Clear()
	for _, d := range a.devices {
		main, sec := a.listRow(d)
		a.list.AddItem(main, sec, 0, nil)
	}
	if cur >= 0 && cur < len(a.devices) {
		a.list.SetCurrentItem(cur)
	}
}

// listRow renders the main/secondary text for a drive row.
func (a *App) listRow(d smart.Device) (string, string) {
	rep, ok := a.reports[d.Name]
	if !ok {
		return fmt.Sprintf("[gray]●[-] %s", shortName(d)), "  scanning…"
	}
	model := rep.ModelName
	if model == "" {
		model = shortName(d)
	}
	main := fmt.Sprintf("%s %s", healthGlyph(rep.Overall()), model)
	sec := fmt.Sprintf("  %s · %s · %s",
		shortName(d), capacityString(rep), tempString(rep))
	return main, sec
}

// Run performs the initial scan and starts the event loop and poll goroutine.
func (a *App) Run(ctx context.Context) error {
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
	return a.app.Run()
}

// shortName trims an over-long device name for display.
func shortName(d smart.Device) string {
	n := d.Name
	if len(n) > 30 {
		return "…" + n[len(n)-29:]
	}
	return n
}
