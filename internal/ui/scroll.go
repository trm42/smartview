// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// clipScreen wraps a tcell.Screen and drops any cell write whose row falls
// outside [top, bottom). tview and tvxwidgets render exclusively through
// SetContent during Draw, so clipping it confines a primitive's output —
// backgrounds, text and bar-chart cells alike — to the scroll viewport. Size()
// and every other method delegate to the embedded real screen: tview's
// printWithStyle leans on Size() to suppress y<0 rows and clip the right edge,
// so it must report the true screen dimensions.
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

// scrollView is a borderless container that vertically scrolls a single inner
// primitive taller than its viewport. tview v0.42 has no scrollable container
// for arbitrary primitives (only TextView/Table scroll natively), so this gives
// widget-based layouts — like the FARM tab's bordered boxes and bar charts — a
// way to be reached when they overflow the terminal.
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

// setContent stores the inner primitive and its full (unclipped) height. The
// scroll offset is preserved across calls so an in-place refresh does not jump
// the view; it is clamped to the viewport in Draw, where the height is known.
func (s *scrollView) setContent(p tview.Primitive, height int) {
	s.inner = p
	s.contentHeight = height
}

// clamp constrains the scroll offset to [0, max(0, contentHeight-h)] for a
// viewport of height h.
func (s *scrollView) clamp(h int) {
	maxOffset := s.contentHeight - h
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.offset > maxOffset {
		s.offset = maxOffset
	}
	if s.offset < 0 {
		s.offset = 0
	}
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

	// Clear the viewport rows: when the offset changes, the inner Flex's
	// flexible spacer does not repaint, so stale rows would otherwise linger.
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			screen.SetContent(cx, cy, ' ', nil, tcell.StyleDefault)
		}
	}

	if s.inner != nil {
		s.inner.SetRect(x, y-s.offset, w, s.contentHeight)
		s.inner.Draw(clipScreen{Screen: screen, top: y, bottom: y + h})
	}

	// Scroll indicators at the right edge, drawn unclipped inside the viewport.
	drawScrollArrows(screen, x, y, w, h, s.offset, s.contentHeight)
}

// drawScrollArrows overlays the up/down indicators at the right edge of the rect
// (x, y, w, h) when content overflows it: ▲ when offset rows are hidden above, ▼
// when content extends below. This is the one scroll affordance shared by every
// tab — the scrollView container and the natively-scrolling TextView, Table and
// List wrappers below all route through it so the cue looks identical everywhere.
// h is both the viewport height and the placement rect, so callers pass an inner
// rect (border/gutter already removed); the arrows then sit just inside the right
// border on the top and bottom content rows. The arrows are a neutral white so
// they read as chrome rather than competing with the aqua focus/accent colour.
func drawScrollArrows(screen tcell.Screen, x, y, w, h, offset, contentHeight int) {
	if w <= 0 || h <= 0 || contentHeight <= h {
		return
	}
	arrow := tcell.StyleDefault.Foreground(tcell.ColorWhite)
	if offset > 0 {
		screen.SetContent(x+w-1, y, '▲', nil, arrow)
	}
	if offset < contentHeight-h {
		screen.SetContent(x+w-1, y+h-1, '▼', nil, arrow)
	}
}

// scrollTextView is a TextView that draws the shared scroll arrows on overflow.
// tview's TextView scrolls itself (arrow/j/k/PgUp/etc when focused); this only
// adds the off-screen cue the bare widget lacks. Used by any tab whose body is a
// single scrollable TextView (Logs, the Overview drive panel, a running test).
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

// scrollTable is a Table that draws the shared scroll arrows on overflow. The
// row offset and total row count are in the same units as the inner viewport
// height (one row each, fixed header included on both sides), so the generic
// helper's offset < contentHeight-h test lines up with the table's own scroll.
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

// scrollList is a List that draws the shared scroll arrows on overflow. Items
// are converted to row units via linesPerItem (a secondary-text list spends two
// rows per item) so the cue matches the list's actual height.
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

// InputHandler scrolls the viewport. These keys already reach the focused page
// primitive: App.onKey returns everything except Tab/Left/Right/Esc/q/r/1–5,
// exactly as the Logs tab's TextView relies on.
func (s *scrollView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return s.WrapInputHandler(func(event *tcell.EventKey, _ func(tview.Primitive)) {
		_, _, _, h := s.GetInnerRect()
		page := h - 1
		if page < 1 {
			page = 1
		}
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
