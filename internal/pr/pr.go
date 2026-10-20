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
	Created time.Time
	Updated time.Time
	Mine    bool
}

var (
	ghLoginOnce sync.Once
	ghLogin     string
	teaOnce     sync.Once
	teaLogins   map[string]string // host -> user
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
		ghLoginOnce.Do(func() {
			if l, err := runCmd(dir, "gh", "api", "user", "-q", ".login"); err == nil {
				ghLogin = strings.TrimSpace(l)
			}
		})
		r.Login = ghLogin
		return r, nil
	}
	teaOnce.Do(func() {
		teaLogins = map[string]string{}
		out, err := runCmd(dir, "tea", "logins", "list", "-o", "json")
		if err != nil {
			return
		}
		var logins []struct {
			URL  string `json:"url"`
			User string `json:"user"`
		}
		if json.Unmarshal([]byte(out), &logins) != nil {
			return
		}
		for _, l := range logins {
			if u, err := url.Parse(l.URL); err == nil {
				teaLogins[strings.ToLower(u.Host)] = l.User
			}
		}
	})
	if user, ok := teaLogins[strings.ToLower(r.Host)]; ok {
		r.Provider = Gitea
		r.Login = user
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
	switch r.Provider {
	case GitHub:
		return listGitHub(dir, r)
	case Gitea:
		return listGitea(dir, r)
	}
	return nil, fmt.Errorf("unsupported provider")
}

func listGitHub(dir string, r Remote) ([]PR, error) {
	out, err := runCmd(dir, "gh", "pr", "list", "--state", "open", "--limit", "100",
		"--json", "number,title,author,headRefName,baseRefName,url,body,isDraft,createdAt,updatedAt")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Author    struct{ Login string }
		Head      string    `json:"headRefName"`
		Base      string    `json:"baseRefName"`
		URL       string    `json:"url"`
		Body      string    `json:"body"`
		IsDraft   bool      `json:"isDraft"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("gh output: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, p := range raw {
		prs = append(prs, PR{
			Number: p.Number, Title: p.Title, Author: p.Author.Login, Head: p.Head, Base: p.Base,
			URL: p.URL, Body: p.Body, Draft: p.IsDraft, Created: p.CreatedAt, Updated: p.UpdatedAt,
			Mine: r.Login != "" && strings.EqualFold(p.Author.Login, r.Login),
		})
	}
	return prs, nil
}

func listGitea(dir string, r Remote) ([]PR, error) {
	out, err := runCmd(dir, "tea", "pr", "ls", "--state", "open", "--limit", "100", "-o", "json",
		"--fields", "index,title,author,url,head,base,created,updated,body")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Index   string `json:"index"`
		Title   string `json:"title"`
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
		prs = append(prs, PR{
			Number: n, Title: p.Title, Author: p.Author, Head: p.Head, Base: p.Base,
			URL: p.URL, Body: p.Body, Created: created, Updated: updated,
			Mine: isMe(r.Login, p.Author),
		})
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

// OpenInBrowser opens the URL with the OS default handler.
func OpenInBrowser(u string) error {
	return exec.Command("open", u).Start()
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
