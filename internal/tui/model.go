// Package tui is the VGXNESS console (`vgxness tui`). It follows the design
// canvas linked from DESIGN.md and shows only real product state (decision
// D-004): Inicio, Setup del plugin, Memoria, Sync and Diagnóstico.
package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type route uint8

const (
	routeHome route = iota
	routeMemory
	routeHandoffs
	routeSetup
	routeSync
	routeDoctor
)

// navigateMsg asks the console to open another module.
type navigateMsg struct{ to route }

func navigate(to route) tea.Cmd { return func() tea.Msg { return navigateMsg{to: to} } }

// page is one console module. Body and Modal receive the space they may use
// and return exactly the lines to draw.
type page interface {
	Init() tea.Cmd
	Update(tea.Msg) (page, tea.Cmd)
	// Section is the header subtitle; Inicio returns "" and gets the banner.
	Section() string
	Keys() []key.Binding
	Body(width, height int) []string
	// Modal returns the dialog lines while a modal is open, else nil.
	Modal(width int) []string
	// Captures reports a focused text input, so `q` is typed instead of
	// quitting the console.
	Captures() bool
	// Busy reports a step that mutates state; only Ctrl+C reaches the page.
	Busy() bool
}

type Options struct {
	Workspace string
}

type Model struct {
	ctx     context.Context
	backend Backend
	options Options
	width   int
	height  int
	blurred bool
	page    page
}

func NewModel(ctx context.Context, backend Backend, options Options) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	m := Model{ctx: ctx, backend: backend, options: options}
	m.page = m.open(routeHome)
	return m
}

func (m Model) open(to route) page {
	env := environment{ctx: m.ctx, backend: m.backend, workspace: m.options.Workspace}
	switch to {
	case routeDoctor:
		return newDoctorPage(env)
	case routeMemory:
		return newMemoryPage(env, false)
	case routeHandoffs:
		return newMemoryPage(env, true)
	case routeSetup, routeSync:
		return newPendingPage(env, to)
	default:
		return newHomePage(env)
	}
}

// environment is what every page shares.
type environment struct {
	ctx       context.Context
	backend   Backend
	workspace string
}

func (m Model) Init() tea.Cmd { return m.page.Init() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.BlurMsg:
		m.blurred = true
		return m, nil
	case tea.FocusMsg:
		m.blurred = false
		return m, nil
	case navigateMsg:
		m.page = m.open(msg.to)
		return m, m.page.Init()
	case tea.KeyPressMsg:
		if m.page.Busy() {
			if msg.String() != "ctrl+c" {
				return m, nil
			}
			break
		}
		if msg.String() == "ctrl+c" || (msg.String() == "q" && !m.page.Captures() && m.page.Modal(0) == nil) || (m.tooSmall() && msg.String() == "q") {
			return m, tea.Quit
		}
		if m.tooSmall() {
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.page, cmd = m.page.Update(msg)
	return m, cmd
}

func (m Model) View() tea.View {
	content := m.render()
	if m.blurred {
		content = faint(content)
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.ReportFocus = true
	view.WindowTitle = "VGXNESS Console"
	view.BackgroundColor = colorCanvas
	return view
}

func (m Model) tooSmall() bool { return m.width < minimumWidth || m.height < minimumHeight }

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.tooSmall() {
		return m.renderTooSmall()
	}
	inner := m.width - 2
	header := m.header(inner)
	modal := m.page.Modal(inner)
	help := ""
	if modal == nil {
		help = keyHelp(m.page.Keys(), inner)
	}
	bodyHeight := m.height - len(header) - 2
	body := fitHeight(m.page.Body(inner, bodyHeight), bodyHeight)
	lines := append(append(append(header, ""), body...), help)
	for index, line := range lines {
		lines[index] = " " + line
	}
	screen := strings.Join(fitHeight(lines, m.height), "\n")
	if modal == nil {
		return screen
	}
	dialog := strings.Join(modal, "\n")
	x := max((m.width-lipgloss.Width(dialog))/2, 0)
	y := max((m.height-lipgloss.Height(dialog))/2, 0)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(faint(screen)),
		lipgloss.NewLayer(dialog).X(x).Y(y).Z(1),
	).Render()
}

func (m Model) header(width int) []string {
	section := m.page.Section()
	if section == "" && width >= bannerColumns-2 {
		const prefix = "CONSOLA VGXNESS  ·  memoria y estado local   workspace  "
		title := styleLabel.Render("CONSOLA VGXNESS") + styleMuted.Render("  ·  memoria y estado local   workspace  ") + styleInk.Render(displayPath(m.options.Workspace, width-len([]rune(prefix))))
		return append(banner(), title)
	}
	if section == "" {
		section = "memoria y estado local"
	}
	prefix := section + "   │   workspace  "
	return []string{
		styleLabel.Render("VGXNESS / CONSOLA"),
		styleMuted.Render(prefix) + styleInk.Render(displayPath(m.options.Workspace, width-lipgloss.Width(prefix))),
	}
}

func (m Model) renderTooSmall() string {
	width := max(m.width, 1)
	message := strings.Join([]string{
		styleLabel.Render("VGXNESS / CONSOLA"),
		styleFrame.Render(strings.Repeat("─", min(width, 60))),
		stateWarn.glyph() + " " + styleStrong.Render("Se necesita más espacio"),
		styleMuted.Render(fmt.Sprintf("  Mínimo %d×%d; la terminal actual es %d×%d.", minimumWidth, minimumHeight, m.width, m.height)),
		styleMuted.Render("  Agranda la ventana y la consola vuelve sola."),
		"  " + keyHint("q", "salir"),
	}, "\n")
	return lipgloss.Place(width, max(m.height, 1), lipgloss.Center, lipgloss.Center, message)
}
