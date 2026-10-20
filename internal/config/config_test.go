package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalCreatedAndEnvironments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, DirName, "config.json")); err != nil {
		t.Fatalf("global config not created: %v", err)
	}
	g.TouchEnvironment("/a")
	g.TouchEnvironment("/b")
	g.TouchEnvironment("/a")
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	g2, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if len(g2.Environments) != 2 || g2.Environments[0].Path != "/a" {
		t.Fatalf("environments = %+v", g2.Environments)
	}
}

func TestLocalProjectsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := LoadLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, DirName, "config.json")); err != nil {
		t.Fatalf("local config not created: %v", err)
	}
	if _, err := l.AddProject("  "); err == nil {
		t.Fatal("empty name must be rejected")
	}
	p, err := l.AddProject("kangaroo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AddProject("kangaroo"); err == nil {
		t.Fatal("duplicate name must be rejected")
	}
	if !p.Toggle("kangaroo-api") || !p.Has("kangaroo-api") {
		t.Fatal("toggle on failed")
	}
	p.Toggle("kangaroo-web")
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}

	l2, err := LoadLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	p2 := l2.Project("kangaroo")
	if p2 == nil || len(p2.Repos) != 2 || p2.Repos[0] != "kangaroo-api" {
		t.Fatalf("project not persisted: %+v", l2.Projects)
	}
	if p2.Toggle("kangaroo-api") || p2.Has("kangaroo-api") {
		t.Fatal("toggle off failed")
	}
	if err := l2.RenameProject("kangaroo", "roo"); err != nil || l2.Project("roo") == nil {
		t.Fatalf("rename failed: %v", err)
	}
	l2.RemoveProject("roo")
	if len(l2.Projects) != 0 {
		t.Fatal("remove failed")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := LoadCache(dir)
	if len(c.Repos) != 0 {
		t.Fatal("expected empty cache")
	}
	c.Repos["x"] = RepoCache{Branch: "main", Ahead: 2}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2 := LoadCache(dir)
	if c2.Repos["x"].Branch != "main" || c2.Repos["x"].Ahead != 2 {
		t.Fatalf("cache not persisted: %+v", c2.Repos)
	}
}
