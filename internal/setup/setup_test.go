package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
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
	if !strings.Contains(plan.Steps[2].Title, "heredada") || !strings.Contains(plan.Steps[2].Explanation, "v1, v2 o v3") {
		t.Fatalf("step 3 does not identify safe legacy retirement: %#v", plan.Steps[2])
	}
	if !strings.Contains(plan.Steps[3].Title, "artefactos del proveedor") || !strings.Contains(plan.Steps[3].Explanation, "15 agentes enlazados al plan de modelos") || !strings.Contains(plan.Steps[4].Explanation, "skills-creator, stacked-pr, cross-platform, installer-lifecycle, agent-evaluation, ci-triage y security-boundary") || !strings.Contains(plan.Steps[4].Explanation, "no pertenecen a OpenCode") {
		t.Fatalf("steps 4-5 do not describe model and provider ownership accurately: step4=%#v step5=%#v", plan.Steps[3], plan.Steps[4])
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
	if _, err := service.Apply(context.Background(), Options{Workspace: "/workspace"}); !errors.Is(err, ErrPrerequisite) {
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
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if requestedLauncher != launcherPath || !result.Changed || result.Handshake.Status != integration.HandshakeHealthy || result.Recovery != "" {
		t.Fatalf("unexpected result=%#v launcher=%q", result, requestedLauncher)
	}
	if strings.Join(managed.calls, ",") != "integration-install,integration-status" || health.calls != 2 {
		t.Fatalf("managed=%v health=%d", managed.calls, health.calls)
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
	if _, err := service.Apply(context.Background(), Options{Workspace: "/workspace"}); err != nil || strings.Join(events, ",") != "integration-install,skills-install" {
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
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || !result.Changed || strings.Join(installed.calls, ",") != "skills-preview,skills-install,skills-status" {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, installed.calls)
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
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if !errors.Is(err, integration.ErrConflict) || result.SelfInstall.ActiveSHA256 != oldDigest || !strings.Contains(result.Recovery, "revirtió") || !strings.Contains(strings.Join(installer.calls, ","), "self-rollback") {
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
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
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
	result, err := service.Apply(ctx, Options{Workspace: "/workspace"})
	if !errors.Is(err, context.Canceled) || result.SelfInstall.ActiveSHA256 != oldDigest || installer.rollbackCtxErr != nil {
		t.Fatalf("result=%#v err=%v rollback context=%v", result, err, installer.rollbackCtxErr)
	}
}

func TestApplyDoesNotInstallSkillsWhenLauncherFails(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}, installErr: errors.New("launcher failed")}
	shared := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return &fakeIntegration{}, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared
	if _, err := service.Apply(context.Background(), Options{Workspace: "/workspace"}); err == nil || strings.Contains(strings.Join(shared.calls, ","), "skills-install") {
		t.Fatalf("err=%v skills=%v", err, shared.calls)
	}
}

func TestApplyDoesNotPublishSkillsAfterIntegrationFailure(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}, installResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable", ActiveSHA256: strings.Repeat("a", 64)}}
	shared := &fakeSkills{preview: skills.Result{State: skills.StateAbsent}, install: skills.Result{State: skills.StateInstalled, Changed: true}}
	managed := &fakeIntegration{installErr: integration.ErrConflict}
	service := New(installer, &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.skills = shared
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if !errors.Is(err, integration.ErrConflict) || strings.Contains(result.Recovery, "global de skills quedó instalado") || strings.Contains(strings.Join(shared.calls, ","), "skills-install") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
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
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if !errors.Is(err, skills.ErrRecovery) || result.SelfInstall.ActiveSHA256 != oldDigest || !strings.Contains(result.Recovery, "revirtió") || !strings.Contains(result.Recovery, "vgxness skills status") || !strings.Contains(result.Recovery, "vgxness skills install") || !strings.Contains(result.Recovery, "puede haber quedado parcial") || strings.Contains(result.Recovery, "verificado") {
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
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if err == nil || !strings.Contains(result.Recovery, "launcher administrado se conserva") || !strings.Contains(result.Recovery, "heredada del proveedor") || !strings.Contains(result.Recovery, "vgxness skills status") || !strings.Contains(result.Recovery, "vgxness skills install") || strings.Contains(result.Recovery, "puede haber quedado parcial") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
