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
	for _, expected := range []string{"VGXNESS / SETUP / CONTROLLED WRITE", "READY TO APPLY", "selected plan  high", "7-STEP PLAN", "Global skills-creator", "Retire legacy provider skill"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Setup preview missing %q:\n%s", expected, view)
		}
	}
	assertMaximumWidth(t, view, 80)
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
			model = updateModel(t, model, previewCmd())
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
	model := NewModel(context.Background(), &recordingSetupBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model.setup = SetupStatus{Ready: false, IntegrationState: "absent", ModelPlan: "medium"}
	model.setRoute(routeSetup)
	model.setupGeneration = 4
	model = updateModel(t, model, setupAppliedMsg{generation: 4, value: successfulSetupResult()})
	for _, expected := range []string{"INITIAL INSTALL COMPLETE", "changed  yes", "/bin/vgxness", "artifacts  18", "healthy", "Restart OpenCode"} {
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
		{Number: 5, Title: "Global skills-creator, stacked-pr, cross-platform, installer-lifecycle, agent-evaluation, ci-triage, and security-boundary", Mutates: true},
		{Number: 6, Title: "Verification"},
		{Number: 7, Title: "Recovery"},
	}
	return SetupPlan{
		Provider: "opencode", Steps: steps, Ready: true,
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
