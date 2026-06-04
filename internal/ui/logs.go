// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"smartview/internal/smart"
)

// hasLogs reports whether the drive exposes any error or self-test log, used to
// decide whether the Logs tab should appear at all.
func hasLogs(r *smart.Report) bool {
	return r.ATASelfTestLog != nil || r.ATAErrorLog != nil ||
		r.NVMeSelfTestLog != nil || r.NVMeErrorLog != nil
}

// buildLogs renders the Logs tab: error-log occupancy plus self-test history.
func buildLogs(r *smart.Report) tview.Primitive {
	tv := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	tv.SetBorder(true).SetTitle(" Logs ")

	var b strings.Builder

	writeErrorLog(&b, r)
	b.WriteString("\n")
	writeSelfTestLog(&b, r)

	tv.SetText(b.String())
	return tv
}

// writeErrorLog summarises the drive's logged command errors.
func writeErrorLog(b *strings.Builder, r *smart.Report) {
	fmt.Fprintln(b, " [::b]Error log[-:-:-]")
	switch {
	case r.NVMeErrorLog != nil:
		fmt.Fprintf(b, "   %d entries (%d unread)\n", r.NVMeErrorLog.Size, sub(r.NVMeErrorLog.Size, r.NVMeErrorLog.Read))
	case r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil:
		n := r.ATAErrorLog.Extended.Count
		if n == 0 {
			fmt.Fprintln(b, "   [green]No errors logged[-]")
		} else {
			fmt.Fprintf(b, "   [yellow]%d error(s) logged[-]\n", n)
		}
	default:
		fmt.Fprintln(b, "   "+strings.TrimPrefix(dash, ""))
	}
}

// writeSelfTestLog renders the self-test history for either protocol.
func writeSelfTestLog(b *strings.Builder, r *smart.Report) {
	fmt.Fprintln(b, " [::b]Self-test history[-:-:-]")
	switch {
	case r.NVMeSelfTestLog != nil:
		if op := r.NVMeSelfTestLog.CurrentSelfTestOperation; op != nil && op.String != "" {
			fmt.Fprintf(b, "   Current: %s\n", op.String)
		}
		if len(r.NVMeSelfTestLog.Table) == 0 {
			fmt.Fprintln(b, "   no self-tests recorded")
			return
		}
		for _, e := range r.NVMeSelfTestLog.Table {
			fmt.Fprintf(b, "   %-10s %-28s @ %d h\n",
				e.SelfTestCode.String, colorResult(e.SelfTestResult.String), e.PowerOnHours)
		}
	case r.ATASelfTestLog != nil && r.ATASelfTestLog.Extended != nil:
		tbl := r.ATASelfTestLog.Extended.Table
		if len(tbl) == 0 {
			fmt.Fprintln(b, "   no self-tests recorded")
			return
		}
		for _, e := range tbl {
			fmt.Fprintf(b, "   %-16s %-28s @ %d h\n",
				e.Type.String, colorResult(e.Status.String), e.LifetimeHours)
		}
	default:
		fmt.Fprintln(b, "   no self-test log")
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
