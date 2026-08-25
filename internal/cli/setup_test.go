package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
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
	plan.Integration.ModelPlan = sdd.PlanHigh
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
	if code != 0 || stderr.Len() != 0 || fake.options.Integration.ModelPlan != sdd.PlanHigh || fake.options.Integration.ModelFrontier != "acme/frontier" {
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
	plan.Integration.ModelPlan = sdd.PlanUltra
	fake := &fakeSetupRuntime{plan: plan}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--preview", "--model-plan", "ultra"}, strings.NewReader(""), &stdout, &stderr, fake)
	testutil.Require(t, code == 0 && stderr.Len() == 0 && fake.options.Integration.ModelPlan == sdd.PlanUltra && strings.Contains(stdout.String(), "Plan de modelos: ultra"), "exit=%d options=%+v stdout=%q stderr=%q", code, fake.options, stdout.String(), stderr.String())
}

func TestSetupWizardMixedSlotsCarryEffortsAndNeverClaimAuthorization(t *testing.T) {
	plan := setupPlanFixture(true)
	plan.Integration.ModelProvider = "mixed"
	plan.Integration.ModelEfficient, plan.Integration.ModelBalanced, plan.Integration.ModelFrontier = "openai/fast", "anthropic/balanced", "acme/frontier"
	plan.Integration.ModelEfficientEffort, plan.Integration.ModelBalancedEffort, plan.Integration.ModelFrontierEffort = sdd.EffortLow, sdd.EffortHigh, sdd.EffortUltra
	plan.Integration.ModelEfficientSource, plan.Integration.ModelBalancedSource, plan.Integration.ModelFrontierSource = sdd.ModelSlotCatalog, sdd.ModelSlotCustom, sdd.ModelSlotCustom
	plan.Integration.ModelEfficientAvailability, plan.Integration.ModelBalancedAvailability, plan.Integration.ModelFrontierAvailability = sdd.ModelSlotCatalogKnown, sdd.ModelSlotUnknown, sdd.ModelSlotUnknown
	fake := &fakeSetupRuntime{plan: plan}
	var stdout, stderr bytes.Buffer
	code := runSetup(context.Background(), []string{"opencode", "--preview", "--model-efficient", "openai/fast", "--model-balanced", "anthropic/balanced", "--model-frontier", "acme/frontier", "--model-efficient-effort", "low", "--model-balanced-effort", "high", "--model-frontier-effort", "ultra"}, strings.NewReader(""), &stdout, &stderr, fake)
	testutil.Require(t, code == 0 && stderr.Len() == 0 && fake.options.Integration.ModelFrontierEffort == sdd.EffortUltra, "code=%d options=%+v stderr=%q", code, fake.options, stderr.String())
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
	if code != 0 || fake.planCalls != 1 || fake.applyCalls != 0 || stderr.Len() != 0 || !strings.Contains(output, "19 skills y 47 archivos") || !strings.Contains(output, "memory-sync y sdd-lifecycle") || !strings.Contains(output, "Paso 1 de 7") || !strings.Contains(output, "Paso 7 de 7") || !strings.Contains(output, "v1-v10") || !strings.Contains(output, "vgxness.ts") || !strings.Contains(output, "vgxness-autonomous-stacked-pr") || !strings.Contains(output, "sustituciones administradas para Explore y general") || !strings.Contains(output, "workspace de solo lectura y operaciones Git aprobadas por el usuario") || !strings.Contains(output, "Proyección: manager con workspace de solo lectura y operaciones Git aprobadas por el usuario") || !strings.Contains(output, "MCP --full administrado como único runtime") || !strings.Contains(output, "verificador independiente") || !strings.Contains(output, "Artefactos administrados: 18") || !strings.Contains(output, "Agente predeterminado: vgxness-manager") || !strings.Contains(output, "no se modificó ningún archivo") {
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
		ModelEfficient: reference, ModelEfficientEffort: sdd.EffortUltra,
		ModelEfficientSource: sdd.ModelSlotCustom, ModelEfficientAvailability: sdd.ModelSlotUnknown,
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
	assignments := new([integration.ModelAssignmentCount]sdd.OpenCodeAgentAssignmentV3)
	assignments[0] = sdd.OpenCodeAgentAssignmentV3{ArtifactKey: "agents/first.md", Provider: "alpha", Model: "alpha/first", RequestedEffort: sdd.EffortUltra, Effort: sdd.EffortHigh, Variant: sdd.VariantXHigh, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown, Degradation: sdd.Degradation{Degraded: true, Reason: "bounded"}}
	assignments[1] = sdd.OpenCodeAgentAssignmentV3{ArtifactKey: "agents/second.md", Provider: "beta", Model: "beta/second", RequestedEffort: sdd.EffortLow, Effort: sdd.EffortLow, Variant: sdd.VariantLow, Source: sdd.ModelSlotCatalog, Availability: sdd.ModelSlotCatalogKnown}
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
	resultPlan.Integration.ModelEfficientEffort, resultPlan.Integration.ModelBalancedEffort, resultPlan.Integration.ModelFrontierEffort = sdd.EffortLow, sdd.EffortHigh, sdd.EffortUltra
	resultPlan.Integration.ModelEfficientSource, resultPlan.Integration.ModelBalancedSource, resultPlan.Integration.ModelFrontierSource = sdd.ModelSlotCatalog, sdd.ModelSlotCustom, sdd.ModelSlotCustom
	resultPlan.Integration.ModelEfficientAvailability, resultPlan.Integration.ModelBalancedAvailability, resultPlan.Integration.ModelFrontierAvailability = sdd.ModelSlotCatalogKnown, sdd.ModelSlotUnknown, sdd.ModelSlotUnknown
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
	if code != 0 || fake.applyCalls != 1 || stderr.Len() != 0 || !strings.Contains(output, "Paso 3: retiro verificado") || !strings.Contains(output, "v1-v10") || !strings.Contains(output, "vgxness.ts") || !strings.Contains(output, "vgxness-autonomous-stacked-pr") || !strings.Contains(output, "catálogo global de 23 archivos") || !strings.Contains(output, "skills-creator + stacked-pr + cross-platform + installer-lifecycle + agent-evaluation + ci-triage + security-boundary + documentation-strategy + product-requirements + software-architecture-docs + user-documentation + api-documentation + quality-test-documentation + operations-runbooks + governance-compliance-docs + release-lifecycle-docs + end-to-end-testing + memory-sync + sdd-lifecycle") || !strings.Contains(output, "Slot efficient:\n    provider=openai\n    ref=openai/gpt-5.6-luna\n    effort=low\n    source=catalog\n    availability=catalog-known") || !strings.Contains(output, "Slot balanced:\n    provider=openai\n    ref=openai/gpt-5.6-terra\n    effort=high\n    source=custom\n    availability=unknown") || !strings.Contains(output, "Slot frontier:\n    provider=openai\n    ref=openai/gpt-5.6-sol\n    effort=ultra\n    source=custom\n    availability=unknown") {
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
		Integration: integration.Result{State: integration.StateAbsent, Path: "/config/agents/vgxness-manager.md", ArtifactCount: 18, ModelPlan: sdd.PlanMedium, ModelProvider: "openai", ModelEfficient: "openai/gpt-5.6-luna", ModelBalanced: "openai/gpt-5.6-terra", ModelFrontier: "openai/gpt-5.6-sol", ManifestPath: "/config/vgxness/model-plan.json", DefaultAgent: "vgxness-manager", DefaultAgentPath: "/config/opencode.json"},
		Skills:      skills.Result{State: skills.StateAbsent, Path: "/shared/skills", FileCount: 22},
		Handshake:   integration.Handshake{OK: ready, Status: integration.HandshakeHealthy},
	}
}
