package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/selfinstall"
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
}

func (fake *fakeIntegration) Preview(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-preview")
	return fake.previewResult, fake.previewErr
}
func (fake *fakeIntegration) Install(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-install")
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

type fakeBridge struct {
	response bridge.Response
	err      error
	calls    int
}

func (fake *fakeBridge) Status(context.Context, string) (bridge.Response, error) {
	fake.calls++
	return fake.response, fake.err
}
func (*fakeBridge) Dispatch(context.Context, string, bridge.DispatchRequest) (bridge.Response, error) {
	return bridge.Response{}, errors.New("unexpected dispatch")
}

func TestPlanExplainsEveryStepAndDoesNotMutate(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent, LauncherPath: "/bin/vgxness", DataDir: "/data"}}
	preview := &fakeIntegration{previewResult: integration.Result{Provider: "opencode", State: integration.StateAbsent, Bridge: integration.BridgeNotRequired, Path: "/config/agents/vgxness-manager.md"}}
	health := &fakeBridge{response: bridge.Response{OK: true, Provider: "opencode", Status: "healthy"}}
	factoryCalls := 0
	service := New(installer, preview, func(string) (integration.Runtime, error) {
		factoryCalls++
		return &fakeIntegration{}, nil
	}, health)
	plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready || len(plan.Steps) != 6 || factoryCalls != 0 || strings.Join(installer.calls, ",") != "self-preview" || strings.Join(preview.calls, ",") != "integration-preview" {
		t.Fatalf("unexpected plan=%#v installer=%v integration=%v factory=%d", plan, installer.calls, preview.calls, factoryCalls)
	}
	for index, step := range plan.Steps {
		if step.Number != index+1 || step.Title == "" || step.Explanation == "" {
			t.Fatalf("incomplete step %d: %#v", index, step)
		}
	}
}

func TestPlanReportsUnavailablePrerequisiteWithoutApplying(t *testing.T) {
	installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent}}
	preview := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}}
	service := New(installer, preview, func(string) (integration.Runtime, error) { return preview, nil }, &fakeBridge{err: bridge.ErrUnavailable})
	plan, err := service.Plan(context.Background(), Options{Workspace: "/workspace"})
	if err != nil || plan.Ready || plan.Blocker == "" || len(plan.Steps) != 6 {
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
		installResult: integration.Result{Provider: "opencode", State: integration.StateInstalled, Bridge: integration.BridgeNotRequired, Changed: true},
		statusResult:  integration.Result{Provider: "opencode", State: integration.StateInstalled, Bridge: integration.BridgeNotRequired},
	}
	health := &fakeBridge{response: bridge.Response{OK: true, Provider: "opencode", Status: "healthy"}}
	requestedLauncher := ""
	service := New(installer, preview, func(path string) (integration.Runtime, error) {
		requestedLauncher = path
		return managed, nil
	}, health)
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if requestedLauncher != launcherPath || !result.Changed || result.Bridge.Status != "healthy" || result.Recovery != "" {
		t.Fatalf("unexpected result=%#v launcher=%q", result, requestedLauncher)
	}
	if strings.Join(managed.calls, ",") != "integration-install,integration-status" || health.calls != 2 {
		t.Fatalf("managed=%v health=%d", managed.calls, health.calls)
	}
}

func TestStatusReadinessDependsOnOpenCodeAndNativeProfilesNotBridgeProjection(t *testing.T) {
	installer := &fakeInstaller{
		statusResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness"},
	}
	managed := &fakeIntegration{
		statusResult: integration.Result{
			Provider: "opencode",
			State:    integration.StateInstalled,
			Bridge:   integration.BridgeUnavailable,
		},
	}
	health := &fakeBridge{response: bridge.Response{OK: true, Provider: "opencode", Status: "healthy"}}
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
	service := New(installer, &fakeIntegration{}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeBridge{response: bridge.Response{OK: true, Status: "healthy"}})
	result, err := service.Apply(context.Background(), Options{Workspace: "/workspace"})
	if !errors.Is(err, integration.ErrConflict) || result.SelfInstall.ActiveSHA256 != oldDigest || !strings.Contains(result.Recovery, "revirtió") || !strings.Contains(strings.Join(installer.calls, ","), "self-rollback") {
		t.Fatalf("result=%#v err=%v calls=%v", result, err, installer.calls)
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
	service := New(installer, &fakeIntegration{}, func(string) (integration.Runtime, error) { return managed, nil }, &fakeBridge{response: bridge.Response{OK: true, Status: "healthy"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Apply(ctx, Options{Workspace: "/workspace"})
	if !errors.Is(err, context.Canceled) || result.SelfInstall.ActiveSHA256 != oldDigest || installer.rollbackCtxErr != nil {
		t.Fatalf("result=%#v err=%v rollback context=%v", result, err, installer.rollbackCtxErr)
	}
}
