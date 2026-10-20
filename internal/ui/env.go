package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/botenvouwer/gitmaster/internal/config"
	"github.com/botenvouwer/gitmaster/internal/git"
	"github.com/botenvouwer/gitmaster/internal/pr"
)

// repoEntry is one repo of the environment plus its (possibly cached) status.
type repoEntry struct {
	name   string
	path   string
	status git.Status
	loaded bool // fresh status fetched this session
	cached bool // status pre-filled from .gitmaster/cache.json
	err    error

	inFlight bool // a status read is running (UI goroutine only)

	archived bool      // archived on the remote
	remoteAt time.Time // when the remote was last asked

	// Open pull requests, loaded on demand and shared by the PR overview
	// and the repo screen.
	prs       []pr.PR
	prRemote  pr.Remote
	prErr     error
	prLoaded  bool
	prLoading bool
	prGen     int // bumped on every load so views can tell what they rendered

	// Live status: the user's recent commits and closed PRs.
	myCommits    []git.Commit
	commitsSince time.Time
	commitsAt    time.Time // when myCommits was last read
	closedPRs    []pr.PR
	closedSince  time.Time
	closedErr    error
	liveLoading  bool
}

// loadLive fetches, for the live status screen, each repo's open PRs, the
// user's commits since the window start and the user's PRs closed since then.
func (a *App) loadLive(repos []*repoEntry, since time.Time, force bool) {
	a.loadPRs(repos, force)
	e := a.env
	gen := e.gen
	sem := make(chan struct{}, 4)
	for _, r := range repos {
		if r.liveLoading || (!force && !r.closedSince.IsZero() && !r.closedSince.After(since) && !r.commitsSince.After(since)) {
			continue
		}
		r.liveLoading = true
		go func(r *repoEntry) {
			sem <- struct{}{}
			defer func() { <-sem }()
			commits, cerr := git.MyCommits(r.path, since)
			remote, err := pr.Detect(r.path)
			var closed []pr.PR
			if err == nil {
				closed, err = pr.ListClosed(r.path, remote, since)
			}
			a.QueueUpdateDraw(func() {
				r.liveLoading = false
				if gen != e.gen {
					return
				}
				if cerr == nil {
					r.myCommits, r.commitsSince, r.commitsAt = commits, since, time.Now()
				}
				r.closedPRs, r.closedErr, r.closedSince = closed, err, since
				a.markDirty()
			})
		}(r)
	}
}

// loadPRs fetches the open pull requests of repos that have none yet (or of
// all of them with force), a few at a time, and notifies views as each
// repo lands.
func (a *App) loadPRs(repos []*repoEntry, force bool) {
	e := a.env
	gen := e.gen
	sem := make(chan struct{}, 4)
	for _, r := range repos {
		if r.prLoading || (r.prLoaded && !force) {
			continue
		}
		r.prLoading = true
		go func(r *repoEntry) {
			sem <- struct{}{}
			defer func() { <-sem }()
			remote, err := pr.Detect(r.path)
			var prs []pr.PR
			if err == nil {
				prs, err = pr.List(r.path, remote)
			}
			a.QueueUpdateDraw(func() {
				r.prLoading = false
				if gen != e.gen {
					return
				}
				r.prRemote, r.prs, r.prErr, r.prLoaded = remote, prs, err, true
				r.prGen++
				a.markDirty()
			})
		}(r)
	}
}

// remoteTTL is how long a remote check (archived flag) stays valid.
const remoteTTL = 24 * time.Hour

// environment is the directory gitmaster was opened in: its repos, the local
// config (projects) and the scan cache. Shared by all views.
type environment struct {
	dir           string
	repos         []*repoEntry
	local         *config.Local // nil until repos were found
	cache         *config.Cache
	gen           int
	pending       int
	remotePending int
}

func (e *environment) repo(name string) *repoEntry {
	for _, r := range e.repos {
		if r.name == name {
			return r
		}
	}
	return nil
}

func (e *environment) projects() []config.Project {
	if e.local == nil {
		return nil
	}
	return e.local.Projects
}

// visible filters out archived repos unless the user asked to see them.
func (a *App) visible(repos []*repoEntry) []*repoEntry {
	if a.showArchived {
		return repos
	}
	out := make([]*repoEntry, 0, len(repos))
	for _, r := range repos {
		if !r.archived {
			out = append(out, r)
		}
	}
	return out
}

// hiddenNote describes how many of repos are hidden, or "" when none.
func (a *App) hiddenNote(repos []*repoEntry) string {
	n := len(repos) - len(a.visible(repos))
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d archived hidden)", n)
}

func (a *App) toggleArchived() {
	a.showArchived = !a.showArchived
	if a.showArchived {
		a.Flash("showing archived repos")
	} else {
		a.Flash("hiding archived repos")
	}
	a.notify()
}

// counts summarises the loaded repos.
func (e *environment) counts(repos []*repoEntry) (dirty, unpushed, loaded int) {
	for _, r := range repos {
		if !r.loaded || r.err != nil {
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
	return
}

// scan rescans the directory and refreshes every repo's status in the
// background. The first successful scan sets up <dir>/.gitmaster and records
// the directory as an environment in the global config. Remote facts are
// re-fetched when older than remoteTTL, or always with force.
func (a *App) scan(force bool) {
	e := a.env
	e.gen++
	gen := e.gen
	paths, err := git.ScanRepos(e.dir)
	if err != nil {
		a.FlashErr(err)
		return
	}
	if len(paths) == 0 {
		e.repos, e.pending = nil, 0
		a.notify()
		a.Flash("no git repositories found in " + shortenHome(e.dir))
		return
	}
	if e.local == nil {
		local, err := config.LoadLocal(e.dir)
		if err != nil {
			a.FlashErr(err)
		} else {
			e.local = local
		}
		e.cache = config.LoadCache(e.dir)
		if a.global != nil {
			a.global.TouchEnvironment(e.dir)
			if err := a.global.Save(); err != nil {
				a.FlashErr(err)
			}
		}
	}
	e.repos = nil
	for _, p := range paths {
		r := &repoEntry{name: filepath.Base(p), path: p}
		if c, ok := e.cache.Repos[r.name]; ok {
			r.status, r.cached = fromCache(c), true
			r.archived, r.remoteAt = c.Archived, c.RemoteCheckedAt
		}
		e.repos = append(e.repos, r)
	}
	e.pending = len(e.repos)
	a.notify()

	for _, r := range e.repos {
		a.fetchStatus(r, time.Time{}, func() {
			e.pending--
			if e.pending == 0 {
				a.saveCache()
			}
		})
	}
	a.checkRemotes(gen, force)
}

// checkRemotes asks each repo's remote whether it is archived, in the
// background, for repos whose last check is stale.
func (a *App) checkRemotes(gen int, force bool) {
	e := a.env
	var todo []*repoEntry
	for _, r := range e.repos {
		if force || time.Since(r.remoteAt) > remoteTTL {
			todo = append(todo, r)
		}
	}
	e.remotePending = len(todo)
	sem := make(chan struct{}, 4)
	for _, r := range todo {
		go func(r *repoEntry) {
			sem <- struct{}{}
			defer func() { <-sem }()
			archived, err := remoteArchived(r.path)
			a.QueueUpdateDraw(func() {
				if gen != e.gen {
					return
				}
				if err == nil {
					r.archived = archived
				}
				r.remoteAt = time.Now()
				e.remotePending--
				if e.remotePending == 0 {
					a.saveCache()
				}
				a.markDirty()
			})
		}(r)
	}
}

func remoteArchived(dir string) (bool, error) {
	remote, err := pr.Detect(dir)
	if err != nil {
		return false, err
	}
	return pr.Archived(dir, remote)
}

func (a *App) saveCache() {
	e := a.env
	if e.cache == nil {
		return
	}
	e.cache.ScannedAt = time.Now()
	e.cache.Repos = map[string]config.RepoCache{}
	for _, r := range e.repos {
		if (r.loaded && r.err == nil) || (!r.loaded && r.cached) {
			c := toCache(r.status)
			c.Archived, c.RemoteCheckedAt = r.archived, r.remoteAt
			e.cache.Repos[r.name] = c
		}
	}
	if err := e.cache.Save(); err != nil {
		a.FlashErr(err)
	}
}

func (a *App) saveLocal() {
	if a.env.local == nil {
		return
	}
	if err := a.env.local.Save(); err != nil {
		a.FlashErr(err)
	}
}

// addRepoToProject adds repo to the named project and persists it.
func (a *App) addRepoToProject(project, repo string) {
	if a.env.local == nil {
		return
	}
	p := a.env.local.Project(project)
	if p == nil {
		return
	}
	if p.Has(repo) {
		a.Flash(repo + " is already in " + project)
		return
	}
	p.Toggle(repo)
	a.saveLocal()
	a.Flash("added " + repo + " to " + project)
	a.notify()
}

// openURL resolves a web address in the background and opens the browser.
func (a *App) openURL(resolve func() (string, error)) {
	go func() {
		u, err := resolve()
		if err == nil {
			err = pr.OpenInBrowser(u)
		}
		if err != nil {
			a.QueueUpdateDraw(func() { a.FlashErr(err) })
		}
	}()
}

// notify tells every view on the stack that repo/project data changed.
func (a *App) notify() {
	a.dirty, a.rendered = false, time.Now()
	for _, v := range a.stack {
		v.update()
	}
	a.refresh()
}

// markDirty asks for a notify shortly: results landing in a burst (one per
// repo) are rendered once. UI goroutine only.
func (a *App) markDirty() {
	if a.dirty {
		return
	}
	a.dirty = true
	time.AfterFunc(50*time.Millisecond, func() {
		a.QueueUpdateDraw(func() {
			if a.dirty {
				a.notify()
			}
		})
	})
}

// refresher is implemented by views that show live repo state.
type refresher interface {
	refreshTargets() []*repoEntry
}

// liveRefresher is implemented by the live status screen: its commits are
// re-read too.
type liveRefresher interface {
	liveSince() time.Time
}

// ticker is implemented by views that want to check something of their own
// on every refresh tick (the repo screen follows the file on screen).
type ticker interface {
	tick()
}

// statusSem bounds the number of git processes reading status at once.
var statusSem = make(chan struct{}, 8)

const (
	// commitsEvery is how often the live status re-reads a repo's commits
	// when its status did not change (a fetch can still move the remote
	// base branches).
	commitsEvery = 10 * time.Second
	// renderEvery is the longest the tables go without a re-render, so the
	// age columns keep moving.
	renderEvery = 30 * time.Second
)

// startAutoRefresh ticks every refreshEvery on the UI goroutine.
func (a *App) startAutoRefresh() {
	if a.refreshEvery <= 0 {
		return
	}
	go func() {
		for range time.Tick(a.refreshEvery) {
			a.QueueUpdateDraw(a.refreshTick)
		}
	}()
}

// refreshTick runs on the UI goroutine: it starts a status read for every
// repo on screen that has none running, lets the view check its own
// things, and re-renders now and then so ages advance.
func (a *App) refreshTick() {
	a.refreshStatuses()
	if t, ok := a.top().(ticker); ok {
		t.tick()
	}
	if time.Since(a.rendered) >= renderEvery {
		a.notify()
	}
}

// refreshStatuses re-reads the status of the repos the top view shows.
func (a *App) refreshStatuses() {
	v, ok := a.top().(refresher)
	if !ok {
		return
	}
	var since time.Time
	if lv, ok := a.top().(liveRefresher); ok {
		since = lv.liveSince()
	}
	for _, r := range v.refreshTargets() {
		a.fetchStatus(r, since, nil)
	}
}

// fetchStatus re-reads r's status in the background and applies it on the
// UI goroutine, marking the views dirty when something the tables show
// changed. With a non-zero since the user's commits since then are re-read
// as well, when the status changed or commitsEvery passed. One read per
// repo runs at a time, so a slow or stuck repo never holds up the others;
// a hung git command is killed by git.Timeout. done, if given, runs after
// the result was applied.
func (a *App) fetchStatus(r *repoEntry, since time.Time, done func()) {
	if r.inFlight {
		return
	}
	r.inFlight = true
	e := a.env
	gen := e.gen
	prevKey := ""
	if r.loaded && r.err == nil {
		prevKey = statusKey(r.status)
	}
	readCommits := !since.IsZero() && time.Since(r.commitsAt) >= commitsEvery
	go func() {
		statusSem <- struct{}{}
		st, err := git.GetStatus(r.path)
		var commits []git.Commit
		var cerr error
		if !since.IsZero() && err == nil && (readCommits || statusKey(st) != prevKey) {
			commits, cerr = git.MyCommits(r.path, since)
			readCommits = true
		}
		<-statusSem
		a.QueueUpdateDraw(func() {
			r.inFlight = false
			if gen != e.gen {
				return
			}
			changed := !r.loaded || errText(err) != errText(r.err)
			if err == nil {
				changed = changed || statusKey(st) != statusKey(r.status)
				r.status = st
			}
			r.err, r.loaded = err, true
			if readCommits && err == nil && cerr == nil {
				changed = changed || commitsKey(commits) != commitsKey(r.myCommits)
				r.myCommits, r.commitsSince, r.commitsAt = commits, since, time.Now()
			}
			if changed {
				a.markDirty()
			}
			if done != nil {
				done()
			}
		})
	}()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func commitsKey(cs []git.Commit) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.SHA)
		if c.Unpushed {
			b.WriteByte('^')
		}
		if c.OnBase {
			b.WriteByte('=')
		}
		b.WriteString(c.Branch + "|")
	}
	return b.String()
}

// statusKey captures everything the tables show, for change detection.
func statusKey(s git.Status) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%t|%t|%d|%d|%d|%d|%d|%d|%d|%s|", s.Branch, s.Upstream, s.HasUpstream, s.HasRemote,
		s.Ahead, s.Behind, s.Staged, s.Modified, s.Untracked, s.Conflicted, s.LastCommit.Unix(), s.LastSubject)
	for _, f := range s.Files {
		b.WriteString(f.XY + f.Path + "\x01" + f.OrigPath + "\x00")
	}
	return b.String()
}

func fromCache(c config.RepoCache) git.Status {
	return git.Status{
		Branch: c.Branch, Ahead: c.Ahead, Behind: c.Behind,
		Staged: c.Staged, Modified: c.Modified, Untracked: c.Untracked,
		LastCommit: c.LastCommit, LastSubject: c.LastSubject,
	}
}

func toCache(s git.Status) config.RepoCache {
	return config.RepoCache{
		Branch: s.Branch, Ahead: s.Ahead, Behind: s.Behind,
		Staged: s.Staged, Modified: s.Modified, Untracked: s.Untracked,
		LastCommit: s.LastCommit, LastSubject: s.LastSubject,
	}
}
