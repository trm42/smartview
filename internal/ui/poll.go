// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"context"
	"time"

	"github.com/trm42/smartview/internal/smart"
)

// fetchTimeout bounds a single smartctl invocation.
const fetchTimeout = 15 * time.Second

// pollLoop refreshes every drive on a ticker and on demand; interval changes arrive on intervalCh.
func (a *App) pollLoop(ctx context.Context, interval time.Duration) {
	a.fetchAndApply(ctx, false)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.fetchAndApply(ctx, false)
		case <-a.refreshCh:
			a.fetchAndApply(ctx, false)
		case <-a.wakeCh:
			a.fetchAndApply(ctx, true)
		case d := <-a.intervalCh:
			ticker.Reset(d)
		}
	}
}

// pollResult is one drive's outcome; a standby result carries no report, so the cached one stands.
type pollResult struct {
	rep     *smart.Report
	standby bool
}

// fetchAndApply queries every device, then applies the batch on the UI goroutine; wake overrides the standby policy.
func (a *App) fetchAndApply(ctx context.Context, wake bool) {
	a.refreshing.Store(true)
	policy := smart.WakeDrive
	if !wake && a.standbyAware.Load() {
		policy = smart.SkipStandby
	}
	results := make(map[string]pollResult, len(a.devices))
	for _, d := range a.devices {
		cctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		rep, _ := smart.Info(cctx, d, policy)
		cancel()
		if rep == nil {
			continue // transient failure; keep the last-known-good report
		}
		if rep.InStandby() {
			results[d.Name] = pollResult{standby: true}
			continue
		}
		// FARM is a separate smartctl call; failures just leave the tab hidden.
		if rep.SupportsFARM() {
			fctx, fcancel := context.WithTimeout(ctx, fetchTimeout)
			if farm, ferr := smart.FarmLog(fctx, d, policy); ferr == nil && farm != nil {
				rep.FARM = farm
			}
			fcancel()
		}
		results[d.Name] = pollResult{rep: rep}
	}

	a.app.QueueUpdateDraw(func() { a.applyPoll(results) })
}

// applyPoll paints a finished batch; split out for tests. Event-loop only.
func (a *App) applyPoll(results map[string]pollResult) {
	a.applyResults(results)
	a.populateList()
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)
	// A tab-set change rebuilds the views; restore focus.
	detailFocused := a.detail.HasFocus()
	a.showSelected()
	if detailFocused {
		a.app.SetFocus(a.detail.content())
	}
	// Resync focus accents and hints after a possible rebuild.
	a.refreshChrome()
	a.refreshing.Store(false)
	a.renderSpinner()
}

// applyResults folds a poll batch into the App state; event-loop only.
func (a *App) applyResults(results map[string]pollResult) {
	now := time.Now()
	for name, res := range results {
		a.asleep[name] = res.standby
		if res.standby {
			continue // reports[name] and lastRead[name] stand
		}
		a.reports[name] = res.rep
		a.lastRead[name] = now
		a.recordTemp(name, res.rep)
	}
}

// recordTemp appends to the runtime temperature ring buffer (NVMe has no on-device log).
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
