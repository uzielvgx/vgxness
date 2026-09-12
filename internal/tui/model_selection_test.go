package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"testing"
)

func TestModelSelectionSeparatesProvidersAndCodexPlans(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupView = setupViewPlan
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderPi, setupflow.ProviderCodex}
	m.updateModelChoices(keyPress("1"))
	m.updateModelChoices(keyPress("m"))
	m.updateModelChoices(keyPress("vendor/model"))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	first := m.setupRequest()
	if first.OpenCodeModels == nil || first.OpenCodeModels.Validate() != nil || first.PiModels != nil {
		t.Fatalf("%+v", first)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m.updateModelChoices(keyPress("2"))
	for i := 0; i < 7; i++ {
		m.modelChoiceRow = i
		m.updateModelChoices(keyPress("m"))
		m.updateModelChoices(keyPress("pi/model"))
		m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	}
	second := m.setupRequest()
	if second.PiModels == nil || second.PiModels.Mode != "per-agent" || second.PiModels.Validate() != nil {
		t.Fatalf("%+v", second)
	}
	if second.OpenCodeModels.Assignments["manager"].Model != "vendor/model" {
		t.Fatal("Pi selection leaked into OpenCode")
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m.setupSelected = "medium"
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if m.setupSelected != "high" {
		t.Fatal("Codex plans unavailable")
	}
	m.updateModelChoices(keyPress("1"))
	if m.setupRequest().PiModels.Mode != "per-agent" {
		t.Fatal("Codex changed Pi mode")
	}
	snapshot := cloneSetupRequest(second)
	m.modelChoices[1].Rows[0].Model = "changed/model"
	if snapshot.PiModels.Assignments["manager"].Model != "pi/model" {
		t.Fatal("preview snapshot mutated")
	}
}
