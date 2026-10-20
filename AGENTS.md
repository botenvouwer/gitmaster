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
- **Archived repos** (archived on GitHub/Gitea) are hidden by default; `A`
  toggles them for the session, `settings.showArchived` sets the default.
  The flag comes from `gh repo view --json isArchived` / the `(archived)`
  heading of `tea repos owner/repo`, is checked in the background with a
  24h TTL, and is stored in the scan cache.
- **Live state**: every `settings.refreshSeconds` (default 1, 0 = off) the
  repos on the current screen get a fresh `git status` in the background
  (`App.refreshStatuses` in `env.go`); tables re-render only when something
  changed, the open-work file list follows the working tree. Views opt in via
  `refreshTargets()`.
- **Syntax colouring**: file content and diff context lines are coloured by
  language with chroma (`internal/ui/highlight.go`), JSON and YAML included.
  `settings.syntaxStyle` picks the chroma style (default `monokai`).
- **Repos screen**: the repos of the directory, or of one project, with branch,
  ahead/behind, staged/modified/untracked counts and a state column. Quick
  filters: all / unpushed commits / uncommitted work, plus a name filter.
  `a` adds the selected repo to a project (picker, or a new one), `x` removes
  it from the current project.
- **Project repos editor**: checklist of every repo; space/enter toggles
  membership, saved immediately.
- **Repo detail screen** with three view modes, each a two-pane layout
  (list left, content right):
  - **open work** (`w`): uncommitted + untracked files; right pane shows file
    content, `d` toggles to the diff against HEAD.
  - **commits** (`c`): `git log` of HEAD; right pane shows `git show` (diff),
    `d` toggles to full-file view (`-U999999`).
  - **pull requests** (`p`): open PRs on `origin`; right pane shows details,
    `d` toggles to the PR diff; `m` toggles mine/all; `o` opens in browser.

## Configuration

- `~/.gitmaster/config.json` (global): `settings` (`showArchived`,
  `syntaxStyle`, `refreshSeconds`) and `environments` (directories gitmaster was opened in,
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
- PR diffs are fetched into the temporary `refs/gitmaster/pr/<n>/{head,base}`
  refs and deleted afterwards; `refs/pull/<n>/head` exists on both GitHub and
  Gitea.

## Build / install

```
make install     # go install → ~/go/bin/gitmaster (on PATH)
make build       # ./gitmaster
go test ./...
```

After every code change, re-run `make install` so the binary on PATH is current.
Headless UI checks: run the binary in a detached tmux session and use
`send-keys` / `capture-pane -p`; send Escape in its own `send-keys` call.

## Gotchas

- Mouse support is on, so terminal text selection needs the terminal's
  override modifier (Option on macOS Terminal/iTerm).
- Shift+arrow depends on the terminal sending modifier escape sequences;
  `J`/`K` are the keyboard-independent fallback for 5-line moves.
- "Mine" for Gitea PRs is matched heuristically on the display name, since
  `tea pr ls` does not return the author login.
