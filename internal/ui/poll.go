// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"context"
	"time"

	"github.com/trm42/smartview/internal/smart"
)

// fetchTimeout bounds a single smartctl invocation.
const fetchTimeout = 15 * time.Second

// pollLoop refreshes every drive on a ticker (and on demand via refreshCh),
// running smartctl off the UI goroutine and applying results through
// QueueUpdateDraw. It does the initial fetch immediately on start.
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
		// Seagate FARM is a separate smartctl call (and usually needs root);
		// only attempt it on Seagate ATA drives. Failures are swallowed — the
		// FARM tab simply stays hidden, like any other unavailable section.
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
		// detail.update refreshes each tab's data in place (preserving table
		// selection, scroll and sort/filter) for the common same-drive,
		// same-tabs case. When the set of tabs changes (e.g. the Logs tab
		// appears after a self-test completes) it rebuilds the views, which
		// orphans focus on the destroyed primitive — restore it so an active
		// detail tab stays keyboard-usable across that transition.
		detailFocused := a.detail.HasFocus()
		a.showSelected()
		if detailFocused {
			a.app.SetFocus(a.detail.content())
		}
		// A rebuild resets tab borders to default and a self-test may have flipped
		// the Tests tab idle↔running, so resync the focus accents and hint bar.
		a.refreshChrome()
		a.refreshing.Store(false)
		a.renderSpinner()
	})
}

// recordTemp appends the current temperature to the device's runtime ring
// buffer, which backs the NVMe sparkline (NVMe drives have no on-device log).
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
