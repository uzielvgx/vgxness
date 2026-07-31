package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type recordingSetupBackend struct {
	fakeBackend
	planRequests  []SetupRequest
	applyRequests []SetupRequest
	plan          SetupPlan
	result        SetupResult
	planErr       error
	applyErr      error
}

func (backend *recordingSetupBackend) PlanSetup(_ context.Context, request SetupRequest) (SetupPlan, error) {
	backend.planRequests = append(backend.planRequests, request)
	return backend.plan, backend.planErr
}

func (backend *recordingSetupBackend) ApplySetup(_ context.Context, request SetupRequest) (SetupResult, error) {
	backend.applyRequests = append(backend.applyRequests, request)
	return backend.result, backend.applyErr
}

func TestSetupNavigationLoadsInstalledPlanAndRendersControlledWritePreview(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("high")}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updateModel(t, model, setupLoadedMsg{generation: 1, value: SetupStatus{ModelPlan: "high"}})

	model, cmd := openSetupRoute(t, model)
	if cmd == nil {
		t.Fatal("opening Setup did not load a real preview")
	}
	model = updateModel(t, model, cmd())

	if len(backend.planRequests) != 1 || backend.planRequests[0].Workspace != "/workspace" || backend.planRequests[0].Plan != "high" {
		t.Fatalf("plan requests=%+v", backend.planRequests)
	}
	view := model.View().Content
	for _, expected := range []string{"VGXNESS / SETUP / CONTROLLED WRITE", "READY TO APPLY", "selected plan  high", "7-STEP PLAN", "Global agent-skill-engineer", "Skill autónoma de OpenCode"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Setup preview missing %q:\n%s", expected, view)
		}
	}
	assertMaximumWidth(t, view, 80)
}

func TestSetupRequiresExplicitConfirmationAndSelectedPlanReachesApply(t *testing.T) {
	backend := &recordingSetupBackend{plan: readySetupPlan("medium"), result: successfulSetupResult()}
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model, cmd := openSetupRoute(t, model)
	model = updateModel(t, model, cmd())

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
	if len(backend.applyRequests) != 0 || !strings.Contains(model.View().Content, "CONFIRM SETUP") {
		t.Fatalf("apply started before y confirmation: requests=%+v", backend.applyRequests)
	}
	model = updateModel(t, model, keyPress("n"))
	if len(backend.applyRequests) != 0 || strings.Contains(model.View().Content, "CONFIRM SETUP") {
		t.Fatal("n did not cancel confirmation without writes")
	}
	model = updateModel(t, model, keyPress("a"))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if len(backend.applyRequests) != 0 || strings.Contains(model.View().Content, "CONFIRM SETUP") {
		t.Fatal("Esc did not cancel confirmation without writes")
	}

	model = updateModel(t, model, keyPress("a"))
	updated, applyCmd := model.Update(keyPress("y"))
	model = updated.(Model)
	if applyCmd == nil || len(backend.applyRequests) != 0 || !strings.Contains(model.View().Content, "Applying verified 7-step plan...") {
		t.Fatalf("confirmed apply state invalid: cmd=%v requests=%+v", applyCmd, backend.applyRequests)
	}
	model = updateModel(t, model, applyCmd())
	if len(backend.applyRequests) != 1 || backend.applyRequests[0].Plan != "high" {
		t.Fatalf("apply requests=%+v", backend.applyRequests)
	}
}

func TestSetupRendersSuccessAndFailureRecovery(t *testing.T) {
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setup = SetupStatus{Ready: false, IntegrationState: "absent", ModelPlan: "medium"}
	model.setRoute(routeSetup)
	model.setupGeneration = 4
	model = updateModel(t, model, setupAppliedMsg{generation: 4, value: successfulSetupResult()})
	for _, expected := range []string{"SETUP COMPLETE", "changed  yes", "/bin/vgxness", "artifacts  20", "healthy", "Restart OpenCode"} {
		if !strings.Contains(model.View().Content, expected) {
			t.Fatalf("success missing %q:\n%s", expected, model.View().Content)
		}
	}
	if !model.setup.Ready || model.setup.IntegrationState != "installed" || model.setup.ModelPlan != "high" {
		t.Fatalf("successful apply did not refresh setup summary: %+v", model.setup)
	}

	model.setupApplying = true
	model.setupGeneration = 5
	model = updateModel(t, model, setupAppliedMsg{
		generation: 5,
		value:      SetupResult{Recovery: "Run safe repair\nthen retry"},
		err:        errors.New("secret backend detail"),
	})
	view := model.View().Content
	if !strings.Contains(view, "SETUP FAILED") || !strings.Contains(view, `Run safe repair\nthen retry`) || strings.Contains(view, "secret backend detail") {
		t.Fatalf("failure/recovery rendering is unsafe:\n%s", view)
	}
	assertMaximumWidth(t, view, 80)
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

func readySetupPlan(plan string) SetupPlan {
	steps := []SetupStep{
		{Number: 1, Title: "Requirements"},
		{Number: 2, Title: "Launcher", Mutates: true},
		{Number: 3, Title: "Global agent-skill-engineer", Mutates: true},
		{Number: 4, Title: "Skill autónoma de OpenCode", Mutates: true},
		{Number: 5, Title: "Storage and model plan", Mutates: true},
		{Number: 6, Title: "Verification"},
		{Number: 7, Title: "Recovery"},
	}
	return SetupPlan{
		Provider: "opencode", Steps: steps, Ready: true,
		SelfInstallState: "absent", SelfInstallPath: "/bin/vgxness",
		IntegrationState: "absent", IntegrationPath: "/config/agents/vgxness-manager.md",
		ArtifactCount: 20, HandshakeOK: true, HandshakeStatus: "healthy", ModelPlan: plan,
		ModelProvider: "openai", ModelEfficient: "openai/fast", ModelBalanced: "openai/balanced", ModelFrontier: "openai/frontier",
	}
}

func successfulSetupResult() SetupResult {
	return SetupResult{
		Plan: readySetupPlan("high"), Changed: true,
		SelfInstallState: "installed", SelfInstallPath: "/bin/vgxness",
		IntegrationState: "installed", IntegrationPath: "/config/agents/vgxness-manager.md",
		ArtifactCount: 20, HandshakeOK: true, HandshakeStatus: "healthy", RestartRequired: true,
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

func keyPress(value string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: rune(value[0]), Text: value})
}
