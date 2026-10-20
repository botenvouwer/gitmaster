package ui

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// clipboardTools are tried in order; the first one installed wins.
func clipboardTools() [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip"}}
	}
	return [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
}

// copyToClipboard puts text on the system clipboard through a native tool,
// falling back to the terminal's OSC 52 clipboard (works over SSH when the
// terminal allows it).
func (a *App) copyToClipboard(text string) error {
	for _, c := range clipboardTools() {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	if a.screen != nil {
		a.screen.SetClipboard([]byte(text))
		return nil
	}
	return errors.New("no clipboard tool found (pbcopy, wl-copy, xclip or xsel)")
}

// copyText copies text and reports it in the flash line.
func (a *App) copyText(what, text string) {
	if text == "" {
		a.Flash("nothing to copy")
		return
	}
	if err := a.copyToClipboard(text); err != nil {
		a.FlashErr(err)
		return
	}
	a.Flash("copied " + what + ": " + trunc(text, 80))
}
