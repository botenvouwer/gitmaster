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
					r.myCommits, r.commitsSince = commits, since
				}
				r.closedPRs, r.closedErr, r.closedSince = closed, err, since
				a.notify()
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
				a.notify()
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

	sem := make(chan struct{}, 8)
	for _, r := range e.repos {
		go func(r *repoEntry) {
			sem <- struct{}{}
			defer func() { <-sem }()
			st, err := git.GetStatus(r.path)
			a.QueueUpdateDraw(func() {
				if gen != e.gen {
					return
				}
				r.status, r.err, r.loaded = st, err, true
				e.pending--
				if e.pending == 0 {
					a.saveCache()
				}
				a.notify()
			})
		}(r)
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
				a.notify()
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

// notify tells every view on the stack that repo/project data changed.
func (a *App) notify() {
	for _, v := range a.stack {
		v.update()
	}
	a.refresh()
}

// refresher is implemented by views that show live repo state.
type refresher interface {
	refreshTargets() []*repoEntry
}

// startAutoRefresh re-reads git status for the repos on screen every
// refreshEvery, re-rendering only when something changed.
func (a *App) startAutoRefresh() {
	if a.refreshEvery <= 0 {
		return
	}
	go func() {
		for range time.Tick(a.refreshEvery) {
			a.QueueUpdateDraw(a.refreshStatuses)
		}
	}()
}

// liveRefresher is implemented by the live status screen: its commits are
// re-read on every tick too.
type liveRefresher interface {
	liveSince() time.Time
}

// refreshStatuses runs on the UI goroutine; the git work happens in the
// background and a tick is skipped while the previous one is still running.
func (a *App) refreshStatuses() {
	if a.refreshing || a.env.pending > 0 {
		return
	}
	v, ok := a.top().(refresher)
	if !ok {
		return
	}
	targets := v.refreshTargets()
	if len(targets) == 0 {
		return
	}
	var since time.Time
	if lv, ok := a.top().(liveRefresher); ok {
		since = lv.liveSince()
	}
	e := a.env
	gen := e.gen
	a.refreshing = true
	go func() {
		type result struct {
			status  git.Status
			err     error
			commits []git.Commit
			cerr    error
		}
		results := make([]result, len(targets))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i, r := range targets {
			wg.Add(1)
			go func(i int, r *repoEntry) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				st, err := git.GetStatus(r.path)
				res := result{status: st, err: err}
				if !since.IsZero() {
					res.commits, res.cerr = git.MyCommits(r.path, since)
				}
				results[i] = res
			}(i, r)
		}
		wg.Wait()
		a.QueueUpdateDraw(func() {
			a.refreshing = false
			if gen != e.gen {
				return
			}
			changed := false
			for i, r := range targets {
				res := results[i]
				if !since.IsZero() && res.cerr == nil && commitsKey(res.commits) != commitsKey(r.myCommits) {
					r.myCommits, r.commitsSince = res.commits, since
					changed = true
				}
				if res.err == nil && r.loaded && r.err == nil && statusKey(res.status) == statusKey(r.status) {
					continue
				}
				r.status, r.err, r.loaded = res.status, res.err, true
				changed = true
			}
			if changed {
				a.notify()
			}
		})
	}()
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
	fmt.Fprintf(&b, "%s|%s|%d|%d|%d|%d|%d|%d|%d|%s|", s.Branch, s.Upstream, s.Ahead, s.Behind,
		s.Staged, s.Modified, s.Untracked, s.Conflicted, s.LastCommit.Unix(), s.LastSubject)
	for _, f := range s.Files {
		b.WriteString(f.XY + f.Path + "\x00")
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
