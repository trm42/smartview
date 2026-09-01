// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/trm42/smartview/internal/config"
)

// Keys go through the screen, never through the focused primitive's handler.
// That is not a style choice: an open DropDown focuses its list, but tview
// still delivers Up/Down to the *DropDown*, which forwards them on. Calling
// the focused primitive directly therefore skips the per-item capture the
// whole model hangs on, and passes tests the running app fails — which is
// exactly how the arrows-inside-a-chooser bug reached a terminal.

type modal struct {
	a      *App
	form   *tview.Form
	screen tcell.SimulationScreen
}

func openForm(t *testing.T, cfg config.Config) *modal {
	t.Helper()
	a, screen := newSimAppCfg(t, 100, 34, cfg)
	t.Cleanup(func() { setTheme(themes["dark"]) })
	form := openSettings(t, a, screen)
	if form == nil {
		t.Fatal("no settings form")
	}
	return &modal{a: a, form: form, screen: screen}
}

// send fires a key without waiting. tcell delivers screen events in order, so
// a later key whose effect can be waited on fences everything before it.
func (m *modal) send(key tcell.Key) { m.screen.InjectKey(key, 0, tcell.ModNone) }

// press sends a key and waits for the effect it is supposed to have.
func (m *modal) press(t *testing.T, key tcell.Key, what string, cond func() bool) {
	t.Helper()
	m.send(key)
	waitFor(t, m.a, what, cond)
}

// row reports the focused settings row, or -1 when focus is elsewhere (an open
// chooser's list, or a button).
func (m *modal) row() int {
	cur := m.a.app.GetFocus()
	for i := range m.form.GetFormItemCount() {
		if tview.Primitive(m.form.GetFormItem(i)) == cur {
			return i
		}
	}
	return -1
}

func (m *modal) button() int {
	cur := m.a.app.GetFocus()
	for i := range m.form.GetButtonCount() {
		if tview.Primitive(m.form.GetButton(i)) == cur {
			return i
		}
	}
	return -1
}

func (m *modal) atRow(t *testing.T, i int) { //nolint:unparam // symmetry with atButton
	t.Helper()
	waitFor(t, m.a, "focus to reach row "+itoa(i), func() bool { return m.row() == i })
}

func itoa(i int) string { return string(rune('0' + i)) }

// goToRow walks down to a row, waiting at each step.
func (m *modal) goToRow(t *testing.T, target int) {
	t.Helper()
	for i := 1; i <= target; i++ {
		m.press(t, tcell.KeyDown, "row "+itoa(i), func() bool { return m.row() == i })
	}
}

func (m *modal) chooser(t *testing.T, row int) *tview.DropDown {
	t.Helper()
	dd, ok := m.form.GetFormItem(row).(*tview.DropDown)
	if !ok {
		t.Fatalf("row %d is not a chooser", row)
	}
	return dd
}

func TestSettingsArrowsMoveBetweenRows(t *testing.T) {
	m := openForm(t, config.Default())
	m.atRow(t, 0)

	m.press(t, tcell.KeyDown, "row 1", func() bool { return m.row() == 1 })
	m.press(t, tcell.KeyUp, "row 0", func() bool { return m.row() == 0 })

	// Clamped at the top: Up changes nothing, so it needs a fence.
	m.send(tcell.KeyUp)
	m.press(t, tcell.KeyDown, "the fence", func() bool { return m.row() == 1 })
	if got := m.row(); got != 1 {
		t.Errorf("Up at the top wrapped to %d; navigation must clamp", got)
	}
}

// TestDownDoesNotOpenAClosedChooser: tview opens a closed DropDown on
// Up/Down/Home/End/PgUp/PgDn, which would swallow row navigation entirely.
func TestDownDoesNotOpenAClosedChooser(t *testing.T) {
	m := openForm(t, config.Default())
	dd := m.chooser(t, 0)

	m.press(t, tcell.KeyDown, "row 1", func() bool { return m.row() == 1 })

	if onLoop(t, m.a, func() bool { return dd.IsOpen() }) {
		t.Error("Down opened the theme chooser instead of moving a row")
	}
}

func TestRightActivatesTheFocusedRow(t *testing.T) {
	t.Run("toggles a checkbox", func(t *testing.T) {
		m := openForm(t, config.Default())
		cb := m.form.GetFormItem(2).(*tview.Checkbox)
		m.goToRow(t, 2)
		m.press(t, tcell.KeyRight, "the checkbox to flip", func() bool { return cb.IsChecked() })
	})

	t.Run("opens a chooser", func(t *testing.T) {
		m := openForm(t, config.Default())
		dd := m.chooser(t, 0)
		m.press(t, tcell.KeyRight, "the chooser to open", func() bool { return dd.IsOpen() })
	})
}

// TestArrowsInsideAnOpenChooserMoveTheList is the bug this harness exists for.
func TestArrowsInsideAnOpenChooserMoveTheList(t *testing.T) {
	m := openForm(t, config.Default())
	dd := m.chooser(t, 1) // Refresh: six options, opens on 30s
	m.goToRow(t, 1)
	m.press(t, tcell.KeyRight, "the chooser to open", func() bool { return dd.IsOpen() })
	start := onLoop(t, m.a, func() int { i, _ := dd.GetCurrentOption(); return i })

	m.send(tcell.KeyUp) // must move the list's highlight, not the row
	// Enter commits and closes, and is the fence for the Up before it.
	m.press(t, tcell.KeyEnter, "the chooser to close", func() bool { return !dd.IsOpen() })

	if end := onLoop(t, m.a, func() int { i, _ := dd.GetCurrentOption(); return i }); end == start {
		t.Errorf("Up did not move the highlight (still option %d); the open list must own the arrows", end)
	}
	if got := m.row(); got != 1 {
		t.Errorf("focus left the chooser's row (now %d)", got)
	}
}

// TestLeavingAnOpenChooser: ← backs out, and Escape does too rather than
// cancelling the modal — DropDown calls the Form's finished handler on its way
// out, which would otherwise take the whole modal with it.
func TestLeavingAnOpenChooser(t *testing.T) {
	for _, k := range []struct {
		name string
		key  tcell.Key
	}{{"Left", tcell.KeyLeft}, {"Escape", tcell.KeyEscape}} {
		t.Run(k.name, func(t *testing.T) {
			m := openForm(t, config.Default())
			dd := m.chooser(t, 0)
			m.press(t, tcell.KeyRight, "the chooser to open", func() bool { return dd.IsOpen() })

			m.press(t, k.key, "the chooser to close", func() bool { return !dd.IsOpen() })

			if !m.a.inModal {
				t.Errorf("%s closed the whole modal; it must only leave the chooser", k.name)
			}
			if got := m.row(); got != 0 {
				t.Errorf("focus landed on row %d, want the chooser's own row", got)
			}
		})
	}
}

func TestEscapeCancelsWithNoChooserOpen(t *testing.T) {
	m := openForm(t, config.Default())
	m.press(t, tcell.KeyEscape, "the modal to close", func() bool { return !m.a.inModal })
}

// TestButtonsJoinTheNavigation: Save and Cancel answer to the same arrows as
// everything else, not only Tab.
func TestButtonsJoinTheNavigation(t *testing.T) {
	m := openForm(t, config.Default())
	last := m.form.GetFormItemCount() - 1
	m.goToRow(t, last)

	m.press(t, tcell.KeyDown, "Save", func() bool { return m.button() == 0 })
	m.press(t, tcell.KeyRight, "Cancel", func() bool { return m.button() == 1 })

	// Clamped at the last button: needs a fence.
	m.send(tcell.KeyRight)
	m.press(t, tcell.KeyLeft, "Save", func() bool { return m.button() == 0 })

	m.press(t, tcell.KeyUp, "the last setting", func() bool { return m.row() == last })
}

func TestHelpLineFollowsFocus(t *testing.T) {
	m := openForm(t, config.Default())
	first := onLoop(t, m.a, func() string { return m.a.settingsHelp.GetText(true) })
	if strings.TrimSpace(first) == "" {
		t.Fatal("no help text for the row focus opens on")
	}

	m.goToRow(t, 2)
	got := onLoop(t, m.a, func() string { return m.a.settingsHelp.GetText(true) })

	if got == first {
		t.Errorf("help did not change with focus (still %q)", first)
	}
	if !strings.Contains(got, "ATA") {
		t.Errorf("standby help %q does not carry the ATA-only caveat", got)
	}
}

func TestChangedRowsAreMarked(t *testing.T) {
	m := openForm(t, config.Default())
	labels := func() []string {
		return onLoop(t, m.a, func() []string {
			out := make([]string, m.form.GetFormItemCount())
			for i := range out {
				out[i] = m.form.GetFormItem(i).GetLabel()
			}
			return out
		})
	}
	for i, l := range labels() {
		if strings.Contains(l, changedGlyph) {
			t.Errorf("row %d marked changed before any edit: %q", i, l)
		}
	}

	cb := m.form.GetFormItem(2).(*tview.Checkbox)
	m.goToRow(t, 2)
	m.press(t, tcell.KeyRight, "the checkbox to flip", func() bool { return cb.IsChecked() })

	marked := 0
	for _, l := range labels() {
		if strings.Contains(l, changedGlyph) {
			marked++
		}
	}
	if marked != 1 {
		t.Errorf("%d rows marked changed, want exactly the edited one", marked)
	}
}

func TestStatusBarAnnouncesSettings(t *testing.T) {
	a, screen := newSimApp(t, 140, 40)
	t.Cleanup(func() { setTheme(themes["dark"]) })
	runSim(t, a, screen)

	bar := onLoop(t, a, func() string { return a.status.GetText(false) })
	if !strings.Contains(bar, "settings") {
		t.Errorf("hint bar does not announce S: %q", bar)
	}
	// The bar is already wider than the 100-column breakpoint it appears at,
	// so the key must be paid for out of the summary, not appended.
	const before = 125
	if got := tview.TaggedStringWidth(bar); got > before {
		t.Errorf("hint bar grew to %d cells (was %d); S must pay for itself", got, before)
	}
}
