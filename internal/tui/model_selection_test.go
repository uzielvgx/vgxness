package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"errors"
	"fmt"
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
	if !m.pickerOpen || m.pickerStep != pickerProviders {
		t.Fatalf("m did not open the provider modal: open=%t step=%d", m.pickerOpen, m.pickerStep)
	}
	assertMaximumWidth(t, m.View().Content, 80)
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Text: "openai"}))
	if providers := m.pickerProviders(); len(providers) != 1 || providers[0].name != "openai" {
		t.Fatalf("query did not filter providers: %+v", providers)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if !m.pickerOpen || m.pickerStep != pickerModels || m.pickerProvider != "openai" {
		t.Fatalf("provider step did not advance: open=%t step=%d provider=%q", m.pickerOpen, m.pickerStep, m.pickerProvider)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Text: "terra"}))
	if models := m.pickerModels(); len(models) != 1 || models[0].Reference != "openai/gpt-5.6-terra" {
		t.Fatalf("query did not filter models: %+v", models)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.pickerOpen {
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
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
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
	m.setupCatalogAttempted = true
	m.modelChoices[0] = modelChoice{Mode: "single", Edited: true, Rows: [7]agentmodels.Assignment{{Model: "acme/fast", Effort: "off"}}}

	m.updateModelChoices(keyPress("m"))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.pickerStep != pickerModels {
		t.Fatalf("provider step did not open models: step=%d", m.pickerStep)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Text: "terra"}))
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if !m.pickerOpen || m.pickerStep != pickerProviders {
		t.Fatalf("Esc did not go back to providers: open=%t step=%d", m.pickerOpen, m.pickerStep)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.pickerOpen || m.modelChoices[0].Rows[0].Model != "acme/fast" {
		t.Fatalf("cancel changed the selection: open=%t row=%+v", m.pickerOpen, m.modelChoices[0].Rows[0])
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
	m.setupCatalogAttempted = true
	m.setupCatalogErr = errors.New("scan failed")
	m.modelChoices[0] = modelChoice{Mode: "single"}
	status := strings.Join(m.modelChoiceCatalogStatus(setupflow.ProviderOpenCode), "\n")
	if !strings.Contains(status, "1 previously scanned models") {
		t.Fatalf("stale catalog was not labeled: %q", status)
	}
	m.updateModelChoices(keyPress("m"))
	if box := m.modelPickerBox(); !strings.Contains(box, "scan unavailable") {
		t.Fatalf("picker hid the scan failure: %q", box)
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
	m.modelChoices[1] = modelChoice{Mode: "single"}
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

func TestMultiSetupScreensStayCompactAndUnwrapped(t *testing.T) {
	model := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderPi}

	model.setupView = setupViewProviders
	view := strings.Join(model.setupRouteLines(), "\n")
	for _, want := range []string{"PROVIDERS", "per-agent models", "VGXNESS_PI_RELEASE_DIR"} {
		if !strings.Contains(view, want) {
			t.Fatalf("providers missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "┌") {
		t.Fatalf("providers kept box borders:\n%s", view)
	}

	model.setupView = setupViewPlan
	model.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"low", "high"}}}
	model.setupCatalogAttempted = true
	model.modelChoices[0] = modelChoice{Mode: "single", Rows: [7]agentmodels.Assignment{{Model: "openai/gpt-5.6-terra", Effort: "high"}}}
	model.updateModelChoices(keyPress("m"))
	content := model.View().Content
	if !strings.Contains(content, "Select provider") {
		t.Fatalf("picker modal missing:\n%s", content)
	}
	if strings.Contains(content, "[Tab] provider") {
		t.Fatalf("picker kept the base footer:\n%s", content)
	}
	if got := strings.Count(content, "[Enter] open provider"); got != 1 {
		t.Fatalf("picker help duplicated %d times:\n%s", got, content)
	}
	model.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if model.pickerOpen {
		t.Fatal("Esc did not close the picker")
	}
	view = strings.Join(model.setupRouteLines(), "\n")
	for _, gone := range []string{"Local discovery proves", "Availability and credentials", "┌", "Choose scanned model"} {
		if strings.Contains(view, gone) {
			t.Fatalf("models kept noise %q:\n%s", gone, view)
		}
	}

	// Codex has plans only, so its help must not advertise model keys.
	model.setupProviders = []setupflow.Provider{setupflow.ProviderCodex}
	if help := model.setupHelp(); strings.Contains(help, "[m] catalog") || !strings.Contains(help, "[↑↓] plan") {
		t.Fatalf("codex help=%q", help)
	}

	// A single-mode review must collapse the seven identical assignments.
	model.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	cfg, err := agentmodels.Single("openai/gpt-5.6-terra", "high")
	if err != nil {
		t.Fatal(err)
	}
	model.setupMultiPlan = setupflow.MultiPlan{Ready: true, Changed: true, Providers: []setupflow.ProviderPlan{{Provider: setupflow.ProviderOpenCode, Ready: true, Changed: true, Models: &cfg}}}
	model.setupView = setupViewReview
	view = strings.Join(model.setupRouteLines(), "\n")
	if !strings.Contains(view, "all agents  openai/gpt-5.6-terra  effort=high") || strings.Contains(view, "manager openai") || strings.Contains(view, "explore openai") {
		t.Fatalf("single-mode review was not collapsed:\n%s", view)
	}
}

func TestModelPickerAndManualInputAcceptPaste(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.setupView = setupViewPlan
	m.setupCatalog = []SetupCatalogModel{
		{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"high"}},
		{Provider: "acme", Reference: "acme/fast", Variants: []string{"high"}},
	}
	m.setupCatalogAttempted = true
	m.modelChoices[0] = modelChoice{Mode: "single", Rows: [7]agentmodels.Assignment{}}

	m.updateModelChoices(keyPress("m"))
	updated, _ := m.Update(tea.PasteMsg{Content: "acme"})
	m = updated.(Model)
	if providers := m.pickerProviders(); len(providers) != 1 || providers[0].name != "acme" {
		t.Fatalf("pasted provider filter = %+v", providers)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	updated, _ = m.Update(tea.PasteMsg{Content: "fast"})
	m = updated.(Model)
	if models := m.pickerModels(); len(models) != 1 || models[0].Reference != "acme/fast" {
		t.Fatalf("pasted model filter = %+v", models)
	}
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if row := m.modelChoices[0].Rows[0]; row.Model != "acme/fast" {
		t.Fatalf("pasted model not assigned: %+v", row)
	}

	m.updateModelChoices(keyPress("i"))
	if got := m.manualInput.Value(); got != "acme/fast" {
		t.Fatalf("manual editor did not seed the current model: %q", got)
	}
	updated, _ = m.Update(tea.PasteMsg{Content: "anthropic/claude"})
	m = updated.(Model)
	if got := m.manualInput.Value(); !strings.HasSuffix(got, "anthropic/claude") {
		t.Fatalf("manual paste = %q", got)
	}
}

func TestModelPickerTrapsBaseKeysAndFitsSmallTerminals(t *testing.T) {
	catalog := make([]SetupCatalogModel, 0, 12)
	for index := 0; index < 12; index++ {
		catalog = append(catalog, SetupCatalogModel{Provider: fmt.Sprintf("p%02d", index), Reference: fmt.Sprintf("p%02d/model", index)})
	}
	for _, height := range []int{10, 12, 24} {
		m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
		m = updateModel(t, m, tea.WindowSizeMsg{Width: 42, Height: height})
		m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
		m.setupView = setupViewPlan
		m.setupCatalog = append([]SetupCatalogModel(nil), catalog...)
		m.setupCatalogAttempted = true
		m.modelChoices[0] = modelChoice{Mode: "single"}
		m.updateModelChoices(keyPress("m"))

		content := m.View().Content
		assertMaximumWidth(t, content, 42)
		if lines := lipgloss.Height(content); lines > height {
			t.Fatalf("modal overflowed height %d: %d lines\n%s", height, lines, content)
		}

		// Letters type into the modal filter instead of quitting or switching tabs.
		provider := m.choiceProvider()
		updated, _ := m.Update(keyPress("q"))
		m = updated.(Model)
		if !m.pickerOpen || m.pickerFilter.Value() != "q" {
			t.Fatalf("q was not trapped by the picker: open=%t filter=%q", m.pickerOpen, m.pickerFilter.Value())
		}
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
		m = updated.(Model)
		if m.choiceProvider() != provider {
			t.Fatalf("Tab switched provider while the picker was open: %s -> %s", provider, m.choiceProvider())
		}
	}
}

func TestModelChoicesQQuitsWhenPickerClosed(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.setupView = setupViewPlan
	m.modelChoices[0] = modelChoice{Mode: "single"}
	if handled, _ := m.updateModelChoices(keyPress("q")); handled {
		t.Fatal("q must fall through to the global quit handler when the picker is closed")
	}
}

func TestModelChoicesAskModeBeforeConfiguring(t *testing.T) {
	m := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderPi}
	m.setupView = setupViewPlan

	view := strings.Join(m.setupRouteLines(), "\n")
	if !strings.Contains(view, "How should models be assigned?") || !strings.Contains(view, "[1] One model for all agents") || !strings.Contains(view, "[2] One model per agent") {
		t.Fatalf("mode gate missing:\n%s", view)
	}
	if help := m.setupHelp(); !strings.Contains(help, "[↑↓] choose") {
		t.Fatalf("gate help=%q", help)
	}
	if strings.Contains(view, "preview selections") {
		t.Fatalf("gate must not offer the preview action:\n%s", view)
	}
	m.updateModelChoices(keyPress("2"))
	if m.modelChoices[0].Mode != "per-agent" {
		t.Fatalf("mode choice not applied: %+v", m.modelChoices[0])
	}
	view = strings.Join(m.setupRouteLines(), "\n")
	if !strings.Contains(view, "Mode: per-agent") || strings.Contains(view, "How should models be assigned?") {
		t.Fatalf("configuration screen not shown after mode choice:\n%s", view)
	}
	if !strings.Contains(view, "preview selections") {
		t.Fatalf("configuration screen lost its preview action:\n%s", view)
	}

	// Tab advances to Pi, which has not decided yet and shows the gate again.
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if m.choiceProvider() != setupflow.ProviderPi {
		t.Fatalf("tab did not advance provider: %s", m.choiceProvider())
	}
	if view = strings.Join(m.setupRouteLines(), "\n"); !strings.Contains(view, "How should models be assigned?") {
		t.Fatalf("second provider skipped the mode gate:\n%s", view)
	}

	// An installed selection skips the gate.
	m.modelChoices[1] = modelChoice{Mode: "single", Rows: [7]agentmodels.Assignment{{Model: "openai-codex/gpt-5.6-luna", Effort: "off"}}}
	if view = strings.Join(m.setupRouteLines(), "\n"); strings.Contains(view, "How should models be assigned?") || !strings.Contains(view, "Mode: single") {
		t.Fatalf("installed selection did not skip the gate:\n%s", view)
	}

	// q falls through to the global quit handler even at the mode gate.
	m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	m.modelChoiceProvider = 0
	m.modelChoices[0] = modelChoice{}
	if handled, _ := m.updateModelChoices(keyPress("q")); handled {
		t.Fatal("q must fall through at the mode gate")
	}

	// Esc from the gate returns to the provider selection.
	m.updateModelChoices(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.setupView != setupViewProviders {
		t.Fatalf("Esc did not return to providers: %v", m.setupView)
	}
}
