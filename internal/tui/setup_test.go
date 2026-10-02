package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func init() { setupTick = time.Millisecond }

func freshSetup() *fakeSetup {
	return &fakeSetup{state: SetupState{
		PathBinary: PathBinary{Path: "/opt/homebrew/bin/vgxness", Version: "v0.9.0", SupportsPlugin: true},
		Claude:     Claude{Version: "2.1.287", Minimum: "2.1.284"},
		Plugin:     Plugin{Offered: "0.2.0"},
	}}
}

func openSetup(t *testing.T, setup *fakeSetup, width, height int) Model {
	t.Helper()
	m := start(t, fakeBackend{overview: healthyOverview(), setup: setup}, width, height)
	m, _ = drive(t, m, press("2"), press("enter"))
	if _, ok := m.page.(*setupPage); !ok {
		t.Fatalf("page=%T after [2][Enter]", m.page)
	}
	return m
}

func TestSetupWalksPrerequisitesPermissionsAndReview(t *testing.T) {
	m := openSetup(t, freshSetup(), 120, 35)
	screen := plain(m)
	assertContains(t, screen, "setup del plugin   │   workspace", "PASOS", "▸ 1. Prerrequisitos", "◌ 2. Permisos", "◌ 4. Aplicar", "Nada se ejecuta hasta",
		"PRERREQUISITOS", "· paso 1 de 4", "✓ vgxness en PATH", "v0.9.0", "/opt/homebrew/bin/vgxness", "✓ Claude Code", "✕ marketplace", "no agregado",
		"✕ plugin", "no instalado", "marketplace v0.2.0", "· permisos", "pendiente",
		"COMANDOS QUE SE EJECUTARÁN", "claude plugin marketplace add uzielvgx/vgxness", "claude plugin install vgxness@vgxness",
		"QUÉ INSTALA EL PLUGIN", "vgxness:explore · general · verifier · reviewer", "SessionStart · PreCompact · SessionEnd · SubagentStart", "memory · vgxness mcp --full", "git-delivery (solo bajo petición)",
		"[Enter] continuar · [c] copiar comandos · [r] revisar de nuevo · [Esc] volver · [q] salir")
	assertFits(t, m)

	m, out := drive(t, m, press("c"))
	if len(out) != 1 || !strings.Contains(plain(m), "Comandos copiados al portapapeles.") {
		t.Fatalf("[c] did not copy commands: %v", out)
	}
	m, _ = drive(t, m, press("enter"))
	assertContains(t, plain(m), "✓ 1. Prerrequisitos", "▸ 2. Permisos", "PERMISOS RECOMENDADOS", "· paso 2 de 4", "~/.claude/settings.json",
		`"mcp__plugin_vgxness_memory__memory_search"`, `"mcp__plugin_vgxness_memory__memory_forget"`, `"ask"`, "Leer y guardar no preguntan", "También lo imprime vgxness",
		"[Enter] continuar · [c] copiar JSON · [Esc] volver")
	if strings.Contains(plain(m), "handoff_get") {
		t.Fatal("permissions name a tool that does not exist")
	}
	assertFits(t, m)

	m, _ = drive(t, m, press("enter"))
	assertContains(t, plain(m), "▸ 3. Revisar y aplicar", "REVISAR Y APLICAR", "PLAN", "1. Agregar marketplace uzielvgx/vgxness", "modifica ~/.claude/plugins",
		"2. Instalar plugin vgxness@vgxness", "3. Verificar plugin habilitado", "solo lectura", "4. Verificar servidor MCP «memory»",
		"No se toca el repositorio", "! Al terminar hay que reiniciar Claude Code.", "[a] aplicar · [Esc] volver · [q] salir")
	assertFits(t, m)
}

func TestSetupApplyingAcceptsOnlyCtrlC(t *testing.T) {
	setup := freshSetup()
	m := openSetup(t, setup, 120, 35)
	m, _ = drive(t, m, press("enter"), press("enter"))
	next, _ := m.Update(press("a"))
	m = next.(Model)
	screen := plain(m)
	assertContains(t, screen, "INSTALANDO PLUGIN", "· paso 4 de 4 · 0/4", "░", "0 %", "◐ Agregar marketplace", "◌ Instalar plugin", "Solo [Ctrl+C] cancela", "REGISTRO", "$ claude plugin marketplace add uzielvgx/vgxness", "[Ctrl+C] cancelar")
	assertFits(t, m)
	if next, cmd := m.Update(press("q")); cmd != nil || next.(Model).page != m.page {
		t.Fatal("q must be ignored while a step mutates")
	}
	// Ctrl+C cancels the step's context; the step then returns the
	// cancellation, which the page reports as an interruption.
	page := m.page.(*setupPage)
	cancel := page.cancel
	called := false
	page.cancel = func() { called = true; cancel() }
	next, _ = m.Update(press("ctrl+c"))
	m = next.(Model)
	if !called {
		t.Fatal("Ctrl+C did not cancel the running step")
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	m, _ = drive(t, m, run(page.runStep(ctx, 0))...)
	assertContains(t, plain(m), "✕ INSTALACIÓN INTERRUMPIDA", "Cancelaste la aplicación.", "[r] reintentar")
	if len(setup.ran) != 0 {
		t.Fatalf("a cancelled step still ran: %v", setup.ran)
	}
}

func TestSetupRunsEveryStepAndReportsTheResult(t *testing.T) {
	setup := freshSetup()
	m := openSetup(t, setup, 120, 35)
	m, _ = drive(t, m, press("enter"), press("enter"), press("a"))
	want := []SetupStep{StepAddMarketplace, StepInstallPlugin, StepVerifyPlugin, StepVerifyMCP}
	if !reflect.DeepEqual(setup.ran, want) {
		t.Fatalf("ran %v, want %v", setup.ran, want)
	}
	screen := plain(m)
	assertContains(t, screen, "✓ 4. Aplicar", "✓ INSTALACIÓN COMPLETA", "█", "100 %", "✓ plugin", "v0.2.0", "✓ servidor MCP", "conectado", "✓ agentes", "✓ hooks",
		"plugin:vgxness:memory", "! Reinicia Claude Code para cargar el plugin.", "política del Manager", "[Enter] volver al inicio · [q] salir")
	assertFits(t, m)
	m, _ = drive(t, m, press("enter"))
	if _, ok := m.page.(*homePage); !ok {
		t.Fatalf("Enter after the result should return home, page=%T", m.page)
	}
}

func TestSetupFailureShowsTheErrorAndRetriesFromTheFailedStep(t *testing.T) {
	setup := freshSetup()
	setup.fail = map[SetupStep]error{StepInstallPlugin: errors.New("claude plugin install vgxness@vgxness exited with status 1")}
	m := openSetup(t, setup, 120, 35)
	m, _ = drive(t, m, press("enter"), press("enter"), press("a"))
	screen := plain(m)
	assertContains(t, screen, "✕ 4. Aplicar", "✕ INSTALACIÓN INTERRUMPIDA", "· paso 2/4", "detenido", "✓ Agregar marketplace", "✕ Instalar plugin", "error",
		"exited with status 1", "Acción: ejecuta  claude plugin marketplace update vgxness", "plugin vgxness not found in marketplace",
		"[r] reintentar · [c] copiar detalle · [Esc] volver · [q] salir")
	assertFits(t, m)
	m, out := drive(t, m, press("c"))
	if len(out) != 1 || !strings.Contains(plain(m), "Detalle copiado") {
		t.Fatalf("[c] did not copy the log: %v", out)
	}
	setup.fail, setup.ran = nil, nil
	m, _ = drive(t, m, press("r"))
	if want := []SetupStep{StepInstallPlugin, StepVerifyPlugin, StepVerifyMCP}; !reflect.DeepEqual(setup.ran, want) {
		t.Fatalf("retry ran %v, want %v", setup.ran, want)
	}
	assertContains(t, plain(m), "✓ INSTALACIÓN COMPLETA")
}

func TestSetupWithAnUpToDatePluginOnlyVerifies(t *testing.T) {
	setup := freshSetup()
	setup.state.Plugin = Plugin{Installed: true, Enabled: true, MarketplaceAdded: true, Version: "0.2.0", Offered: "0.2.0"}
	m := openSetup(t, setup, 120, 35)
	assertContains(t, plain(m), "✓ marketplace", "✓ plugin", "al día", "Ninguno: el plugin ya está instalado y al día")
	if strings.Contains(plain(m), "[c] copiar comandos") {
		t.Fatal("no commands to copy")
	}
	m, _ = drive(t, m, press("enter"), press("enter"))
	if strings.Contains(plain(m), "reiniciar Claude Code") {
		t.Fatal("verification alone needs no restart")
	}
	m, _ = drive(t, m, press("a"))
	if want := []SetupStep{StepVerifyPlugin, StepVerifyMCP}; !reflect.DeepEqual(setup.ran, want) {
		t.Fatalf("ran %v, want %v", setup.ran, want)
	}
	screen := plain(m)
	assertContains(t, screen, "✓ VERIFICACIÓN COMPLETA")
	if strings.Contains(screen, "Reinicia Claude Code") {
		t.Fatal("verification alone needs no restart")
	}
}

func TestSetupWithAnOutdatedPluginUpdatesIt(t *testing.T) {
	setup := freshSetup()
	setup.state.Plugin = Plugin{Installed: true, Enabled: true, MarketplaceAdded: true, Version: "0.1.0", Offered: "0.2.0"}
	m := openSetup(t, setup, 120, 35)
	assertContains(t, plain(m), "! plugin", "v0.2.0 disponible", "claude plugin update vgxness@vgxness")
	m, _ = drive(t, m, press("enter"), press("enter"), press("a"))
	if want := []SetupStep{StepUpdatePlugin, StepVerifyPlugin, StepVerifyMCP}; !reflect.DeepEqual(setup.ran, want) {
		t.Fatalf("ran %v, want %v", setup.ran, want)
	}
	assertContains(t, plain(m), "✓ ACTUALIZACIÓN COMPLETA")
}

func TestSetupWithoutClaudeCodeCannotContinue(t *testing.T) {
	setup := freshSetup()
	setup.state.Claude = Claude{Minimum: "2.1.284"}
	setup.state.PathBinary = PathBinary{}
	m := openSetup(t, setup, 80, 24)
	screen := plain(m)
	assertContains(t, screen, "✕ vgxness en PATH", "no encontrado", "brew install", "uzielvgx/tap/vgxness", "✕ Claude Code", "Sin Claude Code no se puede continuar.", "· paso 1 de 4")
	if strings.Contains(screen, "PASOS") {
		t.Fatal("the step rail should drop below 100 columns")
	}
	m, _ = drive(t, m, press("enter"))
	if page := m.page.(*setupPage); page.phase != phasePrereq {
		t.Fatalf("Enter advanced without Claude Code to phase %d", page.phase)
	}
	assertFits(t, m)
}

func TestPluginContentsMatchTheRepository(t *testing.T) {
	root := filepath.Join("..", "..", "plugins", "vgxness")
	files, err := filepath.Glob(filepath.Join(root, "agents", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	var agents []string
	for _, file := range files {
		agents = append(agents, strings.TrimSuffix(filepath.Base(file), ".md"))
	}
	want := append([]string(nil), pluginAgents...)
	sort.Strings(agents)
	sort.Strings(want)
	if !reflect.DeepEqual(agents, want) {
		t.Fatalf("plugin agents %v, console lists %v", agents, want)
	}
	hooks, err := os.ReadFile(filepath.Join(root, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, hook := range pluginHooks {
		if !strings.Contains(string(hooks), `"`+hook+`"`) {
			t.Errorf("hooks.json lacks %s", hook)
		}
	}
	mcp, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil || !strings.Contains(string(mcp), `"`+pluginMCPServer+`"`) {
		t.Errorf(".mcp.json lacks the %q server: %v", pluginMCPServer, err)
	}
	for _, skill := range pluginSkills {
		if _, err := os.Stat(filepath.Join(root, "skills", skill, "SKILL.md")); err != nil {
			t.Errorf("skill %s: %v", skill, err)
		}
	}
}

func TestStepperShowsDoneCurrentAndPending(t *testing.T) {
	lines := renderStepper([]step{{state: stateOK, title: "a"}, {state: statePending, title: "b"}, {state: statePending, title: "c"}}, 1, []string{"nota"})
	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"✓ 1. a", "│", "▸ 2. b", "◌ 3. c", "nota"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stepper lacks %q:\n%s", want, joined)
		}
	}
}

func TestSetupFlagsAPathBinaryOlderThanThePlugin(t *testing.T) {
	setup := freshSetup()
	setup.state.PathBinary = PathBinary{Path: "/home/.local/bin/vgxness", Version: "dev"}
	m := openSetup(t, setup, 120, 35)
	assertContains(t, plain(m), "✕ vgxness en PATH", "versión anterior al", "Acción: actualízalo: brew upgrade vgxness")
}
