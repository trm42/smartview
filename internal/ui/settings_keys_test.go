// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/config"
)

// press sends one key to whatever the application has focused, the way a real
// keystroke reaches it: through the focused primitive's own input handler,
// which is where the per-item captures live.
func press(t *testing.T, a *App, key tcell.Key) {
	t.Helper()
	onLoop(t, a, func() any {
		p := a.app.GetFocus()
		if p == nil {
			t.Fatal("nothing focused")
		}
		if h := p.InputHandler(); h != nil {
			h(tcell.NewEventKey(key, 0, tcell.ModNone), func(q tview.Primitive) { a.app.SetFocus(q) })
		}
		return nil
	})
}

// focusedRow reports which settings row currently has focus, by identity.
func focusedRow(t *testing.T, a *App, form *tview.Form) int {
	t.Helper()
	return onLoop(t, a, func() int {
		cur := a.app.GetFocus()
		for i := range form.GetFormItemCount() {
			if tview.Primitive(form.GetFormItem(i)) == cur {
				return i
			}
		}
		return -1
	})
}

func openForm(t *testing.T, cfg config.Config) (*App, *tview.Form) {
	t.Helper()
	a, screen := newSimAppCfg(t, 100, 34, cfg)
	t.Cleanup(func() { setTheme(themes["dark"]) })
	form := openSettings(t, a, screen)
	if form == nil {
		t.Fatal("no settings form")
	}
	return a, form
}

// TestSettingsArrowsMoveBetweenRows: up/down is row navigation, clamped at
// both ends the way stepTab already clamps, so the modal does not invent a
// second convention.
func TestSettingsArrowsMoveBetweenRows(t *testing.T) {
	a, form := openForm(t, config.Default())

	if got := focusedRow(t, a, form); got != 0 {
		t.Fatalf("opened on row %d, want 0", got)
	}
	press(t, a, tcell.KeyDown)
	if got := focusedRow(t, a, form); got != 1 {
		t.Errorf("after Down, row = %d, want 1", got)
	}
	press(t, a, tcell.KeyUp)
	if got := focusedRow(t, a, form); got != 0 {
		t.Errorf("after Up, row = %d, want 0", got)
	}
	press(t, a, tcell.KeyUp)
	if got := focusedRow(t, a, form); got != 0 {
		t.Errorf("Up at the top moved to %d; navigation must clamp, not wrap", got)
	}
	last := form.GetFormItemCount() - 1
	for range form.GetFormItemCount() + 2 {
		press(t, a, tcell.KeyDown)
	}
	if got := focusedRow(t, a, form); got != last {
		t.Errorf("Down at the bottom left row %d, want it clamped at %d", got, last)
	}
}

// TestDownDoesNotOpenAClosedDropdown is the regression this whole change
// exists for: tview opens a closed DropDown on Up/Down/Home/End/PgUp/PgDn, so
// without a capture the very first Down swallows row navigation.
func TestDownDoesNotOpenAClosedDropdown(t *testing.T) {
	a, form := openForm(t, config.Default())
	dd := form.GetFormItem(0).(*tview.DropDown)

	press(t, a, tcell.KeyDown)

	if onLoop(t, a, func() bool { return dd.IsOpen() }) {
		t.Error("Down opened the theme dropdown; it must move to the next setting")
	}
	if got := focusedRow(t, a, form); got != 1 {
		t.Errorf("row = %d, want 1", got)
	}
}

// TestRightActivatesTheFocusedRow: one verb for both control types.
func TestRightActivatesTheFocusedRow(t *testing.T) {
	t.Run("toggles a checkbox", func(t *testing.T) {
		a, form := openForm(t, config.Default())
		cb := form.GetFormItem(2).(*tview.Checkbox)
		if cb.IsChecked() {
			t.Fatal("fixture should start unchecked")
		}
		press(t, a, tcell.KeyDown)
		press(t, a, tcell.KeyDown)
		press(t, a, tcell.KeyRight)
		if !onLoop(t, a, func() bool { return cb.IsChecked() }) {
			t.Error("Right did not toggle the checkbox")
		}
	})

	t.Run("opens a dropdown", func(t *testing.T) {
		a, form := openForm(t, config.Default())
		dd := form.GetFormItem(0).(*tview.DropDown)
		press(t, a, tcell.KeyRight)
		if !onLoop(t, a, func() bool { return dd.IsOpen() }) {
			t.Error("Right did not open the theme dropdown")
		}
	})
}

// TestHelpLineFollowsFocus: terse guidance is the only place the ATA caveat
// can reach someone actually editing the setting.
func TestHelpLineFollowsFocus(t *testing.T) {
	a, _ := openForm(t, config.Default())

	first := onLoop(t, a, func() string { return a.settingsHelp.GetText(true) })
	if strings.TrimSpace(first) == "" {
		t.Error("no help text for the row focus opens on")
	}
	press(t, a, tcell.KeyDown)
	press(t, a, tcell.KeyDown)
	standby := onLoop(t, a, func() string { return a.settingsHelp.GetText(true) })

	if standby == first {
		t.Errorf("help did not change with focus (still %q)", first)
	}
	if !strings.Contains(standby, "ATA") {
		t.Errorf("standby help %q does not carry the ATA-only caveat", standby)
	}
}

// TestChangedRowsAreMarked: with an explicit Save there is otherwise no way to
// see what you touched.
func TestChangedRowsAreMarked(t *testing.T) {
	a, form := openForm(t, config.Default())

	labels := func() []string {
		return onLoop(t, a, func() []string {
			out := make([]string, form.GetFormItemCount())
			for i := range out {
				out[i] = form.GetFormItem(i).GetLabel()
			}
			return out
		})
	}
	for i, l := range labels() {
		if strings.Contains(l, changedGlyph) {
			t.Errorf("row %d is marked changed before anything was edited: %q", i, l)
		}
	}

	press(t, a, tcell.KeyDown)
	press(t, a, tcell.KeyDown)
	press(t, a, tcell.KeyRight) // toggle standby

	marked := 0
	for _, l := range labels() {
		if strings.Contains(l, changedGlyph) {
			marked++
		}
	}
	if marked != 1 {
		t.Errorf("%d rows marked changed, want exactly the one that was edited", marked)
	}
}

// TestStatusBarAnnouncesSettings: every other binding announces itself in the
// hint bar, so S must too — it existed only in the ? modal and the README.
func TestStatusBarAnnouncesSettings(t *testing.T) {
	a, screen := newSimApp(t, 140, 40)
	t.Cleanup(func() { setTheme(themes["dark"]) })
	runSim(t, a, screen)

	bar := onLoop(t, a, func() string { return a.status.GetText(false) })
	if !strings.Contains(bar, "settings") {
		t.Errorf("hint bar does not announce S: %q", bar)
	}

	// The wide bar is already wider than the 100-column breakpoint it appears
	// at, so the new key must not make the overflow worse: the long "refresh
	// every 30s" summary restates what "+/- rate" already teaches, and paying
	// for the key out of it keeps the bar no wider than before.
	const before = 125
	if got := tview.TaggedStringWidth(bar); got > before {
		t.Errorf("hint bar grew to %d cells (was %d); S must pay for itself", got, before)
	}
}
