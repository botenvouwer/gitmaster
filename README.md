# gitmaster

A k9s-style terminal UI for all the git repos in a directory, grouped into
projects.

```
gitmaster            # repos under the current directory
gitmaster ~/repo/x   # repos under another directory
```

## Keys

Every list: `/` filter by text (Enter keeps it, Esc clears it), `A` show or
hide repos that are archived on their remote (hidden by default).

Home: `enter` open all repos or a project, `L` live status of all repos,
`n` new project, `e` edit a project's repos, `R` rename, `d` delete,
`r` rescan.

Repos: `tab` cycle repos → pull requests → live status, `0` all,
`1` unpushed commits, `2` uncommitted/untracked, `enter` open repo, `a` add
repo to a project, `x` remove from the current project, `r` rescan.

Pull-request overview: all open PRs of the repos in scope; `enter` opens the
repo on that PR, `m` mine/all, `o` open in browser, `r` reload.

Live status: your work in the repos in scope over the last 12h, 24h or 3d
(`1`, `2`, `3`; the windows are `liveWindowsHours` in the config): open work
per repo, your local commits that are not pushed, your pushed commits that
are not in a pull request, your open PRs and your PRs closed in that
window. `enter` opens the repo at that item, `o` opens a
commit or PR in the browser.

The chosen view (repos, pull requests or live status), the live window, the
repo-screen mode, the layout and the PR mine/all filter are remembered
across screens and restarts.

Project repos: `space`/`enter` toggle a repo, `esc` done.

Repo: `1` open work, `2` commits, `3` pull requests, `←`/`→` switch pane,
`d` toggle content/diff (open work), diff/full file (commits), details/diff
(PRs), `m` mine/all PRs, `o` open the commit or PR in the browser, `enter` on a commit or PR
opens its files with a per-file diff, `v` cycles the layout: default
(list | content), files first (list | files | content) or content first
(list | content | files, the content shows the message while the list is
focused and the file once the files pane is).

Everywhere: `c` copies the selected item (commit hash, file or repo path,
PR url), `↑`/`↓` one line, `shift+↑/↓` or `J`/`K` five lines, mouse wheel
scrolls, `?` help, `esc` back, `q` back/quit, `ctrl-c` quit.

## Files

- `~/.gitmaster/config.json` — settings (`showArchived`, `syntaxStyle` for the
  code colours, any chroma style name, `refreshSeconds` for the live status
  refresh, 0 to disable) and the directories you opened.
- `<dir>/.gitmaster/config.json` — the projects defined for that directory.
- `<dir>/.gitmaster/cache.json` — last scan and remote facts (archived), shown
  until fresh data arrives.

## Install

```
make install     # → ~/go/bin/gitmaster (needs Go)
make build-all   # dist/gitmaster-<os>-<arch> for macOS and Linux, amd64/arm64
```

Runs on macOS and Linux. Requires `git`; the pull-request view uses `gh`
(GitHub) or `tea` (Gitea/Forgejo) with your existing logins. Opening in the
browser uses `open` on macOS and `xdg-open` on Linux (`$BROWSER` overrides
both). Copying uses `pbcopy` on macOS and `wl-copy`, `xclip` or `xsel` on
Linux, falling back to the terminal's OSC 52 clipboard.
