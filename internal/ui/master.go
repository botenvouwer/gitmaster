package ui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/botenvouwer/gitmaster/internal/git"
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
	viewLive
)

func (v masterMode) String() string {
	switch v {
	case viewPulls:
		return "pulls"
	case viewLive:
		return "live"
	}
	return "repos"
}

func parseMasterMode(s string) masterMode {
	switch s {
	case "pulls":
		return viewPulls
	case "live":
		return viewLive
	}
	return viewRepos
}

// prRef identifies a row of the pull-request overview.
type prRef struct {
	repo *repoEntry
	idx  int
}

// liveKind is the section of a live status row.
type liveKind int

const (
	liveWork liveKind = iota
	liveCommit
	liveOpenPR
	liveClosedPR
)

// liveRef identifies a row of the live status table.
type liveRef struct {
	kind   liveKind
	repo   *repoEntry
	key    string // sha or PR number
	commit git.Commit
	pr     pr.PR
}

// masterView lists the repos of the environment, or of one project, as a
// repo table, as an overview of their open pull requests, or as the live
// status of the user's recent work.
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
	switch m.view {
	case viewPulls:
		c = append(c, "pull requests")
	case viewLive:
		c = append(c, "live status")
	}
	return c
}

func (m *masterView) focusTarget() tview.Primitive { return m.table }
func (m *masterView) update()                      { m.rebuild() }
func (m *masterView) escape() bool                 { return m.clear() }
func (m *masterView) refreshTargets() []*repoEntry { return m.repos() }

func (m *masterView) start() {
	m.rebuild()
	m.loadView(false)
}

// loadView fetches what the current view needs beyond repo status.
func (m *masterView) loadView(force bool) {
	switch m.view {
	case viewPulls:
		m.app.loadPRs(m.repos(), force)
	case viewLive:
		m.app.loadLive(m.repos(), m.liveSince(), force)
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
	m.loadView(false)
}

// --- live status window -----------------------------------------------------

func (m *masterView) windows() []int {
	if m.app.global != nil && len(m.app.global.Settings.LiveWindowsHours) > 0 {
		return m.app.global.Settings.LiveWindowsHours
	}
	return []int{12, 24, 72}
}

func (m *masterView) liveHours() int {
	h := m.app.ui().LiveHours
	if h <= 0 {
		ws := m.windows()
		h = ws[min(1, len(ws)-1)]
	}
	return h
}

func (m *masterView) liveSince() time.Time {
	return time.Now().Add(-time.Duration(m.liveHours()) * time.Hour)
}

func hoursLabel(h int) string {
	if h >= 48 && h%24 == 0 {
		return fmt.Sprintf("%dd", h/24)
	}
	return fmt.Sprintf("%dh", h)
}

func (m *masterView) setWindow(i int) {
	ws := m.windows()
	if i < 0 || i >= len(ws) {
		return
	}
	m.app.ui().LiveHours = ws[i]
	m.app.saveGlobal()
	m.app.Flash("live status window: " + hoursLabel(ws[i]))
	m.rebuild()
	m.app.refresh()
	m.app.loadLive(m.repos(), m.liveSince(), false)
}

func (m *masterView) hotkeys() []hotkey {
	switch m.view {
	case viewPulls:
		return []hotkey{
			{"tab", "live status"},
			{"enter", "open pull request"},
			{"m", "mine / all"},
			{"o", "open in browser"},
			{"/", "filter"},
			{"A", "show / hide archived"},
			{"r", "reload"},
		}
	case viewLive:
		keys := []hotkey{{"tab", "repos"}, {"enter", "open"}}
		for i, h := range m.windows() {
			keys = append(keys, hotkey{strconv.Itoa(i + 1), "last " + hoursLabel(h)})
		}
		return append(keys, hotkey{"o", "open in browser"}, hotkey{"/", "filter"}, hotkey{"r", "reload"})
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
	switch m.view {
	case viewPulls:
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
	case viewLive:
		var opts []string
		for i, h := range m.windows() {
			opts = append(opts, fmt.Sprintf("%d:%s", i+1, hoursLabel(h)))
		}
		return [][2]string{scope, {"Repos", strconv.Itoa(len(repos))},
			{"Window", "last " + hoursLabel(m.liveHours())}, {"Windows", strings.Join(opts, " ")}}
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
	case tcell.KeyTab:
		m.setView((m.view + 1) % 3)
		return nil
	case tcell.KeyBacktab:
		m.setView((m.view + 2) % 3)
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
		switch m.view {
		case viewPulls:
			switch ev.Rune() {
			case 'm':
				m.allPRs = !m.allPRs
				m.app.ui().SetMineOnly(!m.allPRs)
				m.app.saveGlobal()
				m.rebuild()
				return nil
			case 'o':
				if _, p := m.selectedPR(); p != nil {
					m.openBrowser(p.URL)
				}
				return nil
			case 'r':
				m.loadView(true)
				m.rebuild()
				return nil
			}
		case viewLive:
			if ev.Rune() >= '1' && ev.Rune() <= '9' {
				m.setWindow(int(ev.Rune() - '1'))
				return nil
			}
			switch ev.Rune() {
			case 'o':
				if ref, ok := m.selectedLive(); ok && (ref.kind == liveOpenPR || ref.kind == liveClosedPR) {
					m.openBrowser(ref.pr.URL)
				}
				return nil
			case 'r':
				m.loadView(true)
				m.rebuild()
				return nil
			}
		default:
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
	}
	return swallowWhenEmpty(m.table, ev)
}

func (m *masterView) openBrowser(url string) {
	if err := pr.OpenInBrowser(url); err != nil {
		m.app.FlashErr(err)
	}
}

func dirOf(k tcell.Key) int {
	if k == tcell.KeyUp {
		return -1
	}
	return 1
}

func (m *masterView) move(delta int) {
	if !hasSelectable(m.table) {
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
	case liveRef:
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

func (m *masterView) selectedLive() (liveRef, bool) {
	row, _ := m.table.GetSelection()
	if row < 1 {
		return liveRef{}, false
	}
	ref, ok := m.table.GetCell(row, 0).GetReference().(liveRef)
	return ref, ok
}

func (m *masterView) rebuild() {
	switch m.view {
	case viewPulls:
		m.rebuildPulls()
		return
	case viewLive:
		m.rebuildLive()
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

// rebuildLive renders the user's recent work in four sections.
func (m *masterView) rebuildLive() {
	keep, hadKeep := m.selectedLive()
	since := m.liveSince()
	repos := m.repos()
	t := m.table
	t.Clear()
	for c, h := range []string{"REPO", "REF", "SUBJECT", "BRANCH", "AGE"} {
		cell := tview.NewTableCell(h).SetStyle(styleHeader).SetSelectable(false)
		if c == 2 {
			cell.SetExpansion(1)
		}
		t.SetCell(0, c, cell)
	}

	// Gather.
	var work, commits, open, closed []liveRef
	loading, failed := 0, 0
	for _, r := range repos {
		if r.liveLoading || r.prLoading {
			loading++
		}
		if r.closedErr != nil || r.prErr != nil {
			failed++
		}
		if r.loaded && r.err == nil && r.status.Dirty() {
			work = append(work, liveRef{kind: liveWork, repo: r, key: "work"})
		}
		heads := map[string]bool{}
		for _, p := range r.prs {
			heads[p.Head] = true
			if p.Mine && !p.Updated.Before(since) {
				open = append(open, liveRef{kind: liveOpenPR, repo: r, key: strconv.Itoa(p.Number), pr: p})
			}
		}
		for _, p := range r.closedPRs {
			heads[p.Head] = true
			if !p.Closed.Before(since) {
				closed = append(closed, liveRef{kind: liveClosedPR, repo: r, key: strconv.Itoa(p.Number), pr: p})
			}
		}
		for _, c := range r.myCommits {
			if !c.When.Before(since) && !heads[c.Branch] && !c.OnBase {
				commits = append(commits, liveRef{kind: liveCommit, repo: r, key: c.SHA, commit: c})
			}
		}
	}
	sort.SliceStable(commits, func(i, j int) bool { return commits[i].commit.When.After(commits[j].commit.When) })
	sort.SliceStable(open, func(i, j int) bool { return open[i].pr.Updated.After(open[j].pr.Updated) })
	sort.SliceStable(closed, func(i, j int) bool { return closed[i].pr.Closed.After(closed[j].pr.Closed) })

	// Render.
	row, selectRow, shown := 1, 0, 0
	section := func(title string, refs []liveRef) {
		head := tview.NewTableCell(fmt.Sprintf("── %s (%d) ──", title, len(refs))).
			SetStyle(tcell.StyleDefault.Foreground(colorKey).Bold(true)).SetSelectable(false)
		t.SetCell(row, 0, head)
		for c := 1; c <= 4; c++ {
			t.SetCell(row, c, tview.NewTableCell("").SetSelectable(false))
		}
		row++
		n := 0
		for _, ref := range refs {
			cells := m.liveCells(ref)
			if !m.match(cells...) {
				continue
			}
			if hadKeep && ref.kind == keep.kind && ref.repo == keep.repo && ref.key == keep.key {
				selectRow = row
			}
			m.setLiveRow(row, ref, cells)
			row++
			n++
			shown++
		}
		if n == 0 {
			t.SetCell(row, 0, tview.NewTableCell("   (none)").SetTextColor(colorMuted).SetSelectable(false))
			for c := 1; c <= 4; c++ {
				t.SetCell(row, c, tview.NewTableCell("").SetSelectable(false))
			}
			row++
		}
	}
	section("OPEN WORK", work)
	section("COMMITS NOT IN A PULL REQUEST", commits)
	section("OPEN PULL REQUESTS", open)
	section("CLOSED PULL REQUESTS", closed)

	scope := "all"
	if m.project != "" {
		scope = m.project
	}
	title := fmt.Sprintf(" Live Status(%s, last %s)[%d]", scope, hoursLabel(m.liveHours()), shown)
	if loading > 0 {
		title += fmt.Sprintf(" loading %d", loading)
	}
	if failed > 0 {
		title += fmt.Sprintf(" %d unavailable", failed)
	}
	t.SetTitle(tview.Escape(title + m.app.hiddenNote(m.scope()) + m.suffix() + " "))
	if selectRow > 0 {
		t.Select(selectRow, 0)
	} else if shown > 0 {
		t.Select(1, 0) // draw moves it to the first selectable row
	}
}

// liveCells returns the REPO, REF, SUBJECT, BRANCH and AGE texts of a row.
func (m *masterView) liveCells(ref liveRef) []string {
	r := ref.repo
	switch ref.kind {
	case liveWork:
		s := r.status
		var files []string
		for _, f := range s.Files {
			files = append(files, f.Path)
		}
		return []string{r.name, fmt.Sprintf("+%d ~%d ?%d", s.Staged, s.Modified, s.Untracked),
			trunc(strings.Join(files, ", "), 80), s.Branch, age(s.LastCommit)}
	case liveCommit:
		c := ref.commit
		sha := c.Short
		if c.Unpushed {
			sha += " ↑"
		}
		return []string{r.name, sha, c.Subject, c.Branch, age(c.When)}
	case liveOpenPR:
		p := ref.pr
		title := p.Title
		if p.Draft {
			title = "[draft] " + title
		}
		return []string{r.name, "#" + strconv.Itoa(p.Number), title, p.Head + " → " + p.Base, age(p.Updated)}
	default:
		p := ref.pr
		return []string{r.name, "#" + strconv.Itoa(p.Number), p.Title, p.Head + " → " + p.Base, p.State + " " + age(p.Closed)}
	}
}

func (m *masterView) setLiveRow(row int, ref liveRef, cells []string) {
	refColor := colorSHA
	if ref.kind == liveWork {
		refColor = colorDirty
	} else if ref.kind == liveCommit && ref.commit.Unpushed {
		refColor = colorDirty
	}
	m.table.SetCell(row, 0, tview.NewTableCell(tview.Escape(cells[0])).SetTextColor(colorValue).SetReference(ref))
	m.table.SetCell(row, 1, tview.NewTableCell(tview.Escape(cells[1])).SetTextColor(refColor))
	m.table.SetCell(row, 2, tview.NewTableCell(tview.Escape(cells[2])).SetTextColor(colorValue).SetExpansion(1))
	m.table.SetCell(row, 3, tview.NewTableCell(tview.Escape(cells[3])).SetTextColor(colorMuted))
	m.table.SetCell(row, 4, tview.NewTableCell(tview.Escape(cells[4])).SetTextColor(colorMuted))
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
	switch m.view {
	case viewPulls:
		r, p := m.selectedPR()
		if r == nil {
			return
		}
		d := newDetail(m.app, r, modePRs)
		d.openPR = p.Number
		d.mineOnly = p.Mine // the chosen PR must be in the list
		m.app.push(d)
	case viewLive:
		ref, ok := m.selectedLive()
		if !ok {
			return
		}
		switch ref.kind {
		case liveWork:
			m.app.push(newDetail(m.app, ref.repo, modeWork))
		case liveCommit:
			d := newDetail(m.app, ref.repo, modeCommits)
			d.openSHA = ref.commit.SHA
			m.app.push(d)
		case liveOpenPR:
			d := newDetail(m.app, ref.repo, modePRs)
			d.openPR = ref.pr.Number
			d.mineOnly = true
			m.app.push(d)
		case liveClosedPR:
			m.openBrowser(ref.pr.URL)
		}
	default:
		r := m.selected()
		if r == nil {
			return
		}
		m.app.push(newDetail(m.app, r, parseMode(m.app.ui().DetailView)))
	}
}

func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
