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

Home: `enter` open all repos or a project, `n` new project, `e` edit a
project's repos, `R` rename, `d` delete, `r` rescan.

Repos: `tab` switch to the pull-request overview and back, `0` all,
`1` unpushed commits, `2` uncommitted/untracked, `enter` open repo, `a` add
repo to a project, `x` remove from the current project, `r` rescan.

Pull-request overview: all open PRs of the repos in scope; `enter` opens the
repo on that PR, `m` mine/all, `o` open in browser, `r` reload.

The chosen view (repos or pull requests), the repo-screen mode and the PR
mine/all filter are remembered across screens and restarts.

Project repos: `space`/`enter` toggle a repo, `esc` done.

Repo: `w` open work, `c` commits, `p` pull requests, `←`/`→` switch pane,
`d` toggle content/diff (open work), diff/full file (commits), details/diff
(PRs), `m` mine/all PRs, `o` open PR in browser.

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
