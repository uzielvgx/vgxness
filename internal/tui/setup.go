package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/uzielvgx/vgxness/internal/claudecli"
)

// Setup del plugin: artboards tui-setup-prerrequisitos, -permisos,
// -confirmar, -aplicando, -resultado and -error.

type setupPhase uint8

const (
	phasePrereq setupPhase = iota
	phasePermissions
	phaseReview
	phaseApplying
	phaseResult
	phaseFailed
)

// What the plugin installs. TestPluginContentsMatchTheRepository keeps these
// in step with plugins/vgxness.
var (
	pluginAgents = []string{"explore", "general", "verifier", "reviewer"}
	pluginHooks  = []string{"SessionStart", "PreCompact", "SessionEnd", "SubagentStart"}
	pluginSkills = []string{"git-delivery"}
)

const pluginMCPServer = "memory"

// setupTick refreshes the elapsed time while a step runs.
var setupTick = time.Second

type setupStateMsg struct {
	value SetupState
	err   error
}

type setupStepMsg struct {
	run    int
	index  int
	output string
	err    error
	took   time.Duration
}

type setupTickMsg struct{ run int }

type stepStatus uint8

const (
	stepPending stepStatus = iota
	stepRunning
	stepDone
	stepFailed
)

type planStep struct {
	kind    SetupStep
	label   string
	command string
	effect  string
	mutates bool
	status  stepStatus
	output  string
	err     error
}

type setupPage struct {
	env     environment
	phase   setupPhase
	state   SetupState
	err     error
	loading bool
	plan    []planStep
	log     []string
	run     int
	cancel  context.CancelFunc
	started time.Time
	now     func() time.Time
	copied  string
}

func newSetupPage(env environment) *setupPage {
	return &setupPage{env: env, loading: true, now: time.Now}
}

func (p *setupPage) Init() tea.Cmd { return p.load() }

func (p *setupPage) load() tea.Cmd {
	env := p.env
	return func() tea.Msg {
		value, err := env.backend.SetupState(env.ctx)
		return setupStateMsg{value: value, err: err}
	}
}

func (p *setupPage) outdated() bool {
	plugin := p.state.Plugin
	return plugin.Installed && plugin.Offered != "" && newer(plugin.Offered, plugin.Version)
}

// buildPlan lists the steps the current state needs, mutating ones first.
func (p *setupPage) buildPlan() []planStep {
	var plan []planStep
	if !p.state.Plugin.MarketplaceAdded {
		plan = append(plan, planStep{kind: StepAddMarketplace, label: "Agregar marketplace " + claudecli.MarketplaceRepo, command: "claude plugin marketplace add " + claudecli.MarketplaceRepo, effect: "modifica ~/.claude/plugins", mutates: true})
	}
	switch {
	case !p.state.Plugin.Installed:
		plan = append(plan, planStep{kind: StepInstallPlugin, label: "Instalar plugin " + claudecli.PluginID, command: "claude plugin install " + claudecli.PluginID, effect: "modifica ~/.claude/plugins", mutates: true})
	case p.outdated():
		plan = append(plan, planStep{kind: StepUpdatePlugin, label: "Actualizar plugin a " + version(p.state.Plugin.Offered), command: "claude plugin update " + claudecli.PluginID, effect: "modifica ~/.claude/plugins", mutates: true})
	}
	return append(plan,
		planStep{kind: StepVerifyPlugin, label: "Verificar plugin habilitado", command: "claude plugin list --json", effect: "solo lectura"},
		planStep{kind: StepVerifyMCP, label: "Verificar servidor MCP «" + pluginMCPServer + "»", command: "claude mcp list", effect: "solo lectura"},
	)
}

func (p *setupPage) mutating() bool {
	for _, step := range p.plan {
		if step.mutates {
			return true
		}
	}
	return false
}

func (p *setupPage) commands() []string {
	var commands []string
	for _, step := range p.buildPlan() {
		if step.mutates {
			commands = append(commands, step.command)
		}
	}
	return commands
}

func (p *setupPage) canContinue() bool {
	return !p.loading && p.err == nil && p.state.Claude.Version != ""
}

func (p *setupPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case setupStateMsg:
		p.loading, p.state, p.err = false, msg.value, msg.err
		return p, nil
	case setupTickMsg:
		if msg.run == p.run && p.phase == phaseApplying {
			return p, p.tick()
		}
		return p, nil
	case setupStepMsg:
		return p.stepFinished(msg)
	case tea.KeyPressMsg:
		return p.updateKeys(msg)
	}
	return p, nil
}

func (p *setupPage) updateKeys(msg tea.KeyPressMsg) (page, tea.Cmd) {
	text := msg.String()
	switch p.phase {
	case phasePrereq:
		switch text {
		case "esc":
			return p, navigate(routeHome)
		case "enter":
			if p.canContinue() {
				p.phase, p.copied = phasePermissions, ""
			}
		case "r":
			p.loading = true
			return p, p.load()
		case "c":
			if commands := p.commands(); len(commands) > 0 {
				p.copied = "Comandos copiados al portapapeles."
				return p, tea.SetClipboard(strings.Join(commands, "\n"))
			}
		}
	case phasePermissions:
		switch text {
		case "esc":
			p.phase, p.copied = phasePrereq, ""
		case "enter":
			p.phase, p.copied, p.plan = phaseReview, "", p.buildPlan()
		case "c":
			p.copied = "JSON copiado al portapapeles."
			return p, tea.SetClipboard(claudecli.PermissionJSON())
		}
	case phaseReview:
		switch text {
		case "esc":
			p.phase = phasePermissions
		case "a":
			return p, p.apply(0)
		}
	case phaseApplying:
		if text == "ctrl+c" && p.cancel != nil {
			p.cancel()
		}
	case phaseResult:
		if text == "enter" || text == "esc" {
			return p, navigate(routeHome)
		}
	case phaseFailed:
		switch text {
		case "esc":
			p.phase, p.plan, p.loading = phasePrereq, nil, true
			return p, p.load()
		case "r":
			for index := range p.plan {
				if p.plan[index].status == stepFailed {
					return p, p.apply(index)
				}
			}
		case "c":
			p.copied = "Detalle copiado al portapapeles."
			return p, tea.SetClipboard(strings.Join(p.log, "\n"))
		}
	}
	return p, nil
}

// apply runs the plan from index on, one step at a time, under a context
// Ctrl+C cancels. Steps already done keep their result.
func (p *setupPage) apply(from int) tea.Cmd {
	ctx, cancel := context.WithCancel(p.env.ctx)
	p.run++
	p.phase, p.cancel, p.started, p.copied = phaseApplying, cancel, p.now(), ""
	for index := from; index < len(p.plan); index++ {
		p.plan[index].status, p.plan[index].output, p.plan[index].err = stepPending, "", nil
	}
	return tea.Batch(p.runStep(ctx, from), p.tick())
}

func (p *setupPage) runStep(ctx context.Context, index int) tea.Cmd {
	p.plan[index].status = stepRunning
	p.log = append(p.log, p.now().Format("15:04:05")+" $ "+p.plan[index].command)
	backend, kind, run, started := p.env.backend, p.plan[index].kind, p.run, p.now()
	return func() tea.Msg {
		output, err := backend.RunSetupStep(ctx, kind)
		if ctx.Err() != nil && err == nil {
			err = ctx.Err()
		}
		return setupStepMsg{run: run, index: index, output: output, err: err, took: time.Since(started)}
	}
}

func (p *setupPage) tick() tea.Cmd {
	run := p.run
	return tea.Tick(setupTick, func(time.Time) tea.Msg { return setupTickMsg{run: run} })
}

func (p *setupPage) stepFinished(msg setupStepMsg) (page, tea.Cmd) {
	if msg.run != p.run || msg.index >= len(p.plan) {
		return p, nil
	}
	step := &p.plan[msg.index]
	step.output, step.err = msg.output, msg.err
	stamp := p.now().Format("15:04:05")
	for _, line := range strings.Split(strings.TrimSpace(msg.output), "\n") {
		if line != "" {
			p.log = append(p.log, stamp+" "+sanitizeTerminal(line))
		}
	}
	if msg.err != nil {
		step.status = stepFailed
		p.log = append(p.log, stamp+" ✕ "+sanitizeTerminal(msg.err.Error()))
		p.phase, p.cancel = phaseFailed, nil
		return p, nil
	}
	step.status = stepDone
	if next := msg.index + 1; next < len(p.plan) {
		ctx, cancel := context.WithCancel(p.env.ctx)
		p.cancel = cancel
		return p, p.runStep(ctx, next)
	}
	p.phase, p.cancel = phaseResult, nil
	return p, p.load()
}

func (p *setupPage) Section() string { return "setup del plugin" }
func (p *setupPage) Captures() bool  { return false }
func (p *setupPage) Busy() bool      { return p.phase == phaseApplying }
func (p *setupPage) Modal(int) []string {
	return nil
}

func (p *setupPage) Keys() []key.Binding {
	esc := binding([]string{"esc"}, "Esc", "volver")
	quit := binding([]string{"q"}, "q", "salir")
	switch p.phase {
	case phasePrereq:
		keys := []key.Binding{}
		if p.canContinue() {
			keys = append(keys, binding([]string{"enter"}, "Enter", "continuar"))
		}
		if len(p.commands()) > 0 {
			keys = append(keys, binding([]string{"c"}, "c", "copiar comandos"))
		}
		return append(keys, binding([]string{"r"}, "r", "revisar de nuevo"), esc, quit)
	case phasePermissions:
		return []key.Binding{binding([]string{"enter"}, "Enter", "continuar"), binding([]string{"c"}, "c", "copiar JSON"), esc, quit}
	case phaseReview:
		return []key.Binding{binding([]string{"a"}, "a", "aplicar"), esc, quit}
	case phaseApplying:
		return []key.Binding{binding([]string{"ctrl+c"}, "Ctrl+C", "cancelar")}
	case phaseResult:
		return []key.Binding{binding([]string{"enter"}, "Enter", "volver al inicio"), quit}
	default:
		return []key.Binding{binding([]string{"r"}, "r", "reintentar"), binding([]string{"c"}, "c", "copiar detalle"), esc, quit}
	}
}

func (p *setupPage) Body(width, height int) []string {
	// Below 100 columns the rail would leave the content too narrow; the
	// panel header still says which step this is.
	contentWidth := width
	var rail []string
	if width >= stackBelow {
		rail = panel(stepperWidth, height, false, sectionLabel("Pasos"), "", renderStepper(p.steps(), p.currentStep(), wrapText("Nada se ejecuta hasta el paso 4, y solo con tu confirmación.", stepperWidth-4)))
		contentWidth = width - stepperWidth - 1
	}
	inner := contentWidth - 4
	var label, aside string
	var body []string
	switch p.phase {
	case phasePrereq:
		label, aside, body = sectionLabel("Prerrequisitos"), styleMuted.Render("· paso 1 de 4"), p.prereqLines(inner)
	case phasePermissions:
		label, aside, body = sectionLabel("Permisos recomendados"), styleMuted.Render("· paso 2 de 4"), p.permissionLines(inner)
	case phaseReview:
		label, aside, body = sectionLabel("Revisar y aplicar"), styleMuted.Render("· paso 3 de 4"), p.reviewLines(inner)
	case phaseResult:
		label, body = outcome(stateOK, p.verb(true)+" completa"), p.resultLines(inner)
	case phaseFailed:
		label, aside, body = outcome(stateError, p.verb(true)+" interrumpida"), styleMuted.Render(fmt.Sprintf("· paso %d/%d", p.failedIndex()+1, len(p.plan))), p.progressLines(inner, height-4)
	default:
		label, aside, body = sectionLabel(p.verb(false)+" plugin"), styleMuted.Render(fmt.Sprintf("· paso 4 de 4 · %d/%d", p.doneCount(), len(p.plan))), p.progressLines(inner, height-4)
	}
	content := panel(contentWidth, height, true, label, aside, body)
	if rail == nil {
		return content
	}
	return joinColumns(1, rail, content)
}

// verb names the operation: install when the plugin was missing, update when
// it was outdated, verify when nothing needed to change.
func (p *setupPage) verb(noun bool) string {
	kind := "verify"
	for _, step := range p.plan {
		switch step.kind {
		case StepInstallPlugin:
			kind = "install"
		case StepUpdatePlugin:
			kind = "update"
		}
	}
	switch {
	case kind == "install" && noun:
		return "Instalación"
	case kind == "install":
		return "Instalando"
	case kind == "update" && noun:
		return "Actualización"
	case kind == "update":
		return "Actualizando"
	case noun:
		return "Verificación"
	default:
		return "Verificando"
	}
}

func (p *setupPage) steps() []step {
	steps := make([]step, len(setupTitles))
	current := p.currentStep()
	for index, title := range setupTitles {
		steps[index] = step{title: title, state: statePending}
		switch {
		case index < current || p.phase == phaseResult:
			steps[index].state = stateOK
		case index == 3 && p.phase == phaseFailed:
			steps[index].state = stateError
		}
	}
	return steps
}

var setupTitles = []string{"Prerrequisitos", "Permisos", "Revisar y aplicar", "Aplicar"}

func (p *setupPage) currentStep() int {
	switch p.phase {
	case phasePrereq:
		return 0
	case phasePermissions:
		return 1
	case phaseReview:
		return 2
	case phaseResult:
		return len(setupTitles)
	default:
		return 3
	}
}

func (p *setupPage) prereqLines(width int) []string {
	if p.loading {
		return []string{stateBusy.glyph() + " " + styleMuted.Render("Revisando Claude Code, el marketplace y el plugin…")}
	}
	if p.err != nil {
		return []string{stateError.glyph() + " " + styleStrong.Render("No se pudo revisar el estado."), styleMuted.Render("Acción: repite con [r].")}
	}
	state := p.state
	onPath := pathBinaryCheck(state.PathBinary)
	marketplace := checkRow{state: stateOK, name: "marketplace", value: "agregado", detail: claudecli.MarketplaceRepo}
	if !state.Plugin.MarketplaceAdded {
		marketplace = checkRow{state: stateError, name: "marketplace", value: "no agregado", detail: claudecli.MarketplaceRepo}
	}
	plugin := checkRow{state: stateOK, name: "plugin", value: version(state.Plugin.Version), detail: claudecli.PluginID + " · al día"}
	switch {
	case !state.Plugin.Installed:
		plugin = checkRow{state: stateError, name: "plugin", value: "no instalado", detail: claudecli.PluginID}
		if state.Plugin.Offered != "" {
			plugin.detail += " · marketplace " + version(state.Plugin.Offered)
		}
	case p.outdated():
		plugin = checkRow{state: stateWarn, name: "plugin", value: version(state.Plugin.Version), detail: version(state.Plugin.Offered) + " disponible"}
	case !state.Plugin.Enabled:
		plugin = checkRow{state: stateWarn, name: "plugin", value: version(state.Plugin.Version), detail: "deshabilitado", action: "claude plugin enable " + claudecli.PluginID}
	}
	claude := claudeCheck(Overview{Claude: state.Claude})
	rows := []checkRow{onPath, claude, marketplace, plugin, {state: stateNeutral, name: "permisos", value: "pendiente", detail: "se recomiendan en el paso 2"}}
	lines := renderChecks(rows, 17, 15, width)
	lines = append(lines, "", sectionLabel("Comandos que se ejecutarán"))
	if commands := p.commands(); len(commands) > 0 {
		for _, command := range commands {
			lines = append(lines, "  "+styleAccent.Render(command))
		}
		lines = append(lines, "")
		lines = append(lines, styledWrap("Puedes copiarlos con [c] y correrlos tú; revisa de nuevo con [r].", width, styleMuted)...)
	} else {
		lines = append(lines, styleMuted.Render("Ninguno: el plugin ya está instalado y al día; el paso 4 solo verifica."))
	}
	lines = append(lines, "", sectionLabel("Qué instala el plugin"),
		styleMuted.Render("agentes      ")+styleInk.Render("vgxness:"+strings.Join(pluginAgents, " · ")),
		styleMuted.Render("hooks        ")+styleInk.Render(strings.Join(pluginHooks, " · ")),
		styleMuted.Render("servidor MCP ")+styleInk.Render(pluginMCPServer+" · vgxness mcp --full"),
		styleMuted.Render("skills       ")+styleInk.Render(strings.Join(pluginSkills, " · ")+" (solo bajo petición)"),
	)
	lines = append(lines, styledWrap("No escribe en el repositorio ni en ~/.claude/settings.json.", width, styleMuted)...)
	if p.copied != "" {
		lines = append(lines, "", stateOK.glyph()+" "+styleMuted.Render(p.copied))
	}
	if state.Claude.Version == "" {
		lines = append([]string{stateError.glyph() + " " + styleStrong.Render("Sin Claude Code no se puede continuar."), ""}, lines...)
	}
	return lines
}

func (p *setupPage) permissionLines(width int) []string {
	lines := styledWrap("El plugin no puede traer reglas de permisos. Agrega esto a ~/.claude/settings.json:", width, styleInk)
	lines = append(lines, "")
	for _, line := range strings.Split(claudecli.PermissionJSON(), "\n") {
		lines = append(lines, styleAccent.Render(ansi.Truncate(line, width, "…")))
	}
	lines = append(lines, "", stateInfo.glyph()+" "+styleInk.Render("Leer y guardar no preguntan; actualizar y olvidar"), "  "+styleInk.Render("piden confirmación cada vez."))
	lines = append(lines, styledWrap("Si lo omites, Claude Code preguntará por cada herramienta. También lo imprime vgxness claude-code setup.", width, styleMuted)...)
	if p.copied != "" {
		lines = append(lines, "", stateOK.glyph()+" "+styleMuted.Render(p.copied))
	}
	return lines
}

func (p *setupPage) reviewLines(width int) []string {
	lines := []string{sectionLabel("Plan")}
	labelWidth := min(44, width-24)
	for index, step := range p.plan {
		lines = append(lines, styleInk.Render(fmt.Sprintf("%d. ", index+1))+padRight(styleInk.Render(ansi.Truncate(step.label, labelWidth, "…")), labelWidth)+" "+styleMuted.Render(step.effect))
	}
	lines = append(lines, "")
	lines = append(lines, styledWrap("No se toca el repositorio ni ~/.claude/settings.json. La memoria sigue en ~/.vgxness.", width, styleMuted)...)
	if p.mutating() {
		lines = append(lines, "", stateWarn.glyph()+" "+styleInk.Render("Al terminar hay que reiniciar Claude Code."))
	}
	return lines
}

func (p *setupPage) doneCount() int {
	done := 0
	for _, step := range p.plan {
		if step.status == stepDone {
			done++
		}
	}
	return done
}

func (p *setupPage) failedIndex() int {
	for index, step := range p.plan {
		if step.status == stepFailed {
			return index
		}
	}
	return len(p.plan) - 1
}

func (p *setupPage) bar(width int) string {
	ratio := 0.0
	if len(p.plan) > 0 {
		ratio = float64(p.doneCount()) / float64(len(p.plan))
	}
	bar := progress.New(progress.WithColors(colorAccent, colorAccentStrong), progress.WithFillCharacters('█', '░'), progress.WithoutPercentage(), progress.WithWidth(min(40, width-16)))
	bar.EmptyColor = colorPanelRaised
	tail := fmt.Sprintf("  %3d %%  ", int(ratio*100))
	switch p.phase {
	case phaseFailed:
		tail += "detenido"
	case phaseApplying:
		tail += fmt.Sprintf("%d s", int(p.now().Sub(p.started).Seconds()))
	}
	return bar.ViewAs(ratio) + styleMuted.Render(tail)
}

func (p *setupPage) stepRows(width int) []string {
	var lines []string
	for _, step := range p.plan {
		glyph, status := statePending.glyph(), ""
		switch step.status {
		case stepRunning:
			glyph, status = stateBusy.glyph(), "…"
		case stepDone:
			glyph, status = stateOK.glyph(), "listo"
		case stepFailed:
			glyph, status = stateError.glyph(), "error"
		}
		lines = append(lines, glyph+" "+padRight(styleInk.Render(ansi.Truncate(step.label, 34, "…")), 35)+padRight(styleMuted.Render(status), 7)+styleMuted.Render(ansi.Truncate(step.command, max(width-45, 8), "…")))
		if step.status == stepFailed && step.err != nil {
			for _, part := range wrapText(sanitizeTerminal(step.err.Error()), max(width-45, 20)) {
				lines = append(lines, strings.Repeat(" ", 44)+styleMuted.Render(part))
			}
		}
	}
	return lines
}

func (p *setupPage) progressLines(width, height int) []string {
	lines := []string{p.bar(width), ""}
	lines = append(lines, p.stepRows(width)...)
	lines = append(lines, "")
	if p.phase == phaseFailed {
		lines = append(lines, styledWrap(p.failureAction(), width, styleMuted)...)
		if p.copied != "" {
			lines = append(lines, stateOK.glyph()+" "+styleMuted.Render(p.copied))
		}
	} else {
		lines = append(lines, styledWrap("Solo [Ctrl+C] cancela mientras se aplica; lo ya instalado se conserva.", width, styleMuted)...)
	}
	lines = append(lines, "", sectionLabel("Registro"))
	room := max(height-len(lines)-2, 1)
	log := p.log
	if len(log) > room {
		log = log[len(log)-room:]
	}
	for _, line := range log {
		lines = append(lines, styleMuted.Render(ansi.Truncate(line, width, "…")))
	}
	return lines
}

func (p *setupPage) failureAction() string {
	step := p.plan[p.failedIndex()]
	if step.err == context.Canceled {
		return "Cancelaste la aplicación. Lo ya hecho se conserva; reintenta con [r]."
	}
	switch step.kind {
	case StepAddMarketplace:
		return "Acción: revisa tu conexión y que el repositorio " + claudecli.MarketplaceRepo + " sea accesible, y reintenta con [r]."
	case StepInstallPlugin, StepUpdatePlugin:
		return "Acción: ejecuta  claude plugin marketplace update " + claudecli.MarketplaceName + "  y reintenta con [r]. Nada más cambió en tu equipo."
	case StepVerifyMCP:
		return "Acción: comprueba que vgxness esté en el PATH (vgxness version) y reintenta con [r]."
	default:
		return "Acción: revisa el registro y reintenta con [r]."
	}
}

func (p *setupPage) resultLines(width int) []string {
	lines := []string{p.bar(width), ""}
	plugin := p.state.Plugin
	pluginVersion := version(plugin.Version)
	mcp := ""
	for _, step := range p.plan {
		if step.kind == StepVerifyMCP {
			mcp = step.output
		}
	}
	agents := len(pluginAgents)
	if plugin.Agents > 0 {
		agents = plugin.Agents
	}
	rows := []checkRow{
		{state: stateOK, name: "plugin", value: pluginVersion, detail: claudecli.PluginID},
		{state: stateOK, name: "servidor MCP", value: pluginMCPServer, detail: "conectado"},
		{state: stateOK, name: "agentes", value: itoa(agents), detail: strings.Join(pluginAgents, " · ")},
		{state: stateOK, name: "hooks", value: itoa(len(pluginHooks)), detail: strings.Join(pluginHooks, " · ")},
	}
	lines = append(lines, renderChecks(rows, 17, 10, width)...)
	if mcp != "" {
		lines = append(lines, "", styleMuted.Render(ansi.Truncate(mcp, width, "…")))
	}
	lines = append(lines, "")
	if p.mutating() {
		lines = append(lines, stateWarn.glyph()+" "+styleInk.Render("Reinicia Claude Code para cargar el plugin."))
	}
	lines = append(lines, styledWrap("La próxima sesión en este proyecto recibirá la política del Manager y el último handoff.", width, styleMuted)...)
	return lines
}
