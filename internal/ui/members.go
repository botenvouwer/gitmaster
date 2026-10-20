package ui

import (
	"fmt"
	"slices"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// membersView is a checklist of all repos for one project; space toggles
// membership and every change is saved immediately.
type membersView struct {
	*filterBox
	app     *App
	project string
}

func newMembers(a *App, project string) *membersView {
	v := &membersView{app: a, project: project}
	table := tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSelectedStyle(styleSelected)
	table.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle).SetBorderColor(colorFocus)
	table.SetSelectedFunc(func(row, _ int) { v.toggle() })
	table.SetInputCapture(v.keys)
	v.filterBox = newFilterBox(a, table, v.rebuild)
	return v
}

func (v *membersView) crumbs() []string             { return []string{v.project, "repos"} }
func (v *membersView) focusTarget() tview.Primitive { return v.table }
func (v *membersView) escape() bool                 { return v.clear() }
func (v *membersView) start()                       { v.rebuild() }
func (v *membersView) update()                      { v.rebuild() }
func (v *membersView) refreshTargets() []*repoEntry { return v.app.visible(v.app.env.repos) }

func (v *membersView) hotkeys() []hotkey {
	return []hotkey{
		{"space", "toggle repo"},
		{"enter", "toggle repo"},
		{"/", "filter"},
		{"A", "show / hide archived"},
		{"esc", "done"},
	}
}

func (v *membersView) info() [][2]string {
	n := 0
	if p := v.app.env.local.Project(v.project); p != nil {
		n = len(p.Repos)
	}
	return [][2]string{
		{"Project", v.project},
		{"Selected", fmt.Sprintf("%d of %d repos", n, len(v.app.env.repos))},
	}
}

func (v *membersView) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyUp, tcell.KeyDown:
		if ev.Modifiers()&tcell.ModShift != 0 {
			v.move(dirOf(ev.Key()) * 5)
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case ' ':
			v.toggle()
			return nil
		case '/':
			v.show(true)
			return nil
		case 'A':
			v.app.toggleArchived()
			return nil
		case 'c':
			if r := v.app.env.repo(v.selectedName()); r != nil {
				v.app.copyText("path", r.path)
			}
			return nil
		case 'J':
			v.move(5)
			return nil
		case 'K':
			v.move(-5)
			return nil
		}
	}
	return swallowWhenEmpty(v.table, ev)
}

func (v *membersView) move(delta int) {
	if v.table.GetRowCount() <= 1 {
		return
	}
	row, _ := v.table.GetSelection()
	v.table.Select(max(1, min(v.table.GetRowCount()-1, row+delta)), 0)
}

func (v *membersView) selectedName() string {
	row, _ := v.table.GetSelection()
	if row < 1 {
		return ""
	}
	name, _ := v.table.GetCell(row, 0).GetReference().(string)
	return name
}

func (v *membersView) toggle() {
	name := v.selectedName()
	p := v.app.env.local.Project(v.project)
	if name == "" || p == nil {
		return
	}
	p.Toggle(name)
	v.app.saveLocal()
	v.rebuild()
	v.app.refresh()
}

func (v *membersView) rebuild() {
	e := v.app.env
	p := e.local.Project(v.project)
	if p == nil {
		return
	}
	t := v.table
	row, _ := t.GetSelection()
	t.Clear()
	for c, name := range []string{"", "NAME", "BRANCH", "STATE"} {
		cell := tview.NewTableCell(name).SetStyle(styleHeader).SetSelectable(false)
		if c == 1 {
			cell.SetExpansion(1)
		}
		t.SetCell(0, c, cell)
	}
	// All repos in the directory, then project members that no longer exist.
	names := make([]string, 0, len(e.repos))
	for _, r := range v.app.visible(e.repos) {
		names = append(names, r.name)
	}
	for _, name := range p.Repos {
		if e.repo(name) == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	names = slices.Compact(names)
	shown := 0
	for _, name := range names {
		if !v.match(name) {
			continue
		}
		shown++
		mark := tview.NewTableCell(tview.Escape("[ ]")).SetTextColor(colorMuted).SetReference(name)
		if p.Has(name) {
			mark.SetText(tview.Escape("[x]")).SetTextColor(colorClean)
		}
		t.SetCell(shown, 0, mark)
		t.SetCell(shown, 1, tview.NewTableCell(tview.Escape(name)).SetTextColor(colorValue).SetExpansion(1))
		if r := e.repo(name); r != nil {
			t.SetCell(shown, 2, tview.NewTableCell(tview.Escape(r.status.Branch)).SetTextColor(colorMuted))
			text, color := stateText(r)
			t.SetCell(shown, 3, tview.NewTableCell(text).SetTextColor(color))
		} else {
			t.SetCell(shown, 3, tview.NewTableCell("missing").SetTextColor(colorError))
		}
	}
	t.SetTitle(tview.Escape(fmt.Sprintf(" Project(%s) repos[%d/%d]%s%s ", v.project, len(p.Repos), len(names), v.app.hiddenNote(e.repos), v.suffix())))
	if shown > 0 {
		t.Select(max(1, min(shown, row)), 0)
	}
}
