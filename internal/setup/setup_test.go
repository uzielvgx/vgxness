package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
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
	orderedEvents  *[]string
}

func (fake *fakeInstaller) Preview(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-preview")
	if fake.orderedEvents != nil {
		*fake.orderedEvents = append(*fake.orderedEvents, "self-preview")
	}
	return fake.previewResult, fake.previewErr
}
func (fake *fakeInstaller) Install(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-install")
	return fake.installResult, fake.installErr
}
func (fake *fakeInstaller) Status(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-status")
	if fake.orderedEvents != nil {
		*fake.orderedEvents = append(*fake.orderedEvents, "self-status")
	}
	return fake.statusResult, fake.statusErr
}
func (fake *fakeInstaller) Rollback(ctx context.Context, _ selfinstall.Options) (selfinstall.Result, error) {
	fake.calls = append(fake.calls, "self-rollback")
	fake.rollbackCtxErr = ctx.Err()
	return fake.rollbackResult, fake.rollbackErr
}
func (*fakeInstaller) GCPreview(context.Context, selfinstall.Options) (selfinstall.GCResult, error) {
	return selfinstall.GCResult{}, errors.New("unexpected self GC preview")
}
func (*fakeInstaller) GCApply(context.Context, selfinstall.Options, string) (selfinstall.GCResult, error) {
	return selfinstall.GCResult{}, errors.New("unexpected self GC apply")
}
func (*fakeInstaller) GCRecover(context.Context, selfinstall.Options) (selfinstall.GCResult, error) {
	return selfinstall.GCResult{}, errors.New("unexpected self GC recover")
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
	orderedEvents *[]string
}

func (fake *fakeIntegration) Preview(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-preview")
	if fake.orderedEvents != nil {
		*fake.orderedEvents = append(*fake.orderedEvents, "integration-preview")
	}
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
	if fake.orderedEvents != nil {
		*fake.orderedEvents = append(*fake.orderedEvents, "integration-status")
	}
	return fake.statusResult, fake.statusErr
}
func (fake *fakeIntegration) Uninstall(context.Context, integration.Options) (integration.Result, error) {
	fake.calls = append(fake.calls, "integration-uninstall")
	return integration.Result{}, nil
}
func (fake *fakeIntegration) ManagedLayout(context.Context, integration.Options) (integration.ManagedLayout, error) {
	return integration.ManagedLayout{}, nil
}
func (fake *fakeIntegration) ReinstallPending(context.Context, integration.Options) (bool, error) {
	return false, nil
}
func (fake *fakeIntegration) Reinstall(context.Context, integration.Options) (integration.Result, error) {
	return integration.Result{}, nil
}

type fakeProber struct {
	result        integration.Handshake
	results       []integration.Handshake
	err           error
	calls         int
	orderedEvents *[]string
}

type fakeSkills struct {
	preview, install, status          skills.Result
	previewErr, installErr, statusErr error
	calls                             []string
	events                            *[]string
	orderedEvents                     *[]string
}

func (fake *fakeSkills) Preview(context.Context, skills.Options) (skills.Result, error) {
	fake.calls = append(fake.calls, "skills-preview")
	if fake.orderedEvents != nil {
		*fake.orderedEvents = append(*fake.orderedEvents, "skills-preview")
	}
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
	if fake.orderedEvents != nil {
		*fake.orderedEvents = append(*fake.orderedEvents, "skills-status")
	}
	return fake.status, fake.statusErr
}
func (fake *fakeSkills) Uninstall(context.Context, skills.Options) (skills.Result, error) {
	return skills.Result{}, nil
}

func (fake *fakeProber) Probe(context.Context, string) (integration.Handshake, error) {
	if fake.orderedEvents != nil {
		*fake.orderedEvents = append(*fake.orderedEvents, "probe")
	}
	if len(fake.results) > fake.calls {
		result := fake.results[fake.calls]
		fake.calls++
		return result, fake.err
	}
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

func TestSharedBoundaryPlansAndAppliesLauncherAndSkills(t *testing.T) {
	installer := &fakeInstaller{
		previewResult: selfinstall.Result{State: selfinstall.StateAbsent},
		installResult: selfinstall.Result{State: selfinstall.StateInstalled, ActiveSHA256: strings.Repeat("a", 64)},
		statusResult:  selfinstall.Result{State: selfinstall.StateInstalled, ActiveSHA256: strings.Repeat("a", 64)},
	}
	sharedSkills := &fakeSkills{
		preview: skills.Result{State: skills.StateAbsent},
		install: skills.Result{State: skills.StateInstalled, Changed: true},
		status:  skills.Result{State: skills.StateInstalled},
	}
	service := New(installer, nil, nil, nil)
	service.skills = sharedSkills
	boundary := service.Shared(Options{Workspace: "/workspace"})
	plan, err := boundary.Plan(context.Background())
	if err != nil || !plan.Ready {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	result, err := boundary.Apply(context.Background(), plan)
	if err != nil || !result.Verified || result.Changed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got := strings.Join(sharedSkills.calls, ","); got != "skills-preview" {
		t.Fatalf("skills published before providers: %s", got)
	}
	result, err = boundary.Finalize(context.Background(), plan, result)
	if err != nil || !result.Verified || !result.Changed {
		t.Fatalf("finalize result=%#v err=%v", result, err)
	}
	if got := strings.Join(installer.calls, ","); got != "self-preview,self-install,self-status" {
		t.Fatalf("launcher calls=%s", got)
	}
	if got := strings.Join(sharedSkills.calls, ","); got != "skills-preview,skills-install,skills-status" {
		t.Fatalf("skills calls=%s", got)
	}
}

func TestSharedBoundaryStatusRequiresLauncherAndSkillsHealth(t *testing.T) {
	tests := []struct {
		name      string
		launcher  selfinstall.Result
		skills    skills.Result
		skillsErr error
		wantErr   bool
	}{
		{name: "launcher absent", launcher: selfinstall.Result{State: selfinstall.StateAbsent}, skills: skills.Result{State: skills.StateInstalled}},
		{name: "skills drifted", launcher: selfinstall.Result{State: selfinstall.StateInstalled}, skills: skills.Result{State: skills.StateDrifted}, skillsErr: skills.ErrDrift},
		{name: "skills unavailable", launcher: selfinstall.Result{State: selfinstall.StateInstalled}, skillsErr: errors.New("skills unavailable"), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := New(&fakeInstaller{statusResult: test.launcher}, nil, nil, nil)
			service.skills = &fakeSkills{status: test.skills, statusErr: test.skillsErr}
			plan, err := service.Shared(Options{}).Status(context.Background())
			if test.wantErr {
				if err == nil {
					t.Fatal("expected status error")
				}
				return
			}
			if err != nil || plan.Ready || plan.Blocker == "" {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
		})
	}
	service := New(&fakeInstaller{statusResult: selfinstall.Result{State: selfinstall.StateInstalled}}, nil, nil, nil)
	if _, err := service.Shared(Options{}).Status(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing skills runtime err=%v", err)
	}
}

func TestOpenCodeProviderStopsBeforeInstallWhenPreWriteHandshakeFlips(t *testing.T) {
	preview := &fakeIntegration{previewResult: integration.Result{Provider: "opencode", State: integration.StateAbsent, ArtifactSHA256: "frozen", ArtifactCount: 18}}
	managed := &fakeIntegration{}
	health := &fakeProber{results: []integration.Handshake{{OK: true, Status: integration.HandshakeHealthy}, {Status: integration.HandshakeUnavailable}}}
	service := New(&fakeInstaller{}, nil, nil, health)
	service.managedIntegrations = func(string) (integration.ManagedRuntime, error) { return managed, nil }
	provider := service.OpenCodeProvider(Options{Workspace: "/workspace"}, func(string) (integration.Runtime, error) { return preview, nil })
	plan, err := provider.Plan(context.Background(), SharedPlan{Ready: true, Launcher: selfinstall.Result{LauncherPath: "/planned/vgxness"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Apply(context.Background(), plan, SharedResult{Verified: true, Launcher: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/verified/vgxness"}}); !errors.Is(err, ErrVerification) || strings.Contains(strings.Join(managed.calls, ","), "integration-install") {
		t.Fatalf("err=%v managed=%v", err, managed.calls)
	}
}

func TestOpenCodeProviderUsesVerifiedLauncherAndHandshakeGates(t *testing.T) {
	preview := &fakeIntegration{previewResult: integration.Result{Provider: "opencode", State: integration.StateAbsent, ArtifactSHA256: "frozen", ArtifactCount: 18}}
	managed := &fakeIntegration{
		installResult: integration.Result{Provider: "opencode", State: integration.StateInstalled, ArtifactSHA256: "frozen", ArtifactCount: 18},
		statusResult:  integration.Result{Provider: "opencode", State: integration.StateInstalled, ArtifactSHA256: "frozen", ArtifactCount: 18},
	}
	health := &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}
	service := New(&fakeInstaller{}, nil, nil, health)
	managedPath := ""
	service.managedIntegrations = func(path string) (integration.ManagedRuntime, error) {
		managedPath = path
		return managed, nil
	}
	previewPaths := []string{}
	provider := service.OpenCodeProvider(Options{Workspace: "/workspace"}, func(path string) (integration.Runtime, error) {
		previewPaths = append(previewPaths, path)
		return preview, nil
	})
	plan, err := provider.Plan(context.Background(), SharedPlan{Ready: true, Launcher: selfinstall.Result{LauncherPath: "/planned/vgxness"}})
	if err != nil || !plan.Ready || health.calls != 1 || strings.Join(previewPaths, ",") != "/planned/vgxness" {
		t.Fatalf("plan=%+v err=%v probes=%d previews=%v", plan, err, health.calls, previewPaths)
	}
	result, err := provider.Apply(context.Background(), plan, SharedResult{Verified: true, Launcher: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/verified/vgxness"}})
	if err != nil || !result.Verified || managedPath != "/verified/vgxness" || strings.Join(previewPaths, ",") != "/planned/vgxness,/verified/vgxness" || health.calls != 3 {
		t.Fatalf("result=%+v err=%v managed=%q previews=%v probes=%d", result, err, managedPath, previewPaths, health.calls)
	}
}

func TestOpenCodeProviderStatusCarriesHandshakeIndependentFromAggregateReadiness(t *testing.T) {
	integrationStatus := integration.Result{Provider: "opencode", State: integration.StateInstalled, ModelProvider: "mixed", ManifestPath: "/config/model-plan.json"}
	managed := &fakeIntegration{statusResult: integrationStatus}
	service := New(&fakeInstaller{statusResult: selfinstall.Result{State: selfinstall.StateAbsent}}, managed, func(string) (integration.Runtime, error) { return managed, nil }, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	provider := service.OpenCodeProvider(Options{Workspace: "/workspace"}, func(string) (integration.Runtime, error) { return managed, nil })
	status, err := provider.Status(context.Background(), SharedPlan{Ready: false})
	if err != nil || status.Ready || !status.Handshake.OK || status.Handshake.Status != integration.HandshakeHealthy || status.Integration.ModelProvider != "mixed" || status.Integration.ManifestPath != "/config/model-plan.json" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestOpenCodeProviderReportsRecoveryAfterPartialInstallFailure(t *testing.T) {
	preview := &fakeIntegration{previewResult: integration.Result{Provider: "opencode", State: integration.StateAbsent, ArtifactSHA256: "frozen", ArtifactCount: 18}}
	failure := errors.New("install failed after mutation")
	managed := &fakeIntegration{installResult: integration.Result{Provider: "opencode", State: integration.StatePartial, Changed: true}, installErr: failure}
	service := New(&fakeInstaller{}, nil, nil, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.managedIntegrations = func(string) (integration.ManagedRuntime, error) { return managed, nil }
	provider := service.OpenCodeProvider(Options{Workspace: "/workspace"}, func(string) (integration.Runtime, error) { return preview, nil })
	plan, err := provider.Plan(context.Background(), SharedPlan{Ready: true, Launcher: selfinstall.Result{LauncherPath: "/planned/vgxness"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Apply(context.Background(), plan, SharedResult{Verified: true, Launcher: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/verified/vgxness"}})
	if !errors.Is(err, failure) || !result.Changed || !strings.Contains(result.Recovery, "vgxness integrate opencode status") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestOpenCodeProviderRejectsChangedPreviewBeforeProviderWrite(t *testing.T) {
	preview := &fakeIntegration{previewResult: integration.Result{Provider: "opencode", State: integration.StateAbsent, ArtifactSHA256: "frozen", ArtifactCount: 18}}
	managed := &fakeIntegration{}
	service := New(&fakeInstaller{}, nil, nil, &fakeProber{result: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}})
	service.managedIntegrations = func(string) (integration.ManagedRuntime, error) { return managed, nil }
	provider := service.OpenCodeProvider(Options{Workspace: "/workspace"}, func(string) (integration.Runtime, error) { return preview, nil })
	plan, err := provider.Plan(context.Background(), SharedPlan{Ready: true, Launcher: selfinstall.Result{LauncherPath: "/planned/vgxness"}})
	if err != nil {
		t.Fatal(err)
	}
	preview.previewResult.ArtifactSHA256 = "changed"
	if _, err := provider.Apply(context.Background(), plan, SharedResult{Verified: true, Launcher: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/verified/vgxness"}}); !errors.Is(err, ErrVerification) || strings.Contains(strings.Join(managed.calls, ","), "integration-install") {
		t.Fatalf("err=%v managed=%v", err, managed.calls)
	}
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
	if !strings.Contains(plan.Steps[3].Title, "artefactos del proveedor") || !strings.Contains(plan.Steps[3].Explanation, "7 agentes enlazados al plan de modelos") || !strings.Contains(plan.Steps[4].Explanation, "18 skills y 46 archivos") || !strings.Contains(plan.Steps[4].Explanation, "memory-sync") || !strings.Contains(plan.Steps[4].Explanation, "no pertenecen a OpenCode") {
		t.Fatalf("steps 4-5 do not describe model and provider ownership accurately: step4=%#v step5=%#v", plan.Steps[3], plan.Steps[4])
	}
}

func TestPlanAndStatusPreflightCharacterization(t *testing.T) {
	tests := []struct {
		name, method, wantBlocker, wantInstaller, wantIntegration string
		launcher                                                  selfinstall.State
		handshake                                                 integration.Handshake
		ready                                                     bool
	}{
		{"plan installable", "plan", "", "self-preview", "integration-preview", selfinstall.StateAbsent, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, true},
		{"status not installed", "status", "La configuración todavía no está completa o presenta drift. Ejecuta el wizard para revisar el plan de reparación.", "self-status", "integration-status", selfinstall.StateAbsent, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, false},
		{"plan unavailable", "plan", "OpenCode no está disponible o el workspace no es válido. Instala una versión compatible y vuelve a ejecutar el wizard.", "self-preview", "integration-preview", selfinstall.StateAbsent, integration.Handshake{Status: integration.HandshakeUnavailable}, false},
		{"status unhealthy", "status", "OpenCode no está disponible, es incompatible o el workspace no es válido.", "self-status", "integration-status", selfinstall.StateInstalled, integration.Handshake{Status: integration.HandshakeUnavailable}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installer := &fakeInstaller{previewResult: selfinstall.Result{State: test.launcher}, statusResult: selfinstall.Result{State: test.launcher}}
			integrationRuntime := &fakeIntegration{previewResult: integration.Result{State: integration.StateAbsent}, statusResult: integration.Result{State: integration.StateAbsent}}
			service := New(installer, integrationRuntime, func(string) (integration.Runtime, error) { return integrationRuntime, nil }, &fakeProber{result: test.handshake})
			var plan Plan
			var err error
			if test.method == "plan" {
				plan, err = service.Plan(context.Background(), Options{Workspace: "/workspace"})
			} else {
				plan, err = service.Status(context.Background(), Options{Workspace: "/workspace"})
			}
			if err != nil || plan.Ready != test.ready || plan.Blocker != test.wantBlocker || strings.Join(installer.calls, ",") != test.wantInstaller || strings.Join(integrationRuntime.calls, ",") != test.wantIntegration {
				t.Fatalf("plan=%+v err=%v installer=%v integration=%v", plan, err, installer.calls, integrationRuntime.calls)
			}
		})
	}
}

func TestPlanAndStatusKeepObservedPreflightOnProbeError(t *testing.T) {
	for _, method := range []string{"plan", "status"} {
		t.Run(method, func(t *testing.T) {
			probeErr := errors.New("probe failed")
			installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateAbsent, LauncherPath: "/preview"}, statusResult: selfinstall.Result{State: selfinstall.StateAbsent, LauncherPath: "/status"}}
			integrationRuntime := &fakeIntegration{previewResult: integration.Result{State: integration.StatePartial, Path: "/preview-agent"}, statusResult: integration.Result{State: integration.StatePartial, Path: "/status-agent"}}
			service := New(installer, integrationRuntime, func(string) (integration.Runtime, error) { return integrationRuntime, nil }, &fakeProber{result: integration.Handshake{Status: integration.HandshakeUnavailable}, err: probeErr})
			var plan Plan
			var err error
			if method == "plan" {
				plan, err = service.Plan(context.Background(), Options{Workspace: "/workspace"})
			} else {
				plan, err = service.Status(context.Background(), Options{Workspace: "/workspace"})
			}
			if !errors.Is(err, probeErr) || plan.SelfInstall.State != selfinstall.StateAbsent || plan.Integration.State != integration.StatePartial || plan.Handshake.Status != integration.HandshakeUnavailable {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
			if method == "plan" && plan.Digest == "" {
				t.Fatalf("plan digest missing")
			}
			if method == "status" && plan.Digest != "" {
				t.Fatalf("status digest=%q", plan.Digest)
			}
		})
	}
}

func TestPlanAndStatusPreflightStatesAndDispatch(t *testing.T) {
	const (
		planDrift   = "Hay contenido administrado modificado o un destino en conflicto. El wizard no sobrescribirá esos archivos."
		statusDrift = "La configuración todavía no está completa o presenta drift. Ejecuta el wizard para revisar el plan de reparación."
	)
	tests := []struct {
		name                       string
		self                       selfinstall.State
		integrated                 integration.State
		skill                      skills.State
		skillErr                   error
		handshake                  integration.Handshake
		planReady, statusReady     bool
		planBlocker, statusBlocker string
		factory                    bool
	}{
		{"absent", selfinstall.StateAbsent, integration.StateAbsent, skills.StateAbsent, nil, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, true, false, "", statusDrift, false},
		{"partial skills", selfinstall.StateAbsent, integration.StatePartial, skills.StatePartial, nil, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, true, false, "", statusDrift, false},
		{"installed", selfinstall.StateInstalled, integration.StateInstalled, skills.StateInstalled, nil, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, true, true, "", "", true},
		{"launcher drift", selfinstall.StateDrifted, integration.StateAbsent, skills.StateAbsent, nil, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, false, false, planDrift, statusDrift, false},
		{"integration drift", selfinstall.StateAbsent, integration.StateDrifted, skills.StateAbsent, nil, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, false, false, planDrift, statusDrift, false},
		{"skills drift", selfinstall.StateAbsent, integration.StateAbsent, skills.StateDrifted, skills.ErrDrift, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, false, false, planDrift, statusDrift, false},
		{"skills conflict", selfinstall.StateAbsent, integration.StateAbsent, skills.StateConflict, skills.ErrConflict, integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, false, false, planDrift, statusDrift, false},
		{"unavailable", selfinstall.StateAbsent, integration.StateAbsent, skills.StateAbsent, nil, integration.Handshake{Status: integration.HandshakeUnavailable}, false, false, "OpenCode no está disponible o el workspace no es válido. Instala una versión compatible y vuelve a ejecutar el wizard.", "OpenCode no está disponible, es incompatible o el workspace no es válido.", false},
		{"incompatible", selfinstall.StateAbsent, integration.StateAbsent, skills.StateAbsent, nil, integration.Handshake{Status: integration.HandshakeIncompatible}, false, false, "OpenCode respondió, pero el adaptador no está saludable o la versión es incompatible. Corrige el requisito antes de continuar.", "OpenCode no está disponible, es incompatible o el workspace no es válido.", false},
		{"unhealthy", selfinstall.StateAbsent, integration.StateAbsent, skills.StateAbsent, nil, integration.Handshake{Status: integration.HandshakeHealthy}, false, false, "OpenCode respondió, pero el adaptador no está saludable o la versión es incompatible. Corrige el requisito antes de continuar.", "OpenCode no está disponible, es incompatible o el workspace no es válido.", false},
	}
	for _, test := range tests {
		for _, method := range []string{"plan", "status"} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				installer := &fakeInstaller{previewResult: selfinstall.Result{State: test.self, LauncherPath: "/launcher"}, statusResult: selfinstall.Result{State: test.self, LauncherPath: "/launcher"}}
				preview := &fakeIntegration{previewResult: integration.Result{State: test.integrated}, statusResult: integration.Result{State: test.integrated}}
				managed := &fakeIntegration{previewResult: integration.Result{State: test.integrated}, statusResult: integration.Result{State: test.integrated}}
				skillRuntime := &fakeSkills{preview: skills.Result{State: test.skill}, status: skills.Result{State: test.skill}, previewErr: test.skillErr, statusErr: test.skillErr}
				factoryCalls := 0
				service := New(installer, preview, func(path string) (integration.Runtime, error) {
					factoryCalls++
					if path != "/launcher" {
						t.Fatalf("factory path=%q", path)
					}
					return managed, nil
				}, &fakeProber{result: test.handshake})
				service.skills = skillRuntime

				var plan Plan
				var err error
				if method == "plan" {
					plan, err = service.Plan(context.Background(), Options{Workspace: "/workspace"})
				} else {
					plan, err = service.Status(context.Background(), Options{Workspace: "/workspace"})
				}
				wantReady, wantBlocker, wantSelf, wantSkill, wantIntegration := test.planReady, test.planBlocker, "self-preview", "skills-preview", "integration-preview"
				if method == "status" {
					wantReady, wantBlocker, wantSelf, wantSkill, wantIntegration = test.statusReady, test.statusBlocker, "self-status", "skills-status", "integration-status"
				}
				active := preview
				if test.factory {
					active = managed
				}
				if err != nil || plan.Ready != wantReady || plan.Blocker != wantBlocker || strings.Join(installer.calls, ",") != wantSelf || strings.Join(skillRuntime.calls, ",") != wantSkill || strings.Join(active.calls, ",") != wantIntegration || factoryCalls != btoi(test.factory) {
					t.Fatalf("plan=%+v err=%v installer=%v skills=%v preview=%v managed=%v factory=%d", plan, err, installer.calls, skillRuntime.calls, preview.calls, managed.calls, factoryCalls)
				}
				if method == "plan" && plan.Digest == "" {
					t.Fatal("Plan did not publish a digest")
				}
				if method == "status" && plan.Digest != "" {
					t.Fatalf("Status published digest %q", plan.Digest)
				}
			})
		}
	}
}

func TestPlanAndStatusPreflightFatalErrorsKeepOriginalPublicationBoundary(t *testing.T) {
	tests := []struct{ name, phase string }{
		{"self", "self"}, {"skills", "skills"}, {"factory", "factory"}, {"integration", "integration"}, {"probe", "probe"},
	}
	for _, test := range tests {
		for _, method := range []string{"plan", "status"} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				fatal := errors.New(test.phase + " failed")
				var events []string
				installer := &fakeInstaller{previewResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/launcher"}, statusResult: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/launcher"}, orderedEvents: &events}
				if test.phase == "self" {
					installer.previewErr, installer.statusErr = fatal, fatal
				}
				preview := &fakeIntegration{previewResult: integration.Result{State: integration.StatePartial}, statusResult: integration.Result{State: integration.StatePartial}}
				managed := &fakeIntegration{previewResult: integration.Result{State: integration.StatePartial}, statusResult: integration.Result{State: integration.StatePartial}, orderedEvents: &events}
				if test.phase == "integration" {
					managed.previewErr, managed.statusErr = fatal, fatal
				}
				skillRuntime := &fakeSkills{preview: skills.Result{State: skills.StatePartial}, status: skills.Result{State: skills.StatePartial}, orderedEvents: &events}
				if test.phase == "skills" {
					skillRuntime.previewErr, skillRuntime.statusErr = fatal, fatal
				}
				factoryCalls := 0
				service := New(installer, preview, func(string) (integration.Runtime, error) {
					factoryCalls++
					events = append(events, "factory")
					if test.phase == "factory" {
						return nil, fatal
					}
					return managed, nil
				}, &fakeProber{result: integration.Handshake{Status: integration.HandshakeUnavailable}, orderedEvents: &events, err: func() error {
					if test.phase == "probe" {
						return fatal
					}
					return nil
				}()})
				service.skills = skillRuntime
				var plan Plan
				var err error
				if method == "plan" {
					plan, err = service.Plan(context.Background(), Options{Workspace: "/workspace"})
				} else {
					plan, err = service.Status(context.Background(), Options{Workspace: "/workspace"})
				}
				if !errors.Is(err, fatal) {
					t.Fatalf("err=%v", err)
				}
				if test.phase == "probe" {
					if plan.SelfInstall.State != selfinstall.StateInstalled || plan.Skills.State != skills.StatePartial || plan.Integration.State != integration.StatePartial || plan.Handshake.Status != integration.HandshakeUnavailable {
						t.Fatalf("probe did not retain observations: %+v", plan)
					}
				} else if plan.SelfInstall.State != "" || plan.Skills.State != "" || plan.Integration.State != "" || plan.Handshake.Status != "" {
					t.Fatalf("%s leaked observations: %+v", test.phase, plan)
				}
				if method == "plan" && plan.Digest == "" {
					t.Fatal("Plan error lacks digest")
				}
				if method == "status" && plan.Digest != "" {
					t.Fatalf("Status error digest=%q", plan.Digest)
				}
				operation := "preview"
				if method == "status" {
					operation = "status"
				}
				wantEvents := []string{"self-" + operation}
				if test.phase != "self" {
					wantEvents = append(wantEvents, "skills-"+operation)
				}
				if test.phase != "self" && test.phase != "skills" {
					wantEvents = append(wantEvents, "factory")
				}
				if test.phase == "integration" || test.phase == "probe" {
					wantEvents = append(wantEvents, "integration-"+operation)
				}
				if test.phase == "probe" {
					wantEvents = append(wantEvents, "probe")
				}
				if got := strings.Join(events, ","); got != strings.Join(wantEvents, ",") || len(preview.calls) != 0 || factoryCalls != btoi(test.phase != "self" && test.phase != "skills") {
					t.Fatalf("events=%v want=%v preview=%v factory=%d", events, wantEvents, preview.calls, factoryCalls)
				}
			})
		}
	}
}

func TestPlanAndStatusSharePrerequisiteValidation(t *testing.T) {
	tests := []struct{ name string }{
		{"nil service"}, {"missing installer"}, {"missing preview"}, {"missing factory"}, {"missing prober"}, {"empty workspace"},
	}
	for _, test := range tests {
		for _, method := range []string{"plan", "status"} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				installer := &fakeInstaller{}
				preview := &fakeIntegration{}
				prober := &fakeProber{}
				factoryCalls := 0
				factory := func(string) (integration.Runtime, error) { factoryCalls++; return preview, nil }
				service := New(installer, preview, factory, prober)
				workspace := "/workspace"
				switch test.name {
				case "nil service":
					service = nil
				case "missing installer":
					service = New(nil, preview, factory, prober)
				case "missing preview":
					service = New(installer, nil, factory, prober)
				case "missing factory":
					service = New(installer, preview, nil, prober)
				case "missing prober":
					service = New(installer, preview, factory, nil)
				case "empty workspace":
					workspace = ""
				}
				var plan Plan
				var err error
				if method == "plan" {
					plan, err = service.Plan(context.Background(), Options{Workspace: workspace})
				} else {
					plan, err = service.Status(context.Background(), Options{Workspace: workspace})
				}
				if !errors.Is(err, ErrInvalid) || len(installer.calls) != 0 || len(preview.calls) != 0 || prober.calls != 0 || factoryCalls != 0 {
					t.Fatalf("plan=%+v err=%v installer=%v preview=%v probe=%d factory=%d", plan, err, installer.calls, preview.calls, prober.calls, factoryCalls)
				}
				if method == "plan" && plan.Digest == "" {
					t.Fatal("Plan invalid response lacks digest")
				}
				if method == "status" && plan.Digest != "" {
					t.Fatalf("Status invalid response digest=%q", plan.Digest)
				}
			})
		}
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
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

func testModelAssignmentRows() [integration.ModelAssignmentCount]modelplan.OpenCodeAgentAssignmentV3 {
	var rows [integration.ModelAssignmentCount]modelplan.OpenCodeAgentAssignmentV3
	for index := range rows {
		rows[index] = modelplan.OpenCodeAgentAssignmentV3{ArtifactKey: fmt.Sprintf("agents/agent-%02d.md", index), Provider: "acme", Model: fmt.Sprintf("acme/model-%02d", index)}
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
