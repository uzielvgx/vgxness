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
	if code != 0 || fake.planCalls != 1 || fake.applyCalls != 0 || stderr.Len() != 0 || !strings.Contains(output, "18 skills y 42 archivos") || !strings.Contains(output, "end-to-end-testing y sdd-lifecycle") || !strings.Contains(output, "Paso 1 de 7") || !strings.Contains(output, "Paso 7 de 7") || !strings.Contains(output, "v1, v2 o v3") || !strings.Contains(output, "sustituciones administradas para Explore y general") || !strings.Contains(output, "workspace de solo lectura y operaciones Git aprobadas por el usuario") || !strings.Contains(output, "Proyección: manager con workspace de solo lectura y operaciones Git aprobadas por el usuario") || !strings.Contains(output, "verificador independiente") || !strings.Contains(output, "MCP administrado") || !strings.Contains(output, "Artefactos administrados: 18") || !strings.Contains(output, "Agente predeterminado: vgxness-manager") || !strings.Contains(output, "no se modificó ningún archivo") {
		t.Fatalf("code=%d calls=%d/%d stdout=%q stderr=%q", code, fake.planCalls, fake.applyCalls, output, stderr.String())
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
	if code != 0 || fake.applyCalls != 1 || stderr.Len() != 0 || !strings.Contains(output, "catálogo global de 23 archivos") || !strings.Contains(output, "skills-creator + stacked-pr + cross-platform + installer-lifecycle + agent-evaluation + ci-triage + security-boundary + documentation-strategy + product-requirements + software-architecture-docs + user-documentation + api-documentation + quality-test-documentation + operations-runbooks + governance-compliance-docs + release-lifecycle-docs + end-to-end-testing + sdd-lifecycle") {
		t.Fatalf("code=%d apply=%d stdout=%q stderr=%q", code, fake.applyCalls, output, stderr.String())
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
	if code != 1 || fake.statusCalls != 1 || fake.applyCalls != 0 || !strings.Contains(stdout.String(), "Estado completo") {
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
		Integration: integration.Result{State: integration.StateAbsent, Path: "/config/agents/vgxness-manager.md", ArtifactCount: 18, ModelPlan: sdd.PlanMedium, ModelProvider: "openai", ModelEfficient: "openai/gpt-5.6-luna-fast", ModelBalanced: "openai/gpt-5.6-terra", ModelFrontier: "openai/gpt-5.6-sol", ManifestPath: "/config/vgxness/model-plan.json", DefaultAgent: "vgxness-manager", DefaultAgentPath: "/config/opencode.json"},
		Skills:      skills.Result{State: skills.StateAbsent, Path: "/shared/skills", FileCount: 22},
		Handshake:   integration.Handshake{OK: ready, Status: integration.HandshakeHealthy},
	}
}
