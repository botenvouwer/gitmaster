package ui

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// homeRef identifies a row of the home table.
type homeRef struct {
	all     bool
	project string
}

// homeView is the entry screen: all repos plus the projects of this directory.
type homeView struct {
	*tview.Table
	app *App
}

func newHome(a *App) *homeView {
	h := &homeView{Table: tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSelectedStyle(styleSelected), app: a}
	h.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle).SetBorderColor(colorFocus)
	h.SetSelectedFunc(func(row, _ int) { h.open() })
	h.SetInputCapture(h.keys)
	return h
}

func (h *homeView) crumbs() []string             { return []string{"home"} }
func (h *homeView) focusTarget() tview.Primitive { return h.Table }
func (h *homeView) escape() bool                 { return false }
func (h *homeView) start()                       { h.rebuild() }
func (h *homeView) update()                      { h.rebuild() }

func (h *homeView) hotkeys() []hotkey {
	return []hotkey{
		{"enter", "open"},
		{"n", "new project"},
		{"e", "edit project repos"},
		{"R", "rename project"},
		{"d", "delete project"},
		{"r", "rescan"},
	}
}

func (h *homeView) info() [][2]string {
	e := h.app.env
	dirty, unpushed, loaded := e.counts(e.repos)
	repos := strconv.Itoa(len(e.repos))
	if loaded < len(e.repos) {
		repos += fmt.Sprintf(" (%d loading)", len(e.repos)-loaded)
	}
	return [][2]string{
		{"Dir", shortenHome(e.dir)},
		{"Repos", repos},
		{"Projects", strconv.Itoa(len(e.projects()))},
		{"Dirty", fmt.Sprintf("%d uncommitted, %d unpushed", dirty, unpushed)},
	}
}

func (h *homeView) selected() (homeRef, bool) {
	row, _ := h.GetSelection()
	if row < 1 {
		return homeRef{}, false
	}
	ref, ok := h.GetCell(row, 0).GetReference().(homeRef)
	return ref, ok
}

func (h *homeView) selectedProject() string {
	ref, ok := h.selected()
	if !ok || ref.all {
		h.app.Flash("select a project first")
		return ""
	}
	return ref.project
}

func (h *homeView) keys(ev *tcell.EventKey) *tcell.EventKey {
	e := h.app.env
	switch ev.Key() {
	case tcell.KeyUp, tcell.KeyDown:
		if ev.Modifiers()&tcell.ModShift != 0 {
			h.move(dirOf(ev.Key()) * 5)
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'n':
			if e.local == nil {
				h.app.Flash("no repositories here to build a project from")
				return nil
			}
			h.app.prompt("New project name", "", func(name string) {
				if _, err := e.local.AddProject(name); err != nil {
					h.app.FlashErr(err)
					return
				}
				h.app.saveLocal()
				h.rebuild()
				h.app.push(newMembers(h.app, name))
			})
			return nil
		case 'e':
			if name := h.selectedProject(); name != "" {
				h.app.push(newMembers(h.app, name))
			}
			return nil
		case 'R':
			if name := h.selectedProject(); name != "" {
				h.app.prompt("Rename project", name, func(newName string) {
					if err := e.local.RenameProject(name, newName); err != nil {
						h.app.FlashErr(err)
						return
					}
					h.app.saveLocal()
					h.rebuild()
				})
			}
			return nil
		case 'd':
			if name := h.selectedProject(); name != "" {
				h.app.confirm(fmt.Sprintf("Delete project %q?\n(repositories are not touched)", name), func() {
					e.local.RemoveProject(name)
					h.app.saveLocal()
					h.rebuild()
					h.app.refresh()
				})
			}
			return nil
		case 'r':
			h.app.scan()
			return nil
		case 'J':
			h.move(5)
			return nil
		case 'K':
			h.move(-5)
			return nil
		}
	}
	return swallowWhenEmpty(h.Table, ev)
}

func (h *homeView) move(delta int) {
	if h.GetRowCount() <= 1 {
		return
	}
	row, _ := h.GetSelection()
	h.Select(max(1, min(h.GetRowCount()-1, row+delta)), 0)
}

func (h *homeView) rebuild() {
	keep, _ := h.selected()
	e := h.app.env
	h.Clear()
	for c, name := range []string{"NAME", "REPOS", "UNCOMMITTED", "UNPUSHED", "MISSING"} {
		cell := tview.NewTableCell(name).SetStyle(styleHeader).SetSelectable(false)
		if c > 0 {
			cell.SetAlign(tview.AlignRight)
		} else {
			cell.SetExpansion(1)
		}
		h.SetCell(0, c, cell)
	}
	num := func(n int, hot tcell.Color) *tview.TableCell {
		c := tview.NewTableCell(strconv.Itoa(n)).SetAlign(tview.AlignRight).SetTextColor(colorMuted)
		if n > 0 {
			c.SetTextColor(hot)
		}
		return c
	}
	row := 1
	add := func(ref homeRef, label string, repos []*repoEntry, missing int) {
		dirty, unpushed, _ := e.counts(repos)
		h.SetCell(row, 0, tview.NewTableCell(tview.Escape(label)).SetTextColor(colorValue).SetExpansion(1).SetReference(ref))
		h.SetCell(row, 1, num(len(repos), colorValue))
		h.SetCell(row, 2, num(dirty, colorDirty))
		h.SetCell(row, 3, num(unpushed, colorDirty))
		h.SetCell(row, 4, num(missing, colorError))
		if ref == keep {
			h.Select(row, 0)
		}
		row++
	}
	if len(e.repos) > 0 {
		add(homeRef{all: true}, "all repos", e.repos, 0)
	}
	for _, p := range e.projects() {
		var repos []*repoEntry
		missing := 0
		for _, name := range p.Repos {
			if r := e.repo(name); r != nil {
				repos = append(repos, r)
			} else {
				missing++
			}
		}
		add(homeRef{project: p.Name}, p.Name, repos, missing)
	}
	title := fmt.Sprintf(" Home(%s)[%s, %s] ", shortenHome(e.dir), plural(len(e.repos), "repo"), plural(len(e.projects()), "project"))
	if len(e.repos) == 0 {
		title = fmt.Sprintf(" Home(%s) no git repositories found ", shortenHome(e.dir))
	}
	h.SetTitle(tview.Escape(title))
	if r, _ := h.GetSelection(); row > 1 && (r < 1 || r >= row) {
		h.Select(1, 0)
	}
}

func (h *homeView) open() {
	ref, ok := h.selected()
	if !ok {
		return
	}
	if ref.all {
		h.app.push(newMaster(h.app, ""))
		return
	}
	h.app.push(newMaster(h.app, ref.project))
}
