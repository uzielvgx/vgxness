package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type fakeBackend struct{}

func (fakeBackend) Inspect(context.Context, Request) (Inspection, error) {
	return Inspection{}, nil
}

func (fakeBackend) SetupStatus(context.Context, Request) (SetupStatus, error) {
	return SetupStatus{}, nil
}

func (fakeBackend) Recent(context.Context, Request) ([]MemorySummary, error) {
	return nil, nil
}

func (fakeBackend) Search(context.Context, MemorySearch) ([]MemorySummary, error) {
	return nil, nil
}

func (fakeBackend) GetMemory(context.Context, MemoryLookup) (MemoryDetail, error) {
	return MemoryDetail{}, nil
}

func (fakeBackend) PlanSetup(context.Context, SetupRequest) (SetupPlan, error) {
	return SetupPlan{}, nil
}

func (fakeBackend) ApplySetup(context.Context, SetupRequest) (SetupResult, error) {
	return SetupResult{}, nil
}

func TestOverviewRendersIndependentResultsAtEightyColumns(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updateModel(t, model, inspectionLoadedMsg{generation: 1, value: Inspection{
		Root: "/storage", Database: "/storage/memory.db", Migration: 5,
	}})
	model = updateModel(t, model, setupLoadedMsg{generation: 1, err: errors.New("opencode unavailable")})
	model = updateModel(t, model, memoriesLoadedMsg{generation: 1, value: []MemorySummary{{
		ID: "obs-1", Title: "Durable TUI decision", Type: "architecture",
	}}})

	view := model.View()
	if !view.AltScreen || view.WindowTitle != "VGXNESS Console" {
		t.Fatalf("unexpected terminal view options: alt=%t title=%q", view.AltScreen, view.WindowTitle)
	}
	for index, line := range strings.Split(view.Content, "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Fatalf("line %d width=%d: %q", index+1, width, line)
		}
	}
	for _, expected := range []string{"schema v5", "Durable TUI decision", "Setup unavailable"} {
		if !strings.Contains(view.Content, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view.Content)
		}
	}
}

func TestOverviewIgnoresStaleAsyncResultsAndQuits(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, inspectionLoadedMsg{generation: 0, value: Inspection{Migration: 99}})
	if strings.Contains(model.View().Content, "schema v99") {
		t.Fatal("stale inspection result was rendered")
	}

	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	if _, ok := updated.(Model); !ok || cmd == nil {
		t.Fatalf("quit update returned model=%T cmd=%v", updated, cmd)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit command returned %T", cmd())
	}
}

func TestRefreshAndQuitCancelSupersededLoads(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	firstLoad := model.loadCtx
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'r', Text: "r"}))
	select {
	case <-firstLoad.Done():
	default:
		t.Fatal("refresh did not cancel the superseded load")
	}
	secondLoad := model.loadCtx
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	model = updated.(Model)
	select {
	case <-secondLoad.Done():
	default:
		t.Fatal("quit did not cancel the active load")
	}
}

func TestSectionNavigatorOpensSystemAndEscReturnsOverview(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updateModel(t, model, inspectionLoadedMsg{generation: 1, value: Inspection{
		Root: "/storage", Database: "/storage/memory.db", Migration: 5,
	}})
	model = updateModel(t, model, setupLoadedMsg{generation: 1, value: SetupStatus{
		Ready: true, SelfInstallState: "installed", SelfInstallPath: "/bin/vgxness",
		IntegrationState: "installed", IntegrationPath: "/config/vgxness-manager.md",
		ArtifactCount: 15, HandshakeOK: true, HandshakeStatus: "healthy", ModelPlan: "medium",
	}})

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'g', Text: "g"}))
	if !strings.Contains(model.View().Content, "SECTIONS") {
		t.Fatalf("section navigator did not open:\n%s", model.View().Content)
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	view := model.View().Content
	if !strings.Contains(view, "SYSTEM HEALTH") || !strings.Contains(view, "schema v5") || !strings.Contains(view, "CLI-only") {
		t.Fatalf("system route missing evidence:\n%s", view)
	}
	assertMaximumWidth(t, view, 80)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if view = model.View().Content; !strings.Contains(view, "RECENT PROJECT MEMORY") || strings.Contains(view, "SYSTEM HEALTH") {
		t.Fatalf("escape did not return to overview:\n%s", view)
	}
}

func TestWideSystemKeepsPersistentSectionList(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'g', Text: "g"}))
	if view := model.View().Content; !strings.Contains(view, "NAV FOCUS") || !strings.Contains(view, "j/k move") {
		t.Fatalf("wide navigation focus is not visible:\n%s", view)
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	view := model.View().Content
	for _, expected := range []string{"Overview", "System", "SYSTEM HEALTH"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("wide system view missing %q:\n%s", expected, view)
		}
	}
	assertMaximumWidth(t, view, 120)
}

func TestSanitizeTerminalEscapesControls(t *testing.T) {
	got := sanitizeTerminal("line\nreturn\rtab\tescape\x1bdel\x7fbidi\u202e")
	want := `line\nreturn\rtab\tescape\x1bdel\x7fbidi\u202e`
	if got != want {
		t.Fatalf("sanitizeTerminal()=%q want=%q", got, want)
	}
}

func updateModel(t *testing.T, model Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(msg)
	result, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return result
}

func assertMaximumWidth(t *testing.T, content string, maximum int) {
	t.Helper()
	for index, line := range strings.Split(content, "\n") {
		if width := lipgloss.Width(line); width > maximum {
			t.Fatalf("line %d width=%d maximum=%d: %q", index+1, width, maximum, line)
		}
	}
}
