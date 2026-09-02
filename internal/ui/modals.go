// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The modal overlay stack and its three consumers (confirm, key list, error).

// pushModal shows a modal overlay, suspending the normal key-handler logic.
// It is a PAGE above the main layout, not a new root: replacing the root drew
// the modal on an empty screen, so opening the key list or Settings hid every
// widget the setting being edited applies to.
func (a *App) pushModal(m tview.Primitive) {
	a.inModal = true
	a.rootPages.AddPage(pageModal, newModalLayer(m), true, true)
}

// opaqueFlex is a Flex that paints its own ground. tview's Flex sets dontClear,
// so a box built on one draws its border and children but never clears what is
// behind them — invisible while a modal replaced the root, and the app's own
// text showing through the modal's interior once it became a layer over it.
type opaqueFlex struct {
	*tview.Flex
}

func newOpaqueFlex() *opaqueFlex {
	return &opaqueFlex{Flex: tview.NewFlex()}
}

// Draw fills the box's rect before the Flex draws its border and children.
// ColorDefault is legal here and still erases: SetContent replaces the rune
// whatever style it carries, which is what mono and terminal need.
func (f *opaqueFlex) Draw(screen tcell.Screen) {
	x, y, w, h := f.GetRect()
	ground := tcell.StyleDefault.Background(activeTheme.Background)
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			screen.SetContent(col, row, ' ', nil, ground)
		}
	}
	f.Flex.Draw(screen)
}

// modalLayer is the screen-filling page a modal sits on. It exists to stop the
// mouse: tview's Pages passes an unconsumed click down to the page underneath,
// so a click beside an open modal would land on the drive list behind it.
type modalLayer struct {
	*tview.Flex
	inner tview.Primitive
}

func newModalLayer(inner tview.Primitive) *modalLayer {
	l := &modalLayer{Flex: tview.NewFlex(), inner: inner}
	l.AddItem(inner, 0, 1, true)
	return l
}

// MouseHandler offers the event to the modal and swallows whatever it declines.
func (l *modalLayer) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		if consumed, capture := l.inner.MouseHandler()(action, event, setFocus); consumed {
			return true, capture
		}
		return true, nil
	}
}

// popModal removes the modal overlay and restores the main layout, returning
// focus to the detail content so the Tests tab stays interactive.
func (a *App) popModal() {
	a.inModal = false
	a.rootPages.RemovePage(pageModal)
	if a.fleetMode {
		a.app.SetFocus(a.fleet.table)
	} else {
		a.app.SetFocus(a.detail.content())
	}
	a.refreshChrome()
}

// styleModal grounds a modal in the active theme. tview builds one from its own
// contrast palette, and Modal.SetBackgroundColor reaches the inner form and
// frame but not the surrounding box, so both are set here.
func styleModal(m *tview.Modal) *tview.Modal {
	m.SetBackgroundColor(activeTheme.Background)
	m.Box.SetBackgroundColor(activeTheme.Background)
	m.SetBorderColor(activeTheme.Accent)
	m.SetTextColor(activeTheme.Neutral)
	m.SetButtonBackgroundColor(activeTheme.SelectionBg)
	m.SetButtonTextColor(activeTheme.SelectionFg)
	// Accent, not OK: the affirmative may be destructive, and OK stays
	// reserved for healthy/go semantics. Bold carries the focus under mono,
	// where every colour collapses to the terminal default.
	m.SetButtonActivatedStyle(tcell.StyleDefault.
		Background(activeTheme.Accent).
		Foreground(activeTheme.Inverse).
		Attributes(tcell.AttrBold))
	return m
}

// styleForm grounds a Form in the active theme. tview builds fields and
// buttons from its own contrast palette, and the ground does not reach the
// field or button colours, so each is set here. Unlike Modal, Form does not
// override SetBackgroundColor, so the one Box call covers the whole widget —
// which is why this is not styleModal's two-call dance.
func styleForm(f *tview.Form) *tview.Form {
	f.SetBackgroundColor(activeTheme.Background)
	f.SetBorderColor(activeTheme.Accent)
	f.SetTitleColor(activeTheme.Neutral)
	f.SetLabelColor(activeTheme.Neutral)
	f.SetFieldBackgroundColor(activeTheme.SelectionBg)
	f.SetFieldTextColor(activeTheme.SelectionFg)
	f.SetButtonBackgroundColor(activeTheme.SelectionBg)
	f.SetButtonTextColor(activeTheme.SelectionFg)
	// Accent, not OK, and bold so focus survives mono — same rule as styleModal.
	f.SetButtonActivatedStyle(tcell.StyleDefault.
		Background(activeTheme.Accent).
		Foreground(activeTheme.Inverse).
		Attributes(tcell.AttrBold))
	// A DropDown's open list is a separate widget; route it through
	// selectedRowStyle so it matches every other selection in the app.
	for i := range f.GetFormItemCount() {
		if dd, ok := f.GetFormItem(i).(*tview.DropDown); ok {
			// A DropDown keeps a SEPARATE style for the focused-but-closed
			// field, which SetFieldBackgroundColor does not reach. tview's
			// default is Neutral-on-SelectionBg, which inverts the palette: on
			// a light theme the focused chooser was the one dark blob on the
			// page. Inverse on Accent is what marks focus everywhere else.
			dd.SetFocusedStyle(tcell.StyleDefault.
				Background(activeTheme.Accent).
				Foreground(activeTheme.Inverse).
				Attributes(tcell.AttrBold))
			dd.SetListStyles(
				tcell.StyleDefault.
					Background(activeTheme.Background).
					Foreground(activeTheme.Neutral),
				selectedRowStyle(activeTheme.SelectionFg))
		}
	}
	return f
}

// confirm shows a two-button confirmation modal; onYes runs when the user picks
// the affirmative label.
func (a *App) confirm(text, yesLabel string, onYes func()) {
	modal := styleModal(tview.NewModal()).
		SetText(text).
		AddButtons([]string{yesLabel, "Back"}).
		SetDoneFunc(func(_ int, label string) {
			a.popModal()
			if label == yesLabel {
				onYes()
			}
		})
	a.pushModal(modal)
}

// keyBinding is one row of the '?' list; an empty key starts a new group.
type keyBinding struct{ key, what string }

// keyBindings is what the '?' modal lists, grouped by what the keys are for.
// The grouping is the point of the table: the list used to run all twenty-odd
// bindings together, and tview.Modal centred every line, so neither the key
// column nor the description column lined up with itself.
var keyBindings = []keyBinding{
	{"↑/↓", "select / scroll"},
	{"←/→", "prev / next tab"},
	{"1-9", "jump to tab"},
	{"Click", "switch tab"},
	{"Tab", "move focus"},
	{"Enter", "open drive"},
	{"Esc", "back"},
	{},
	{"j / k", "scroll content"},
	{"PgUp ^B", "page up"},
	{"PgDn ^F", "page down"},
	{"g/G Home/End", "top / bottom"},
	{"s / f", "sort / filter"},
	{},
	{"t", "Tests tab"},
	{"Enter", "start test"},
	{"x", "cancel test"},
	{"c", "fleet compare"},
	{"r / R", "refresh / wake"},
	{},
	{"+ / -", "refresh rate"},
	{"T", "colour theme"},
	{"S", "settings"},
	{"?", "this list"},
	{"q", "quit"},
}

// The two columns of a rendered binding row. The separator is two spaces and
// the key column is left-aligned: keys_test.go reads the key out of every line
// by cutting at the first double space, so both are a contract, not a style.
const (
	keyColWidth  = 12
	descColWidth = 15
	// keysColumnWidth is one rendered column plus the two-space separator.
	keysColumnWidth = keyColWidth + 2 + descColWidth
	// keysColumnGap separates the columns: the left description column runs to
	// its full width, so the two gutters alone leave the right column's keys
	// touching it.
	keysColumnGap = 2
	// keysModalWidth is two columns, each with its own gutter, the gap between
	// them, and the border.
	keysModalWidth = 2*(keysColumnWidth+2*uiGutter) + keysColumnGap + 2
)

// keysText is the '?' modal's list of record, one binding per line with the
// columns padded to a fixed width. keys_test.go parses it, so a binding that is
// not in keyBindings fails the build.
var keysText = renderKeyBindings()

func renderKeyBindings() string {
	var b strings.Builder
	for _, k := range keyBindings {
		if k.key == "" {
			b.WriteByte('\n')
			continue
		}
		fmt.Fprintf(&b, "%-*s  %-*s\n", keyColWidth, k.key, descColWidth, k.what)
	}
	return strings.TrimRight(b.String(), "\n")
}

// keysColumns splits the list into two side-by-side columns, breaking at the
// group boundary nearest the middle so a group is never torn in half.
func keysColumns() (string, string) {
	lines := strings.Split(keysText, "\n")
	best, mid := -1, len(lines)/2
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			continue
		}
		if best < 0 || abs(i-mid) < abs(best-mid) {
			best = i
		}
	}
	if best < 0 {
		return keysText, ""
	}
	return strings.Join(lines[:best], "\n"), strings.Join(lines[best+1:], "\n")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// keysColumnView renders one column, the key column accented so the eye can
// find a key without reading the descriptions.
func keysColumnView(text string) *tview.TextView {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorderPadding(0, 0, uiGutter, uiGutter)
	v.SetBackgroundColor(activeTheme.Background)
	var b strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		r := []rune(line)
		if len(r) <= keyColWidth {
			b.WriteString(accentTag() + line + "[-]")
			continue
		}
		fmt.Fprintf(&b, "%s%s[-]%s", accentTag(), string(r[:keyColWidth]), string(r[keyColWidth:]))
	}
	v.SetText(b.String())
	return v
}

// keysModal lays the bindings out in two columns inside a box of our own.
// tview.Modal was the wrong container twice over: it word-wraps at a third of
// the screen, which forced one narrow ragged column, and its height grows with
// the line count, so the list had already outgrown a 24-row terminal.
func (a *App) keysModal() tview.Primitive {
	left, right := keysColumns()
	rows := max(strings.Count(left, "\n"), strings.Count(right, "\n")) + 1

	body := tview.NewFlex().
		AddItem(keysColumnView(left), keysColumnWidth+2*uiGutter, 0, false).
		AddItem(nil, keysColumnGap, 0, false).
		AddItem(keysColumnView(right), 0, 1, false)

	close := tview.NewButton("Close")
	close.SetStyle(tcell.StyleDefault.
		Background(activeTheme.SelectionBg).
		Foreground(activeTheme.SelectionFg))
	close.SetActivatedStyle(tcell.StyleDefault.
		Background(activeTheme.Accent).
		Foreground(activeTheme.Inverse).
		Attributes(tcell.AttrBold))
	close.SetSelectedFunc(a.popModal)
	// The capture has to sit on the button, not on a wrapper: tview delivers
	// the event to the focused primitive itself, so an ancestor's capture is
	// never in the chain.
	close.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyEscape, ev.Rune() == 'q', ev.Rune() == '?':
			a.popModal()
			return nil
		}
		return ev
	})
	buttons := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(close, 9, 0, true).
		AddItem(nil, 0, 1, false)

	box := newOpaqueFlex()
	box.SetDirection(tview.FlexRow).
		AddItem(body, rows, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(buttons, 1, 0, true)
	box.SetBackgroundColor(activeTheme.Background)
	// No gutter on the wrapper: each column carries its own, and taking it
	// twice would narrow the columns into wrapping their descriptions.
	box.SetBorder(true).SetBorderPadding(0, 0, 0, 0).SetTitle(" Keys ")
	box.SetBorderColor(activeTheme.Accent)
	box.SetTitleColor(activeTheme.Neutral)
	return centeredModal(box, keysModalWidth, rows+4)
}

// notice shows a modal with one dismissing button.
func (a *App) notice(text, button string) {
	a.pushModal(styleModal(tview.NewModal()).
		SetText(text).
		AddButtons([]string{button}).
		SetDoneFunc(func(int, string) { a.popModal() }))
}

// showKeys lists every binding in a dismissable modal; the narrow hint bar
// points here, so nothing is merely hidden.
func (a *App) showKeys() {
	m := a.keysModal()
	a.pushModal(m)
	a.app.SetFocus(m)
}

// showError displays a smartctl failure in a dismissable modal; action names
// the operation that failed.
func (a *App) showError(action string, err error) {
	a.notice(fmt.Sprintf("Could not %s:\n%s", action, err), "OK")
}
