package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/botenvouwer/gitmaster/internal/git"
	"github.com/botenvouwer/gitmaster/internal/pr"
)

type mode int

const (
	modeWork mode = iota
	modeCommits
	modePRs
	modeChange    // the files of one commit or pull request (pushed with Enter)
	modePRCommits // the commits of one pull request (pushed with Enter on a PR)
)

func (m mode) String() string {
	switch m {
	case modeCommits:
		return "commits"
	case modePRs:
		return "pull requests"
	case modeChange:
		return "change"
	case modePRCommits:
		return "commits"
	}
	return "open work"
}

func parseMode(s string) mode {
	switch s {
	case "commits":
		return modeCommits
	case "pull requests":
		return modePRs
	}
	return modeWork
}

// layout is how commits and pull requests are laid out on the repo screen.
type layout int

const (
	layoutDefault      layout = iota // list | content; Enter opens the change's files
	layoutFilesFirst                 // list | files | content
	layoutContentFirst               // list | content (65%) | files
)

func (l layout) String() string {
	switch l {
	case layoutFilesFirst:
		return "files first"
	case layoutContentFirst:
		return "content first"
	}
	return "default"
}

func parseLayout(s string) layout {
	switch s {
	case "files first":
		return layoutFilesFirst
	case "content first":
		return layoutContentFirst
	}
	return layoutDefault
}

type pane int

const (
	paneList pane = iota
	paneFiles
	paneContent
)

const commitLimit = 500

// change is a commit or a pull request seen as a set of file changes.
type change struct {
	key, label string
	files      func() ([]git.ChangedFile, error)
	fileText   func(f git.ChangedFile, full bool) (string, error)
	message    func() (string, error)       // header/message/stat (content-first, list focused)
	primary    func() (string, error)       // default layout content
	alt        func() (string, error)       // default layout content after d
	labels     [2]string                    // titles for primary / alt
	url        func() (string, error)       // web page of the change
	commits    func() ([]git.Commit, error) // pull requests: their commits
}

// detailView shows one repo: a list on the left (open-work files, commits or
// pull requests) and content on the right, optionally with a files pane for
// the selected commit/PR.
type detailView struct {
	*tview.Flex
	app    *App
	repo   *repoEntry
	mode   mode
	layout layout
	fixed  *change // modeChange: the change whose files are listed; modePRCommits: the pull request

	list    *tview.Table
	filter  *filterBox
	files   *tview.Table
	content *tview.TextView

	order    []pane
	focus    pane
	msgMode  bool    // content-first: show the change's message (list focused last) instead of a file
	showAlt  [5]bool // per mode: alternate content rendering (d toggles)
	mineOnly bool
	openPR   int    // select this pull request once the list is rendered
	openSHA  string // select this commit once the list is rendered

	workFiles   []git.File
	commits     []git.Commit
	commitsHead string // headKey of the status the commit list was loaded for
	renderedPRs int    // repo.prGen that the PR list was last rendered from

	cur        *change
	curFiles   []git.ChangedFile
	filesCache map[string][]git.ChangedFile
	mu         sync.Mutex
	prRefs     map[int][2]string

	leftGen    int
	filesGen   int
	rightGen   int
	rightTimer *time.Timer
	rightPath  string  // open work: the file the content pane shows
	rightSig   fileSig // its state on disk when the content was read
	rightDone  bool    // the content pane shows what was last loaded
}

func newDetail(a *App, r *repoEntry, m mode) *detailView {
	d := &detailView{
		Flex: tview.NewFlex(), app: a, repo: r, mode: m,
		layout:     parseLayout(a.ui().Layout),
		mineOnly:   a.ui().MineOnly(),
		filesCache: map[string][]git.ChangedFile{}, prRefs: map[int][2]string{},
	}
	d.build()
	return d
}

// newChangeView lists the files of one change with their content on the right.
func newChangeView(a *App, r *repoEntry, ch *change) *detailView {
	d := &detailView{
		Flex: tview.NewFlex(), app: a, repo: r, mode: modeChange, fixed: ch,
		filesCache: map[string][]git.ChangedFile{}, prRefs: map[int][2]string{},
	}
	d.build()
	return d
}

// newPRCommitsView lists the commits of one pull request like the commits
// mode: content on the right, Enter opens a commit's files, f the files of
// the whole pull request.
func newPRCommitsView(a *App, r *repoEntry, ch *change) *detailView {
	d := &detailView{
		Flex: tview.NewFlex(), app: a, repo: r, mode: modePRCommits, fixed: ch,
		layout:     parseLayout(a.ui().Layout),
		filesCache: map[string][]git.ChangedFile{}, prRefs: map[int][2]string{},
	}
	d.build()
	return d
}

func (d *detailView) build() {
	d.list = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	d.list.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle)
	d.list.SetSelectionChangedFunc(func(int, int) { d.onListChanged() })
	d.list.SetInputCapture(d.keys)
	d.list.SetFocusFunc(func() { d.onFocus(paneList) })
	d.filter = newFilterBox(d.app, d.list, d.renderLeft)

	d.files = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	d.files.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle)
	d.files.SetSelectionChangedFunc(func(int, int) {
		if d.threePane() && !(d.layout == layoutContentFirst && d.msgMode) {
			d.scheduleRight()
		}
	})
	d.files.SetInputCapture(d.keys)
	selectOnScroll(d.files)
	d.files.SetFocusFunc(func() { d.onFocus(paneFiles) })

	d.content = tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	d.content.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle)
	d.content.SetInputCapture(d.keys)
	d.content.SetFocusFunc(func() { d.onFocus(paneContent) })

	d.buildLayout()
}

// threePane reports whether the files pane is part of the layout.
func (d *detailView) threePane() bool {
	return (d.mode == modeCommits || d.mode == modePRs || d.mode == modePRCommits) && d.layout != layoutDefault
}

func (d *detailView) buildLayout() {
	d.Clear()
	switch {
	case !d.threePane():
		d.AddItem(d.filter, 0, 2, true).AddItem(d.content, 0, 3, false)
		d.order = []pane{paneList, paneContent}
	case d.layout == layoutFilesFirst:
		d.AddItem(d.filter, 0, 1, true).AddItem(d.files, 0, 1, false).AddItem(d.content, 0, 2, false)
		d.order = []pane{paneList, paneFiles, paneContent}
	default:
		d.AddItem(d.filter, 0, 7, true).AddItem(d.content, 0, 26, false).AddItem(d.files, 0, 7, false)
		d.order = []pane{paneList, paneContent, paneFiles}
	}
	d.focus = paneList
	d.msgMode = true
	d.paintFocus()
}

func (d *detailView) crumbs() []string {
	if d.mode == modeChange {
		return []string{d.fixed.label}
	}
	if d.mode == modePRCommits {
		return []string{d.fixed.label + " commits"}
	}
	return []string{d.repo.name, d.mode.String()}
}

func (d *detailView) escape() bool                 { return d.filter.clear() }
func (d *detailView) refreshTargets() []*repoEntry { return []*repoEntry{d.repo} }
func (d *detailView) focusTarget() tview.Primitive { return d.prim(d.focus) }

// stop drops the pull-request refs fetched while browsing.
func (d *detailView) stop() {
	if len(d.prRefs) > 0 {
		go git.CleanPRRefs(d.repo.path)
	}
}

func (d *detailView) prim(p pane) tview.Primitive {
	switch p {
	case paneFiles:
		return d.files
	case paneContent:
		return d.content
	}
	return d.list
}

func (d *detailView) hotkeys() []hotkey {
	if d.mode == modeChange {
		return []hotkey{
			{"d", "diff / full file"},
			{"←/→", "switch pane"},
			{"c", "copy file path"},
			{"o", "open in browser"},
			{"/", "filter"},
			{"esc", "back"},
		}
	}
	if d.mode == modePRCommits {
		return []hotkey{
			{"enter", "files of commit"},
			{"f", "files of pull request"},
			{"d", "diff / full file"},
			{"v", "layout"},
			{"←/→", "switch pane"},
			{"c", "copy hash"},
			{"o", "open in browser"},
			{"/", "filter"},
			{"esc", "back"},
		}
	}
	keys := []hotkey{
		{"1", "open work"},
		{"2", "commits"},
		{"3", "pull requests"},
		{"←/→", "switch pane"},
		{"/", "filter"},
		{"c", "copy"},
		{"r", "reload"},
	}
	switch d.mode {
	case modeWork:
		keys = append(keys, hotkey{"d", "content / diff"})
	case modeCommits:
		keys = append(keys, hotkey{"enter", "files of commit"}, hotkey{"d", "diff / full file"}, hotkey{"v", "layout"},
			hotkey{"o", "open in browser"})
	case modePRs:
		keys = append(keys, hotkey{"enter", "commits of PR"}, hotkey{"f", "files of PR"}, hotkey{"d", "details / diff"},
			hotkey{"v", "layout"}, hotkey{"m", "mine / all"}, hotkey{"o", "open in browser"})
	}
	return keys
}

func (d *detailView) info() [][2]string {
	s := d.repo.status
	branch := s.Branch
	if s.HasUpstream {
		branch += " → " + s.Upstream
	}
	rows := [][2]string{
		{"Repo", d.repo.name},
		{"Branch", branch},
		{"Sync", fmt.Sprintf("↑%d ↓%d", s.Ahead, s.Behind)},
	}
	switch d.mode {
	case modeChange:
		return append(rows, [2]string{"Change", d.fixed.label})
	case modePRCommits:
		return append(rows, [2]string{"PR", d.fixed.label}, [2]string{"Layout", d.layout.String()})
	}
	return append(rows, [2]string{"Layout", d.layout.String()})
}

func (d *detailView) start() {
	if d.mode == modeChange || d.mode == modePRCommits {
		d.loadLeft() // the parent repo screen owns the PR refs
		return
	}
	git.CleanPRRefs(d.repo.path) // leftovers from an earlier session
	d.reload()
}

func (d *detailView) reload() {
	if d.mode == modePRs {
		d.app.loadPRs([]*repoEntry{d.repo}, true)
	}
	go func() {
		st, err := git.GetStatus(d.repo.path)
		d.app.QueueUpdateDraw(func() {
			if err != nil {
				d.app.FlashErr(err)
				return
			}
			d.repo.status, d.repo.loaded, d.repo.err = st, true, nil
			d.app.refresh()
			d.loadLeft()
		})
	}()
}

// update picks up refreshed data: the header always, a freshly loaded PR
// list, and in open-work mode the file list when it changed (keeping the
// selected file).
func (d *detailView) update() {
	if d.mode == modePRs && d.repo.prLoaded && d.renderedPRs != d.repo.prGen {
		d.renderPRs()
		return
	}
	if d.repo.err != nil {
		return
	}
	if d.mode == modeCommits {
		if d.repo.loaded && headKey(d.repo.status) != d.commitsHead {
			d.reloadCommits()
		}
		return
	}
	if d.mode != modeWork || sameFiles(d.workFiles, d.repo.status.Files) {
		return
	}
	selected := ""
	if i := d.selectedIndex(d.list); i >= 0 && i < len(d.workFiles) {
		selected = d.workFiles[i].Path
	}
	d.workFiles = d.repo.status.Files
	d.renderWork()
	for row := 1; row < d.list.GetRowCount(); row++ {
		if i, ok := d.list.GetCell(row, 0).GetReference().(int); ok && d.workFiles[i].Path == selected {
			d.list.Select(row, 0)
			break
		}
	}
}

// reloadCommits re-reads the commit list after HEAD moved, keeping the
// selected commit unless the newest one was selected: then the new newest
// commit is.
func (d *detailView) reloadCommits() {
	if row, _ := d.list.GetSelection(); row > 1 {
		if i := d.selectedIndex(d.list); i >= 0 && i < len(d.commits) {
			d.openSHA = d.commits[i].SHA
		}
	}
	d.loadLeft()
}

// headKey captures what moves the commit list: HEAD and what is pushed.
func headKey(s git.Status) string {
	return fmt.Sprintf("%s|%d|%d|%s", s.Branch, s.LastCommit.Unix(), s.Ahead, s.LastSubject)
}

// tick runs on every refresh tick. In open-work mode the content pane
// follows the file it shows: when the file changed on disk it is re-read in
// place, keeping the scroll position.
func (d *detailView) tick() {
	if d.mode != modeWork || d.rightPath == "" || !d.rightDone {
		return
	}
	path, sig, gen := d.rightPath, d.rightSig, d.rightGen
	go func() {
		if statFile(path) == sig {
			return
		}
		d.app.QueueUpdateDraw(func() {
			if gen == d.rightGen {
				d.loadRight(true)
			}
		})
	}()
}

// fileSig is what tick compares to notice a changed file.
type fileSig struct {
	exists bool
	dir    bool
	size   int64
	mod    time.Time
}

func statFile(path string) fileSig {
	if path == "" {
		return fileSig{}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fileSig{}
	}
	return fileSig{exists: true, dir: fi.IsDir(), size: fi.Size(), mod: fi.ModTime()}
}

func sameFiles(a, b []git.File) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (d *detailView) setMode(m mode) {
	if d.mode == m {
		return
	}
	d.mode = m
	d.app.ui().DetailView = m.String()
	d.app.saveGlobal()
	d.filter.reset()
	d.cur, d.curFiles = nil, nil
	d.buildLayout()
	d.setFocus(paneList)
	d.app.refresh()
	d.loadLeft()
}

func (d *detailView) cycleLayout() {
	if d.mode != modeCommits && d.mode != modePRs && d.mode != modePRCommits {
		d.app.Flash("layouts apply to commits and pull requests")
		return
	}
	d.layout = (d.layout + 1) % 3
	d.app.ui().Layout = d.layout.String()
	d.app.saveGlobal()
	d.app.Flash("layout: " + d.layout.String())
	d.buildLayout()
	d.setFocus(paneList)
	d.app.refresh()
	d.cur = nil
	d.onListChanged()
}

// --- focus -----------------------------------------------------------------

func (d *detailView) setFocus(p pane) { d.app.SetFocus(d.prim(p)) }

// onFocus runs whenever a pane gains focus (keys or mouse). In the
// content-first layout the list shows the change's message and the files
// pane shows a file; the content pane keeps whatever was shown last.
func (d *detailView) onFocus(p pane) {
	d.focus = p
	d.paintFocus()
	if p == paneContent {
		return
	}
	msg := p == paneList
	if d.msgMode != msg {
		d.msgMode = msg
		if d.threePane() && d.layout == layoutContentFirst {
			d.scheduleRight()
		}
	}
}

func (d *detailView) paintFocus() {
	for _, p := range []pane{paneList, paneFiles, paneContent} {
		color := colorBlur
		style := styleSelectedBlur
		if p == d.focus {
			color, style = colorFocus, styleSelected
		}
		switch p {
		case paneList:
			d.list.SetBorderColor(color)
			d.list.SetSelectedStyle(style)
		case paneFiles:
			d.files.SetBorderColor(color)
			d.files.SetSelectedStyle(style)
		case paneContent:
			d.content.SetBorderColor(color)
		}
	}
}

func (d *detailView) moveFocus(delta int, wrap bool) {
	i := 0
	for j, p := range d.order {
		if p == d.focus {
			i = j
		}
	}
	i += delta
	if wrap {
		i = (i + len(d.order)) % len(d.order)
	} else {
		i = max(0, min(len(d.order)-1, i))
	}
	d.setFocus(d.order[i])
}

// --- keys ------------------------------------------------------------------

func (d *detailView) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyLeft:
		d.moveFocus(-1, false)
		return nil
	case tcell.KeyRight:
		d.moveFocus(1, false)
		return nil
	case tcell.KeyTab:
		d.moveFocus(1, true)
		return nil
	case tcell.KeyBacktab:
		d.moveFocus(-1, true)
		return nil
	case tcell.KeyEnter:
		if d.focus == paneList && d.mode == modePRs {
			d.openPRCommits()
		} else if d.focus == paneList && !d.threePane() {
			d.openChange()
		}
		return nil
	case tcell.KeyUp, tcell.KeyDown:
		if ev.Modifiers()&tcell.ModShift != 0 {
			d.scroll(dirOf(ev.Key()) * 5)
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case '1', '2', '3':
			if d.mode == modeChange || d.mode == modePRCommits {
				return nil
			}
			d.setMode(map[rune]mode{'1': modeWork, '2': modeCommits, '3': modePRs}[ev.Rune()])
		case 'c':
			d.copySelected()
		case 'v':
			if d.mode == modeChange {
				return nil
			}
			d.cycleLayout()
		case 'f':
			switch d.mode {
			case modePRCommits:
				d.app.push(newChangeView(d.app, d.repo, d.fixed))
			case modePRs:
				d.openChange()
			}
		case '/':
			d.setFocus(paneList)
			d.filter.show(true)
		case 'd':
			d.showAlt[d.mode] = !d.showAlt[d.mode]
			d.scheduleRight()
		case 'm':
			if d.mode == modePRs {
				d.mineOnly = !d.mineOnly
				d.app.ui().SetMineOnly(d.mineOnly)
				d.app.saveGlobal()
				d.renderPRs()
			}
		case 'o':
			ch := d.fixed
			if d.mode != modeChange {
				ch = d.selectedChange()
			}
			if d.mode == modePRCommits && ch == nil {
				ch = d.fixed
			}
			if ch != nil {
				d.app.openURL(ch.url)
			}
		case 'r':
			if d.mode != modeChange && d.mode != modePRCommits {
				d.reload()
			}
		case 'J':
			d.scroll(5)
		case 'K':
			d.scroll(-5)
		default:
			return d.passThrough(ev)
		}
		return nil
	}
	return d.passThrough(ev)
}

// passThrough hands a key to the focused primitive, except that an empty
// table gets nothing (see swallowWhenEmpty).
func (d *detailView) passThrough(ev *tcell.EventKey) *tcell.EventKey {
	switch d.focus {
	case paneList:
		return swallowWhenEmpty(d.list, ev)
	case paneFiles:
		return swallowWhenEmpty(d.files, ev)
	}
	return ev
}

// scroll moves the focused pane by delta lines.
func (d *detailView) scroll(delta int) {
	if d.focus == paneContent {
		row, col := d.content.GetScrollOffset()
		d.content.ScrollTo(max(0, row+delta), col)
		return
	}
	t := d.list
	if d.focus == paneFiles {
		t = d.files
	}
	if t.GetRowCount() <= 1 {
		return
	}
	row, _ := t.GetSelection()
	t.Select(max(1, min(t.GetRowCount()-1, row+delta)), 0)
}

// copySelected copies the file path, commit hash or PR url under the cursor.
func (d *detailView) copySelected() {
	switch {
	case d.mode == modeWork:
		if i := d.selectedIndex(d.list); i >= 0 && i < len(d.workFiles) {
			d.app.copyText("path", d.workFiles[i].Path)
			return
		}
	case d.mode == modeChange:
		if i := d.selectedIndex(d.list); i >= 0 && i < len(d.curFiles) {
			d.app.copyText("path", d.curFiles[i].Path)
			return
		}
	case d.threePane() && d.focus == paneFiles:
		if i := d.selectedIndex(d.files); i >= 0 && i < len(d.curFiles) {
			d.app.copyText("path", d.curFiles[i].Path)
			return
		}
	case d.mode == modeCommits || d.mode == modePRCommits:
		if i := d.selectedIndex(d.list); i >= 0 && i < len(d.commits) {
			d.app.copyText("commit", d.commits[i].SHA)
			return
		}
	case d.mode == modePRs:
		if p := d.selectedPR(); p != nil {
			d.app.copyText("url", p.URL)
			return
		}
	}
	d.app.Flash("nothing to copy")
}

// openPRCommits pushes the commits of the selected pull request.
func (d *detailView) openPRCommits() {
	if ch := d.selectedChange(); ch != nil {
		d.app.push(newPRCommitsView(d.app, d.repo, ch))
	}
}

// openChange pushes the files of the selected commit or PR as its own page.
func (d *detailView) openChange() {
	ch := d.selectedChange()
	if ch == nil {
		return
	}
	d.app.push(newChangeView(d.app, d.repo, ch))
}

// --- left pane -------------------------------------------------------------

func (d *detailView) loadLeft() {
	d.leftGen++
	gen := d.leftGen
	d.list.Clear()
	d.list.SetTitle(tview.Escape(fmt.Sprintf(" %s [loading…] ", d.mode)))
	d.content.Clear()
	d.content.SetTitle("")
	d.files.Clear()
	d.files.SetTitle(" Files ")

	switch d.mode {
	case modeWork:
		d.workFiles = d.repo.status.Files
		d.renderWork()
	case modeCommits:
		d.commitsHead = headKey(d.repo.status)
		go func() {
			commits, err := git.Commits(d.repo.path, commitLimit)
			d.app.QueueUpdateDraw(func() {
				if gen != d.leftGen {
					return
				}
				if err != nil {
					d.app.FlashErr(err)
				}
				d.commits = commits
				d.renderCommits()
			})
		}()
	case modePRs:
		if d.repo.prLoaded {
			d.renderPRs()
			return
		}
		d.app.loadPRs([]*repoEntry{d.repo}, false) // update() renders it
	case modePRCommits:
		go func() {
			commits, err := d.fixed.commits()
			d.app.QueueUpdateDraw(func() {
				if gen != d.leftGen {
					return
				}
				if err != nil {
					d.app.FlashErr(err)
				}
				d.commits = commits
				d.renderCommits()
			})
		}()
	case modeChange:
		if cached, ok := d.filesCache[d.fixed.key]; ok {
			d.curFiles = cached
			d.renderChangeList()
			return
		}
		go func() {
			files, err := d.fixed.files()
			d.app.QueueUpdateDraw(func() {
				if gen != d.leftGen {
					return
				}
				if err != nil {
					d.app.FlashErr(err)
				}
				d.filesCache[d.fixed.key] = files
				d.curFiles = files
				d.renderChangeList()
			})
		}()
	}
}

// renderLeft re-renders the current mode's list from data already loaded.
func (d *detailView) renderLeft() {
	switch d.mode {
	case modeWork:
		d.renderWork()
	case modeCommits, modePRCommits:
		d.renderCommits()
	case modePRs:
		d.renderPRs()
	case modeChange:
		d.renderChangeList()
	}
}

func header(t *tview.Table, cols ...string) {
	for c, h := range cols {
		t.SetCell(0, c, tview.NewTableCell(h).SetStyle(styleHeader).SetSelectable(false))
	}
}

func (d *detailView) finishLeft(title string, count int, empty string) {
	d.list.SetTitle(tview.Escape(title + d.filter.suffix() + " "))
	if count == 0 {
		d.cur, d.curFiles = nil, nil
		d.files.Clear()
		d.files.SetTitle(" Files ")
		d.content.SetTitle("")
		d.content.SetText("[gray]" + tview.Escape(empty) + "[-]")
		return
	}
	d.list.Select(1, 0) // triggers onListChanged
	d.list.ScrollToBeginning()
}

// onListChanged follows the list selection: files pane and content.
func (d *detailView) onListChanged() {
	if d.threePane() {
		ch := d.selectedChange()
		if ch == nil || d.cur == nil || ch.key != d.cur.key {
			d.cur = ch
			d.loadFiles()
		}
	}
	d.scheduleRight()
}

func (d *detailView) renderWork() {
	d.list.Clear()
	header(d.list, "ST", "FILE")
	row := 1
	for i, f := range d.workFiles {
		name := f.Path
		if f.OrigPath != "" {
			name += " ← " + f.OrigPath
		}
		if !d.filter.match(name, f.XY) {
			continue
		}
		d.list.SetCell(row, 0, tview.NewTableCell(f.XY).SetTextColor(statusColor(f.XY)).SetReference(i))
		d.list.SetCell(row, 1, tview.NewTableCell(tview.Escape(name)).SetTextColor(colorValue).SetExpansion(1))
		row++
	}
	d.finishLeft(fmt.Sprintf(" Open Work[%d]", row-1), row-1, "working tree clean")
}

func statusColor(xy string) tcell.Color {
	switch {
	case xy == "??":
		return colorMuted
	case strings.ContainsAny(xy, "UDA") && (xy[0] == 'U' || xy[1] == 'U'):
		return colorError
	case xy[1] != '.' && xy[1] != ' ':
		return colorDirty
	default:
		return colorClean
	}
}

func changeColor(status string) tcell.Color {
	switch status {
	case "A":
		return colorClean
	case "D":
		return colorError
	case "R", "C":
		return colorTitle
	}
	return colorDirty
}

// fillChangeFiles renders a change's files into a table; match filters rows.
func fillChangeFiles(t *tview.Table, files []git.ChangedFile, match func(...string) bool) int {
	t.Clear()
	header(t, "ST", "FILE")
	row := 1
	for i, f := range files {
		name := f.Path
		if f.OrigPath != "" {
			name += " ← " + f.OrigPath
		}
		if match != nil && !match(name, f.Status) {
			continue
		}
		t.SetCell(row, 0, tview.NewTableCell(f.Status).SetTextColor(changeColor(f.Status)).SetReference(i))
		t.SetCell(row, 1, tview.NewTableCell(tview.Escape(name)).SetTextColor(colorValue).SetExpansion(1))
		row++
	}
	return row - 1
}

func (d *detailView) renderChangeList() {
	n := fillChangeFiles(d.list, d.curFiles, d.filter.match)
	d.finishLeft(fmt.Sprintf(" Files(%s)[%d]", d.fixed.label, n), n, "no files changed")
}

func (d *detailView) renderCommits() {
	d.list.Clear()
	header(d.list, "SHA", "", "AGE", "AUTHOR", "SUBJECT")
	row, selectRow := 1, 0
	for i, c := range d.commits {
		if !d.filter.match(c.Short, c.Author, c.Subject) {
			continue
		}
		if c.SHA == d.openSHA {
			selectRow = row
		}
		d.list.SetCell(row, 0, tview.NewTableCell(c.Short).SetTextColor(colorSHA).SetReference(i))
		mark := tview.NewTableCell("").SetTextColor(colorDirty)
		if c.Unpushed {
			mark.SetText("↑")
		}
		d.list.SetCell(row, 1, mark)
		d.list.SetCell(row, 2, tview.NewTableCell(age(c.When)).SetTextColor(colorMuted))
		d.list.SetCell(row, 3, tview.NewTableCell(tview.Escape(trunc(c.Author, 16))).SetTextColor(colorMuted))
		d.list.SetCell(row, 4, tview.NewTableCell(tview.Escape(c.Subject)).SetTextColor(colorValue).SetExpansion(1))
		row++
	}
	more := ""
	if d.mode == modeCommits && len(d.commits) == commitLimit {
		more = "+"
	}
	title := fmt.Sprintf(" Commits[%d%s]", row-1, more)
	if d.mode == modePRCommits {
		title = fmt.Sprintf(" Commits(%s)[%d]", d.fixed.label, row-1)
	}
	d.finishLeft(title, row-1, "no commits")
	if selectRow > 0 {
		d.list.Select(selectRow, 0)
	}
	d.openSHA = ""
}

func (d *detailView) renderPRs() {
	r := d.repo
	d.renderedPRs = r.prGen
	d.list.Clear()
	header(d.list, "#", "TITLE", "AUTHOR", "HEAD → BASE", "UPDATED")
	row, selectRow := 1, 0
	for i, p := range r.prs {
		if d.mineOnly && !p.Mine {
			continue
		}
		title := p.Title
		if p.Draft {
			title = "[draft] " + title
		}
		if !d.filter.match(strconv.Itoa(p.Number), title, p.Author, p.Head, p.Base) {
			continue
		}
		if p.Number == d.openPR {
			selectRow = row
		}
		d.list.SetCell(row, 0, tview.NewTableCell(strconv.Itoa(p.Number)).SetTextColor(colorSHA).SetReference(i))
		d.list.SetCell(row, 1, tview.NewTableCell(tview.Escape(title)).SetTextColor(colorValue).SetExpansion(1))
		d.list.SetCell(row, 2, tview.NewTableCell(tview.Escape(trunc(p.Author, 18))).SetTextColor(colorMuted))
		d.list.SetCell(row, 3, tview.NewTableCell(tview.Escape(p.Head+" → "+p.Base)).SetTextColor(colorMuted))
		d.list.SetCell(row, 4, tview.NewTableCell(age(p.Updated)).SetTextColor(colorMuted))
		row++
	}
	scope := "mine"
	if !d.mineOnly {
		scope = "all"
	}
	where := r.prRemote.Provider.String()
	if r.prRemote.Slug != "" {
		where += ":" + r.prRemote.Slug
	}
	empty := "no open pull requests"
	if r.prErr != nil {
		empty = "cannot list pull requests: " + r.prErr.Error()
	} else if d.mineOnly && len(r.prs) > 0 && d.filter.text == "" {
		empty = fmt.Sprintf("none of the %s are yours (press m to show all)", plural(len(r.prs), "open pull request"))
	}
	d.finishLeft(fmt.Sprintf(" Pull Requests(%s)[%d] %s", scope, row-1, where), row-1, empty)
	if selectRow > 0 {
		d.list.Select(selectRow, 0)
	}
	d.openPR = 0
}

// --- files pane (three-pane layouts) ----------------------------------------

func (d *detailView) loadFiles() {
	d.filesGen++
	gen := d.filesGen
	d.files.Clear()
	d.curFiles = nil
	ch := d.cur
	if ch == nil {
		d.files.SetTitle(" Files ")
		return
	}
	if cached, ok := d.filesCache[ch.key]; ok {
		d.curFiles = cached
		d.renderFiles()
		return
	}
	d.files.SetTitle(tview.Escape(fmt.Sprintf(" Files(%s) [loading…] ", ch.label)))
	go func() {
		files, err := ch.files()
		d.app.QueueUpdateDraw(func() {
			if gen != d.filesGen {
				return
			}
			if err != nil {
				d.app.FlashErr(err)
			}
			d.filesCache[ch.key] = files
			d.curFiles = files
			d.renderFiles()
		})
	}()
}

func (d *detailView) renderFiles() {
	n := fillChangeFiles(d.files, d.curFiles, nil)
	d.files.SetTitle(tview.Escape(fmt.Sprintf(" Files(%s)[%d] ", d.cur.label, n)))
	if n > 0 {
		d.files.Select(1, 0) // triggers the files selection handler
		d.files.ScrollToBeginning()
	} else {
		d.scheduleRight()
	}
}

// --- changes ---------------------------------------------------------------

func (d *detailView) selectedIndex(t *tview.Table) int {
	row, _ := t.GetSelection()
	if row < 1 {
		return -1
	}
	if i, ok := t.GetCell(row, 0).GetReference().(int); ok {
		return i
	}
	return -1
}

func (d *detailView) selectedPR() *pr.PR {
	if d.mode != modePRs {
		return nil
	}
	if i := d.selectedIndex(d.list); i >= 0 && i < len(d.repo.prs) {
		return &d.repo.prs[i]
	}
	return nil
}

// selectedChange wraps the selected commit or pull request.
func (d *detailView) selectedChange() *change {
	i := d.selectedIndex(d.list)
	switch d.mode {
	case modeCommits, modePRCommits:
		if i >= 0 && i < len(d.commits) {
			return d.commitChange(d.commits[i])
		}
	case modePRs:
		if i >= 0 && i < len(d.repo.prs) {
			return d.prChange(d.repo.prs[i])
		}
	}
	return nil
}

func (d *detailView) commitChange(c git.Commit) *change {
	dir, style := d.repo.path, d.app.syntaxStyle
	diff := func(full bool) func() (string, error) {
		return func() (string, error) {
			out, err := git.Show(dir, c.SHA, full)
			return colorizeDiff(out, style), err
		}
	}
	return &change{
		key: c.SHA, label: c.Short,
		url:   func() (string, error) { return commitURL(dir, c) },
		files: func() ([]git.ChangedFile, error) { return git.CommitFiles(dir, c.SHA) },
		fileText: func(f git.ChangedFile, full bool) (string, error) {
			out, err := git.CommitFileDiff(dir, c.SHA, f, full)
			return colorizeDiff(out, style), err
		},
		message: func() (string, error) {
			out, err := git.CommitMessage(dir, c.SHA)
			return colorizeDiff(out, style), err
		},
		primary: diff(false), alt: diff(true), labels: [2]string{"diff", "full file"},
	}
}

func (d *detailView) prChange(p pr.PR) *change {
	dir, style, remote := d.repo.path, d.app.syntaxStyle, d.repo.prRemote
	refs := func() (string, string, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if r, ok := d.prRefs[p.Number]; ok {
			return r[0], r[1], nil
		}
		base, head, err := git.FetchPR(dir, remote.Name, p.Number, p.Base)
		if err == nil {
			d.prRefs[p.Number] = [2]string{base, head}
		}
		return base, head, err
	}
	details := func() (string, error) { return prDetails(p), nil }
	return &change{
		key: fmt.Sprintf("pr-%d", p.Number), label: fmt.Sprintf("#%d", p.Number),
		files: func() ([]git.ChangedFile, error) {
			base, head, err := refs()
			if err != nil {
				return nil, err
			}
			return git.RangeFiles(dir, base, head)
		},
		fileText: func(f git.ChangedFile, full bool) (string, error) {
			base, head, err := refs()
			if err != nil {
				return "", err
			}
			out, err := git.RangeFileDiff(dir, base, head, f, full)
			return colorizeDiff(out, style), err
		},
		message: details,
		primary: details,
		url:     func() (string, error) { return p.URL, nil },
		commits: func() ([]git.Commit, error) {
			base, head, err := refs()
			if err != nil {
				return nil, err
			}
			return git.RangeCommits(dir, base, head)
		},
		alt: func() (string, error) {
			base, head, err := refs()
			if err != nil {
				return "", err
			}
			out, err := git.RangeDiff(dir, base, head)
			return colorizeDiff(out, style), err
		},
		labels: [2]string{"details", "diff"},
	}
}

// --- content pane ----------------------------------------------------------

// scheduleRight debounces content loads while the selection moves fast.
func (d *detailView) scheduleRight() { d.loadRight(false) }

// loadRight loads the content pane for the current state. With keep the
// pane is re-read in place (same scroll position, no "loading…"), for a
// file that changed under the user's eyes.
func (d *detailView) loadRight(keep bool) {
	d.rightGen++
	gen := d.rightGen
	if d.rightTimer != nil {
		d.rightTimer.Stop()
	}
	title, load, path := d.rightJob()
	d.content.SetTitle(tview.Escape(title))
	d.rightPath, d.rightDone = path, false
	if load == nil {
		d.content.SetText("")
		return
	}
	if !keep {
		d.content.SetText("[gray]loading…[-]")
	}
	row, col := d.content.GetScrollOffset()
	d.rightTimer = time.AfterFunc(60*time.Millisecond, func() {
		sig := statFile(path) // before the read: a change during it is seen next tick
		text, err := load()
		d.app.QueueUpdateDraw(func() {
			if gen != d.rightGen {
				return
			}
			if err != nil {
				text = "[red]" + tview.Escape(err.Error()) + "[-]"
			}
			d.content.SetText(text)
			d.rightSig, d.rightDone = sig, true
			if keep {
				d.content.ScrollTo(row, col)
			} else {
				d.content.ScrollToBeginning()
			}
		})
	})
}

// rightJob returns the content title, a loader for the current state and,
// for a working-tree file, its path (for tick to watch).
func (d *detailView) rightJob() (string, func() (string, error), string) {
	alt := d.showAlt[d.mode]
	fileLabel := "diff"
	if alt {
		fileLabel = "full file"
	}
	switch {
	case d.mode == modeWork:
		i := d.selectedIndex(d.list)
		if i < 0 || i >= len(d.workFiles) {
			return "", nil, ""
		}
		f := d.workFiles[i]
		style := d.app.syntaxStyle
		full := filepath.Join(d.repo.path, f.Path)
		if alt {
			return fmt.Sprintf(" %s [diff] ", f.Path), func() (string, error) {
				out, err := git.WorkDiff(d.repo.path, f)
				return colorizeDiff(out, style), err
			}, full
		}
		return fmt.Sprintf(" %s [content] ", f.Path), func() (string, error) {
			out, err := git.FileContent(d.repo.path, f.Path)
			if strings.HasPrefix(out, "(") && !strings.Contains(out, "\n") {
				return "[gray]" + tview.Escape(out) + "[-]", err
			}
			return numbered(newHighlighter(f.Path, out, style).lines(out)), err
		}, full
	case d.mode == modeChange:
		i := d.selectedIndex(d.list)
		if i < 0 || i >= len(d.curFiles) {
			return "", nil, ""
		}
		f := d.curFiles[i]
		return fmt.Sprintf(" %s [%s] ", f.Path, fileLabel), func() (string, error) { return d.fixed.fileText(f, alt) }, ""
	case !d.threePane():
		ch := d.selectedChange()
		if ch == nil {
			return "", nil, ""
		}
		if alt {
			return fmt.Sprintf(" %s [%s] ", ch.label, ch.labels[1]), ch.alt, ""
		}
		return fmt.Sprintf(" %s [%s] ", ch.label, ch.labels[0]), ch.primary, ""
	case d.layout == layoutContentFirst && d.msgMode:
		ch := d.cur
		if ch == nil {
			return "", nil, ""
		}
		return fmt.Sprintf(" %s [message] ", ch.label), ch.message, ""
	default:
		ch := d.cur
		i := d.selectedIndex(d.files)
		if ch == nil || i < 0 || i >= len(d.curFiles) {
			return "", nil, ""
		}
		f := d.curFiles[i]
		return fmt.Sprintf(" %s: %s [%s] ", ch.label, f.Path, fileLabel), func() (string, error) { return ch.fileText(f, alt) }, ""
	}
}

// commitURL resolves a commit's web page; unpushed commits have none yet.
func commitURL(dir string, c git.Commit) (string, error) {
	if c.Unpushed {
		return "", fmt.Errorf("%s is not pushed yet", c.Short)
	}
	remote, err := pr.Detect(dir)
	if err != nil {
		return "", err
	}
	if u := pr.CommitURL(remote, c.SHA); u != "" {
		return u, nil
	}
	return "", fmt.Errorf("no web url known for %s", remote.Host)
}

func prDetails(p pr.PR) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[yellow::b]#%d %s[-::-]\n\n", p.Number, tview.Escape(p.Title))
	row := func(k, v string) { fmt.Fprintf(&b, "[orange]%-9s[-] %s\n", k+":", tview.Escape(v)) }
	row("Author", p.Author)
	row("Branch", p.Head+" → "+p.Base)
	state := "open"
	if p.Draft {
		state = "draft"
	}
	row("State", state)
	row("Created", p.Created.Local().Format("2006-01-02 15:04")+" ("+age(p.Created)+")")
	row("Updated", p.Updated.Local().Format("2006-01-02 15:04")+" ("+age(p.Updated)+")")
	row("URL", p.URL)
	b.WriteString("\n[gray]" + strings.Repeat("─", 60) + "[-]\n\n")
	body := strings.ReplaceAll(strings.TrimSpace(p.Body), "\r\n", "\n")
	if body == "" {
		body = "(no description)"
	}
	b.WriteString(tview.Escape(body))
	b.WriteString("\n")
	return b.String()
}
