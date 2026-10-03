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

// pollResult is one drive's outcome; a standby or failed result carries no report, so the cached one stands.
type pollResult struct {
	rep     *smart.Report
	standby bool
	failed  bool
}

// outcome classifies one fetch; a nil report is a failed read.
func outcome(rep *smart.Report) pollResult {
	switch {
	case rep == nil:
		return pollResult{failed: true}
	case rep.InStandby():
		return pollResult{standby: true}
	}
	return pollResult{rep: rep}
}

// fetchers are the two smartctl reads a poll makes per drive.
type fetchers struct {
	info func(context.Context, smart.Device, smart.PowerPolicy) (*smart.Report, error)
	farm func(context.Context, smart.Device, smart.PowerPolicy) (*smart.FARM, error)
}

// smartctlFetchers reads real drives.
var smartctlFetchers = fetchers{info: smart.Info, farm: smart.FarmLog}

// fetchAndApply queries every device, then applies the batch on the UI goroutine; wake overrides the standby policy.
func (a *App) fetchAndApply(ctx context.Context, wake bool) {
	a.refreshing.Store(true)
	results := a.fetchAll(ctx, wake, smartctlFetchers)
	a.app.QueueUpdateDraw(func() { a.applyPoll(results) })
}

// fetchAll reads every device; each gets a result, so a failed read is reported rather than dropped.
func (a *App) fetchAll(ctx context.Context, wake bool, f fetchers) map[string]pollResult {
	policy := smart.WakeDrive
	if !wake && a.standbyAware.Load() {
		policy = smart.SkipStandby
	}
	results := make(map[string]pollResult, len(a.devices))
	for _, d := range a.devices {
		results[d.Name] = fetchDevice(ctx, d, policy, f)
	}
	return results
}

// fetchDevice reads one drive; both calls take the same policy, since either one wakes it.
func fetchDevice(ctx context.Context, d smart.Device, policy smart.PowerPolicy, f fetchers) pollResult {
	cctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	rep, _ := f.info(cctx, d, policy)
	cancel()
	res := outcome(rep)
	if res.rep == nil || !rep.SupportsFARM() {
		return res
	}
	// FARM is a separate smartctl call; failures just leave the tab hidden.
	fctx, fcancel := context.WithTimeout(ctx, fetchTimeout)
	defer fcancel()
	if farm, ferr := f.farm(fctx, d, policy); ferr == nil && farm != nil {
		rep.FARM = farm
	}
	return res
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
		a.unreadable[name] = false
		if res.standby {
			continue // reports[name] and lastRead[name] stand
		}
		// A failed open still prints an envelope; it must not replace a real reading.
		prev := a.reports[name]
		hadReading := prev != nil && prev.HasHealth()
		if res.failed || (!res.rep.HasHealth() && hadReading) {
			a.unreadable[name] = hadReading
			continue
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
