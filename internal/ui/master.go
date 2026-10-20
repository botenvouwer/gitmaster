package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type filterMode int

const (
	filterAll filterMode = iota
	filterUnpushed
	filterUncommitted
)

func (f filterMode) String() string {
	switch f {
	case filterUnpushed:
		return "unpushed"
	case filterUncommitted:
		return "uncommitted"
	}
	return "all"
}

// masterView lists the repos of the environment, or of one project.
type masterView struct {
	*tview.Flex
	app     *App
	project string // "" for all repos
	table   *tview.Table
	input   *tview.InputField

	mode filterMode
	text string
}

func newMaster(a *App, project string) *masterView {
	m := &masterView{Flex: tview.NewFlex().SetDirection(tview.FlexRow), app: a, project: project}

	m.input = tview.NewInputField().SetLabel(" /> ").SetLabelColor(colorKey).
		SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(colorValue)
	m.input.SetChangedFunc(func(text string) {
		m.text = text
		m.rebuild()
	})
	m.input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc {
			m.input.SetText("")
		}
		m.showInput(false)
	})

	m.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSelectedStyle(styleSelected)
	m.table.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle).SetBorderColor(colorFocus)
	m.table.SetSelectedFunc(func(row, _ int) { m.open() })
	m.table.SetInputCapture(m.keys)

	m.AddItem(m.input, 0, 0, false).AddItem(m.table, 0, 1, true)
	return m
}

func (m *masterView) crumbs() []string {
	if m.project != "" {
		return []string{m.project}
	}
	return []string{"repos"}
}

func (m *masterView) focusTarget() tview.Primitive { return m.table }
func (m *masterView) start()                       { m.rebuild() }
func (m *masterView) update()                      { m.rebuild() }

func (m *masterView) hotkeys() []hotkey {
	keys := []hotkey{
		{"0", "all"},
		{"1", "unpushed commits"},
		{"2", "uncommitted / untracked"},
		{"/", "filter by name"},
		{"enter", "open repo"},
		{"a", "add to project"},
	}
	if m.project != "" {
		keys = append(keys, hotkey{"x", "remove from project"})
	}
	return append(keys, hotkey{"r", "rescan"})
}

// repos returns the repos in scope: all, or the project's members.
func (m *masterView) repos() []*repoEntry {
	e := m.app.env
	if m.project == "" {
		return e.repos
	}
	p := e.local.Project(m.project)
	if p == nil {
		return nil
	}
	var out []*repoEntry
	for _, r := range e.repos {
		if p.Has(r.name) {
			out = append(out, r)
		}
	}
	return out
}

func (m *masterView) info() [][2]string {
	repos := m.repos()
	dirty, unpushed, loaded := m.app.env.counts(repos)
	count := strconv.Itoa(len(repos))
	if loaded < len(repos) {
		count += fmt.Sprintf(" (%d loading)", len(repos)-loaded)
	}
	scope := [2]string{"Dir", shortenHome(m.app.env.dir)}
	if m.project != "" {
		scope = [2]string{"Project", m.project}
	}
	return [][2]string{
		scope,
		{"Repos", count},
		{"Dirty", fmt.Sprintf("%d uncommitted, %d unpushed", dirty, unpushed)},
		{"Filter", m.mode.String()},
	}
}

func (m *masterView) escape() bool {
	if m.text != "" {
		m.input.SetText("")
		m.text = ""
		m.rebuild()
		return true
	}
	return false
}

func (m *masterView) showInput(show bool) {
	if show {
		m.ResizeItem(m.input, 1, 0)
		m.app.SetFocus(m.input)
		return
	}
	m.ResizeItem(m.input, 0, 0)
	m.app.SetFocus(m.table)
	m.rebuild()
}

func (m *masterView) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyUp, tcell.KeyDown:
		if ev.Modifiers()&tcell.ModShift != 0 {
			m.move(dirOf(ev.Key()) * 5)
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case '0', '1', '2':
			m.mode = filterMode(ev.Rune() - '0')
			m.rebuild()
			m.app.refresh()
			return nil
		case '/':
			m.showInput(true)
			return nil
		case 'a':
			m.addToProject()
			return nil
		case 'x':
			m.removeFromProject()
			return nil
		case 'r':
			m.app.scan()
			return nil
		case 'J':
			m.move(5)
			return nil
		case 'K':
			m.move(-5)
			return nil
		}
	}
	return swallowWhenEmpty(m.table, ev)
}

func dirOf(k tcell.Key) int {
	if k == tcell.KeyUp {
		return -1
	}
	return 1
}

func (m *masterView) move(delta int) {
	if m.table.GetRowCount() <= 1 {
		return
	}
	row, _ := m.table.GetSelection()
	row = max(1, min(m.table.GetRowCount()-1, row+delta))
	m.table.Select(row, 0)
}

func (m *masterView) addToProject() {
	r := m.selected()
	local := m.app.env.local
	if r == nil || local == nil {
		return
	}
	var names []string
	for _, p := range local.Projects {
		names = append(names, p.Name)
	}
	items := append(append([]string{}, names...), "+ new project…")
	m.app.pick("Add "+r.name+" to project", items, func(i int) {
		if i < len(names) {
			m.app.addRepoToProject(names[i], r.name)
			return
		}
		m.app.prompt("New project name", "", func(name string) {
			if _, err := local.AddProject(name); err != nil {
				m.app.FlashErr(err)
				return
			}
			m.app.addRepoToProject(name, r.name)
		})
	})
}

func (m *masterView) removeFromProject() {
	r := m.selected()
	if r == nil || m.project == "" || m.app.env.local == nil {
		return
	}
	p := m.app.env.local.Project(m.project)
	if p == nil || !p.Has(r.name) {
		return
	}
	p.Toggle(r.name)
	m.app.saveLocal()
	m.app.Flash("removed " + r.name + " from " + m.project)
	m.app.notify()
}

func (m *masterView) visible(r *repoEntry) bool {
	if m.text != "" && !strings.Contains(strings.ToLower(r.name), strings.ToLower(m.text)) {
		return false
	}
	switch m.mode {
	case filterUnpushed:
		return r.loaded && r.err == nil && r.status.Ahead > 0
	case filterUncommitted:
		return r.loaded && r.err == nil && r.status.Dirty()
	}
	return true
}

func (m *masterView) selected() *repoEntry {
	row, _ := m.table.GetSelection()
	if row < 1 {
		return nil
	}
	if ref, ok := m.table.GetCell(row, 0).GetReference().(*repoEntry); ok {
		return ref
	}
	return nil
}

func (m *masterView) rebuild() {
	keep := m.selected()
	m.table.Clear()

	headers := []string{"NAME", "BRANCH", "↑", "↓", "STAGED", "MODIFIED", "UNTRACKED", "LAST COMMIT", "STATE"}
	for c, h := range headers {
		cell := tview.NewTableCell(h).SetStyle(styleHeader).SetSelectable(false)
		if c >= 2 && c <= 6 {
			cell.SetAlign(tview.AlignRight)
		}
		if c == 0 {
			cell.SetExpansion(1)
		}
		m.table.SetCell(0, c, cell)
	}

	row, selectRow := 1, 1
	for _, r := range m.repos() {
		if !m.visible(r) {
			continue
		}
		if r == keep {
			selectRow = row
		}
		m.setRow(row, r)
		row++
	}
	count := row - 1
	scope := m.mode.String()
	if m.project != "" {
		scope = m.project + ":" + scope
	}
	title := fmt.Sprintf(" Repos(%s)[%d] ", scope, count)
	if m.text != "" {
		title += "/" + m.text + " "
	}
	m.table.SetTitle(tview.Escape(title))
	if count > 0 {
		m.table.Select(selectRow, 0)
	}
}

// stateText summarises a repo for the STATE column.
func stateText(r *repoEntry) (string, tcell.Color) {
	if !r.loaded {
		if r.cached {
			return "cached", colorMuted
		}
		return "loading…", colorMuted
	}
	if r.err != nil {
		return trunc(r.err.Error(), 60), colorError
	}
	var state []string
	if r.status.Ahead > 0 {
		state = append(state, "unpushed")
	}
	if r.status.Dirty() {
		state = append(state, "uncommitted")
	}
	if r.status.Conflicted > 0 {
		state = append(state, "conflicts")
	}
	if len(state) == 0 {
		return "clean", colorClean
	}
	return strings.Join(state, ","), colorDirty
}

func (m *masterView) setRow(row int, r *repoEntry) {
	name := tview.NewTableCell(tview.Escape(r.name)).SetTextColor(colorValue).SetExpansion(1).SetReference(r)
	m.table.SetCell(row, 0, name)
	text, color := stateText(r)
	m.table.SetCell(row, 8, tview.NewTableCell(text).SetTextColor(color))
	if (!r.loaded && !r.cached) || (r.loaded && r.err != nil) {
		return
	}
	s := r.status
	num := func(n int) *tview.TableCell {
		c := tview.NewTableCell(strconv.Itoa(n)).SetAlign(tview.AlignRight).SetTextColor(colorMuted)
		if n > 0 {
			c.SetTextColor(colorDirty)
		}
		return c
	}
	branch := s.Branch
	if r.loaded && !s.HasUpstream && s.HasRemote {
		branch += " (no upstream)"
	}
	m.table.SetCell(row, 1, tview.NewTableCell(tview.Escape(branch)).SetTextColor(colorValue))
	m.table.SetCell(row, 2, num(s.Ahead))
	behind := num(s.Behind)
	behind.SetTextColor(colorMuted)
	m.table.SetCell(row, 3, behind)
	m.table.SetCell(row, 4, num(s.Staged))
	m.table.SetCell(row, 5, num(s.Modified))
	m.table.SetCell(row, 6, num(s.Untracked))
	last := age(s.LastCommit)
	if s.LastSubject != "" {
		last += "  " + trunc(s.LastSubject, 40)
	}
	m.table.SetCell(row, 7, tview.NewTableCell(tview.Escape(last)).SetTextColor(colorMuted))
}

func (m *masterView) open() {
	r := m.selected()
	if r == nil {
		return
	}
	m.app.push(newDetail(m.app, r))
}

func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
