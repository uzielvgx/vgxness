package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Diagnóstico: artboards tui-doctor-diagnostico and tui-doctor-error.

type diagnosisMsg struct {
	value Diagnosis
	err   error
}

type doctorPage struct {
	env     environment
	data    Diagnosis
	err     error
	loading bool
	copied  string
}

func newDoctorPage(env environment) *doctorPage { return &doctorPage{env: env, loading: true} }

func (p *doctorPage) Init() tea.Cmd { return p.load() }

func (p *doctorPage) load() tea.Cmd {
	env := p.env
	return func() tea.Msg {
		if env.backend == nil {
			return diagnosisMsg{err: fmt.Errorf("backend unavailable")}
		}
		value, err := env.backend.Diagnose(env.ctx)
		return diagnosisMsg{value: value, err: err}
	}
}

// suggestions are the warning and error rows that say what to do; the ones
// with a command can be copied with their number.
func (p *doctorPage) suggestions() []checkRow {
	var rows []checkRow
	for _, section := range diagnosisSections(p.data) {
		for _, row := range section.rows {
			if (row.state == stateWarn || row.state == stateError) && (row.command != "" || row.action != "") {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func (p *doctorPage) copyable() []checkRow {
	var rows []checkRow
	for _, row := range p.suggestions() {
		if row.command != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

func (p *doctorPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case diagnosisMsg:
		p.loading, p.data, p.err, p.copied = false, msg.value, msg.err, ""
		return p, nil
	case tea.KeyPressMsg:
		switch text := msg.String(); text {
		case "esc":
			return p, navigate(routeHome)
		case "r":
			p.loading = true
			return p, p.load()
		case "c":
			if !p.loading && p.err == nil {
				p.copied = "informe"
				return p, tea.SetClipboard(p.report())
			}
		default:
			copyable := p.copyable()
			if len(text) == 1 && text[0] >= '1' && int(text[0]-'1') < len(copyable) {
				p.copied = "comando"
				return p, tea.SetClipboard(copyable[text[0]-'1'].command)
			}
		}
	}
	return p, nil
}

func (p *doctorPage) Section() string { return "diagnóstico" }

func (p *doctorPage) Keys() []key.Binding {
	keys := []key.Binding{binding([]string{"r"}, "r", "repetir"), binding([]string{"c"}, "c", "copiar informe")}
	switch n := len(p.copyable()); {
	case n == 1:
		keys = append(keys, binding([]string{"1"}, "1", "copiar comando"))
	case n > 1:
		keys = append(keys, binding([]string{"1"}, fmt.Sprintf("1-%d", n), "copiar comando"))
	}
	return append(keys, binding([]string{"esc"}, "Esc", "volver"))
}

func (p *doctorPage) Modal(int) []string { return nil }
func (p *doctorPage) Captures() bool     { return false }
func (p *doctorPage) Busy() bool         { return false }

func (p *doctorPage) counts() (ok, warn, failed, total int) {
	for _, section := range diagnosisSections(p.data) {
		for _, row := range section.rows {
			total++
			switch row.state {
			case stateOK:
				ok++
			case stateWarn:
				warn++
			case stateError:
				failed++
			}
		}
	}
	return ok, warn, failed, total
}

func (p *doctorPage) Body(width, height int) []string {
	aside := "vgxness doctor"
	if !p.data.At.IsZero() {
		aside += " · " + p.data.At.Local().Format("2006-01-02 15:04")
	}
	if width < stackBelow {
		return panel(width, height, false, sectionLabel("Diagnóstico"), styleMuted.Render(aside), append(append(p.mainLines(width-4), ""), p.summaryLines(width-4)...))
	}
	main := width - sidePanel - 1
	return joinColumns(1,
		panel(main, height, false, sectionLabel("Diagnóstico"), styleMuted.Render(aside), p.mainLines(main-4)),
		panel(sidePanel, height, false, sectionLabel("Resumen"), "", p.summaryLines(sidePanel-4)),
	)
}

func (p *doctorPage) mainLines(width int) []string {
	if p.loading {
		return []string{stateBusy.glyph() + " " + styleMuted.Render("Revisando almacenamiento, plugin y servidor MCP…")}
	}
	if p.err != nil {
		return []string{stateError.glyph() + " " + styleStrong.Render("No se pudo completar el diagnóstico."), styleMuted.Render("Acción: ejecuta  vgxness doctor  en la terminal y repite con ") + styleAccent.Render("[r]")}
	}
	var lines []string
	if _, _, failed, _ := p.counts(); failed > 0 {
		message := fmt.Sprintf("%s críticos.", plural(failed, "problema", "problemas"))
		if p.data.Storage.Err != nil {
			message += " La consola no puede leer la memoria hasta resolverlos."
		}
		for index, part := range wrapText(message, width-2) {
			prefix := "  "
			if index == 0 {
				prefix = stateError.glyph() + " "
			}
			lines = append(lines, prefix+styleStrong.Render(part))
		}
		lines = append(lines, "")
	}
	for index, section := range diagnosisSections(p.data) {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, sectionLabel(section.label))
		lines = append(lines, renderChecks(section.rows, 15, 10, width)...)
	}
	return lines
}

func (p *doctorPage) summaryLines(width int) []string {
	if p.loading || p.err != nil {
		return nil
	}
	ok, warn, failed, total := p.counts()
	lines := []string{
		stateOK.glyph() + " " + styleInk.Render(itoa(ok)) + "  " + stateWarn.glyph() + " " + styleInk.Render(itoa(warn)) + "  " + stateError.glyph() + " " + styleInk.Render(itoa(failed)),
		styleMuted.Render(fmt.Sprintf("%s · %.1f s", plural(total, "comprobación", "comprobaciones"), p.data.Took.Seconds())),
		"",
		sectionLabel("Acciones sugeridas"),
	}
	suggestions := p.suggestions()
	if len(suggestions) == 0 {
		lines = append(lines, stateOK.glyph()+" "+styleInk.Render("Nada que corregir."))
	}
	copyIndex := 0
	for index, row := range suggestions {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, row.state.glyph()+" "+styleInk.Render(suggestionTitle(row)))
		if row.command == "" {
			for _, part := range wrapText(row.action, width-2) {
				lines = append(lines, "  "+styleMuted.Render(part))
			}
			continue
		}
		copyIndex++
		lines = append(lines,
			"  "+styleAccent.Render(ansi.Truncate(row.command, width-2, "…")),
			"  "+keyHint(itoa(copyIndex), "copiar comando"),
		)
	}
	if p.copied != "" {
		lines = append(lines, "", stateOK.glyph()+" "+styleMuted.Render(strings.ToUpper(p.copied[:1])+p.copied[1:]+" copiado al portapapeles."))
	}
	return lines
}

func suggestionTitle(row checkRow) string {
	switch {
	case strings.Contains(row.command, " update "):
		return "Actualiza el plugin"
	case strings.Contains(row.command, " install "):
		return "Instala el plugin"
	case strings.Contains(row.command, "marketplace add"):
		return "Agrega el marketplace"
	case strings.Contains(row.command, " enable "):
		return "Habilita el plugin"
	case row.name == "memory.db" || row.name == "esquema":
		return "Repara la base de datos"
	case row.name == "Claude Code":
		return "Actualiza Claude Code"
	default:
		return "Revisa " + row.name
	}
}

// report is the plain-text diagnosis copied with [c].
func (p *doctorPage) report() string {
	var out strings.Builder
	fmt.Fprintf(&out, "vgxness doctor · %s\n", p.data.At.Local().Format(time.RFC3339))
	for _, section := range diagnosisSections(p.data) {
		fmt.Fprintf(&out, "\n%s\n", strings.ToUpper(section.label))
		for _, row := range section.rows {
			fmt.Fprintf(&out, "%s %-15s %-10s %s\n", ansi.Strip(row.state.glyph()), row.name, row.value, row.detail)
			if row.action != "" {
				fmt.Fprintf(&out, "  Acción: %s\n", row.action)
			}
		}
	}
	return out.String()
}
