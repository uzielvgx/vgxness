// Package tui is the VGXNESS console. This is its shell: the program loop,
// brand header, terminal-size guard and text helpers. The console modules
// (Inicio, Setup del plugin, Memoria, Sync, Doctor) are rebuilt from the
// design canvas in plan task 17; until then the shell shows a placeholder.
package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	minimumWidth  = 42
	minimumHeight = 10
)

var (
	softbricCanvas   = lipgloss.Color("#071522")
	softbricDelivery = lipgloss.Color("#005F5C")
	softbricBric     = lipgloss.Color("#008B87")
	softbricAqua     = lipgloss.Color("#4DD4D4")

	studioAccent = lipgloss.NewStyle().Foreground(softbricAqua).Bold(true)
	studioMuted  = lipgloss.NewStyle().Foreground(softbricDelivery)
)

type Options struct {
	Workspace string
}

type Model struct {
	ctx     context.Context
	options Options
	width   int
	height  int
}

func NewModel(ctx context.Context, options Options) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	return Model{ctx: ctx, options: options}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "VGXNESS"
	return view
}

func (m Model) render() string {
	width := max(1, m.width)
	if m.tooSmall() {
		lines := []string{
			"VGXNESS",
			strings.Repeat("─", width),
			"! Terminal demasiado pequeña",
			fmt.Sprintf("  Se necesita al menos %d×%d; la terminal actual es %d×%d.", minimumWidth, minimumHeight, m.width, m.height),
			"  [q] salir",
		}
		return fit(lines, width, m.height)
	}
	lines := append(m.brandHeader(), "", studioMuted.Render("La consola está en construcción. [q] salir"))
	return lipgloss.NewStyle().Background(softbricCanvas).Width(width).Render(fit(lines, width, m.height))
}

// headerWorkspace keeps the workspace on one header line by trimming the path
// from the left, which preserves the most specific trailing directories.
func (m Model) headerWorkspace(maxWidth int) string {
	path := sanitizeTerminal(m.options.Workspace)
	runes := []rune(path)
	if maxWidth < 4 {
		maxWidth = 4
	}
	if len(runes) > maxWidth {
		path = "…" + string(runes[len(runes)-(maxWidth-1):])
	}
	return studioMuted.Render("workspace  ") + path
}

func (m Model) brandHeader() []string {
	const prefix = "CONSOLA   workspace  "
	title := studioAccent.Render("CONSOLA") + "   " + m.headerWorkspace(m.width-lipgloss.Width(prefix))
	if m.wide() {
		return append(softbricBanner(), title)
	}
	return []string{studioAccent.Render("VGXNESS"), title}
}

func softbricBanner() []string {
	columns := [][]string{
		{"██╗   ██╗", "╚██╗ ██╔╝", " ╚████╔╝ ", "  ╚██╔╝  ", "   ██║   ", "   ╚═╝   "},       // V
		{" ██████╗", "██╔════╝", "██║  ███╗", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},         // G
		{"██╗  ██╗", "╚██╗██╔╝", " ╚███╔╝ ", " ██╔██╗ ", "██╔╝ ██╗", "╚═╝  ╚═╝"},             // X
		{"███╗   ██╗", "████╗  ██║", "██╔██╗ ██║", "██║╚██╗██║", "██║ ╚████║", "╚═╝  ╚═══╝"}, // N
		{"███████╗", "██╔════╝", "█████╗  ", "██╔══╝  ", "███████╗", "╚══════╝"},             // E
		{"███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"},             // S
		{"███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"},             // S
	}
	lines := make([]string, len(columns[0]))
	for row := range lines {
		parts := make([]string, len(columns))
		for column := range columns {
			parts[column] = padLine(columns[column][row], lipgloss.Width(columns[column][0]))
		}
		lines[row] = strings.Join(parts, " ")
	}
	gradient := lipgloss.Blend1D(len(lines), softbricAqua, softbricBric)
	for index, line := range lines {
		lines[index] = lipgloss.NewStyle().Foreground(gradient[index]).Bold(true).Render(line)
	}
	return lines
}

func (m Model) tooSmall() bool {
	return m.width < minimumWidth || m.height < minimumHeight
}

func (m Model) wide() bool {
	return m.width >= 100
}

func padLine(value string, width int) string {
	value = ansi.Truncate(value, width, "")
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func fit(lines []string, width, height int) string {
	width = max(1, width)
	var rendered []string
	for _, line := range lines {
		wrapped := lipgloss.Wrap(line, width, "")
		for _, part := range strings.Split(wrapped, "\n") {
			rendered = append(rendered, ansi.Truncate(part, width, ""))
		}
	}
	if height > 0 && len(rendered) > height {
		if height < 2 {
			return strings.Join(rendered[:height], "\n")
		}
		footer := append([]string(nil), rendered[len(rendered)-2:]...)
		rendered = append(rendered[:height-len(footer)], footer...)
	}
	return strings.Join(rendered, "\n")
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
