package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// swallowWhenEmpty drops key events headed for a table that holds nothing but
// its header row. tview's Table (v0.42) loops forever on Up/Down/Home/End when
// no cell is selectable, because Draw moves the selection past the last row
// and the search for a selectable cell then never reaches its stop cell.
func swallowWhenEmpty(t *tview.Table, ev *tcell.EventKey) *tcell.EventKey {
	if !hasSelectable(t) {
		return nil
	}
	return ev
}

// hasSelectable reports whether any row below the header can be selected
// (section headers in the live status table are not).
func hasSelectable(t *tview.Table) bool {
	for r := 1; r < t.GetRowCount(); r++ {
		if c := t.GetCell(r, 0); c != nil && !c.NotSelectable {
			return true
		}
	}
	return false
}

// selectOnScroll makes the mouse wheel (and a trackpad's two-finger scroll)
// move the selection of a table one row at a time, k9s-style. tview only
// scrolls the viewport, which shows nothing on a table that fits on screen.
func selectOnScroll(t *tview.Table) {
	t.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		delta := 0
		switch action {
		case tview.MouseScrollUp:
			delta = -1
		case tview.MouseScrollDown:
			delta = 1
		default:
			return action, ev
		}
		if x, y := ev.Position(); !t.InRect(x, y) {
			return action, ev
		}
		if hasSelectable(t) {
			row, _ := t.GetSelection()
			t.Select(max(1, min(t.GetRowCount()-1, row+delta)), 0)
		}
		return tview.MouseConsumed, nil
	})
}
