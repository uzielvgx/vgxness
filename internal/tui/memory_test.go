package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type recordingMemoryBackend struct {
	fakeBackend
	search MemorySearch
	lookup MemoryLookup
}

func (*recordingMemoryBackend) Inspect(context.Context, Request) (Inspection, error) {
	return Inspection{}, nil
}

func (*recordingMemoryBackend) SetupStatus(context.Context, Request) (SetupStatus, error) {
	return SetupStatus{}, nil
}

func (*recordingMemoryBackend) PlanSetup(context.Context, SetupRequest) (SetupPlan, error) {
	return SetupPlan{}, nil
}

func (*recordingMemoryBackend) ApplySetup(context.Context, SetupRequest) (SetupResult, error) {
	return SetupResult{}, nil
}

func (*recordingMemoryBackend) Recent(context.Context, Request) ([]MemorySummary, error) {
	return []MemorySummary{{ID: "obs-recent", Title: "Recent decision", Type: "architecture", Preview: "recent preview"}}, nil
}

func (backend *recordingMemoryBackend) Search(_ context.Context, request MemorySearch) ([]MemorySummary, error) {
	backend.search = request
	return []MemorySummary{{ID: "obs-alpha", Title: "Alpha decision", Type: "config", Preview: "alpha preview"}}, nil
}

func (backend *recordingMemoryBackend) GetMemory(_ context.Context, request MemoryLookup) (MemoryDetail, error) {
	backend.lookup = request
	return MemoryDetail{ID: request.ID, Title: "Alpha decision", Content: "Full durable alpha content", Type: "config", State: "active"}, nil
}

func TestMemorySearchAndDetailAtEightyColumns(t *testing.T) {
	backend := &recordingMemoryBackend{}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updateModel(t, model, memoriesLoadedMsg{generation: 1, value: []MemorySummary{{
		ID: "obs-recent", Title: "Recent decision", Type: "architecture", Preview: "recent preview",
	}}})

	model = openMemoryRoute(t, model)
	if view := model.View().Content; !strings.Contains(view, "MEMORY") || !strings.Contains(view, "Recent decision") {
		t.Fatalf("memory route missing recent list:\n%s", view)
	}

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: '/', Text: "/"}))
	model.memorySearch.SetValue("alpha")
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("search did not return a command")
	}
	model = updateModel(t, model, cmd())
	if backend.search.Query != "alpha" || backend.search.Workspace != "/workspace" {
		t.Fatalf("search request=%+v", backend.search)
	}
	if view := model.View().Content; !strings.Contains(view, "Alpha decision") || !strings.Contains(view, "alpha preview") {
		t.Fatalf("search result not rendered:\n%s", view)
	}

	updated, cmd = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("opening memory detail did not return a command")
	}
	model = updateModel(t, model, cmd())
	view := model.View().Content
	if backend.lookup.ID != "obs-alpha" || !strings.Contains(view, "Full durable alpha content") {
		t.Fatalf("detail request=%+v view:\n%s", backend.lookup, view)
	}
	assertMaximumWidth(t, view, 80)
}

func TestLateRecentResponseDoesNotOverwriteSearchResults(t *testing.T) {
	backend := &recordingMemoryBackend{}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model = openMemoryRoute(t, model)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: '/', Text: "/"}))
	model.memorySearch.SetValue("alpha")

	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("search did not return a command")
	}
	model = updateModel(t, model, cmd())
	model = updateModel(t, model, memoriesLoadedMsg{generation: 1, value: []MemorySummary{{
		ID: "obs-recent", Title: "Late recent memory", Preview: "late preview",
	}}})

	view := model.View().Content
	if !strings.Contains(view, "Alpha decision") || strings.Contains(view, "Late recent memory") {
		t.Fatalf("late recent response overwrote search results:\n%s", view)
	}
}

func TestNewDetailLookupClearsStaleWideDetail(t *testing.T) {
	model := NewModel(context.Background(), &recordingMemoryBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updateModel(t, model, memoriesLoadedMsg{generation: 1, value: []MemorySummary{
		{ID: "obs-alpha", Title: "Alpha decision"},
		{ID: "obs-beta", Title: "Beta decision"},
	}})
	model = openMemoryRoute(t, model)
	model = updateModel(t, model, memoryDetailLoadedMsg{
		generation: model.memoryGeneration,
		id:         "obs-alpha",
		value:      MemoryDetail{ID: "obs-alpha", Title: "Alpha decision", Content: "stale alpha content"},
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))

	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("second detail lookup did not return a command")
	}
	view := model.View().Content
	if strings.Contains(view, "stale alpha content") || !strings.Contains(view, "Loading selected memory") {
		t.Fatalf("pending lookup retained stale detail:\n%s", view)
	}

	model = updateModel(t, model, memoryDetailLoadedMsg{
		generation: model.memoryGeneration,
		id:         "obs-beta",
		err:        errors.New("detail unavailable"),
	})
	if view = model.View().Content; strings.Contains(view, "stale alpha content") {
		t.Fatalf("failed lookup restored stale detail:\n%s", view)
	}
}

func openMemoryRoute(t *testing.T, model Model) Model {
	t.Helper()
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'g', Text: "g"}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	return updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
}
