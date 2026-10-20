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

Home: `enter` open live status, all repos or a project, `L` live status,
`n` new project, `e` edit a project's repos, `R` rename, `d` delete,
`r` rescan.

Repos: `tab` cycle repos → pull requests → live status, `0` all,
`1` unpushed commits, `2` uncommitted/untracked, `enter` open repo, `a` add
repo to a project, `x` remove from the current project, `r` rescan.

Pull-request overview: all open PRs of the repos in scope; `enter` opens the
repo on that PR, `m` mine/all, `o` open in browser, `r` reload.

Live status: your work in the repos in scope over the last 12h, 24h or 3d
(`1`, `2`, `3`; the windows are `liveWindowsHours` in the config): open work
per repo, your commits not in a pull request, your open PRs and your PRs
closed in that window. `enter` opens the repo at that item, `o` opens a
commit or PR in the browser.

The chosen view (repos, pull requests or live status), the live window, the
repo-screen mode, the layout and the PR mine/all filter are remembered
across screens and restarts.

Project repos: `space`/`enter` toggle a repo, `esc` done.

Repo: `w` open work, `c` commits, `p` pull requests, `←`/`→` switch pane,
`d` toggle content/diff (open work), diff/full file (commits), details/diff
(PRs), `m` mine/all PRs, `o` open the commit or PR in the browser, `enter` on a commit or PR
opens its files with a per-file diff, `v` cycles the layout: default
(list | content), files first (list | files | content) or content first
(list | content | files, the content shows the message while the list is
focused and the file once the files pane is).

Everywhere: `↑`/`↓` one line, `shift+↑/↓` or `J`/`K` five lines, mouse wheel
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
make install   # → ~/go/bin/gitmaster
```

Requires `git`; the pull-request view uses `gh` (GitHub) or `tea` (Gitea)
with your existing logins.
