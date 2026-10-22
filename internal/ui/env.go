package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
	prAt      time.Time // when the PRs were last read from the remote this session
	prDirty   bool      // git state moved: re-read the PRs on the next tick
	prFails   int       // consecutive failed reads, for backoff
	prGen     int       // bumped on every load so views can tell what they rendered

	// Live status: the user's recent commits and closed PRs.
	myCommits    []git.Commit
	commitsSince time.Time
	commitsAt    time.Time // when myCommits was last read
	closedPRs    []pr.PR
	closedSince  time.Time
	closedAt     time.Time // when the closed PRs were last read
	closedErr    error
	liveLoading  bool
}

// loadLive fetches, for the live status screen, each repo's open PRs, the
// user's commits since the window start and the user's PRs closed since then.
// Repos whose data is already fresh this session are skipped, unless force.
func (a *App) loadLive(repos []*repoEntry, since time.Time, force bool) {
	a.liveLoad(repos, since, !force, force)
}

// pollLive re-reads the open PRs and commits of repos on screen. The closed
// PRs are only read again when they can have changed (see liveLoad).
func (a *App) pollLive(repos []*repoEntry, since time.Time) {
	a.liveLoad(repos, since, false, false)
}

// liveLoad reads open PRs, commits and, when needed, closed PRs of each repo
// in parallel and lands them in one update, so a PR that just closed is never
// in neither list (its commits would flash up as "pushed, not in a pull
// request"). A closed PR stays closed, so that list is only read when the
// window grew, an open PR vanished (it was closed or merged), now and then as
// a safety net, or on force.
func (a *App) liveLoad(repos []*repoEntry, since time.Time, onlyMissing, forceClosed bool) {
	e := a.env
	gen := e.gen
	for _, r := range repos {
		covered := !r.prAt.IsZero() && !r.closedSince.IsZero() && !r.closedSince.After(since) && !r.commitsSince.After(since)
		if r.liveLoading || (onlyMissing && covered) {
			continue
		}
		r.liveLoading, r.prDirty = true, false
		started := time.Now()
		remote0, prev := r.prRemote, openNumbers(r.prs)
		needClosed := forceClosed || r.closedSince.IsZero() || r.closedSince.After(since) || time.Since(r.closedAt) >= closedEvery
		go func(r *repoEntry) {
			liveSem <- struct{}{}
			defer func() { <-liveSem }()
			var (
				wg                sync.WaitGroup
				commits, closed   = []git.Commit(nil), []pr.PR(nil)
				open              []pr.PR
				cerr, oerr, clerr error
				remote            = remote0
				closedRead        bool
			)
			readClosed := func() {
				closedRead = true
				closed, clerr = pr.ListClosed(r.path, remote, since)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				commits, cerr = git.MyCommits(r.path, since)
			}()
			if remote.Provider == pr.None {
				remote, oerr = pr.Detect(r.path)
			}
			if oerr == nil {
				if needClosed {
					wg.Add(1)
					go func() { defer wg.Done(); readClosed() }()
				}
				open, oerr = pr.List(r.path, remote)
				if oerr == nil && !needClosed && vanished(prev, open) {
					readClosed()
				}
			}
			wg.Wait()
			a.QueueUpdateDraw(func() {
				r.liveLoading = false
				if gen != e.gen {
					return
				}
				changed := a.applyOpenPRs(r, remote, open, oerr, started)
				if cerr == nil {
					changed = changed || commitsKey(commits) != commitsKey(r.myCommits)
					r.myCommits, r.commitsSince, r.commitsAt = commits, since, time.Now()
				}
				if closedRead {
					changed = changed || errText(clerr) != errText(r.closedErr)
					r.closedErr = clerr
					if clerr == nil {
						changed = changed || r.closedSince != since || prsKey(closed) != prsKey(r.closedPRs)
						r.closedPRs, r.closedSince, r.closedAt = closed, since, time.Now()
						e.cacheDirty = true
					} else {
						r.prFails++
					}
				}
				if changed {
					a.markDirty()
				}
			})
		}(r)
	}
}

func openNumbers(prs []pr.PR) map[int]bool {
	m := make(map[int]bool, len(prs))
	for _, p := range prs {
		m[p.Number] = true
	}
	return m
}

// vanished reports whether a PR of before is missing from now: it was
// closed or merged, so the closed list must be read again.
func vanished(before map[int]bool, now []pr.PR) bool {
	if len(before) == 0 {
		return false
	}
	still := openNumbers(now)
	for n := range before {
		if !still[n] {
			return true
		}
	}
	return false
}

// applyOpenPRs stores a fresh open-PR read on r (UI goroutine) and reports
// whether anything the tables show changed; views notice through prGen. A
// failed read keeps what is known, so one hiccup never blanks the tables. at
// is when the read started: the poll interval counts from there.
func (a *App) applyOpenPRs(r *repoEntry, remote pr.Remote, prs []pr.PR, err error, at time.Time) bool {
	changed := !r.prLoaded || errText(err) != errText(r.prErr)
	if remote.Provider != pr.None {
		r.prRemote = remote
	}
	if err != nil {
		r.prFails++
	} else {
		r.prFails = 0
		changed = changed || prsKey(prs) != prsKey(r.prs)
		r.prs = prs
	}
	r.prErr, r.prLoaded, r.prAt = err, true, at
	if changed {
		r.prGen++
		a.env.cacheDirty = true
	}
	return changed
}

// loadPRs fetches the open pull requests of repos not read yet this session
// (or of all of them with force), a few at a time, and notifies views as each
// repo lands. PRs restored from the cache count as unread: they show at once
// and are revalidated.
func (a *App) loadPRs(repos []*repoEntry, force bool) {
	e := a.env
	gen := e.gen
	for _, r := range repos {
		if r.prLoading || (!r.prAt.IsZero() && !force) {
			continue
		}
		r.prLoading, r.prDirty = true, false
		started := time.Now()
		remote0 := r.prRemote
		go func(r *repoEntry) {
			liveSem <- struct{}{}
			defer func() { <-liveSem }()
			remote, err := remote0, error(nil)
			if remote.Provider == pr.None {
				remote, err = pr.Detect(r.path)
			}
			var prs []pr.PR
			if err == nil {
				prs, err = pr.List(r.path, remote)
			}
			a.QueueUpdateDraw(func() {
				r.prLoading = false
				if gen != e.gen {
					return
				}
				if a.applyOpenPRs(r, remote, prs, err, started) {
					a.markDirty()
				}
			})
		}(r)
	}
}

// watching reports whether a PR of r may appear or change any moment: the
// user has an open one, or pushed a commit a few minutes ago that is in none.
func (r *repoEntry) watching() bool {
	open := map[string]bool{}
	for _, p := range r.prs {
		if p.Mine {
			return true
		}
		open[p.Head] = true
	}
	for _, c := range r.myCommits {
		if !c.Unpushed && !c.OnBase && !open[c.Branch] && time.Since(c.When) < prHotWindow {
			return true
		}
	}
	return false
}

// prInterval is how long r's PRs may go unread: short where something is
// going on, long elsewhere, and doubling with every failed read.
func (r *repoEntry) prInterval() time.Duration {
	d := prIdleEvery
	if r.watching() {
		d = prWatchEvery
	}
	for i := 0; i < r.prFails && d < prMaxBackoff; i++ {
		d *= 2
	}
	return min(d, prMaxBackoff)
}

// prsKey captures what the PR tables show, for change detection.
func prsKey(prs []pr.PR) string {
	var b strings.Builder
	for _, p := range prs {
		fmt.Fprintf(&b, "%d|%s|%s|%s|%t|%d;", p.Number, p.Title, p.Head, p.State, p.Draft, p.Updated.Unix())
	}
	return b.String()
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
	cacheDirty    bool // PR data changed since the cache was written
	cacheSaved    time.Time
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
			if c.PRsLoaded {
				r.prRemote, r.prs, r.prLoaded = c.PRRemote, c.OpenPRs, true
				r.closedPRs, r.closedSince, r.closedAt = c.ClosedPRs, c.ClosedSince, c.ClosedAt
			}
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
	e.cacheDirty, e.cacheSaved = false, time.Now()
	e.cache.Repos = map[string]config.RepoCache{}
	for _, r := range e.repos {
		if (r.loaded && r.err == nil) || (!r.loaded && r.cached) {
			c := toCache(r.status)
			c.Archived, c.RemoteCheckedAt = r.archived, r.remoteAt
			if r.prLoaded && r.prErr == nil {
				c.PRsLoaded, c.PRRemote, c.OpenPRs = true, r.prRemote, r.prs
				c.ClosedPRs, c.ClosedSince, c.ClosedAt = withoutBodies(r.closedPRs), r.closedSince, r.closedAt
			}
			e.cache.Repos[r.name] = c
		}
	}
	if err := e.cache.Save(); err != nil {
		a.FlashErr(err)
	}
}

// withoutBodies drops the descriptions of closed PRs: they are only listed.
func withoutBodies(prs []pr.PR) []pr.PR {
	out := make([]pr.PR, len(prs))
	for i, p := range prs {
		p.Body = ""
		out[i] = p
	}
	return out
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
	// Pull requests are polled by interest, not evenly. Where one may appear
	// or change any moment (see repoEntry.watching) every prWatchEvery; other
	// repos on screen every prIdleEvery; a failing remote backs off up to
	// prMaxBackoff. A commit or push seen in the git status re-reads at once.
	prWatchEvery = 2 * time.Second
	prIdleEvery  = 10 * time.Second
	prMaxBackoff = time.Minute
	// prHotWindow is how long a pushed commit keeps its repo "watched" while
	// no PR contains it.
	prHotWindow = 10 * time.Minute
	// closedEvery is the safety-net interval for re-reading closed PRs, which
	// are otherwise only read when the window grows or an open PR vanishes.
	closedEvery = 5 * time.Minute
	// cacheEvery is the longest PR changes wait before the cache is written.
	cacheEvery = 5 * time.Second
)

// liveSem bounds the number of PR / commit reads at once.
var liveSem = make(chan struct{}, 8)

// prWatcher is implemented by views that show pull requests: while it wants
// them, the PRs of its repos are re-read as often as prInterval says.
type prWatcher interface {
	wantsPRs() bool
	refreshTargets() []*repoEntry
}

// refreshPRs re-reads the PRs of repos on screen that are due: their
// interval passed, or git state moved since the last read.
func (a *App) refreshPRs() {
	v, ok := a.top().(prWatcher)
	if !ok || !v.wantsPRs() {
		return
	}
	var due []*repoEntry
	for _, r := range v.refreshTargets() {
		if !r.prLoading && !r.liveLoading && (r.prDirty || time.Since(r.prAt)+a.refreshEvery/2 >= r.prInterval()) { // half a tick of slack: ticks are the clock
			due = append(due, r)
		}
	}
	if len(due) == 0 {
		return
	}
	if lv, ok := a.top().(liveRefresher); ok {
		a.pollLive(due, lv.liveSince())
		return
	}
	a.loadPRs(due, true)
}

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
	a.refreshPRs()
	if e := a.env; e.cacheDirty && time.Since(e.cacheSaved) >= cacheEvery {
		a.saveCache()
	}
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
				if prevKey != "" && statusKey(st) != prevKey {
					r.prDirty = true // a commit or push: a PR may follow
				}
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
