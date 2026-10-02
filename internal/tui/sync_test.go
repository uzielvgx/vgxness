package tui

import (
	"context"
	"strings"
	"testing"
	"time"
)

func init() { spinnerTick = time.Millisecond }

func configuredSync() *fakeSync {
	return &fakeSync{
		overview: SyncOverview{Configured: true, Enabled: true, Credential: "available", Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", PortableID: "2c4f76e4-8be6-4ac3", Pending: 43, LastPull: testNow.Add(-2 * time.Minute)},
		outcome:  SyncOutcome{Status: "synced", Pushed: 3, Took: 400 * time.Millisecond},
	}
}

func openSync(t *testing.T, sync *fakeSync, width, height int) Model {
	t.Helper()
	m := start(t, fakeBackend{overview: healthyOverview(), sync: sync}, width, height)
	m, _ = drive(t, m, press("3"), press("enter"))
	page, ok := m.page.(*syncPage)
	if !ok {
		t.Fatalf("page=%T after [3][Enter]", m.page)
	}
	page.now = func() time.Time { return testNow }
	return m
}

func TestSyncStatusShowsTheRealProfile(t *testing.T) {
	m := openSync(t, configuredSync(), 120, 35)
	screen := plain(m)
	assertContains(t, screen, "sync entre equipos   │   workspace", "SYNC", "https://sync.example.test",
		"✓ credencial", "disponible", "keyring del sistema", "· dispositivo", "550e8400", "✓ proyecto", "vinculado", "id portable 2c4f76e4",
		"· pendientes", "43", "cambios por enviar en este equipo", "✓ recepción", "hace 2 min",
		"CÓMO FUNCIONA", "nunca ejecuta git pull", "memory sync reseed", "memory sync rejoin", "panel del servidor",
		"ACCIONES", "[s] Sincronizar ahora", "[c] Configurar", "[s] sincronizar · [c] configurar · [r] actualizar · [Esc] volver")
	for _, absent := range []string{"K7QF", "cerrar sesión", "daemon", "[d]"} {
		if strings.Contains(strings.ToLower(screen), strings.ToLower(absent)) {
			t.Fatalf("screen shows the non-existent %q", absent)
		}
	}
	assertFits(t, m)
}

func TestSyncNowRunsAndReportsTheResult(t *testing.T) {
	sync := configuredSync()
	m := openSync(t, sync, 120, 35)
	next, _ := m.Update(press("s"))
	m = next.(Model)
	assertContains(t, plain(m), "SINCRONIZANDO", "proyecto 2c4f76e4", "◐ Enviando y recibiendo", "Solo [Ctrl+C] cancela", "[Ctrl+C] cancelar")
	if !m.page.Busy() {
		t.Fatal("sync must be busy while it runs")
	}
	m, _ = drive(t, m, run(m.page.(*syncPage).sync())...)
	if sync.syncs == 0 {
		t.Fatal("SyncNow was not called")
	}
	assertContains(t, plain(m), "ÚLTIMA SINCRONIZACIÓN", "3 enviadas · 0 rechazadas · 0 conflictos · 0.4 s", "· pendientes", "0")
	assertFits(t, m)
}

func TestSyncFailureExplainsByStatus(t *testing.T) {
	sync := configuredSync()
	sync.outcome = SyncOutcome{Status: "unreachable", FailureOperation: "push", FailureClass: "timeout"}
	m := openSync(t, sync, 120, 35)
	m, _ = drive(t, m, press("s"))
	screen := plain(m)
	assertContains(t, screen, "✕ SIN CONEXIÓN CON EL SERVIDOR", "El servidor no respondió.", "a salvo en ~/.vgxness/memory.db; 43 cambios",
		"Acción: Revisa tu conexión", "DETALLE", "estado unreachable · operación push · clase timeout", "[r] reintentar · [c] copiar detalle · [Esc] volver")
	assertFits(t, m)
	sync.outcome = SyncOutcome{Status: "unauthorized"}
	m, _ = drive(t, m, press("r"))
	assertContains(t, plain(m), "✕ CREDENCIAL RECHAZADA", "vgxness-syncd")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if title, _, _ := syncStatusText("", ctx.Err()); title != "Sincronización cancelada" {
		t.Fatalf("cancel title = %q", title)
	}
}

func TestSyncConfigureReplacesPairingWithTheRealInputs(t *testing.T) {
	sync := &fakeSync{overview: SyncOverview{Credential: "not_configured"}}
	m := openSync(t, sync, 120, 35)
	screen := plain(m)
	assertContains(t, screen, "◇ Sync no está configurado en este equipo.", "vgxness-syncd", "[c] Configurar", "[c] configurar · [Esc] volver")
	if strings.Contains(screen, "[s] Sincronizar") {
		t.Fatal("sync offered without a profile")
	}
	m, _ = drive(t, m, press("c"))
	assertContains(t, plain(m), "SYNC · CONFIGURAR", "Servidor (HTTPS)", "ID del dispositivo", "Bearer", "keyring del sistema", "[Tab] siguiente campo · [Enter] guardar · [Esc] cancelar")
	if !m.page.Captures() {
		t.Fatal("the form must own the keyboard")
	}
	m = typeText(t, m, "http://insecure")
	m, _ = drive(t, m, press("enter"), press("enter"), press("enter"))
	assertContains(t, plain(m), "✕ El servidor debe ser una URL https://.")
	if len(sync.configured) != 0 {
		t.Fatal("an http endpoint reached ConfigureSync")
	}
	page := m.page.(*syncPage)
	page.fields[0].SetValue("https://sync.example.test")
	page.fields[1].SetValue("550e8400-e29b-41d4-a716-446655440000")
	m, _ = drive(t, m, page.focus(2))
	m = typeText(t, m, "secret-bearer")
	if strings.Contains(plain(m), "secret-bearer") {
		t.Fatal("the bearer is echoed on screen")
	}
	m, _ = drive(t, m, press("enter"))
	if len(sync.configured) != 1 || sync.configured[0] != [3]string{"https://sync.example.test", "550e8400-e29b-41d4-a716-446655440000", "secret-bearer"} {
		t.Fatalf("configured=%v", sync.configured)
	}
	assertContains(t, plain(m), "Configuración guardada", "✓ credencial")
	if strings.Contains(plain(m), "secret-bearer") {
		t.Fatal("the bearer leaked into the status screen")
	}
}

func TestSyncUnboundProjectOffersTheInitCommand(t *testing.T) {
	sync := configuredSync()
	sync.overview.PortableID, sync.overview.LastPull = "", time.Time{}
	m := openSync(t, sync, 80, 24)
	screen := plain(m)
	assertContains(t, screen, "! proyecto", "sin vincular", "memory project init", "[p] copiar comando")
	if strings.Contains(screen, "[s] sincronizar") {
		t.Fatal("sync offered for an unbound project")
	}
	m, out := drive(t, m, press("p"))
	if len(out) != 1 || !strings.Contains(plain(m), "Comando copiado") {
		t.Fatalf("[p] did not copy: %v", out)
	}
	assertFits(t, m)
}
