package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Tokens of the midnight theme (DESIGN.md). The console never switches to
// paper; Bubble Tea downsamples these hex values for 256- and 16-color
// terminals.
var (
	colorCanvas       = lipgloss.Color("#071522")
	colorPanel        = lipgloss.Color("#102231")
	colorPanelRaised  = lipgloss.Color("#183447")
	colorInk          = lipgloss.Color("#f5f7f8")
	colorInkMuted     = lipgloss.Color("#9db1bc")
	colorInkFaint     = lipgloss.Color("#4f6675")
	colorAccent       = lipgloss.Color("#4dd4d4")
	colorAccentStrong = lipgloss.Color("#008b87")
	colorFrame        = lipgloss.Color("#005f5c")
	colorOnAccent     = lipgloss.Color("#071522")
	colorSuccess      = lipgloss.Color("#25d366")
	colorWarning      = lipgloss.Color("#f59e0b")
	colorError        = lipgloss.Color("#f87171")
	colorInfo         = lipgloss.Color("#60a5fa")
)

var (
	styleInk    = lipgloss.NewStyle().Foreground(colorInk)
	styleStrong = lipgloss.NewStyle().Foreground(colorInk).Bold(true)
	styleMuted  = lipgloss.NewStyle().Foreground(colorInkMuted)
	styleFaint  = lipgloss.NewStyle().Foreground(colorInkFaint)
	styleAccent = lipgloss.NewStyle().Foreground(colorAccent)
	styleLabel  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleFocus  = lipgloss.NewStyle().Foreground(colorOnAccent).Background(colorAccent).Bold(true)
	styleFrame  = lipgloss.NewStyle().Foreground(colorFrame)
)

// state is the status vocabulary of DESIGN.md: always glyph, color and word.
type state uint8

const (
	stateNeutral state = iota
	stateOK
	stateWarn
	stateError
	stateInfo
	stateBusy
	statePending
)

func (s state) glyph() string {
	switch s {
	case stateOK:
		return lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
	case stateWarn:
		return lipgloss.NewStyle().Foreground(colorWarning).Render("!")
	case stateError:
		return lipgloss.NewStyle().Foreground(colorError).Render("✕")
	case stateInfo:
		return lipgloss.NewStyle().Foreground(colorInfo).Render("◇")
	case stateBusy:
		return styleAccent.Render("◐")
	case statePending:
		return styleFaint.Render("◌")
	default:
		return styleMuted.Render("·")
	}
}

func (s state) style() lipgloss.Style {
	switch s {
	case stateOK:
		return lipgloss.NewStyle().Foreground(colorSuccess)
	case stateWarn:
		return lipgloss.NewStyle().Foreground(colorWarning)
	case stateError:
		return lipgloss.NewStyle().Foreground(colorError)
	case stateInfo:
		return lipgloss.NewStyle().Foreground(colorInfo)
	default:
		return styleMuted
	}
}

// outcome renders an outcome line: glyph plus uppercase strong text.
func outcome(s state, text string) string {
	return s.glyph() + " " + s.style().Bold(true).Render(strings.ToUpper(text))
}

// keyHint renders one `[Tecla] acción` pair.
func keyHint(key, action string) string {
	return styleAccent.Render("["+key+"]") + " " + styleMuted.Render(action)
}

// backgroundSequence is the SGR that paints color as background. Inner spans
// end with a reset, which also clears an outer background, so containers
// re-open it after every reset (see withBackground).
func backgroundSequence(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// withBackground paints a rendered line on c, surviving nested resets.
func withBackground(line string, c color.Color) string {
	open := backgroundSequence(c)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[m")
	return open + strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+open) + "\x1b[m"
}

// faint dims a rendered block for the base layer under a modal.
func faint(block string) string {
	lines := strings.Split(block, "\n")
	for index, line := range lines {
		line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[m")
		lines[index] = "\x1b[2m" + strings.ReplaceAll(line, "\x1b[m", "\x1b[m\x1b[2m") + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}
