// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"context"
	"fmt"

	"github.com/trm42/smartview/internal/smart"
)

// The App's only smartctl-action path: starting and cancelling self-tests, and
// remembering which type smartview itself started.

// startedTest records the self-test type smartview asked a drive to run, plus
// whether the drive has since been seen running it (see observeSelfTest).
type startedTest struct {
	typ  smart.SelfTestType
	seen bool
}

// selfTestStarted reports the self-test type smartview started on the selected
// drive, or "" when unknown: the drive reports progress but not what is running.
func (a *App) selfTestStarted() smart.SelfTestType {
	dev, ok := a.selectedDevice()
	if !ok {
		return ""
	}
	return a.startedTests[dev.Name].typ
}

// observeSelfTest ages out the recorded type for a device. The record is dropped
// only after the drive has been seen running the test: dropping it on the first
// idle report would race the refresh that follows a start, and never dropping it
// would let a stale type label a test another tool began.
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
	a.confirm(
		fmt.Sprintf("Run %s self-test on %s?\n(Requires root; the drive stays usable.)",
			testLabel(testType), shortName(dev)),
		"Run",
		func() {
			a.status.SetText(cautionTag() + "⟳[-] Starting " + testLabel(testType) +
				" self-test on " + shortName(dev) + "…")
			a.runSmartctl(
				fmt.Sprintf("start the %s self-test on %s", testLabel(testType), shortName(dev)),
				func(ctx context.Context) error {
					return smart.RunSelfTest(ctx, dev.Name, testType)
				},
				// Recorded only on success, and only here: the type is what the
				// Tests tab times the run against, and the drive never reports it.
				func() { a.startedTests[dev.Name] = startedTest{typ: testType} })
		},
	)
}

// onSelfTestCancel confirms, then aborts the running self-test on the selected
// drive.
func (a *App) onSelfTestCancel() {
	dev, ok := a.selectedDevice()
	if !ok {
		return
	}
	a.confirm(
		fmt.Sprintf("Cancel the running self-test on %s?", shortName(dev)),
		"Cancel test",
		func() {
			a.status.SetText(cautionTag() + "⟳[-] Cancelling self-test on " + shortName(dev) + "…")
			a.runSmartctl(
				fmt.Sprintf("cancel the self-test on %s", shortName(dev)),
				func(ctx context.Context) error {
					return smart.AbortSelfTest(ctx, dev.Name)
				},
				func() { delete(a.startedTests, dev.Name) })
		},
	)
}

// runSmartctl runs a self-test control call off the event loop, then either
// surfaces the error or triggers an immediate refresh. onSuccess (optional) runs
// on the event loop only after a call the drive accepted.
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
