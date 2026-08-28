// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// clipScreen drops cell writes outside [top, bottom), confining a primitive's
// Draw output to the scroll viewport. Size() must keep reporting the true
// screen dimensions — tview's printWithStyle relies on it.
type clipScreen struct {
	tcell.Screen
	top, bottom int
}

func (c clipScreen) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	if y < c.top || y >= c.bottom {
		return
	}
	c.Screen.SetContent(x, y, primary, combining, style)
}

// scrollView is a borderless container that vertically scrolls one inner
// primitive; tview has no scrollable container for arbitrary primitives, so
// widget-composed layouts (e.g. the FARM tab) need this.
type scrollView struct {
	*tview.Box
	inner         tview.Primitive
	contentHeight int
	offset        int
}

// newScrollView returns an empty, borderless scroll container. Inner content
// supplies its own borders.
func newScrollView() *scrollView {
	return &scrollView{Box: tview.NewBox()}
}

// setContent stores the inner primitive and its full height. The scroll
// offset survives so an in-place refresh doesn't jump; Draw clamps it.
func (s *scrollView) setContent(p tview.Primitive, height int) {
	s.inner = p
	s.contentHeight = height
}

// clamp constrains the offset to [0, max(0, contentHeight-h)].
func (s *scrollView) clamp(h int) {
	s.offset = min(max(s.offset, 0), max(s.contentHeight-h, 0))
}

// Draw paints the inner primitive at the current scroll offset, clipped to the
// viewport, with up/down arrows when there is more content off-screen.
func (s *scrollView) Draw(screen tcell.Screen) {
	s.DrawForSubclass(screen, s)
	x, y, w, h := s.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	s.clamp(h)

	// Clear the viewport: the inner Flex's spacer doesn't repaint on scroll,
	// so stale rows would linger.
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			screen.SetContent(cx, cy, ' ', nil, tcell.StyleDefault)
		}
	}

	if s.inner != nil {
		s.inner.SetRect(x, y-s.offset, w, s.contentHeight)
		s.inner.Draw(clipScreen{Screen: screen, top: y, bottom: y + h})
	}

	drawScrollArrows(screen, x, y, w, h, s.offset, s.contentHeight)
}

// drawScrollArrows overlays ▲/▼ at the right edge when content overflows —
// the one scroll affordance every wrapper routes through. Callers pass an
// inner rect (border/gutter already removed).
func drawScrollArrows(screen tcell.Screen, x, y, w, h, offset, contentHeight int) {
	if w <= 0 || h <= 0 || contentHeight <= h {
		return
	}
	arrow := tcell.StyleDefault.Foreground(activeTheme.ScrollArrow)
	if offset > 0 {
		screen.SetContent(x+w-1, y, '▲', nil, arrow)
	}
	if offset < contentHeight-h {
		screen.SetContent(x+w-1, y+h-1, '▼', nil, arrow)
	}
}

// scrollTextView is a TextView plus the shared scroll arrows; the widget
// already scrolls itself, this only adds the off-screen cue.
type scrollTextView struct {
	*tview.TextView
}

func newScrollTextView() *scrollTextView {
	return &scrollTextView{TextView: tview.NewTextView()}
}

func (s *scrollTextView) Draw(screen tcell.Screen) {
	s.TextView.Draw(screen)
	x, y, w, h := s.GetInnerRect()
	row, _ := s.GetScrollOffset()
	drawScrollArrows(screen, x, y, w, h, row, s.GetWrappedLineCount())
}

// scrollTable is a Table plus the shared scroll arrows; row offset/count are
// already in viewport-row units, so the overflow test lines up.
type scrollTable struct {
	*tview.Table
}

func newScrollTable() *scrollTable {
	return &scrollTable{Table: tview.NewTable()}
}

func (s *scrollTable) Draw(screen tcell.Screen) {
	s.Table.Draw(screen)
	x, y, w, h := s.GetInnerRect()
	row, _ := s.GetOffset()
	drawScrollArrows(screen, x, y, w, h, row, s.GetRowCount())
}

// scrollList is a List plus the shared scroll arrows; linesPerItem converts
// items to row units (a secondary-text list spends two rows per item).
type scrollList struct {
	*tview.List
	linesPerItem int
}

func newScrollList(linesPerItem int) *scrollList {
	return &scrollList{List: tview.NewList(), linesPerItem: linesPerItem}
}

func (s *scrollList) Draw(screen tcell.Screen) {
	s.List.Draw(screen)
	x, y, w, h := s.GetInnerRect()
	offset, _ := s.GetOffset()
	drawScrollArrows(screen, x, y, w, h, offset*s.linesPerItem, s.GetItemCount()*s.linesPerItem)
}

// InputHandler scrolls the viewport with the keys App.onKey lets through to
// the focused primitive.
func (s *scrollView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return s.WrapInputHandler(func(event *tcell.EventKey, _ func(tview.Primitive)) {
		_, _, _, h := s.GetInnerRect()
		page := max(h-1, 1)
		switch event.Key() {
		case tcell.KeyUp:
			s.offset--
		case tcell.KeyDown:
			s.offset++
		case tcell.KeyPgUp, tcell.KeyCtrlB:
			s.offset -= page
		case tcell.KeyPgDn, tcell.KeyCtrlF:
			s.offset += page
		case tcell.KeyHome:
			s.offset = 0
		case tcell.KeyEnd:
			s.offset = s.contentHeight
		case tcell.KeyRune:
			switch event.Rune() {
			case 'k':
				s.offset--
			case 'j':
				s.offset++
			case 'g':
				s.offset = 0
			case 'G':
				s.offset = s.contentHeight
			}
		}
		s.clamp(h)
	})
}

// MouseHandler scrolls on the wheel (mouse is enabled via EnableMouse in app.go).
func (s *scrollView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return s.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, _ func(tview.Primitive)) (bool, tview.Primitive) {
		x, y := event.Position()
		if !s.InRect(x, y) {
			return false, nil
		}
		_, _, _, h := s.GetInnerRect()
		switch action {
		case tview.MouseScrollUp:
			s.offset--
			s.clamp(h)
			return true, nil
		case tview.MouseScrollDown:
			s.offset++
			s.clamp(h)
			return true, nil
		}
		return false, nil
	})
}
