package ui

import (
	"testing"
	"time"

	"github.com/botenvouwer/gitmaster/internal/git"
	"github.com/botenvouwer/gitmaster/internal/pr"
)

func TestVanished(t *testing.T) {
	before := map[int]bool{1: true, 2: true}
	if vanished(before, []pr.PR{{Number: 1}, {Number: 2}, {Number: 3}}) {
		t.Error("nothing vanished, a PR was added")
	}
	if !vanished(before, []pr.PR{{Number: 2}}) {
		t.Error("PR 1 vanished (closed or merged)")
	}
	if vanished(nil, nil) {
		t.Error("no earlier PRs, nothing can vanish")
	}
}

func TestPRInterval(t *testing.T) {
	idle := &repoEntry{}
	if got := idle.prInterval(); got != prIdleEvery {
		t.Errorf("idle repo: %v, want %v", got, prIdleEvery)
	}

	fresh := git.Commit{SHA: "a", Branch: "feat", When: time.Now()}
	hot := &repoEntry{myCommits: []git.Commit{fresh}}
	if got := hot.prInterval(); got != prWatchEvery {
		t.Errorf("fresh pushed commit without PR: %v, want %v", got, prWatchEvery)
	}

	inPR := &repoEntry{myCommits: []git.Commit{fresh}, prs: []pr.PR{{Head: "feat"}}}
	if inPR.watching() {
		t.Error("a commit in someone else's PR must not make the repo hot")
	}
	mine := &repoEntry{prs: []pr.PR{{Head: "feat", Mine: true}}}
	if got := mine.prInterval(); got != prWatchEvery {
		t.Errorf("own open PR: %v, want %v", got, prWatchEvery)
	}

	old := fresh
	old.When = time.Now().Add(-2 * prHotWindow)
	if (&repoEntry{myCommits: []git.Commit{old}}).watching() {
		t.Error("an old pushed commit must not keep the repo hot")
	}

	failing := &repoEntry{prFails: 20}
	if got := failing.prInterval(); got != prMaxBackoff {
		t.Errorf("failing repo: %v, want backoff capped at %v", got, prMaxBackoff)
	}
}
