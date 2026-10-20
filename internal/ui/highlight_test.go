package ui

import (
	"strings"
	"testing"
)

func TestHighlighterByExtension(t *testing.T) {
	cases := map[string]string{
		"a.json": `{"name": "x", "n": 1}`,
		"a.yaml": "name: x\nlist:\n  - 1\n",
		"a.go":   "package x\n\nfunc main() {}\n",
	}
	for name, src := range cases {
		h := newHighlighter(name, src, "monokai")
		if h == nil {
			t.Fatalf("%s: no lexer", name)
		}
		lines := h.lines(src)
		if len(lines) != strings.Count(strings.TrimRight(src, "\n"), "\n")+1 {
			t.Fatalf("%s: got %d lines for %q", name, len(lines), src)
		}
		if !strings.Contains(strings.Join(lines, "\n"), "[#") {
			t.Fatalf("%s: no colour tags in %q", name, lines)
		}
	}
	if h := newHighlighter("README", "just words\n", "monokai"); h != nil && len(h.lines("just words")) != 1 {
		t.Fatal("plain text must still produce one line")
	}
}

func TestHighlighterEscapesTags(t *testing.T) {
	h := newHighlighter("a.go", "", "monokai")
	out := strings.Join(h.lines(`var s = "[red]x"`), "")
	if !strings.Contains(out, "[red[]") {
		t.Fatalf("literal tag not escaped: %q", out)
	}
}

func TestColorizeDiffHighlightsContext(t *testing.T) {
	diff := "diff --git a/x.json b/x.json\n--- a/x.json\n+++ b/x.json\n@@ -1,2 +1,2 @@\n {\"a\": 1,\n-  \"b\": 2\n+  \"b\": 3\n }\n"
	out := colorizeDiff(diff, "monokai")
	if !strings.Contains(out, "[green]+  ") || !strings.Contains(out, "[red]-  ") {
		t.Fatalf("diff colours missing: %q", out)
	}
	if !strings.Contains(out, " [#") {
		t.Fatalf("context line not syntax coloured: %q", out)
	}
}
