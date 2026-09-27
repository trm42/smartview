// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"context"
	"fmt"

	"github.com/trm42/smartview/internal/smart"
)

// startedTest records the self-test type smartview started and whether the drive was since seen running it.
type startedTest struct {
	typ  smart.SelfTestType
	seen bool
}

// selfTestStarted reports the self-test type smartview started on the selected drive, or "".
func (a *App) selfTestStarted() smart.SelfTestType {
	dev, ok := a.selectedDevice()
	if !ok {
		return ""
	}
	return a.startedTests[dev.Name].typ
}

// observeSelfTest drops a recorded type only after the drive was seen running it, so the post-start refresh cannot race it.
func (a *App) observeSelfTest(name string, rep *smart.Report) {
	st, ok := a.startedTests[name]
	if !ok {
		return
	}
	if _, _, running := rep.SelfTestProgress(); running {
		if !st.seen {
			st.seen = true
			a.startedTests[name] = st
		}
		return
	}
	if st.seen {
		delete(a.startedTests, name)
	}
}

// testLabel renders a friendly self-test name for prompts.
func testLabel(testType smart.SelfTestType) string {
	if testType == smart.SelfTestLong {
		return "long (extended)"
	}
	return string(testType)
}

// onSelfTestRun confirms, then starts a self-test on the selected drive.
func (a *App) onSelfTestRun(testType smart.SelfTestType) {
	dev, ok := a.selectedDevice()
	if !ok {
		return
	}
	name, label := shortName(dev), testLabel(testType)
	a.confirm(
		fmt.Sprintf("Run %s self-test on %s?\n(Requires root; the drive stays usable.)", label, name),
		"Run",
		func() {
			a.status.SetText(cautionTag() + "⟳[-] Starting " + label + " self-test on " + name + "…")
			a.runSmartctl(
				fmt.Sprintf("start the %s self-test on %s", label, name),
				func(ctx context.Context) error {
					return smart.RunSelfTest(ctx, dev.Name, testType)
				},
				// Recorded only on success.
				func() { a.startedTests[dev.Name] = startedTest{typ: testType} })
		},
	)
}

// onSelfTestCancel confirms, then aborts the running self-test on the selected drive.
func (a *App) onSelfTestCancel() {
	dev, ok := a.selectedDevice()
	if !ok {
		return
	}
	name := shortName(dev)
	a.confirm(
		fmt.Sprintf("Cancel the running self-test on %s?", name),
		"Cancel test",
		func() {
			a.status.SetText(cautionTag() + "⟳[-] Cancelling self-test on " + name + "…")
			a.runSmartctl(
				fmt.Sprintf("cancel the self-test on %s", name),
				func(ctx context.Context) error {
					return smart.AbortSelfTest(ctx, dev.Name)
				},
				func() { delete(a.startedTests, dev.Name) })
		},
	)
}

// runSmartctl runs fn off the event loop, then shows the error or refreshes; onSuccess runs on the event loop.
func (a *App) runSmartctl(action string, fn func(context.Context) error, onSuccess func()) {
	parent := a.rootCtx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, fetchTimeout)
		defer cancel()
		err := fn(ctx)
		a.app.QueueUpdateDraw(func() {
			a.status.SetText(a.statusText())
			if err != nil {
				a.showError(action, err)
				return
			}
			if onSuccess != nil {
				onSuccess()
			}
			a.triggerRefresh()
		})
	}()
}
