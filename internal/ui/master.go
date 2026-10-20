package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/botenvouwer/gitmaster/internal/pr"
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

// masterMode is what the master screen shows for its scope.
type masterMode int

const (
	viewRepos masterMode = iota
	viewPulls
)

func (v masterMode) String() string {
	if v == viewPulls {
		return "pulls"
	}
	return "repos"
}

func parseMasterMode(s string) masterMode {
	if s == "pulls" {
		return viewPulls
	}
	return viewRepos
}

// prRef identifies a row of the pull-request overview.
type prRef struct {
	repo *repoEntry
	idx  int
}

// masterView lists the repos of the environment, or of one project, either
// as a repo table or as an overview of their open pull requests.
type masterView struct {
	*filterBox
	app     *App
	project string // "" for all repos
	view    masterMode
	mode    filterMode
	allPRs  bool
}

func newMaster(a *App, project string) *masterView {
	m := &masterView{app: a, project: project, view: parseMasterMode(a.ui().MasterView), allPRs: !a.ui().MineOnly()}
	table := tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSelectedStyle(styleSelected)
	table.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle).SetBorderColor(colorFocus)
	table.SetSelectedFunc(func(row, _ int) { m.open() })
	table.SetInputCapture(m.keys)
	m.filterBox = newFilterBox(a, table, m.rebuild)
	return m
}

func (m *masterView) crumbs() []string {
	c := []string{"repos"}
	if m.project != "" {
		c = []string{m.project}
	}
	if m.view == viewPulls {
		c = append(c, "pull requests")
	}
	return c
}

func (m *masterView) focusTarget() tview.Primitive { return m.table }
func (m *masterView) update()                      { m.rebuild() }
func (m *masterView) escape() bool                 { return m.clear() }
func (m *masterView) refreshTargets() []*repoEntry { return m.repos() }

func (m *masterView) start() {
	m.rebuild()
	if m.view == viewPulls {
		m.app.loadPRs(m.repos(), false)
	}
}

func (m *masterView) setView(v masterMode) {
	if m.view == v {
		return
	}
	m.view = v
	m.reset()
	m.app.ui().MasterView = v.String()
	m.app.saveGlobal()
	m.rebuild()
	m.app.refresh()
	if v == viewPulls {
		m.app.loadPRs(m.repos(), false)
	}
}

func (m *masterView) hotkeys() []hotkey {
	if m.view == viewPulls {
		return []hotkey{
			{"tab", "repos"},
			{"enter", "open pull request"},
			{"m", "mine / all"},
			{"o", "open in browser"},
			{"/", "filter"},
			{"A", "show / hide archived"},
			{"r", "reload"},
		}
	}
	keys := []hotkey{
		{"tab", "pull requests"},
		{"0", "all"},
		{"1", "unpushed commits"},
		{"2", "uncommitted / untracked"},
		{"/", "filter"},
		{"enter", "open repo"},
		{"a", "add to project"},
	}
	if m.project != "" {
		keys = append(keys, hotkey{"x", "remove from project"})
	}
	return append(keys, hotkey{"A", "show / hide archived"}, hotkey{"r", "rescan"})
}

// scope returns the repos in scope, archived ones included: all, or the
// project's members.
func (m *masterView) scope() []*repoEntry {
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

// repos returns the repos to show.
func (m *masterView) repos() []*repoEntry { return m.app.visible(m.scope()) }

func (m *masterView) info() [][2]string {
	repos := m.repos()
	scope := [2]string{"Dir", shortenHome(m.app.env.dir)}
	if m.project != "" {
		scope = [2]string{"Project", m.project}
	}
	if m.view == viewPulls {
		loaded, total, mine := 0, 0, 0
		for _, r := range repos {
			if r.prLoaded {
				loaded++
			}
			for _, p := range r.prs {
				total++
				if p.Mine {
					mine++
				}
			}
		}
		state := fmt.Sprintf("%d open, %d mine", total, mine)
		if loaded < len(repos) {
			state += fmt.Sprintf(" (%d/%d repos loaded)", loaded, len(repos))
		}
		return [][2]string{scope, {"Repos", strconv.Itoa(len(repos))}, {"PRs", state}, {"View", "pull requests"}}
	}
	dirty, unpushed, loaded := m.app.env.counts(repos)
	count := strconv.Itoa(len(repos))
	if loaded < len(repos) {
		count += fmt.Sprintf(" (%d loading)", len(repos)-loaded)
	}
	return [][2]string{
		scope,
		{"Repos", count},
		{"Dirty", fmt.Sprintf("%d uncommitted, %d unpushed", dirty, unpushed)},
		{"Filter", m.mode.String()},
	}
}

func (m *masterView) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyBacktab:
		if m.view == viewPulls {
			m.setView(viewRepos)
		} else {
			m.setView(viewPulls)
		}
		return nil
	case tcell.KeyUp, tcell.KeyDown:
		if ev.Modifiers()&tcell.ModShift != 0 {
			m.move(dirOf(ev.Key()) * 5)
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case '/':
			m.show(true)
			return nil
		case 'A':
			m.app.toggleArchived()
			return nil
		case 'J':
			m.move(5)
			return nil
		case 'K':
			m.move(-5)
			return nil
		}
		if m.view == viewPulls {
			switch ev.Rune() {
			case 'm':
				m.allPRs = !m.allPRs
				m.app.ui().SetMineOnly(!m.allPRs)
				m.app.saveGlobal()
				m.rebuild()
				return nil
			case 'o':
				if _, p := m.selectedPR(); p != nil {
					if err := pr.OpenInBrowser(p.URL); err != nil {
						m.app.FlashErr(err)
					}
				}
				return nil
			case 'r':
				m.app.loadPRs(m.repos(), true)
				m.rebuild()
				return nil
			}
			break
		}
		switch ev.Rune() {
		case '0', '1', '2':
			m.mode = filterMode(ev.Rune() - '0')
			m.rebuild()
			m.app.refresh()
			return nil
		case 'a':
			m.addToProject()
			return nil
		case 'x':
			m.removeFromProject()
			return nil
		case 'r':
			m.app.scan(true)
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
	if !m.match(r.name, r.status.Branch) {
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
	switch ref := m.table.GetCell(row, 0).GetReference().(type) {
	case *repoEntry:
		return ref
	case prRef:
		return ref.repo
	}
	return nil
}

func (m *masterView) selectedPR() (*repoEntry, *pr.PR) {
	row, _ := m.table.GetSelection()
	if row < 1 {
		return nil, nil
	}
	if ref, ok := m.table.GetCell(row, 0).GetReference().(prRef); ok && ref.idx < len(ref.repo.prs) {
		return ref.repo, &ref.repo.prs[ref.idx]
	}
	return nil, nil
}

func (m *masterView) rebuild() {
	if m.view == viewPulls {
		m.rebuildPulls()
		return
	}
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
	title := fmt.Sprintf(" Repos(%s)[%d]%s%s ", scope, count, m.app.hiddenNote(m.scope()), m.suffix())
	m.table.SetTitle(tview.Escape(title))
	if count > 0 {
		m.table.Select(selectRow, 0)
	}
}

func (m *masterView) rebuildPulls() {
	keepRepo, keepPR := m.selectedPR()
	m.table.Clear()
	for c, h := range []string{"REPO", "#", "TITLE", "AUTHOR", "HEAD → BASE", "UPDATED"} {
		cell := tview.NewTableCell(h).SetStyle(styleHeader).SetSelectable(false)
		if c == 2 {
			cell.SetExpansion(1)
		}
		m.table.SetCell(0, c, cell)
	}
	row, selectRow := 1, 1
	loaded, failed := 0, 0
	repos := m.repos()
	for _, r := range repos {
		if r.prLoaded {
			loaded++
		}
		if r.prErr != nil {
			failed++
		}
		for i := range r.prs {
			p := &r.prs[i]
			if !m.allPRs && !p.Mine {
				continue
			}
			title := p.Title
			if p.Draft {
				title = "[draft] " + title
			}
			if !m.match(r.name, strconv.Itoa(p.Number), title, p.Author, p.Head, p.Base) {
				continue
			}
			if r == keepRepo && keepPR != nil && p.Number == keepPR.Number {
				selectRow = row
			}
			author := trunc(p.Author, 18)
			authorColor := colorMuted
			if p.Mine {
				authorColor = colorClean
			}
			m.table.SetCell(row, 0, tview.NewTableCell(tview.Escape(r.name)).SetTextColor(colorValue).SetReference(prRef{r, i}))
			m.table.SetCell(row, 1, tview.NewTableCell(strconv.Itoa(p.Number)).SetTextColor(colorSHA))
			m.table.SetCell(row, 2, tview.NewTableCell(tview.Escape(title)).SetTextColor(colorValue).SetExpansion(1))
			m.table.SetCell(row, 3, tview.NewTableCell(tview.Escape(author)).SetTextColor(authorColor))
			m.table.SetCell(row, 4, tview.NewTableCell(tview.Escape(p.Head+" → "+p.Base)).SetTextColor(colorMuted))
			m.table.SetCell(row, 5, tview.NewTableCell(age(p.Updated)).SetTextColor(colorMuted))
			row++
		}
	}
	count := row - 1
	scope := "all"
	if !m.allPRs {
		scope = "mine"
	}
	if m.project != "" {
		scope = m.project + ":" + scope
	}
	title := fmt.Sprintf(" Pull Requests(%s)[%d]", scope, count)
	if loaded < len(repos) {
		title += fmt.Sprintf(" loading %d/%d", loaded, len(repos))
	}
	if failed > 0 {
		title += fmt.Sprintf(" %d unavailable", failed)
	}
	m.table.SetTitle(tview.Escape(title + m.app.hiddenNote(m.scope()) + m.suffix() + " "))
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
		if r.archived {
			return "archived", colorMuted
		}
		return "clean", colorClean
	}
	if r.archived {
		state = append([]string{"archived"}, state...)
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
	if m.view == viewPulls {
		r, p := m.selectedPR()
		if r == nil {
			return
		}
		d := newDetail(m.app, r, modePRs)
		d.openPR = p.Number
		d.mineOnly = p.Mine // the chosen PR must be in the list
		m.app.push(d)
		return
	}
	r := m.selected()
	if r == nil {
		return
	}
	m.app.push(newDetail(m.app, r, parseMode(m.app.ui().DetailView)))
}

func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
