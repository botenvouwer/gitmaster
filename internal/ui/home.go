package ui

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// homeRef identifies a row of the home table.
type homeRef struct {
	live    bool
	all     bool
	project string
}

// homeView is the entry screen: all repos plus the projects of this directory.
type homeView struct {
	*filterBox
	app *App
}

func newHome(a *App) *homeView {
	h := &homeView{app: a}
	table := tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSelectedStyle(styleSelected)
	table.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle).SetBorderColor(colorFocus)
	table.SetSelectedFunc(func(row, _ int) { h.open() })
	table.SetInputCapture(h.keys)
	h.filterBox = newFilterBox(a, table, h.rebuild)
	return h
}

func (h *homeView) crumbs() []string             { return []string{"home"} }
func (h *homeView) focusTarget() tview.Primitive { return h.table }
func (h *homeView) escape() bool                 { return h.clear() }
func (h *homeView) start()                       { h.rebuild() }
func (h *homeView) update()                      { h.rebuild() }
func (h *homeView) refreshTargets() []*repoEntry { return h.app.visible(h.app.env.repos) }

func (h *homeView) hotkeys() []hotkey {
	return []hotkey{
		{"enter", "open"},
		{"/", "filter"},
		{"L", "live status"},
		{"n", "new project"},
		{"e", "edit project repos"},
		{"R", "rename project"},
		{"d", "delete project"},
		{"A", "show / hide archived"},
		{"r", "rescan"},
	}
}

func (h *homeView) info() [][2]string {
	e := h.app.env
	all := h.app.visible(e.repos)
	dirty, unpushed, loaded := e.counts(all)
	repos := strconv.Itoa(len(all))
	if loaded < len(all) {
		repos += fmt.Sprintf(" (%d loading)", len(all)-loaded)
	}
	return [][2]string{
		{"Dir", shortenHome(e.dir)},
		{"Repos", repos},
		{"Projects", strconv.Itoa(len(e.projects()))},
		{"Dirty", fmt.Sprintf("%d uncommitted, %d unpushed", dirty, unpushed)},
	}
}

func (h *homeView) selected() (homeRef, bool) {
	row, _ := h.table.GetSelection()
	if row < 1 {
		return homeRef{}, false
	}
	ref, ok := h.table.GetCell(row, 0).GetReference().(homeRef)
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
		case '/':
			h.show(true)
			return nil
		case 'L':
			if len(e.repos) > 0 {
				h.table.Select(1, 0)
				h.open()
			}
			return nil
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
		case 'A':
			h.app.toggleArchived()
			return nil
		case 'r':
			h.app.scan(true)
			return nil
		case 'J':
			h.move(5)
			return nil
		case 'K':
			h.move(-5)
			return nil
		}
	}
	return swallowWhenEmpty(h.table, ev)
}

func (h *homeView) move(delta int) {
	if h.table.GetRowCount() <= 1 {
		return
	}
	row, _ := h.table.GetSelection()
	h.table.Select(max(1, min(h.table.GetRowCount()-1, row+delta)), 0)
}

func (h *homeView) rebuild() {
	keep, _ := h.selected()
	e := h.app.env
	t := h.table
	t.Clear()
	for c, name := range []string{"NAME", "REPOS", "UNCOMMITTED", "UNPUSHED", "MISSING"} {
		cell := tview.NewTableCell(name).SetStyle(styleHeader).SetSelectable(false)
		if c > 0 {
			cell.SetAlign(tview.AlignRight)
		} else {
			cell.SetExpansion(1)
		}
		t.SetCell(0, c, cell)
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
		if !h.match(label) {
			return
		}
		dirty, unpushed, _ := e.counts(repos)
		t.SetCell(row, 0, tview.NewTableCell(tview.Escape(label)).SetTextColor(colorValue).SetExpansion(1).SetReference(ref))
		t.SetCell(row, 1, num(len(repos), colorValue))
		t.SetCell(row, 2, num(dirty, colorDirty))
		t.SetCell(row, 3, num(unpushed, colorDirty))
		t.SetCell(row, 4, num(missing, colorError))
		if ref == keep {
			t.Select(row, 0)
		}
		row++
	}
	all := h.app.visible(e.repos)
	if len(e.repos) > 0 {
		add(homeRef{live: true}, "live status", all, 0)
		add(homeRef{all: true}, "all repos", all, 0)
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
		add(homeRef{project: p.Name}, p.Name, h.app.visible(repos), missing)
	}
	title := fmt.Sprintf(" Home(%s)[%s, %s]%s%s ", shortenHome(e.dir), plural(len(all), "repo"), plural(len(e.projects()), "project"), h.app.hiddenNote(e.repos), h.suffix())
	if len(e.repos) == 0 {
		title = fmt.Sprintf(" Home(%s) no git repositories found ", shortenHome(e.dir))
	}
	t.SetTitle(tview.Escape(title))
	if r, _ := t.GetSelection(); row > 1 && (r < 1 || r >= row) {
		t.Select(1, 0)
	}
}

func (h *homeView) open() {
	ref, ok := h.selected()
	if !ok {
		return
	}
	if ref.live {
		m := newMaster(h.app, "")
		m.view = viewLive
		h.app.ui().MasterView = viewLive.String()
		h.app.saveGlobal()
		h.app.push(m)
		return
	}
	if ref.all {
		h.app.push(newMaster(h.app, ""))
		return
	}
	h.app.push(newMaster(h.app, ref.project))
}
