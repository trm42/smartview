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
		r.ATASmartData != nil || r.SATAPhyEvents != nil ||
		r.ATAPendingDefects != nil || r.ATASCTErc != nil
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

// setFocused accents the Logs tab's border when it holds keyboard focus.
func (v *logsView) setFocused(focused bool) {
	v.SetBorderColor(borderColor(focused))
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
	if r.ATASCTErc != nil {
		b.WriteString("\n")
		writeSCTErc(&b, r.ATASCTErc)
	}
	if r.SATAPhyEvents != nil {
		b.WriteString("\n")
		writePhyCounters(&b, r.SATAPhyEvents)
	}
	return b.String()
}

// writeSCTErc renders the SCT Error Recovery Control (TLER/ERC) read/write time
// limits. A configured short limit is what keeps a drive from being dropped by a
// RAID controller; "disabled" leaves the (much longer) firmware default in play.
func writeSCTErc(b *strings.Builder, e *smart.ATASCTErc) {
	fmt.Fprintln(b, "[::b]SCT Error Recovery Control (TLER)[-:-:-]")
	fmt.Fprintf(b, nestIndent+"%-6s %s\n", "Read", ercTimerString(e.Read))
	fmt.Fprintf(b, nestIndent+"%-6s %s\n", "Write", ercTimerString(e.Write))
}

// ercTimerString renders one ERC limit as seconds, or "disabled".
func ercTimerString(t *smart.ERCTimer) string {
	if t == nil || !t.Enabled {
		return "disabled (firmware default)"
	}
	return fmt.Sprintf("%.1f s", float64(t.Deciseconds)/10)
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
			fmt.Fprintf(b, nestIndent+"[yellow]%-52s %d[-]\n", esc(c.Name), c.Value)
			nonzero++
		}
	}
	if nonzero == 0 {
		fmt.Fprintf(b, nestIndent+"[green]No link errors logged[-] (%d counters)\n", len(e.Table))
	}
}

// writeErrorLog summarises the drive's logged command errors, listing the most
// recent decoded entries when the log carries any.
func writeErrorLog(b *strings.Builder, r *smart.Report) {
	fmt.Fprintln(b, "[::b]Error log[-:-:-]")
	switch {
	case r.NVMeErrorLog != nil:
		fmt.Fprintf(b, nestIndent+"%d entries (%d unread)\n", r.NVMeErrorLog.Size, sub(r.NVMeErrorLog.Size, r.NVMeErrorLog.Read))
		writeNVMeErrorEntries(b, r.NVMeErrorLog.Table)
	case r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil:
		n := r.ATAErrorLog.Extended.Count
		if n == 0 {
			fmt.Fprintln(b, nestIndent+"[green]No errors logged[-]")
		} else {
			fmt.Fprintf(b, nestIndent+"[yellow]%d error(s) logged[-]\n", n)
		}
		writeATAErrorEntries(b, r.ATAErrorLog.Extended.Table)
	default:
		fmt.Fprintln(b, nestIndent+strings.TrimPrefix(dash, ""))
	}
	writePendingDefects(b, r.ATAPendingDefects)
}

// maxErrorEntries caps how many decoded log entries the Logs tab lists, newest
// first, so a drive with a full error log does not flood the view.
const maxErrorEntries = 8

// writeNVMeErrorEntries lists decoded NVMe error-log entries (newest first).
func writeNVMeErrorEntries(b *strings.Builder, table []smart.NVMeErrorLogEntry) {
	for i, e := range table {
		if i >= maxErrorEntries {
			fmt.Fprintf(b, nestIndent+"[gray]… %d more[-]\n", len(table)-maxErrorEntries)
			break
		}
		status := e.StatusField.String
		if status == "" {
			status = fmt.Sprintf("0x%x", e.StatusField.Value)
		}
		fmt.Fprintf(b, nestIndent+"[yellow]#%d[-] %s [gray](cmd %d)[-]\n", e.ErrorCount, colorResult(esc(status)), e.CommandID)
	}
}

// writeATAErrorEntries lists decoded ATA extended-error-log entries (newest
// first), pairing each with the lifetime hour it occurred at.
func writeATAErrorEntries(b *strings.Builder, table []smart.ATAErrorLogEntry) {
	for i, e := range table {
		if i >= maxErrorEntries {
			fmt.Fprintf(b, nestIndent+"[gray]… %d more[-]\n", len(table)-maxErrorEntries)
			break
		}
		desc := esc(e.ErrorDescription)
		if e.ErrorDescription == "" {
			desc = fmt.Sprintf("error %d", e.ErrorNumber)
		}
		fmt.Fprintf(b, nestIndent+"[yellow]#%d[-] %s [gray]@ %s[-]\n",
			e.ErrorNumber, desc, humanDuration(e.LifetimeHours))
	}
}

// writePendingDefects renders the Pending Defects count: sectors awaiting
// reallocation. Nonzero is a caution worth surfacing; zero reads as healthy.
func writePendingDefects(b *strings.Builder, d *smart.ATAPendingDefects) {
	if d == nil {
		return
	}
	if d.Count == 0 {
		fmt.Fprintln(b, nestIndent+"[green]No pending defects[-]")
		return
	}
	fmt.Fprintf(b, nestIndent+"[yellow]%d sector(s) pending reallocation[-]\n", d.Count)
}

// writeSelfTestLog renders the self-test history for either protocol.
func writeSelfTestLog(b *strings.Builder, r *smart.Report) {
	fmt.Fprintln(b, "[::b]Self-test history[-:-:-]")
	switch {
	case r.NVMeSelfTestLog != nil:
		if op := r.NVMeSelfTestLog.CurrentSelfTestOperation; op != nil && op.String != "" {
			fmt.Fprintf(b, nestIndent+"Current: %s\n", esc(op.String))
		}
		if len(r.NVMeSelfTestLog.Table) == 0 {
			fmt.Fprintln(b, nestIndent+"no self-tests recorded")
			return
		}
		for _, e := range r.NVMeSelfTestLog.Table {
			fmt.Fprintf(b, nestIndent+"%-10s %-28s @ %s\n",
				esc(e.SelfTestCode.String), colorResult(e.SelfTestResult.String), humanDuration(e.PowerOnHours))
		}
	case r.ATASelfTestLog != nil && r.ATASelfTestLog.Extended != nil:
		tbl := r.ATASelfTestLog.Extended.Table
		if len(tbl) == 0 {
			fmt.Fprintln(b, nestIndent+"no self-tests recorded")
			return
		}
		for _, e := range tbl {
			fmt.Fprintf(b, nestIndent+"%-16s %-28s @ %s\n",
				esc(e.Type.String), colorResult(e.Status.String), humanDuration(e.LifetimeHours))
		}
	default:
		fmt.Fprintln(b, nestIndent+"no self-test log")
	}
}

// colorResult tints a self-test outcome string green/red by keyword. The string
// is drive-controlled, so the keyword test runs on the original but the rendered
// copy is markup-escaped (see esc) — a hostile drive can't inject colour tags.
func colorResult(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "without error"), strings.Contains(low, "completed"):
		return "[green]" + esc(s) + "[-]"
	case strings.Contains(low, "fail"), strings.Contains(low, "error"), strings.Contains(low, "aborted"):
		return "[red]" + esc(s) + "[-]"
	default:
		return esc(s)
	}
}

// sub returns a-b, floored at zero.
func sub(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}
