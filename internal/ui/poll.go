// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"context"
	"time"

	"github.com/trm42/smartview/internal/smart"
)

// fetchTimeout bounds a single smartctl invocation.
const fetchTimeout = 15 * time.Second

// pollLoop refreshes every drive on a ticker (and on demand via refreshCh):
// smartctl runs off the UI goroutine, results apply through QueueUpdateDraw.
func (a *App) pollLoop(ctx context.Context) {
	a.fetchAndApply(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.fetchAndApply(ctx)
		case <-a.refreshCh:
			a.fetchAndApply(ctx)
		case d := <-a.intervalCh:
			ticker.Reset(d)
		}
	}
}

// fetchAndApply queries every device, then applies the batch on the UI goroutine.
func (a *App) fetchAndApply(ctx context.Context) {
	a.refreshing.Store(true)
	results := make(map[string]*smart.Report, len(a.devices))
	for _, d := range a.devices {
		cctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		rep, _ := smart.Info(cctx, d.Name)
		cancel()
		if rep == nil {
			continue // transient failure; keep the last-known-good report
		}
		// FARM is a separate smartctl call; failures just leave the tab hidden.
		if rep.SupportsFARM() {
			fctx, fcancel := context.WithTimeout(ctx, fetchTimeout)
			if farm, ferr := smart.FarmLog(fctx, d.Name); ferr == nil && farm != nil {
				rep.FARM = farm
			}
			fcancel()
		}
		results[d.Name] = rep
	}

	a.app.QueueUpdateDraw(func() {
		for name, rep := range results {
			a.reports[name] = rep
			a.recordTemp(name, rep)
		}
		a.populateList()
		// Refreshed even when off screen, so it is current on switch.
		a.fleet.refresh(a.devices, a.reports, a.history)
		// A tab-set change rebuilds the views, orphaning focus on the
		// destroyed primitive — restore it afterwards.
		detailFocused := a.detail.HasFocus()
		a.showSelected()
		if detailFocused {
			a.app.SetFocus(a.detail.content())
		}
		// A rebuild resets tab borders and the Tests tab may have flipped
		// idle↔running; resync focus accents and the hint bar.
		a.refreshChrome()
		a.refreshing.Store(false)
		a.renderSpinner()
	})
}

// recordTemp appends to the runtime ring buffer backing the NVMe sparkline
// (NVMe has no on-device temperature log).
func (a *App) recordTemp(name string, rep *smart.Report) {
	t, ok := rep.CurrentTemp()
	if !ok {
		return
	}
	h := append(a.history[name], float64(t))
	if len(h) > maxHistory {
		h = h[len(h)-maxHistory:]
	}
	a.history[name] = h
}
