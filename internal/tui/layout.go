package tui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	minimumWidth  = 80
	minimumHeight = 24
	bannerColumns = 120
	sidePanel     = 45
	stepperWidth  = 28
	stackBelow    = 100
)

// box draws a rounded frame of exactly width×height cells around lines,
// with one cell of padding and the given fill, truncating or padding every
// line to the inner width.
func box(width, height int, frame, fill color.Color, lines []string) []string {
	width, height = max(width, 4), max(height, 2)
	inner := width - 4
	border := lipgloss.NewStyle().Foreground(frame)
	out := []string{border.Render("╭" + strings.Repeat("─", width-2) + "╮")}
	for index := range height - 2 {
		line := ""
		if index < len(lines) {
			line = lines[index]
		}
		line = padRight(ansi.Truncate(line, inner, "…"), inner)
		out = append(out, border.Render("│")+withBackground(" "+line+" ", fill)+border.Render("│"))
	}
	return append(out, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
}

// panel is a framed section whose first line is its label and an optional
// right-aligned aside, followed by a blank line and the body.
func panel(width, height int, focused bool, label, aside string, body []string) []string {
	frame := colorFrame
	if focused {
		frame = colorAccentStrong
	}
	inner := max(width-4, 1)
	head := label
	if aside != "" {
		gap := inner - lipgloss.Width(label) - lipgloss.Width(aside)
		head = label + strings.Repeat(" ", max(gap, 1)) + aside
	}
	lines := append([]string{head, ""}, body...)
	return box(width, height, frame, colorPanel, lines)
}

// sectionLabel is an uppercase term-label in accent.
func sectionLabel(text string) string { return styleLabel.Render(strings.ToUpper(text)) }

// checkRow is one status line: glyph, name, value and detail in columns.
type checkRow struct {
	state               state
	name, value, detail string
	// action is the "Acción:" line under the row; command is a copyable
	// shell command that resolves it, listed by Doctor.
	action, command string
}

func renderChecks(rows []checkRow, nameWidth, valueWidth, width int) []string {
	indent := strings.Repeat(" ", 2+nameWidth+valueWidth)
	detailWidth := max(width-len(indent), 12)
	var lines []string
	for _, row := range rows {
		detail := wrapText(row.detail, detailWidth)
		lines = append(lines, row.state.glyph()+" "+padRight(styleInk.Render(row.name), nameWidth)+padRight(styleMuted.Render(row.value), valueWidth)+styleInk.Render(detail[0]))
		for _, more := range detail[1:] {
			lines = append(lines, indent+styleInk.Render(more))
		}
		if row.action != "" {
			for _, part := range wrapText("Acción: "+row.action, detailWidth) {
				lines = append(lines, indent+styleMuted.Render(part))
			}
		}
	}
	return lines
}

// card is one ActionCards entry.
type card struct {
	key, title, detail string
}

func renderCards(cards []card, selected, width int) []string {
	if len(cards) == 0 {
		return nil
	}
	gap := 2
	cardWidth := min(30, (width-gap*(len(cards)-1))/len(cards))
	columns := make([][]string, len(cards))
	for index, entry := range cards {
		frame := colorFrame
		if index == selected {
			frame = colorAccent
		}
		columns[index] = box(cardWidth, 4, frame, colorPanel, []string{
			styleAccent.Render("["+entry.key+"]") + " " + styleStrong.Render(entry.title),
			styleMuted.Render(entry.detail),
		})
	}
	var lines []string
	for row := range columns[0] {
		parts := make([]string, len(columns))
		for index := range columns {
			parts[index] = columns[index][row]
		}
		lines = append(lines, strings.Join(parts, strings.Repeat(" ", gap)))
	}
	return lines
}

// step is one Stepper entry.
type step struct {
	state state
	title string
}

func renderStepper(steps []step, current int, note []string) []string {
	var lines []string
	for index, entry := range steps {
		title := styleMuted.Render(entry.title)
		glyph := entry.state.glyph()
		if index == current && entry.state != stateError {
			glyph, title = styleAccent.Render("▸"), styleStrong.Render(entry.title)
		}
		lines = append(lines, glyph+" "+styleInk.Render(fmt.Sprintf("%d. ", index+1))+title)
		if index < len(steps)-1 {
			lines = append(lines, styleFrame.Render("│"))
		}
	}
	lines = append(lines, "")
	for _, line := range note {
		lines = append(lines, styleMuted.Render(line))
	}
	return lines
}

// keyHelp renders the KeyHelp row with bubbles/help: `[Tecla] acción`
// joined by " · ".
func keyHelp(bindings []key.Binding, width int) string {
	model := help.New()
	model.ShortSeparator = " · "
	model.Styles.ShortKey = styleAccent
	model.Styles.ShortDesc = styleMuted
	model.Styles.ShortSeparator = styleMuted
	model.Styles.Ellipsis = styleMuted
	model.SetWidth(width)
	return model.ShortHelpView(bindings)
}

func binding(keys []string, label, action string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp("["+label+"]", action))
}

// joinColumns places blocks side by side, top aligned, with gap cells.
func joinColumns(gap int, blocks ...[]string) []string {
	height := 0
	for _, block := range blocks {
		height = max(height, len(block))
	}
	widths := make([]int, len(blocks))
	for index, block := range blocks {
		for _, line := range block {
			widths[index] = max(widths[index], lipgloss.Width(line))
		}
	}
	lines := make([]string, height)
	for row := range height {
		parts := make([]string, len(blocks))
		for index, block := range blocks {
			line := ""
			if row < len(block) {
				line = block[row]
			}
			parts[index] = padRight(line, widths[index])
		}
		lines[row] = strings.Join(parts, strings.Repeat(" ", gap))
	}
	return lines
}

// wrapText wraps plain text to width without styling.
func wrapText(text string, width int) []string {
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		wrapped := lipgloss.Wrap(paragraph, max(width, 1), "")
		lines = append(lines, strings.Split(wrapped, "\n")...)
	}
	return lines
}

func padRight(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

// fitHeight clips or pads lines to exactly height rows.
func fitHeight(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:max(height, 0)]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// displayPath shortens the home directory to ~ and trims long paths from the
// left so the most specific directories stay visible.
func displayPath(path string, width int) string {
	path = sanitizeTerminal(path)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			path = "~"
		} else if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			path = "~/" + rest
		}
	}
	if width > 1 && ansi.StringWidth(path) > width {
		path = "…" + ansi.TruncateLeft(path, ansi.StringWidth(path)-width+1, "")
	}
	return path
}

func sanitizeTerminal(value string) string {
	var result strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			result.WriteString(`\n`)
		case '\r':
			result.WriteString(`\r`)
		case '\t':
			result.WriteString(`\t`)
		case '\x1b':
			result.WriteString(`\x1b`)
		case '\x7f':
			result.WriteString(`\x7f`)
		default:
			if unicode.IsControl(r) || isBidiControl(r) {
				if r <= 0xff {
					fmt.Fprintf(&result, `\x%02x`, r)
				} else {
					fmt.Fprintf(&result, `\u%04x`, r)
				}
				continue
			}
			result.WriteRune(r)
		}
	}
	return result.String()
}

func isBidiControl(r rune) bool {
	return r == '؜' || r == '‎' || r == '‏' ||
		r >= '‪' && r <= '‮' || r >= '⁦' && r <= '⁩'
}
