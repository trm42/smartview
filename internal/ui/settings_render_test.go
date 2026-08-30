// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// screenText renders the simulation screen as lines, so an assertion can ask
// what the user would actually see rather than what a widget was told.
func screenText(screen tcell.SimulationScreen) []string {
	cells, w, h := screen.GetContents()
	lines := make([]string, h)
	for y := range h {
		var b strings.Builder
		for x := range w {
			r := cells[y*w+x].Runes
			if len(r) == 0 || r[0] == 0 {
				b.WriteRune(' ')
				continue
			}
			b.WriteRune(r[0])
		}
		lines[y] = b.String()
	}
	return lines
}

// openSettings puts the modal up on a live event loop and returns the form.
// It mirrors showSettings rather than calling it, because Form.Focus delegates
// to the focused item, so GetFocus cannot hand the Form back;
// TestSettingsModalOpensAndCancels is what covers showSettings itself.
func openSettings(t *testing.T, a *App, screen tcell.SimulationScreen) *tview.Form {
	t.Helper()
	runSim(t, a, screen)
	// runSim's cleanup quits with 'q', which a modal swallows.
	t.Cleanup(func() { onLoop(t, a, func() any { a.popModal(); return nil }) })
	return onLoop(t, a, func() *tview.Form {
		form := a.settingsForm(a.currentConfig())
		a.pushModal(centeredModal(form, settingsWidth, settingsHeight))
		a.app.SetFocus(form)
		return form
	})
}

// TestSettingsModalFitsTheSmallestTerminal renders the real modal. pushModal
// calls SetRoot(_, false), which assigns the root no rect, so a plain Flex
// draws in the corner with its left edge clipped — invisible to any assertion
// that only inspects widget state.
func TestSettingsModalFitsTheSmallestTerminal(t *testing.T) {
	a, screen := newSimApp(t, 80, 24)
	t.Cleanup(func() { setTheme(themes["dark"]) })
	openSettings(t, a, screen)
	a.app.Draw()

	lines := screenText(screen)
	joined := strings.Join(lines, "\n")

	// Every setting, and the buttons: too short a box silently drops them.
	for _, want := range append(slices.Clone(settingsRows), "Save", "Cancel") {
		if !strings.Contains(joined, want) {
			t.Errorf("%q is not on screen:\n%s", want, joined)
		}
	}
	// Centred, not stranded in the corner: the box must not touch column 0.
	for i, line := range lines {
		if strings.HasPrefix(line, "║") || strings.HasPrefix(line, "╔") {
			t.Errorf("line %d starts at column 0; the modal is not centred:\n%s", i, joined)
		}
	}
	if !strings.Contains(joined, "[ ]") {
		t.Errorf("no checkbox glyph on screen; tview's default is a bare space "+
			"that vanishes under mono:\n%s", joined)
	}
}

// TestThemeDropdownFitsOpen is the clipping risk: twelve options opening
// downward inside a non-fullscreen root.
func TestThemeDropdownFitsOpen(t *testing.T) {
	for _, h := range []int{24, 20} {
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a, screen := newSimApp(t, 80, h)
			t.Cleanup(func() { setTheme(themes["dark"]) })
			form := openSettings(t, a, screen)
			if form == nil {
				t.Fatal("settings modal did not take focus")
			}

			onLoop(t, a, func() any {
				dd := form.GetFormItem(0).(*tview.DropDown)
				a.app.SetFocus(dd)
				dd.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
					func(p tview.Primitive) { a.app.SetFocus(p) })
				return nil
			})
			a.app.Draw()

			joined := strings.Join(screenText(screen), "\n")
			// The first and last options must both be on screen, or the list
			// is clipped and some themes are unreachable.
			for _, want := range []string{themeCycle[0], themeCycle[len(themeCycle)-1]} {
				if !strings.Contains(joined, want) {
					t.Errorf("theme %q is not visible with the dropdown open at 80x%d:\n%s",
						want, h, joined)
				}
			}
		})
	}
}
