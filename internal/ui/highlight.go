package ui

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/rivo/tview"
)

// highlighter colours source code with tview tags using chroma lexers, so
// Go, Python, JSON, YAML, shell, Markdown and friends all get their own look.
type highlighter struct {
	lexer chroma.Lexer
	style *chroma.Style
}

// newHighlighter picks a lexer by file name, falling back to sniffing the
// content. It returns nil when the file type is not recognised.
func newHighlighter(filename, content, styleName string) *highlighter {
	lexer := lexers.Match(filename)
	if lexer == nil && content != "" {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		return nil
	}
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}
	return &highlighter{lexer: chroma.Coalesce(lexer), style: style}
}

// lines returns the highlighted text, one tagged and escaped entry per line.
func (h *highlighter) lines(text string) []string {
	if h == nil {
		return plainLines(text)
	}
	it, err := h.lexer.Tokenise(nil, text)
	if err != nil {
		return plainLines(text)
	}
	var b strings.Builder
	for tok := it(); tok != chroma.EOF; tok = it() {
		tag := tagFor(h.style.Get(tok.Type))
		// Tag each line of a multi-line token separately so colours never
		// straddle a line break (line numbers reset the colour per line).
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			if part == "" {
				continue
			}
			b.WriteString(tag)
			b.WriteString(tview.Escape(part))
			if tag != "" {
				b.WriteString("[-::-]")
			}
		}
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

// line highlights a single line, e.g. a diff context line.
func (h *highlighter) line(s string) string {
	ls := h.lines(s)
	if len(ls) == 0 {
		return ""
	}
	return ls[0]
}

func tagFor(e chroma.StyleEntry) string {
	colour, attrs := "", ""
	if e.Colour.IsSet() {
		colour = e.Colour.String()
	}
	if e.Bold == chroma.Yes {
		attrs += "b"
	}
	if e.Italic == chroma.Yes {
		attrs += "i"
	}
	if e.Underline == chroma.Yes {
		attrs += "u"
	}
	if colour == "" && attrs == "" {
		return ""
	}
	return fmt.Sprintf("[%s::%s]", colour, attrs)
}

func plainLines(text string) []string {
	ls := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := range ls {
		ls[i] = tview.Escape(ls[i])
	}
	return ls
}
