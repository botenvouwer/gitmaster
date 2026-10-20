package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// age renders a time as a short k9s-style relative age.
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
}

// colorizeDiff adds tview color tags to unified diff output. Added and
// removed lines keep their diff colours; context lines are syntax-coloured
// for the file named in the diff header.
func colorizeDiff(s, styleName string) string {
	var b strings.Builder
	var hl *highlighter
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if strings.HasPrefix(line, " ") && hl != nil {
			b.WriteString(" " + hl.line(line[1:]) + "\n")
			continue
		}
		tag := ""
		switch {
		case strings.HasPrefix(line, "+++ "):
			tag = "[yellow::b]"
			hl = nil
			if path := strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/"); path != "/dev/null" {
				hl = newHighlighter(path, "", styleName)
			}
		case strings.HasPrefix(line, "---"):
			tag = "[yellow::b]"
		case strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"),
			strings.HasPrefix(line, "rename "), strings.HasPrefix(line, "similarity "),
			strings.HasPrefix(line, "Binary files"):
			tag = "[yellow]"
		case strings.HasPrefix(line, "@@"):
			tag = "[aqua]"
		case strings.HasPrefix(line, "+"):
			tag = "[green]"
		case strings.HasPrefix(line, "-"):
			tag = "[red]"
		case strings.HasPrefix(line, "commit "):
			tag = "[yellow::b]"
		case strings.HasPrefix(line, "Author:"), strings.HasPrefix(line, "Date:"):
			tag = "[gray]"
		}
		b.WriteString(tag)
		b.WriteString(tview.Escape(line))
		if tag != "" {
			b.WriteString("[-::-]")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// numbered prefixes every (already tagged and escaped) line with a muted
// line number.
func numbered(lines []string) string {
	width := len(strconv.Itoa(len(lines)))
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "[gray]%*d[-] %s\n", width, i+1, line)
	}
	return b.String()
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
