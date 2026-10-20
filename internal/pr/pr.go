// Package pr lists pull requests through the CLIs the user is already logged
// in with: `gh` for GitHub and `tea` for Gitea.
package pr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Provider identifies which CLI serves a remote.
type Provider int

const (
	None Provider = iota
	GitHub
	Gitea
)

func (p Provider) String() string {
	switch p {
	case GitHub:
		return "github"
	case Gitea:
		return "gitea"
	}
	return "unsupported"
}

// Remote describes the remote a repo's pull requests live on.
type Remote struct {
	Name     string // git remote name, e.g. origin
	Host     string
	Slug     string // owner/repo
	Provider Provider
	Login    string // our own user name on that host
	WebURL   string // https://host/owner/repo
}

// CommitURL is the web page of a commit (same path on GitHub and Gitea/Forgejo).
func CommitURL(r Remote, sha string) string {
	if r.WebURL == "" {
		return ""
	}
	return r.WebURL + "/commit/" + sha
}

type teaLogin struct {
	user string
	url  string
}

// PR is one pull request.
type PR struct {
	Number  int
	Title   string
	Author  string
	Head    string
	Base    string
	URL     string
	Body    string
	Draft   bool
	State   string // open, closed or merged
	Created time.Time
	Updated time.Time
	Closed  time.Time // closed/merged PRs only
	Mine    bool
}

var (
	ghLoginOnce sync.Once
	ghLogin     string
	teaOnce     sync.Once
	teaLogins   map[string]teaLogin // host or ssh host -> login
)

// Detect figures out which provider and login serve the repo's "origin".
func Detect(dir string) (Remote, error) {
	r := Remote{Name: "origin"}
	out, err := runCmd(dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return r, fmt.Errorf("no origin remote")
	}
	r.Host, r.Slug = parseRemote(strings.TrimSpace(out))
	if r.Host == "" {
		return r, fmt.Errorf("cannot parse remote url %q", strings.TrimSpace(out))
	}
	if strings.EqualFold(r.Host, "github.com") {
		r.Provider = GitHub
		r.WebURL = "https://github.com/" + r.Slug
		ghLoginOnce.Do(func() {
			if l, err := runCmd(dir, "gh", "api", "user", "-q", ".login"); err == nil {
				ghLogin = strings.TrimSpace(l)
			}
		})
		r.Login = ghLogin
		return r, nil
	}
	teaOnce.Do(func() {
		teaLogins = map[string]teaLogin{}
		out, err := runCmd(dir, "tea", "logins", "list", "-o", "json")
		if err != nil {
			return
		}
		var logins []struct {
			URL     string `json:"url"`
			SSHHost string `json:"ssh_host"`
			User    string `json:"user"`
		}
		if json.Unmarshal([]byte(out), &logins) != nil {
			return
		}
		for _, l := range logins {
			login := teaLogin{user: l.User, url: strings.TrimRight(l.URL, "/")}
			if u, err := url.Parse(l.URL); err == nil {
				teaLogins[strings.ToLower(u.Host)] = login
			}
			if l.SSHHost != "" {
				teaLogins[strings.ToLower(l.SSHHost)] = login
			}
		}
	})
	if login, ok := teaLogins[strings.ToLower(r.Host)]; ok {
		r.Provider = Gitea
		r.Login = login.user
		r.WebURL = login.url + "/" + r.Slug
		return r, nil
	}
	return r, fmt.Errorf("no gh or tea login for host %s", r.Host)
}

var scpRemote = regexp.MustCompile(`^[^@]+@([^:]+):(.+)$`)

func parseRemote(raw string) (host, slug string) {
	if m := scpRemote.FindStringSubmatch(raw); m != nil && !strings.Contains(raw, "://") {
		host, slug = m[1], m[2]
	} else if u, err := url.Parse(raw); err == nil {
		host, slug = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	}
	slug = strings.TrimSuffix(strings.TrimSuffix(slug, "/"), ".git")
	return host, slug
}

// List returns the open pull requests on the remote.
func List(dir string, r Remote) ([]PR, error) {
	return list(dir, r, "open", 100)
}

// ListClosed returns the user's own pull requests closed or merged since the
// given time (the remote returns the most recently updated ones first).
func ListClosed(dir string, r Remote, since time.Time) ([]PR, error) {
	prs, err := list(dir, r, "closed", 50)
	if err != nil {
		return nil, err
	}
	var out []PR
	for _, p := range prs {
		if p.Mine && !p.Closed.Before(since) {
			out = append(out, p)
		}
	}
	return out, nil
}

func list(dir string, r Remote, state string, limit int) ([]PR, error) {
	switch r.Provider {
	case GitHub:
		return listGitHub(dir, r, state, limit)
	case Gitea:
		return listGitea(dir, r, state, limit)
	}
	return nil, fmt.Errorf("unsupported provider")
}

func listGitHub(dir string, r Remote, state string, limit int) ([]PR, error) {
	out, err := runCmd(dir, "gh", "pr", "list", "--state", state, "--limit", strconv.Itoa(limit),
		"--json", "number,title,author,headRefName,baseRefName,url,body,isDraft,state,createdAt,updatedAt,closedAt,mergedAt")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Author    struct{ Login string }
		Head      string     `json:"headRefName"`
		Base      string     `json:"baseRefName"`
		URL       string     `json:"url"`
		Body      string     `json:"body"`
		IsDraft   bool       `json:"isDraft"`
		State     string     `json:"state"`
		CreatedAt time.Time  `json:"createdAt"`
		UpdatedAt time.Time  `json:"updatedAt"`
		ClosedAt  *time.Time `json:"closedAt"`
		MergedAt  *time.Time `json:"mergedAt"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("gh output: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, p := range raw {
		pr := PR{
			Number: p.Number, Title: p.Title, Author: p.Author.Login, Head: p.Head, Base: p.Base,
			URL: p.URL, Body: p.Body, Draft: p.IsDraft, State: strings.ToLower(p.State),
			Created: p.CreatedAt, Updated: p.UpdatedAt,
			Mine: r.Login != "" && strings.EqualFold(p.Author.Login, r.Login),
		}
		if p.MergedAt != nil {
			pr.Closed = *p.MergedAt
		} else if p.ClosedAt != nil {
			pr.Closed = *p.ClosedAt
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

func listGitea(dir string, r Remote, state string, limit int) ([]PR, error) {
	out, err := runCmd(dir, "tea", "pr", "ls", "--state", state, "--limit", strconv.Itoa(limit), "-o", "json",
		"--fields", "index,title,state,author,url,head,base,created,updated,body")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Index   string `json:"index"`
		Title   string `json:"title"`
		State   string `json:"state"`
		Author  string `json:"author"`
		URL     string `json:"url"`
		Head    string `json:"head"`
		Base    string `json:"base"`
		Created string `json:"created"`
		Updated string `json:"updated"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("tea output: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, p := range raw {
		n, _ := strconv.Atoi(p.Index)
		created, _ := time.Parse(time.RFC3339, p.Created)
		updated, _ := time.Parse(time.RFC3339, p.Updated)
		pr := PR{
			Number: n, Title: p.Title, Author: p.Author, Head: p.Head, Base: p.Base,
			URL: p.URL, Body: p.Body, State: strings.ToLower(p.State), Created: created, Updated: updated,
			Mine: isMe(r.Login, p.Author),
		}
		if pr.State != "open" {
			pr.Closed = updated // tea has no close time; the last update is it
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// isMe matches a Gitea login like "William.Loosman" against the display
// name tea prints ("Loosman, William"): every name part must appear.
func isMe(login, author string) bool {
	if login == "" {
		return false
	}
	if strings.EqualFold(login, author) {
		return true
	}
	a := strings.ToLower(author)
	parts := strings.FieldsFunc(strings.ToLower(login), func(c rune) bool {
		return c == '.' || c == '-' || c == '_' || c == ' '
	})
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !strings.Contains(a, p) {
			return false
		}
	}
	return true
}

// Archived reports whether the repo is archived on its remote.
func Archived(dir string, r Remote) (bool, error) {
	switch r.Provider {
	case GitHub:
		out, err := runCmd(dir, "gh", "repo", "view", r.Slug, "--json", "isArchived", "-q", ".isArchived")
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(out) == "true", nil
	case Gitea:
		out, err := runCmd(dir, "tea", "repos", r.Slug)
		if err != nil {
			return false, err
		}
		return teaArchived(out), nil
	}
	return false, fmt.Errorf("unsupported provider")
}

// teaArchived reads the heading of `tea repos owner/repo`, which looks like
// "# acme/archived (archived)".
func teaArchived(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.HasSuffix(line, "(archived)")
		}
	}
	return false
}

// OpenInBrowser opens the URL with the user's browser: $BROWSER when set,
// otherwise the platform's default handler.
func OpenInBrowser(u string) error {
	if b := os.Getenv("BROWSER"); b != "" {
		return exec.Command(b, u).Start()
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
	if _, err := exec.LookPath("xdg-open"); err != nil {
		return fmt.Errorf("no browser found: install xdg-utils or set $BROWSER")
	}
	return exec.Command("xdg-open", u).Start()
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "NO_COLOR=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", name, msg)
	}
	return out.String(), nil
}
