// Package git wraps the git CLI. Everything goes through the user's own git
// binary and config so credentials, SSH keys and helpers all keep working.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxOutput caps the size of file/diff output shown in the UI.
const maxOutput = 4 << 20

// Status is the summarised state of a working tree.
type Status struct {
	Branch      string
	Upstream    string
	HasUpstream bool
	HasRemote   bool
	Ahead       int // commits not on the upstream (or on any remote if no upstream)
	Behind      int
	Staged      int
	Modified    int
	Untracked   int
	Conflicted  int
	Files       []File
	LastCommit  time.Time
	LastSubject string
}

// Dirty reports whether there is uncommitted or untracked work.
func (s Status) Dirty() bool {
	return s.Staged+s.Modified+s.Untracked+s.Conflicted > 0
}

// File is one entry of `git status`.
type File struct {
	Path      string
	OrigPath  string // for renames/copies
	XY        string // two-letter porcelain code, "??" for untracked
	Untracked bool
}

// Commit is one entry of `git log`.
type Commit struct {
	SHA      string
	Short    string
	Author   string
	When     time.Time
	Subject  string
	Unpushed bool
	Branch   string // the local branch the commit was found on (MyCommits)
	OnBase   bool   // reachable from a remote base branch, i.e. already merged (MyCommits)
}

// ScanRepos returns the immediate subdirectories of root that are git repos.
func ScanRepos(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			repos = append(repos, p)
		}
	}
	sort.Strings(repos)
	return repos, nil
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-c", "color.ui=never"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		// `git diff` family exits 1 when there are differences.
		if errors.As(err, &ee) && ee.ExitCode() == 1 && len(args) > 0 && args[0] == "diff" && errb.Len() == 0 {
			return out.String(), nil
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}

// GetStatus runs `git status --porcelain=v2` and summarises it.
func GetStatus(dir string) (Status, error) {
	var s Status
	out, err := run(dir, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return s, err
	}
	fields := strings.Split(out, "\x00")
	hasCommits := true
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		switch {
		case strings.HasPrefix(f, "# branch.oid "):
			hasCommits = strings.TrimPrefix(f, "# branch.oid ") != "(initial)"
		case strings.HasPrefix(f, "# branch.head "):
			s.Branch = strings.TrimPrefix(f, "# branch.head ")
		case strings.HasPrefix(f, "# branch.upstream "):
			s.Upstream = strings.TrimPrefix(f, "# branch.upstream ")
			s.HasUpstream = true
		case strings.HasPrefix(f, "# branch.ab "):
			parts := strings.Fields(strings.TrimPrefix(f, "# branch.ab "))
			if len(parts) == 2 {
				s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(parts[0], "+"))
				s.Behind, _ = strconv.Atoi(strings.TrimPrefix(parts[1], "-"))
			}
		case strings.HasPrefix(f, "#"):
		case strings.HasPrefix(f, "? "):
			s.Untracked++
			s.Files = append(s.Files, File{Path: f[2:], XY: "??", Untracked: true})
		case strings.HasPrefix(f, "! "):
		default:
			parts := strings.SplitN(f, " ", 2)
			if len(parts) != 2 || len(parts[1]) < 2 {
				continue
			}
			xy := parts[1][:2]
			file := File{XY: xy}
			switch parts[0] {
			case "1":
				cols := strings.SplitN(f, " ", 9)
				file.Path = cols[len(cols)-1]
			case "2":
				cols := strings.SplitN(f, " ", 10)
				file.Path = cols[len(cols)-1]
				if i+1 < len(fields) {
					i++
					file.OrigPath = fields[i]
				}
			case "u":
				cols := strings.SplitN(f, " ", 11)
				file.Path = cols[len(cols)-1]
				s.Conflicted++
				s.Files = append(s.Files, file)
				continue
			default:
				continue
			}
			if xy[0] != '.' {
				s.Staged++
			}
			if xy[1] != '.' {
				s.Modified++
			}
			s.Files = append(s.Files, file)
		}
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Path < s.Files[j].Path })

	if remotes, err := run(dir, "remote"); err == nil && strings.TrimSpace(remotes) != "" {
		s.HasRemote = true
	}
	if !s.HasUpstream && s.HasRemote && hasCommits {
		if n, err := run(dir, "rev-list", "--count", "HEAD", "--not", "--remotes"); err == nil {
			s.Ahead, _ = strconv.Atoi(strings.TrimSpace(n))
		}
	}
	if hasCommits {
		if out, err := run(dir, "log", "-1", "--format=%ct%x1f%s"); err == nil {
			parts := strings.SplitN(strings.TrimSpace(out), "\x1f", 2)
			if len(parts) == 2 {
				ts, _ := strconv.ParseInt(parts[0], 10, 64)
				s.LastCommit = time.Unix(ts, 0)
				s.LastSubject = parts[1]
			}
		}
	}
	return s, nil
}

// Commits lists the most recent commits on HEAD, flagging those not on any remote.
func Commits(dir string, limit int) ([]Commit, error) {
	out, err := run(dir, "log", fmt.Sprintf("-n%d", limit), "--format=%H%x1f%h%x1f%an%x1f%ct%x1f%s%x1e")
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, err
	}
	unpushed := map[string]bool{}
	if rl, err := run(dir, "rev-list", "HEAD", "--not", "--remotes"); err == nil {
		for _, sha := range strings.Fields(rl) {
			unpushed[sha] = true
		}
	}
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		p := strings.SplitN(rec, "\x1f", 5)
		if len(p) != 5 {
			continue
		}
		ts, _ := strconv.ParseInt(p[3], 10, 64)
		commits = append(commits, Commit{
			SHA: p[0], Short: p[1], Author: p[2], When: time.Unix(ts, 0), Subject: p[4],
			Unpushed: unpushed[p[0]],
		})
	}
	return commits, nil
}

// baseBranches are the remote branches that pull requests usually target;
// commits reachable from them count as merged.
var baseBranches = []string{"master", "main", "develop", "development"}

// MyCommits lists the current user's non-merge commits on any local branch
// since the given time, newest first, each tagged with the branch it was
// reached from and whether a remote base branch already contains it.
func MyCommits(dir string, since time.Time) ([]Commit, error) {
	args := []string{"log", "--branches", "--source", "--no-merges",
		"--since=" + strconv.FormatInt(since.Unix(), 10),
		"--format=%H%x1f%h%x1f%an%x1f%ct%x1f%s%x1f%S%x1e"}
	authors := 0
	for _, key := range []string{"user.email", "user.name"} {
		if v, _ := run(dir, "config", key); strings.TrimSpace(v) != "" {
			args = append(args, "--author="+regexp.QuoteMeta(strings.TrimSpace(v)))
			authors++
		}
	}
	if authors == 0 {
		return nil, fmt.Errorf("git user not configured")
	}
	out, err := run(dir, args...)
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, err
	}
	unpushed := map[string]bool{}
	if rl, err := run(dir, "rev-list", "--branches", "--not", "--remotes"); err == nil {
		for _, sha := range strings.Fields(rl) {
			unpushed[sha] = true
		}
	}
	onBase := map[string]bool{}
	baseArgs := []string{"rev-list", "--ignore-missing", "--since=" + strconv.FormatInt(since.Unix(), 10)}
	if head, err := run(dir, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil && strings.TrimSpace(head) != "" {
		baseArgs = append(baseArgs, strings.TrimSpace(head))
	}
	for _, b := range baseBranches {
		baseArgs = append(baseArgs, "refs/remotes/origin/"+b)
	}
	if rl, err := run(dir, baseArgs...); err == nil {
		for _, sha := range strings.Fields(rl) {
			onBase[sha] = true
		}
	}
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		p := strings.SplitN(rec, "\x1f", 6)
		if len(p) != 6 {
			continue
		}
		ts, _ := strconv.ParseInt(p[3], 10, 64)
		commits = append(commits, Commit{
			SHA: p[0], Short: p[1], Author: p[2], When: time.Unix(ts, 0), Subject: p[4],
			Unpushed: unpushed[p[0]], Branch: strings.TrimPrefix(p[5], "refs/heads/"), OnBase: onBase[p[0]],
		})
	}
	return commits, nil
}

// Show returns a commit's header, stat and patch. With full=true the patch
// carries unlimited context so every touched file is shown in its entirety.
func Show(dir, sha string, full bool) (string, error) {
	args := []string{"show", "--stat", "--patch", "--format=medium"}
	if full {
		args = append(args, "-U999999")
	}
	args = append(args, sha)
	out, err := run(dir, args...)
	return truncate(out), err
}

// WorkDiff returns the diff of an uncommitted file against HEAD; untracked
// files are diffed against /dev/null so they show up as pure additions.
func WorkDiff(dir string, f File) (string, error) {
	var out string
	var err error
	if f.Untracked {
		out, err = run(dir, "diff", "--no-index", "--", os.DevNull, f.Path)
	} else {
		out, err = run(dir, "diff", "HEAD", "--", f.Path)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		out = "(no differences)"
	}
	return truncate(out), nil
}

// FileContent reads a working-tree file. Directories (untracked dirs appear
// as a single "dir/" entry) are listed instead.
func FileContent(dir, path string) (string, error) {
	full := filepath.Join(dir, path)
	fi, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "(deleted)", nil
		}
		return "", err
	}
	if fi.IsDir() {
		var b strings.Builder
		filepath.WalkDir(full, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			if d.IsDir() {
				rel += "/"
			}
			b.WriteString(rel + "\n")
			return nil
		})
		return b.String(), nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return fmt.Sprintf("(binary file, %d bytes)", len(data)), nil
	}
	return truncate(string(data)), nil
}

// ChangedFile is one file touched by a commit or a pull request.
type ChangedFile struct {
	Status   string // A, M, D, R, C, T, U
	Path     string
	OrigPath string // for renames/copies
}

// Paths returns the pathspec that selects this file's diff.
func (f ChangedFile) Paths() []string {
	if f.OrigPath != "" {
		return []string{f.Path, f.OrigPath}
	}
	return []string{f.Path}
}

// parseNameStatus reads `--name-status -z` output.
func parseNameStatus(out string) []ChangedFile {
	toks := strings.Split(out, "\x00")
	var files []ChangedFile
	for i := 0; i < len(toks); {
		st := toks[i]
		i++
		if st == "" || i >= len(toks) {
			continue
		}
		f := ChangedFile{Status: st[:1], Path: toks[i]}
		i++
		if (st[0] == 'R' || st[0] == 'C') && i < len(toks) {
			f.OrigPath, f.Path = f.Path, toks[i]
			i++
		}
		files = append(files, f)
	}
	return files
}

// CommitFiles lists the files touched by a commit.
func CommitFiles(dir, sha string) ([]ChangedFile, error) {
	out, err := run(dir, "show", "--name-status", "--format=", "-M", "-z", sha)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// CommitFileDiff returns one file's patch in a commit; full shows the whole
// file with the changes inline.
func CommitFileDiff(dir, sha string, f ChangedFile, full bool) (string, error) {
	args := []string{"show", "--format=", "-M"}
	if full {
		args = append(args, "-U999999")
	}
	args = append(append(args, sha, "--"), f.Paths()...)
	out, err := run(dir, args...)
	return truncate(out), err
}

// CommitMessage returns a commit's header, message and diffstat.
func CommitMessage(dir, sha string) (string, error) {
	return run(dir, "show", "--stat", "--format=medium", sha)
}

// FetchPR fetches a pull request's head and base into a private ref
// namespace and returns the two refs. Call CleanPRRefs when done.
func FetchPR(dir, remote string, number int, base string) (baseRef, headRef string, err error) {
	headRef = fmt.Sprintf("refs/gitmaster/pr/%d/head", number)
	baseRef = fmt.Sprintf("refs/gitmaster/pr/%d/base", number)
	_, err = run(dir, "fetch", "-q", remote,
		fmt.Sprintf("+refs/pull/%d/head:%s", number, headRef),
		fmt.Sprintf("+refs/heads/%s:%s", base, baseRef))
	return baseRef, headRef, err
}

// CleanPRRefs removes every ref FetchPR created in the repo.
func CleanPRRefs(dir string) {
	out, _ := run(dir, "for-each-ref", "--format=%(refname)", "refs/gitmaster/")
	for _, ref := range strings.Fields(out) {
		run(dir, "update-ref", "-d", ref)
	}
}

// RangeFiles lists the files changed between base and head (merge-base diff).
func RangeFiles(dir, baseRef, headRef string) ([]ChangedFile, error) {
	out, err := run(dir, "diff", "--name-status", "-M", "-z", baseRef+"..."+headRef)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// RangeFileDiff returns one file's patch between base and head.
func RangeFileDiff(dir, baseRef, headRef string, f ChangedFile, full bool) (string, error) {
	args := []string{"diff", "-M"}
	if full {
		args = append(args, "-U999999")
	}
	args = append(append(args, baseRef+"..."+headRef, "--"), f.Paths()...)
	out, err := run(dir, args...)
	return truncate(out), err
}

// RangeDiff returns the stat and patch between base and head.
func RangeDiff(dir, baseRef, headRef string) (string, error) {
	out, err := run(dir, "diff", "--stat", "--patch", baseRef+"..."+headRef)
	return truncate(out), err
}

func truncate(s string) string {
	if len(s) > maxOutput {
		return s[:maxOutput] + "\n\n(output truncated)\n"
	}
	return s
}
