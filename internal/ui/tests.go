// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/smart"
)

// selfTestActions are the App-supplied callbacks to start/cancel a self-test;
// the App owns the smartctl calls, modals and refresh — the view only
// renders state and forwards intent.
type selfTestActions struct {
	run    func(testType smart.SelfTestType)
	cancel func()
	// started reports the type of the self-test smartview itself started on this
	// drive, or "" when it is unknown — a test already running when smartview
	// launched, or one started by another tool. The drive cannot tell us: ATA's
	// status string is "in progress, N% remaining" and names no type, so the
	// running view's time estimate has no other source (see remainingTime).
	started func() smart.SelfTestType
}

// testMode is the Tests view's display state.
type testMode int

const (
	modeNone testMode = iota
	modeIdle
	modeRunning
)

// testsView is the interactive Tests tab — the one tab that forwards user
// intent. Each refresh picks between the running view (progress + cancel) and
// the idle view (start Short/Long).
type testsView struct {
	*tview.Flex
	actions selfTestActions
	instr   *tview.TextView // idle-mode header/instructions
	list    *scrollList     // idle-mode test selection
	info    *scrollTextView // running-mode progress display
	mode    testMode
}

func newTestsView(r *smart.Report, actions selfTestActions) *testsView {
	v := &testsView{
		Flex:    tview.NewFlex().SetDirection(tview.FlexRow),
		actions: actions,
		instr:   tview.NewTextView().SetDynamicColors(true),
		list:    newScrollList(2), // each item shows main + secondary text
		info:    newScrollTextView(),
	}
	v.list.ShowSecondaryText(true)
	v.info.SetDynamicColors(true).SetScrollable(true)
	v.SetBorder(true).SetBorderPadding(0, 0, uiGutter, uiGutter).SetTitle(" Tests ")
	v.list.SetHighlightFullLine(true)
	styleList(v.list.List) // theme secondary text + selection (else tview leaks green)

	// 'x' cancels a running test (the global handler lets it through).
	v.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if v.mode == modeRunning && ev.Key() == tcell.KeyRune && ev.Rune() == 'x' {
			if v.actions.cancel != nil {
				v.actions.cancel()
			}
			return nil
		}
		return ev
	})

	v.refresh(r, nil)
	return v
}

// setFocused accents the Tests tab's border (the Flex itself is bordered).
func (v *testsView) setFocused(focused bool) {
	v.SetBorderColor(borderColor(focused))
}

// refresh switches between the running and idle layouts.
func (v *testsView) refresh(r *smart.Report, _ []float64) {
	if label, pct, running := r.SelfTestProgress(); running {
		v.showRunning(r, label, pct)
		return
	}
	v.showIdle(r)
}

// showRunning renders live self-test progress; the text rebuilds every poll
// but the layout is only rewired on the transition into running mode.
func (v *testsView) showRunning(r *smart.Report, label string, pct int) {
	var b strings.Builder
	// A drive-supplied label replaces the generic heading, except an ATA
	// "N% remaining" string that would duplicate the bar's own percent.
	// label is drive-controlled: escape it.
	heading := "Self-test in progress"
	if label != "" && !strings.Contains(strings.ToLower(label), "remaining") {
		heading = esc(label)
	}
	fmt.Fprintf(&b, "[::b]%s[-:-:-]\n\n", heading)
	fmt.Fprintf(&b, "%s", progressBar(pct))
	if left, ok := remainingTime(r, pct, v.startedType()); ok {
		fmt.Fprintf(&b, "%s   about %s left[-]", mutedTag(), formatTestDuration(left))
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Press %sx[-] to cancel the running test.\n", accentTag())
	fmt.Fprintf(&b, "Results appear in the %sLogs[-] tab when complete.\n", accentTag())
	v.info.SetText(b.String())

	if v.mode == modeRunning {
		return
	}
	v.mode = modeRunning
	v.Clear()
	v.AddItem(v.info, 0, 1, true)
}

// showIdle renders the test-selection list, rebuilt only on entering idle
// mode so a poll doesn't reset the highlighted row.
func (v *testsView) showIdle(r *smart.Report) {
	if v.mode == modeIdle {
		return
	}
	v.mode = modeIdle

	v.instr.SetText("No self-test running. Select a test to start " +
		"(" + accentTag() + "Enter[-]); starting one usually requires root.\n")

	v.list.Clear()
	for _, t := range []struct {
		key   smart.SelfTestType
		title string
	}{
		{key: smart.SelfTestShort, title: "Short test"},
		{key: smart.SelfTestLong, title: "Long (extended) test"},
	} {
		sec := "estimated duration unknown"
		if d, ok := r.SelfTestDuration(t.key); ok {
			sec = "~" + formatTestDuration(d)
		}
		testType := t.key // capture per iteration
		v.list.AddItem(t.title, sec, 0, func() {
			if v.actions.run != nil {
				v.actions.run(testType)
			}
		})
	}

	v.Clear()
	v.AddItem(v.instr, 3, 0, false)
	v.AddItem(v.list, 0, 1, true)
}

// barWidth is the fixed cell count of the self-test progress bar.
const barWidth = 24

// progressBar renders a fixed-width bar with the percent label after it (not
// inside, where digits replaced fill cells). Same glyph vocabulary as
// marginBar, so it survives mono.
func progressBar(pct int) string {
	pct = clampPct(pct)
	filled := pct * barWidth / 100
	return fmt.Sprintf("%s%s[-]%s%s[-]  %d%%",
		okTag(), strings.Repeat("█", filled),
		mutedTag(), strings.Repeat("░", barWidth-filled),
		pct)
}

// startedType returns the self-test type the App recorded for this drive, or ""
// when none is known; nothing in the report can supply it.
func (v *testsView) startedType() smart.SelfTestType {
	if v.actions.started == nil {
		return ""
	}
	return v.actions.started()
}

// remainingTime estimates time left from the completion percentage and the
// drive's whole-run estimate for testType. False when no duration is advertised
// (NVMe never is), the run is complete, or the type is unknown — short and
// extended differ by orders of magnitude, so a guess would be badly wrong.
func remainingTime(r *smart.Report, pct int, testType smart.SelfTestType) (time.Duration, bool) {
	total, ok := r.SelfTestDuration(testType)
	if !ok || pct >= 100 {
		return 0, false
	}
	return time.Duration(float64(total) * float64(100-pct) / 100), true
}

// formatTestDuration renders an estimated self-test runtime compactly.
func formatTestDuration(d time.Duration) string {
	m := int(d.Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	h, rem := m/60, m%60
	if rem == 0 {
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, rem)
}
