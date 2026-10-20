// Package ui is the tview front end. Layout follows k9s: an info/hotkey/logo
// header, a main area, a flash line and a breadcrumb bar.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/botenvouwer/gitmaster/internal/config"
)

type hotkey struct{ key, desc string }

// view is a screen that can be pushed on the navigation stack.
type view interface {
	tview.Primitive
	crumbs() []string
	hotkeys() []hotkey
	info() [][2]string
	start()
	update() // repo or project data changed
	focusTarget() tview.Primitive
	escape() bool // true when the view consumed Esc itself
}

// App owns the tview application, the environment and the navigation stack.
type App struct {
	*tview.Application
	version   string
	dir       string
	global    *config.Global
	globalErr error
	env       *environment

	pages   *tview.Pages
	header  *tview.Flex
	infoTV  *tview.TextView
	keysTV  *tview.TextView
	logoTV  *tview.TextView
	flashTV *tview.TextView
	crumbTV *tview.TextView
	helpTV  *tview.TextView

	stack        []view
	flashTimer   *time.Timer
	helpShown    bool
	modal        bool
	showArchived bool
	syntaxStyle  string
	refreshEvery time.Duration
	refreshing   bool
	prevFocus    tview.Primitive
}

// New builds the application for the given directory.
func New(dir, version string) *App {
	a := &App{Application: tview.NewApplication(), dir: dir, version: version}
	a.env = &environment{dir: dir}
	a.global, a.globalErr = config.LoadGlobal()
	a.syntaxStyle = config.DefaultSyntaxStyle
	a.refreshEvery = config.DefaultRefreshSeconds * time.Second
	if a.global != nil {
		a.showArchived = a.global.Settings.ShowArchived
		a.syntaxStyle = a.global.Settings.SyntaxStyle
		a.refreshEvery = time.Duration(a.global.Settings.RefreshSeconds) * time.Second
	}

	a.infoTV = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	a.keysTV = tview.NewTextView().SetDynamicColors(true)
	a.logoTV = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight)
	a.logoTV.SetText("[orange]" + strings.Join(logo, "\n"))
	logoWidth := 0
	for _, l := range logo {
		if len(l) > logoWidth {
			logoWidth = len(l)
		}
	}
	a.header = tview.NewFlex().
		AddItem(a.infoTV, 36, 0, false).
		AddItem(a.keysTV, 0, 1, false).
		AddItem(a.logoTV, logoWidth+1, 0, false)

	a.pages = tview.NewPages()
	a.flashTV = tview.NewTextView().SetDynamicColors(true)
	a.crumbTV = tview.NewTextView().SetDynamicColors(true)

	a.helpTV = tview.NewTextView().SetDynamicColors(true)
	a.helpTV.SetBorder(true).SetTitle(" Help ").SetTitleAlign(tview.AlignLeft).
		SetTitleColor(colorTitle).SetBorderColor(colorFocus).SetBorderPadding(0, 0, 1, 1)
	helpBox := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(a.helpTV, 0, 8, true).
			AddItem(nil, 0, 1, false), 0, 2, true).
		AddItem(nil, 0, 1, false)
	a.pages.AddPage("help", helpBox, true, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, len(logo), 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.flashTV, 1, 0, false).
		AddItem(a.crumbTV, 1, 0, false)

	a.SetRoot(root, true).EnableMouse(true)
	a.SetInputCapture(a.globalKeys)
	return a
}

// Run starts on the home screen, scans the directory and blocks until quit.
func (a *App) Run() error {
	a.push(newHome(a))
	if a.globalErr != nil {
		a.FlashErr(a.globalErr)
	}
	a.scan(false)
	a.startAutoRefresh()
	return a.Application.Run()
}

func (a *App) top() view {
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1]
}

func pageName(i int) string { return fmt.Sprintf("view-%d", i) }

func (a *App) push(v view) {
	if cur := a.top(); cur != nil {
		a.pages.HidePage(pageName(len(a.stack) - 1))
	}
	a.stack = append(a.stack, v)
	a.pages.AddPage(pageName(len(a.stack)-1), v, true, true)
	a.SetFocus(v.focusTarget())
	a.refresh()
	v.start()
}

func (a *App) pop() {
	if len(a.stack) <= 1 {
		a.Stop()
		return
	}
	if c, ok := a.top().(interface{ stop() }); ok {
		c.stop()
	}
	a.pages.RemovePage(pageName(len(a.stack) - 1))
	a.stack = a.stack[:len(a.stack)-1]
	a.pages.ShowPage(pageName(len(a.stack) - 1))
	top := a.top()
	top.update()
	a.SetFocus(top.focusTarget())
	a.refresh()
}

// refresh redraws header info, hotkeys and crumbs from the current view.
// Must be called from the UI goroutine (or inside QueueUpdateDraw).
func (a *App) refresh() {
	v := a.top()
	if v == nil {
		return
	}
	// Info block.
	var b strings.Builder
	rows := append(v.info(), [2]string{"GM Rev", a.version})
	keyW := 0
	for _, r := range rows {
		if len(r[0]) > keyW {
			keyW = len(r[0])
		}
	}
	infoW := 0
	for i, r := range rows {
		if i >= len(logo) {
			break
		}
		fmt.Fprintf(&b, "[orange::b]%-*s[-::-] [white]%s[-]\n", keyW+1, r[0]+":", tview.Escape(r[1]))
		infoW = max(infoW, keyW+2+len([]rune(r[1]))+2)
	}
	a.infoTV.SetText(b.String())
	a.header.ResizeItem(a.infoTV, max(30, min(60, infoW)), 0)

	// Hotkeys block: columns of len(logo) rows.
	keys := v.hotkeys()
	rowsPer := len(logo)
	cols := (len(keys) + rowsPer - 1) / rowsPer
	var lines [5]strings.Builder
	for c := 0; c < cols; c++ {
		start, end := c*rowsPer, min((c+1)*rowsPer, len(keys))
		kw, dw := 0, 0
		for _, k := range keys[start:end] {
			kw = max(kw, len(k.key)+2)
			dw = max(dw, len(k.desc))
		}
		for r := 0; r < rowsPer; r++ {
			i := start + r
			if i < end {
				fmt.Fprintf(&lines[r], "[dodgerblue::b]%-*s[-::-] [gray]%-*s[-]  ", kw, "<"+keys[i].key+">", dw, keys[i].desc)
			}
		}
	}
	var kb strings.Builder
	for _, l := range lines {
		kb.WriteString(l.String() + "\n")
	}
	a.keysTV.SetText(kb.String())

	// Crumbs.
	var all []string
	for _, s := range a.stack {
		all = append(all, s.crumbs()...)
	}
	// Crumbs use an explicit RGB black and no bold: palette black is often
	// remapped by terminal themes, and bold black renders as dark grey in
	// many terminals, which made the inactive crumbs unreadable.
	var cb strings.Builder
	for i, c := range all {
		bg := "#808080"
		if i == len(all)-1 {
			bg = "#00ffff"
		}
		fmt.Fprintf(&cb, "[#000000:%s] %s [-:-:-] ", bg, tview.Escape(c))
	}
	a.crumbTV.SetText(cb.String())
}

// ui returns the remembered view state (a throwaway when no config loaded).
func (a *App) ui() *config.UIState {
	if a.global == nil {
		return &config.UIState{}
	}
	return &a.global.UI
}

func (a *App) saveGlobal() {
	if a.global == nil {
		return
	}
	if err := a.global.Save(); err != nil {
		a.FlashErr(err)
	}
}

// Flash shows a transient message in the flash line.
func (a *App) Flash(msg string) { a.flash("[aqua]" + tview.Escape(msg)) }

// FlashErr shows a transient error in the flash line.
func (a *App) FlashErr(err error) { a.flash("[red::b]" + tview.Escape(err.Error())) }

func (a *App) flash(text string) {
	a.flashTV.SetText(" " + text)
	if a.flashTimer != nil {
		a.flashTimer.Stop()
	}
	a.flashTimer = time.AfterFunc(4*time.Second, func() {
		a.QueueUpdateDraw(func() { a.flashTV.SetText("") })
	})
}

func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() == tcell.KeyCtrlC {
		a.Stop()
		return nil
	}
	if a.modal {
		if ev.Key() == tcell.KeyEsc {
			a.closeModal()
			return nil
		}
		return ev
	}
	if a.helpShown {
		switch {
		case ev.Key() == tcell.KeyEsc, ev.Rune() == '?', ev.Rune() == 'q':
			a.toggleHelp()
		}
		return nil
	}
	if _, ok := a.GetFocus().(*tview.InputField); ok {
		return ev
	}
	switch {
	case ev.Rune() == '?':
		a.toggleHelp()
		return nil
	case ev.Key() == tcell.KeyEsc:
		if v := a.top(); v != nil && v.escape() {
			return nil
		}
		if len(a.stack) > 1 {
			a.pop()
		}
		return nil
	case ev.Rune() == 'q':
		a.pop()
		return nil
	}
	return ev
}

func (a *App) toggleHelp() {
	if a.helpShown {
		a.helpShown = false
		a.pages.HidePage("help")
		if a.prevFocus != nil {
			a.SetFocus(a.prevFocus)
		}
		return
	}
	a.helpShown = true
	a.prevFocus = a.GetFocus()
	var b strings.Builder
	write := func(title string, keys []hotkey) {
		fmt.Fprintf(&b, "[aqua::b]%s[-::-]\n", title)
		for _, k := range keys {
			fmt.Fprintf(&b, "  [dodgerblue::b]%-14s[-::-] [white]%s[-]\n", "<"+k.key+">", k.desc)
		}
		b.WriteString("\n")
	}
	if v := a.top(); v != nil {
		write("View", v.hotkeys())
	}
	write("Navigation", []hotkey{
		{"↑/↓ j/k", "move / scroll one line"},
		{"shift+↑/↓ J/K", "move / scroll five lines"},
		{"pgup/pgdn", "page"},
		{"g/G", "top / bottom"},
		{"h/l", "scroll horizontally (content pane)"},
		{"mouse wheel", "scroll"},
	})
	write("General", []hotkey{
		{"c", "copy: commit hash, file/repo path or PR url"},
		{"?", "toggle this help"},
		{"esc", "back"},
		{"q", "back / quit"},
		{"ctrl-c", "quit"},
	})
	a.helpTV.SetText(b.String())
	a.pages.ShowPage("help")
	a.pages.SendToFront("help")
	a.SetFocus(a.helpTV)
}
