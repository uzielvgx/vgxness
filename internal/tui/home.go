package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Inicio: artboards tui-inicio-estado, tui-inicio-estado-avisos and
// tui-inicio-sin-instalar.

type overviewMsg struct {
	value Overview
	err   error
}

type homePage struct {
	env      environment
	data     Overview
	err      error
	loading  bool
	selected int
	now      func() time.Time
}

func newHomePage(env environment) *homePage {
	return &homePage{env: env, loading: true, now: time.Now}
}

func (p *homePage) Init() tea.Cmd { return p.load() }

func (p *homePage) load() tea.Cmd {
	env := p.env
	return func() tea.Msg {
		if env.backend == nil {
			return overviewMsg{err: fmt.Errorf("backend unavailable")}
		}
		value, err := env.backend.Overview(env.ctx)
		return overviewMsg{value: value, err: err}
	}
}

func (p *homePage) firstUse() bool {
	return !p.loading && p.err == nil && !p.data.Plugin.Installed
}

func (p *homePage) cards() []card {
	if p.firstUse() {
		return []card{
			{key: "1", title: "Instalar plugin", detail: "nada corre sin confirmar"},
			{key: "2", title: "Diagnóstico", detail: "vgxness doctor"},
		}
	}
	setup := "instalar o actualizar"
	if p.pluginOutdated() {
		setup = "actualizar a " + version(p.data.Plugin.Offered)
	}
	sync := "estado y credencial"
	if !p.data.Sync.Configured {
		sync = "configurar"
	}
	return []card{
		{key: "1", title: "Memoria", detail: "buscar, leer, handoffs"},
		{key: "2", title: "Setup del plugin", detail: setup},
		{key: "3", title: "Sync", detail: sync},
		{key: "4", title: "Diagnóstico", detail: "vgxness doctor"},
	}
}

func (p *homePage) targets() []route {
	if p.firstUse() {
		return []route{routeSetup, routeDoctor}
	}
	return []route{routeMemory, routeSetup, routeSync, routeDoctor}
}

func (p *homePage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case overviewMsg:
		p.loading, p.data, p.err = false, msg.value, msg.err
		p.selected = min(p.selected, len(p.cards())-1)
		if p.pluginOutdated() || (!p.firstUse() && !p.data.Plugin.Enabled) {
			p.selected = 1
		}
		return p, nil
	case tea.KeyPressMsg:
		targets := p.targets()
		switch msg.String() {
		case "r":
			p.loading = true
			return p, p.load()
		case "h":
			if !p.firstUse() {
				return p, navigate(routeMemory)
			}
		case "left", "shift+tab":
			p.selected = (p.selected + len(targets) - 1) % len(targets)
		case "right", "tab":
			p.selected = (p.selected + 1) % len(targets)
		case "enter":
			return p, navigate(targets[p.selected])
		default:
			if text := msg.String(); len(text) == 1 && text[0] >= '1' && int(text[0]-'1') < len(targets) {
				p.selected = int(text[0] - '1')
			}
		}
	}
	return p, nil
}

func (p *homePage) Section() string { return "" }

func (p *homePage) Keys() []key.Binding {
	choose := fmt.Sprintf("1-%d", len(p.targets()))
	keys := []key.Binding{
		binding([]string{"1", "2", "3", "4"}, choose, "elegir"),
		binding([]string{"enter"}, "Enter", "abrir"),
		binding([]string{"r"}, "r", "actualizar"),
	}
	if !p.firstUse() {
		keys = append(keys, binding([]string{"h"}, "h", "handoffs"))
	}
	return append(keys, binding([]string{"q"}, "q", "salir"))
}

func (p *homePage) Modal(int) []string { return nil }
func (p *homePage) Captures() bool     { return false }
func (p *homePage) Busy() bool         { return false }

func (p *homePage) Body(width, height int) []string {
	actions := panel(width, 9, false, sectionLabel("Acciones"), "", append(
		renderCards(p.cards(), p.selected, width-4),
		styleMuted.Render("elige ")+styleAccent.Render(fmt.Sprintf("[1-%d]", len(p.targets())))+styleMuted.Render(" y ")+styleAccent.Render("[Enter]"),
	))
	top := max(height-len(actions)-1, 6)
	aside := p.asidePanel()
	var row []string
	if width >= stackBelow {
		main := width - sidePanel - 1
		row = joinColumns(1, panel(main, top, false, sectionLabel("Estado"), p.freshness(), p.statusLines(main-4)), panel(sidePanel, top, false, aside.label, "", aside.lines))
	} else {
		row = panel(width, top, false, sectionLabel("Estado"), p.freshness(), p.statusLines(width-4))
	}
	return append(append(row, ""), actions...)
}

func (p *homePage) freshness() string {
	if p.loading {
		return styleMuted.Render("◐ leyendo…")
	}
	return styleMuted.Render("actualizado "+ago(p.now(), p.data.At)+" · ") + keyHint("r", "actualizar")
}

type asideContent struct {
	label string
	lines []string
}

func (p *homePage) asidePanel() asideContent {
	inner := sidePanel - 4
	if p.firstUse() {
		lines := []string{stateInfo.glyph() + " " + styleStrong.Render("Bienvenido."), ""}
		lines = append(lines, styledWrap("Guarda lo que el proyecto aprende (decisiones, notas, handoffs) en ~/.vgxness/memory.db y se lo devuelve a Claude Code en cada sesión.", inner, styleInk)...)
		lines = append(lines, "",
			styleInk.Render("1. Instala el plugin con ")+styleAccent.Render("[1]")+styleInk.Render("."),
			styleInk.Render("2. Reinicia Claude Code."),
			styleInk.Render("3. Trabaja normal: la memoria se"),
			styleInk.Render("   llena sola."),
			"",
		)
		lines = append(lines, styledWrap("Sync es opcional y se configura después.", inner, styleMuted)...)
		return asideContent{label: sectionLabel("Qué hace VGXNESS"), lines: lines}
	}
	if len(p.data.Handoffs) == 0 {
		lines := append(styledWrap("Todavía no hay handoffs. Claude Code deja uno al terminar una sesión con trabajo sustancial.", inner, styleMuted), "")
		lines = append(lines, styledWrap("Se inyecta al iniciar Claude Code aquí, como datos, nunca como instrucciones.", inner, styleMuted)...)
		return asideContent{label: sectionLabel("Último handoff"), lines: lines}
	}
	last := p.data.Handoffs[0]
	lines := []string{
		styleLabel.Render(sessionLabel(last.Handle)) + styleMuted.Render(" · "+shortDate(last.Completed)+" · "+duration(last.Completed.Sub(last.Started))),
		"",
	}
	lines = append(lines, styledWrap(sanitizeTerminal(last.Summary), inner, styleInk)...)
	lines = append(lines, "")
	lines = append(lines, styledWrap("Se inyecta al iniciar Claude Code aquí, como datos, nunca como instrucciones.", inner, styleMuted)...)
	lines = append(lines, keyHint("h", "ver todos"))
	return asideContent{label: sectionLabel("Último handoff"), lines: lines}
}

func (p *homePage) pluginOutdated() bool {
	plugin := p.data.Plugin
	return plugin.Installed && plugin.Offered != "" && plugin.Offered != plugin.Version && newer(plugin.Offered, plugin.Version)
}

func (p *homePage) statusLines(width int) []string {
	if p.loading && p.data.At.IsZero() {
		return []string{stateBusy.glyph() + " " + styleMuted.Render("Leyendo el estado local…")}
	}
	if p.err != nil {
		return []string{stateError.glyph() + " " + styleStrong.Render("No se pudo leer el estado."), styleMuted.Render("Acción: abre el diagnóstico con ") + styleAccent.Render("[4]")}
	}
	var lines []string
	if p.pluginOutdated() {
		lines = append(lines, stateWarn.glyph()+" "+styleInk.Render("Actualiza el plugin y reinicia Claude Code para cargarlo."), "")
	}
	lines = append(lines, renderChecks(overviewChecks(p.data, p.firstUse()), 16, 15, width)...)
	recent := p.data.Handoffs
	if len(recent) > 3 {
		recent = recent[:3]
	}
	if len(recent) > 0 {
		lines = append(lines, "", sectionLabel("Sesiones recientes"))
		for _, handoff := range recent {
			lines = append(lines, padRight(styleMuted.Render(ago(p.now(), handoff.Completed)), 13)+styleInk.Render(sessionLabel(handoff.Handle)+" cerrada · handoff guardado"))
		}
	}
	return lines
}

func styledWrap(text string, width int, style interface{ Render(...string) string }) []string {
	lines := wrapText(text, width)
	for index, line := range lines {
		lines[index] = style.Render(line)
	}
	return lines
}
