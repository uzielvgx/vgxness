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
	plan     SharedPlan
	applyErr error
	calls    *[]string
}

type fakeIntegrationRuntime struct {
	preview, install, reinstall, status integration.Result
	previewOptions                      integration.Options
	installOptions                      integration.Options
	statusOptions                       integration.Options
	reinstallCalls                      int
}

func (f *fakeIntegrationRuntime) Preview(_ context.Context, options integration.Options) (integration.Result, error) {
	f.previewOptions = options
	return f.preview, nil
}
func (f *fakeIntegrationRuntime) Install(_ context.Context, options integration.Options) (integration.Result, error) {
	f.installOptions = options
	return f.install, nil
}
func (f *fakeIntegrationRuntime) Status(_ context.Context, options integration.Options) (integration.Result, error) {
	f.statusOptions = options
	return f.status, nil
}
func (f *fakeIntegrationRuntime) Uninstall(context.Context, integration.Options) (integration.Result, error) {
	return integration.Result{}, nil
}
func (f *fakeIntegrationRuntime) ManagedLayout(context.Context, integration.Options) (integration.ManagedLayout, error) {
	return integration.ManagedLayout{}, nil
}
func (f *fakeIntegrationRuntime) ReinstallPending(context.Context, integration.Options) (bool, error) {
	return false, nil
}
func (f *fakeIntegrationRuntime) Reinstall(_ context.Context, options integration.Options) (integration.Result, error) {
	f.reinstallCalls++
	f.installOptions = options
	return f.reinstall, nil
}

func (f fakeShared) Plan(context.Context) (SharedPlan, error) {
	*f.calls = append(*f.calls, "shared:plan")
	return f.plan, nil
}
func (f fakeShared) Apply(context.Context, SharedPlan) (SharedResult, error) {
	*f.calls = append(*f.calls, "shared:apply")
	return SharedResult{Verified: f.applyErr == nil}, f.applyErr
}

func (f fakeMultiProvider) Provider() Provider { return f.name }
func (f fakeMultiProvider) Plan(context.Context, SharedPlan) (ProviderPlan, error) {
	*f.calls = append(*f.calls, string(f.name)+":plan")
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
	if !errors.Is(err, ErrVerification) || result.Verified {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestIntegrationProviderAllowsPartialPlanForRepair(t *testing.T) {
	runtime := &fakeIntegrationRuntime{preview: integration.Result{Provider: "codex", State: integration.StatePartial, ArtifactSHA256: "repair", ArtifactCount: 1}}
	plan, err := NewIntegrationProvider(ProviderCodex, runtime, integration.Options{HomeDir: "/home"}).Plan(context.Background(), SharedPlan{})
	if err != nil || !plan.Ready || !plan.Changed || plan.Installed {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestIntegrationProviderReinstallsPartialCodex(t *testing.T) {
	runtime := &fakeIntegrationRuntime{
		preview:   integration.Result{Provider: "codex", State: integration.StatePartial, ArtifactSHA256: "repair", ArtifactCount: 1},
		reinstall: integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "repair", ArtifactCount: 1},
		status:    integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "repair", ArtifactCount: 1},
	}
	adapter := NewIntegrationProvider(ProviderCodex, runtime, integration.Options{HomeDir: "/home"})
	plan, err := adapter.Plan(context.Background(), SharedPlan{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Apply(context.Background(), plan, SharedResult{}); err != nil || runtime.reinstallCalls != 1 {
		t.Fatalf("result err=%v reinstallCalls=%d", err, runtime.reinstallCalls)
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
