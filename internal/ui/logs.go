// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/trm42/smartview/internal/smart"
)

// hasLogs reports whether the drive exposes any error/self-test log, self-test
// timing, or SATA link diagnostics — used to decide whether the Logs tab shows.
func hasLogs(r *smart.Report) bool {
	return r.ATASelfTestLog != nil || r.ATAErrorLog != nil ||
		r.NVMeSelfTestLog != nil || r.NVMeErrorLog != nil ||
		r.ATASmartData != nil || r.SATAPhyEvents != nil
}

// logsView renders the Logs tab: error-log occupancy plus self-test history. It
// refreshes its text in place, preserving the scroll position across polls.
type logsView struct {
	*scrollTextView
}

func newLogsView(r *smart.Report) *logsView {
	v := &logsView{newScrollTextView()}
	v.SetDynamicColors(true).SetScrollable(true)
	v.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" Logs ")
	v.refresh(r, nil)
	return v
}

// refresh re-renders the log text, restoring the prior scroll offset so a poll
// does not jump the view back to the top.
func (v *logsView) refresh(r *smart.Report, _ []float64) {
	row, col := v.GetScrollOffset()
	v.SetText(buildLogsText(r))
	v.ScrollTo(row, col)
}

// buildLogsText assembles the Logs tab body.
func buildLogsText(r *smart.Report) string {
	var b strings.Builder
	writeErrorLog(&b, r)
	b.WriteString("\n")
	writeSelfTestLog(&b, r)
	if dur := selfTestDurations(r); dur != "" {
		fmt.Fprintf(&b, nestIndent+"Estimated duration: %s\n", dur)
	}
	if r.SATAPhyEvents != nil {
		b.WriteString("\n")
		writePhyCounters(&b, r.SATAPhyEvents)
	}
	return b.String()
}

// selfTestDurations renders the polling time for each self-test type, or "".
func selfTestDurations(r *smart.Report) string {
	if r.ATASmartData == nil || r.ATASmartData.SelfTest == nil || r.ATASmartData.SelfTest.PollingMinutes == nil {
		return ""
	}
	p := r.ATASmartData.SelfTest.PollingMinutes
	return fmt.Sprintf("short %s · extended %s · conveyance %s",
		humanMinutes(p.Short), humanMinutes(p.Extended), humanMinutes(p.Conveyance))
}

// writePhyCounters summarises the SATA PHY event counters: non-zero counters
// (cable/link trouble) are flagged; an all-zero log reads as healthy.
func writePhyCounters(b *strings.Builder, e *smart.SATAPhyEvents) {
	fmt.Fprintln(b, "[::b]SATA link health[-:-:-]")
	nonzero := 0
	for _, c := range e.Table {
		if c.Value > 0 {
			fmt.Fprintf(b, nestIndent+"[yellow]%-52s %d[-]\n", c.Name, c.Value)
			nonzero++
		}
	}
	if nonzero == 0 {
		fmt.Fprintf(b, nestIndent+"[green]No link errors logged[-] (%d counters)\n", len(e.Table))
	}
}

// writeErrorLog summarises the drive's logged command errors.
func writeErrorLog(b *strings.Builder, r *smart.Report) {
	fmt.Fprintln(b, "[::b]Error log[-:-:-]")
	switch {
	case r.NVMeErrorLog != nil:
		fmt.Fprintf(b, nestIndent+"%d entries (%d unread)\n", r.NVMeErrorLog.Size, sub(r.NVMeErrorLog.Size, r.NVMeErrorLog.Read))
	case r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil:
		n := r.ATAErrorLog.Extended.Count
		if n == 0 {
			fmt.Fprintln(b, nestIndent+"[green]No errors logged[-]")
		} else {
			fmt.Fprintf(b, nestIndent+"[yellow]%d error(s) logged[-]\n", n)
		}
	default:
		fmt.Fprintln(b, nestIndent+strings.TrimPrefix(dash, ""))
	}
}

// writeSelfTestLog renders the self-test history for either protocol.
func writeSelfTestLog(b *strings.Builder, r *smart.Report) {
	fmt.Fprintln(b, "[::b]Self-test history[-:-:-]")
	switch {
	case r.NVMeSelfTestLog != nil:
		if op := r.NVMeSelfTestLog.CurrentSelfTestOperation; op != nil && op.String != "" {
			fmt.Fprintf(b, nestIndent+"Current: %s\n", op.String)
		}
		if len(r.NVMeSelfTestLog.Table) == 0 {
			fmt.Fprintln(b, nestIndent+"no self-tests recorded")
			return
		}
		for _, e := range r.NVMeSelfTestLog.Table {
			fmt.Fprintf(b, nestIndent+"%-10s %-28s @ %s\n",
				e.SelfTestCode.String, colorResult(e.SelfTestResult.String), humanDuration(e.PowerOnHours))
		}
	case r.ATASelfTestLog != nil && r.ATASelfTestLog.Extended != nil:
		tbl := r.ATASelfTestLog.Extended.Table
		if len(tbl) == 0 {
			fmt.Fprintln(b, nestIndent+"no self-tests recorded")
			return
		}
		for _, e := range tbl {
			fmt.Fprintf(b, nestIndent+"%-16s %-28s @ %s\n",
				e.Type.String, colorResult(e.Status.String), humanDuration(e.LifetimeHours))
		}
	default:
		fmt.Fprintln(b, nestIndent+"no self-test log")
	}
}

// colorResult tints a self-test outcome string green/red by keyword.
func colorResult(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "without error"), strings.Contains(low, "completed"):
		return "[green]" + s + "[-]"
	case strings.Contains(low, "fail"), strings.Contains(low, "error"), strings.Contains(low, "aborted"):
		return "[red]" + s + "[-]"
	default:
		return s
	}
}

// sub returns a-b, floored at zero.
func sub(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}
