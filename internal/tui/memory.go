package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Memoria: artboards tui-memoria-buscar, -buscar-vacio, -primer-uso,
// -detalle, -olvidar-confirmar and -handoffs.

type memoryMode uint8

const (
	modeList memoryMode = iota
	modeDetail
	modeHandoffs
)

const handoffLimit = 30

// searchDelay debounces typing in the search box.
var searchDelay = 200 * time.Millisecond

type memorySearchMsg struct {
	seq     int
	results MemoryResults
	err     error
}

type searchTickMsg struct{ seq int }

type memoryDetailMsg struct {
	item MemoryItem
	err  error
}

type memoryForgotMsg struct {
	id, title string
	err       error
}

type handoffsMsg struct {
	items []Handoff
	err   error
}

type memoryPage struct {
	env  environment
	mode memoryMode
	// direct is true when Inicio opened the handoffs view; Esc then returns
	// to Inicio instead of the search.
	direct bool

	input     textinput.Model
	seq       int
	loading   bool
	results   MemoryResults
	err       error
	typeIndex int
	cursor    int
	notice    string

	detail     MemoryItem
	detailErr  error
	scroll     int
	confirming bool
	forgetErr  error

	handoffs      []Handoff
	handoffsErr   error
	handoffCursor int
}

func newMemoryPage(env environment, handoffs bool) *memoryPage {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "buscar en la memoria"
	input.CharLimit = 120
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Prompt, styles.Blurred.Prompt = styleAccent, styleMuted
	styles.Focused.Text, styles.Blurred.Text = styleInk, styleMuted
	styles.Focused.Placeholder, styles.Blurred.Placeholder = styleFaint, styleFaint
	// A steady cursor in accent: blinking redraws the frame twice a second
	// for no information.
	styles.Cursor.Color, styles.Cursor.Blink = colorAccent, false
	input.SetStyles(styles)
	page := &memoryPage{env: env, input: input, loading: true}
	if handoffs {
		page.mode, page.direct = modeHandoffs, true
	}
	return page
}

func (p *memoryPage) Init() tea.Cmd {
	if p.mode == modeHandoffs {
		return p.loadHandoffs()
	}
	return tea.Batch(p.input.Focus(), p.search())
}

func (p *memoryPage) query() MemoryQuery {
	return MemoryQuery{Text: strings.TrimSpace(p.input.Value()), Type: p.typeFilter()}
}

func (p *memoryPage) typeFilter() string {
	if p.typeIndex == 0 || p.typeIndex > len(p.results.Types) {
		return ""
	}
	return p.results.Types[p.typeIndex-1].Type
}

func (p *memoryPage) search() tea.Cmd {
	p.seq++
	seq, env, query := p.seq, p.env, p.query()
	return func() tea.Msg {
		results, err := env.backend.SearchMemories(env.ctx, query)
		return memorySearchMsg{seq: seq, results: results, err: err}
	}
}

// scheduleSearch debounces typing: only the last keystroke's tick searches.
func (p *memoryPage) scheduleSearch() tea.Cmd {
	p.seq++
	seq := p.seq
	return tea.Tick(searchDelay, func(time.Time) tea.Msg { return searchTickMsg{seq: seq} })
}

func (p *memoryPage) open(id string) tea.Cmd {
	env := p.env
	return func() tea.Msg {
		item, err := env.backend.GetMemory(env.ctx, id)
		return memoryDetailMsg{item: item, err: err}
	}
}

func (p *memoryPage) forget(item MemoryItem) tea.Cmd {
	env := p.env
	return func() tea.Msg {
		return memoryForgotMsg{id: item.ID, title: item.Title, err: env.backend.ForgetMemory(env.ctx, item.ID)}
	}
}

func (p *memoryPage) loadHandoffs() tea.Cmd {
	env := p.env
	return func() tea.Msg {
		items, err := env.backend.Handoffs(env.ctx, handoffLimit)
		return handoffsMsg{items: items, err: err}
	}
}

func (p *memoryPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case memorySearchMsg:
		if msg.seq != p.seq {
			return p, nil
		}
		p.loading, p.results, p.err = false, msg.results, msg.err
		p.cursor = min(p.cursor, max(len(p.results.Items)-1, 0))
		return p, nil
	case searchTickMsg:
		if msg.seq != p.seq {
			return p, nil
		}
		return p, p.search()
	case memoryDetailMsg:
		p.detail, p.detailErr, p.scroll = msg.item, msg.err, 0
		return p, nil
	case memoryForgotMsg:
		p.confirming = false
		if msg.err != nil {
			p.forgetErr = msg.err
			return p, nil
		}
		p.mode, p.notice, p.forgetErr = modeList, "Olvidada «"+msg.title+"».", nil
		return p, p.search()
	case handoffsMsg:
		p.handoffs, p.handoffsErr = msg.items, msg.err
		p.handoffCursor = min(p.handoffCursor, max(len(p.handoffs)-1, 0))
		return p, nil
	case tea.KeyPressMsg:
		switch p.mode {
		case modeDetail:
			return p.updateDetail(msg)
		case modeHandoffs:
			return p.updateHandoffs(msg)
		default:
			return p.updateList(msg)
		}
	}
	if p.mode == modeList && p.input.Focused() {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p *memoryPage) updateList(msg tea.KeyPressMsg) (page, tea.Cmd) {
	items := p.results.Items
	switch msg.String() {
	case "esc":
		if p.input.Focused() {
			p.input.Blur()
			return p, nil
		}
		return p, navigate(routeHome)
	case "up":
		p.cursor = max(p.cursor-1, 0)
		return p, nil
	case "down":
		p.cursor = min(p.cursor+1, max(len(items)-1, 0))
		return p, nil
	case "pgdown":
		p.cursor = min(p.cursor+10, max(len(items)-1, 0))
		return p, nil
	case "pgup":
		p.cursor = max(p.cursor-10, 0)
		return p, nil
	case "tab":
		p.typeIndex = (p.typeIndex + 1) % (len(p.results.Types) + 1)
		p.cursor, p.notice = 0, ""
		return p, p.search()
	case "shift+tab":
		p.typeIndex = (p.typeIndex + len(p.results.Types)) % (len(p.results.Types) + 1)
		p.cursor, p.notice = 0, ""
		return p, p.search()
	case "enter":
		if p.cursor < len(items) {
			p.mode, p.detail, p.detailErr, p.confirming = modeDetail, items[p.cursor], nil, false
			return p, p.open(items[p.cursor].ID)
		}
		return p, nil
	}
	if p.input.Focused() {
		before := p.input.Value()
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		if p.input.Value() != before {
			p.cursor, p.notice = 0, ""
			return p, tea.Batch(cmd, p.scheduleSearch())
		}
		return p, cmd
	}
	switch msg.String() {
	case "/":
		return p, p.input.Focus()
	case "j":
		p.cursor = min(p.cursor+1, max(len(items)-1, 0))
	case "k":
		p.cursor = max(p.cursor-1, 0)
	case "h":
		p.mode = modeHandoffs
		return p, p.loadHandoffs()
	case "r":
		p.input.SetValue("")
		p.typeIndex, p.cursor, p.notice = 0, 0, ""
		return p, p.search()
	}
	return p, nil
}

func (p *memoryPage) updateDetail(msg tea.KeyPressMsg) (page, tea.Cmd) {
	if p.confirming {
		switch msg.String() {
		case "y":
			return p, p.forget(p.detail)
		case "n", "esc":
			p.confirming = false
		}
		return p, nil
	}
	switch msg.String() {
	case "esc":
		p.mode, p.forgetErr = modeList, nil
	case "o":
		if p.detailErr == nil && p.detail.ID != "" {
			p.confirming, p.forgetErr = true, nil
		}
	case "c":
		if p.detailErr == nil {
			return p, tea.SetClipboard(p.detail.Content)
		}
	case "up", "k":
		p.scroll = max(p.scroll-1, 0)
	case "down", "j":
		p.scroll++
	}
	return p, nil
}

func (p *memoryPage) updateHandoffs(msg tea.KeyPressMsg) (page, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if p.direct {
			return p, navigate(routeHome)
		}
		p.mode = modeList
	case "up", "k":
		p.handoffCursor = max(p.handoffCursor-1, 0)
	case "down", "j":
		p.handoffCursor = min(p.handoffCursor+1, max(len(p.handoffs)-1, 0))
	case "c":
		if p.handoffCursor < len(p.handoffs) {
			return p, tea.SetClipboard(p.handoffs[p.handoffCursor].Summary)
		}
	}
	return p, nil
}

func (p *memoryPage) Section() string { return "memoria del proyecto" }

func (p *memoryPage) Captures() bool { return p.mode == modeList && p.input.Focused() }
func (p *memoryPage) Busy() bool     { return false }

func (p *memoryPage) Keys() []key.Binding {
	esc := binding([]string{"esc"}, "Esc", "volver")
	switch p.mode {
	case modeDetail:
		return []key.Binding{binding([]string{"o"}, "o", "olvidar"), binding([]string{"c"}, "c", "copiar"), binding([]string{"up", "down"}, "↑↓", "desplazar"), esc}
	case modeHandoffs:
		return []key.Binding{binding([]string{"up", "down"}, "↑↓", "mover"), binding([]string{"c"}, "c", "copiar"), esc}
	}
	if p.results.Total == 0 && !p.loading && p.err == nil {
		return []key.Binding{esc}
	}
	if p.input.Focused() {
		return []key.Binding{binding([]string{"up", "down"}, "↑↓", "mover"), binding([]string{"enter"}, "Enter", "abrir"), binding([]string{"tab"}, "Tab", "tipo"), binding([]string{"esc"}, "Esc", "dejar de escribir")}
	}
	return []key.Binding{binding([]string{"/"}, "/", "buscar"), binding([]string{"up", "down"}, "↑↓", "mover"), binding([]string{"enter"}, "Enter", "abrir"), binding([]string{"tab"}, "Tab", "tipo"), binding([]string{"h"}, "h", "handoffs"), esc}
}

func (p *memoryPage) Modal(width int) []string {
	if !p.confirming || p.mode != modeDetail {
		return nil
	}
	dialogWidth := min(66, max(width-8, 40))
	inner := dialogWidth - 4
	lines := []string{outcome(stateError, "Olvidar memoria"), ""}
	lines = append(lines, styledWrap("¿Olvidar «"+sanitizeTerminal(p.detail.Title)+"»?", inner, styleStrong)...)
	lines = append(lines, styledWrap("Se archiva y sale de la búsqueda; si usas sync, el cambio viaja en la próxima sincronización. No se puede deshacer desde la consola.", inner, styleMuted)...)
	lines = append(lines, "", keyHint("y", "olvidar")+styleMuted.Render(" · ")+keyHint("n", "cancelar"))
	return box(dialogWidth, len(lines)+2, colorError, colorPanel, lines)
}

func (p *memoryPage) Body(width, height int) []string {
	switch p.mode {
	case modeDetail:
		return p.detailBody(width, height)
	case modeHandoffs:
		return p.handoffsBody(width, height)
	default:
		return p.listBody(width, height)
	}
}

func (p *memoryPage) count() string {
	if p.loading && p.results.Total == 0 {
		return styleMuted.Render("◐ leyendo…")
	}
	return styleMuted.Render(plural(p.results.Total, "memoria", "memorias"))
}

func (p *memoryPage) listBody(width, height int) []string {
	if p.err != nil {
		return panel(width, height, false, sectionLabel("Memoria"), "", []string{
			stateError.glyph() + " " + styleStrong.Render("No se pudo leer la memoria."),
			styleMuted.Render("Acción: abre el diagnóstico desde Inicio con ") + styleAccent.Render("[4]"),
		})
	}
	if !p.loading && p.results.Total == 0 {
		return panel(width, height, false, sectionLabel("Memoria"), p.count(), p.firstUse(width-4, height-4))
	}
	p.input.SetWidth(max(width-sidePanel-12, 10))
	filter := "todos"
	if t := p.typeFilter(); t != "" {
		filter = typeLabel(t)
	}
	head := []string{
		p.input.View(),
		styleMuted.Render(plural(len(p.results.Items), "resultado", "resultados") + " · tipo: " + filter + " ▾"),
		"",
	}
	if len(p.results.Items) == 0 && !p.loading {
		message := "Sin resultados"
		if text := strings.TrimSpace(p.input.Value()); text != "" {
			message += " para «" + sanitizeTerminal(text) + "»"
		}
		if t := p.typeFilter(); t != "" {
			message += " en " + typeLabel(t)
		}
		hint := []string{stateInfo.glyph() + " " + styleStrong.Render(message+"."), styleMuted.Render("Prueba otro término o cambia el tipo con ") + styleAccent.Render("[Tab]") + styleMuted.Render(".")}
		if !p.input.Focused() {
			hint = append(hint, styleMuted.Render("También puedes quitar los filtros con ")+styleAccent.Render("[r]")+styleMuted.Render("."))
		}
		body := append(head, placeCentered(width-4, height-4-len(head), hint)...)
		return panel(width, height, true, sectionLabel("Memoria"), p.count(), body)
	}
	main := width
	if width >= stackBelow {
		main = width - sidePanel - 1
	}
	table := p.table(main-4, height-4-len(head))
	if p.notice != "" {
		head[2] = stateOK.glyph() + " " + styleMuted.Render(p.notice)
	}
	list := panel(main, height, true, sectionLabel("Memoria"), p.count(), append(head, table...))
	if width < stackBelow {
		return list
	}
	return joinColumns(1, list, panel(sidePanel, height, false, sectionLabel("Vista previa"), "", p.preview(sidePanel-4)))
}

func (p *memoryPage) firstUse(width, height int) []string {
	lines := []string{stateInfo.glyph() + " " + styleStrong.Render("Aún no hay memorias en este proyecto."), ""}
	lines = append(lines, styledWrap("Claude Code las guarda mientras trabaja contigo: decisiones, notas y el handoff de cada sesión. También puedes pedírselo directamente:", min(width, 70), styleMuted)...)
	lines = append(lines, "", styleAccent.Render("«guarda en la memoria de VGXNESS que usamos pnpm»"))
	return placeCentered(width, height, lines)
}

func (p *memoryPage) table(width, height int) []string {
	dateWidth, typeWidth, sourceWidth := 12, 14, 13
	topicWidth := max(width-2-dateWidth-typeWidth-sourceWidth, 10)
	lines := []string{styleMuted.Render("  " + padRight("FECHA", dateWidth) + padRight("TIPO", typeWidth) + padRight("TEMA", topicWidth) + "FUENTE")}
	rows := max(height-2, 1)
	offset := 0
	if p.cursor >= rows {
		offset = p.cursor - rows + 1
	}
	items := p.results.Items
	for index := offset; index < len(items) && index < offset+rows; index++ {
		item := items[index]
		cells := padRight(item.Updated.Local().Format("2006-01-02"), dateWidth) +
			padRight(ansi.Truncate(typeLabel(item.Type), typeWidth-2, "…"), typeWidth) +
			padRight(ansi.Truncate(sanitizeTerminal(titleOf(item)), topicWidth-2, "…"), topicWidth) +
			ansi.Truncate(sourceLabel(item.Producer), sourceWidth, "…")
		if index == p.cursor {
			lines = append(lines, styleFocus.Render(padRight("▸ "+cells, width)))
		} else {
			lines = append(lines, styleInk.Render("  "+cells))
		}
	}
	if remaining := len(items) - offset - rows; remaining > 0 {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("  … %d más · ", remaining))+styleAccent.Render("[PgDn]")+styleMuted.Render(" ver"))
	}
	return lines
}

func (p *memoryPage) preview(width int) []string {
	if p.cursor >= len(p.results.Items) {
		return nil
	}
	item := p.results.Items[p.cursor]
	lines := styledWrap(sanitizeTerminal(titleOf(item)), width, styleStrong)
	lines = append(lines, styleMuted.Render(typeLabel(item.Type)+" · "+item.Updated.Local().Format("2006-01-02")+" · "+sourceLabel(item.Producer)), "")
	text := item.Preview
	if text == "" {
		text = item.Content
	}
	lines = append(lines, styledWrap(sanitizeTerminal(text), width, styleInk)...)
	lines = append(lines, "")
	if item.Topic != "" {
		lines = append(lines, styleMuted.Render("tema      ")+styleInk.Render(ansi.Truncate(sanitizeTerminal(item.Topic), width-10, "…")))
	}
	if n := len(item.References); n > 0 {
		lines = append(lines, styleMuted.Render("refs      ")+styleInk.Render(plural(n, "referencia", "referencias")))
	}
	return append(lines, "", keyHint("Enter", "abrir completo"))
}

func (p *memoryPage) detailBody(width, height int) []string {
	item := p.detail
	aside := styleMuted.Render(typeLabel(item.Type) + " · " + item.Updated.Local().Format("2006-01-02") + " · " + sourceLabel(item.Producer))
	main := width
	if width >= stackBelow {
		main = width - sidePanel - 1
	}
	inner := main - 4
	var body []string
	switch {
	case p.detailErr != nil:
		body = []string{stateError.glyph() + " " + styleStrong.Render("No se pudo abrir la memoria."), styleMuted.Render("Acción: vuelve con [Esc] y búscala de nuevo.")}
	case p.forgetErr != nil:
		body = []string{stateError.glyph() + " " + styleStrong.Render("No se pudo olvidar la memoria."), styleMuted.Render("Acción: revisa el diagnóstico desde Inicio y reintenta con [o].")}
	default:
		body = styledWrap(sanitizeTerminal(titleOf(item)), inner, styleStrong)
		content := item.Content
		if content == "" {
			content = item.Preview
		}
		text := styledWrap(sanitizeTerminal(content), inner, styleInk)
		fields := []string{""}
		if item.Topic != "" {
			fields = append(fields, styleMuted.Render("tema        ")+styleInk.Render(sanitizeTerminal(item.Topic)))
		}
		fields = append(fields,
			styleMuted.Render("creada      ")+styleInk.Render(item.Created.Local().Format("2006-01-02 15:04")),
			styleMuted.Render("actualizada ")+styleInk.Render(item.Updated.Local().Format("2006-01-02 15:04")),
			styleMuted.Render("id          ")+styleInk.Render(sanitizeTerminal(item.ID)),
		)
		room := max(height-4-len(body)-1-len(fields), 1)
		p.scroll = min(p.scroll, max(len(text)-room, 0))
		visible := text[p.scroll:min(p.scroll+room, len(text))]
		body = append(append(append(body, ""), visible...), fields...)
	}
	detail := panel(main, height, true, sectionLabel("Memoria · detalle"), aside, body)
	if width < stackBelow {
		return detail
	}
	var refs []string
	if len(item.References) == 0 {
		refs = []string{styleMuted.Render("Sin referencias guardadas.")}
	}
	for _, ref := range item.References {
		refs = append(refs, styledWrap("· "+sanitizeTerminal(ref), sidePanel-4, styleInk)...)
	}
	return joinColumns(1, detail, panel(sidePanel, height, false, sectionLabel("Referencias"), "", refs))
}

func (p *memoryPage) handoffsBody(width, height int) []string {
	listWidth := min(sidePanel, width/2)
	var list []string
	switch {
	case p.handoffsErr != nil:
		list = []string{stateError.glyph() + " " + styleStrong.Render("No se pudieron leer.")}
	case len(p.handoffs) == 0:
		list = styledWrap("Todavía no hay handoffs: Claude Code deja uno al terminar una sesión con trabajo sustancial.", listWidth-4, styleMuted)
	}
	for index, handoff := range p.handoffs {
		row := padRight(sessionLabel(handoff.Handle), 18) + padRight(shortDate(handoff.Completed), 9) + duration(handoff.Completed.Sub(handoff.Started))
		if index == p.handoffCursor {
			list = append(list, styleFocus.Render(padRight("▸ "+row, listWidth-4)))
		} else {
			list = append(list, styleInk.Render("  "+row))
		}
	}
	left := panel(listWidth, height, false, sectionLabel("Handoffs"), styleMuted.Render(fmt.Sprintf("últimos %d", len(p.handoffs))), list)
	rightWidth := width - listWidth - 1
	if p.handoffCursor >= len(p.handoffs) {
		return joinColumns(1, left, panel(rightWidth, height, true, sectionLabel("Sesión"), "", nil))
	}
	handoff := p.handoffs[p.handoffCursor]
	inner := rightWidth - 4
	body := []string{styleMuted.Render(handoff.Completed.Local().Format("2006-01-02 15:04") + " · " + duration(handoff.Completed.Sub(handoff.Started))), ""}
	body = append(body, styledWrap(sanitizeTerminal(handoff.Summary), inner, styleInk)...)
	body = append(body, "")
	body = append(body, styledWrap("Se inyecta como contexto al iniciar la próxima sesión en este proyecto, como datos, nunca como instrucciones.", inner, styleMuted)...)
	right := panel(rightWidth, height, true, sectionLabel(sessionLabel(handoff.Handle)), styleMuted.Render(fmt.Sprintf("%d de %d", p.handoffCursor+1, len(p.handoffs))), body)
	return joinColumns(1, left, right)
}

// placeCentered centers a block inside width×height.
func placeCentered(width, height int, lines []string) []string {
	block := lipgloss.Place(max(width, 1), max(height, len(lines)), lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
	return strings.Split(block, "\n")
}

func titleOf(item MemoryItem) string {
	if item.Title != "" {
		return item.Title
	}
	return firstLine(item.Preview + "\n" + item.Content)
}

var typeLabels = map[string]string{
	"decision": "decisión", "note": "nota", "summary": "handoff", "architecture": "arquitectura",
	"discovery": "hallazgo", "config": "configuración", "configuration": "configuración",
	"learning": "aprendizaje", "observation": "observación", "bugfix": "corrección",
	"task-result": "resultado", "continuity": "continuidad", "delivery": "entrega",
}

func typeLabel(t string) string {
	if label, ok := typeLabels[t]; ok {
		return label
	}
	if t == "" {
		return "sin tipo"
	}
	return sanitizeTerminal(t)
}

func sourceLabel(producer string) string {
	switch producer {
	case "mcp":
		return "Claude Code"
	case "provider-session":
		return "sesión"
	case "cli":
		return "CLI"
	case "console":
		return "consola"
	case "":
		return "—"
	default:
		return sanitizeTerminal(producer)
	}
}
