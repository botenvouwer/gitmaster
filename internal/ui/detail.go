package ui

import (
	"fmt"
	"strconv"
	"strings"
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
)

func (m mode) String() string {
	switch m {
	case modeCommits:
		return "commits"
	case modePRs:
		return "pull requests"
	}
	return "open work"
}

const commitLimit = 500

// detailView shows one repo with a list on the left and content on the right.
type detailView struct {
	*tview.Flex
	app   *App
	repo  *repoEntry
	mode  mode
	left  *tview.Table
	right *tview.TextView

	focusRight bool
	showAlt    [3]bool // per mode: the alternate right-pane rendering (d toggles)
	mineOnly   bool

	files     []git.File
	commits   []git.Commit
	prs       []pr.PR
	remote    pr.Remote
	remoteErr error
	prsLoaded bool

	leftGen    int
	rightGen   int
	rightTimer *time.Timer
}

func newDetail(a *App, r *repoEntry) *detailView {
	d := &detailView{Flex: tview.NewFlex(), app: a, repo: r, mineOnly: true}
	d.showAlt[modeCommits] = false // commits: diff by default, alt = full file

	d.left = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSelectedStyle(styleSelected)
	d.left.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle)
	d.left.SetSelectionChangedFunc(func(row, _ int) { d.scheduleRight() })
	d.left.SetInputCapture(d.keys)

	d.right = tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	d.right.SetBorder(true).SetTitleAlign(tview.AlignLeft).SetTitleColor(colorTitle)
	d.right.SetInputCapture(d.keys)

	d.AddItem(d.left, 0, 2, true).AddItem(d.right, 0, 3, false)
	d.setFocus(false)
	return d
}

func (d *detailView) crumbs() []string { return []string{d.repo.name, d.mode.String()} }
func (d *detailView) escape() bool     { return false }
func (d *detailView) update()          {}

func (d *detailView) focusTarget() tview.Primitive {
	if d.focusRight {
		return d.right
	}
	return d.left
}

func (d *detailView) hotkeys() []hotkey {
	keys := []hotkey{
		{"w", "open work"},
		{"c", "commits"},
		{"p", "pull requests"},
		{"←/→", "switch pane"},
		{"r", "reload"},
	}
	switch d.mode {
	case modeWork:
		keys = append(keys, hotkey{"d", "content / diff"})
	case modeCommits:
		keys = append(keys, hotkey{"d", "diff / full file"})
	case modePRs:
		keys = append(keys, hotkey{"d", "details / diff"}, hotkey{"m", "mine / all"}, hotkey{"o", "open in browser"})
	}
	return keys
}

func (d *detailView) info() [][2]string {
	s := d.repo.status
	branch := s.Branch
	if s.HasUpstream {
		branch += " → " + s.Upstream
	}
	sync := fmt.Sprintf("↑%d ↓%d", s.Ahead, s.Behind)
	return [][2]string{
		{"Repo", d.repo.name},
		{"Branch", branch},
		{"Sync", sync},
		{"Path", shortenHome(d.repo.path)},
	}
}

func (d *detailView) start() { d.reload() }

func (d *detailView) reload() {
	d.prsLoaded = false
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

func (d *detailView) setMode(m mode) {
	if d.mode == m {
		return
	}
	d.mode = m
	d.setFocus(false)
	d.app.refresh()
	d.loadLeft()
}

func (d *detailView) setFocus(right bool) {
	d.focusRight = right
	if right {
		d.left.SetBorderColor(colorBlur)
		d.left.SetSelectedStyle(styleSelectedBlur)
		d.right.SetBorderColor(colorFocus)
	} else {
		d.left.SetBorderColor(colorFocus)
		d.left.SetSelectedStyle(styleSelected)
		d.right.SetBorderColor(colorBlur)
	}
	d.app.SetFocus(d.focusTarget())
}

func (d *detailView) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyLeft:
		d.setFocus(false)
		return nil
	case tcell.KeyRight:
		d.setFocus(true)
		return nil
	case tcell.KeyTab, tcell.KeyBacktab:
		d.setFocus(!d.focusRight)
		return nil
	case tcell.KeyUp, tcell.KeyDown:
		if ev.Modifiers()&tcell.ModShift != 0 {
			d.scroll(dirOf(ev.Key()) * 5)
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'w':
			d.setMode(modeWork)
		case 'c':
			d.setMode(modeCommits)
		case 'p':
			d.setMode(modePRs)
		case 'd':
			d.showAlt[d.mode] = !d.showAlt[d.mode]
			d.scheduleRight()
		case 'm':
			if d.mode == modePRs {
				d.mineOnly = !d.mineOnly
				d.renderPRs()
			}
		case 'o':
			if p := d.selectedPR(); p != nil {
				if err := pr.OpenInBrowser(p.URL); err != nil {
					d.app.FlashErr(err)
				}
			}
		case 'r':
			d.reload()
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
// left table gets nothing (see swallowWhenEmpty).
func (d *detailView) passThrough(ev *tcell.EventKey) *tcell.EventKey {
	if d.focusRight {
		return ev
	}
	return swallowWhenEmpty(d.left, ev)
}

// scroll moves the focused pane by delta lines.
func (d *detailView) scroll(delta int) {
	if d.focusRight {
		row, col := d.right.GetScrollOffset()
		d.right.ScrollTo(max(0, row+delta), col)
		return
	}
	if d.left.GetRowCount() <= 1 {
		return
	}
	row, _ := d.left.GetSelection()
	row = max(1, min(d.left.GetRowCount()-1, row+delta))
	d.left.Select(row, 0)
}

// --- left pane -------------------------------------------------------------

func (d *detailView) loadLeft() {
	d.leftGen++
	gen := d.leftGen
	d.left.Clear()
	d.left.SetTitle(tview.Escape(fmt.Sprintf(" %s [loading…] ", d.mode)))
	d.right.Clear()
	d.right.SetTitle("")

	switch d.mode {
	case modeWork:
		d.files = d.repo.status.Files
		d.renderFiles()
	case modeCommits:
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
		if d.prsLoaded {
			d.renderPRs()
			return
		}
		go func() {
			remote, err := pr.Detect(d.repo.path)
			var prs []pr.PR
			if err == nil {
				prs, err = pr.List(d.repo.path, remote)
			}
			d.app.QueueUpdateDraw(func() {
				if gen != d.leftGen {
					return
				}
				d.remote, d.remoteErr, d.prs, d.prsLoaded = remote, err, prs, true
				if err != nil {
					d.app.FlashErr(err)
				}
				d.renderPRs()
			})
		}()
	}
}

func (d *detailView) header(cols ...string) {
	for c, h := range cols {
		cell := tview.NewTableCell(h).SetStyle(styleHeader).SetSelectable(false)
		d.left.SetCell(0, c, cell)
	}
}

func (d *detailView) finishLeft(count int, empty string) {
	if count == 0 {
		d.right.SetText("[gray]" + tview.Escape(empty) + "[-]")
		return
	}
	d.left.Select(1, 0)
	d.left.ScrollToBeginning()
	d.scheduleRight()
}

func (d *detailView) renderFiles() {
	d.left.Clear()
	d.header("ST", "FILE")
	for i, f := range d.files {
		st := tview.NewTableCell(f.XY).SetTextColor(statusColor(f.XY)).SetReference(i)
		name := f.Path
		if f.OrigPath != "" {
			name += " ← " + f.OrigPath
		}
		d.left.SetCell(i+1, 0, st)
		d.left.SetCell(i+1, 1, tview.NewTableCell(tview.Escape(name)).SetTextColor(colorValue).SetExpansion(1))
	}
	d.left.SetTitle(fmt.Sprintf(" Open Work[%d] ", len(d.files)))
	d.finishLeft(len(d.files), "working tree clean")
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

func (d *detailView) renderCommits() {
	d.left.Clear()
	d.header("SHA", "", "AGE", "AUTHOR", "SUBJECT")
	for i, c := range d.commits {
		d.left.SetCell(i+1, 0, tview.NewTableCell(c.Short).SetTextColor(colorSHA).SetReference(i))
		mark := tview.NewTableCell("").SetTextColor(colorDirty)
		if c.Unpushed {
			mark.SetText("↑")
		}
		d.left.SetCell(i+1, 1, mark)
		d.left.SetCell(i+1, 2, tview.NewTableCell(age(c.When)).SetTextColor(colorMuted))
		d.left.SetCell(i+1, 3, tview.NewTableCell(tview.Escape(trunc(c.Author, 16))).SetTextColor(colorMuted))
		d.left.SetCell(i+1, 4, tview.NewTableCell(tview.Escape(c.Subject)).SetTextColor(colorValue).SetExpansion(1))
	}
	title := fmt.Sprintf(" Commits[%d] ", len(d.commits))
	if len(d.commits) == commitLimit {
		title = fmt.Sprintf(" Commits[%d+] ", len(d.commits))
	}
	d.left.SetTitle(title)
	d.finishLeft(len(d.commits), "no commits")
}

func (d *detailView) visiblePRs() []int {
	var idx []int
	for i, p := range d.prs {
		if d.mineOnly && !p.Mine {
			continue
		}
		idx = append(idx, i)
	}
	return idx
}

func (d *detailView) renderPRs() {
	d.left.Clear()
	d.header("#", "TITLE", "AUTHOR", "HEAD → BASE", "UPDATED")
	idx := d.visiblePRs()
	for row, i := range idx {
		p := d.prs[i]
		title := p.Title
		if p.Draft {
			title = "[draft] " + title
		}
		d.left.SetCell(row+1, 0, tview.NewTableCell(strconv.Itoa(p.Number)).SetTextColor(colorSHA).SetReference(i))
		d.left.SetCell(row+1, 1, tview.NewTableCell(tview.Escape(title)).SetTextColor(colorValue).SetExpansion(1))
		d.left.SetCell(row+1, 2, tview.NewTableCell(tview.Escape(trunc(p.Author, 18))).SetTextColor(colorMuted))
		d.left.SetCell(row+1, 3, tview.NewTableCell(tview.Escape(p.Head+" → "+p.Base)).SetTextColor(colorMuted))
		d.left.SetCell(row+1, 4, tview.NewTableCell(age(p.Updated)).SetTextColor(colorMuted))
	}
	scope := "mine"
	if !d.mineOnly {
		scope = "all"
	}
	where := d.remote.Provider.String()
	if d.remote.Slug != "" {
		where += ":" + d.remote.Slug
	}
	d.left.SetTitle(fmt.Sprintf(" Pull Requests(%s)[%d] %s ", scope, len(idx), where))
	empty := "no open pull requests"
	if d.remoteErr != nil {
		empty = "cannot list pull requests: " + d.remoteErr.Error()
	} else if d.mineOnly && len(d.prs) > 0 {
		empty = fmt.Sprintf("none of the %s are yours (press m to show all)", plural(len(d.prs), "open pull request"))
	}
	d.finishLeft(len(idx), empty)
}

// --- right pane ------------------------------------------------------------

func (d *detailView) selectedIndex() int {
	row, _ := d.left.GetSelection()
	if row < 1 {
		return -1
	}
	if i, ok := d.left.GetCell(row, 0).GetReference().(int); ok {
		return i
	}
	return -1
}

func (d *detailView) selectedPR() *pr.PR {
	if d.mode != modePRs {
		return nil
	}
	if i := d.selectedIndex(); i >= 0 && i < len(d.prs) {
		return &d.prs[i]
	}
	return nil
}

// scheduleRight debounces right-pane loads while the selection moves fast.
func (d *detailView) scheduleRight() {
	d.rightGen++
	gen := d.rightGen
	if d.rightTimer != nil {
		d.rightTimer.Stop()
	}
	title, load := d.rightJob()
	d.right.SetTitle(tview.Escape(title))
	if load == nil {
		d.right.SetText("")
		return
	}
	d.right.SetText("[gray]loading…[-]")
	d.rightTimer = time.AfterFunc(60*time.Millisecond, func() {
		text, err := load()
		d.app.QueueUpdateDraw(func() {
			if gen != d.rightGen {
				return
			}
			if err != nil {
				text = "[red]" + tview.Escape(err.Error()) + "[-]"
			}
			d.right.SetText(text)
			d.right.ScrollToBeginning()
		})
	})
}

// rightJob returns the right-pane title and a loader for the current selection.
func (d *detailView) rightJob() (string, func() (string, error)) {
	i := d.selectedIndex()
	alt := d.showAlt[d.mode]
	switch d.mode {
	case modeWork:
		if i < 0 || i >= len(d.files) {
			return "", nil
		}
		f := d.files[i]
		if alt {
			return fmt.Sprintf(" %s [diff] ", f.Path), func() (string, error) {
				out, err := git.WorkDiff(d.repo.path, f)
				return colorizeDiff(out), err
			}
		}
		return fmt.Sprintf(" %s [content] ", f.Path), func() (string, error) {
			out, err := git.FileContent(d.repo.path, f.Path)
			if strings.HasPrefix(out, "(") && !strings.Contains(out, "\n") {
				return "[gray]" + tview.Escape(out) + "[-]", err
			}
			return numbered(out), err
		}
	case modeCommits:
		if i < 0 || i >= len(d.commits) {
			return "", nil
		}
		c := d.commits[i]
		label := "diff"
		if alt {
			label = "full file"
		}
		return fmt.Sprintf(" %s [%s] ", c.Short, label), func() (string, error) {
			out, err := git.Show(d.repo.path, c.SHA, alt)
			return colorizeDiff(out), err
		}
	case modePRs:
		if i < 0 || i >= len(d.prs) {
			return "", nil
		}
		p := d.prs[i]
		if alt {
			return fmt.Sprintf(" #%d [diff] ", p.Number), func() (string, error) {
				out, err := git.PRDiff(d.repo.path, d.remote.Name, p.Number, p.Base)
				return colorizeDiff(out), err
			}
		}
		return fmt.Sprintf(" #%d [details] ", p.Number), func() (string, error) {
			return prDetails(p), nil
		}
	}
	return "", nil
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
