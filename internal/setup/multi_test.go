package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/providers/codex"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/selfinstall"
	"github.com/vgxness/vgxness/internal/skills"
)

type fakeMultiProvider struct {
	name          Provider
	plan          ProviderPlan
	planErr       error
	applyErr      error
	calls         *[]string
	cancelOnApply context.CancelFunc
}

type fakeShared struct {
	plan        SharedPlan
	statusErr   error
	applyErr    error
	finalizeErr error
	calls       *[]string
}

type fakeIntegrationRuntime struct {
	preview, install, reinstall, status integration.Result
	layout                              integration.ManagedLayout
	previewOptions                      integration.Options
	installOptions                      integration.Options
	statusOptions                       integration.Options
	installCalls                        int
	reinstallCalls                      int
	protectedInstallCalls               int
	protectedReinstallCalls             int
	events                              *[]string
	installErr, statusErr               error
}

type fakeSourceIdentity struct{}

func (fakeSourceIdentity) SourceIdentity() {}

func (f *fakeIntegrationRuntime) Preview(_ context.Context, options integration.Options) (integration.Result, error) {
	f.previewOptions = options
	return f.preview, nil
}
func (f *fakeIntegrationRuntime) Install(_ context.Context, options integration.Options) (integration.Result, error) {
	f.installCalls++
	if f.events != nil {
		*f.events = append(*f.events, "install")
	}
	f.installOptions = options
	return f.install, f.installErr
}
func (f *fakeIntegrationRuntime) Status(_ context.Context, options integration.Options) (integration.Result, error) {
	f.statusOptions = options
	return f.status, f.statusErr
}
func (f *fakeIntegrationRuntime) Uninstall(context.Context, integration.Options) (integration.Result, error) {
	return integration.Result{}, nil
}
func (f *fakeIntegrationRuntime) ManagedLayout(context.Context, integration.Options) (integration.ManagedLayout, error) {
	return f.layout, nil
}

type fakeManagedProtection struct {
	snapshot ManagedSnapshot
	err      error
	calls    int
	events   *[]string
}

func (f *fakeManagedProtection) Protect(context.Context) (ManagedSnapshot, error) {
	f.calls++
	if f.events != nil {
		*f.events = append(*f.events, "protect")
	}
	return f.snapshot, f.err
}
func (f *fakeIntegrationRuntime) ReinstallPending(context.Context, integration.Options) (bool, error) {
	return false, nil
}
func (f *fakeIntegrationRuntime) Reinstall(_ context.Context, options integration.Options) (integration.Result, error) {
	f.reinstallCalls++
	if f.events != nil {
		*f.events = append(*f.events, "install")
	}
	f.installOptions = options
	return f.reinstall, nil
}
func (f *fakeIntegrationRuntime) InstallProtected(ctx context.Context, options integration.Options, _ integration.SourceIdentity) (integration.Result, error) {
	f.protectedInstallCalls++
	return f.Install(ctx, options)
}
func (f *fakeIntegrationRuntime) ReinstallProtected(ctx context.Context, options integration.Options, _ integration.SourceIdentity) (integration.Result, error) {
	f.protectedReinstallCalls++
	return f.Reinstall(ctx, options)
}

func TestCodexProtectionPrecedesInstalledAndPartialMutations(t *testing.T) {
	for _, state := range []integration.State{integration.StateInstalled, integration.StatePartial} {
		t.Run(string(state), func(t *testing.T) {
			events := []string{}
			runtime := &fakeIntegrationRuntime{layout: integration.ManagedLayout{Root: t.TempDir()}, status: integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "sum", ArtifactCount: 15}, events: &events}
			runtime.install, runtime.reinstall = runtime.status, runtime.status
			protection := &fakeManagedProtection{snapshot: ManagedSnapshot{ID: "snapshot", Verified: true, Source: fakeSourceIdentity{}}, events: &events}
			adapter := &IntegrationProvider{provider: ProviderCodex, runtime: runtime, protection: protection}
			result, err := adapter.Apply(context.Background(), ProviderPlan{Provider: ProviderCodex, Ready: true, State: state, ArtifactSHA256: "sum", ArtifactCount: 15}, SharedResult{})
			if err != nil || protection.calls != 1 || !result.SnapshotVerified || result.SnapshotID != "snapshot" {
				t.Fatalf("result=%#v err=%v calls=%d", result, err, protection.calls)
			}
			if len(events) != 2 || events[0] != "protect" || events[1] != "install" {
				t.Fatalf("events=%v", events)
			}
			if state == integration.StatePartial && runtime.reinstallCalls != 1 {
				t.Fatal("partial did not reinstall")
			}
		})
	}
}

func TestCodexProtectionFailurePreventsMutationAndAbsentSkips(t *testing.T) {
	runtime := &fakeIntegrationRuntime{layout: integration.ManagedLayout{Root: t.TempDir()}, status: integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "sum", ArtifactCount: 15}}
	runtime.install = runtime.status
	protection := &fakeManagedProtection{err: errors.New("unsafe")}
	adapter := &IntegrationProvider{provider: ProviderCodex, runtime: runtime, protection: protection}
	if _, err := adapter.Apply(context.Background(), ProviderPlan{Provider: ProviderCodex, Ready: true, State: integration.StateInstalled, ArtifactSHA256: "sum", ArtifactCount: 15}, SharedResult{}); err == nil || runtime.installCalls != 0 || runtime.reinstallCalls != 0 {
		t.Fatalf("install=%d reinstall=%d", runtime.installCalls, runtime.reinstallCalls)
	}
	if _, err := adapter.Apply(context.Background(), ProviderPlan{Provider: ProviderCodex, Ready: true, State: integration.StatePartial, ArtifactSHA256: "sum", ArtifactCount: 15}, SharedResult{}); err == nil || runtime.installCalls != 0 || runtime.reinstallCalls != 0 {
		t.Fatal("protection failure mutated partial")
	}
	if _, err := adapter.Apply(context.Background(), ProviderPlan{Provider: ProviderCodex, Ready: true, State: integration.StateAbsent, ArtifactSHA256: "sum", ArtifactCount: 15}, SharedResult{}); err != nil || protection.calls != 2 {
		t.Fatalf("err=%v calls=%d", err, protection.calls)
	}
	protection.err, protection.snapshot = nil, ManagedSnapshot{}
	if _, err := adapter.Apply(context.Background(), ProviderPlan{Provider: ProviderCodex, Ready: true, State: integration.StateInstalled, ArtifactSHA256: "sum", ArtifactCount: 15}, SharedResult{}); err == nil {
		t.Fatal("unverified snapshot allowed mutation")
	}
}

func (f fakeShared) Plan(context.Context) (SharedPlan, error) {
	*f.calls = append(*f.calls, "shared:plan")
	return f.plan, nil
}
func (f fakeShared) Status(context.Context) (SharedPlan, error) {
	*f.calls = append(*f.calls, "shared:status")
	return f.plan, f.statusErr
}
func (f fakeShared) Apply(context.Context, SharedPlan) (SharedResult, error) {
	*f.calls = append(*f.calls, "shared:apply")
	return SharedResult{Verified: f.applyErr == nil}, f.applyErr
}
func (f fakeShared) Finalize(context.Context, SharedPlan, SharedResult) (SharedResult, error) {
	*f.calls = append(*f.calls, "shared:finalize")
	return SharedResult{Verified: f.finalizeErr == nil}, f.finalizeErr
}

func (f fakeMultiProvider) Provider() Provider { return f.name }
func (f fakeMultiProvider) Plan(context.Context, SharedPlan) (ProviderPlan, error) {
	*f.calls = append(*f.calls, string(f.name)+":plan")
	return f.plan, f.planErr
}
func (f fakeMultiProvider) Status(context.Context, SharedPlan) (ProviderPlan, error) {
	return f.plan, f.planErr
}
func (f fakeMultiProvider) Apply(ctx context.Context, _ ProviderPlan, _ SharedResult) (ProviderResult, error) {
	*f.calls = append(*f.calls, string(f.name)+":apply")
	if f.cancelOnApply != nil {
		f.cancelOnApply()
	}
	return ProviderResult{Provider: f.name, Verified: f.applyErr == nil}, f.applyErr
}

func TestMultiApplyOrdersProvidersAndSharesWorkOnce(t *testing.T) {
	calls := []string{}
	multi := NewMulti(
		fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true, Changed: true}, calls: &calls},
		fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, calls: &calls},
	)
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderCodex, ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, ","); got != "opencode:plan,codex:plan" {
		t.Fatalf("plan order = %s", got)
	}
	if len(plan.Shared) != 1 || !plan.Ready || !plan.Changed || plan.Digest == "" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	calls = nil
	result, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderCodex, ProviderOpenCode}, ExpectedPlanDigest: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, ","); got != "opencode:plan,codex:plan,opencode:apply,codex:apply" {
		t.Fatalf("apply order = %s", got)
	}
	if len(result.Providers) != 2 || !result.Providers[0].Verified || !result.Providers[1].Verified {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestMultiStatusRequiresSharedHealthForCodexAndAll(t *testing.T) {
	states := []struct {
		name string
		plan SharedPlan
		err  error
	}{
		{name: "launcher-absent", plan: SharedPlan{Launcher: selfinstall.Result{State: selfinstall.StateAbsent}, Skills: skills.Result{State: skills.StateInstalled}, Blocker: "shared launcher is absent"}},
		{name: "skills-drifted", plan: SharedPlan{Launcher: selfinstall.Result{State: selfinstall.StateInstalled}, Skills: skills.Result{State: skills.StateDrifted}, Blocker: "shared skills have drift"}},
		{name: "shared-unavailable", err: errors.New("shared status unavailable")},
	}
	for _, providers := range [][]Provider{{ProviderCodex}, {ProviderOpenCode, ProviderCodex}} {
		for _, state := range states {
			t.Run(state.name+"/"+string(providers[len(providers)-1]), func(t *testing.T) {
				calls := []string{}
				multi := NewMultiWithShared(
					fakeShared{plan: state.plan, statusErr: state.err, calls: &calls},
					fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true, Installed: true}, calls: &calls},
					fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true, Installed: true}, calls: &calls},
				)
				plan, err := multi.Status(context.Background(), MultiOptions{Providers: providers})
				if state.err != nil {
					if !errors.Is(err, state.err) {
						t.Fatalf("err=%v", err)
					}
					return
				}
				if err != nil || plan.Ready || plan.Blocker == "" {
					t.Fatalf("plan=%+v err=%v", plan, err)
				}
			})
		}
	}
}

func TestMultiFinalizesSharedWorkOnlyAfterAllProvidersVerify(t *testing.T) {
	calls := []string{}
	multi := NewMultiWithShared(fakeShared{plan: SharedPlan{Ready: true}, calls: &calls}, fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, calls: &calls}, fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true}, calls: &calls})
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}})
	if err != nil {
		t.Fatal(err)
	}
	calls = nil
	if _, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}, ExpectedPlanDigest: plan.Digest}); err != nil || strings.Count(strings.Join(calls, ","), "shared:finalize") != 1 || !strings.HasSuffix(strings.Join(calls, ","), "shared:finalize") {
		t.Fatalf("err=%v calls=%v", err, calls)
	}
}

func TestMultiDoesNotFinalizeSharedWorkAfterProviderFailureOrCancellation(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider fakeMultiProvider
		cancel   bool
	}{
		{name: "failure", provider: fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, applyErr: errors.New("provider failed")}},
		{name: "cancellation", provider: fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}}, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := []string{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			test.provider.calls = &calls
			if test.cancel {
				test.provider.cancelOnApply = cancel
			}
			multi := NewMultiWithShared(fakeShared{plan: SharedPlan{Ready: true}, calls: &calls}, test.provider, fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true}, calls: &calls})
			plan, err := multi.Plan(ctx, MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = multi.Apply(ctx, MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}, ExpectedPlanDigest: plan.Digest})
			if err == nil || strings.Contains(strings.Join(calls, ","), "shared:finalize") {
				t.Fatalf("err=%v calls=%v", err, calls)
			}
		})
	}
}

func TestMultiFinalizeFailurePreservesVerifiedProviderOutcomes(t *testing.T) {
	calls := []string{}
	failure := errors.New("skills failed")
	multi := NewMultiWithShared(fakeShared{plan: SharedPlan{Ready: true}, finalizeErr: failure, calls: &calls}, fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, calls: &calls})
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}, ExpectedPlanDigest: plan.Digest})
	if !errors.Is(err, failure) || len(result.Providers) != 1 || !result.Providers[0].Verified || strings.Count(strings.Join(calls, ","), "shared:finalize") != 1 {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, calls)
	}
}

func TestMultiApplyPreservesPartialSuccessAndRetrySkipsVerified(t *testing.T) {
	calls := []string{}
	codexErr := errors.New("codex unavailable")
	multi := NewMulti(
		fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true, Installed: true}, calls: &calls},
		fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true, Installed: true}, applyErr: codexErr, calls: &calls},
	)
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}, ExpectedPlanDigest: plan.Digest})
	if !errors.Is(err, codexErr) || len(result.Providers) != 2 || !result.Providers[0].Verified || result.Providers[1].Verified {
		t.Fatalf("partial result=%#v err=%v", result, err)
	}
	calls = nil
	multi.providers[ProviderCodex] = fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true, Installed: true}, calls: &calls}
	plan, err = multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}})
	if err != nil {
		t.Fatal(err)
	}
	calls = nil
	_, err = multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}, ExpectedPlanDigest: plan.Digest, Verified: []ProviderResult{{Provider: ProviderOpenCode, Verified: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, ","); got != "opencode:plan,codex:plan,codex:apply" {
		t.Fatalf("retry calls = %s", got)
	}
}

func TestMultiAppliesSharedBoundaryOnceForEverySelection(t *testing.T) {
	for _, providers := range [][]Provider{{ProviderOpenCode}, {ProviderCodex}, {ProviderOpenCode, ProviderCodex}} {
		calls := []string{}
		multi := NewMultiWithShared(fakeShared{plan: SharedPlan{Ready: true}, calls: &calls},
			fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, calls: &calls},
			fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true}, calls: &calls})
		plan, err := multi.Plan(context.Background(), MultiOptions{Providers: providers})
		if err != nil {
			t.Fatal(err)
		}
		calls = nil
		if _, err := multi.Apply(context.Background(), MultiOptions{Providers: providers, ExpectedPlanDigest: plan.Digest}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(strings.Join(calls, ","), "shared:apply"); got != 1 {
			t.Fatalf("providers=%v shared applies=%d calls=%v", providers, got, calls)
		}
	}
}

func TestMultiStopsBeforeProviderWritesWhenSharedApplyFails(t *testing.T) {
	calls := []string{}
	failure := errors.New("shared failure")
	multi := NewMultiWithShared(fakeShared{plan: SharedPlan{Ready: true}, applyErr: failure, calls: &calls}, fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, calls: &calls})
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}, ExpectedPlanDigest: plan.Digest}); !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
	if got := strings.Join(calls, ","); strings.Contains(got, "opencode:apply") {
		t.Fatalf("provider applied after shared failure: %s", got)
	}
}

func TestMultiDoesNotSkipVerifiedProviderWhenCurrentPlanChanged(t *testing.T) {
	calls := []string{}
	multi := NewMulti(fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true, Installed: true, Changed: true}, calls: &calls})
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	calls = nil
	if _, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}, ExpectedPlanDigest: plan.Digest, Verified: []ProviderResult{{Provider: ProviderOpenCode, Verified: true}}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, ","); !strings.Contains(got, "opencode:apply") {
		t.Fatalf("changed provider was skipped: %s", got)
	}
}

func TestIntegrationProviderSanitizesCodexOptionsAndVerifiesIdentity(t *testing.T) {
	runtime := &fakeIntegrationRuntime{
		preview: integration.Result{Provider: "codex", State: integration.StateAbsent, ArtifactSHA256: "preview", ArtifactCount: 2},
		install: integration.Result{Provider: "codex", State: integration.StateInstalled, Changed: true, ArtifactSHA256: "preview", ArtifactCount: 2},
		status:  integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "preview", ArtifactCount: 2},
	}
	adapter := NewIntegrationProvider(ProviderCodex, runtime, integration.Options{
		ConfigDir: "/codex", HomeDir: "/home", ModelPlan: "high", ModelEfficient: "openai/fast", ModelBalanced: "openai/balanced", ModelFrontier: "openai/frontier", ModelVariantsSpecified: true,
	})
	plan, err := adapter.Plan(context.Background(), SharedPlan{})
	if err != nil || !plan.Ready || plan.Installed {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if got := runtime.previewOptions; got.ConfigDir != "/codex" || got.ModelPlan != "high" || got.HomeDir != "" || got.ModelEfficient != "" || got.ModelBalanced != "" || got.ModelFrontier != "" || got.ModelVariantsSpecified {
		t.Fatalf("unsafe codex options: %#v", got)
	}
	result, err := adapter.Apply(context.Background(), plan, SharedResult{})
	if err != nil || !result.Verified || !result.Changed || runtime.installOptions != runtime.previewOptions || runtime.statusOptions != runtime.previewOptions {
		t.Fatalf("result=%#v err=%v install=%#v status=%#v", result, err, runtime.installOptions, runtime.statusOptions)
	}
}

func TestIntegrationProviderRejectsUnverifiedStatusIdentity(t *testing.T) {
	runtime := &fakeIntegrationRuntime{
		preview: integration.Result{Provider: "opencode", State: integration.StateAbsent, ArtifactSHA256: "planned", ArtifactCount: 1},
		install: integration.Result{Provider: "opencode", State: integration.StateInstalled, ArtifactSHA256: "planned", ArtifactCount: 1},
		status:  integration.Result{Provider: "opencode", State: integration.StateInstalled, ArtifactSHA256: "changed", ArtifactCount: 1},
	}
	adapter := NewIntegrationProvider(ProviderOpenCode, runtime, integration.Options{ModelEfficient: "openai/fast"})
	plan, err := adapter.Plan(context.Background(), SharedPlan{})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.previewOptions.ModelEfficient != "openai/fast" {
		t.Fatalf("opencode slot override was lost: %#v", runtime.previewOptions)
	}
	result, err := adapter.Apply(context.Background(), plan, SharedResult{})
	if !errors.Is(err, ErrVerification) || result.Verified || !strings.Contains(result.Recovery, "vgxness integrate opencode status") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestIntegrationProviderReportsRecoveryAfterPartialInstallFailure(t *testing.T) {
	failure := errors.New("install failed after mutation")
	runtime := &fakeIntegrationRuntime{install: integration.Result{Provider: "codex", State: integration.StatePartial, Changed: true}, installErr: failure}
	adapter := NewIntegrationProvider(ProviderCodex, runtime, integration.Options{HomeDir: "/home"})
	result, err := adapter.Apply(context.Background(), ProviderPlan{Provider: ProviderCodex, Ready: true, State: integration.StateAbsent}, SharedResult{})
	if !errors.Is(err, failure) || !result.Changed || !strings.Contains(result.Recovery, "vgxness integrate codex status") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestIntegrationProviderAllowsPartialPlanForRepair(t *testing.T) {
	runtime := &fakeIntegrationRuntime{preview: integration.Result{Provider: "codex", State: integration.StatePartial, ArtifactSHA256: "repair", ArtifactCount: 1}}
	plan, err := NewIntegrationProvider(ProviderCodex, runtime, integration.Options{HomeDir: "/home"}).Plan(context.Background(), SharedPlan{})
	if err != nil || !plan.Ready || !plan.Changed || plan.Installed {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestIntegrationProviderPreservesCodexHomeForProtection(t *testing.T) {
	home := t.TempDir()
	adapter := NewIntegrationProvider(ProviderCodex, &fakeIntegrationRuntime{}, integration.Options{ConfigDir: t.TempDir(), HomeDir: home})
	if adapter.options.HomeDir != "" || adapter.protection.(*managedProtection).home != home {
		t.Fatal("Codex backup home was discarded")
	}
}

func TestIntegrationProviderReinstallsPartialCodex(t *testing.T) {
	runtime := &fakeIntegrationRuntime{
		preview:   integration.Result{Provider: "codex", State: integration.StatePartial, ArtifactSHA256: "repair", ArtifactCount: 1},
		reinstall: integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "repair", ArtifactCount: 1},
		status:    integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "repair", ArtifactCount: 1},
	}
	adapter := NewIntegrationProvider(ProviderCodex, runtime, integration.Options{HomeDir: "/home"})
	adapter.protection = &fakeManagedProtection{snapshot: ManagedSnapshot{Skipped: true, Source: fakeSourceIdentity{}}}
	plan, err := adapter.Plan(context.Background(), SharedPlan{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Apply(context.Background(), plan, SharedResult{}); err != nil || runtime.protectedReinstallCalls != 1 {
		t.Fatalf("result err=%v protectedReinstallCalls=%d", err, runtime.protectedReinstallCalls)
	}
}

func TestIntegrationProviderPartialCodexPlanMatchesReinstallStatusIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	runtime := codex.NewIntegration()
	medium := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanMedium}
	if _, err := runtime.Install(context.Background(), medium); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "agents", "general.toml")); err != nil {
		t.Fatal(err)
	}
	want, err := codex.RenderPlan("v0.0.0", sdd.PlanUltra)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewIntegrationProvider(ProviderCodex, runtime, integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra})
	plan, err := adapter.Plan(context.Background(), SharedPlan{})
	if err != nil || plan.State != integration.StatePartial || plan.ArtifactSHA256 != want.SHA256 || plan.ArtifactCount != len(want.Artifacts) {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	result, err := adapter.Apply(context.Background(), plan, SharedResult{})
	if err != nil || !result.Verified {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMultiPreservesVerifiedSkippedOutcomeForThirdRetry(t *testing.T) {
	calls := []string{}
	multi := NewMulti(fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true, Installed: true}, calls: &calls})
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}, ExpectedPlanDigest: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}, ExpectedPlanDigest: plan.Digest, Verified: first.Providers})
	if err != nil || !second.Providers[0].Skipped {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	plan, err = multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	third, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}, ExpectedPlanDigest: plan.Digest, Verified: second.Providers})
	if err != nil || !third.Providers[0].Skipped {
		t.Fatalf("third=%#v err=%v", third, err)
	}
}

func TestMultiApplyStopsOnCancellationAfterCompletedProvider(t *testing.T) {
	calls := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	multi := NewMulti(
		fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, calls: &calls, cancelOnApply: cancel},
		fakeMultiProvider{name: ProviderCodex, plan: ProviderPlan{Ready: true}, calls: &calls},
	)
	plan, err := multi.Plan(ctx, MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := multi.Apply(ctx, MultiOptions{Providers: []Provider{ProviderOpenCode, ProviderCodex}, ExpectedPlanDigest: plan.Digest})
	if !errors.Is(err, context.Canceled) || len(result.Providers) != 1 || !result.Providers[0].Verified {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got := strings.Join(calls, ","); strings.Contains(got, "codex:apply") {
		t.Fatalf("cancelled apply continued: %s", got)
	}
}

func TestMultiApplyRejectsStaleReplanBeforeProviderWrites(t *testing.T) {
	calls := []string{}
	provider := fakeMultiProvider{name: ProviderOpenCode, plan: ProviderPlan{Ready: true}, calls: &calls}
	multi := NewMulti(provider)
	plan, err := multi.Plan(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := multi.Apply(context.Background(), MultiOptions{Providers: []Provider{ProviderOpenCode}, ExpectedPlanDigest: "stale"}); !errors.Is(err, ErrPrerequisite) {
		t.Fatalf("err = %v", err)
	}
	if got := strings.Join(calls, ","); strings.Contains(got, "apply") {
		t.Fatalf("stale preview wrote provider state: %s", got)
	}
	if plan.Digest == "" {
		t.Fatal("plan digest is empty")
	}
}
