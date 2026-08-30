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
// The cadence is a parameter because a.interval is written on the event loop;
// runtime changes arrive on intervalCh.
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
			a.fetchAndApply(ctx, false) // 'r' respects standby
		case <-a.wakeCh:
			a.fetchAndApply(ctx, true) // 'R' wakes spun-down drives
		case d := <-a.intervalCh:
			ticker.Reset(d)
		}
	}
}

// pollResult is one drive's outcome. A standby result carries no report: the
// cached one stands, because an empty envelope must never overwrite real data.
type pollResult struct {
	rep     *smart.Report
	standby bool
}

// fetchAndApply queries every device, then applies the batch on the UI
// goroutine. wake overrides the standby policy for a user-initiated read.
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
			// The envelope carries no drive data, so there is nothing to
			// attach a FARM log to and nothing worth storing.
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

// applyPoll paints a finished poll batch. Split out of the QueueUpdateDraw
// closure so a test can drive it without a smartctl stub; event-loop only.
func (a *App) applyPoll(results map[string]pollResult) {
	a.applyResults(results)
	// The rows carry the health glyph, model, capacity and temperature, so
	// they go stale the moment a report lands; without this the list sits on
	// "scanning…" for the whole session.
	a.populateList()
	// Refreshed even when off screen, so it is current on switch.
	a.fleet.refresh(a.devices, a.reports, a.history, a.asleep)
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
}

// applyResults folds a poll batch into the App state. Event-loop only, like
// every map it touches.
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
