package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func init() {
	// k9s uses single-line borders whether focused or not.
	tview.Borders.HorizontalFocus = tview.Borders.Horizontal
	tview.Borders.VerticalFocus = tview.Borders.Vertical
	tview.Borders.TopLeftFocus = tview.Borders.TopLeft
	tview.Borders.TopRightFocus = tview.Borders.TopRight
	tview.Borders.BottomLeftFocus = tview.Borders.BottomLeft
	tview.Borders.BottomRightFocus = tview.Borders.BottomRight
}

// k9s-inspired palette.
var (
	colorKey        = tcell.ColorOrange
	colorValue      = tcell.ColorWhite
	colorLogo       = tcell.ColorOrange
	colorHotkey     = tcell.ColorDodgerBlue
	colorHotkeyDesc = tcell.ColorGray
	colorTitle      = tcell.ColorAqua
	colorFocus      = tcell.ColorDodgerBlue
	colorBlur       = tcell.ColorGray
	colorHeaderText = tcell.ColorWhite
	colorDirty      = tcell.ColorOrange
	colorClean      = tcell.ColorPaleGreen
	colorError      = tcell.ColorRed
	colorMuted      = tcell.ColorGray
	colorSHA        = tcell.ColorYellow

	styleSelected     = tcell.StyleDefault.Background(tcell.ColorAqua).Foreground(tcell.ColorBlack)
	styleSelectedBlur = tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
	styleHeader       = tcell.StyleDefault.Foreground(colorHeaderText).Bold(true)
)

var logo = []string{
	"  ____ _ _   __  __           _            ",
	" / ___(_) |_|  \\/  | __ _ ___| |_ ___ _ __ ",
	"| |  _| | __| |\\/| |/ _` / __| __/ _ \\ '__|",
	"| |_| | | |_| |  | | (_| \\__ \\ ||  __/ |   ",
	" \\____|_|\\__|_|  |_|\\__,_|___/\\__\\___|_|   ",
}
