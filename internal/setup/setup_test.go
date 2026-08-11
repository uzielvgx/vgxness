package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/selfinstall"
	"github.com/vgxness/vgxness/internal/skills"
)

type fakeInstaller struct {
	previewResult  selfinstall.Result
	installResult  selfinstall.Result
	statusResult   selfinstall.Result
	rollbackResult selfinstall.Result
	previewErr     error
	installErr     error
	statusErr      error
	rollbackErr    error
	rollbackCtxErr error
	calls          []string
}

func (fake *fakeInstaller) Preview(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-preview")
	return fake.previewResult, fake.previewErr
}
func (fake *fakeInstaller) Install(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-install")
	return fake.installResult, fake.installErr
}
func (fake *fakeInstaller) Status(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-status")
	return fake.statusResult, fake.statusErr
}
func (fake *fakeInstaller) Rollback(ctx context.Context, _ selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-rollback")
	fake.rollbackCtxErr = ctx.Err()
	return fake.rollbackResult, fake.rollbackErr
}

type fakeIntegration struct {
	previewResult integration.Result
	installResult integration.Result
	statusResult  integration.Result
	previewErr    error
	installErr    error
	statusErr     error
	calls         []string
	events        *[]string
}

func (fake *fakeIntegration) Preview(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-preview")
	return fake.previewResult, fake.previewErr
}
func (fake *fakeIntegration) Install(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-install")
	if fake.events != nil {
		*fake.events = append(*fake.events, "integration-install")
	}
	return fake.installResult, fake.installErr
}
func (fake *fakeIntegration) Status(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-status")
	return fake.statusResult, fake.statusErr
}
func (fake *fakeIntegration) Uninstall(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-uninstall")
	return integration.Result{}, nil
}

type fakeProber struct {
	result integration.Handshake
	err    error
	calls  int
}

type fakeSkills struct {
	preview, install, status          skills.Result
	previewErr, installErr, statusErr error
	calls                             []string
	events                            *[]string
}

func (fake *fakeSkills) Preview(context.Context, skills.Options) (skills.Result, error) {
	fake.calls = append(fake.calls, "skills-preview")
	return fake.preview, fake.previewErr
}
func (fake *fakeSkills) Install(context.Context, skills.Options) (skills.Result, error) {
	fake.calls = append(fake.calls, "skills-install")
	if fake.events != nil {
		*fake.events = append(*fake.events, "skills-install")
	}
	return fake.install, fake.installErr
}
func (fake *fakeSkills) Status(context.Context, skills.Options) (skills.Result, error) {
	fake.calls = append(fake.calls, "skills-status")
	return fake.status, fake.statusErr
}
func (fake *fakeSkills) Uninstall(context.Context, skills.Options) (skills.Result, error) {
	return skills.Result{}, nil
}

func (fake *fakeProber) Probe(context.Context, string) (integration.Handshake, error) {
	fake.calls++
	return fake.result, fake.err
}

func applyConfirmed(t *testing.T, service *Service, options Options) (Result, error) {
	t.Helper()
	plan, err := service.Plan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	options.ExpectedPlanDigest = plan.Digest
	return service.Apply(context.Background(), options)
}

func TestPlanExplainsEveryStepAndDoesNotMutate(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent, LauncherPath: "/bin/vgxness", DataDir: "/data"}}
	preview := &fakeIntegration{previewResult: integration.Result{Provider: "opencode", State: integration.StateAbsent, Path: "/config/agents/vgxness-manager.md"}}
	health := &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}
	factoryCalls := 0
	service := New(installer, preview, func(string) (integration.Runtime, error) {
		factoryCalls++
		return &fakeIntegration{}, nil
	}, health)
	plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready || len(plan.Steps) != 7 || factoryCalls != 0 || strings.Join(installer.calls, ",") != "self-preview" || strings.Join(preview.calls, ",") != "integration-preview" {
		t.Fatalf("unexpected plan=%#v installer=%v integration=%v factory=%d", plan, installer.calls, preview.calls, factoryCalls)
	}
	for index, step := range plan.Steps {
		if step.Number != index+1 || step.Title == "" || step.Explanation == "" {
			t.Fatalf("incomplete step %d: %#v", index, step)
		}
	}
	if !strings.Contains(plan.Steps[2].Title, "plugin y la skill") || !strings.Contains(plan.Steps[2].Explanation, "v1-v10") || !strings.Contains(plan.Steps[2].Explanation, "vgxness.ts") || !strings.Contains(plan.Steps[2].Explanation, "vgxness-autonomous-stacked-pr") {
		t.Fatalf("step 3 does not identify safe legacy retirement: %#v", plan.Steps[2])
	}
	if !strings.Contains(plan.Steps[3].Title, "artefactos del proveedor") || !strings.Contains(plan.Steps[3].Explanation, "15 agentes enlazados al plan de modelos") || !strings.Contains(plan.Steps[4].Explanation, "18 skills y 42 archivos") || !strings.Contains(plan.Steps[4].Explanation, "end-to-end-testing y sdd-lifecycle") || !strings.Contains(plan.Steps[4].Explanation, "no pertenecen a OpenCode") {
		t.Fatalf("steps 4-5 do not describe model and provider ownership accurately: step4=%#v step5=%#v", plan.Steps[3], plan.Steps[4])
	}
}

func TestPlanDigestIsStableAndBindsFullPlan(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent, ModelEfficient: "openai/fast"}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return preview, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	first, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	second, secondErr := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || secondErr != nil || first.Digest == "" || first.Digest != second.Digest {
		t.Fatalf("digests=%q/%q errors=%v/%v", first.Digest, second.Digest, err, secondErr)
	}
	preview.previewResult.ModelEfficient = "acme/fast"
	changed, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || changed.Digest == first.Digest {
		t.Fatalf("slot change digest=%q err=%v", changed.Digest, err)
	}
	preview.previewResult.ModelEfficient = "openai/fast"
	preview.previewResult.State = integration.StatePartial
	changed, err = service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || changed.Digest == first.Digest {
		t.Fatalf("state change digest=%q err=%v", changed.Digest, err)
	}
	otherWorkspace, err := service.Plan(context.Background(), Options{Workspace: "/other-workspace"})
	if err != nil || otherWorkspace.Digest == "" || otherWorkspace.Digest == changed.Digest {
		t.Fatalf("workspace digest=%q current=%q err=%v", otherWorkspace.Digest, changed.Digest, err)
	}
}

func TestApplyDigestBindsWorkspaceBeforeMutation(t *testing.T) {
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}, statusResult: integration.Result{State: integration.StateInstalled}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})

	confirmed, err := service.Plan(context.Background(), Options{Workspace: "/workspace-a"})
	if err != nil || confirmed.Digest == "" {
		t.Fatalf("plan=%+v err=%v", confirmed, err)
	}
	if _, err := service.Apply(context.Background(), Options{Workspace: "/workspace-b", ExpectedPlanDigest: confirmed.Digest}); !errors.Is(err, ErrPrerequisite) || strings.Contains(strings.Join(installer.calls, ","), "self-install") || strings.Contains(strings.Join(managed.calls, ","), "integration-install") {
		t.Fatalf("cross-workspace apply err=%v installer=%v integration=%v", err, installer.calls, managed.calls)
	}
	if _, err := service.Apply(context.Background(), Options{Workspace: "/workspace-a", ExpectedPlanDigest: confirmed.Digest}); err != nil {
		t.Fatalf("same-workspace apply err=%v", err)
	}
}

func TestApplyRejectsMismatchedPlanDigestBeforeMutation(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return preview, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace", ExpectedPlanDigest: "stale"})
	if !errors.Is(err, ErrPrerequisite) || len(installer.calls) != 1 || len(preview.calls) != 1 || result.Plan.Digest == "" {
		t.Fatalf("result=%+v err=%v installer=%v preview=%v", result, err, installer.calls, preview.calls)
	}
}

func TestApplyRejectsEmptyPlanDigestBeforeMutation(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return preview, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})

	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if !errors.Is(err, ErrPrerequisite) || result.Plan.Digest == "" || strings.Join(installer.calls, ",") != "self-preview" || strings.Join(preview.calls, ",") != "integration-preview" {
		t.Fatalf("result=%+v err=%v installer=%v preview=%v", result, err, installer.calls, preview.calls)
	}
}

func TestApplyReadsBackIntegrationAfterRecoveryError(t *testing.T) {
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	installCause := errors.New("install failed")
	installErr := errors.Join(integration.ErrRecovery, installCause)
	statusErr := errors.New("status failed")
	observed := integration.Result{State: integration.StatePartial, Path: "/config/observed"}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent, ModelEfficient: "openai/fast"}}
	managed := &fakeIntegration{installErr: installErr, statusResult: observed, statusErr: statusErr}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace", ExpectedPlanDigest: plan.Digest})
	if !errors.Is(err, integration.ErrRecovery) || !errors.Is(err, installCause) || !errors.Is(err, statusErr) || strings.Join(managed.calls, ",") != "integration-install,integration-status" || result.Integration.State != observed.State || result.Plan.Integration.Path != observed.Path || result.Integration.ModelEfficient != "openai/fast" || result.Plan.Integration.ModelEfficient != "openai/fast" || !strings.Contains(result.Recovery, "integración no pudo revertir") {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, managed.calls)
	}
}

func TestApplyReturnsObservedDriftedSelfStatusInResultAndPlan(t *testing.T) {
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateDrifted, LauncherPath: "/stable", ActiveSHA256: "observed-drift"},
	}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, ErrVerification) || result.SelfInstall.State != selfinstall.StateDrifted || result.Plan.SelfInstall.ActiveSHA256 != "observed-drift" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPlanReportsUnavailablePrerequisiteWithoutApplying(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return preview, nil }, &fakeProber{result: integration.Handshake{Status: integration.HandshakeUnavailable}})
	plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || plan.Ready || plan.Blocker == "" || len(plan.Steps) != 7 {
		t.Fatalf("unexpected blocked plan=%#v err=%v", plan, err)
	}
	if _, err := service.Apply(context.Background(), Options{Workspace: "/workspace", ExpectedPlanDigest: plan.Digest}); !errors.Is(err, ErrPrerequisite) {
		t.Fatalf("apply error=%v", err)
	}
	if strings.Contains(strings.Join(installer.calls, ","), "self-install") {
		t.Fatalf("blocked setup mutated installer: %v", installer.calls)
	}
}

func TestApplyInstallsThroughStableLauncherAndVerifiesEverything(t *testing.T) {
	const launcherPath = "/stable/vgxness"
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent, LauncherPath: launcherPath},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: launcherPath, ActiveSHA256: strings.Repeat("a", 64), Changed: true},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: launcherPath, ActiveSHA256: strings.Repeat("a", 64)},
	}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	managed := &fakeIntegration{
		installResult: integration.Result{Provider: "opencode", State: integration.StateInstalled, Changed: true},
		statusResult:  integration.Result{Provider: "opencode", State: integration.StateInstalled},
	}
	health := &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}
	requestedLauncher := ""
	service := New(installer, preview, func(path string) (integration.Runtime, error) {
		requestedLauncher = path
		return managed, nil
	}, health)
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if requestedLauncher != launcherPath || !result.Changed || result.Handshake.Status != integration.HandshakeHealthy || result.Recovery != "" {
		t.Fatalf("unexpected result=%#v launcher=%q", result, requestedLauncher)
	}
	if strings.Join(managed.calls, ",") != "integration-install,integration-status" || health.calls != 3 {
		t.Fatalf("managed=%v health=%d", managed.calls, health.calls)
	}
}

func TestPlanAndApplyPreserveExactModelSlotDetails(t *testing.T) {
	requested := integration.Options{
		ModelEfficient: "openai/fast", ModelBalanced: "anthropic/balanced", ModelFrontier: "acme/frontier",
		ModelEfficientEffort: "low", ModelBalancedEffort: "high", ModelFrontierEffort: "ultra",
	}
	modelResult := integration.Result{
		State:          integration.StateAbsent,
		ModelEfficient: requested.ModelEfficient, ModelBalanced: requested.ModelBalanced, ModelFrontier: requested.ModelFrontier,
		ModelEfficientEffort: requested.ModelEfficientEffort, ModelBalancedEffort: requested.ModelBalancedEffort, ModelFrontierEffort: requested.ModelFrontierEffort,
		ModelEfficientSource: "custom", ModelBalancedSource: "custom", ModelFrontierSource: "custom",
		ModelEfficientAvailability: "unknown", ModelBalancedAvailability: "unknown", ModelFrontierAvailability: "unknown",
	}
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	managed := &fakeIntegration{installResult: modelResult, statusResult: integration.Result{State: integration.StateInstalled}}
	service := New(installer, &fakeIntegration{previewResult: modelResult}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})

	plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace", Integration: requested})
	if err != nil || plan.Integration != modelResult {
		t.Fatalf("plan=%+v err=%v", plan.Integration, err)
	}
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace", Integration: requested})
	wantInstalled := modelResult
	wantInstalled.State = integration.StateInstalled
	if err != nil || result.Integration != wantInstalled || result.Plan.Integration != wantInstalled {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPreserveModelDetailsKeepsV2Variants(t *testing.T) {
	fallback := integration.Result{ModelEfficientVariant: "xhigh", ModelBalancedVariant: "max", ModelFrontierVariant: "thinking", ModelVariantsSpecified: true}
	if result := preserveModelDetails(integration.Result{}, fallback); result.ModelEfficientVariant != "xhigh" || result.ModelBalancedVariant != "max" || result.ModelFrontierVariant != "thinking" || !result.ModelVariantsSpecified {
		t.Fatalf("fallback variants lost: %+v", result)
	}
	result := preserveModelDetails(integration.Result{ModelVariantsSpecified: true}, fallback)
	if result.ModelEfficientVariant != "" || result.ModelBalancedVariant != "" || result.ModelFrontierVariant != "" || !result.ModelVariantsSpecified {
		t.Fatalf("explicit provider defaults replaced: %+v", result)
	}
}

func TestApplyRetiresProviderSkillBeforePublishingGlobalSkill(t *testing.T) {
	var events []string
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}, statusResult: integration.Result{State: integration.StateInstalled}, events: &events}
	shared := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}, install: skills.Result{State: skills.StateInstalled}, status: skills.Result{State: skills.StateInstalled}, events: &events}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared
	if _, err := applyConfirmed(t, service, Options{Workspace: "/workspace"}); err != nil || strings.Join(events, ",") != "integration-install,skills-install" {
		t.Fatalf("err=%v events=%v", err, events)
	}
}

func TestPlanBlocksSkillDriftAndApplyIndependentlyVerifiesSkills(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}, installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)}, statusResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)}}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}, statusResult: integration.Result{State: integration.StateInstalled}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	drifted := &fakeSkills{preview: skills.Result{State: skills.StateDrifted}, previewErr: skills.ErrDrift}
	service.skills = drifted
	plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || plan.Ready || plan.Blocker == "" {
		t.Fatalf("drift plan=%+v err=%v", plan, err)
	}
	installed := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}, install: skills.Result{State: skills.StateInstalled, Changed: true}, status: skills.Result{State: skills.StateInstalled}}
	service.skills = installed
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if err != nil || !result.Changed || strings.Join(installed.calls, ",") != "skills-preview,skills-preview,skills-install,skills-status" {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, installed.calls)
	}
}

func TestApplyRetainsSkillsReadbackRecoveryEvidence(t *testing.T) {
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}, statusResult: integration.Result{State: integration.StateInstalled}}
	readback := skills.Result{State: skills.StateDrifted, Path: "/shared/skills", BackupPath: "/shared/skills/.vgxness-backups/uninstall-0"}
	shared := &fakeSkills{
		preview:   skills.Result{State: skills.StateAbsent},
		install:   skills.Result{State: skills.StateInstalled, Changed: true},
		status:    readback,
		statusErr: skills.ErrDrift,
	}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared

	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, ErrVerification) || !errors.Is(err, skills.ErrRecovery) || !errors.Is(err, skills.ErrDrift) || result.Plan.Skills.State != readback.State || result.Plan.Skills.Path != readback.Path || result.Plan.Skills.BackupPath != readback.BackupPath || !strings.Contains(result.Recovery, "vgxness skills status") || !strings.Contains(result.Recovery, "vgxness skills install") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestApplyPreservesSkillsReadbackDeadline(t *testing.T) {
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}, statusResult: integration.Result{State: integration.StateInstalled}}
	shared := &fakeSkills{
		preview:   skills.Result{State: skills.StateAbsent},
		install:   skills.Result{State: skills.StateInstalled, Changed: true},
		status:    skills.Result{State: skills.StateDrifted, Path: "/skills/observed"},
		statusErr: context.DeadlineExceeded,
	}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared

	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, skills.ErrRecovery) || result.Recovery != "" || result.Plan.Skills.State != skills.StateDrifted || result.Plan.Skills.Path != "/skills/observed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPlanAndStatusBlockSkillConflictWithoutError(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}, statusResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable"}}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}, statusResult: integration.Result{State: integration.StateInstalled}}
	conflicted := &fakeSkills{preview: skills.Result{State: skills.StateConflict}, previewErr: skills.ErrConflict, status: skills.Result{State: skills.StateConflict}, statusErr: skills.ErrConflict}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return preview, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = conflicted
	if plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace"}); err != nil || plan.Ready || plan.Blocker == "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if plan, err := service.Status(context.Background(), Options{Workspace: "/workspace"}); err != nil || plan.Ready || plan.Blocker == "" {
		t.Fatalf("status=%+v err=%v", plan, err)
	}
}

func TestStatusReadinessDependsOnOpenCodeAndNativeProfiles(t *testing.T) {
	installer := &fakeInstaller{
		statusResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness"},
	}
	managed := &fakeIntegration{
		statusResult: integration.Result{Provider: "opencode", State: integration.StateInstalled},
	}
	health := &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}
	service := New(installer, &fakeIntegration{}, func(string) (integration.Runtime, error) { return managed, nil }, health)

	plan, err := service.Status(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || !plan.Ready || plan.Blocker != "" {
		t.Fatalf("native setup should be ready without bridge projection: plan=%#v err=%v", plan, err)
	}
}

func TestApplyRollsBackManagedUpdateWhenIntegrationFails(t *testing.T) {
	oldDigest := strings.Repeat("a", 64)
	newDigest := strings.Repeat("b", 64)
	installer := &fakeInstaller{
		previewResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: oldDigest, UpdateAvailable: true},
		installResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: newDigest, PreviousSHA256: oldDigest, RollbackAvailable: true, Changed: true},
		rollbackResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: oldDigest, Changed: true},
	}
	managed := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}, installErr: integration.ErrConflict}
	service := New(installer, &fakeIntegration{}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, integration.ErrConflict) || result.SelfInstall.ActiveSHA256 != oldDigest || result.Plan.SelfInstall.ActiveSHA256 != oldDigest || !strings.Contains(result.Recovery, "revirtió") || !strings.Contains(strings.Join(installer.calls, ","), "self-rollback") {
		t.Fatalf("result=%#v err=%v calls=%v", result, err, installer.calls)
	}
}

func TestApplyReportsIntegrationAndLauncherRecovery(t *testing.T) {
	oldDigest, newDigest := strings.Repeat("a", 64), strings.Repeat("b", 64)
	installer := &fakeInstaller{
		previewResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: oldDigest, UpdateAvailable: true},
		installResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: newDigest, PreviousSHA256: oldDigest, RollbackAvailable: true, Changed: true},
		rollbackResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: oldDigest, Changed: true},
	}
	managed := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}, installErr: errors.Join(integration.ErrConflict, integration.ErrRecovery)}
	service := New(installer, &fakeIntegration{}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, integration.ErrRecovery) || !strings.Contains(result.Recovery, "integración no pudo revertir") || !strings.Contains(result.Recovery, "revirtió") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestApplyRollbackSurvivesCallerCancellation(t *testing.T) {
	oldDigest, newDigest := strings.Repeat("a", 64), strings.Repeat("b", 64)
	installer := &fakeInstaller{
		previewResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: oldDigest, UpdateAvailable: true},
		installResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: newDigest, PreviousSHA256: oldDigest, RollbackAvailable: true, Changed: true},
		rollbackResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness", ActiveSHA256: oldDigest, Changed: true},
	}
	managed := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}, installErr: context.Canceled}
	service := New(installer, &fakeIntegration{}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, planErr := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if planErr != nil {
		t.Fatal(planErr)
	}
	result, err := service.Apply(ctx, Options{Workspace: "/workspace", ExpectedPlanDigest: plan.Digest})
	if !errors.Is(err, context.Canceled) || result.SelfInstall.ActiveSHA256 != oldDigest || installer.rollbackCtxErr != nil {
		t.Fatalf("result=%#v err=%v rollback context=%v", result, err, installer.rollbackCtxErr)
	}
}

func TestApplyDoesNotInstallSkillsWhenLauncherFails(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}, installErr: errors.New("launcher failed")}
	shared := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return &fakeIntegration{}, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared
	if _, err := applyConfirmed(t, service, Options{Workspace: "/workspace"}); err == nil || strings.Contains(strings.Join(shared.calls, ","), "skills-install") {
		t.Fatalf("err=%v skills=%v", err, shared.calls)
	}
}

func TestApplyDoesNotPublishSkillsAfterIntegrationFailure(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}, installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)}}
	shared := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}, install: skills.Result{State: skills.StateInstalled, Changed: true}}
	managed := &fakeIntegration{installErr: integration.ErrConflict}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, integration.ErrConflict) || strings.Contains(result.Recovery, "global de skills quedó instalado") || strings.Contains(strings.Join(shared.calls, ","), "skills-install") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPlanDigestBindsAssignmentRowsAndCopiesPreview(t *testing.T) {
	rows := testModelAssignmentRows()
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent, ModelSchemaVersion: 3, ModelAssignments: &rows}}
	service := New(&fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}}, preview, func(string) (integration.Runtime, error) { return preview, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	first, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	second, secondErr := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || secondErr != nil || first.Digest != second.Digest || first.Integration.ModelAssignments == preview.previewResult.ModelAssignments {
		t.Fatalf("first=%+v second=%+v errors=%v/%v", first, second, err, secondErr)
	}
	rows[0].Model = "acme/changed"
	changed, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || changed.Digest == first.Digest || first.Integration.ModelAssignments[0].Model == rows[0].Model {
		t.Fatalf("first=%+v changed=%+v err=%v", first.Integration.ModelAssignments[0], changed.Integration.ModelAssignments[0], err)
	}
}

func TestApplyPreservesAndCopiesAssignmentRowsAcrossSparseReadback(t *testing.T) {
	rows := testModelAssignmentRows()
	modelResult := integration.Result{State: integration.StateAbsent, ModelSchemaVersion: 3, ModelAssignments: &rows}
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}, statusResult: integration.Result{State: integration.StateInstalled}}
	service := New(installer, &fakeIntegration{previewResult: modelResult}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if err != nil || result.Integration.ModelAssignments == nil || result.Plan.Integration.ModelAssignments == nil || result.Integration.ModelAssignments == &rows || result.Plan.Integration.ModelAssignments == result.Integration.ModelAssignments {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := result.Integration.ModelAssignments[0].Model
	rows[0].Model = "mutated/source"
	result.Plan.Integration.ModelAssignments[0].Model = "mutated/plan"
	if result.Integration.ModelAssignments[0].Model != want {
		t.Fatalf("assignment rows alias another boundary: %+v", result)
	}
}

func TestApplyErrorReadbackPreservesAndCopiesAssignmentRows(t *testing.T) {
	rows := testModelAssignmentRows()
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent, ModelSchemaVersion: 3, ModelAssignments: &rows}}
	managed := &fakeIntegration{installErr: integration.ErrRecovery, statusResult: integration.Result{State: integration.StatePartial}, statusErr: errors.New("status")}
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}, installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, integration.ErrRecovery) || result.Integration.ModelSchemaVersion != 3 || result.Integration.ModelAssignments == nil || result.Plan.Integration.ModelAssignments == nil || result.Integration.ModelAssignments == result.Plan.Integration.ModelAssignments {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func testModelAssignmentRows() [integration.ModelAssignmentCount]sdd.OpenCodeAgentAssignmentV3 {
	var rows [integration.ModelAssignmentCount]sdd.OpenCodeAgentAssignmentV3
	for index := range rows {
		rows[index] = sdd.OpenCodeAgentAssignmentV3{ArtifactKey: fmt.Sprintf("agents/agent-%02d.md", index), Provider: "acme", Model: fmt.Sprintf("acme/model-%02d", index)}
	}
	return rows
}

func TestApplyDisclosesIncompleteSkillsRecovery(t *testing.T) {
	oldDigest, newDigest := strings.Repeat("a", 64), strings.Repeat("b", 64)
	installer := &fakeInstaller{
		previewResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: oldDigest, UpdateAvailable: true},
		installResult:  selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: newDigest, PreviousSHA256: oldDigest, RollbackAvailable: true, Changed: true},
		rollbackResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: oldDigest, Changed: true},
	}
	managed := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	shared := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}, installErr: errors.Join(errors.New("sync failed"), skills.ErrRecovery)}
	service := New(installer, &fakeIntegration{}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if !errors.Is(err, skills.ErrRecovery) || result.SelfInstall.ActiveSHA256 != oldDigest || !strings.Contains(result.Recovery, "revirtió") || !strings.Contains(result.Recovery, "v1-v10") || !strings.Contains(result.Recovery, "plugin heredado vgxness.ts") || !strings.Contains(result.Recovery, "skill heredada vgxness-autonomous-stacked-pr") || !strings.Contains(result.Recovery, "vgxness skills status") || !strings.Contains(result.Recovery, "vgxness skills install") || !strings.Contains(result.Recovery, "puede haber quedado parcial") || strings.Contains(result.Recovery, "verificado") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestApplyDisclosesUnconfirmedSkillsAfterOrdinaryInstallFailure(t *testing.T) {
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)},
	}
	managed := &fakeIntegration{installResult: integration.Result{State: integration.StateInstalled}}
	shared := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}, installErr: errors.New("publication failed")}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared
	result, err := applyConfirmed(t, service, Options{Workspace: "/workspace"})
	if err == nil || !strings.Contains(result.Recovery, "launcher administrado se conserva") || !strings.Contains(result.Recovery, "v1-v10") || !strings.Contains(result.Recovery, "plugin heredado vgxness.ts") || !strings.Contains(result.Recovery, "skill heredada vgxness-autonomous-stacked-pr") || !strings.Contains(result.Recovery, "vgxness skills status") || !strings.Contains(result.Recovery, "vgxness skills install") || strings.Contains(result.Recovery, "puede haber quedado parcial") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
