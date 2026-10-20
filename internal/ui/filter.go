package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// filterBox wraps a table with a k9s-style "/" text filter row above it.
// The row is hidden until "/" is pressed; the filter text stays active after
// Enter and is shown in the table title. Esc clears it.
type filterBox struct {
	*tview.Flex
	app      *App
	table    *tview.Table
	input    *tview.InputField
	text     string
	quiet    bool
	onChange func()
}

func newFilterBox(a *App, table *tview.Table, onChange func()) *filterBox {
	f := &filterBox{Flex: tview.NewFlex().SetDirection(tview.FlexRow), app: a, table: table, onChange: onChange}
	selectOnScroll(table)
	f.input = tview.NewInputField().SetLabel(" /> ").SetLabelColor(colorKey).
		SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(colorValue)
	f.input.SetChangedFunc(func(text string) {
		f.text = text
		if !f.quiet {
			f.onChange()
		}
	})
	f.input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc {
			f.input.SetText("")
		}
		f.show(false)
	})
	f.AddItem(f.input, 0, 0, false).AddItem(table, 0, 1, true)
	return f
}

// show opens the filter row for typing, or hides it again (keeping the text).
func (f *filterBox) show(on bool) {
	if on {
		f.ResizeItem(f.input, 1, 0)
		f.app.SetFocus(f.input)
		return
	}
	f.ResizeItem(f.input, 0, 0)
	f.app.SetFocus(f.table)
	f.onChange()
}

// clear drops the filter and reports whether there was one.
func (f *filterBox) clear() bool {
	if f.text == "" {
		return false
	}
	f.input.SetText("")
	return true
}

// reset drops the filter without re-rendering (the caller is about to).
func (f *filterBox) reset() {
	f.quiet = true
	f.input.SetText("")
	f.quiet = false
	f.text = ""
}

// match reports whether any field contains the filter text (case-insensitive).
func (f *filterBox) match(fields ...string) bool {
	if f.text == "" {
		return true
	}
	q := strings.ToLower(f.text)
	for _, s := range fields {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

// suffix is appended to a table title to show the active filter.
func (f *filterBox) suffix() string {
	if f.text == "" {
		return ""
	}
	return " /" + f.text
}
