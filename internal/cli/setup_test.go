package cli

import (
	"bytes"
	"context"
	"errors"
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

func TestSetupWizardModelPlanFlagsAndRestartMessaging(t *testing.T) {
	plan := setupPlanFixture(true)
	plan.Integration.ModelPlan = modelplan.PlanHigh
	plan.Integration.ModelProvider = "acme"
	plan.Integration.ModelEfficient = "acme/fast"
	plan.Integration.ModelBalanced = "acme/balanced"
	plan.Integration.ModelFrontier = "acme/frontier"
	plan.Integration.ManifestPath = "/config/vgxness/model-plan.json"
	plan.Integration.DirectoryDurability = "fsync"
	fake := &fakeSetupRuntime{plan: plan}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{
		"opencode", "--preview", "--workspace", "/workspace", "--model-plan", "high",
		"--model-efficient", "acme/fast", "--model-balanced", "acme/balanced", "--model-frontier", "acme/frontier",
	}, strings.NewReader(""), &stdout, &stderr, fake)
	if code != 0 || stderr.Len() != 0 || fake.options.Integration.ModelPlan != modelplan.PlanHigh || fake.options.Integration.ModelFrontier != "acme/frontier" {
		t.Fatalf("code=%d options=%+v stderr=%q", code, fake.options, stderr.String())
	}
	for _, expected := range []string{"Plan de modelos: high", "acme/fast", "acme/balanced", "acme/frontier", "Durabilidad de directorio: fsync.", "reinicia OpenCode"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("output missing %q: %q", expected, stdout.String())
		}
	}
}

func TestSetupWizardAcceptsUltraModelPlan(t *testing.T) {
	plan := setupPlanFixture(true)
	plan.Integration.ModelPlan = modelplan.PlanUltra
	fake := &fakeSetupRuntime{plan: plan}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--preview", "--model-plan", "ultra"}, strings.NewReader(""), &stdout, &stderr, fake)
	testutil.Require(t, code == 0 && stderr.Len() == 0 && fake.options.Integration.ModelPlan == modelplan.PlanUltra && strings.Contains(stdout.String(), "Plan de modelos: ultra"), "exit=%d options=%+v stdout=%q stderr=%q", code, fake.options, stdout.String(), stderr.String())
}

func TestSetupWizardMixedSlotsCarryEffortsAndNeverClaimAuthorization(t *testing.T) {
	plan := setupPlanFixture(true)
	plan.Integration.ModelProvider = "mixed"
	plan.Integration.ModelEfficient, plan.Integration.ModelBalanced, plan.Integration.ModelFrontier = "openai/fast", "anthropic/balanced", "acme/frontier"
	plan.Integration.ModelEfficientEffort, plan.Integration.ModelBalancedEffort, plan.Integration.ModelFrontierEffort = modelplan.EffortLow, modelplan.EffortHigh, modelplan.EffortUltra
	plan.Integration.ModelEfficientSource, plan.Integration.ModelBalancedSource, plan.Integration.ModelFrontierSource = modelplan.ModelSlotCatalog, modelplan.ModelSlotCustom, modelplan.ModelSlotCustom
	plan.Integration.ModelEfficientAvailability, plan.Integration.ModelBalancedAvailability, plan.Integration.ModelFrontierAvailability = modelplan.ModelSlotCatalogKnown, modelplan.ModelSlotUnknown, modelplan.ModelSlotUnknown
	fake := &fakeSetupRuntime{plan: plan}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--preview", "--model-efficient", "openai/fast", "--model-balanced", "anthropic/balanced", "--model-frontier", "acme/frontier", "--model-efficient-effort", "low", "--model-balanced-effort", "high", "--model-frontier-effort", "ultra"}, strings.NewReader(""), &stdout, &stderr, fake)
	testutil.Require(t, code == 0 && stderr.Len() == 0 && fake.options.Integration.ModelFrontierEffort == modelplan.EffortUltra, "code=%d options=%+v stderr=%q", code, fake.options, stderr.String())
	for _, expected := range []string{
		"Slot efficient:\n    provider=openai\n    ref=openai/fast\n    effort=low\n    source=catalog\n    availability=catalog-known",
		"Slot balanced:\n    provider=anthropic\n    ref=anthropic/balanced\n    effort=high\n    source=custom\n    availability=unknown",
		"Slot frontier:\n    provider=acme\n    ref=acme/frontier\n    effort=ultra\n    source=custom\n    availability=unknown",
	} {
		testutil.Require(t, strings.Contains(stdout.String(), expected), "missing %q: %s", expected, stdout.String())
	}
	testutil.Require(t, !strings.Contains(strings.ToLower(stdout.String()), "authorized"), "output claims authorization: %s", stdout.String())
}

func TestSetupModelReferenceValidationMatchesCatalogGrammar(t *testing.T) {
	valid := []string{
		"provider/model",
		"provider/model:variant@host+feature/nested",
		strings.Repeat("a", 256) + "/" + strings.Repeat("b", 255),
	}
	for _, reference := range valid {
		if !validSetupModelReference(reference) {
			t.Errorf("validSetupModelReference(%q) = false", reference)
		}
	}
	for _, reference := range []string{
		"model", "@provider/model", "provider//model", "provider/model name",
		"provider/model?query", "provider/model#fragment", "provider/model=value", `provider/model\path`,
		strings.Repeat("a", 257) + "/model",
		strings.Repeat("a", 256) + "/" + strings.Repeat("b", 256),
	} {
		if validSetupModelReference(reference) {
			t.Errorf("validSetupModelReference(%q) = true", reference)
		}
	}
}

func TestSetupWizardRejectsHomogeneousEffortOverride(t *testing.T) {
	fake := &fakeSetupRuntime{plan: setupPlanFixture(true)}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--model-efficient", "openai/a", "--model-balanced", "openai/b", "--model-frontier", "openai/c", "--model-efficient-effort", "low", "--model-balanced-effort", "medium", "--model-frontier-effort", "high"}, strings.NewReader(""), &stdout, &stderr, fake)
	testutil.Require(t, code == 2 && fake.planCalls == 0 && strings.Contains(stderr.String(), "per-slot efforts require mixed providers"), "code=%d calls=%d stderr=%q", code, fake.planCalls, stderr.String())
}

func TestSetupWizardRejectsMixedSlotsWithoutAllEffortsBeforePlanning(t *testing.T) {
	fake := &fakeSetupRuntime{plan: setupPlanFixture(true)}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--model-efficient", "openai/a", "--model-balanced", "anthropic/b", "--model-frontier", "acme/c"}, strings.NewReader(""), &stdout, &stderr, fake)
	testutil.Require(t, code == 2 && fake.planCalls == 0 && strings.Contains(stderr.String(), "all refs and efforts"), "code=%d calls=%d stderr=%q", code, fake.planCalls, stderr.String())
}

func TestSetupRendersBestEffortDirectoryDurability(t *testing.T) {
	plan := setupPlanFixture(true)
	plan.Integration.DirectoryDurability = "file-sync-namespace-best-effort"
	var output bytes.Buffer
	renderSetupPlan(&output, plan, "/workspace")
	renderSetupStatus(&output, plan, "/workspace")
	if !strings.Contains(output.String(), "Durabilidad de directorio: mejor esfuerzo") {
		t.Fatalf("output=%q", output.String())
	}
	result := setupflow.Result{Integration: plan.Integration, Plan: plan, Handshake: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}
	fake := &fakeSetupRuntime{plan: plan, result: result}
	output.Reset()
	var stderr bytes.Buffer
	if code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace"}, strings.NewReader(""), &output, &stderr, fake); code != 0 || stderr.Len() != 0 || !strings.Contains(output.String(), "Durabilidad de directorio: mejor esfuerzo") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), stderr.String())
	}
}

func TestSetupWizardPreviewExplainsAllStepsWithoutApplying(t *testing.T) {
	fake := &fakeSetupRuntime{plan: setupPlanFixture(true)}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--preview", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, fake)
	output := stdout.String()
	if code != 0 || fake.planCalls != 1 || fake.applyCalls != 0 || stderr.Len() != 0 || !strings.Contains(output, "18 skills y 46 archivos") || !strings.Contains(output, "memory-sync") || !strings.Contains(output, "Paso 1 de 7") || !strings.Contains(output, "Paso 7 de 7") || !strings.Contains(output, "v1-v10") || !strings.Contains(output, "vgxness.ts") || !strings.Contains(output, "vgxness-autonomous-stacked-pr") || !strings.Contains(output, "sustituciones administradas para Explore y general") || !strings.Contains(output, "workspace de solo lectura y operaciones Git aprobadas por el usuario") || !strings.Contains(output, "Proyección: manager con workspace de solo lectura y operaciones Git aprobadas por el usuario") || !strings.Contains(output, "MCP --full administrado como único runtime") || !strings.Contains(output, "verificador independiente") || !strings.Contains(output, "Artefactos administrados: 18") || !strings.Contains(output, "Agente predeterminado: vgxness-manager") || !strings.Contains(output, "no se modificó ningún archivo") {
		t.Fatalf("code=%d calls=%d/%d stdout=%q stderr=%q", code, fake.planCalls, fake.applyCalls, output, stderr.String())
	}
}

func TestSetupWizardShowsLifecycleActionAndExactDigestBeforePrompt(t *testing.T) {
	for _, test := range []struct {
		name, action string
		plan         setupflow.Plan
	}{
		{name: "install", action: "install", plan: setupPlanFixture(true)},
		{name: "no change", action: "no-change", plan: func() setupflow.Plan {
			plan := setupPlanFixture(true)
			plan.SelfInstall.State, plan.Integration.State, plan.Skills.State = selfinstall.StateInstalled, integration.StateInstalled, skills.StateInstalled
			return plan
		}()},
		{name: "update or reinstall", action: "update/reinstall", plan: func() setupflow.Plan {
			plan := setupPlanFixture(true)
			plan.SelfInstall.State, plan.Integration.State, plan.Skills.State = selfinstall.StateInstalled, integration.StateInstalled, skills.StateInstalled
			plan.Integration.Changed = true
			return plan
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.plan.Digest = strings.Repeat("d", 64)
			fake := &fakeSetupRuntime{plan: test.plan}
			var stdout, stderr bytes.Buffer
			code := runSetup(context.Background(), []string{"opencode", "--workspace", "/workspace"}, strings.NewReader("\n"), &stdout, &stderr, fake)
			output := stdout.String()
			action := "Lifecycle action: " + test.action
			digest := "Plan digest: " + test.plan.Digest
			prompt := "¿Aplicar exactamente este plan?"
			if code != 0 || stderr.Len() != 0 || !strings.Contains(output, action) || !strings.Contains(output, digest) || strings.Index(output, action) > strings.Index(output, prompt) || strings.Index(output, digest) > strings.Index(output, prompt) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, output, stderr.String())
			}
		})
	}
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

func TestSetupWizardRequiresExplicitConfirmation(t *testing.T) {
	for _, test := range []struct {
		name       string
		input      string
		wantApply  int
		wantOutput string
	}{
		{name: "default-no", input: "\n", wantOutput: "cancelado por el usuario"},
		{name: "explicit-no", input: "no\n", wantOutput: "cancelado por el usuario"},
		{name: "spanish-yes", input: "sí\n", wantApply: 1, wantOutput: "configuración completa"},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := setupPlanFixture(true)
			fake := &fakeSetupRuntime{plan: plan, result: setupflow.Result{
				SelfInstall: selfinstall.Result{LauncherPath: "/stable/vgxness"},
				Integration: integration.Result{Path: "/config/agent", ArtifactCount: 18},
				Handshake:   integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, Changed: true,
			}}
			var stdout, stderr bytes.Buffer
			code := runSetup(context.Background(), []string{"opencode", "--workspace", "/workspace"}, strings.NewReader(test.input), &stdout, &stderr, fake)
			if code != 0 || fake.applyCalls != test.wantApply || stderr.Len() != 0 || !strings.Contains(stdout.String(), test.wantOutput) {
				t.Fatalf("code=%d apply=%d stdout=%q stderr=%q", code, fake.applyCalls, stdout.String(), stderr.String())
			}
		})
	}
}

func TestSetupWizardSuccessfulApplyReportsGlobalSkillCatalog(t *testing.T) {
	plan := setupPlanFixture(true)
	resultPlan := plan
	resultPlan.Skills.FileCount = 23
	resultPlan.Integration.ModelEfficientEffort, resultPlan.Integration.ModelBalancedEffort, resultPlan.Integration.ModelFrontierEffort = modelplan.EffortLow, modelplan.EffortHigh, modelplan.EffortUltra
	resultPlan.Integration.ModelEfficientSource, resultPlan.Integration.ModelBalancedSource, resultPlan.Integration.ModelFrontierSource = modelplan.ModelSlotCatalog, modelplan.ModelSlotCustom, modelplan.ModelSlotCustom
	resultPlan.Integration.ModelEfficientAvailability, resultPlan.Integration.ModelBalancedAvailability, resultPlan.Integration.ModelFrontierAvailability = modelplan.ModelSlotCatalogKnown, modelplan.ModelSlotUnknown, modelplan.ModelSlotUnknown
	fake := &fakeSetupRuntime{
		plan: plan,
		result: setupflow.Result{
			Plan:        resultPlan,
			SelfInstall: selfinstall.Result{LauncherPath: "/stable/vgxness"},
			Integration: integration.Result{ArtifactCount: 18, ManifestPath: "/config/vgxness/model-plan.json"},
			Handshake:   integration.Handshake{OK: true, Status: integration.HandshakeHealthy},
			Changed:     true,
		},
	}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, fake)
	output := stdout.String()
	if code != 0 || fake.applyCalls != 1 || stderr.Len() != 0 || !strings.Contains(output, "Paso 3: retiro verificado") || !strings.Contains(output, "v1-v10") || !strings.Contains(output, "vgxness.ts") || !strings.Contains(output, "vgxness-autonomous-stacked-pr") || !strings.Contains(output, "catálogo global de 23 archivos") || !strings.Contains(output, "skills-creator + git-delivery + cross-platform + installer-lifecycle + agent-evaluation + ci-triage + security-boundary + documentation-strategy + product-requirements + software-architecture-docs + user-documentation + api-documentation + quality-test-documentation + operations-runbooks + governance-compliance-docs + release-lifecycle-docs + end-to-end-testing + memory-sync") || !strings.Contains(output, "Slot efficient:\n    provider=openai\n    ref=openai/gpt-5.6-luna\n    effort=low\n    source=catalog\n    availability=catalog-known") || !strings.Contains(output, "Slot balanced:\n    provider=openai\n    ref=openai/gpt-5.6-terra\n    effort=high\n    source=custom\n    availability=unknown") || !strings.Contains(output, "Slot frontier:\n    provider=openai\n    ref=openai/gpt-5.6-sol\n    effort=ultra\n    source=custom\n    availability=unknown") {
		t.Fatalf("code=%d apply=%d stdout=%q stderr=%q", code, fake.applyCalls, output, stderr.String())
	}
}

func TestSetupUsageListsModelSlotFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"invalid"}, strings.NewReader(""), &stdout, &stderr, &fakeSetupRuntime{})
	for _, flag := range []string{"--model-efficient", "--model-balanced", "--model-frontier", "--model-efficient-effort", "--model-balanced-effort", "--model-frontier-effort"} {
		if code != 2 || !strings.Contains(stderr.String(), flag) {
			t.Fatalf("code=%d missing %q: %q", code, flag, stderr.String())
		}
	}
}

func TestSetupWizardBlocksBeforeConfirmationAndStatusIsNonMutating(t *testing.T) {
	blocked := setupPlanFixture(false)
	blocked.Blocker = "OpenCode no disponible"
	fake := &fakeSetupRuntime{plan: blocked}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--workspace", "/workspace"}, strings.NewReader("sí\n"), &stdout, &stderr, fake)
	if code != 1 || fake.applyCalls != 0 || !strings.Contains(stdout.String(), "bloqueado sin cambios") {
		t.Fatalf("code=%d apply=%d stdout=%q", code, fake.applyCalls, stdout.String())
	}
	stdout.Reset()
	code = runSetup(context.Background(), []string{"opencode", "--status", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, fake)
	if code != 1 || fake.statusCalls != 1 || fake.applyCalls != 0 || !strings.Contains(stdout.String(), "Estado completo") || !strings.Contains(stdout.String(), "projection=agents+mcp-full") || strings.Contains(stdout.String(), "plugin") {
		t.Fatalf("status code=%d calls=%d/%d stdout=%q", code, fake.statusCalls, fake.applyCalls, stdout.String())
	}
}

func TestSetupWizardDoesNotRequireASecondaryModel(t *testing.T) {
	fake := &fakeSetupRuntime{plan: setupPlanFixture(true)}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace", "--model", "legacy/ignored"}, strings.NewReader(""), &stdout, &stderr, fake)
	if code != 0 || fake.planCalls != 1 || fake.applyCalls != 1 || stderr.Len() != 0 {
		t.Fatalf("code=%d calls=%d/%d stdout=%q stderr=%q", code, fake.planCalls, fake.applyCalls, stdout.String(), stderr.String())
	}
}

func TestSetupWizardBindsApplyToReturnedPlanDigest(t *testing.T) {
	plan := setupPlanFixture(true)
	plan.Digest = "confirmed-digest"
	fake := &fakeSetupRuntime{plan: plan, result: setupflow.Result{Plan: plan, Handshake: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--yes", "--workspace", "/workspace"}, strings.NewReader(""), &stdout, &stderr, fake)
	if code != 0 || fake.options.ExpectedPlanDigest != plan.Digest {
		t.Fatalf("code=%d options=%+v stderr=%q", code, fake.options, stderr.String())
	}
}

func TestSetupWizardRejectsCustomSkillsDirectoryBeforePlanning(t *testing.T) {
	fake := &fakeSetupRuntime{plan: setupPlanFixture(true)}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--skills-dir", "/tmp/skills"}, strings.NewReader(""), &stdout, &stderr, fake)
	if code != 2 || fake.planCalls != 0 || fake.applyCalls != 0 || !strings.Contains(stderr.String(), "invalid setup arguments") {
		t.Fatalf("code=%d calls=%d/%d stderr=%q", code, fake.planCalls, fake.applyCalls, stderr.String())
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
	code := runSetup(context.Background(), []string{"all", "--preview", "--config-dir", openCodeConfigDir, "--codex-home", codexHome, "--model-efficient", "openai/a", "--model-balanced", "openai/b", "--model-frontier", "openai/c"}, strings.NewReader(""), &stdout, &stderr, setup, codex)
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
}

func (fake *fakeUnifiedSetup) Shared(setupflow.Options) setupflow.SharedRuntime {
	return fakeSetupShared{status: fake.sharedStatus, statusErr: fake.sharedStatusErr}
}
func (fake *fakeUnifiedSetup) OpenCodeProvider(options setupflow.Options, _ setupflow.PreviewIntegrationFactory) setupflow.ProviderRuntime {
	fake.openCodeOptions = options.Integration
	return fakeSetupProvider{}
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

type fakeSetupProvider struct{}

func (fakeSetupProvider) Provider() setupflow.Provider { return setupflow.ProviderOpenCode }
func (fakeSetupProvider) Plan(context.Context, setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	return setupflow.ProviderPlan{Provider: setupflow.ProviderOpenCode, Ready: true}, nil
}
func (fakeSetupProvider) Status(context.Context, setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	return setupflow.ProviderPlan{Provider: setupflow.ProviderOpenCode, Ready: true, Installed: true, State: integration.StateInstalled, Integration: integration.Result{ModelProvider: "mixed"}, Handshake: integration.Handshake{OK: true, Status: integration.HandshakeHealthy}}, nil
}
func (fakeSetupProvider) Apply(context.Context, setupflow.ProviderPlan, setupflow.SharedResult) (setupflow.ProviderResult, error) {
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
