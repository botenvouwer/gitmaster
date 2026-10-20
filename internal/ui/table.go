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
	if t.GetRowCount() <= 1 {
		return nil
	}
	return ev
}
