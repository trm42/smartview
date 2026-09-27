// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// pushModal shows m as a page above the main layout and suspends onKey.
func (a *App) pushModal(m tview.Primitive) {
	a.inModal = true
	a.rootPages.AddPage(pageModal, newModalLayer(m), true, true)
}

// opaqueFlex is a Flex that clears its rect; tview's Flex sets dontClear, so the app would show through.
type opaqueFlex struct {
	*tview.Flex
}

func newOpaqueFlex() *opaqueFlex {
	return &opaqueFlex{Flex: tview.NewFlex()}
}

// Draw fills the box's rect before the Flex draws; ColorDefault still erases.
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

// modalLayer is the screen-filling page a modal sits on; Pages would pass an unconsumed click to the page underneath.
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

// popModal removes the modal overlay and returns focus to the content.
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

// styleModal grounds a modal in the active theme; Modal.SetBackgroundColor
// misses the surrounding box, so both are set.
func styleModal(m *tview.Modal) *tview.Modal {
	m.SetBackgroundColor(activeTheme.Background)
	m.Box.SetBackgroundColor(activeTheme.Background)
	m.SetBorderColor(activeTheme.Accent)
	m.SetTextColor(activeTheme.Neutral)
	m.SetButtonBackgroundColor(activeTheme.SelectionBg)
	m.SetButtonTextColor(activeTheme.SelectionFg)
	m.SetButtonActivatedStyle(activatedStyle())
	return m
}

// activatedStyle marks a focused control: Accent, not OK, since the affirmative
// may be destructive; bold carries focus under mono.
func activatedStyle() tcell.Style {
	return tcell.StyleDefault.Background(activeTheme.Accent).Foreground(activeTheme.Inverse).Attributes(tcell.AttrBold)
}

// modalBox is the bordered, opaque FlexRow frame a custom modal sits in.
func modalBox(title string, hPad int) *opaqueFlex {
	box := newOpaqueFlex()
	box.SetDirection(tview.FlexRow)
	box.SetBackgroundColor(activeTheme.Background)
	box.SetBorder(true).SetBorderPadding(0, 0, hPad, hPad).SetTitle(title)
	box.SetBorderColor(activeTheme.Accent)
	box.SetTitleColor(activeTheme.Neutral)
	return box
}

// styleForm grounds a Form in the active theme; Form has no SetBackgroundColor override, so one Box call suffices.
func styleForm(f *tview.Form) *tview.Form {
	f.SetBackgroundColor(activeTheme.Background)
	f.SetBorderColor(activeTheme.Accent)
	f.SetTitleColor(activeTheme.Neutral)
	f.SetLabelColor(activeTheme.Neutral)
	f.SetFieldBackgroundColor(activeTheme.SelectionBg)
	f.SetFieldTextColor(activeTheme.SelectionFg)
	f.SetButtonBackgroundColor(activeTheme.SelectionBg)
	f.SetButtonTextColor(activeTheme.SelectionFg)
	f.SetButtonActivatedStyle(activatedStyle())
	for i := range f.GetFormItemCount() {
		if dd, ok := f.GetFormItem(i).(*tview.DropDown); ok {
			// A DropDown's focused-closed style is separate from the field colours; use the app's focus style.
			dd.SetFocusedStyle(activatedStyle())
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

// keyBindings is what the '?' modal lists, grouped by purpose.
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

// keys_test.go cuts the key at the first double space: keep the key column left-aligned and the separator two spaces.
const (
	keyColWidth     = 12
	descColWidth    = 15
	keysColumnWidth = keyColWidth + 2 + descColWidth
	// keysColumnGap keeps the right column's keys off the left descriptions.
	keysColumnGap = 2
	// keysModalWidth is two columns with their gutters, the gap and the border.
	keysModalWidth = 2*(keysColumnWidth+2*uiGutter) + keysColumnGap + 2
)

// keysText is the '?' modal's list of record, one fixed-width binding per line; keys_test.go parses it.
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

// keysColumns splits the list into two columns at the group boundary nearest the middle.
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

// keysColumnView renders one column with the keys accented.
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

// keysModal lays the bindings out in two fixed-width columns; tview.Modal wraps at a third of the screen.
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
	close.SetActivatedStyle(activatedStyle())
	close.SetSelectedFunc(a.popModal)
	// The capture must be on the focused button; ancestors' captures are not in the chain.
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

	// No gutter on the wrapper: each column carries its own.
	box := modalBox(" Keys ", 0)
	box.AddItem(body, rows, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(buttons, 1, 0, true)
	return centeredModal(box, keysModalWidth, rows+4)
}

// notice shows a modal with one dismissing button.
func (a *App) notice(text, button string) {
	a.pushModal(styleModal(tview.NewModal()).
		SetText(text).
		AddButtons([]string{button}).
		SetDoneFunc(func(int, string) { a.popModal() }))
}

// showKeys lists every binding in a dismissable modal.
func (a *App) showKeys() {
	m := a.keysModal()
	a.pushModal(m)
	a.app.SetFocus(m)
}

// showError displays a failure in a dismissable modal; action names the operation.
func (a *App) showError(action string, err error) {
	a.notice(fmt.Sprintf("Could not %s:\n%s", action, err), "OK")
}
