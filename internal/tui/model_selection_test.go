package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"github.com/vgxness/vgxness/internal/agentmodels"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"strings"
	"testing"
)

func TestModelSelectionSeparatesProvidersAndCodexPlans(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupView = setupViewPlan
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderPi, setupflow.ProviderCodex}
	m.updateModelChoices(keyPress("1"))
	m.updateModelChoices(keyPress("i"))
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
		m.updateModelChoices(keyPress("i"))
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

func TestModelSelectionCanReplaceInheritedUnknownEffort(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.modelChoices[0].Mode = "single"
	m.modelChoices[0].Rows[0].Model = "vendor/model"
	m.modelChoices[0].Rows[0].Effort = "max"
	m.updateModelChoices(keyPress("e"))
	if c := m.modelChoices[0].config(); c == nil || c.Validate() != nil {
		t.Fatalf("inherited effort could not be replaced: %+v", c)
	}
}

func TestModelChoicesPickScannedModelsAndExactEfforts(t *testing.T) {
	catalog := []SetupCatalogModel{
		{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{Provider: "acme", Reference: "acme/fast", Variants: []string{"high"}},
	}
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.setupView = setupViewPlan
	m.setupCatalog = append([]SetupCatalogModel(nil), catalog...)
	m.modelChoices[0] = modelChoice{Mode: "single", Rows: [7]agentmodels.Assignment{{Model: "acme/fast", Effort: "off"}}}

	m.updateModelChoices(keyPress("m"))
	if !m.modelChoiceSearching {
		t.Fatal("m did not open the scanned model picker")
	}
	assertMaximumWidth(t, m.View().Content, 80)
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Text: "terra"}))
	matches := m.filteredModelChoiceCatalog(setupflow.ProviderOpenCode)
	if len(matches) != 1 || matches[0].Reference != "openai/gpt-5.6-terra" {
		t.Fatalf("query did not filter the scanned catalog: %+v", matches)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.modelChoiceSearching {
		t.Fatal("Enter did not close the picker")
	}
	row := m.modelChoices[0].Rows[0]
	if row.Model != "openai/gpt-5.6-terra" || row.Effort != "off" || row.Variant != "" {
		t.Fatalf("picked model = %+v, want provider default", row)
	}

	// Scanned efforts cycle in discovery order on the OpenCode tab.
	wantVariants := []string{"none", "", "", "", "", "max"}
	wantEfforts := []string{"off", "low", "medium", "high", "xhigh", "xhigh"}
	for index := range wantVariants {
		m.updateModelChoices(keyPress("e"))
		row = m.modelChoices[0].Rows[0]
		if row.Variant != wantVariants[index] || row.Effort != wantEfforts[index] {
			t.Fatalf("effort step %d = %+v, want variant=%q effort=%q", index, row, wantVariants[index], wantEfforts[index])
		}
	}
	if config := m.modelChoices[0].config(); config == nil || config.Validate() != nil || config.Assignments["manager"].Variant != "max" {
		t.Fatalf("exact scanned variant did not transport: %+v", config)
	}
	assertMaximumWidth(t, m.View().Content, 80)
}

func TestModelChoiceManualEntryResetsStaleVariant(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.modelChoices[0] = modelChoice{Mode: "single", Edited: true, Rows: [7]agentmodels.Assignment{{Model: "acme/fast", Effort: "xhigh", Variant: "max"}}}

	m.updateModelChoices(keyPress("i"))
	if !m.modelChoiceEditing {
		t.Fatal("i did not open the manual editor")
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Text: "/luna"}))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	row := m.modelChoices[0].Rows[0]
	if row.Model != "acme/fast/luna" || row.Effort != "off" || row.Variant != "" {
		t.Fatalf("manual replacement = %+v, want reset provider default", row)
	}
}

func TestModelChoicesPiTabUsesScannedStoreEfforts(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderPi}
	m.setupView = setupViewPlan
	m.piCatalog = []SetupCatalogModel{
		{Provider: "openai-codex", Reference: "openai-codex/gpt-5.6-luna", Variants: []string{"minimal", "xhigh"}},
		{Provider: "openai-codex", Reference: "openai-codex/plain"},
	}
	m.modelChoices[1] = modelChoice{Mode: "single", Rows: [7]agentmodels.Assignment{{Effort: "off"}}}

	m.updateModelChoices(keyPress("m"))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Text: "luna"}))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	row := m.modelChoices[1].Rows[0]
	if row.Model != "openai-codex/gpt-5.6-luna" || row.Effort != "off" || row.Variant != "" {
		t.Fatalf("Pi pick = %+v, want provider default", row)
	}
	m.updateModelChoices(keyPress("e"))
	row = m.modelChoices[1].Rows[0]
	if row.Variant != "" || row.Effort != "minimal" {
		t.Fatalf("Pi effort = %+v, want minimal without an OpenCode variant", row)
	}
	m.updateModelChoices(keyPress("e"))
	if row = m.modelChoices[1].Rows[0]; row.Effort != "xhigh" || row.Variant != "" {
		t.Fatalf("Pi effort = %+v, want xhigh", row)
	}
	m.updateModelChoices(keyPress("e"))
	if row = m.modelChoices[1].Rows[0]; row.Effort != "off" || row.Variant != "" {
		t.Fatalf("Pi effort = %+v, want provider default wrap", row)
	}
	if config := m.modelChoices[1].config(); config == nil || config.Validate() != nil {
		t.Fatalf("Pi scanned selection did not validate: %+v", config)
	}
}

func TestModelChoicesPiModelWithoutEffortsKeepsProviderDefault(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderPi}
	m.setupView = setupViewPlan
	m.piCatalog = []SetupCatalogModel{{Provider: "openai-codex", Reference: "openai-codex/plain"}}
	m.modelChoices[1] = modelChoice{Mode: "single", Edited: true, Rows: [7]agentmodels.Assignment{{Model: "openai-codex/plain", Effort: "off"}}}
	m.updateModelChoices(keyPress("e"))
	if row := m.modelChoices[1].Rows[0]; row.Effort != "off" || row.Variant != "" {
		t.Fatalf("discovered non-reasoning model left provider default: %+v", row)
	}
}

func TestModelChoicesTabScansProviderCatalogOnce(t *testing.T) {
	backend := &recordingMultiSetupBackend{}
	m := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderPi}
	m.setupView = setupViewPlan
	m.setupCatalogAttempted = true
	m.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"high"}}}

	_, cmd := m.updateModelChoices(keyPress("tab"))
	if cmd == nil || m.choiceProvider() != setupflow.ProviderPi || !m.piCatalogLoading {
		t.Fatalf("tab did not start the Pi scan: provider=%v cmd=%v", m.choiceProvider(), cmd)
	}
	message := cmd()
	loaded, ok := message.(setupCatalogLoadedMsg)
	if !ok || loaded.provider != setupflow.ProviderPi {
		t.Fatalf("Pi scan message = %#v", message)
	}
	m.handleSetupCatalogLoaded(loaded)
	if m.piCatalogLoading || len(m.piCatalog) != 0 {
		t.Fatalf("Pi scan state: loading=%t rows=%+v", m.piCatalogLoading, m.piCatalog)
	}
	if len(backend.catalogProviders) != 1 || backend.catalogProviders[0] != setupflow.ProviderPi {
		t.Fatalf("Pi scan calls = %v", backend.catalogProviders)
	}
	if _, again := m.updateModelChoices(keyPress("tab")); again != nil {
		t.Fatal("already scanned OpenCode catalog was reloaded")
	}
	if _, again := m.updateModelChoices(keyPress("tab")); again != nil {
		t.Fatal("already scanned Pi catalog was reloaded")
	}
}

func TestModelChoicesModelsEntryScansPiCatalog(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderPi}
	m.setupView = setupViewProviders
	m.width, m.height = 80, 24

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if cmd == nil || m.setupView != setupViewPlan || m.piCatalogGeneration != 1 || !m.piCatalogLoading {
		t.Fatalf("Models entry did not scan Pi once: view=%v generation=%d loading=%t", m.setupView, m.piCatalogGeneration, m.piCatalogLoading)
	}
	if m.setupCatalogGeneration != 0 {
		t.Fatal("Models entry scanned an unselected provider")
	}
}

func TestModelChoicesSearchCancelKeepsSelection(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.setupView = setupViewPlan
	m.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra"}}
	m.modelChoices[0] = modelChoice{Mode: "single", Edited: true, Rows: [7]agentmodels.Assignment{{Model: "acme/fast", Effort: "off"}}}
	m.updateModelChoices(keyPress("m"))
	m.updateModelChoices(keyPress("i"))
	if !m.modelChoiceSearching || m.modelChoiceQuery != "i" {
		t.Fatalf("typed letters must filter instead of opening the manual editor: searching=%t query=%q", m.modelChoiceSearching, m.modelChoiceQuery)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Text: "terra"}))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.modelChoiceSearching || m.modelChoices[0].Rows[0].Model != "acme/fast" {
		t.Fatalf("cancel changed the selection: %+v", m.modelChoices[0].Rows[0])
	}
}

func TestModelChoicesSeedInstalledVariantTokens(t *testing.T) {
	var rows [integration.ModelAssignmentCount]modelplan.OpenCodeAgentAssignmentV3
	for index, identity := range setupAgentRows {
		rows[index] = modelplan.OpenCodeAgentAssignmentV3{ArtifactKey: identity.ArtifactKey, Model: "openai/gpt-5.6-terra", RequestedEffort: modelplan.EffortUltra, Effort: modelplan.EffortUltra, Variant: modelplan.OpenCodeVariant("max")}
	}
	plan := setupflow.MultiPlan{Providers: []setupflow.ProviderPlan{{Provider: setupflow.ProviderOpenCode, Integration: integration.Result{ModelAssignments: &rows}}}}
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.seedModelChoices(plan)
	row := m.modelChoices[0].Rows[0]
	if row.Model != "openai/gpt-5.6-terra" || row.Variant != "max" || row.Effort != "xhigh" {
		t.Fatalf("installed variant was not seeded exactly: %+v", row)
	}
	if m.modelChoices[0].Edited {
		t.Fatal("seeding marked installed selections as edited")
	}
	m.modelChoices[0].Edited = true
	if config := m.modelChoices[0].config(); config == nil || config.Validate() != nil || config.Assignments["manager"].Variant != "max" {
		t.Fatalf("seeded variant did not validate: %+v", config)
	}
}

func TestModelChoicesCancellationKeepsAutoScanAvailable(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderPi}
	m.cancelSetupOperation()
	if m.piCatalogAttempted {
		t.Fatal("cancellation marked the Pi scan as completed")
	}
	if cmd := m.ensureProviderCatalog(setupflow.ProviderPi); cmd == nil || !m.piCatalogLoading {
		t.Fatal("cancellation disabled the Pi auto-scan")
	}
	m.handleSetupCatalogLoaded(setupCatalogLoadedMsg{provider: setupflow.ProviderPi, generation: m.piCatalogGeneration, err: errors.New("unavailable")})
	if cmd := m.ensureProviderCatalog(setupflow.ProviderPi); cmd != nil {
		t.Fatal("a completed scan attempt was repeated automatically")
	}
}

func TestModelChoicesFailedRefreshLabelsStaleCatalog(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.setupView = setupViewPlan
	m.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"high"}}}
	m.setupCatalogErr = errors.New("scan failed")
	status := strings.Join(m.modelChoiceCatalogStatus(setupflow.ProviderOpenCode), "\n")
	if !strings.Contains(status, "1 previously scanned models") {
		t.Fatalf("stale catalog was not labeled: %q", status)
	}
	m.modelChoiceSearching = true
	search := strings.Join(m.modelChoiceSearchLines(setupflow.ProviderOpenCode), "\n")
	if !strings.Contains(search, "previously scanned models") || !strings.Contains(search, "openai/gpt-5.6-terra") {
		t.Fatalf("stale search results were not labeled: %q", search)
	}
}

func TestModelChoicesStalePiCatalogResultIsDropped(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.piCatalogGeneration = 2
	m.handleSetupCatalogLoaded(setupCatalogLoadedMsg{provider: setupflow.ProviderPi, generation: 1, rows: []SetupCatalogModel{{Reference: "stale/model"}}})
	if len(m.piCatalog) != 0 || m.piCatalogAttempted || m.piCatalogLoading {
		t.Fatalf("stale Pi result was applied: rows=%+v attempted=%t loading=%t", m.piCatalog, m.piCatalogAttempted, m.piCatalogLoading)
	}
}

func TestModelChoicesRefreshRescansCurrentProvider(t *testing.T) {
	backend := &recordingMultiSetupBackend{}
	m := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderPi}
	m.setupView = setupViewPlan
	m.modelChoiceProvider = 1
	_, cmd := m.updateModelChoices(keyPress("r"))
	if cmd == nil {
		t.Fatal("r did not start a rescan")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("r command = %#v", cmd)
	}
	for _, sub := range batch {
		_ = sub()
	}
	if len(backend.catalogProviders) != 1 || backend.catalogProviders[0] != setupflow.ProviderPi || len(backend.catalogCalls) != 1 || !backend.catalogCalls[0] {
		t.Fatalf("rescan calls providers=%v refresh=%v", backend.catalogProviders, backend.catalogCalls)
	}
	if len(backend.multiRequests) != 1 {
		t.Fatalf("r did not refresh the plan: requests=%d", len(backend.multiRequests))
	}
}
