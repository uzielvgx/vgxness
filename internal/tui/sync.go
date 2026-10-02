package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Sync: artboards tui-sync-estado, -login (here: configure), -en-progreso
// and -error. The device-pairing login of the canvas has no backend
// (decision D-004); configuring takes the endpoint, device ID and bearer that
// `memory sync configure` takes.

type syncMode uint8

const (
	syncStatus syncMode = iota
	syncConfigure
	syncRunning
	syncFailed
)

// spinnerFrames and spinnerTick follow DESIGN.md (◐ ◓ ◑ ◒ at 1/8 s).
var (
	spinnerFrames = []string{"◐", "◓", "◑", "◒"}
	spinnerTick   = 125 * time.Millisecond
)

type syncOverviewMsg struct {
	value SyncOverview
	err   error
}

type syncConfiguredMsg struct{ err error }

type syncDoneMsg struct {
	run     int
	outcome SyncOutcome
	err     error
}

type syncSpinMsg struct{ run int }

type syncPage struct {
	env      environment
	mode     syncMode
	data     SyncOverview
	err      error
	loading  bool
	selected int

	fields  [3]textinput.Model
	field   int
	formErr string
	saved   bool
	last    *SyncOutcome
	failure SyncOutcome
	failErr error
	run     int
	frame   int
	cancel  context.CancelFunc
	started time.Time
	now     func() time.Time
	copied  string
}

func newSyncPage(env environment) *syncPage {
	p := &syncPage{env: env, loading: true, now: time.Now}
	placeholders := [3]string{"https://sync.ejemplo.com", "550e8400-e29b-41d4-a716-446655440000", "pega el bearer"}
	for index := range p.fields {
		input := textinput.New()
		input.Prompt = "> "
		input.Placeholder = placeholders[index]
		input.CharLimit = 4096
		styles := textinput.DefaultDarkStyles()
		styles.Focused.Prompt, styles.Blurred.Prompt = styleAccent, styleMuted
		styles.Focused.Text, styles.Blurred.Text = styleInk, styleMuted
		styles.Focused.Placeholder, styles.Blurred.Placeholder = styleFaint, styleFaint
		styles.Cursor.Color, styles.Cursor.Blink = colorAccent, false
		input.SetStyles(styles)
		p.fields[index] = input
	}
	p.fields[2].EchoMode = textinput.EchoPassword
	p.fields[2].EchoCharacter = '•'
	return p
}

func (p *syncPage) Init() tea.Cmd { return p.load() }

func (p *syncPage) load() tea.Cmd {
	env := p.env
	return func() tea.Msg {
		value, err := env.backend.SyncOverview(env.ctx)
		return syncOverviewMsg{value: value, err: err}
	}
}

func (p *syncPage) ready() bool {
	return p.data.Configured && p.data.Enabled && p.data.Credential == "available" && p.data.PortableID != ""
}

func (p *syncPage) initCommand() string {
	workspace, err := filepath.Abs(p.env.workspace)
	if err != nil {
		workspace = p.env.workspace
	}
	return "vgxness memory project init --workspace " + workspace
}

func (p *syncPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case syncOverviewMsg:
		p.loading, p.data, p.err = false, msg.value, msg.err
		return p, nil
	case syncConfiguredMsg:
		if msg.err != nil {
			p.formErr = configureError(msg.err)
			return p, nil
		}
		p.mode, p.saved, p.formErr = syncStatus, true, ""
		p.fields[2].SetValue("")
		p.loading = true
		return p, p.load()
	case syncSpinMsg:
		if msg.run == p.run && p.mode == syncRunning {
			p.frame = (p.frame + 1) % len(spinnerFrames)
			return p, p.spin()
		}
		return p, nil
	case syncDoneMsg:
		if msg.run != p.run {
			return p, nil
		}
		p.cancel = nil
		if msg.err != nil || msg.outcome.Status != "synced" {
			p.mode, p.failure, p.failErr = syncFailed, msg.outcome, msg.err
			return p, p.load()
		}
		outcome := msg.outcome
		p.mode, p.last = syncStatus, &outcome
		p.loading = true
		return p, p.load()
	case tea.KeyPressMsg:
		return p.updateKeys(msg)
	}
	if p.mode == syncConfigure {
		var cmd tea.Cmd
		p.fields[p.field], cmd = p.fields[p.field].Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p *syncPage) updateKeys(msg tea.KeyPressMsg) (page, tea.Cmd) {
	text := msg.String()
	switch p.mode {
	case syncConfigure:
		switch text {
		case "esc":
			p.mode, p.formErr = syncStatus, ""
			p.fields[2].SetValue("")
			return p, nil
		case "tab", "down":
			return p, p.focus((p.field + 1) % len(p.fields))
		case "shift+tab", "up":
			return p, p.focus((p.field + len(p.fields) - 1) % len(p.fields))
		case "enter":
			if p.field < len(p.fields)-1 {
				return p, p.focus(p.field + 1)
			}
			return p, p.save()
		}
		var cmd tea.Cmd
		p.fields[p.field], cmd = p.fields[p.field].Update(msg)
		return p, cmd
	case syncRunning:
		if text == "ctrl+c" && p.cancel != nil {
			p.cancel()
		}
		return p, nil
	case syncFailed:
		switch text {
		case "esc":
			p.mode = syncStatus
		case "r":
			return p, p.sync()
		case "c":
			p.copied = "Detalle copiado al portapapeles."
			return p, tea.SetClipboard(p.failureReport())
		}
		return p, nil
	}
	switch text {
	case "esc":
		return p, navigate(routeHome)
	case "s":
		if p.ready() {
			return p, p.sync()
		}
	case "c":
		return p, p.openForm()
	case "p":
		if p.data.Configured && p.data.PortableID == "" {
			p.copied = "Comando copiado al portapapeles."
			return p, tea.SetClipboard(p.initCommand())
		}
	case "r":
		p.loading = true
		return p, p.load()
	case "left", "right", "tab":
		p.selected = 1 - p.selected
	case "enter":
		if p.selected == 0 && p.ready() {
			return p, p.sync()
		}
		return p, p.openForm()
	}
	return p, nil
}

func (p *syncPage) openForm() tea.Cmd {
	p.mode, p.formErr, p.saved = syncConfigure, "", false
	p.fields[0].SetValue(p.data.Endpoint)
	p.fields[1].SetValue(p.data.DeviceID)
	p.fields[2].SetValue("")
	start := 0
	if p.data.Endpoint != "" && p.data.DeviceID != "" {
		start = 2
	}
	return p.focus(start)
}

func (p *syncPage) focus(index int) tea.Cmd {
	for i := range p.fields {
		p.fields[i].Blur()
	}
	p.field = index
	return p.fields[index].Focus()
}

func (p *syncPage) save() tea.Cmd {
	endpoint, device, bearer := strings.TrimSpace(p.fields[0].Value()), strings.TrimSpace(p.fields[1].Value()), strings.TrimSpace(p.fields[2].Value())
	switch {
	case !strings.HasPrefix(endpoint, "https://"):
		p.formErr = "El servidor debe ser una URL https://."
		return p.focus(0)
	case device == "":
		p.formErr = "Falta el ID del dispositivo."
		return p.focus(1)
	case bearer == "":
		p.formErr = "Falta el bearer."
		return p.focus(2)
	}
	env := p.env
	return func() tea.Msg {
		return syncConfiguredMsg{err: env.backend.ConfigureSync(env.ctx, endpoint, device, bearer)}
	}
}

func configureError(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "invalid"):
		return "Revisa la URL (https) y que el ID del dispositivo sea un UUID."
	case strings.Contains(message, "unsupported"), strings.Contains(message, "keyring"), strings.Contains(message, "credential"):
		return "No se pudo guardar el bearer en el keyring del sistema."
	default:
		return "No se pudo guardar la configuración."
	}
}

func (p *syncPage) sync() tea.Cmd {
	ctx, cancel := context.WithCancel(p.env.ctx)
	p.run++
	p.mode, p.cancel, p.started, p.copied, p.frame = syncRunning, cancel, p.now(), "", 0
	backend, run := p.env.backend, p.run
	return tea.Batch(func() tea.Msg {
		outcome, err := backend.SyncNow(ctx)
		if ctx.Err() != nil && err == nil {
			err = ctx.Err()
		}
		return syncDoneMsg{run: run, outcome: outcome, err: err}
	}, p.spin())
}

func (p *syncPage) spin() tea.Cmd {
	run := p.run
	return tea.Tick(spinnerTick, func(time.Time) tea.Msg { return syncSpinMsg{run: run} })
}

func (p *syncPage) Section() string { return "sync entre equipos" }
func (p *syncPage) Captures() bool  { return p.mode == syncConfigure }
func (p *syncPage) Busy() bool      { return p.mode == syncRunning }
func (p *syncPage) Modal(int) []string {
	return nil
}

func (p *syncPage) Keys() []key.Binding {
	esc := binding([]string{"esc"}, "Esc", "volver")
	switch p.mode {
	case syncConfigure:
		return []key.Binding{binding([]string{"tab"}, "Tab", "siguiente campo"), binding([]string{"enter"}, "Enter", "guardar"), binding([]string{"esc"}, "Esc", "cancelar")}
	case syncRunning:
		return []key.Binding{binding([]string{"ctrl+c"}, "Ctrl+C", "cancelar")}
	case syncFailed:
		return []key.Binding{binding([]string{"r"}, "r", "reintentar"), binding([]string{"c"}, "c", "copiar detalle"), esc}
	}
	if !p.data.Configured {
		return []key.Binding{binding([]string{"c"}, "c", "configurar"), esc}
	}
	keys := []key.Binding{}
	if p.ready() {
		keys = append(keys, binding([]string{"s"}, "s", "sincronizar"))
	}
	keys = append(keys, binding([]string{"c"}, "c", "configurar"))
	if p.data.PortableID == "" {
		keys = append(keys, binding([]string{"p"}, "p", "copiar comando"))
	}
	return append(keys, binding([]string{"r"}, "r", "actualizar"), esc)
}

func (p *syncPage) Body(width, height int) []string {
	switch p.mode {
	case syncConfigure:
		return panel(width, height, true, sectionLabel("Sync · configurar"), styleMuted.Render(ansi.Truncate(p.data.Endpoint, 40, "…")), p.formLines(width-4))
	case syncRunning:
		return panel(width, height, true, sectionLabel("Sincronizando"), styleMuted.Render("proyecto "+shortID(p.data.PortableID)), p.runningLines(width-4))
	case syncFailed:
		title, _, _ := syncStatusText(p.failure.Status, p.failErr)
		return panel(width, height, true, outcome(stateError, title), "", p.failureLines(width-4))
	}
	actions := panel(width, 9, false, sectionLabel("Acciones"), "", p.actionLines(width-4))
	top := max(height-len(actions)-1, 6)
	aside := styleMuted.Render(ansi.Truncate(p.data.Endpoint, 48, "…"))
	main := panel(width, top, false, sectionLabel("Sync"), aside, p.statusLines(width-4))
	if width >= stackBelow {
		mainWidth := width - sidePanel - 1
		main = joinColumns(1,
			panel(mainWidth, top, false, sectionLabel("Sync"), aside, p.statusLines(mainWidth-4)),
			panel(sidePanel, top, false, sectionLabel("Cómo funciona"), "", p.howLines(sidePanel-4)),
		)
	}
	return append(append(main, ""), actions...)
}

func (p *syncPage) statusLines(width int) []string {
	if p.loading && !p.data.Configured && p.err == nil {
		return []string{stateBusy.glyph() + " " + styleMuted.Render("Leyendo el perfil de sync…")}
	}
	if p.err != nil {
		return []string{stateError.glyph() + " " + styleStrong.Render("No se pudo leer el perfil de sync."), styleMuted.Render("Acción: abre el diagnóstico desde Inicio con [4].")}
	}
	if !p.data.Configured {
		lines := []string{stateInfo.glyph() + " " + styleStrong.Render("Sync no está configurado en este equipo."), ""}
		lines = append(lines, styledWrap("Es opcional: respalda la memoria en tu propio servidor vgxness-syncd y la comparte con tus otros equipos. Necesitas la URL HTTPS del servidor, el ID de este dispositivo y el bearer que emite su panel (vgxness-syncd admin).", width, styleMuted)...)
		return append(lines, "", styleMuted.Render("Configúralo con ")+styleAccent.Render("[c]")+styleMuted.Render("."))
	}
	data := p.data
	rows := []checkRow{}
	switch {
	case !data.Enabled:
		rows = append(rows, checkRow{state: stateWarn, name: "perfil", value: "pausado", detail: "el perfil existe pero está deshabilitado"})
	case data.Credential != "available":
		rows = append(rows, checkRow{state: stateError, name: "credencial", value: credentialWord(data.Credential), detail: "keyring del sistema", action: "vuelve a configurar con [c]"})
	default:
		rows = append(rows, checkRow{state: stateOK, name: "credencial", value: "disponible", detail: "keyring del sistema"})
	}
	rows = append(rows, checkRow{state: stateNeutral, name: "dispositivo", value: shortID(data.DeviceID), detail: sanitizeTerminal(data.DeviceID)})
	if data.PortableID == "" {
		rows = append(rows, checkRow{state: stateWarn, name: "proyecto", value: "sin vincular", detail: "falta .vgxness/project-id en el workspace", action: p.initCommand()})
	} else {
		rows = append(rows, checkRow{state: stateOK, name: "proyecto", value: "vinculado", detail: "id portable " + shortID(data.PortableID)})
	}
	rows = append(rows, checkRow{state: stateNeutral, name: "pendientes", value: thousands(data.Pending), detail: "cambios por enviar en este equipo"})
	if data.LastPull.IsZero() {
		rows = append(rows, checkRow{state: stateNeutral, name: "recepción", value: "nunca", detail: "este proyecto aún no recibe historia"})
	} else {
		rows = append(rows, checkRow{state: stateOK, name: "recepción", value: ago(p.now(), data.LastPull), detail: "última vez que llegó historia del servidor"})
	}
	lines := renderChecks(rows, 13, 14, width)
	if p.last != nil {
		lines = append(lines, "", sectionLabel("Última sincronización"), stateOK.glyph()+" "+styleInk.Render(fmt.Sprintf("%s · %s · %s · %.1f s",
			plural(p.last.Pushed, "enviada", "enviadas"), plural(p.last.Rejected, "rechazada", "rechazadas"), plural(p.last.Conflicts, "conflicto", "conflictos"), p.last.Took.Seconds())))
	}
	var notices []string
	if p.saved {
		notices = append(notices, stateOK.glyph()+" "+styleMuted.Render("Configuración guardada; el bearer quedó en el keyring."))
	}
	if p.copied != "" {
		notices = append(notices, stateOK.glyph()+" "+styleMuted.Render(p.copied))
	}
	if len(notices) > 0 {
		lines = append(append(notices, ""), lines...)
	}
	return lines
}

func (p *syncPage) howLines(width int) []string {
	lines := styledWrap("Sync es por proyecto y en primer plano: envía y recibe solo la historia de este workspace, nunca ejecuta git pull.", width, styleInk)
	lines = append(lines, "", styleMuted.Render("Primer equipo (nube vacía):"))
	lines = append(lines, styledWrap("vgxness memory sync reseed --workspace <ruta> --confirm-cloud-empty", width, styleAccent)...)
	lines = append(lines, "", styleMuted.Render("Cada equipo siguiente:"))
	lines = append(lines, styledWrap("vgxness memory sync rejoin --workspace <ruta> --confirm-merge", width, styleAccent)...)
	lines = append(lines, "")
	return append(lines, styledWrap("Los dispositivos se emiten y revocan en el panel del servidor (vgxness-syncd admin).", width, styleMuted)...)
}

func (p *syncPage) actionLines(width int) []string {
	cards := []card{{key: "s", title: "Sincronizar ahora", detail: "enviar y recibir"}, {key: "c", title: "Configurar", detail: "servidor, dispositivo, bearer"}}
	if !p.ready() {
		cards[0].detail = "no disponible aún"
	}
	if !p.data.Configured {
		cards = cards[1:]
	}
	selected := p.selected
	if !p.data.Configured {
		selected = 0
	}
	return append(renderCards(cards, selected, width), styleMuted.Render("elige con la tecla o ")+styleAccent.Render("[Enter]"))
}

func (p *syncPage) formLines(width int) []string {
	labels := [3]string{"Servidor (HTTPS)", "ID del dispositivo", "Bearer"}
	var lines []string
	for index := range p.fields {
		p.fields[index].SetWidth(max(width-4, 10))
		label := styleMuted.Render(labels[index])
		if index == p.field {
			label = styleLabel.Render(labels[index])
		}
		lines = append(lines, label, p.fields[index].View(), "")
	}
	lines = append(lines, styledWrap("Pega el bearer que emitió el panel de vgxness-syncd (vgxness-syncd admin). Se guarda en el keyring del sistema; nunca en la base ni en archivos, y no se muestra.", width, styleMuted)...)
	if p.formErr != "" {
		lines = append(lines, "", stateError.glyph()+" "+styleStrong.Render(p.formErr))
	}
	return lines
}

func (p *syncPage) runningLines(width int) []string {
	glyph := styleAccent.Render(spinnerFrames[p.frame])
	lines := []string{
		glyph + " " + styleInk.Render("Enviando y recibiendo los cambios de este proyecto…") + styleMuted.Render(fmt.Sprintf("  %d s", int(p.now().Sub(p.started).Seconds()))),
		"",
		styleMuted.Render("servidor     ") + styleInk.Render(ansi.Truncate(p.data.Endpoint, width-13, "…")),
		styleMuted.Render("pendientes   ") + styleInk.Render(thousands(p.data.Pending)+" cambios en este equipo"),
		"",
	}
	return append(lines, styledWrap("Solo [Ctrl+C] cancela; lo ya enviado queda en el servidor y se retoma después.", width, styleMuted)...)
}

// syncStatusText explains a sync outcome: title, explanation and action.
func syncStatusText(status string, err error) (string, string, string) {
	if errors.Is(err, context.Canceled) {
		return "Sincronización cancelada", "La cancelaste antes de terminar.", "Lo ya enviado queda en el servidor; reintenta con [r]."
	}
	switch status {
	case "unreachable":
		return "Sin conexión con el servidor", "El servidor no respondió.", "Revisa tu conexión y la URL del servidor, y reintenta con [r]."
	case "unauthorized", "credential_missing", "credential_unavailable":
		return "Credencial rechazada", "El servidor no aceptó el bearer de este dispositivo, o no está en el keyring.", "Emite un bearer nuevo en vgxness-syncd admin y configúralo con [c] desde Sync."
	case "incompatible":
		return "Versión incompatible", "El servidor y este binario no hablan la misma versión del protocolo.", "Actualiza vgxness y vgxness-syncd a la misma versión."
	case "conflict":
		return "Hay conflictos", "Algunos cambios chocaron con los de otro equipo.", "Revisa los conflictos con vgxness memory sync status y reintenta."
	case "partial", "rejected":
		return "Sincronización parcial", "El servidor rechazó una parte de los cambios.", "Reintenta con [r]; si se repite, copia el detalle con [c]."
	case "disabled", "absent":
		return "Sync no disponible", "El perfil de sync está deshabilitado o no existe.", "Configúralo con [c] desde Sync."
	default:
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "project") {
			return "Proyecto sin vincular", "Este workspace no tiene identidad portable, y sync es por proyecto.", "Ejecuta  vgxness memory project init --workspace <ruta>  y reintenta."
		}
		return "No se pudo sincronizar", "La sincronización terminó con error.", "Reintenta con [r]; si se repite, copia el detalle con [c]."
	}
}

func (p *syncPage) failureLines(width int) []string {
	_, explanation, action := syncStatusText(p.failure.Status, p.failErr)
	lines := styledWrap(explanation, width, styleInk)
	lines = append(lines, styledWrap(fmt.Sprintf("Tus memorias están a salvo en ~/.vgxness/memory.db; %s quedan pendientes de enviar.", plural(p.data.Pending, "cambio", "cambios")), width, styleMuted)...)
	lines = append(lines, "")
	lines = append(lines, styledWrap("Acción: "+action, width, styleMuted)...)
	lines = append(lines, "", sectionLabel("Detalle"))
	for _, line := range strings.Split(p.failureReport(), "\n") {
		lines = append(lines, styleMuted.Render(ansi.Truncate(line, width, "…")))
	}
	if p.copied != "" {
		lines = append(lines, "", stateOK.glyph()+" "+styleMuted.Render(p.copied))
	}
	return lines
}

func (p *syncPage) failureReport() string {
	outcome := p.failure
	parts := []string{"estado " + valueOr(outcome.Status, "desconocido")}
	if outcome.FailureOperation != "" {
		parts = append(parts, "operación "+outcome.FailureOperation)
	}
	if outcome.FailureClass != "" {
		parts = append(parts, "clase "+outcome.FailureClass)
	}
	if outcome.HTTPStatus != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", outcome.HTTPStatus))
	}
	lines := []string{strings.Join(parts, " · ")}
	if p.failErr != nil {
		lines = append(lines, "error "+sanitizeTerminal(p.failErr.Error()))
	}
	lines = append(lines, fmt.Sprintf("enviadas %d · reintentos %d · rechazadas %d · conflictos %d", outcome.Pushed, outcome.Retried, outcome.Rejected, outcome.Conflicts))
	return strings.Join(lines, "\n")
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" {
		return "—"
	}
	return id
}
