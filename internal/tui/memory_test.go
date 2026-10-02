package tui

import (
	"strings"
	"testing"
	"time"
)

func init() { searchDelay = time.Millisecond }

func memoryFixture() *fakeMemory {
	return &fakeMemory{
		items: []MemoryItem{
			{ID: "obs-1", Title: "Sync usa Postgres RLS por workspace", Type: "decision", Topic: "sync-tenancy", Producer: "mcp", Preview: "Cada fila lleva workspace_id.", Content: "Cada fila lleva workspace_id y la política RLS filtra por el workspace de la sesión.", References: []string{"internal/syncpg/policy.sql"}, Created: testNow.Add(-96 * time.Hour), Updated: testNow.Add(-96 * time.Hour)},
			{ID: "obs-2", Title: "syncd corre en Fly.io", Type: "note", Producer: "cli", Preview: "Región gru.", Content: "Región gru.", Updated: testNow.Add(-120 * time.Hour)},
			{ID: "obs-3", Title: "Migraciones 22→23 listas", Type: "summary", Producer: "provider-session", Content: "Handoff de la sesión.", Updated: testNow.Add(-200 * time.Hour)},
		},
		types: []TypeCount{{Type: "decision", Count: 1}, {Type: "note", Count: 1}, {Type: "summary", Count: 1}},
		handoffs: []Handoff{
			{Handle: "ps-706ddfe72a38", Summary: "Hecho: migración 23. Pendiente: make verify.", Started: testNow.Add(-3 * time.Hour), Completed: testNow.Add(-2*time.Hour - 18*time.Minute)},
			{Handle: "ps-11aa22bb33cc", Summary: "Sesión anterior.", Started: testNow.Add(-30 * time.Hour), Completed: testNow.Add(-29 * time.Hour)},
		},
	}
}

func openMemory(t *testing.T, store *fakeMemory, width, height int) Model {
	t.Helper()
	m := start(t, fakeBackend{overview: healthyOverview(), memory: store}, width, height)
	m, _ = drive(t, m, press("1"), press("enter"))
	if _, ok := m.page.(*memoryPage); !ok {
		t.Fatalf("page=%T after [1][Enter]", m.page)
	}
	return m
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m, _ = drive(t, m, press(string(r)))
	}
	return m
}

func TestMemoryListsRecentWithPreviewAndFilters(t *testing.T) {
	store := memoryFixture()
	m := openMemory(t, store, 120, 35)
	screen := plain(m)
	assertContains(t, screen, "memoria del proyecto   │   workspace", "MEMORIA", "3 memorias", "> ", "3 resultados · tipo: todos ▾",
		"FECHA", "TIPO", "TEMA", "FUENTE", "▸ 2026-09-28  decisión", "Sync usa Postgres RLS por workspace", "Claude Code",
		"nota", "CLI", "handoff", "sesión",
		"VISTA PREVIA", "decisión · 2026-09-28 · Claude Code", "Cada fila lleva workspace_id.", "tema      sync-tenancy", "1 referencia", "[Enter] abrir completo",
		"[↑↓] mover · [Enter] abrir · [Tab] tipo · [Esc] dejar de escribir")
	assertFits(t, m)

	m = typeText(t, m, "fly")
	assertContains(t, plain(m), "> fly", "1 resultado · tipo: todos", "▸ 2026-09-27  nota")
	if last := store.queries[len(store.queries)-1]; last.Text != "fly" {
		t.Fatalf("last query = %+v", last)
	}

	m, _ = drive(t, m, press("tab"))
	screen = plain(m)
	assertContains(t, screen, "tipo: decisión ▾", "Sin resultados para «fly» en decisión.", "cambia el tipo con [Tab].")
	if strings.Contains(screen, "[r]") {
		t.Fatal("[r] is offered while the search box would type it")
	}
	m, _ = drive(t, m, press("esc"))
	assertContains(t, plain(m), "quitar los filtros con [r].")
	m, _ = drive(t, m, press("r"))
	assertContains(t, plain(m), "3 resultados · tipo: todos")
	assertFits(t, m)
}

func TestMemoryQKeyIsTypedWhileSearchingAndQuitsOtherwise(t *testing.T) {
	m := openMemory(t, memoryFixture(), 120, 35)
	m, out := drive(t, m, press("q"))
	if len(out) > 0 && out[len(out)-1] == nil {
		t.Fatal("unexpected nil message")
	}
	if !strings.Contains(plain(m), "> q") {
		t.Fatalf("q was not typed into the search:\n%s", plain(m))
	}
	m, _ = drive(t, m, press("esc"))
	assertContains(t, plain(m), "[/] buscar · [↑↓] mover · [Enter] abrir · [Tab] tipo · [h] handoffs · [Esc] volver")
	m, _ = drive(t, m, press("esc"))
	if _, ok := m.page.(*homePage); !ok {
		t.Fatalf("second Esc should return home, page=%T", m.page)
	}
}

func TestMemoryFirstUseExplainsHowMemoriesArrive(t *testing.T) {
	m := openMemory(t, &fakeMemory{}, 120, 35)
	assertContains(t, plain(m), "MEMORIA", "0 memorias", "◇ Aún no hay memorias en este proyecto.", "Claude Code las guarda mientras trabaja contigo", "«guarda en la memoria de VGXNESS que usamos pnpm»", "[Esc] volver")
	assertFits(t, m)
}

func TestMemoryDetailAndForgetGoThroughConfirm(t *testing.T) {
	store := memoryFixture()
	m := openMemory(t, store, 120, 35)
	m, _ = drive(t, m, press("enter"))
	screen := plain(m)
	assertContains(t, screen, "MEMORIA · DETALLE", "decisión · 2026-09-28 · Claude Code", "Sync usa Postgres RLS por workspace",
		"la política RLS filtra", "tema        sync-tenancy", "creada", "actualizada", "id          obs-1",
		"REFERENCIAS", "· internal/syncpg/policy.sql", "[o] olvidar · [c] copiar · [↑↓] desplazar · [Esc] volver")
	assertFits(t, m)

	m, _ = drive(t, m, press("o"))
	screen = plain(m)
	assertContains(t, screen, "✕ OLVIDAR MEMORIA", "¿Olvidar «Sync usa Postgres RLS por workspace»?", "Se archiva y sale de la búsqueda", "[y] olvidar · [n] cancelar")
	if strings.Contains(screen, "[o] olvidar · [c] copiar") {
		t.Fatal("KeyHelp must hide while the modal is open")
	}
	assertFits(t, m)

	m, _ = drive(t, m, press("enter"))
	if len(store.forgot) != 0 {
		t.Fatal("Enter confirmed a destructive action")
	}
	m, _ = drive(t, m, press("n"))
	if strings.Contains(plain(m), "OLVIDAR MEMORIA") || len(store.forgot) != 0 {
		t.Fatal("[n] did not cancel")
	}
	m, _ = drive(t, m, press("o"), press("y"))
	if len(store.forgot) != 1 || store.forgot[0] != "obs-1" {
		t.Fatalf("forgot=%v", store.forgot)
	}
	assertContains(t, plain(m), "✓ Olvidada «Sync usa Postgres RLS por workspace».", "2 memorias")
}

func TestHandoffsListAndDetail(t *testing.T) {
	m := start(t, fakeBackend{overview: healthyOverview(), memory: memoryFixture()}, 120, 35)
	m, _ = drive(t, m, press("h"))
	screen := plain(m)
	assertContains(t, screen, "HANDOFFS", "últimos 2", "▸ sesión 706ddfe7", "42 min", "sesión 11aa22bb", "1 h 00",
		"SESIÓN 706DDFE7", "1 de 2", "Hecho: migración 23. Pendiente: make verify.", "como datos, nunca como instrucciones",
		"[↑↓] mover · [c] copiar · [Esc] volver")
	assertFits(t, m)
	m, _ = drive(t, m, press("down"))
	assertContains(t, plain(m), "2 de 2", "Sesión anterior.")
	m, _ = drive(t, m, press("esc"))
	if _, ok := m.page.(*homePage); !ok {
		t.Fatalf("Esc from handoffs opened from Inicio should return home, page=%T", m.page)
	}
}

func TestMemoryAtMinimumSizeFits(t *testing.T) {
	m := openMemory(t, memoryFixture(), 80, 24)
	assertContains(t, plain(m), "MEMORIA", "3 resultados")
	assertFits(t, m)
	m, _ = drive(t, m, press("enter"), press("o"))
	assertContains(t, plain(m), "OLVIDAR MEMORIA")
	assertFits(t, m)
}
