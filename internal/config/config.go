// Package config persists gitmaster's settings.
//
//   - Global:  ~/.gitmaster/config.json      user settings + known environments
//   - Local:   <dir>/.gitmaster/config.json  projects defined for that directory
//   - Cache:   <dir>/.gitmaster/cache.json   last scan results for that directory
//
// Missing files are created with defaults on first load.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DirName is the name of both the global (~/.gitmaster) and local (.gitmaster) dirs.
const DirName = ".gitmaster"

// Settings holds user settings.
type Settings struct {
	// ShowArchived shows repos that are archived on their remote; they are
	// hidden by default. The A key toggles it for the running session.
	ShowArchived bool `json:"showArchived"`
	// SyntaxStyle is the chroma style used to colour code (e.g. monokai,
	// dracula, github-dark, solarized-dark).
	SyntaxStyle string `json:"syntaxStyle"`
	// RefreshSeconds is how often the repos on screen get a fresh git status
	// (branch, ahead/behind, staged/modified/untracked, state). 0 disables it.
	RefreshSeconds int `json:"refreshSeconds"`
}

// Defaults for settings that are missing from the file.
const (
	DefaultSyntaxStyle    = "monokai"
	DefaultRefreshSeconds = 1
)

// Environment is a directory gitmaster has been opened in that contained repos.
type Environment struct {
	Path       string    `json:"path"`
	LastOpened time.Time `json:"lastOpened"`
}

// UIState remembers which views were used last, so navigation and the next
// start land in the same place.
type UIState struct {
	MasterView string `json:"masterView"` // "repos" or "pulls"
	DetailView string `json:"detailView"` // "open work", "commits" or "pull requests"
}

// Global is the user-wide configuration.
type Global struct {
	Settings     Settings      `json:"settings"`
	UI           UIState       `json:"ui"`
	Environments []Environment `json:"environments"`
	path         string
}

// GlobalDir returns ~/.gitmaster.
func GlobalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// LoadGlobal reads ~/.gitmaster/config.json, creating it when missing.
func LoadGlobal() (*Global, error) {
	dir, err := GlobalDir()
	if err != nil {
		return nil, err
	}
	g := &Global{
		Settings:     Settings{SyntaxStyle: DefaultSyntaxStyle, RefreshSeconds: DefaultRefreshSeconds},
		Environments: []Environment{},
		path:         filepath.Join(dir, "config.json"),
	}
	if err := load(g.path, g); err != nil {
		return nil, err
	}
	if g.Settings.SyntaxStyle == "" {
		g.Settings.SyntaxStyle = DefaultSyntaxStyle
	}
	// Write back so newly added settings show up in the file with defaults.
	return g, g.Save()
}

// Save writes the global config.
func (g *Global) Save() error {
	if g.Environments == nil {
		g.Environments = []Environment{}
	}
	return save(g.path, g)
}

// TouchEnvironment records dir as an environment opened just now.
func (g *Global) TouchEnvironment(dir string) {
	now := time.Now()
	found := false
	for i := range g.Environments {
		if g.Environments[i].Path == dir {
			g.Environments[i].LastOpened = now
			found = true
		}
	}
	if !found {
		g.Environments = append(g.Environments, Environment{Path: dir, LastOpened: now})
	}
	sort.Slice(g.Environments, func(i, j int) bool {
		return g.Environments[i].LastOpened.After(g.Environments[j].LastOpened)
	})
}

// Project is a named set of repos (by directory name) that belong together.
type Project struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

// Has reports whether repo is part of the project.
func (p *Project) Has(repo string) bool {
	for _, r := range p.Repos {
		if r == repo {
			return true
		}
	}
	return false
}

// Toggle adds or removes repo and returns whether it is now a member.
func (p *Project) Toggle(repo string) bool {
	for i, r := range p.Repos {
		if r == repo {
			p.Repos = append(p.Repos[:i], p.Repos[i+1:]...)
			return false
		}
	}
	p.Repos = append(p.Repos, repo)
	sort.Strings(p.Repos)
	return true
}

// Local is the per-directory configuration.
type Local struct {
	Projects []Project `json:"projects"`
	path     string
}

// LoadLocal reads <dir>/.gitmaster/config.json, creating it when missing.
func LoadLocal(dir string) (*Local, error) {
	l := &Local{Projects: []Project{}, path: filepath.Join(dir, DirName, "config.json")}
	if err := load(l.path, l); err != nil {
		return nil, err
	}
	return l, nil
}

// Save writes the local config.
func (l *Local) Save() error {
	if l.Projects == nil {
		l.Projects = []Project{}
	}
	for i := range l.Projects {
		if l.Projects[i].Repos == nil {
			l.Projects[i].Repos = []string{}
		}
	}
	return save(l.path, l)
}

// Project returns the named project or nil.
func (l *Local) Project(name string) *Project {
	for i := range l.Projects {
		if l.Projects[i].Name == name {
			return &l.Projects[i]
		}
	}
	return nil
}

// AddProject creates an empty project with a unique, non-empty name.
func (l *Local) AddProject(name string) (*Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("project name is empty")
	}
	if l.Project(name) != nil {
		return nil, fmt.Errorf("project %q already exists", name)
	}
	l.Projects = append(l.Projects, Project{Name: name, Repos: []string{}})
	sort.Slice(l.Projects, func(i, j int) bool { return l.Projects[i].Name < l.Projects[j].Name })
	return l.Project(name), nil
}

// RenameProject renames a project, keeping names unique.
func (l *Local) RenameProject(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("project name is empty")
	}
	if newName != oldName && l.Project(newName) != nil {
		return fmt.Errorf("project %q already exists", newName)
	}
	p := l.Project(oldName)
	if p == nil {
		return fmt.Errorf("project %q not found", oldName)
	}
	p.Name = newName
	sort.Slice(l.Projects, func(i, j int) bool { return l.Projects[i].Name < l.Projects[j].Name })
	return nil
}

// RemoveProject deletes a project; repos themselves are untouched.
func (l *Local) RemoveProject(name string) {
	for i := range l.Projects {
		if l.Projects[i].Name == name {
			l.Projects = append(l.Projects[:i], l.Projects[i+1:]...)
			return
		}
	}
}

// RepoCache is the last known state of one repo.
type RepoCache struct {
	Branch      string    `json:"branch"`
	Ahead       int       `json:"ahead"`
	Behind      int       `json:"behind"`
	Staged      int       `json:"staged"`
	Modified    int       `json:"modified"`
	Untracked   int       `json:"untracked"`
	LastCommit  time.Time `json:"lastCommit"`
	LastSubject string    `json:"lastSubject"`
	// Remote facts, refreshed when RemoteCheckedAt is older than a day.
	Archived        bool      `json:"archived"`
	RemoteCheckedAt time.Time `json:"remoteCheckedAt"`
}

// Cache holds the result of the last scan so the UI can show it instantly.
type Cache struct {
	ScannedAt time.Time            `json:"scannedAt"`
	Repos     map[string]RepoCache `json:"repos"`
	path      string
}

// LoadCache reads <dir>/.gitmaster/cache.json; a missing or broken cache is
// simply empty.
func LoadCache(dir string) *Cache {
	c := &Cache{Repos: map[string]RepoCache{}, path: filepath.Join(dir, DirName, "cache.json")}
	if data, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(data, c)
	}
	if c.Repos == nil {
		c.Repos = map[string]RepoCache{}
	}
	return c
}

// Save writes the cache.
func (c *Cache) Save() error { return save(c.path, c) }

func load(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return save(path, v)
	}
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func save(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
