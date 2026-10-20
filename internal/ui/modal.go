package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const modalPage = "modal"

func (a *App) showModal(p tview.Primitive, width, height int) {
	a.closeModal()
	var box tview.Primitive = p
	if width > 0 {
		box = tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 0, true).
				AddItem(nil, 0, 1, false), width, 0, true).
			AddItem(nil, 0, 1, false)
	}
	a.modal = true
	a.prevFocus = a.GetFocus()
	a.pages.AddPage(modalPage, box, true, true)
	a.SetFocus(p)
}

func (a *App) closeModal() {
	if !a.modal {
		return
	}
	a.modal = false
	a.pages.RemovePage(modalPage)
	if a.prevFocus != nil {
		a.SetFocus(a.prevFocus)
	}
}

// prompt asks for a line of text; done runs only for a non-empty answer.
func (a *App) prompt(title, initial string, done func(string)) {
	in := tview.NewInputField().SetLabel(" ").SetText(initial).
		SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(colorValue)
	in.SetBorder(true).SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft).
		SetTitleColor(colorTitle).SetBorderColor(colorFocus)
	in.SetDoneFunc(func(key tcell.Key) {
		text := strings.TrimSpace(in.GetText())
		a.closeModal()
		if key == tcell.KeyEnter && text != "" {
			done(text)
		}
	})
	a.showModal(in, 60, 3)
}

// pick lets the user choose one of items; done receives its index.
func (a *App) pick(title string, items []string, done func(int)) {
	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).
		SetMainTextColor(colorValue).
		SetSelectedBackgroundColor(tcell.ColorAqua).SetSelectedTextColor(tcell.ColorBlack)
	for _, it := range items {
		list.AddItem(tview.Escape(it), "", 0, nil)
	}
	list.SetSelectedFunc(func(i int, _, _ string, _ rune) {
		a.closeModal()
		done(i)
	})
	list.SetBorder(true).SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft).
		SetTitleColor(colorTitle).SetBorderColor(colorFocus)
	a.showModal(list, 60, min(len(items)+2, 20))
}

// confirm asks a yes/no question; yes runs only on "Yes".
func (a *App) confirm(text string, yes func()) {
	m := tview.NewModal().SetText(text).AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(_ int, label string) {
			a.closeModal()
			if label == "Yes" {
				yes()
			}
		})
	a.showModal(m, 0, 0)
}
