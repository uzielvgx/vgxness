package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

type recordingSetupBackend struct {
	fakeBackend
	planRequests   []SetupRequest
	applyRequests  []SetupRequest
	plan           SetupPlan
	result         SetupResult
	planErr        error
	applyErr       error
	catalog        []SetupCatalogModel
	catalogErr     error
	catalogCalls   []bool
	catalogStarted chan struct{}
	catalogBlock   chan struct{}
}

type recordingMultiSetupBackend struct {
	recordingSetupBackend
	multiPlan     setupflow.MultiPlan
	multiResult   setupflow.MultiResult
	multiRequests []MultiSetupRequest
}

func (backend *recordingMultiSetupBackend) PlanMultiSetup(_ context.Context, request MultiSetupRequest) (setupflow.MultiPlan, error) {
	backend.multiRequests = append(backend.multiRequests, request)
	return backend.multiPlan, nil
}
func (backend *recordingMultiSetupBackend) ApplyMultiSetup(_ context.Context, request MultiSetupRequest) (setupflow.MultiResult, error) {
	backend.multiRequests = append(backend.multiRequests, request)
	return backend.multiResult, nil
}

func TestMultiSetupTogglesProvidersAndBlocksCodexModelEditor(t *testing.T) {
	backend := &recordingMultiSetupBackend{}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model.route = routeSetup
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !model.hasSetupProvider(setupflow.ProviderOpenCode) || model.hasSetupProvider(setupflow.ProviderCodex) {
		t.Fatalf("default providers=%v", model.setupProviders)
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'c', Text: "c"}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'o', Text: "o"}))
	if model.hasSetupProvider(setupflow.ProviderOpenCode) || !model.hasSetupProvider(setupflow.ProviderCodex) {
		t.Fatalf("toggle providers=%v", model.setupProviders)
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'm', Text: "m"}))
	if model.setupModelEditing || !strings.Contains(strings.Join(model.setupRouteLines(), "\n"), "shared plan only") {
		t.Fatalf("codex model state=%t lines=%v", model.setupModelEditing, model.setupRouteLines())
	}
}

func TestMultiSetupApplyPassesOnlyVerifiedOutcomesToRetry(t *testing.T) {
	backend := &recordingMultiSetupBackend{multiResult: setupflow.MultiResult{Providers: []setupflow.ProviderResult{{Provider: setupflow.ProviderOpenCode, Verified: true}, {Provider: setupflow.ProviderCodex}}}}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model.route = routeSetup
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setupMultiPlan = setupflow.MultiPlan{Digest: "digest", Ready: true}
	model.setupPlan = SetupPlan{Digest: "digest", Ready: true}
	model.setupPreviewed = true
	message := model.applySetup()()
	model = updateModel(t, model, message)
	if len(backend.multiRequests) != 1 || len(model.setupMultiResult.Providers) != 2 {
		t.Fatalf("requests=%#v result=%#v", backend.multiRequests, model.setupMultiResult)
	}
	request := model.multiSetupRequest()
	if len(request.Verified) != 1 || !request.Verified[0].Verified {
		t.Fatalf("retry outcomes=%#v", request.Verified)
	}
}

func TestMultiSetupChangedPlanAllowsConfirmationButUnchangedDoesNot(t *testing.T) {
	backend := &recordingMultiSetupBackend{}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model.route = routeSetup
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setupPreviewed = true
	model.setupPreviewRequest = model.setupRequest()
	model.setupMultiPlan = setupflow.MultiPlan{Digest: "changed", Ready: true, Changed: true}
	model.setupPlan = SetupPlan{Digest: "changed", Ready: true}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if !model.setupConfirm {
		t.Fatal("changed multi plan did not open confirmation")
	}
	model.setupConfirm = false
	model.setupMultiPlan.Changed = false
	model.setupPlan.Digest = model.setupMultiPlan.Digest
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if model.setupConfirm || !strings.Contains(strings.Join(model.setupRouteLines(), "\n"), "NO CHANGES") {
		t.Fatal("unchanged multi plan allowed apply")
	}
}

func TestMultiSetupUnchangedPlanAllowsRetryOnlyForUnverifiedSelectedProvider(t *testing.T) {
	model := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	model.route = routeSetup
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setupPreviewed = true
	model.setupPreviewRequest = model.setupRequest()
	model.setupMultiPlan = setupflow.MultiPlan{
		Digest: "unchanged",
		Ready:  true,
		Providers: []setupflow.ProviderPlan{
			{Provider: setupflow.ProviderOpenCode, Ready: true, Installed: true},
		},
	}
	model.setupPlan = SetupPlan{Digest: "unchanged", Ready: true}
	model.setupMultiResult = setupflow.MultiResult{Providers: []setupflow.ProviderResult{{Provider: setupflow.ProviderOpenCode}}}
	if !model.setupApplyAllowed() {
		t.Fatal("unchanged plan with an unverified selected provider did not allow retry")
	}

	model.setupMultiResult.Providers[0].Verified = true
	if model.setupApplyAllowed() {
		t.Fatal("unchanged plan with all selected providers verified allowed apply")
	}
}

func TestMultiSetupFailureRendersSanitizedReasonOutcomesAndSharedRecovery(t *testing.T) {
	model := NewModel(context.Background(), &recordingMultiSetupBackend{}, Options{Workspace: "/workspace"})
	model.route = routeSetup
	model.setupApplyErr = errors.New("bad\nreason\x1b")
	model.setupMultiResult = setupflow.MultiResult{
		Shared:    setupflow.SharedResult{Recovery: "restore\nlauncher"},
		Providers: []setupflow.ProviderResult{{Provider: setupflow.ProviderOpenCode, Verified: true}, {Provider: setupflow.ProviderCodex}},
	}
	view := strings.Join(model.setupRouteLines(), "\n")
	for _, expected := range []string{"Reason: bad\\nreason\\x1b", "Recovery: restore\\nlauncher", "✓ opencode", "✕ codex", "replan/retry"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("missing %q in %s", expected, view)
		}
	}
}

func (backend *recordingSetupBackend) ModelCatalog(ctx context.Context, refresh bool) ([]SetupCatalogModel, error) {
	backend.catalogCalls = append(backend.catalogCalls, refresh)
	if backend.catalogStarted != nil {
		close(backend.catalogStarted)
	}
	if backend.catalogBlock != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-backend.catalogBlock:
		}
	}
	return append([]SetupCatalogModel(nil), backend.catalog...), backend.catalogErr
}

func (backend *recordingSetupBackend) PlanSetup(_ context.Context, request SetupRequest) (SetupPlan, error) {
	backend.planRequests = append(backend.planRequests, request)
	return backend.plan, backend.planErr
}

func (backend *recordingSetupBackend) ApplySetup(_ context.Context, request SetupRequest) (SetupResult, error) {
	backend.applyRequests = append(backend.applyRequests, request)
	return backend.result, backend.applyErr
}

func TestSetupEntryCommandsAreIndependentAndCatalogCancellationIsOwned(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium")}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	_ = model.Init()
	if model.setupCatalogLoading || len(backend.catalogCalls) != 0 {
		t.Fatalf("Init started catalog: loading=%t calls=%v", model.setupCatalogLoading, backend.catalogCalls)
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model, entry := openSetupRoute(t, model)
	message := entry()
	batch, ok := message.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Errorf("Setup entry command=%T len=%d, want two independent commands", message, len(batch))
	} else {
		model = updateModel(t, model, batch[0]())
		if !strings.Contains(model.View().Content, "READY TO APPLY") {
			t.Error("plan did not render independently")
		}
		model = updateModel(t, model, batch[1]())
		if len(backend.catalogCalls) != 1 || backend.catalogCalls[0] {
			t.Fatalf("cached catalog calls=%v", backend.catalogCalls)
		}
	}
	blocked := &recordingSetupBackend{catalogStarted: make(chan struct{}), catalogBlock: make(chan struct{})}
	model = NewModel(context.Background(), blocked, Options{Workspace: "/workspace"})
	catalog := model.loadSetupCatalog(false)
	done := make(chan tea.Msg, 1)
	go func() { done <- catalog() }()
	<-blocked.catalogStarted
	model.cancelSetupOperation()
	select {
	case msg := <-done:
		loaded := msg.(setupCatalogLoadedMsg)
		if !errors.Is(loaded.err, context.Canceled) {
			t.Fatalf("catalog cancellation error=%v", loaded.err)
		}
	case <-time.After(time.Second):
		close(blocked.catalogBlock)
		t.Fatal("catalog command did not stop after leaving Setup")
	}
}

func TestSetupNavigationLoadsInstalledPlanAndRendersControlledWritePreview(t *testing.T) {
	plan := readySetupPlan("high")
	plan.Digest = strings.Repeat("d", 64)
	backend := &recordingSetupBackend{plan: plan}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 26})
	model = updateModel(t, model, setupLoadedMsg{generation: 1, value: SetupStatus{ModelPlan: "high"}})

	model, cmd := openSetupRoute(t, model)
	if cmd == nil {
		t.Fatal("opening Setup did not load a real preview")
	}
	model = runSetupCommands(t, model, cmd)

	if len(backend.planRequests) != 1 || backend.planRequests[0].Workspace != "/workspace" || backend.planRequests[0].Plan != "high" {
		t.Fatalf("plan requests=%+v", backend.planRequests)
	}
	view := model.View().Content
	for _, expected := range []string{"VGXNESS / SETUP / CONTROLLED WRITE", "READY TO APPLY", "selected plan  high", "action  initial install", "digest  " + strings.Repeat("d", 64), "7-STEP PLAN", "Global skills-creator", "Retire legacy provider skill"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Setup preview missing %q:\n%s", expected, view)
		}
	}
	assertMaximumWidth(t, view, 80)
	for _, line := range model.setupRouteLines() {
		if (strings.HasPrefix(line, "action  ") || strings.HasPrefix(line, "digest  ")) && len(line) > 80 {
			t.Fatalf("raw action/digest line width=%d: %q", len(line), line)
		}
	}
}

func TestSetupAcceptsAndPreservesEachModelPlan(t *testing.T) {
	for _, modelPlan := range []string{"low", "medium", "high", "ultra"} {
		t.Run(modelPlan, func(t *testing.T) {
			backend := &recordingSetupBackend{
				plan:   readySetupPlan(modelPlan),
				result: successfulSetupResultForPlan(modelPlan),
			}
			model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
			model = updateModel(t, model, setupLoadedMsg{generation: 1, value: SetupStatus{ModelPlan: modelPlan}})

			model, previewCmd := openSetupRoute(t, model)
			if previewCmd == nil {
				t.Fatal("opening Setup did not load a real preview")
			}
			model = runSetupCommands(t, model, previewCmd)
			if len(backend.planRequests) != 1 || backend.planRequests[0].Plan != modelPlan {
				t.Fatalf("preview requests=%+v want plan=%q", backend.planRequests, modelPlan)
			}
			if !strings.Contains(model.View().Content, "selected plan  "+modelPlan) {
				t.Fatalf("preview did not preserve selected plan %q:\n%s", modelPlan, model.View().Content)
			}

			model = updateModel(t, model, keyPress("a"))
			updated, applyCmd := model.Update(keyPress("y"))
			model = updated.(Model)
			if applyCmd == nil {
				t.Fatal("confirming Setup did not start apply")
			}
			model = updateModel(t, model, applyCmd())
			if len(backend.applyRequests) != 1 || backend.applyRequests[0].Plan != modelPlan {
				t.Fatalf("apply requests=%+v want plan=%q", backend.applyRequests, modelPlan)
			}
			if model.setup.ModelPlan != modelPlan {
				t.Fatalf("applied setup plan=%q want %q", model.setup.ModelPlan, modelPlan)
			}
		})
	}
}

func TestSetupModelProfileEditorCyclesVariantsAndBlocksUnknownMetadata(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium")}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupModelRefs = [3]string{"openai/gpt-5.6-terra", "anthropic/balanced", "acme/frontier"}
	model.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh", "max"}}, {Provider: "anthropic", Reference: "anthropic/balanced", Variants: []string{"thinking"}}, {Provider: "acme", Reference: "acme/frontier", Variants: []string{"none"}}}
	model = updateModel(t, model, keyPress("m"))
	if !strings.Contains(model.View().Content, "MODEL PROFILE EDITOR") || !strings.Contains(model.setupHelp(), "[Tab] variant") {
		t.Fatalf("editor/help missing:\n%s\n%s", model.View().Content, model.setupHelp())
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if model.setupModelVariants[0] != "xhigh" {
		t.Fatalf("variant did not cycle: %+v", model.setupModelVariants)
	}
	model.setupModelRefs[1] = ""
	if model.setupApplyAllowed() || !strings.Contains(model.View().Content, "Each slot needs") {
		t.Fatalf("invalid profile was hidden or applyable:\n%s", model.View().Content)
	}
	model.setupModelRefs[1] = "unknown/balanced"
	updated, previewCmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if previewCmd != nil || !model.setupModelEditing || !strings.Contains(model.modelEditorError(), "metadata is not available") {
		t.Fatalf("unknown profile previewed: cmd=%v editing=%t err=%q", previewCmd, model.setupModelEditing, model.modelEditorError())
	}
	assertMaximumWidth(t, model.View().Content, 80)
}

func TestSetupModelEditorUsesOnlyDiscoveredVariants(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 200, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupModelRefs = [3]string{"openai/gpt-5.6-terra", "acme/unlisted", "openai/gpt-5.6-sol"}
	model.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh", "max"}}, {Provider: "openai", Reference: "openai/gpt-5.6-sol", Variants: []string{}}}

	model = updateModel(t, model, keyPress("m"))
	view := model.View().Content
	for _, expected := range []string{"allowed=xhigh, max", "allowed=provider default"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("editor missing %q:\n%s", expected, view)
		}
	}

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if model.setupModelVariants[0] != "xhigh" {
		t.Fatalf("known model variant=%q, want xhigh", model.setupModelVariants[0])
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if help := model.setupHelp(); strings.Contains(help, "[Tab] variant") || !strings.Contains(help, "variant not available") {
		t.Fatalf("unknown model offered variant selection: %s", help)
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if model.setupModelVariants[1] != "" {
		t.Fatalf("unknown model variant changed to %q", model.setupModelVariants[1])
	}
}

func TestSetupLegacyAssignmentsRequireCatalogMembershipWhenAvailable(t *testing.T) {
	for _, test := range []struct {
		name     string
		schema   int
		unlisted bool
		allowed  bool
	}{
		{name: "v1 listed", schema: 1, allowed: true},
		{name: "v2 listed", schema: 2, allowed: true},
		{name: "v1 unlisted", schema: 1, unlisted: true},
		{name: "v2 unlisted", schema: 2, unlisted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := assignmentSetupPlan(test.schema)
			if test.unlisted {
				plan.ModelAssignments[0].Provider = "acme"
				plan.ModelAssignments[0].Model = "acme/unlisted"
				plan.ModelAssignments[0].Source = "custom"
				plan.ModelAssignments[0].Availability = "unknown"
			}
			backend := &recordingSetupBackend{catalog: []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh"}}}}
			model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
			model.setRoute(routeSetup)
			model = updateModel(t, model, model.loadSetupCatalog(false)())
			model.handleSetupPlanLoaded(setupPlanLoadedMsg{generation: model.setupGeneration, request: model.setupRequest(), value: plan})

			if got := model.setupApplyAllowed(); got != test.allowed {
				t.Fatalf("setupApplyAllowed()=%t, want %t; editor error=%q", got, test.allowed, model.modelEditorError())
			}
		})
	}
}

func TestSetupModelEditorUsesDiscoveredVariantsAndPreservesProviderDefault(t *testing.T) {
	catalogRows := []SetupCatalogModel{
		{Provider: "acme", Reference: "acme/fast", Variants: []string{"xhigh", "max"}},
		{Provider: "acme", Reference: "acme/balanced", Variants: []string{"none", "thinking"}},
		{Provider: "acme", Reference: "acme/default", Variants: []string{}},
	}
	model := NewModel(context.Background(), &recordingSetupBackend{catalog: catalogRows}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupModelRefs = [3]string{"acme/fast", "acme/balanced", "acme/default"}
	model.setupCatalog = append([]SetupCatalogModel(nil), catalogRows...)
	updated, catalog := model.Update(keyPress("m"))
	model = updated.(Model)
	if catalog == nil || !model.setupCatalogLoading {
		t.Fatal("editor did not start a catalog load")
	}
	model = updateModel(t, model, catalog())
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if model.setupModelVariants[0] != "xhigh" || !strings.Contains(model.View().Content, "variant=xhigh") {
		t.Fatalf("catalog variant was not selected/displayed: variants=%+v\n%s", model.setupModelVariants, model.View().Content)
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if model.setupModelVariants[1] != "none" {
		t.Fatalf("catalog order was not used: variants=%+v", model.setupModelVariants)
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	before := model.setupModelVariants[2]
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if model.setupModelVariants[2] != before || !strings.Contains(model.setupHelp(), "provider default") {
		t.Fatalf("provider default offered variant cycling: variants=%+v help=%q", model.setupModelVariants, model.setupHelp())
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	request := model.setupRequest()
	if !request.ModelVariantsSpecified || request.ModelEfficientVariant != "xhigh" || request.ModelBalancedVariant != "none" || request.ModelFrontierVariant != "" {
		t.Fatalf("variants did not transport exactly: %+v", request)
	}
	assertMaximumWidth(t, model.View().Content, 80)
}

func TestSetupExactEditorAcceptsLegacyResolvedVariant(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	plan := assignmentSetupPlan(3)
	for index := range *plan.ModelAssignments {
		plan.ModelAssignments[index].VariantSpecified = false
	}
	model.seedSetupAssignments(plan)
	model.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh"}}}
	if err := model.modelEditorError(); err != "" {
		t.Fatalf("legacy resolved variants rejected: %q", err)
	}
}

func TestSetupLegacyAssignmentSeedOmitsDerivedVariants(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	plan := assignmentSetupPlan(3)
	for index := range *plan.ModelAssignments {
		plan.ModelAssignments[index].Variant, plan.ModelAssignments[index].VariantSpecified = "xhigh", false
	}
	model.seedSetupAssignments(plan)
	model.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh"}}}
	request := model.setupRequest()
	if request.ModelAssignments == nil || model.modelEditorError() != "" {
		t.Fatalf("legacy assignments were not retained: request=%+v error=%q", request, model.modelEditorError())
	}
	for _, row := range *request.ModelAssignments {
		if row.Variant != "" || row.VariantSpecified {
			t.Fatalf("derived variant reached request: %+v", row)
		}
	}
}

func TestSetupV2AssignmentReentryPreservesExplicitVariant(t *testing.T) {
	plan := assignmentSetupPlan(2)
	for index := range *plan.ModelAssignments {
		plan.ModelAssignments[index].VariantSpecified = false
	}
	plan.ModelAssignments[0].Variant, plan.ModelAssignments[0].VariantSpecified = "max", true

	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model.setup = SetupStatus{ModelSchemaVersion: 2, ModelAssignments: plan.ModelAssignments}
	model.setRoute(routeSetup)

	if row := model.setupAssignmentRows[0]; row.Variant != "max" || !row.VariantSpecified {
		t.Fatalf("re-entry lost explicit v2 variant: %+v", row)
	}
}

func TestSetupProfileRequestPreservesSlotEffortsWithVariants(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model.setupOverrides = true
	model.setupModelRefs = [3]string{"openai/fast", "anthropic/balanced", "acme/frontier"}
	model.setupModelEfforts = [3]string{"low", "high", "ultra"}
	model.setupModelVariants = [3]string{"xhigh", "max", ""}
	request := model.setupRequest()
	if request.ModelEfficientEffort != "low" || request.ModelBalancedEffort != "high" || request.ModelFrontierEffort != "ultra" || !request.ModelVariantsSpecified {
		t.Fatalf("profile request lost slot efforts: %+v", request)
	}
}

func TestSetupUnknownMetadataClearsEditedEffortsAndBlocksPreview(t *testing.T) {
	t.Run("profile reference change", func(t *testing.T) {
		model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
		model.setRoute(routeSetup)
		model.setupPlan = readySetupPlan("medium")
		model.setupModelRefs = [3]string{"openai/gpt-5.6-terra", "anthropic/balanced", "openai/gpt-5.6-sol"}
		model.setupModelVariants = [3]string{"xhigh", "thinking", "none"}
		model.setupCatalog = []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh"}}, {Provider: "anthropic", Reference: "anthropic/balanced", Variants: []string{"thinking"}}, {Provider: "openai", Reference: "openai/gpt-5.6-sol", Variants: []string{"none"}}}
		model = updateModel(t, model, keyPress("m"))
		model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
		model = updateModel(t, model, keyPress("x"))
		if model.setupModelVariants[1] != "" || !strings.Contains(model.modelEditorError(), "metadata is not available") {
			t.Fatalf("unknown edited profile retained variant or remained valid: variants=%+v err=%q", model.setupModelVariants, model.modelEditorError())
		}
		updated, preview := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		model = updated.(Model)
		if preview != nil || !model.setupModelEditing {
			t.Fatalf("unknown profile started preview: cmd=%v editing=%t", preview, model.setupModelEditing)
		}
	})

	t.Run("assignment selection", func(t *testing.T) {
		model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
		model.setRoute(routeSetup)
		model.seedSetupAssignments(assignmentSetupPlan(2))
		model.setupCatalog = []SetupCatalogModel{
			{Provider: "openai", Reference: "openai/gpt-5.6-terra"},
			{Provider: "acme", Reference: "acme/unlisted"},
		}
		model = updateModel(t, model, keyPress("m"))
		model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
		if row := model.setupAssignmentRows[0]; row.Reference != "acme/unlisted" || row.Variant != "" || model.modelEditorError() == "" {
			t.Fatalf("unknown assignment retained variant or remained valid: row=%+v err=%q", row, model.modelEditorError())
		}
		updated, preview := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		model = updated.(Model)
		if preview != nil || !model.setupModelEditing {
			t.Fatalf("unknown assignment started preview: cmd=%v editing=%t", preview, model.setupModelEditing)
		}
	})

	t.Run("existing assignment read", func(t *testing.T) {
		plan := assignmentSetupPlan(3)
		plan.ModelAssignments[0].Provider = "acme"
		plan.ModelAssignments[0].Model = "acme/existing"
		plan.ModelAssignments[0].RequestedEffort = "high"
		plan.ModelAssignments[0].Source = "custom"
		plan.ModelAssignments[0].Availability = "unknown"
		model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
		model.setRoute(routeSetup)
		model.handleSetupPlanLoaded(setupPlanLoadedMsg{generation: model.setupGeneration, request: model.setupRequest(), value: plan})
		if row := model.setupAssignmentRows[0]; row.RequestedEffort != "high" || model.setupApplyAllowed() || model.modelEditorError() == "" {
			t.Fatalf("existing unknown assignment was changed or confirmed: row=%+v allowed=%t err=%q", row, model.setupApplyAllowed(), model.modelEditorError())
		}
	})
}

func TestSetupSameProviderUnlistedRefsBlockPreviewAndApply(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupModelRefs = [3]string{"acme/efficient", "acme/balanced", "acme/frontier"}
	model.setupModelEfforts = [3]string{}
	model.setupOverrides = true
	model.setupPreviewed = true
	model.setupPreviewRequest = model.setupRequest()

	if model.setupApplyAllowed() || !strings.Contains(model.modelEditorError(), "metadata is not available") {
		t.Fatalf("same-provider unlisted refs were applyable: allowed=%t err=%q", model.setupApplyAllowed(), model.modelEditorError())
	}
	model = updateModel(t, model, keyPress("m"))
	updated, preview := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if preview != nil || !model.setupModelEditing {
		t.Fatalf("same-provider unlisted refs started preview: cmd=%v editing=%t", preview, model.setupModelEditing)
	}
}

func TestSetupAssignmentMatrixCatalogNavigationPromotionAndFreshPreview(t *testing.T) {
	want := []setupAgentIdentity{
		{"agents/vgxness-manager.md", "manager", "manager", "core"},
		{"agents/explore.md", "explore", "research", "core"},
		{"agents/general.md", "general", "implementation", "core"},
		{"agents/vgxness-verifier.md", "verifier", "verification", "core"},
		{"agents/vgxness-care-reviewer.md", "CARE reviewer", "review", "review"},
		{"agents/vgxness-care-specialist.md", "CARE specialist", "review", "review"},
		{"agents/vgxness-care-challenger.md", "CARE challenger", "review", "review"},
		{"agents/vgxness-sdd-research.md", "sdd-research", "research", "sdd"},
		{"agents/vgxness-sdd-proposal.md", "sdd-proposal", "proposal", "sdd"},
		{"agents/vgxness-sdd-spec.md", "sdd-spec", "spec", "sdd"},
		{"agents/vgxness-sdd-design.md", "sdd-design", "design", "sdd"},
		{"agents/vgxness-sdd-tasks.md", "sdd-tasks", "tasks", "sdd"},
		{"agents/vgxness-sdd-apply.md", "sdd-apply", "apply", "sdd"},
	}
	if got := setupAgentRows[:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("current setup identities=%+v want=%+v", got, want)
	}
	backend := &recordingSetupBackend{catalog: []SetupCatalogModel{
		{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh", "max"}, Source: "catalog", Availability: "catalog-known"},
		{Provider: "openai", Reference: "openai/gpt-5.6-sol", Variants: []string{"none", "thinking"}, Source: "catalog", Availability: "catalog-known"},
	}}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.handleSetupPlanLoaded(setupPlanLoadedMsg{generation: model.setupGeneration, request: model.setupRequest(), value: assignmentSetupPlan(2)})
	if request := model.setupRequest(); request.ModelAssignments != nil {
		t.Fatalf("legacy plan promoted before edit: %+v", request)
	}
	model.setupCatalog = append([]SetupCatalogModel(nil), backend.catalog...)
	model = updateModel(t, model, keyPress("m"))
	model.markAssignmentEdit()
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if !model.setupPreviewed || model.setupPlan.Digest != "fixture-digest" || !model.setupApplyAllowed() {
		t.Fatal("Esc lost matching preview")
	}
	model = updateModel(t, model, keyPress("m"))
	for _, expected := range []string{"AGENT ASSIGNMENT MATRIX", "▸ manager", "core/manager", "sdd/apply", "[↑↓/j/k]", "[←→]", "[[/]]", "[Enter]", "[Esc]", "[q]"} {
		if !strings.Contains(model.View().Content, expected) {
			t.Fatalf("matrix missing %q:\n%s", expected, model.View().Content)
		}
	}
	assertMaximumWidth(t, strings.Join(model.modelAssignmentLines(), "\n"), 80)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	model = updateModel(t, model, keyPress("]"))
	if row := model.setupAssignmentRows[1]; model.setupModelSlot != 1 || !model.setupAssignmentsExact || row.Reference != "openai/gpt-5.6-sol" || row.Source != "catalog" || row.Availability != "catalog-known" || row.Variant != "thinking" {
		t.Fatalf("matrix edit state slot=%d exact=%t row=%+v", model.setupModelSlot, model.setupAssignmentsExact, model.setupAssignmentRows[1])
	}
	if model.setupPreviewed || model.setupPlan.Digest != "" {
		t.Fatalf("edit retained stale preview: previewed=%t digest=%q", model.setupPreviewed, model.setupPlan.Digest)
	}
	updated, preview := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if preview == nil || model.setupModelEditing {
		t.Fatal("Enter did not leave matrix and request a preview")
	}
	request := model.setupRequest()
	if request.ModelAssignments == nil || len(*request.ModelAssignments) != SetupModelAssignmentCount || (*request.ModelAssignments)[1].ArtifactKey != setupAgentRows[1].ArtifactKey {
		t.Fatalf("promoted request=%+v", request)
	}
	copyRows := *request.ModelAssignments
	model.setupAssignmentRows[1].Reference = "changed/later"
	if copyRows[1].Reference != "openai/gpt-5.6-sol" {
		t.Fatalf("request aliases editor rows: %+v", copyRows[1])
	}
}

func TestSetupPlanSelectionIsHiddenForExactAssignments(t *testing.T) {
	for _, schema := range []int{1, 2} {
		t.Run(fmt.Sprintf("v%d remains preset", schema), func(t *testing.T) {
			backend := &recordingSetupBackend{plan: assignmentSetupPlan(schema)}
			model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
			model.setRoute(routeSetup)
			model.setupSelected = "medium"
			model.handleSetupPlanLoaded(setupPlanLoadedMsg{generation: model.setupGeneration, request: model.setupRequest(), value: backend.plan})
			updated, cmd := model.Update(keyPress("l"))
			model = updated.(Model)
			model = updateModel(t, model, cmd())
			if len(backend.planRequests) != 1 || backend.planRequests[0].Plan != "high" || model.setupSelected != "high" {
				t.Fatalf("legacy preset selection did not preview high: requests=%+v selected=%q", backend.planRequests, model.setupSelected)
			}
		})
	}

	backend := &recordingSetupBackend{plan: assignmentSetupPlan(3), catalog: []SetupCatalogModel{{Provider: "openai", Reference: "openai/gpt-5.6-terra", Variants: []string{"xhigh"}}, {Provider: "openai", Reference: "openai/gpt-5.6-sol", Variants: []string{"none"}}}}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupSelected = "medium"
	model.setupCatalog = append([]SetupCatalogModel(nil), backend.catalog...)
	model.handleSetupPlanLoaded(setupPlanLoadedMsg{generation: model.setupGeneration, request: model.setupRequest(), value: backend.plan})
	request, selected, digest := model.setupRequest(), model.setupSelected, model.setupPlan.Digest
	updated, cmd := model.Update(keyPress("l"))
	model = updated.(Model)
	if cmd != nil || !setupRequestsEqual(request, model.setupRequest()) || selected != model.setupSelected || digest != model.setupPlan.Digest || len(backend.planRequests) != 0 {
		t.Fatalf("exact preset selection changed state: cmd=%v request=%+v selected=%q digest=%q calls=%d", cmd, model.setupRequest(), model.setupSelected, model.setupPlan.Digest, len(backend.planRequests))
	}
	if view, help := model.View().Content, model.setupHelp(); strings.Contains(view, "selected plan") || !strings.Contains(view, "per-agent assignments") || strings.Contains(help, "[h/l]") || !strings.Contains(help, "[m]") || !strings.Contains(help, "[a]") || !strings.Contains(help, "[r]") {
		t.Fatalf("exact setup UI=%q help=%q", view, help)
	}
	assertMaximumWidth(t, model.View().Content, 80)
	model.setupPlan = assignmentSetupPlan(3)
	model.setupPlan.SelfInstallState, model.setupPlan.IntegrationState, model.setupPlan.SkillsState = "installed", "installed", "installed"
	model.setupPreviewed, model.setupPreviewRequest = true, model.setupRequest()
	if help := model.setupHelp(); !strings.Contains(help, "[m] edit assignments") || !strings.Contains(help, "[r]") || !strings.Contains(help, "Recovery") || !strings.Contains(help, "[j/k]") || strings.Contains(help, "[a]") || strings.Contains(help, "[h/l]") {
		t.Fatalf("exact no-change help=%q", help)
	}

	model.seedSetupAssignments(assignmentSetupPlan(2))
	model.setupCatalog = backend.catalog
	model = updateModel(t, model, keyPress("m"))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	updated, preview := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if !model.setupAssignmentsExact {
		t.Fatalf("matrix edit did not promote legacy assignments: editing=%t slot=%d row=%+v", model.setupModelEditing, model.setupModelSlot, model.setupAssignmentRows[0])
	}
	if preview == nil {
		t.Fatal("matrix edit did not request a fresh preview")
	}
	if _, cmd = model.Update(keyPress("l")); cmd != nil {
		t.Fatal("promoted assignments allowed a preset preview")
	}
}

func TestSetupAssignmentJourneyPreviewApplyStatusAndReentry(t *testing.T) {
	const discovered = "openai/gpt-5.6-terra"
	backend := &recordingSetupBackend{
		plan:    assignmentSetupPlan(2),
		catalog: []SetupCatalogModel{{Provider: "openai", Reference: discovered, Variants: []string{"xhigh", "max"}, Source: "catalog", Availability: "catalog-known"}},
	}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 120})
	model.setRoute(routeSetup)
	model = updateModel(t, model, model.loadSetupPlan()())
	model = updateModel(t, model, model.loadSetupCatalog(false)())

	model = updateModel(t, model, keyPress("m"))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	model = updateModel(t, model, keyPress("]"))
	request := model.setupRequest()
	if request.ModelAssignments == nil || len(*request.ModelAssignments) != SetupModelAssignmentCount {
		t.Fatalf("edit did not create exact assignment request: %+v", request)
	}
	if row := request.ModelAssignments[0]; row.Reference != discovered || row.Variant != "max" || !row.VariantSpecified || row.Source != "catalog" || row.Availability != "catalog-known" {
		t.Fatalf("catalog model metadata was lost: %+v", row)
	}

	preview := assignmentSetupPlan(3)
	preview.Digest = "journey-digest"
	for index, row := range *request.ModelAssignments {
		preview.ModelAssignments[index].Provider = row.Provider
		preview.ModelAssignments[index].Model = row.Reference
		preview.ModelAssignments[index].RequestedEffort = row.RequestedEffort
		preview.ModelAssignments[index].Effort = row.RequestedEffort
		preview.ModelAssignments[index].Variant = row.Variant
		preview.ModelAssignments[index].VariantSpecified = row.VariantSpecified
		preview.ModelAssignments[index].Source = row.Source
		preview.ModelAssignments[index].Availability = row.Availability
	}
	backend.plan = preview
	updated, previewCmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	model = updateModel(t, model, previewCmd())
	model = updateModel(t, model, keyPress("a"))
	confirmation := model.View().Content
	if !strings.Contains(confirmation, "digest  journey-digest") || strings.Count(confirmation, "model=") != SetupModelAssignmentCount || strings.Count(confirmation, "variant=") != SetupModelAssignmentCount || strings.Count(confirmation, "source=") != SetupModelAssignmentCount || strings.Count(confirmation, "availability=") != SetupModelAssignmentCount {
		t.Fatalf("confirmation did not bind the exact preview:\n%s", confirmation)
	}

	backend.result = SetupResult{Plan: preview, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed", ArtifactCount: 18, HandshakeOK: true, HandshakeStatus: "healthy", Changed: true}
	updated, applyCmd := model.Update(keyPress("y"))
	model = updated.(Model)
	model = updateModel(t, model, applyCmd())
	if len(backend.applyRequests) != 1 || backend.applyRequests[0].ExpectedPlanDigest != "journey-digest" || !setupRequestsEqual(backend.applyRequests[0], request) {
		t.Fatalf("apply was not bound to preview: %+v", backend.applyRequests)
	}
	if view := model.View().Content; !strings.Contains(view, "requested=medium") || !strings.Contains(view, "effective=medium") || !strings.Contains(view, "variant=max") {
		t.Fatalf("success hid requested/effective assignment:\n%s", view)
	}

	installed := *model.setup.ModelAssignments
	model.setRoute(routeOverview)
	model.setRoute(routeSetup)
	reentry := model.setupRequest()
	if reentry.ModelAssignments == nil || !model.setupAssignmentsSeeded || !model.setupAssignmentsExact {
		t.Fatalf("re-entry hid installed v3 assignments: request=%+v seeded=%t exact=%t", reentry, model.setupAssignmentsSeeded, model.setupAssignmentsExact)
	}
	for index, row := range installed {
		if model.setupAssignmentRows[index].Reference != row.Model || model.setupAssignmentRows[index].RequestedEffort != row.RequestedEffort || model.setupAssignmentRows[index].Variant != row.Variant || model.setupAssignmentRows[index].VariantSpecified != row.VariantSpecified || (*reentry.ModelAssignments)[index] != model.setupAssignmentRows[index] {
			t.Fatalf("re-entry changed assignment %d: row=%+v installed=%+v", index, model.setupAssignmentRows[index], row)
		}
	}
}

func TestSetupAssignmentMatrixCatalogLoadingRefreshEmptyErrorAndStaleSafety(t *testing.T) {
	backend := &recordingSetupBackend{catalog: []SetupCatalogModel{{Provider: "acme", Reference: "acme/model", Source: "catalog", Availability: "catalog-known"}}}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	initial := model.loadSetupCatalog(false)
	model = updateModel(t, model, initial())
	if len(backend.catalogCalls) != 1 || backend.catalogCalls[0] || len(model.setupCatalog) != 1 {
		t.Fatalf("cached catalog call=%v rows=%+v", backend.catalogCalls, model.setupCatalog)
	}
	model.setRoute(routeSetup)
	model.seedSetupAssignments(assignmentSetupPlan(2))
	model.setupModelEditing = true
	updated, refresh := model.Update(keyPress("r"))
	model = updated.(Model)
	if !model.setupCatalogLoading || model.setupAssignmentsExact {
		t.Fatal("refresh became an edit or hid loading state")
	}
	model = updateModel(t, model, refresh())
	if len(backend.catalogCalls) != 2 || !backend.catalogCalls[1] {
		t.Fatalf("refresh calls=%v", backend.catalogCalls)
	}
	rows := append([]SetupCatalogModel(nil), model.setupCatalog...)
	model = updateModel(t, model, setupCatalogLoadedMsg{generation: model.setupCatalogGeneration - 1, rows: []SetupCatalogModel{{Reference: "stale/model"}}})
	if !reflect.DeepEqual(model.setupCatalog, rows) {
		t.Fatalf("stale catalog replaced rows: %+v", model.setupCatalog)
	}
	model.setupCatalog = nil
	if view := strings.Join(model.modelAssignmentLines(), "\n"); !strings.Contains(view, "No locally cached models") {
		t.Fatalf("empty state missing: %s", view)
	}
	model.setupCatalogErr = errors.New("secret backend detail")
	if view := strings.Join(model.modelAssignmentLines(), "\n"); !strings.Contains(view, "Model catalog unavailable") || strings.Contains(view, "secret") || !strings.Contains(view, "[r]") {
		t.Fatalf("unsafe catalog error state: %s", view)
	}
	model.setupCatalog = rows
	model.handleSetupCatalogLoaded(setupCatalogLoadedMsg{generation: model.setupCatalogGeneration, err: errors.New("unavailable")})
	if !reflect.DeepEqual(model.setupCatalog, rows) || model.setupAssignmentRows[0].Reference == "" {
		t.Fatalf("catalog failure discarded seeds: catalog=%+v assignment=%+v", model.setupCatalog, model.setupAssignmentRows[0])
	}
}

func TestSetupAssignmentIdentitySeedingAndLongExactRendering(t *testing.T) {
	plan := assignmentSetupPlan(3)
	longRef := "p/" + strings.Repeat("a", 256) + "/" + strings.Repeat("b", 253)
	plan.ModelAssignments[0].Model, plan.ModelAssignments[0].Provider, plan.ModelAssignments[0].RequestedEffort = longRef, "p", "ultra"
	plan.ModelAssignments[0].Variant, plan.ModelAssignments[0].VariantSpecified = "xhigh", true
	plan.ModelAssignments[0], plan.ModelAssignments[SetupModelAssignmentCount-1] = plan.ModelAssignments[SetupModelAssignmentCount-1], plan.ModelAssignments[0]
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.seedSetupAssignments(plan)
	if model.setupAssignmentRows[0].ArtifactKey != setupAgentRows[0].ArtifactKey || model.setupAssignmentRows[0].Reference != longRef {
		t.Fatalf("shuffled rows mislabeled: first=%+v", model.setupAssignmentRows[0])
	}
	before := model.setupAssignmentRows
	invalid := plan
	rows := *plan.ModelAssignments
	rows[1] = rows[0]
	invalid.ModelAssignments = &rows
	model.seedSetupAssignments(invalid)
	if model.setupAssignmentRows != before {
		t.Fatal("duplicate identity set partially replaced valid rows")
	}
	model.setupPlan, model.setupPreviewed, model.setupPreviewRequest, model.setupConfirm = plan, true, model.setupRequest(), true
	if model.modelProfileChanged() || !strings.Contains(strings.Join(model.setupRouteLines(), "\n"), "variant=xhigh") || !strings.Contains(model.setupHelp(), "[j/k] scroll") {
		t.Fatal("exact defaults were hidden or reported edited")
	}
	for _, pressed := range []string{"m", "r", "l"} {
		updated, cmd := model.Update(keyPress(pressed))
		model = updated.(Model)
		if cmd != nil || model.setupModelEditing || !model.setupConfirm || !setupRequestsEqual(model.setupPreviewRequest, model.setupRequest()) {
			t.Fatalf("confirmation key %q escaped modal state", pressed)
		}
	}
	model.markAssignmentEdit()
	if !model.modelProfileChanged() {
		t.Fatal("exact row edit was not reported")
	}
	model.setupSucceeded, model.setupResult = true, SetupResult{Plan: plan}
	model.setupConfirm = false
	lines := model.setupRouteLines()
	joined, exact := strings.Join(lines, "\n"), false
	for _, line := range lines {
		if line == "EXACT AGENT ASSIGNMENTS" {
			exact = true
			continue
		}
		if exact && strings.HasPrefix(line, "Restart OpenCode") {
			break
		}
		if exact && len(line) > 80 {
			t.Fatalf("exact line width=%d: %q", len(line), line)
		}
	}
	if !strings.Contains(joined, strings.Repeat("a", 64)) || !strings.Contains(joined, strings.Repeat("b", 64)) || !strings.Contains(joined, "requested=ultra") {
		t.Fatal("wrapped exact profile hid model bytes or effort")
	}
}

func TestSetupAssignmentMatrixKeepsTruncatedVariantVisibleAt80Columns(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.seedSetupAssignments(assignmentSetupPlan(3))
	model.setupAssignmentRows[0].Reference = "provider/" + strings.Repeat("reference", 20)
	model.setupAssignmentRows[0].Variant = "thinking-with-extra-details"
	lines := model.modelAssignmentLines()
	joined := strings.Join(lines, "\n")
	assertMaximumWidth(t, joined, 80)
	if !strings.Contains(joined, "· thinking-wi…") {
		t.Fatalf("truncated variant suffix was hidden:\n%s", joined)
	}
}

func TestSetupExactAssignmentConfirmationAndOutcomeDiscloseResolution(t *testing.T) {
	requestRows := [SetupModelAssignmentCount]SetupModelAssignmentRequest{}
	plan := assignmentSetupPlan(3)
	for index, identity := range setupAgentRows {
		requestRows[index] = SetupModelAssignmentRequest{
			ArtifactKey: identity.ArtifactKey, Provider: "acme", Reference: "acme/model",
			RequestedEffort: "ultra", Variant: "xhigh", VariantSpecified: true, Source: "custom", Availability: "unknown",
		}
		plan.ModelAssignments[index].RequestedEffort = "ultra"
		plan.ModelAssignments[index].Effort = "high"
		plan.ModelAssignments[index].Variant = "high"
	}
	plan.ModelAssignments[0].Degraded = true
	plan.ModelAssignments[0].DegradationReason = "unsupported\nreason"

	confirmation := strings.Join(setupAssignmentRequestProfile(requestRows, 80), "\n")
	for _, expected := range []string{"variant=xhigh", "source=custom", "availability=unknown"} {
		if !strings.Contains(confirmation, expected) {
			t.Fatalf("confirmation missing %q:\n%s", expected, confirmation)
		}
	}
	outcome := strings.Join(setupPlanModelProfile(plan, 80), "\n")
	for _, expected := range []string{"requested=ultra", "effective=high", "variant=high", "degraded=unsupported\\nreason"} {
		if !strings.Contains(outcome, expected) {
			t.Fatalf("outcome missing %q:\n%s", expected, outcome)
		}
	}
	assertMaximumWidth(t, confirmation+"\n"+outcome, 80)
}

func TestSetupModelEditorBlocksUnknownMetadataBeforePreview(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium")}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupModelRefs = [3]string{"openai/fast", "anthropic/balanced", "acme/frontier"}
	model.setupModelEfforts = [3]string{"low", "high", "ultra"}
	model = updateModel(t, model, keyPress("m"))
	updated, previewCmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if previewCmd != nil || !model.setupModelEditing || model.setupPlanLoading || model.setupApplyAllowed() || !strings.Contains(model.modelEditorError(), "metadata is not available") || len(backend.planRequests) != 0 {
		t.Fatalf("unknown metadata previewed: cmd=%v editing=%t loading=%t allowed=%t err=%q requests=%+v", previewCmd, model.setupModelEditing, model.setupPlanLoading, model.setupApplyAllowed(), model.modelEditorError(), backend.planRequests)
	}
}

func TestSetupApplyUsesConfirmedPreviewDigest(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium"), result: successfulSetupResult()}
	backend.plan.Digest = "confirmed-digest"
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model, cmd := openSetupRoute(t, model)
	model = runSetupCommands(t, model, cmd)
	if !model.setupApplyAllowed() {
		t.Fatal("digest-bearing preview was not applyable")
	}
	model = updateModel(t, model, keyPress("a"))
	updated, apply := model.Update(keyPress("y"))
	model = updated.(Model)
	model = updateModel(t, model, apply())
	if len(backend.applyRequests) != 1 || backend.applyRequests[0].ExpectedPlanDigest != "confirmed-digest" {
		t.Fatalf("apply requests=%+v", backend.applyRequests)
	}
}

func TestSetupModelEditorUnknownMetadataEscapePreservesExistingInput(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium"), planErr: errors.New("preview failed")}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupModelRefs = [3]string{"openai/fast", "anthropic/balanced", "acme/frontier"}
	model.setupModelEfforts = [3]string{"low", "high", "ultra"}
	model = updateModel(t, model, keyPress("m"))
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if cmd != nil || model.setupModelRefs[1] != "anthropic/balanced" || model.setupModelEfforts[2] != "ultra" || !model.setupModelEditing || model.setupApplyAllowed() {
		t.Fatalf("unknown metadata lost or applied input: cmd=%v refs=%+v efforts=%+v", cmd, model.setupModelRefs, model.setupModelEfforts)
	}
	model.setupModelRefs[0] = "changed/model"
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if model.setupModelRefs[0] != "openai/fast" || model.setupModelEditing {
		t.Fatalf("Esc did not restore editor snapshot: refs=%+v editing=%t", model.setupModelRefs, model.setupModelEditing)
	}
}

func TestSetupRejectsStaleOrMismatchedPreviewResponse(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model.setRoute(routeSetup)
	model.setupModelRefs = [3]string{"openai/fast", "anthropic/balanced", "acme/frontier"}
	model.setupModelEfforts = [3]string{"low", "high", "ultra"}
	model.setupOverrides = true
	model.setupGeneration = 3
	request := model.setupRequest()
	model.handleSetupPlanLoaded(setupPlanLoadedMsg{generation: 3, request: SetupRequest{Workspace: "/workspace", Plan: "medium", ModelEfficient: "other/fast"}, value: readySetupPlan("low")})
	if model.setupPlan.ModelPlan != "" || model.setupModelRefs != [3]string{"openai/fast", "anthropic/balanced", "acme/frontier"} {
		t.Fatalf("mismatched response replaced newer input: plan=%+v refs=%+v request=%+v", model.setupPlan, model.setupModelRefs, request)
	}
}

func TestSetupScrollSynchronizesCurrentRouteBeforeViewportUpdate(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 14})
	model.setRoute(routeSetup)
	plan := readySetupPlan("medium")
	for index := 8; index <= 24; index++ {
		plan.Steps = append(plan.Steps, SetupStep{Number: index, Title: fmt.Sprintf("later-step-%02d", index)})
	}
	model.setupPlan = plan
	model.setupPreviewed = true
	model.setupPreviewRequest = model.setupRequest()
	if strings.Contains(model.View().Content, "later-step-24") {
		t.Fatal("test plan did not clip initial route")
	}
	for range 31 {
		model = updateModel(t, model, keyPress("j"))
	}
	if !strings.Contains(model.View().Content, "later-step-24") {
		t.Fatalf("scroll did not reveal current route content:\n%s", model.View().Content)
	}
}

func TestSetupModelEditorRejectsUnsafeReferencesInline(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupModelRefs = [3]string{"openai/fast", "anthropic/bad?ref", "acme/frontier"}
	model.setupModelEfforts = [3]string{"low", "high", "ultra"}
	model = updateModel(t, model, keyPress("m"))
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(Model)
	if cmd != nil || !model.setupModelEditing || model.setupModelRefs[1] != "anthropic/bad?ref" || !strings.Contains(model.View().Content, "provider/model") {
		t.Fatalf("unsafe ref previewed or was cleared: cmd=%v refs=%+v view=%s", cmd, model.setupModelRefs, model.View().Content)
	}
}

func TestSetupModelReferenceSyntaxMatchesSafeContract(t *testing.T) {
	for _, test := range []struct {
		reference string
		valid     bool
	}{
		{"openai/gpt-5.6", true}, {"provider/nested/model", true}, {"openai/a:b@c+d", true}, {"@openai/model", false}, {"openai/bad?ref", false}, {"openai/bad#ref", false}, {"openai/bad=ref", false}, {`openai/bad\ref`, false}, {"openai/bad ref", false}, {"p/" + strings.Repeat("a", 256) + "/" + strings.Repeat("b", 253), true}, {"p/" + strings.Repeat("a", 256) + "/" + strings.Repeat("b", 254), false}, {"p/" + strings.Repeat("a", 257), false},
	} {
		if got := validSetupModelReference(test.reference); got != test.valid {
			t.Fatalf("validSetupModelReference(%q)=%t want %t", test.reference, got, test.valid)
		}
	}
}

func TestSetupRequiresExplicitConfirmationAndSelectedPlanReachesApply(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium"), result: successfulSetupResult()}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model, cmd := openSetupRoute(t, model)
	model = runSetupCommands(t, model, cmd)

	updated, planCmd := model.Update(keyPress("l"))
	model = updated.(Model)
	if planCmd == nil {
		t.Fatal("changing plan did not reload the real preview")
	}
	model = updateModel(t, model, planCmd())
	if len(backend.applyRequests) != 0 {
		t.Fatal("changing plan applied Setup")
	}
	model = updateModel(t, model, keyPress("a"))
	if len(backend.applyRequests) != 0 || !strings.Contains(model.View().Content, "CONFIRM INITIAL INSTALL") {
		t.Fatalf("apply started before y confirmation: requests=%+v", backend.applyRequests)
	}
	model = updateModel(t, model, keyPress("n"))
	if len(backend.applyRequests) != 0 || strings.Contains(model.View().Content, "CONFIRM INITIAL INSTALL") {
		t.Fatal("n did not cancel confirmation without writes")
	}
	model = updateModel(t, model, keyPress("a"))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if len(backend.applyRequests) != 0 || strings.Contains(model.View().Content, "CONFIRM INITIAL INSTALL") {
		t.Fatal("Esc did not cancel confirmation without writes")
	}

	model = updateModel(t, model, keyPress("a"))
	updated, applyCmd := model.Update(keyPress("y"))
	model = updated.(Model)
	if applyCmd == nil || len(backend.applyRequests) != 0 || !strings.Contains(model.View().Content, "APPLYING SETUP · VERIFIED PLAN") {
		t.Fatalf("confirmed apply state invalid: cmd=%v requests=%+v", applyCmd, backend.applyRequests)
	}
	model = updateModel(t, model, applyCmd())
	if len(backend.applyRequests) != 1 || backend.applyRequests[0].Plan != "high" {
		t.Fatalf("apply requests=%+v", backend.applyRequests)
	}
}

func TestSetupRendersSuccessAndFailureRecovery(t *testing.T) {
	backend := &recordingSetupBackend{}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setup = SetupStatus{Ready: false, IntegrationState: "absent", ModelPlan: "medium"}
	model.setRoute(routeSetup)
	staleStatus := model.setupStatusGeneration
	result := successfulSetupResult()
	result.Plan = assignmentSetupPlan(3)
	result.Plan.ModelPlan, result.Plan.ModelAssignments[0].Model = "high", "acme/persisted"
	backend.result = result
	model = updateModel(t, model, model.applySetup()())
	result.Plan.ModelAssignments[0].Model = "mutated/source"
	model = updateModel(t, model, setupLoadedMsg{generation: model.generation, value: SetupStatus{statusGeneration: staleStatus, ModelPlan: "low"}})
	for _, expected := range []string{"INITIAL INSTALL COMPLETE", "changed  yes", "/bin/vgxness", "artifacts  18", "healthy", "Restart OpenCode"} {
		if !strings.Contains(strings.Join(model.setupRouteLines(), "\n"), expected) {
			t.Fatalf("success missing %q:\n%s", expected, model.View().Content)
		}
	}
	if !model.setup.Ready || model.setup.ModelPlan != "high" || model.setup.ModelAssignments[0].Model != "acme/persisted" {
		t.Fatalf("successful apply did not refresh setup summary: %+v", model.setup)
	}
	newer := assignmentSetupPlan(3)
	newer.ModelAssignments[0].Model = "acme/newer"
	model = updateModel(t, model, setupLoadedMsg{generation: model.generation, value: SetupStatus{statusGeneration: model.setupStatusGeneration, ModelSchemaVersion: 3, ModelAssignments: newer.ModelAssignments}})
	newer.ModelAssignments[0].Model = "mutated/newer"
	model.setRoute(routeOverview)
	model.setRoute(routeSetup)
	if request := model.setupRequest(); request.ModelAssignments == nil || (*request.ModelAssignments)[0].Reference != "acme/newer" {
		t.Fatalf("newer status was not retained: %+v", request)
	}

	staleStatus, backend.result, backend.applyErr = model.setupStatusGeneration, SetupResult{IntegrationState: "partial", Recovery: "Run safe repair\nthen retry"}, errors.New("secret backend detail")
	model = updateModel(t, model, model.applySetup()())
	model = updateModel(t, model, setupLoadedMsg{generation: model.generation, value: SetupStatus{statusGeneration: staleStatus, ModelPlan: "low"}})
	if model.setup.ModelAssignments == nil || model.setup.ModelAssignments[0].Model != "acme/newer" {
		t.Fatalf("stale status overwrote failed apply state: %+v", model.setup)
	}
	view := model.View().Content
	if !strings.Contains(view, "SETUP FAILED") || !strings.Contains(view, `Run safe repair\nthen retry`) || strings.Contains(view, "secret backend detail") {
		t.Fatalf("failure/recovery rendering is unsafe:\n%s", view)
	}
	assertMaximumWidth(t, view, 80)
}

func TestSetupSuccessReportsAppliedMixedProfileFromResultPlan(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 200, Height: 24})
	model.setRoute(routeSetup)
	model.setupGeneration = 1
	result := successfulSetupResult()
	result.Plan.ModelEfficient, result.Plan.ModelBalanced, result.Plan.ModelFrontier = "openai/fast", "anthropic/balanced", "acme/frontier"
	result.Plan.ModelEfficientVariant, result.Plan.ModelBalancedVariant, result.Plan.ModelFrontierVariant = "xhigh", "max", "none"
	result.Plan.ModelEfficientSource, result.Plan.ModelBalancedSource, result.Plan.ModelFrontierSource = "catalog", "custom", "custom"
	result.Plan.ModelEfficientAvailability, result.Plan.ModelBalancedAvailability, result.Plan.ModelFrontierAvailability = "catalog-known", "unknown", "unknown"
	model = updateModel(t, model, setupAppliedMsg{generation: 1, value: result})
	for _, expected := range []string{"model efficient  openai/fast  variant=xhigh  source=catalog  availability=catalog-known", "model balanced   anthropic/balanced  variant=max  source=custom  availability=unknown", "model frontier   acme/frontier  variant=none  source=custom  availability=unknown", "Restart OpenCode"} {
		if !strings.Contains(model.View().Content, expected) {
			t.Fatalf("success missing %q:\n%s", expected, model.View().Content)
		}
	}
}

func TestSetupIgnoresStaleAsyncMessagesAndProtectsApplyingKeys(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupGeneration = 7
	model.setupPlan = readySetupPlan("medium")
	model = updateModel(t, model, setupPlanLoadedMsg{generation: 6, value: readySetupPlan("low")})
	if model.setupPlan.ModelPlan != "medium" {
		t.Fatal("stale Setup plan replaced current preview")
	}

	applyCtx, cancel := context.WithCancel(context.Background())
	model.setupApplying = true
	model.cancelSetup = cancel
	for _, key := range []tea.KeyPressMsg{
		keyPress("q"), keyPress("g"), keyPress("r"), keyPress("a"),
		tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}), tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}),
	} {
		updated, cmd := model.Update(key)
		model = updated.(Model)
		if cmd != nil || model.route != routeSetup || !model.setupApplying {
			t.Fatalf("applying key %q escaped protected state: route=%v applying=%t cmd=%v", key.String(), model.route, model.setupApplying, cmd)
		}
	}
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	model = updated.(Model)
	if cmd != nil || !model.setupApplying {
		t.Fatalf("ctrl+c abandoned applying state: applying=%t cmd=%v", model.setupApplying, cmd)
	}
	select {
	case <-applyCtx.Done():
	default:
		t.Fatal("ctrl+c did not cancel the active apply context")
	}
	if !strings.Contains(model.View().Content, "[ctrl+c] emergency cancel") {
		t.Fatalf("applying help did not disclose emergency cancellation:\n%s", model.View().Content)
	}
	if !strings.Contains(model.View().Content, "Cancellation requested") {
		t.Fatalf("cancel request was not visible while awaiting recovery:\n%s", model.View().Content)
	}
}

func TestSetupCannotConfirmWhileTerminalIsTooSmall(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupPreviewed = true
	model.setupPreviewRequest = model.setupRequest()
	model = updateModel(t, model, keyPress("a"))
	if !model.setupConfirm {
		t.Fatal("setup confirmation did not open")
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 30, Height: 8})
	updated, cmd := model.Update(keyPress("y"))
	model = updated.(Model)
	if cmd != nil || model.setupApplying {
		t.Fatalf("undersized terminal started setup: applying=%t cmd=%v", model.setupApplying, cmd)
	}
}

func TestSetupClassifiesLifecycleFromPlanSignals(t *testing.T) {
	tests := []struct {
		name string
		plan SetupPlan
		want string
	}{
		{"initial install", readySetupPlan("medium"), "initial install"},
		{"update", SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed", SelfInstallUpdateAvailable: true}, "reinstall/update"},
		{"skills update", SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed", SkillsUpdateNeeded: true}, "reinstall/update"},
		{"integration changed", SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed", IntegrationChanged: true}, "reinstall/update"},
		{"partial setup", SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "absent", SkillsState: "installed"}, "reinstall/update"},
		{"no changes", SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed"}, "no changes"},
		{"recovery", SetupPlan{Ready: false, SelfInstallState: "recovery_pending", Blocker: "recovery required"}, "blocked/recovery"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifySetup(test.plan); got != test.want {
				t.Fatalf("classifySetup(%+v) = %q, want %q", test.plan, got, test.want)
			}
		})
	}
}

func TestSetupNoChangesCannotBeConfirmed(t *testing.T) {
	backend := &recordingSetupBackend{plan: SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed"}}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = backend.plan
	model = updateModel(t, model, keyPress("a"))
	if model.setupConfirm || !strings.Contains(model.View().Content, "no changes detected by preflight") {
		t.Fatalf("no-change plan entered confirmation:\n%s", model.View().Content)
	}
	if len(backend.applyRequests) != 0 {
		t.Fatalf("no-change plan applied: %+v", backend.applyRequests)
	}
}

func TestSetupRendersLifecycleDetailsAndRecoveryActions(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed", SelfInstallUpdateAvailable: true, SelfInstallRollbackAvailable: true, SelfInstallActiveSHA256: "active-sha", SelfInstallPreviousSHA256: "previous-sha"}
	for _, expected := range []string{"self update  yes", "active SHA  active-sha", "previous SHA  previous-sha", "rollback  available"} {
		if !strings.Contains(model.View().Content, expected) {
			t.Fatalf("preflight missing %q:\n%s", expected, model.View().Content)
		}
	}

	model.setupApplyErr = errors.New("hidden detail")
	model.setupResult = SetupResult{SelfInstallState: "recovery_pending", SelfInstallActiveSHA256: "active-sha", SelfInstallPreviousSHA256: "previous-sha", SelfInstallRollbackAvailable: true, Recovery: "Inspect safely\nthen refresh"}
	view := model.View().Content
	for _, expected := range []string{"recovery_pending", "active SHA  active-sha", "previous SHA  previous-sha", "rollback  available", "inspect and refresh before retry", `Inspect safely\nthen refresh`} {
		if !strings.Contains(view, expected) {
			t.Fatalf("recovery missing %q:\n%s", expected, view)
		}
	}
}

func TestSetupSuccessClassifiesResultAgainstPreflight(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupSucceeded = true
	model.setupResult = SetupResult{Plan: SetupPlan{SelfInstallState: "installed"}, Changed: true}
	if !strings.Contains(model.View().Content, "REINSTALLED/UPDATED COMPLETE") {
		t.Fatalf("changed result was classified as initial install:\n%s", model.View().Content)
	}
	model.setupResult = SetupResult{Plan: SetupPlan{SelfInstallState: "absent"}, Changed: true}
	if !strings.Contains(model.View().Content, "INITIAL INSTALL COMPLETE") {
		t.Fatalf("initial result missing:\n%s", model.View().Content)
	}
}

func TestSetupBlockedHelpDoesNotAdvertiseApply(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model.setRoute(routeSetup)
	model.setupPlan = SetupPlan{Ready: false, Blocker: "recovery required", SelfInstallState: "recovery_pending"}
	help := model.setupHelp()
	if strings.Contains(help, "[a]") || !strings.Contains(help, "[r]") || !strings.Contains(help, "Recovery") {
		t.Fatalf("blocked setup help=%q", help)
	}
}

func TestSetupFailureWithoutRecoveryRendersKnownStateAndRequiresInspection(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupApplyErr = context.Canceled
	model.setupResult = SetupResult{
		SelfInstallState: "installed", SelfInstallPath: "/bin/vgxness", SelfInstallRollbackAvailable: true,
		SelfInstallActiveSHA256: "active-sha", SelfInstallPreviousSHA256: "previous-sha",
		IntegrationState: "partial", IntegrationPath: "/config/manager.md", SkillsState: "installed", SkillsPath: "/skills",
	}
	view := model.View().Content
	for _, expected := range []string{"launcher  installed", "integration  partial", "shared skills  installed", "active SHA  active-sha", "previous SHA  previous-sha", "rollback  available", "operation may be partial or unverified", "inspect and refresh before retry"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("failure missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "context canceled") {
		t.Fatalf("failure exposed backend error:\n%s", view)
	}
}

func TestSetupApplyConfirmationRequiresEligibleFreshPlan(t *testing.T) {
	tests := []struct {
		name string
		plan SetupPlan
	}{
		{"ready with blocker", SetupPlan{Ready: true, HandshakeOK: true, Blocker: "repair required", SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed"}},
		{"failed handshake", SetupPlan{Ready: true, HandshakeOK: false, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed"}},
		{"launcher drift", SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "drifted", IntegrationState: "installed", SkillsState: "installed"}},
		{"no changes", SetupPlan{Ready: true, HandshakeOK: true, SelfInstallState: "installed", IntegrationState: "installed", SkillsState: "installed"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
			model.setRoute(routeSetup)
			model.setupPlan = test.plan
			model = updateModel(t, model, keyPress("a"))
			if model.setupConfirm {
				t.Fatalf("ineligible plan opened confirmation: %+v", test.plan)
			}
		})
	}
}

func TestSetupSuccessRequiresRefreshBeforeAnotherApply(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium")}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setRoute(routeSetup)
	model.setupPlan = readySetupPlan("medium")
	model.setupSucceeded = true
	model = updateModel(t, model, keyPress("a"))
	if model.setupConfirm {
		t.Fatal("successful setup reused its stale plan for confirmation")
	}
	updated, cmd := model.Update(keyPress("y"))
	model = updated.(Model)
	if cmd != nil || len(backend.applyRequests) != 0 || model.setupApplying {
		t.Fatalf("successful setup allowed stale apply: cmd=%v requests=%+v applying=%t", cmd, backend.applyRequests, model.setupApplying)
	}
}

func readySetupPlan(plan string) SetupPlan {
	steps := []SetupStep{
		{Number: 1, Title: "Requirements"},
		{Number: 2, Title: "Launcher", Mutates: true},
		{Number: 3, Title: "Retire legacy provider skill", Mutates: true},
		{Number: 4, Title: "OpenCode provider artifacts", Mutates: true},
		{Number: 5, Title: "Global skills-creator, git-delivery, cross-platform, installer-lifecycle, agent-evaluation, ci-triage, and security-boundary", Mutates: true},
		{Number: 6, Title: "Verification"},
		{Number: 7, Title: "Recovery"},
	}
	return SetupPlan{
		Digest: "fixture-digest", Provider: "opencode", Steps: steps, Ready: true,
		SelfInstallState: "absent", SelfInstallPath: "/bin/vgxness", SkillsState: "absent",
		IntegrationState: "absent", IntegrationPath: "/config/agents/vgxness-manager.md",
		ArtifactCount: 18, HandshakeOK: true, HandshakeStatus: "healthy", ModelPlan: plan,
		ModelProvider: "openai", ModelEfficient: "openai/fast", ModelBalanced: "openai/balanced", ModelFrontier: "openai/frontier",
	}
}

func successfulSetupResult() SetupResult {
	return successfulSetupResultForPlan("high")
}

func successfulSetupResultForPlan(modelPlan string) SetupResult {
	return SetupResult{
		Plan: readySetupPlan(modelPlan), Changed: true,
		SelfInstallState: "installed", SelfInstallPath: "/bin/vgxness",
		IntegrationState: "installed", IntegrationPath: "/config/agents/vgxness-manager.md",
		ArtifactCount: 18, HandshakeOK: true, HandshakeStatus: "healthy", RestartRequired: true,
	}
}

func assignmentSetupPlan(schema int) SetupPlan {
	plan := readySetupPlan("medium")
	plan.ModelSchemaVersion = schema
	plan.ModelAssignments = new([SetupModelAssignmentCount]SetupModelAssignment)
	for index, identity := range setupAgentRows {
		plan.ModelAssignments[index] = SetupModelAssignment{
			ArtifactKey: identity.ArtifactKey, Role: identity.Role, Class: identity.Class,
			Provider: "openai", Model: "openai/gpt-5.6-terra", RequestedEffort: "medium", Variant: "xhigh", VariantSpecified: true,
			Source: "catalog", Availability: "catalog-known",
		}
	}
	return plan
}

func TestSchemaV3PreservesProviderReturnedAssignmentOrder(t *testing.T) {
	plan := assignmentSetupPlan(3)
	plan.ModelAssignments[0], plan.ModelAssignments[1] = plan.ModelAssignments[1], plan.ModelAssignments[0]
	rows, ok := orderedSetupAssignments(plan.ModelAssignments)
	if !ok || rows[0].ArtifactKey != plan.ModelAssignments[0].ArtifactKey {
		t.Fatalf("schema v3 reordered provider-returned assignments")
	}
}

func openSetupRoute(t *testing.T, model Model) (Model, tea.Cmd) {
	t.Helper()
	model = updateModel(t, model, keyPress("g"))
	for range 3 {
		model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	}
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	return updated.(Model), cmd
}

func runSetupCommands(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	message := cmd()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, command := range batch {
			model = updateModel(t, model, command())
		}
		return model
	}
	return updateModel(t, model, message)
}

func keyPress(value string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: rune(value[0]), Text: value})
}
