# gitmaster

A k9s-style terminal UI for all the git repos in a directory.

```
gitmaster            # repos under the current directory
gitmaster ~/repo/x   # repos under another directory
```

## Keys

Master screen: `0` all, `1` unpushed commits, `2` uncommitted/untracked,
`/` filter by name, `enter` open repo, `r` reload.

Repo screen: `w` open work, `c` commits, `p` pull requests, `←`/`→` switch
pane, `d` toggle content/diff (open work), diff/full file (commits),
details/diff (PRs), `m` mine/all PRs, `o` open PR in browser.

Everywhere: `↑`/`↓` one line, `shift+↑/↓` or `J`/`K` five lines, mouse wheel
scrolls, `?` help, `esc` back, `q` back/quit, `ctrl-c` quit.

## Install

```
make install   # → ~/go/bin/gitmaster
```

Requires `git`; the pull-request view uses `gh` (GitHub) or `tea` (Gitea)
with your existing logins.
