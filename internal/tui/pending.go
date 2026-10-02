package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// pendingPage stands in for a module whose slice of plan task 17 has not
// landed yet. It is removed once Memoria, Setup and Sync exist.
type pendingPage struct {
	env environment
	to  route
}

func newPendingPage(env environment, to route) *pendingPage { return &pendingPage{env: env, to: to} }

func (p *pendingPage) Init() tea.Cmd { return nil }

func (p *pendingPage) Update(msg tea.Msg) (page, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "esc" {
		return p, navigate(routeHome)
	}
	return p, nil
}

func (p *pendingPage) Section() string {
	switch p.to {
	case routeSetup:
		return "setup del plugin"
	default:
		return "sync entre equipos"
	}
}

func (p *pendingPage) Keys() []key.Binding {
	return []key.Binding{binding([]string{"esc"}, "Esc", "volver"), binding([]string{"q"}, "q", "salir")}
}

func (p *pendingPage) Body(width, height int) []string {
	return panel(width, height, false, sectionLabel(p.Section()), "", []string{stateInfo.glyph() + " " + styleInk.Render("Este módulo todavía no está disponible en la consola.")})
}

func (p *pendingPage) Modal(int) []string { return nil }
func (p *pendingPage) Captures() bool     { return false }
func (p *pendingPage) Busy() bool         { return false }
