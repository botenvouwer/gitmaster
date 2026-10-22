# gitmaster

k9s-style terminal UI (Go) for browsing all git repositories inside a directory,
grouped into projects. Personal tool of William Loosman; lives in the `namsool`
namespace.

## Purpose

- **Home screen** (entry): one row for "all repos" plus one row per project of
  this directory, with repo / uncommitted / unpushed / missing counts.
  `n` new project, `e` edit a project's repos, `R` rename, `d` delete, `r` rescan.
- **Every list** has a k9s-style `/` text filter (`internal/ui/filter.go`):
  type to narrow, Enter keeps it (shown in the title), Esc clears it.
- **`c` copies** the selected item to the clipboard (`clipboard.go`): commit
  hash, file or repo path, PR url. pbcopy on macOS; wl-copy, xclip or xsel
  on Linux; OSC 52 through the terminal as the fallback (works over SSH).
- **Platforms**: macOS and Linux are supported (Windows compiles but is
  untested). Platform differences live in `clipboard.go` and
  `pr.OpenInBrowser` (`open` / `xdg-open` / `$BROWSER`); everything else is
  plain Go, `git`, `gh` and `tea`. `make check` cross-compiles for all
  platforms in `PLATFORMS`, `make build-all` writes the binaries to `dist/`.
- **Archived repos** (archived on GitHub/Gitea) are hidden by default; `A`
  toggles them for the session, `settings.showArchived` sets the default.
  The flag comes from `gh repo view --json isArchived` / the `(archived)`
  heading of `tea repos owner/repo`, is checked in the background with a
  24h TTL, and is stored in the scan cache.
- **Live state**: every `settings.refreshSeconds` (default 1, 0 = off) the
  repos on the current screen get a fresh `git status` in the background
  (`App.refreshTick` / `fetchStatus` in `env.go`). Each repo is read on its
  own: one read in flight per repo (`repoEntry.inFlight`), at most 8 git
  processes at once, so a slow repo never holds up the others; every git
  command is killed after `git.Timeout` (2 min) and the error lands in the
  STATE column. Results that change something call `App.markDirty`, which
  coalesces into one `notify` (re-render) per 50 ms; the tables are also
  re-rendered at least every 30 s so ages advance. On the live status screen
  the user's commits are re-read when a repo's status changed or every 10 s.
  Views opt in via `refreshTargets()`; a view can implement `tick()` to check
  its own things each tick: the repo screen re-reads the open-work file
  shown in the content pane when it changed on disk (stat, scroll kept) and
  reloads the commit list when HEAD moved (`headKey`).
- **Syntax colouring**: file content and diff context lines are coloured by
  language with chroma (`internal/ui/highlight.go`), JSON and YAML included.
  `settings.syntaxStyle` picks the chroma style (default `monokai`).
- **Repos screen**: the repos of the directory, or of one project, with branch,
  ahead/behind, staged/modified/untracked counts and a state column. Quick
  filters: all / unpushed commits / uncommitted work, plus a name filter.
  `a` adds the selected repo to a project (picker, or a new one), `x` removes
  it from the current project. `tab` switches to the **pull request
  overview** of the same scope: every open PR of those repos (streamed in
  per repo, cached on the `repoEntry`), `m` mine/all, `o` browser, `enter`
  opens the repo's PR view on that PR.
- **Live status** (`tab` on the repos screen, or `L` on Home):
  the user's work in the chosen window, in four sections: open work per
  repo, local commits (not pushed), pushed commits not in a pull request
  (the user's non-merge commits on any local branch that no remote base
  branch contains yet; unpushed ones always show, pushed ones are left out
  when their branch is the head of an open PR, or of a PR that closed after
  the commit was made), own open PRs
  updated in the window, own
  PRs closed in the window (`enter` opens the browser for those). Windows
  come from `settings.liveWindowsHours` (default 12, 24, 72), picked with
  the number keys and remembered in `ui.liveHours`. Data: `git.MyCommits`,
  `pr.ListClosed` and the shared PR cache, loaded by `App.loadLive`; the
  periodic refresh re-reads the commits too. **PRs are polled by interest**
  (`App.refreshPRs`, only while a view shows PRs): a repo where a PR may
  appear or change (the user has an open one, or pushed a commit under 10 min
  ago that is in none: `repoEntry.watching`) every 2 s, other repos every
  10 s, doubling per failed read up to 1 min. A change in a repo's git status
  (commit, push) re-reads its PRs on the next tick (`prDirty`) — gitmaster
  derives its signal from the local status poll instead of git hooks (git has
  no post-push hook, `gh`/`tea` have none for PRs made in the web UI).
  Closed PRs never change, so they are read when the window grows, when an
  open PR vanishes from the open list, and every 5 min as a safety net.
  Open PRs, commits and closed PRs land in one update (`App.liveLoad`) and a
  failed read keeps the old data. PRs are cached in `.gitmaster/cache.json`
  (`RepoCache.OpenPRs/ClosedPRs`) and shown on the first frame, then
  revalidated; pushed commits stay hidden until a repo's PR state is known,
  so nothing flashes under "not in a pull request".
- **Remembered views**: the master view (repos/pulls/live), the repo-screen
  mode (open work/commits/pull requests), the layout and the PR mine/all
  filter (one setting shared by the overview and the repo's PR view) are
  stored in the global config's `ui` section whenever they are switched, so new screens
  and the next start open in the same view.
- **Project repos editor**: checklist of every repo; space/enter toggles
  membership, saved immediately.
- **Repo detail screen** with three view modes, each a two-pane layout
  (list left, content right):
  - **open work** (`1`): uncommitted + untracked files; right pane shows file
    content, `d` toggles to the diff against HEAD.
  - **commits** (`2`): `git log` of HEAD; right pane shows `git show` (diff),
    `d` toggles to full-file view (`-U999999`).
  - **pull requests** (`3`): open PRs on `origin`; right pane shows details,
    `d` toggles to the PR diff; `m` toggles mine/all; `o` opens in browser.
    Enter pushes the **commits of the PR** (`modePRCommits`,
    `newPRCommitsView`: `git log base..head` on the PR refs via
    `change.commits`), which behaves like the commits mode: diff on the
    right, `d` full file, `v` layouts, Enter the files of one commit. `f`
    (there and in the PR list) pushes the files of the whole PR.
  - Commits and PRs are "changes" (`change` struct in `detail.go`): a file
    list (`--name-status`), a per-file diff / full view and a message view.
    `v` cycles the **layout**, remembered in `ui.layout`:
    *default* = list | content, Enter pushes a files | content page for the
    change (`newChangeView`, mode `modeChange`);
    *files first* = list | files | content;
    *content first* = list | content (65%) | files, where the content shows
    the change's message while the list was focused last and the selected
    file once the files pane was focused (`msgMode`).
    PR refs are fetched once per PR into `refs/gitmaster/pr/<n>/` and deleted
    when the repo screen is popped (`stop()`, called by `App.pop`).

## Configuration

- `~/.gitmaster/config.json` (global): `settings` (`showArchived`,
  `syntaxStyle`, `refreshSeconds`, `liveWindowsHours`), `ui` (last used
  views and live window) and `environments` (directories gitmaster was opened in,
  most recent first). Created on first start and re-saved so new settings
  appear with their defaults.
- `<dir>/.gitmaster/config.json` (local): `projects` — name + list of repo
  directory names. Created the first time a directory with repos is opened.
  A directory without repos gets no `.gitmaster` and the home screen says so.
- `<dir>/.gitmaster/cache.json`: last scan result per repo (status plus the
  remote's archived flag and when it was checked), used to pre-fill the
  tables (state "cached") until the fresh status arrives.

## Structure

```
main.go            flags + bootstrap
internal/config    global / local config + scan cache (JSON)
internal/git       git CLI wrappers (status/log/show/diff, PR ref fetch)
internal/pr        PR listing via `gh` (GitHub) and `tea` (Gitea)
internal/ui        tview UI: app.go (shell/nav/help), env.go (repo scan + remote
                   checks shared by all views), home.go, master.go, members.go,
                   detail.go, filter.go ("/" filter box), highlight.go (chroma),
                   modal.go (prompt/pick/confirm), table.go (empty-table guard),
                   format.go (diff colouring, ages), styles.go (palette, logo)
```

## Key dependencies

- `github.com/rivo/tview` + `github.com/gdamore/tcell/v2` (same stack as k9s).
- `github.com/alecthomas/chroma/v2` for syntax colouring.
- External binaries at runtime: `git`, and `gh` / `tea` for the PR view. All
  auth comes from the user's existing logins; nothing is stored by gitmaster.

## Conventions

- Shell out to `git` rather than using go-git, so the user's config,
  credential helpers and SSH setup are honoured.
- The repo list and statuses live in `App.env` (`internal/ui/env.go`) and are
  loaded once by `App.scan()`; views render from it and are told about changes
  through `view.update()` (`App.notify()`).
- All blocking work runs in goroutines; UI mutation only via
  `app.QueueUpdateDraw`. Loads carry a generation counter so stale results
  are dropped.
- Anything rendered into a `TextView`, `TableCell` or box title goes through
  `tview.Escape` (dynamic colour tags are on; `[x]` or `[diff]` would vanish).
- Views implement the `view` interface in `internal/ui/app.go` and are
  pushed/popped on the navigation stack in `App`. Modal dialogs come from
  `modal.go` and are closed by Esc in `globalKeys`.
- Never let a navigation key reach a tview `Table` that only has its header
  row: tview v0.42 loops forever (see `table.go`).
- PR diffs come from the temporary `refs/gitmaster/pr/<n>/{head,base}` refs
  (`git.FetchPR`), removed by `git.CleanPRRefs` when the repo screen closes
  and on its next open; `refs/pull/<n>/head` exists on both GitHub and Gitea.

## Updates

`gitmaster update [--check] [--auto on|off]` and the start-up check live in
`update.go` (main) and `internal/update` (GitHub release lookup, SHA-256
verified download, atomic replace, `Restart` via exec on unix).
`settings.autoUpdate` (off by default) makes start check at most hourly
(`update.lastCheck` in the global config, 3 s timeout, never blocks start),
install and re-exec with `GITMASTER_NO_UPDATE=1` so it cannot loop. Only
release versions (`vX.Y.Z`) update: `make install` builds
(`v0.1.0-3-gabc-dirty`) and `go run` never do. A `v*` tag push runs
`.github/workflows/release.yml` (`make release`: `dist/gitmaster-<os>-<arch>`
+ `checksums.txt`; the asset names are what `update.AssetName` expects).
`GITMASTER_UPDATE_API` points the lookup at another server (used in tests).

## Build / install

```
make install     # go install → ~/go/bin/gitmaster (on PATH)
make build       # ./gitmaster
make check       # vet + tests + cross-compile for darwin/linux, amd64/arm64
make build-all   # dist/gitmaster-<os>-<arch>
```

After every code change, re-run `make install` so the binary on PATH is current.
Headless UI checks: run the binary in a detached tmux session and use
`send-keys` / `capture-pane -p`; send Escape in its own `send-keys` call.

## Gotchas

- Mouse support is on, so terminal text selection needs the terminal's
  override modifier (Option on macOS Terminal/iTerm). Trackpads: tview fires
  a click only when press and release hit the same cell, which a trackpad
  press often misses, so `App.mouseCapture` turns a release within 3
  columns / 1 row of the press into a click at the press position; and
  since tview's wheel handling only scrolls the viewport, `selectOnScroll`
  (`table.go`, applied to every table) moves the selection instead.
- Shift+arrow depends on the terminal sending modifier escape sequences;
  `J`/`K` are the keyboard-independent fallback for 5-line moves.
- "Mine" for Gitea PRs is matched heuristically on the display name, since
  `tea pr ls` does not return the author login.
