package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatusAndCommits(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0o755)
	os.Mkdir(filepath.Join(root, "notarepo"), 0o755)
	mustGit(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "a.txt", "one\n")
	write(t, repo, "b.txt", "two\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-q", "-m", "first")

	repos, err := ScanRepos(root)
	if err != nil || len(repos) != 1 || repos[0] != repo {
		t.Fatalf("ScanRepos = %v, %v", repos, err)
	}

	write(t, repo, "a.txt", "changed\n") // unstaged modification
	write(t, repo, "c.txt", "new\n")     // untracked
	write(t, repo, "d.txt", "staged\n")  // staged addition
	mustGit(t, repo, "add", "d.txt")
	mustGit(t, repo, "mv", "b.txt", "e.txt") // staged rename

	s, err := GetStatus(repo)
	if err != nil {
		t.Fatal(err)
	}
	if s.Branch != "main" || s.Staged != 2 || s.Modified != 1 || s.Untracked != 1 || s.HasRemote {
		t.Fatalf("unexpected status %+v", s)
	}
	if len(s.Files) != 4 || s.LastSubject != "first" {
		t.Fatalf("unexpected files %+v", s.Files)
	}
	var renamed *File
	for i := range s.Files {
		if s.Files[i].Path == "e.txt" {
			renamed = &s.Files[i]
		}
	}
	if renamed == nil || renamed.OrigPath != "b.txt" {
		t.Fatalf("rename not parsed: %+v", s.Files)
	}

	commits, err := Commits(repo, 10)
	if err != nil || len(commits) != 1 || commits[0].Subject != "first" || !commits[0].Unpushed {
		t.Fatalf("Commits = %+v, %v", commits, err)
	}
	show, err := Show(repo, commits[0].SHA, false)
	if err != nil || len(show) == 0 {
		t.Fatalf("Show: %v", err)
	}
	diff, err := WorkDiff(repo, File{Path: "c.txt", Untracked: true})
	if err != nil || len(diff) == 0 {
		t.Fatalf("WorkDiff untracked: %v", err)
	}
	content, err := FileContent(repo, "a.txt")
	if err != nil || content != "changed\n" {
		t.Fatalf("FileContent = %q, %v", content, err)
	}
}
