package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/buildinfo"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/providers/pi"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/skills"
	"github.com/vgxness/vgxness/internal/testutil"
)

type fakeSetupRuntime struct {
	plan        setupflow.Plan
	result      setupflow.Result
	planErr     error
	applyErr    error
	statusErr   error
	planCalls   int
	applyCalls  int
	statusCalls int
	options     setupflow.Options
}

func (fake *fakeSetupRuntime) Plan(_ context.Context, options setupflow.Options) (setupflow.Plan, error) {
	fake.planCalls++
	fake.options = options
	return fake.plan, fake.planErr
}
func (fake *fakeSetupRuntime) Apply(_ context.Context, options setupflow.Options) (setupflow.Result, error) {
	fake.applyCalls++
	fake.options = options
	return fake.result, fake.applyErr
}
func (fake *fakeSetupRuntime) Status(_ context.Context, options setupflow.Options) (setupflow.Plan, error) {
	fake.statusCalls++
	fake.options = options
	return fake.plan, fake.statusErr
}

func TestSetupUsageDescribesCurrentCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"invalid"}, {"--preview"}} {
		setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
		var stdout, stderr bytes.Buffer
		code := runSetup(context.Background(), args, strings.NewReader(""), &stdout, &stderr, setup, nil)
		usage := stderr.String()
		testutil.Require(t, code == 2 && stdout.Len() == 0 && setup.openCodePlanCalls == 0 && strings.HasPrefix(usage, "usage: vgxness setup <opencode|codex|pi|all> [--preview|--status] [--yes] "), "args=%v code=%d stdout=%q stderr=%q", args, code, stdout.String(), usage)
		for _, flag := range []string{"--workspace PATH", "--bin-dir PATH", "--data-dir PATH", "--config-dir PATH", "--codex-home PATH", "--model-plan", "--model-mode single|per-agent", "--agent-model ROLE=PROVIDER/MODEL", "--pi-model-mode", "--pi-release-dir PATH|--pi-release-version vSemVer", "--pi-agent-dir PATH", "--pi-root PATH"} {
			testutil.Require(t, strings.Contains(usage, flag), "args=%v usage missing %q: %q", args, flag, usage)
		}
		for _, retired := range []string{"--model-efficient", "--model-balanced", "--model-frontier"} {
			testutil.Require(t, !strings.Contains(usage, retired), "args=%v usage advertises retired %q: %q", args, retired, usage)
		}
	}
}

func TestSetupRejectsRetiredModelSlotsBeforePlanning(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"opencode", "--model-efficient", "openai/a", "--model-balanced", "openai/b", "--model-frontier", "openai/c", "--model-efficient-effort", "low", "--model-balanced-effort", "medium", "--model-frontier-effort", "high"}, "model slots are retired"},
		{[]string{"opencode", "--model-efficient", "openai/a", "--model-balanced", "anthropic/b", "--model-frontier", "acme/c"}, "model slots are retired"},
		{[]string{"all", "--preview", "--model-frontier-effort", "ultra"}, "model slots are retired"},
		{[]string{"opencode", "--preview", "--model-plan", "high"}, "--model-plan applies only to Codex"},
		{[]string{"opencode", "--yes", "--model", "legacy/ignored"}, "provide single or complete per-agent model selection"},
	} {
		setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
		codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent}}
		var stdout, stderr bytes.Buffer
		code := runSetup(context.Background(), test.args, strings.NewReader(""), &stdout, &stderr, setup, codex)
		testutil.Require(t, code == 2 && setup.openCodePlanCalls == 0 && setup.openCodeApplyCalls == 0 && codex.calls == 0 && strings.Contains(stderr.String(), test.want), "args=%v code=%d calls=%d/%d/%d stderr=%q", test.args, code, setup.openCodePlanCalls, setup.openCodeApplyCalls, codex.calls, stderr.String())
	}
}

func TestSetupCodexAcceptsUltraModelPlan(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent, ArtifactSHA256: "codex-plan", ArtifactCount: 2}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"codex", "--preview", "--codex-home", t.TempDir(), "--model-plan", "ultra"}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	testutil.Require(t, code == 0 && stderr.Len() == 0 && codex.calls == 1 && codex.options.ModelPlan == modelplan.PlanUltra, "code=%d codex=%+v stdout=%q stderr=%q", code, codex.options, stdout.String(), stderr.String())
	codex = &fakeIntegrationRuntime{}
	stderr.Reset()
	code = runSetup(context.Background(), []string{"codex", "--preview", "--model-plan", "extreme"}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	testutil.Require(t, code == 2 && codex.calls == 0 && strings.Contains(stderr.String(), "invalid setup arguments"), "code=%d calls=%d stderr=%q", code, codex.calls, stderr.String())
}

func TestSetupWizardPreviewExplainsAllStepsWithoutApplying(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--preview", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	output := stdout.String()
	workspace, err := filepath.Abs("/workspace")
	testutil.Require(t, err == nil && code == 0 && setup.openCodePlanCalls == 1 && setup.openCodeApplyCalls == 0 && stderr.Len() == 0 && setup.openCodeWorkspace == filepath.Clean(workspace), "code=%d calls=%d/%d workspace=%q stdout=%q stderr=%q", code, setup.openCodePlanCalls, setup.openCodeApplyCalls, setup.openCodeWorkspace, output, stderr.String())
	for _, want := range []string{"Provider opencode: ready=true", "Paso 1 de 7 — validar preflight y destinos.", "Paso 7 de 7 — reportar recuperación y activación.", "Resultado: preview completo; no se modificó ningún archivo."} {
		testutil.Require(t, strings.Contains(output, want), "missing %q: %q", want, output)
	}
	testutil.Require(t, !strings.Contains(output, "¿Aplicar"), "preview prompted: %q", output)
}

func TestSetupWizardShowsExactDigestBeforePrompt(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--workspace", "/workspace"}, strings.NewReader("\n"), &stdout, &stderr, setup, nil)
	output := stdout.String()
	digest := regexp.MustCompile(`Plan digest: [0-9a-f]{64}\n`).FindStringIndex(output)
	prompt := strings.Index(output, "¿Aplicar exactamente este plan?")
	testutil.Require(t, code == 0 && stderr.Len() == 0 && setup.openCodeApplyCalls == 0 && digest != nil && prompt > digest[1], "code=%d stdout=%q stderr=%q", code, output, stderr.String())
}

func TestSetupWizardRequiresExplicitConfirmation(t *testing.T) {
	for _, test := range []struct {
		name, input, wantOutput, wantErr string
		wantCode, wantApply              int
	}{
		{name: "default-no", input: "\n", wantOutput: "cancelado por el usuario"},
		{name: "explicit-no", input: "no\n", wantOutput: "cancelado por el usuario"},
		{name: "spanish-yes", input: "sí\n", wantApply: 1, wantOutput: "configuración completa"},
		{name: "short-yes", input: "s\n", wantApply: 1, wantOutput: "configuración completa"},
		{name: "invalid", input: "quizá\n", wantCode: 2, wantErr: "confirmation must be s/si/sí or n/no"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
			var stdout, stderr bytes.Buffer
			code := runSetup(context.Background(), []string{"opencode", "--workspace", "/workspace"}, strings.NewReader(test.input), &stdout, &stderr, setup, nil)
			testutil.Require(t, code == test.wantCode && setup.openCodeApplyCalls == test.wantApply && strings.Contains(stdout.String(), "¿Aplicar exactamente este plan? [s/N]: ") && strings.Contains(stdout.String(), test.wantOutput) && strings.Contains(stderr.String(), test.wantErr) && (test.wantErr != "" || stderr.Len() == 0), "code=%d apply=%d stdout=%q stderr=%q", code, setup.openCodeApplyCalls, stdout.String(), stderr.String())
		})
	}
}

func TestSetupWizardYesAppliesWithoutPrompt(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	output := stdout.String()
	testutil.Require(t, code == 0 && stderr.Len() == 0 && setup.openCodeApplyCalls == 1 && strings.Contains(output, "Confirmación: aceptada mediante --yes.") && !strings.Contains(output, "¿Aplicar") && strings.Contains(output, "Resultado: configuración completa; changed=false.") && strings.Contains(output, "Provider opencode: verified=true changed=false skipped=false"), "code=%d apply=%d stdout=%q stderr=%q", code, setup.openCodeApplyCalls, output, stderr.String())
}

func TestSetupWizardBlocksBeforeConfirmationAndStatusIsNonMutating(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}, openCodePlans: []setupflow.ProviderPlan{{Blocker: "OpenCode no disponible"}}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--workspace", "/workspace"}, strings.NewReader("sí\n"), &stdout, &stderr, setup, nil)
	testutil.Require(t, code == 1 && setup.openCodeApplyCalls == 0 && strings.Contains(stdout.String(), "Resultado: bloqueado sin cambios. OpenCode no disponible") && !strings.Contains(stdout.String(), "¿Aplicar"), "code=%d apply=%d stdout=%q", code, setup.openCodeApplyCalls, stdout.String())

	setup = &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}, openCodeStatus: &setupflow.ProviderPlan{Blocker: "OpenCode drifted", State: integration.StateDrifted}}
	stdout.Reset()
	code = runSetup(context.Background(), []string{"opencode", "--status", "--workspace", "/workspace"}, strings.NewReader("sí\n"), &stdout, &stderr, setup, nil)
	testutil.Require(t, code == 1 && setup.openCodeStatusCalls == 1 && setup.openCodePlanCalls == 0 && setup.openCodeApplyCalls == 0 && strings.Contains(stdout.String(), "Provider opencode: ready=false changed=false state=drifted") && strings.Contains(stdout.String(), "Resultado: requires attention.") && !strings.Contains(stdout.String(), "¿Aplicar"), "status code=%d calls=%d/%d/%d stdout=%q", code, setup.openCodeStatusCalls, setup.openCodePlanCalls, setup.openCodeApplyCalls, stdout.String())
}

func TestSetupWizardReportsProviderPlanErrors(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}, openCodePlanErr: setupflow.ErrInvalid}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	testutil.Require(t, code == 2 && stdout.Len() == 0 && setup.openCodeApplyCalls == 0 && strings.Contains(stderr.String(), "invalid: setup request is invalid"), "code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
}

func TestSetupWizardBindsApplyToConfirmedPlanDigest(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}, openCodePlans: []setupflow.ProviderPlan{{Ready: true}, {Ready: true, Changed: true}}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	testutil.Require(t, code == 1 && setup.openCodePlanCalls == 2 && setup.openCodeApplyCalls == 0 && strings.Contains(stderr.String(), "unavailable: setup prerequisites are not ready") && !strings.Contains(stdout.String(), "configuración completa"), "code=%d calls=%d/%d stdout=%q stderr=%q", code, setup.openCodePlanCalls, setup.openCodeApplyCalls, stdout.String(), stderr.String())
}

func TestSetupWizardRejectsCustomSkillsDirectoryBeforePlanning(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--skills-dir", "/tmp/skills"}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	testutil.Require(t, code == 2 && setup.openCodePlanCalls == 0 && setup.openCodeApplyCalls == 0 && strings.Contains(stderr.String(), "invalid setup arguments"), "code=%d calls=%d/%d stderr=%q", code, setup.openCodePlanCalls, setup.openCodeApplyCalls, stderr.String())
}

func TestRenderModelSlotsWrapsMaximumReferenceWithoutLoss(t *testing.T) {
	reference := strings.Repeat("a", 256) + "/" + strings.Repeat("b", 255)
	result := integration.Result{
		ModelEfficient: reference, ModelEfficientEffort: modelplan.EffortUltra,
		ModelEfficientSource: modelplan.ModelSlotCustom, ModelEfficientAvailability: modelplan.ModelSlotUnknown,
	}
	var output bytes.Buffer
	renderModelSlots(&output, result)
	for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if len(line) > 80 {
			t.Fatalf("model slot line width=%d: %q", len(line), line)
		}
	}
	lines := strings.Split(output.String(), "\n")
	var renderedReference strings.Builder
	collect := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "    ref="):
			collect = true
			renderedReference.WriteString(strings.TrimPrefix(line, "    ref="))
		case collect && strings.HasPrefix(line, "        "):
			renderedReference.WriteString(strings.TrimPrefix(line, "        "))
		case collect:
			collect = false
		}
	}
	if renderedReference.String() != reference || !strings.Contains(output.String(), "effort=ultra") || !strings.Contains(output.String(), "source=custom") || !strings.Contains(output.String(), "availability=unknown") {
		t.Fatalf("wrapped slot lost data: ref-bytes=%d output=%q", renderedReference.Len(), output.String())
	}
}

func TestRenderModelSlotsV3UsesAssignmentOrderAndOmitsLegacySlots(t *testing.T) {
	assignments := new([integration.ModelAssignmentCount]modelplan.OpenCodeAgentAssignmentV3)
	assignments[0] = modelplan.OpenCodeAgentAssignmentV3{ArtifactKey: "agents/first.md", Provider: "alpha", Model: "alpha/first", RequestedEffort: modelplan.EffortUltra, Effort: modelplan.EffortHigh, Variant: modelplan.VariantXHigh, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown, Degradation: modelplan.Degradation{Degraded: true, Reason: "bounded"}}
	assignments[1] = modelplan.OpenCodeAgentAssignmentV3{ArtifactKey: "agents/second.md", Provider: "beta", Model: "beta/second", RequestedEffort: modelplan.EffortLow, Effort: modelplan.EffortLow, Variant: modelplan.VariantLow, Source: modelplan.ModelSlotCatalog, Availability: modelplan.ModelSlotCatalogKnown}
	var output bytes.Buffer
	renderModelSlots(&output, integration.Result{ModelSchemaVersion: 3, ModelAssignments: assignments})
	got := output.String()
	first := "  Assignment artifact_key=agents/first.md provider=alpha model=alpha/first requested_effort=ultra effective_effort=high variant=xhigh source=custom availability=unknown degradation=bounded\n"
	second := "  Assignment artifact_key=agents/second.md provider=beta model=beta/second requested_effort=low effective_effort=low variant=low source=catalog availability=catalog-known\n"
	if !strings.Contains(got, first) || !strings.Contains(got, second) || strings.Index(got, first) > strings.Index(got, second) || strings.Contains(got, "Slot efficient") {
		t.Fatalf("assignments=%q", got)
	}
}

func setupPlanFixture(ready bool) setupflow.Plan {
	return setupflow.Plan{
		Provider: "opencode", Steps: setupflow.OpenCodeSteps(), Ready: ready,
		SelfInstall: selfinstall.Result{State: selfinstall.StateAbsent, LauncherPath: "/stable/vgxness", DataDir: "/data"},
		Integration: integration.Result{State: integration.StateAbsent, Path: "/config/agents/vgxness-manager.md", ArtifactCount: 18, ModelPlan: modelplan.PlanMedium, ModelProvider: "openai", ModelEfficient: "openai/gpt-5.6-luna", ModelBalanced: "openai/gpt-5.6-terra", ModelFrontier: "openai/gpt-5.6-sol", ManifestPath: "/config/vgxness/model-plan.json", DefaultAgent: "vgxness-manager", DefaultAgentPath: "/config/opencode.json"},
		Skills:      skills.Result{State: skills.StateAbsent, Path: "/shared/skills", FileCount: 22},
		Handshake:   integration.Handshake{OK: ready, Status: integration.HandshakeHealthy},
	}
}

func TestSetupWizardAcceptsCodexPreviewThroughMultiCoordinator(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent, ArtifactSHA256: "codex-plan", ArtifactCount: 2}}
	codexHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"codex", "--preview", "--codex-home", codexHome}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	output := stdout.String()
	if code != 0 || codex.calls != 1 || stderr.Len() != 0 || codex.options.HomeDir != codexHome || !strings.Contains(output, "Provider codex: MCP/runtime health=unobserved") || !strings.Contains(output, "reinstall --config-dir <same-codex-home>") || strings.Contains(output, "configuration is healthy") {
		t.Fatalf("code=%d calls=%d codex=%+v stdout=%q stderr=%q", code, codex.calls, codex.options, stdout.String(), stderr.String())
	}
}

func TestSetupWizardAllSanitizesOpenCodeOptionsForCodex(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent, ArtifactSHA256: "codex-plan", ArtifactCount: 2}}
	openCodeConfigDir := t.TempDir()
	codexHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"all", "--preview", "--config-dir", openCodeConfigDir, "--codex-home", codexHome, "--model-mode", "single", "--model", "openai/a"}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	if code != 0 || stderr.Len() != 0 || codex.options.ModelEfficient != "" || codex.options.ModelBalanced != "" || codex.options.ModelFrontier != "" {
		t.Fatalf("code=%d codex=%+v stdout=%q stderr=%q", code, codex.options, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Provider opencode") || !strings.Contains(stdout.String(), "Provider codex") {
		t.Fatalf("missing deterministic provider report: %q", stdout.String())
	}
}

func TestSetupWizardCodexPreviewFailureReportsWithoutApply(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &fakeIntegrationRuntime{err: errors.New("codex unavailable")}
	codexHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"codex", "--preview", "--codex-home", codexHome}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	if code != 1 || codex.calls != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "io:") {
		t.Fatalf("code=%d calls=%d stdout=%q stderr=%q", code, codex.calls, stdout.String(), stderr.String())
	}
}

func TestSetupWizardAllRoutesIndependentProviderRoots(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent, ArtifactSHA256: "codex-plan", ArtifactCount: 2}}
	openCodeConfigDir := t.TempDir()
	codexHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"all", "--preview", "--config-dir", openCodeConfigDir, "--codex-home", codexHome}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	if code != 0 || stderr.Len() != 0 || setup.openCodeOptions.ConfigDir != openCodeConfigDir || codex.options.HomeDir != codexHome {
		t.Fatalf("code=%d opencode=%+v codex=%+v stderr=%q", code, setup.openCodeOptions, codex.options, stderr.String())
	}
}

func TestSetupWizardOpenCodeRetainsConfigDirInMultiFlow(t *testing.T) {
	plan := setupPlanFixture(true)
	plan.Integration.State = integration.StateInstalled
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: plan}}
	openCodeConfigDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--status", "--config-dir", openCodeConfigDir}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	if code != 0 || stderr.Len() != 0 || setup.openCodeOptions.ConfigDir != openCodeConfigDir || !strings.Contains(stdout.String(), "Provider opencode") || !strings.Contains(stdout.String(), "Launcher: state=installed") || !strings.Contains(stdout.String(), "Handshake: ok=true status=healthy") || !strings.Contains(stdout.String(), "Plan de modelos:  provider=mixed manifest=") || !strings.Contains(stdout.String(), "Resultado: configuration is healthy.") {
		t.Fatalf("code=%d opencode=%+v stdout=%q stderr=%q", code, setup.openCodeOptions, stdout.String(), stderr.String())
	}
}

func TestSetupWizardOpenCodeStatusKeepsHandshakeIndependentFromSharedHealth(t *testing.T) {
	shared := setupflow.SharedPlan{Ready: false, Blocker: "shared launcher or skills are unhealthy", Launcher: selfinstall.Result{State: selfinstall.StateDrifted}}
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}, sharedStatus: &shared}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--status", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	if code != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Launcher: state=drifted") || !strings.Contains(stdout.String(), "Handshake: ok=true status=healthy") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSetupWizardOpenCodeRendersGuidedCompatibilityThroughMultiCoordinator(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	output := stdout.String()
	if code != 0 || stderr.Len() != 0 || !strings.Contains(output, "Paso 1 de 7") || !strings.Contains(output, "Paso 7 de 7") || !strings.Contains(output, "handshake OpenCode=healthy") || !strings.Contains(output, "Reinicia OpenCode") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output, stderr.String())
	}
}

func TestSetupWizardRejectsProviderInapplicableRootFlagsBeforePlanning(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent, ArtifactSHA256: "codex-plan", ArtifactCount: 2}}
	for _, args := range [][]string{
		{"codex", "--preview", "--config-dir", "/tmp/opencode"},
		{"opencode", "--preview", "--codex-home", "/tmp/codex"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runSetup(context.Background(), args, strings.NewReader(""), &stdout, &stderr, setup, codex); code != 2 || codex.calls != 0 || !strings.Contains(stderr.String(), "only") {
			t.Fatalf("args=%v code=%d calls=%d stdout=%q stderr=%q", args, code, codex.calls, stdout.String(), stderr.String())
		}
	}
}

func TestSetupWizardStatusRequiresInstalledProviderHealth(t *testing.T) {
	openCodeConfigDir := t.TempDir()
	codexHome := t.TempDir()
	for _, test := range []struct {
		name  string
		state integration.State
		err   error
		code  int
	}{
		{name: "absent", state: integration.StateAbsent, code: 1},
		{name: "installed", state: integration.StateInstalled, code: 0},
		{name: "drifted", state: integration.StateDrifted, code: 1},
		{name: "unavailable", err: errors.New("unavailable"), code: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := setupPlanFixture(true)
			plan.Integration.State = integration.StateInstalled
			setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: plan}}
			codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: test.state, ArtifactSHA256: "codex-plan", ArtifactCount: 2}, err: test.err}
			var stdout, stderr bytes.Buffer
			code := runSetup(context.Background(), []string{"all", "--status", "--config-dir", openCodeConfigDir, "--codex-home", codexHome}, strings.NewReader(""), &stdout, &stderr, setup, codex)
			if code != test.code {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if test.code == 0 && (!strings.Contains(stdout.String(), "managed artifacts and shared setup are healthy") || !strings.Contains(stdout.String(), "Codex MCP/runtime health remains unobserved")) {
				t.Fatalf("healthy status=%q", stdout.String())
			}
			if test.code == 1 && test.err == nil && !strings.Contains(stdout.String(), "requires attention") {
				t.Fatalf("unhealthy status=%q", stdout.String())
			}
		})
	}
}

func TestSetupWizardCodexStatusRequiresSharedHealth(t *testing.T) {
	shared := setupflow.SharedPlan{Ready: false, Blocker: "shared launcher or skills are unhealthy"}
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}, sharedStatus: &shared}
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "codex-plan", ArtifactCount: 2}}
	codexHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"codex", "--status", "--codex-home", codexHome}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	if code != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "requires attention") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSetupWizardCodexStatusSeparatesManagedArtifactsFromUnobservedRuntime(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactSHA256: "codex-plan", ArtifactCount: 13}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"codex", "--status", "--codex-home", t.TempDir()}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	output := stdout.String()
	for _, want := range []string{
		"Provider codex: managed-artifacts state=installed count=13",
		"Provider codex: MCP/runtime health=unobserved (operator-managed config.toml; not inspected)",
		"Recovery codex: repair managed artifacts with `vgxness integrate codex reinstall --config-dir <same-codex-home>`; review config.toml and restart Codex yourself.",
		"Resultado: managed artifacts and shared setup are healthy; Codex MCP/runtime health remains unobserved.",
	} {
		if code != 0 || stderr.Len() != 0 || !strings.Contains(output, want) {
			t.Fatalf("code=%d stdout=%q stderr=%q missing=%q", code, output, stderr.String(), want)
		}
	}
	for _, forbidden := range []string{"configuration is healthy", "codex handshake=healthy", "codex connectivity=healthy", "automatic memory injection"} {
		if strings.Contains(strings.ToLower(output), forbidden) {
			t.Fatalf("Codex status overclaims %q: %q", forbidden, output)
		}
	}
}

func TestSetupWizardApplyFailureRendersPartialOutcomesAndRecovery(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	codex := &installFailingRuntime{fakeIntegrationRuntime: fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent, ArtifactSHA256: "codex-plan", ArtifactCount: 2, Changed: true}}, installErr: errors.New("install failed")}
	codexHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"codex", "--yes", "--codex-home", codexHome}, strings.NewReader(""), &stdout, &stderr, setup, codex)
	if code != 1 || !strings.Contains(stdout.String(), "Provider codex: verified=false changed=true") || !strings.Contains(stdout.String(), "Recovery codex:") || !strings.Contains(stdout.String(), "vgxness integrate codex status") || strings.Contains(stdout.String(), "repair shared") || !strings.Contains(stderr.String(), "io:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

type installFailingRuntime struct {
	fakeIntegrationRuntime
	installErr error
}

func (runtime *installFailingRuntime) Install(_ context.Context, options integration.Options) (integration.Result, error) {
	runtime.calls++
	runtime.action = "install"
	runtime.options = options
	return runtime.result, runtime.installErr
}

type fakeUnifiedSetup struct {
	*fakeSetupRuntime
	openCodeOptions integration.Options
	sharedStatus    *setupflow.SharedPlan
	sharedStatusErr error
	piReady         *bool
	piApplyCalls    int
	// openCodePlans are returned by successive OpenCode Plan calls; the last
	// one repeats. Empty means a ready plan.
	openCodePlans       []setupflow.ProviderPlan
	openCodePlanErr     error
	openCodeStatus      *setupflow.ProviderPlan
	openCodeWorkspace   string
	openCodePlanCalls   int
	openCodeStatusCalls int
	openCodeApplyCalls  int
}

func (fake *fakeUnifiedSetup) Shared(setupflow.Options) setupflow.SharedRuntime {
	return fakeSetupShared{status: fake.sharedStatus, statusErr: fake.sharedStatusErr}
}
func (fake *fakeUnifiedSetup) OpenCodeProvider(options setupflow.Options, _ setupflow.PreviewIntegrationFactory) setupflow.ProviderRuntime {
	fake.openCodeOptions, fake.openCodeWorkspace = options.Integration, options.Workspace
	return fakeSetupProvider{owner: fake}
}

type fakeSetupShared struct {
	status    *setupflow.SharedPlan
	statusErr error
}

func (fakeSetupShared) Plan(context.Context) (setupflow.SharedPlan, error) {
	return setupflow.SharedPlan{Ready: true, Launcher: selfinstall.Result{LauncherPath: "/stable/vgxness"}}, nil
}
func (fake fakeSetupShared) Status(context.Context) (setupflow.SharedPlan, error) {
	if fake.status != nil || fake.statusErr != nil {
		if fake.status == nil {
			return setupflow.SharedPlan{}, fake.statusErr
		}
		return *fake.status, fake.statusErr
	}
	return setupflow.SharedPlan{Ready: true, Launcher: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness"}}, nil
}
func (fakeSetupShared) Apply(context.Context, setupflow.SharedPlan) (setupflow.SharedResult, error) {
	return setupflow.SharedResult{Verified: true, Launcher: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/stable/vgxness"}}, nil
}
func (fakeSetupShared) Finalize(context.Context, setupflow.SharedPlan, setupflow.SharedResult) (setupflow.SharedResult, error) {
	return setupflow.SharedResult{Verified: true}, nil
}

type fakeSetupProvider struct{ owner *fakeUnifiedSetup }

func (fakeSetupProvider) Provider() setupflow.Provider { return setupflow.ProviderOpenCode }
func (provider fakeSetupProvider) Plan(context.Context, setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	owner := provider.owner
	owner.openCodePlanCalls++
	if owner.openCodePlanErr != nil {
		return setupflow.ProviderPlan{}, owner.openCodePlanErr
	}
	if len(owner.openCodePlans) == 0 {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderOpenCode, Ready: true}, nil
	}
	plan := owner.openCodePlans[0]
	if len(owner.openCodePlans) > 1 {
		owner.openCodePlans = owner.openCodePlans[1:]
	}
	return plan, nil
}
func (provider fakeSetupProvider) Status(context.Context, setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	provider.owner.openCodeStatusCalls++
	if provider.owner.openCodeStatus != nil {
		return *provider.owner.openCodeStatus, nil
	}
	return setupflow.ProviderPlan{Provider: setupflow.ProviderOpenCode, Ready: true, Installed: true, State: integration.StateInstalled, Integration: integration.Result{ModelProvider: "mixed"}, Handshake: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}, nil
}
func (provider fakeSetupProvider) Apply(context.Context, setupflow.ProviderPlan, setupflow.SharedResult) (setupflow.ProviderResult, error) {
	provider.owner.openCodeApplyCalls++
	return setupflow.ProviderResult{Provider: setupflow.ProviderOpenCode, Verified: true}, nil
}

func (fake *fakeUnifiedSetup) PiProvider(options pi.Options) setupflow.ProviderRuntime {
	return fakePiSetupProvider{owner: fake}
}

type fakePiSetupProvider struct{ owner *fakeUnifiedSetup }

func (fakePiSetupProvider) Provider() setupflow.Provider { return setupflow.ProviderPi }
func (provider fakePiSetupProvider) Plan(context.Context, setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	ready := true
	if provider.owner.piReady != nil {
		ready = *provider.owner.piReady
	}
	return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Ready: ready, Blocker: func() string {
		if !ready {
			return "injected Pi plan failure"
		}
		return ""
	}()}, nil
}
func (fakePiSetupProvider) Status(context.Context, setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Ready: true, Installed: true, State: integration.StateInstalled}, nil
}
func (provider fakePiSetupProvider) Apply(context.Context, setupflow.ProviderPlan, setupflow.SharedResult) (setupflow.ProviderResult, error) {
	provider.owner.piApplyCalls++
	return setupflow.ProviderResult{Provider: setupflow.ProviderPi, Verified: true}, nil
}

func TestSetupWizardPiOnlyDoesNotRequireCodex(t *testing.T) {
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"pi", "--preview", "--workspace", t.TempDir()}, strings.NewReader(""), &stdout, &stderr, setup, nil)
	if code != 0 || strings.Contains(stderr.String(), "runtime is unavailable") || !strings.Contains(stdout.String(), "Pi release: acquisition required") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestPiAcquisitionRoutingAndCleanup(t *testing.T) {
	oldAcquire := acquirePiRelease
	defer func() { acquirePiRelease = oldAcquire }()
	calls, cleanups := 0, 0
	var requested []string
	acquirePiRelease = func(_ context.Context, value string) (string, func() error, error) {
		calls++
		requested = append(requested, value)
		return t.TempDir(), func() error { cleanups++; return nil }, nil
	}
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	for _, args := range [][]string{{"pi", "--preview"}, {"pi", "--status"}, {"pi", "--yes", "--pi-release-dir", t.TempDir()}} {
		var stdout, stderr bytes.Buffer
		if code := runSetup(context.Background(), args, strings.NewReader(""), &stdout, &stderr, setup, nil); code != 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
	if calls != 0 || cleanups != 0 {
		t.Fatalf("offline/readonly acquired=%d cleanup=%d", calls, cleanups)
	}
	var stdout, stderr bytes.Buffer
	if code := runSetup(context.Background(), []string{"pi", "--yes", "--pi-release-version", "v1.2.3"}, strings.NewReader(""), &stdout, &stderr, setup, nil); code != 0 || calls != 1 || cleanups != 1 || len(requested) != 1 || requested[0] != "v1.2.3" {
		t.Fatalf("apply code=%d calls=%d cleanups=%d stderr=%q", code, calls, cleanups, stderr.String())
	}
}

func TestPiDevelopmentBuildRequiresExplicitPinWithoutAcquire(t *testing.T) {
	oldAcquire := acquirePiRelease
	defer func() { acquirePiRelease = oldAcquire }()
	calls := 0
	acquirePiRelease = func(context.Context, string) (string, func() error, error) {
		calls++
		return "", nil, errors.New("unexpected")
	}
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var stdout, stderr bytes.Buffer
	if code := runSetup(context.Background(), []string{"pi", "--yes"}, strings.NewReader(""), &stdout, &stderr, setup, nil); code != 2 || calls != 0 {
		t.Fatalf("code=%d calls=%d stderr=%q", code, calls, stderr.String())
	}
}

func TestPiAcquireCleanupOnDeclineAndFailedPlan(t *testing.T) {
	old := acquirePiRelease
	defer func() { acquirePiRelease = old }()
	calls, clean := 0, 0
	acquirePiRelease = func(context.Context, string) (string, func() error, error) {
		calls++
		return t.TempDir(), func() error { clean++; return nil }, nil
	}
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var out, err bytes.Buffer
	if code := runSetup(context.Background(), []string{"pi", "--pi-release-version", "v1.2.3"}, strings.NewReader("n\n"), &out, &err, setup, nil); code != 0 || clean != 1 || setup.piApplyCalls != 0 {
		t.Fatalf("decline code=%d clean=%d apply=%d", code, clean, setup.piApplyCalls)
	}
	ready := false
	setup = &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}, piReady: &ready}
	out.Reset()
	err.Reset()
	if code := runSetup(context.Background(), []string{"pi", "--yes", "--pi-release-version", "v1.2.3"}, strings.NewReader(""), &out, &err, setup, nil); code != 1 || clean != 2 || setup.piApplyCalls != 0 {
		t.Fatalf("failed plan code=%d clean=%d apply=%d", code, clean, setup.piApplyCalls)
	}
}

func TestPiAcquireCleanupFailureChangesSuccessfulExit(t *testing.T) {
	old := acquirePiRelease
	defer func() { acquirePiRelease = old }()
	acquirePiRelease = func(context.Context, string) (string, func() error, error) {
		return t.TempDir(), func() error { return errors.New("cleanup failed") }, nil
	}
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var out, err bytes.Buffer
	if code := runSetup(context.Background(), []string{"pi", "--yes", "--pi-release-version", "v1.2.3"}, strings.NewReader(""), &out, &err, setup, nil); code != 1 || setup.piApplyCalls != 1 || !strings.Contains(err.String(), "cleanup failed") {
		t.Fatalf("code=%d apply=%d stderr=%q", code, setup.piApplyCalls, err.String())
	}
}

func TestPiDefaultReleasePinUsesBuildVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v1.2.3"
	defer func() { buildinfo.Version = old }()
	oldAcquire := acquirePiRelease
	defer func() { acquirePiRelease = oldAcquire }()
	var got string
	acquirePiRelease = func(_ context.Context, v string) (string, func() error, error) {
		got = v
		return t.TempDir(), func() error { return nil }, nil
	}
	setup := &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{plan: setupPlanFixture(true)}}
	var out, err bytes.Buffer
	if code := runSetup(context.Background(), []string{"pi", "--yes"}, strings.NewReader(""), &out, &err, setup, nil); code != 0 || got != "v1.2.3" {
		t.Fatalf("code=%d got=%q stderr=%q", code, got, err.String())
	}
}
