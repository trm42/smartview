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
		v.showRunning(label, pct)
		return
	}
	v.showIdle(r)
}

// showRunning renders the live progress of an executing self-test. It rebuilds
// the text every poll (progress advances) but only rewires the layout on the
// transition into running mode.
func (v *testsView) showRunning(label string, pct int) {
	var b strings.Builder
	b.WriteString("[::b]Self-test in progress[-:-:-]\n\n")
	// The bar carries its own percent; ATA's raw status string duplicates it as
	// "in progress, N% remaining", so suppress any "remaining" label to avoid
	// showing the same fact twice in contradictory forms. NVMe operation names
	// and plain ATA strings (no "remaining") are kept.
	if label != "" && !strings.Contains(strings.ToLower(label), "remaining") {
		// label is smartctl's drive-controlled status string; escape markup (see esc).
		fmt.Fprintf(&b, "%s\n\n", esc(label))
	}
	fmt.Fprintf(&b, "%s\n\n", progressBar(pct))
	b.WriteString("Press [aqua]x[-] to cancel the running test.\n")
	b.WriteString("Results appear in the [aqua]Logs[-] tab when complete.\n")
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
		"([aqua]Enter[-]); starting one usually requires root.\n")

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

// progressBar renders a fixed-width bar for a 0..100 percentage with the
// percent label centered inside it. Done cells are green, remaining cells are a
// dim grey; the split shows progress without a separate number elsewhere. The
// result is tview markup (dynamic colors are enabled on the running view).
func progressBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * barWidth / 100
	label := fmt.Sprintf("%d%%", pct) // ASCII, so byte len == rune count
	start := (barWidth - len(label)) / 2

	var b strings.Builder
	for i := 0; i < barWidth; i++ {
		if i < filled {
			b.WriteString("[black:green]")
		} else {
			b.WriteString("[white:#3a3a3a]")
		}
		if i >= start && i < start+len(label) {
			b.WriteByte(label[i-start])
		} else {
			b.WriteByte(' ')
		}
	}
	b.WriteString("[-:-]")
	return b.String()
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
