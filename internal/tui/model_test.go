package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var testNow = time.Date(2026, 10, 2, 9, 14, 0, 0, time.Local)

type fakeBackend struct {
	overview  Overview
	diagnosis Diagnosis
	err       error
	memory    *fakeMemory
}

// fakeMemory is a mutable memory store shared by the copies of fakeBackend
// the console holds.
type fakeMemory struct {
	items    []MemoryItem
	types    []TypeCount
	handoffs []Handoff
	queries  []MemoryQuery
	forgot   []string
	err      error
}

func (f fakeBackend) SearchMemories(_ context.Context, query MemoryQuery) (MemoryResults, error) {
	if f.memory == nil {
		return MemoryResults{}, nil
	}
	f.memory.queries = append(f.memory.queries, query)
	var items []MemoryItem
	for _, item := range f.memory.items {
		text := strings.ToLower(query.Text)
		if (query.Type == "" || item.Type == query.Type) && (text == "" || strings.Contains(strings.ToLower(item.Title+" "+item.Content), text)) {
			items = append(items, item)
		}
	}
	return MemoryResults{Items: items, Total: len(f.memory.items), Types: f.memory.types}, f.memory.err
}

func (f fakeBackend) GetMemory(_ context.Context, id string) (MemoryItem, error) {
	for _, item := range f.memory.items {
		if item.ID == id {
			return item, nil
		}
	}
	return MemoryItem{}, errors.New("not found")
}

func (f fakeBackend) ForgetMemory(_ context.Context, id string) error {
	f.memory.forgot = append(f.memory.forgot, id)
	kept := f.memory.items[:0]
	for _, item := range f.memory.items {
		if item.ID != id {
			kept = append(kept, item)
		}
	}
	f.memory.items = kept
	return nil
}

func (f fakeBackend) Handoffs(context.Context, int) ([]Handoff, error) {
	if f.memory == nil {
		return nil, nil
	}
	return f.memory.handoffs, nil
}

func (f fakeBackend) Overview(context.Context) (Overview, error) { return f.overview, f.err }
func (f fakeBackend) Diagnose(context.Context) (Diagnosis, error) {
	if f.diagnosis.At.IsZero() {
		f.diagnosis.Overview, f.diagnosis.At = f.overview, testNow
	}
	return f.diagnosis, f.err
}

func healthyOverview() Overview {
	return Overview{
		Workspace: "/work/vgxness",
		Binary:    Binary{Version: "v0.9.0", Path: "/usr/local/bin/vgxness"},
		Claude:    Claude{Version: "2.1.287", Minimum: "2.1.284"},
		Plugin:    Plugin{Installed: true, Enabled: true, MarketplaceAdded: true, Version: "0.1.0", Offered: "0.1.0", Agents: 4, InstallPath: "/cache/vgxness"},
		Storage:   Storage{Root: "/home/.vgxness/projects/x", Database: "/home/.vgxness/memory.db", Exists: true, Schema: 23, Expected: 23, Memories: 184},
		Sync:      SyncState{Configured: true, Enabled: true, Credential: "available"},
		Handoffs: []Handoff{
			{Handle: "ps-706ddfe72a38", Summary: "Migración 23 aplicada y probada.\nPendiente: make verify.", Started: testNow.Add(-2*time.Hour - 42*time.Minute), Completed: testNow.Add(-2 * time.Hour)},
			{Handle: "ps-11aa22bb33cc", Summary: "Sesión anterior", Started: testNow.Add(-26 * time.Hour), Completed: testNow.Add(-25 * time.Hour)},
		},
		At: testNow,
	}
}

// drive runs the program loop synchronously: it applies msg, then executes
// returned commands (flattening batches) and feeds their messages back.
func drive(t *testing.T, m Model, msgs ...tea.Msg) (Model, []tea.Msg) {
	t.Helper()
	var produced []tea.Msg
	queue := append([]tea.Msg(nil), msgs...)
	for steps := 0; len(queue) > 0 && steps < 50; steps++ {
		msg := queue[0]
		queue = queue[1:]
		next, cmd := m.Update(msg)
		m = next.(Model)
		for _, out := range run(cmd) {
			produced = append(produced, out)
			switch out.(type) {
			case overviewMsg, diagnosisMsg, navigateMsg, memorySearchMsg, memoryDetailMsg, memoryForgotMsg, handoffsMsg:
				queue = append(queue, out)
			case searchTickMsg:
				queue = append(queue, out)
			}
		}
	}
	return m, produced
}

func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, inner := range batch {
			out = append(out, run(inner)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func start(t *testing.T, backend Backend, width, height int) Model {
	t.Helper()
	m := NewModel(context.Background(), backend, Options{Workspace: "/work/vgxness"})
	if home, ok := m.page.(*homePage); ok {
		home.now = func() time.Time { return testNow }
	}
	m, _ = drive(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	m, _ = drive(t, m, run(m.Init())...)
	return m
}

func press(text string) tea.KeyPressMsg {
	switch text {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		return tea.KeyPressMsg{Code: rune(text[0]), Text: text}
	}
}

func plain(m Model) string { return ansi.Strip(m.render()) }

func assertFits(t *testing.T, m Model) {
	t.Helper()
	lines := strings.Split(m.render(), "\n")
	if len(lines) != m.height {
		t.Fatalf("rendered %d lines, want %d", len(lines), m.height)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > m.width {
			t.Fatalf("line wider than %d (%d): %q", m.width, width, ansi.Strip(line))
		}
	}
}

func assertContains(t *testing.T, screen string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestTooSmallTerminalShowsOnlyTheResizeNotice(t *testing.T) {
	m := start(t, fakeBackend{overview: healthyOverview()}, 64, 18)
	screen := plain(m)
	assertContains(t, screen, "VGXNESS / CONSOLA", "! Se necesita más espacio", "Mínimo 80×24; la terminal actual es 64×18.", "Agranda la ventana", "[q] salir")
	if strings.Contains(screen, "ESTADO") {
		t.Fatal("TooSmall drew the console")
	}
	assertFits(t, m)
	_, out := drive(t, m, press("q"))
	if len(out) != 1 || out[0] != tea.Quit() {
		t.Fatalf("q did not quit from TooSmall: %v", out)
	}
}

func TestHomeHealthyMatchesTheWideArtboard(t *testing.T) {
	m := start(t, fakeBackend{overview: healthyOverview()}, 120, 35)
	screen := plain(m)
	assertContains(t, screen,
		"█", "CONSOLA VGXNESS", "memoria y estado local", "/work/vgxness",
		"ESTADO", "actualizado hace 0 s", "[r] actualizar",
		"✓ binario", "v0.9.0", "✓ Claude Code", "v2.1.287", "mínimo v2.1.284",
		"✓ plugin", "vgxness@vgxness · 4 agentes", "✓ base de datos", "schema v23", "184 memorias",
		"✓ sync", "configurado", "· handoff", "«Migración 23 aplicada y probada.»",
		"SESIONES RECIENTES", "sesión 706ddfe7 cerrada · handoff guardado",
		"ÚLTIMO HANDOFF", "sesión 706ddfe7", "42 min", "Pendiente: make verify.", "como datos, nunca como instrucciones", "[h] ver todos",
		"ACCIONES", "[1] Memoria", "[2] Setup del plugin", "[3] Sync", "[4] Diagnóstico", "elige [1-4] y [Enter]",
		"[1-4] elegir · [Enter] abrir · [r] actualizar · [h] handoffs · [q] salir",
	)
	assertFits(t, m)
}

func TestHomeWarnsAboutAnOutdatedPluginAndAMissingCredential(t *testing.T) {
	overview := healthyOverview()
	overview.Plugin.Offered = "0.2.0"
	overview.Sync.Credential = "missing"
	m := start(t, fakeBackend{overview: overview}, 120, 35)
	screen := plain(m)
	assertContains(t, screen, "! Actualiza el plugin y reinicia Claude Code", "! plugin", "v0.2.0 disponible · [2] actualizar", "✕ sync", "sin credencial", "credencial ausente", "Acción: vuelve a configurar con [3]", "actualizar a v0.2.0")
	if home := m.page.(*homePage); home.selected != 1 {
		t.Fatalf("outdated plugin should preselect Setup, got card %d", home.selected)
	}
	assertFits(t, m)
}

func TestHomeFirstUseOffersOnlyInstallAndDiagnosis(t *testing.T) {
	overview := healthyOverview()
	overview.Plugin = Plugin{}
	overview.Storage = Storage{Database: "/home/.vgxness/memory.db", Expected: 23}
	overview.Sync = SyncState{Credential: "not_configured"}
	overview.Handoffs = nil
	m := start(t, fakeBackend{overview: overview}, 120, 35)
	screen := plain(m)
	assertContains(t, screen, "✕ plugin", "no instalado", "falta marketplace uzielvgx/vgxness", "· base de datos", "sin crear", "· sync", "sin configurar", "· handoff", "ninguno",
		"QUÉ HACE VGXNESS", "Bienvenido.", "1. Instala el plugin con [1].", "[1] Instalar plugin", "[2] Diagnóstico", "[1-2] elegir")
	for _, absent := range []string{"[3] Sync", "[h] handoffs", "SESIONES RECIENTES"} {
		if strings.Contains(screen, absent) {
			t.Fatalf("first use shows %q", absent)
		}
	}
	m, out := drive(t, m, press("enter"))
	if _, ok := m.page.(*pendingPage); !ok || len(out) == 0 {
		t.Fatalf("Enter on [1] did not open Setup: page=%T out=%v", m.page, out)
	}
}

func TestHomeAtMinimumSizeDropsTheBannerAndStacksTheSidePanel(t *testing.T) {
	m := start(t, fakeBackend{overview: healthyOverview()}, 80, 24)
	screen := plain(m)
	assertContains(t, screen, "VGXNESS / CONSOLA", "memoria y estado local   │   workspace", "ESTADO", "ACCIONES", "[q] salir")
	if strings.Contains(screen, "█") || strings.Contains(screen, "ÚLTIMO HANDOFF") {
		t.Fatalf("80×24 should drop the banner and side panel:\n%s", screen)
	}
	assertFits(t, m)
}

func TestHomeReportsAnUnreadableState(t *testing.T) {
	m := start(t, fakeBackend{err: errors.New("boom")}, 120, 35)
	assertContains(t, plain(m), "✕ No se pudo leer el estado.", "Acción: abre el diagnóstico con [4]")
}

func TestNavigationOpensDoctorAndEscReturnsHome(t *testing.T) {
	m := start(t, fakeBackend{overview: healthyOverview()}, 120, 35)
	m, _ = drive(t, m, press("4"), press("enter"))
	if _, ok := m.page.(*doctorPage); !ok {
		t.Fatalf("page=%T after [4][Enter]", m.page)
	}
	assertContains(t, plain(m), "VGXNESS / CONSOLA", "diagnóstico   │   workspace", "DIAGNÓSTICO", "vgxness doctor · 2026-10-02 09:14")
	m, _ = drive(t, m, press("esc"))
	if _, ok := m.page.(*homePage); !ok {
		t.Fatalf("page=%T after Esc", m.page)
	}
	m, _ = drive(t, m, press("right"), press("right"))
	if home := m.page.(*homePage); home.selected != 2 {
		t.Fatalf("arrow navigation selected %d", home.selected)
	}
}

func TestDoctorHealthyListsEverySectionAndSummary(t *testing.T) {
	diagnosis := Diagnosis{Overview: healthyOverview(), PolicyChars: 4986, MCPInstructions: 1362, MCPTools: 8, Took: 800 * time.Millisecond, At: testNow}
	m := start(t, fakeBackend{overview: healthyOverview(), diagnosis: diagnosis}, 120, 35)
	m, _ = drive(t, m, press("4"), press("enter"))
	screen := plain(m)
	assertContains(t, screen,
		"ALMACENAMIENTO", "escribible", "✓ memory.db", "íntegra", "✓ esquema", "v23", "sin migraciones pendientes",
		"PLUGIN", "✓ vgxness", "✓ Claude Code", "✓ plugin",
		"HOOKS Y MCP", "✓ SessionStart", "política 4 986 chars (tope 10 000)", "✓ servidor MCP", "memory · 8 tools · instructions 1 362",
		"RESUMEN", "8 comprobaciones · 0.8 s", "ACCIONES SUGERIDAS", "Nada que corregir.",
		"[r] repetir · [c] copiar informe · [Esc] volver",
	)
	assertFits(t, m)
}

func TestDoctorCriticalExplainsAndSuggestsCopyableCommands(t *testing.T) {
	overview := healthyOverview()
	overview.Storage.Err = errors.New("database disk image is malformed")
	overview.Plugin.Offered = "0.2.0"
	diagnosis := Diagnosis{Overview: overview, PolicyChars: 4986, MCPInstructions: 1362, MCPTools: 8, At: testNow}
	m := start(t, fakeBackend{overview: overview, diagnosis: diagnosis}, 120, 35)
	m, _ = drive(t, m, press("4"), press("enter"))
	screen := plain(m)
	assertContains(t, screen, "✕ 2 problemas críticos. La consola no puede leer la memoria", "resolverlos.",
		"✕ memory.db", "dañada o ilegible", "Acción: no la borres; sigue", "«Actualización y migraciones»", "✕ esquema", "depende de memory.db",
		"! plugin", "v0.2.0 disponible", "Acción: claude plugin update",
		"! Actualiza el plugin", "[1] copiar comando", "[c] copiar informe · [1] copiar comando", "✕ Repara la base de datos")
	m, out := drive(t, m, press("1"))
	if len(out) != 1 || !strings.Contains(plain(m), "Comando copiado al portapapeles.") {
		t.Fatalf("[1] did not copy: out=%v", out)
	}
	_, out = drive(t, m, press("c"))
	if len(out) != 1 {
		t.Fatalf("[c] did not copy the report: %v", out)
	}
}

func TestQuitKeysAndFocusDimming(t *testing.T) {
	m := start(t, fakeBackend{overview: healthyOverview()}, 120, 35)
	for _, text := range []string{"q", "ctrl+c"} {
		_, out := drive(t, m, press(text))
		if len(out) != 1 || out[0] != tea.Quit() {
			t.Fatalf("%s did not quit: %v", text, out)
		}
	}
	m, _ = drive(t, m, tea.BlurMsg{})
	if !strings.Contains(m.View().Content, "\x1b[2m") {
		t.Fatal("blurred console is not dimmed")
	}
	view := m.View()
	if !view.AltScreen || !view.ReportFocus || view.WindowTitle != "VGXNESS Console" || view.BackgroundColor != colorCanvas {
		t.Fatalf("view options = %+v", view)
	}
}

func TestBannerIsFourHalfBlockRows(t *testing.T) {
	rows := banner()
	if len(rows) != 4 {
		t.Fatalf("banner rows = %d", len(rows))
	}
	for _, row := range rows {
		if width := ansi.StringWidth(row); width != bannerWidth {
			t.Fatalf("banner row width = %d", width)
		}
		if strings.ContainsAny(ansi.Strip(row), "╗║═") {
			t.Fatal("banner uses double-line glyphs")
		}
	}
}

func TestWithBackgroundReopensAfterEveryReset(t *testing.T) {
	line := styleAccent.Render("ok") + " plain"
	painted := withBackground(line, colorPanel)
	open := backgroundSequence(colorPanel)
	if strings.Count(painted, open) < 2 || !strings.HasSuffix(painted, "\x1b[m") {
		t.Fatalf("background not reopened: %q", painted)
	}
}

func TestFormatHelpers(t *testing.T) {
	for _, tc := range []struct {
		then time.Time
		want string
	}{
		{testNow.Add(-3 * time.Second), "hace 3 s"},
		{testNow.Add(-2 * time.Minute), "hace 2 min"},
		{testNow.Add(-1 * time.Hour), "hace 1 h"},
		{time.Time{}, "nunca"},
	} {
		if got := ago(testNow, tc.then); got != tc.want {
			t.Errorf("ago(%v) = %q, want %q", tc.then, got, tc.want)
		}
	}
	if got := duration(70 * time.Minute); got != "1 h 10" {
		t.Errorf("duration = %q", got)
	}
	if got := thousands(10000); got != "10 000" {
		t.Errorf("thousands = %q", got)
	}
	if got := sanitizeTerminal("a\x1b[31mb\u202e"); strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\u202e') {
		t.Errorf("sanitize kept controls: %q", got)
	}
}

func TestRunRequiresInteractiveTerminals(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &stderr, fakeBackend{}, Options{})
	if code != 2 || !strings.Contains(stderr.String(), "interactive terminals") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestHomeWithMemoriesButNoPluginStaysCompleteAndPointsToSetup(t *testing.T) {
	overview := healthyOverview()
	overview.Plugin = Plugin{}
	m := start(t, fakeBackend{overview: overview}, 120, 35)
	screen := plain(m)
	assertContains(t, screen, "✕ plugin", "no instalado", "[1] Memoria", "[2] Setup del plugin", "instalar", "ÚLTIMO HANDOFF")
	if strings.Contains(screen, "Bienvenido.") {
		t.Fatal("a machine with memories is not a first use")
	}
	if home := m.page.(*homePage); home.selected != 1 {
		t.Fatalf("missing plugin should preselect Setup, got card %d", home.selected)
	}
}
