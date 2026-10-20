package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestSwallowWhenEmpty(t *testing.T) {
	tbl := tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	tbl.SetCell(0, 0, tview.NewTableCell("HEADER").SetSelectable(false))
	down := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)

	if got := swallowWhenEmpty(tbl, down); got != nil {
		t.Fatal("header-only table must swallow navigation keys")
	}
	tbl.SetCell(1, 0, tview.NewTableCell("row"))
	if got := swallowWhenEmpty(tbl, down); got != down {
		t.Fatal("table with rows must pass navigation keys through")
	}
}
