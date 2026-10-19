# gitmaster

k9s-style terminal UI (Go) for browsing all git repositories inside a directory.
Personal tool of William Loosman; lives in the `namsool` namespace.

## Purpose

- Master screen: every git repo directly under `$PWD` (or the dir given as
  argument) with branch, ahead/behind, staged/modified/untracked counts and a
  state column. Quick filters: all / unpushed commits / uncommitted work.
- Repo detail screen with three view modes, each a two-pane layout
  (list left, content right):
  - **open work** (`w`): uncommitted + untracked files; right pane shows file
    content, `d` toggles to the diff against HEAD.
  - **commits** (`c`): `git log` of HEAD; right pane shows `git show` (diff),
    `d` toggles to full-file view (`-U999999`).
  - **pull requests** (`p`): open PRs on `origin`; right pane shows details,
    `d` toggles to the PR diff; `m` toggles mine/all; `o` opens in browser.

## Structure

```
main.go            flags + bootstrap
internal/git       git CLI wrappers (status/log/show/diff, PR ref fetch)
internal/pr        PR listing via `gh` (GitHub) and `tea` (Gitea)
internal/ui        tview UI: app.go (shell/nav/help), master.go, detail.go,
                   format.go (diff colouring, ages), styles.go (palette, logo)
```

## Key dependencies

- `github.com/rivo/tview` + `github.com/gdamore/tcell/v2` (same stack as k9s).
- External binaries at runtime: `git`, and `gh` / `tea` for the PR view. All
  auth comes from the user's existing logins; nothing is stored by gitmaster.

## Conventions

- Shell out to `git` rather than using go-git, so the user's config,
  credential helpers and SSH setup are honoured.
- All blocking work runs in goroutines; UI mutation only via
  `app.QueueUpdateDraw`. Loads carry a generation counter (`leftGen`,
  `rightGen`, `loadGen`) so stale results are dropped.
- Anything rendered into a `TextView` or `TableCell` goes through
  `tview.Escape` (dynamic colour tags are on).
- Views implement the `view` interface in `internal/ui/app.go`
  (crumbs, hotkeys, info, start, focusTarget, escape) and are pushed/popped on
  the navigation stack in `App`.
- PR diffs are fetched into the temporary `refs/gitmaster/pr/<n>/{head,base}`
  refs and deleted afterwards; `refs/pull/<n>/head` exists on both GitHub and
  Gitea.

## Build / install

```
make install     # go install → ~/go/bin/gitmaster (on PATH)
make build       # ./gitmaster
```

After every code change, re-run `make install` so the binary on PATH is current.

## Gotchas

- Mouse support is on, so terminal text selection needs the terminal's
  override modifier (Option on macOS Terminal/iTerm).
- Shift+arrow depends on the terminal sending modifier escape sequences;
  `J`/`K` are the keyboard-independent fallback for 5-line moves.
- "Mine" for Gitea PRs is matched heuristically on the display name, since
  `tea pr ls` does not return the author login.
