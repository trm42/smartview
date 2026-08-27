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

// selfTestActions are the callbacks the Tests view invokes to start or cancel a
// self-test. They are supplied by the App, which owns the smartctl calls,
// confirmation/error modals and refresh scheduling — the view itself only
// renders state and forwards intent.
type selfTestActions struct {
	run    func(testType smart.SelfTestType)
	cancel func()
}

// testMode is the Tests view's display state.
type testMode int

const (
	modeNone testMode = iota
	modeIdle
	modeRunning
)

// testsView is the interactive Tests tab. Unlike the other (pure-renderer)
// tabs it forwards user intent through selfTestActions. It has two states,
// chosen on each refresh from Report.SelfTestProgress: a running view with a
// progress bar and a cancel affordance, and an idle view offering a Short or
// Long test to start.
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

	// 'x' cancels a running test. The global key handler ignores 'x', so it
	// reaches the focused view here; we act on it only while a test runs.
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

// refresh switches between the running and idle layouts based on whether a
// self-test is currently executing on the drive.
func (v *testsView) refresh(r *smart.Report, _ []float64) {
	if label, pct, running := r.SelfTestProgress(); running {
		v.showRunning(r, label, pct)
		return
	}
	v.showIdle(r)
}

// showRunning renders the live progress of an executing self-test. It rebuilds
// the text every poll (progress advances) but only rewires the layout on the
// transition into running mode.
func (v *testsView) showRunning(r *smart.Report, label string, pct int) {
	var b strings.Builder
	// The heading names the test where the drive gives us a name, rather than
	// stating "Self-test in progress" above smartctl's own "Self-test routine in
	// progress" — the same sentence twice, in two voices.
	//
	// The bar carries its own percent; ATA's raw status string duplicates it as
	// "in progress, N% remaining", so a "remaining" label is suppressed to avoid
	// showing one fact twice in contradictory forms. NVMe operation names and
	// plain ATA strings are kept. label is drive-controlled: escape it (see esc).
	heading := "Self-test in progress"
	if label != "" && !strings.Contains(strings.ToLower(label), "remaining") {
		heading = esc(label)
	}
	fmt.Fprintf(&b, "[::b]%s[-:-:-]\n\n", heading)
	fmt.Fprintf(&b, "%s", progressBar(pct))
	// "60%" of a thirty-hour extended test is arithmetic the reader should not
	// have to do; state it where the drive advertises a duration.
	if left, ok := remainingTime(r, pct); ok {
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

// showIdle renders the test-selection list. The list (and its selection) is
// rebuilt only when entering idle mode, so a live poll does not reset the
// highlighted row.
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

// progressBar renders a fixed-width bar for a 0..100 percentage, with the
// percent label AFTER the bar rather than inside it. Writing the digits into
// the bar replaced fill cells, so a run at 60% rendered as
// "██████████60%█░░░░░░░░░░" — the bar appeared broken at exactly the point the
// eye goes to read it. Done cells are █ in the theme's OK colour, remaining
// cells ░ in the muted colour: the same glyph/role vocabulary as marginBar, so
// the bar survives the mono theme where colour drops out and only the glyph
// distinction remains. The result is tview markup (dynamic colors are enabled
// on the running view).
func progressBar(pct int) string {
	pct = clampPct(pct)
	filled := pct * barWidth / 100
	return fmt.Sprintf("%s%s[-]%s%s[-]  %d%%",
		okTag(), strings.Repeat("█", filled),
		mutedTag(), strings.Repeat("░", barWidth-filled),
		pct)
}

// remainingTime estimates how long a self-test has left from its completion
// percentage and the drive's own estimate of the whole run. A long test can be
// thirty hours, so "60%" alone leaves the reader to do the arithmetic; ok is
// false when the drive advertises no estimate (NVMe never does).
func remainingTime(r *smart.Report, pct int) (time.Duration, bool) {
	total, ok := r.SelfTestDuration(smart.SelfTestLong)
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
