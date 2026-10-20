package ui

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// copyToClipboard puts text on the system clipboard.
func copyToClipboard(text string) error {
	var candidates [][]string
	if runtime.GOOS == "darwin" {
		candidates = [][]string{{"pbcopy"}}
	} else {
		candidates = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errors.New("no clipboard tool found (pbcopy, wl-copy, xclip or xsel)")
}

// copyText copies text and reports it in the flash line.
func (a *App) copyText(what, text string) {
	if text == "" {
		a.Flash("nothing to copy")
		return
	}
	if err := copyToClipboard(text); err != nil {
		a.FlashErr(err)
		return
	}
	a.Flash("copied " + what + ": " + trunc(text, 80))
}
