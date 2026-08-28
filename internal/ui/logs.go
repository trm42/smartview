// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/trm42/smartview/internal/smart"
)

// hasLogs reports whether the drive exposes any log/link diagnostics; gates
// the Logs tab.
func hasLogs(r *smart.Report) bool {
	return r.ATASelfTestLog != nil || r.ATAErrorLog != nil ||
		r.NVMeSelfTestLog != nil || r.NVMeErrorLog != nil ||
		r.ATASmartData != nil || r.SATAPhyEvents != nil ||
		r.ATAPendingDefects != nil || r.ATASCTErc != nil
}

// logsView renders the Logs tab, refreshing in place so the scroll position
// survives polls.
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

// refresh re-renders the log text, restoring the prior scroll offset.
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

// writeSCTErc renders the SCT Error Recovery Control (TLER/ERC) time limits.
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

// writePhyCounters summarises the SATA PHY event counters.
func writePhyCounters(b *strings.Builder, e *smart.SATAPhyEvents) {
	fmt.Fprintln(b, "[::b]SATA link health[-:-:-]")
	nonzero := 0
	for _, c := range e.Table {
		if c.Value == 0 {
			continue
		}
		nonzero++
		// Only counters that indicate a bad cable/marginal link are graded; a
		// couple of COMRESETs is a normal power-up.
		tag := mutedTag()
		if phyCounterConcerning(c.Name) {
			tag = cautionTag()
		}
		fmt.Fprintf(b, nestIndent+"%s%6d[-] %s%s[-]\n", tag, c.Value, mutedTag(), esc(c.Name))
	}
	if nonzero == 0 {
		fmt.Fprintf(b, nestIndent+"No link events logged (%d counters)\n", len(e.Table))
	}
}

// phyCounterConcerning reports whether a PHY counter indicates a link fault:
// CRC/decode errors point at a cable, resets accumulate on any healthy link.
func phyCounterConcerning(name string) bool {
	low := strings.ToLower(name)
	for _, kw := range []string{"crc", "non-crc", "decode", "disparity", "handshake"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// writeErrorLog summarises the drive's logged command errors with the most
// recent decoded entries.
func writeErrorLog(b *strings.Builder, r *smart.Report) {
	fmt.Fprintln(b, "[::b]Error log[-:-:-]")
	switch {
	case r.NVMeErrorLog != nil:
		writeNVMeErrorCount(b, r.NVMeErrorLog)
		writeNVMeErrorEntries(b, r.NVMeErrorLog.Table)
	case r.ATAErrorLog != nil && r.ATAErrorLog.Extended != nil:
		n := r.ATAErrorLog.Extended.Count
		if n == 0 {
			fmt.Fprintln(b, nestIndent+"No errors logged")
		} else {
			fmt.Fprintf(b, nestIndent+cautionTag()+"%s logged[-]\n", plural(n, "error", "errors"))
		}
		writeATAErrorEntries(b, r, r.ATAErrorLog.Extended.Table)
	default:
		fmt.Fprintln(b, nestIndent+dash)
	}
	writePendingDefects(b, r.ATAPendingDefects)
}

// writeNVMeErrorCount states the logged-error count — len(Table), never the
// log's slot capacity (Size is 256 on a drive with three errors).
func writeNVMeErrorCount(b *strings.Builder, l *smart.NVMeErrorLog) {
	n := len(l.Table)
	if n == 0 {
		fmt.Fprintln(b, nestIndent+"No errors logged")
		return
	}
	fmt.Fprintf(b, nestIndent+cautionTag()+"%s logged[-]", plural(n, "error", "errors"))
	if l.Unread > 0 {
		fmt.Fprintf(b, mutedTag()+" (%d not read back)[-]", l.Unread)
	}
	b.WriteByte('\n')
}

// maxErrorEntries caps the decoded entries listed, newest first.
const maxErrorEntries = 8

// writeNVMeErrorEntries lists decoded NVMe error-log entries (newest first).
func writeNVMeErrorEntries(b *strings.Builder, table []smart.NVMeErrorLogEntry) {
	for i, e := range table {
		if i >= maxErrorEntries {
			fmt.Fprintf(b, nestIndent+mutedTag()+"… %d more[-]\n", len(table)-maxErrorEntries)
			break
		}
		status := e.StatusField.String
		if status == "" {
			status = fmt.Sprintf("0x%x", e.StatusField.Value)
		}
		fmt.Fprintf(b, nestIndent+cautionTag()+"#%d[-] %s %s(cmd %d)[-]\n", e.ErrorCount, colorResult(status), mutedTag(), e.CommandID)
	}
}

// writeATAErrorEntries lists decoded ATA error-log entries (newest first)
// with the lifetime hour each occurred at.
func writeATAErrorEntries(b *strings.Builder, r *smart.Report, table []smart.ATAErrorLogEntry) {
	for i, e := range table {
		if i >= maxErrorEntries {
			fmt.Fprintf(b, nestIndent+mutedTag()+"… %d more[-]\n", len(table)-maxErrorEntries)
			break
		}
		desc := esc(tidyErrorDescription(e.ErrorDescription))
		if e.ErrorDescription == "" {
			desc = fmt.Sprintf("error %d", e.ErrorNumber)
		}
		fmt.Fprintf(b, nestIndent+cautionTag()+"#%d[-] %s %s%s[-]\n",
			e.ErrorNumber, desc, mutedTag(), driveAge(r, e.LifetimeHours))
	}
}

// driveAge renders the drive's age at an event and how long ago it was; the
// elapsed figure is omitted, not guessed, when current hours are unreported.
func driveAge(r *smart.Report, hours int) string {
	now, ok := r.PowerOnHours()
	if !ok || now < hours {
		return fmt.Sprintf("at %s", humanDuration(hours))
	}
	if ago := now - hours; ago < 24 {
		return fmt.Sprintf("at %s · %d h ago", humanDuration(hours), ago)
	} else {
		return fmt.Sprintf("at %s · %s ago", humanDuration(hours), humanDuration(ago))
	}
}

// tidyErrorDescription drops smartctl's "Error: " prefix and keeps only the
// decimal LBA of its hex/decimal pair.
func tidyErrorDescription(s string) string {
	s = strings.TrimPrefix(s, "Error: ")
	const marker = "LBA = "
	if i := strings.Index(s, marker); i >= 0 {
		// "LBA = 0x0011a034 = 1155124" -> "LBA 1155124".
		rest := s[i+len(marker):]
		if k := strings.LastIndex(rest, " = "); k >= 0 {
			s = s[:i] + "LBA " + rest[k+3:]
		}
	}
	return s
}

// writePendingDefects renders the Pending Defects count; nonzero is a caution.
func writePendingDefects(b *strings.Builder, d *smart.ATAPendingDefects) {
	if d == nil {
		return
	}
	if d.Count == 0 {
		fmt.Fprintln(b, nestIndent+"No pending defects")
		return
	}
	fmt.Fprintf(b, nestIndent+cautionTag()+"%s pending reallocation[-]\n",
		plural(d.Count, "sector", "sectors"))
}

// writeSelfTestSummary states run count, failures and the newest run, so the
// reader needn't scan the near-identical rows below.
func writeSelfTestSummary(b *strings.Builder, r *smart.Report, tbl []smart.ATASelfTestEntry) {
	failed := 0
	for _, e := range tbl {
		if !selfTestPassed(e.Status.String) {
			failed++
		}
	}
	verdict := okTag() + "all passed[-]"
	if failed > 0 {
		verdict = fmt.Sprintf("%s%s failed[-]", failingTag(), plural(failed, "run", "runs"))
	}
	fmt.Fprintf(b, nestIndent+"%s, %s %s· newest %s[-]\n",
		plural(len(tbl), "run", "runs"), verdict,
		mutedTag(), driveAge(r, tbl[0].LifetimeHours))
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
		writeSelfTestSummary(b, r, tbl)
		for _, e := range tbl {
			fmt.Fprintf(b, nestIndent+"%-16s %-28s %s\n",
				esc(e.Type.String), colorResult(e.Status.String),
				mutedTag()+driveAge(r, e.LifetimeHours)+"[-]")
		}
	default:
		fmt.Fprintln(b, nestIndent+"no self-test log")
	}
}

// selfTestPassed is the single keyword test shared with colorResult, so the
// summary and row colours can't disagree. "Completed" alone is not a pass
// (failures read "Completed: read failure"); "without error" is checked first
// since it contains "error" itself.
func selfTestPassed(s string) bool {
	low := strings.ToLower(s)
	if strings.Contains(low, "without error") {
		return true
	}
	for _, kw := range []string{"fail", "error", "aborted", "interrupted", "fatal", "unknown"} {
		if strings.Contains(low, kw) {
			return false
		}
	}
	return strings.Contains(low, "completed")
}

// colorResult tints a self-test outcome by keyword; the test runs on the
// original but the rendered copy is markup-escaped (drive-controlled string).
func colorResult(s string) string {
	low := strings.ToLower(s)
	switch {
	case selfTestPassed(s):
		return okTag() + esc(s) + "[-]"
	case strings.Contains(low, "fail"), strings.Contains(low, "error"),
		strings.Contains(low, "aborted"), strings.Contains(low, "interrupted"):
		return failingTag() + esc(s) + "[-]"
	default:
		return esc(s)
	}
}

// plural renders a count with the right noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
