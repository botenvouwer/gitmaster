package pr

import "testing"

func TestTeaArchived(t *testing.T) {
	archived := "\n  # acme/archived (archived)\n\n  Issues: 0\n"
	if !teaArchived(archived) {
		t.Fatal("expected archived")
	}
	if teaArchived("\n  # acme/project\n\n  Issues: 0\n") {
		t.Fatal("expected not archived")
	}
}

func TestParseRemote(t *testing.T) {
	cases := map[string][2]string{
		"https://github.com/acme/scanner.git":                {"github.com", "acme/scanner"},
		"git@github.com:acme/tool.git":                       {"github.com", "acme/tool"},
		"ssh://git@git-ssh.example.com:443/acme/project.git": {"git-ssh.example.com", "acme/project"},
	}
	for raw, want := range cases {
		host, slug := parseRemote(raw)
		if host != want[0] || slug != want[1] {
			t.Errorf("parseRemote(%q) = %q, %q; want %q, %q", raw, host, slug, want[0], want[1])
		}
	}
}

func TestCommitURL(t *testing.T) {
	gh := Remote{Provider: GitHub, WebURL: "https://github.com/acme/tool"}
	if got := CommitURL(gh, "abc"); got != "https://github.com/acme/tool/commit/abc" {
		t.Fatalf("github: %s", got)
	}
	tea := Remote{Provider: Gitea, WebURL: "https://git.example.nl/acme/project"}
	if got := CommitURL(tea, "abc"); got != "https://git.example.nl/acme/project/commit/abc" {
		t.Fatalf("gitea: %s", got)
	}
	if CommitURL(Remote{}, "abc") != "" {
		t.Fatal("unknown remote must give no url")
	}
}
