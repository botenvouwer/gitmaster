package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/botenvouwer/gitmaster/internal/git"
)

type repoEntry struct {
	name   string
	path   string
	status git.Status
	loaded bool
	err    error
}

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

// masterView lists every git repo under a directory.
type masterView struct {
	*tview.Flex
	app   *App
	dir   string
	table *tview.Table
	input *tview.InputField

	repos   []*repoEntry
	mode    filterMode
	text    string
	loadGen int
}

func newMaster(a *App, dir string) *masterView {
	m := &masterView{Flex: tview.NewFlex().SetDirection(tview.FlexRow), app: a, dir: dir}

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
	m.table.SetSelectedFunc(func(row, _ int) { m.open(row) })
	m.table.SetInputCapture(m.keys)

	m.AddItem(m.input, 0, 0, false).AddItem(m.table, 0, 1, true)
	return m
}

func (m *masterView) crumbs() []string             { return []string{"repos"} }
func (m *masterView) focusTarget() tview.Primitive { return m.table }

func (m *masterView) hotkeys() []hotkey {
	return []hotkey{
		{"0", "all"},
		{"1", "unpushed commits"},
		{"2", "uncommitted / untracked"},
		{"/", "filter by name"},
		{"enter", "open repo"},
		{"r", "reload"},
	}
}

func (m *masterView) info() [][2]string {
	dirty, unpushed, loaded := 0, 0, 0
	for _, r := range m.repos {
		if !r.loaded {
			continue
		}
		loaded++
		if r.status.Dirty() {
			dirty++
		}
		if r.status.Ahead > 0 {
			unpushed++
		}
	}
	repos := strconv.Itoa(len(m.repos))
	if loaded < len(m.repos) {
		repos += fmt.Sprintf(" (%d loading)", len(m.repos)-loaded)
	}
	return [][2]string{
		{"Dir", shortenHome(m.dir)},
		{"Repos", repos},
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

func (m *masterView) start() { m.reload() }

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
		case 'r':
			m.reload()
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

func (m *masterView) reload() {
	m.loadGen++
	gen := m.loadGen
	paths, err := git.ScanRepos(m.dir)
	if err != nil {
		m.app.FlashErr(err)
		return
	}
	m.repos = nil
	for _, p := range paths {
		m.repos = append(m.repos, &repoEntry{name: filepath.Base(p), path: p})
	}
	m.rebuild()
	m.app.refresh()

	sem := make(chan struct{}, 8)
	for _, r := range m.repos {
		go func(r *repoEntry) {
			sem <- struct{}{}
			defer func() { <-sem }()
			st, err := git.GetStatus(r.path)
			m.app.QueueUpdateDraw(func() {
				if gen != m.loadGen {
					return
				}
				r.status, r.err, r.loaded = st, err, true
				m.rebuild()
				m.app.refresh()
			})
		}(r)
	}
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
	for _, r := range m.repos {
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
	title := fmt.Sprintf(" Repos(%s)[%d] ", m.mode, count)
	if m.text != "" {
		title = fmt.Sprintf(" Repos(%s)[%d] /%s ", m.mode, count, m.text)
	}
	m.table.SetTitle(title)
	if count > 0 {
		m.table.Select(selectRow, 0)
	}
}

func (m *masterView) setRow(row int, r *repoEntry) {
	name := tview.NewTableCell(tview.Escape(r.name)).SetTextColor(colorValue).SetExpansion(1).SetReference(r)
	m.table.SetCell(row, 0, name)
	if !r.loaded {
		m.table.SetCell(row, 8, tview.NewTableCell("loading…").SetTextColor(colorMuted))
		return
	}
	if r.err != nil {
		m.table.SetCell(row, 8, tview.NewTableCell(trunc(r.err.Error(), 60)).SetTextColor(colorError))
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
	if !s.HasUpstream && s.HasRemote {
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

	var state []string
	if s.Ahead > 0 {
		state = append(state, "unpushed")
	}
	if s.Dirty() {
		state = append(state, "uncommitted")
	}
	if s.Conflicted > 0 {
		state = append(state, "conflicts")
	}
	stateCell := tview.NewTableCell("clean").SetTextColor(colorClean)
	if len(state) > 0 {
		stateCell.SetText(strings.Join(state, ",")).SetTextColor(colorDirty)
	}
	m.table.SetCell(row, 8, stateCell)
}

func (m *masterView) open(row int) {
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
