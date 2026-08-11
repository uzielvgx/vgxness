package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/modelcatalog"
	"github.com/vgxness/vgxness/internal/providers/opencode"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/skills"
	"github.com/vgxness/vgxness/internal/testutil"
	"github.com/vgxness/vgxness/internal/tui"
)

type recordingTUIMemoryRuntime struct {
	recall     memory.Recall
	lookup     memory.Lookup
	references []string
}

type recordingTUISetupRuntime struct {
	planOptions  setupflow.Options
	applyOptions setupflow.Options
	plan         setupflow.Plan
	result       setupflow.Result
}

type recordingCatalog struct {
	refresh   bool
	discovers int
	refreshes int
	snapshot  modelcatalog.Snapshot
}

func (catalog *recordingCatalog) Discover(context.Context) (modelcatalog.Snapshot, error) {
	catalog.refresh = false
	catalog.discovers++
	return catalog.snapshot, nil
}

func (catalog *recordingCatalog) Refresh(context.Context) (modelcatalog.Snapshot, error) {
	catalog.refresh = true
	catalog.refreshes++
	return catalog.snapshot, nil
}

func (runtime *recordingTUISetupRuntime) Status(context.Context, setupflow.Options) (setupflow.Plan, error) {
	return runtime.plan, nil
}

func (runtime *recordingTUISetupRuntime) Plan(_ context.Context, options setupflow.Options) (setupflow.Plan, error) {
	runtime.planOptions = options
	return runtime.plan, nil
}

func (runtime *recordingTUISetupRuntime) Apply(_ context.Context, options setupflow.Options) (setupflow.Result, error) {
	runtime.applyOptions = options
	return runtime.result, nil
}

func (*recordingTUIMemoryRuntime) ResolveProject(context.Context, config.Options, string) (string, error) {
	return "project-1", nil
}

func (*recordingTUIMemoryRuntime) Recent(context.Context, config.Options, memory.Recent) ([]memory.Entry, error) {
	return nil, nil
}

func (runtime *recordingTUIMemoryRuntime) Recall(_ context.Context, _ config.Options, request memory.Recall) ([]memory.Entry, error) {
	runtime.recall = request
	return []memory.Entry{{ID: "obs-1", Title: "Decision", Preview: "bounded preview", Type: "architecture", State: memory.StateActive}}, nil
}

func (runtime *recordingTUIMemoryRuntime) Get(_ context.Context, _ config.Options, request memory.Lookup) (memory.Entry, error) {
	runtime.lookup = request
	runtime.references = []string{"obs-prior"}
	return memory.Entry{
		ID: request.ID, Title: "Decision", Content: "Full durable content",
		Project: request.Project, Scope: request.Scope, Type: "architecture",
		State: memory.StateActive, References: runtime.references,
	}, nil
}

func TestMemoryRuntime_ReadAbsentStorageOperationalAndNonMutating(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"token","project":"p","scope":"project"}`), &out, &stderr)
	_, err := os.Stat(root)
	testutil.Require(t, code == 1 && out.Len() == 0 && os.IsNotExist(err), "exit=%d out=%q stat=%v", code, out.String(), err)
}

func TestMemoryRuntime_SaveCloseAndOfflineRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "save", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"title":"T","content":"restart token","project":"p"}`), &out, &stderr)
	testutil.Require(t, code == 0, "save exit=%d stderr=%q", code, stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"restart","project":"p","scope":"project"}`), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "T"), "restart exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestOpenCodeIntegrationRuntime_InstallStatusAndRecoverableUninstall(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"integrate", "opencode", "install", "--model", "openai/gpt-5.6-sol", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=installed") && stderr.Len() == 0, "install exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"integrate", "opencode", "status", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=installed") && strings.Contains(out.String(), "changed=false"), "status exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"integrate", "opencode", "uninstall", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=absent") && strings.Contains(out.String(), "backup="), "uninstall exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestCodexIntegrationRuntime_PreservesConfigToml(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "codex")
	config := []byte("model = \"user-owned\"\n[mcp_servers.user]\ncommand = \"user-tool\"\n")
	if err := os.Mkdir(configDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDirectory, "config.toml"), config, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	for _, action := range []string{"install", "status", "reinstall", "uninstall"} {
		out.Reset()
		stderr.Reset()
		code := Run(context.Background(), []string{"integrate", "codex", action, "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
		testutil.Require(t, code == 0 && stderr.Len() == 0, "%s exit=%d out=%q stderr=%q", action, code, out.String(), stderr.String())
		got, err := os.ReadFile(filepath.Join(configDirectory, "config.toml"))
		testutil.Require(t, err == nil && string(got) == string(config), "%s config=%q err=%v", action, got, err)
	}
}

func TestVersionUsesLightweightPathWithoutWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows prevents removing the active working directory")
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.HasPrefix(stdout.String(), "version=dev\ncommit=unknown\ndate=unknown\n") || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunDispatchesExplicitTUI(t *testing.T) {
	called := 0
	launcher := func(_ context.Context, _ io.Reader, _ io.Writer, _ io.Writer, backend tui.Backend, options tui.Options) int {
		called++
		if backend == nil || options.Workspace == "" {
			t.Fatalf("invalid TUI launch: backend=%v options=%+v", backend, options)
		}
		return 23
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"tui"}, bytes.NewReader(nil), &stdout, &stderr, launcher)
	if code != 23 || called != 1 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d called=%d stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
	}
}

func TestRunRejectsTUIArgumentsWithoutLaunching(t *testing.T) {
	called := 0
	launcher := func(context.Context, io.Reader, io.Writer, io.Writer, tui.Backend, tui.Options) int {
		called++
		return 0
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"tui", "--unknown"}, bytes.NewReader(nil), &stdout, &stderr, launcher)
	if code != 2 || called != 0 || stdout.Len() != 0 || stderr.String() != "usage: vgxness tui\n" {
		t.Fatalf("code=%d called=%d stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
	}
}

func TestRunDispatchesMCPWithoutInitializingTUI(t *testing.T) {
	tuiCalls, mcpCalls := 0, 0
	launcher := func(context.Context, io.Reader, io.Writer, io.Writer, tui.Backend, tui.Options) int {
		tuiCalls++
		return 0
	}
	launchMCP := func(context.Context, []string, io.Reader, io.Writer, io.Writer, string) int {
		mcpCalls++
		return 29
	}
	var stdout, stderr bytes.Buffer
	code := runWithMCP(context.Background(), []string{"mcp"}, bytes.NewReader(nil), &stdout, &stderr, launcher, launchMCP)
	if code != 29 || mcpCalls != 1 || tuiCalls != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d mcpCalls=%d tuiCalls=%d stdout=%q stderr=%q", code, mcpCalls, tuiCalls, stdout.String(), stderr.String())
	}
}

func TestTUIBackendSearchAndDetailStayProjectScoped(t *testing.T) {
	runtime := &recordingTUIMemoryRuntime{}
	backend := tuiBackend{memory: runtime}

	results, err := backend.Search(context.Background(), tui.MemorySearch{Workspace: "/workspace", Query: "architecture reliability", Limit: 12})
	testutil.Require(t, err == nil && len(results) == 1, "search results=%+v err=%v", results, err)
	testutil.Require(t, runtime.recall.Project == "project-1" && runtime.recall.Scope == memory.ScopeProject && runtime.recall.MatchAny && runtime.recall.Limit == 12, "recall=%+v", runtime.recall)
	testutil.Require(t, results[0].ID == "obs-1" && results[0].Preview == "bounded preview", "summary=%+v", results[0])

	detail, err := backend.GetMemory(context.Background(), tui.MemoryLookup{Workspace: "/workspace", ID: "obs-1"})
	testutil.Require(t, err == nil && detail.ID == "obs-1" && detail.Content == "Full durable content", "detail=%+v err=%v", detail, err)
	testutil.Require(t, runtime.lookup.Project == "project-1" && runtime.lookup.Scope == memory.ScopeProject && runtime.lookup.ID == "obs-1", "lookup=%+v", runtime.lookup)
	detail.References[0] = "changed"
	testutil.Require(t, runtime.references[0] == "obs-prior", "detail references alias runtime storage: %+v", runtime.references)
}

func TestTUIBackendSetupPlanAndApplyMapOptionsAndResults(t *testing.T) {
	steps := []setupflow.Step{{Number: 1, Title: "Check", Explanation: "Read only"}, {Number: 2, Title: "Install", Mutates: true}}
	plan := setupflow.Plan{
		Provider: "opencode", Steps: steps, Ready: true,
		SelfInstall: selfinstall.Result{State: selfinstall.StateAbsent, LauncherPath: "/bin/vgxness", Changed: true, UpdateAvailable: true, RollbackAvailable: true, ActiveSHA256: "active", PreviousSHA256: "previous"},
		Integration: integration.Result{
			State: integration.StatePartial, Path: "/config/manager.md", ArtifactCount: 17,
			ModelPlan: sdd.PlanHigh, ModelProvider: "acme", ModelEfficient: "acme/fast",
			ModelBalanced: "acme/balanced", ModelFrontier: "acme/frontier", ModelEfficientEffort: sdd.EffortLow, ModelBalancedEffort: sdd.EffortHigh, ModelFrontierEffort: sdd.EffortUltra, ModelFrontierSource: sdd.ModelSlotCustom, ModelFrontierAvailability: sdd.ModelSlotUnknown, Changed: true, RestartRequired: true,
		},
		Handshake: integration.Handshake{OK: true, Status: integration.HandshakeHealthy},
		Skills:    skills.Result{State: skills.StateInstalled, Changed: true, UpdateNeeded: true},
	}
	runtime := &recordingTUISetupRuntime{plan: plan, result: setupflow.Result{
		Plan: plan, SelfInstall: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/bin/vgxness", UpdateAvailable: true, RollbackAvailable: true, ActiveSHA256: "active", PreviousSHA256: "previous"},
		Integration: integration.Result{State: integration.StateInstalled, Path: "/config/manager.md", ArtifactCount: 17, RestartRequired: true},
		Handshake:   integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, Changed: true, Recovery: "safe recovery",
	}}
	backend := tuiBackend{setup: runtime}

	for _, selected := range []string{"low", "medium", "high"} {
		preview, err := backend.PlanSetup(context.Background(), tui.SetupRequest{Workspace: "workspace/../project", Plan: selected})
		testutil.Require(t, err == nil && preview.Ready && preview.ModelPlan == "high" && len(preview.Steps) == 2, "preview=%+v err=%v", preview, err)
		expectedWorkspace, _ := filepath.Abs("project")
		testutil.Require(t, runtime.planOptions.Workspace == filepath.Clean(expectedWorkspace) && runtime.planOptions.Integration.ModelPlan == sdd.Plan(selected), "selected=%s options=%+v", selected, runtime.planOptions)
	}
	preview, _ := backend.PlanSetup(context.Background(), tui.SetupRequest{Workspace: "/workspace", Plan: "high", ModelEfficient: "openai/fast", ModelBalanced: "anthropic/balanced", ModelFrontier: "acme/frontier", ModelEfficientEffort: "low", ModelBalancedEffort: "high", ModelFrontierEffort: "ultra"})
	testutil.Require(t, runtime.planOptions.Integration.ModelEfficient == "openai/fast" && runtime.planOptions.Integration.ModelBalanced == "anthropic/balanced" && runtime.planOptions.Integration.ModelFrontier == "acme/frontier" && runtime.planOptions.Integration.ModelEfficientEffort == sdd.EffortLow && runtime.planOptions.Integration.ModelBalancedEffort == sdd.EffortHigh && runtime.planOptions.Integration.ModelFrontierEffort == sdd.EffortUltra, "exact profile options=%+v", runtime.planOptions)
	preview.Steps[0].Title = "changed"
	testutil.Require(t, runtime.plan.Steps[0].Title == "Check", "preview steps alias setupflow plan: %+v", runtime.plan.Steps)

	applyWorkspace := filepath.Join(t.TempDir(), "workspace")
	result, err := backend.ApplySetup(context.Background(), tui.SetupRequest{Workspace: applyWorkspace, Plan: "low", ExpectedPlanDigest: "confirmed-digest"})
	testutil.Require(t, err == nil && result.Changed && result.SelfInstallState == "installed" && result.IntegrationState == "installed" && result.ArtifactCount == 17 && result.HandshakeOK && result.RestartRequired && result.Recovery == "safe recovery", "result=%+v err=%v", result, err)
	testutil.Require(t, runtime.applyOptions.Workspace == filepath.Clean(applyWorkspace) && runtime.applyOptions.Integration.ModelPlan == sdd.PlanLow && runtime.applyOptions.ExpectedPlanDigest == "confirmed-digest", "apply options=%+v", runtime.applyOptions)
	result.Plan.Steps[0].Title = "changed"
	testutil.Require(t, runtime.result.Plan.Steps[0].Title == "Check", "result steps alias setupflow result: %+v", runtime.result.Plan.Steps)
	testutil.Require(t, preview.SelfInstallChanged && preview.IntegrationChanged && preview.IntegrationRestartRequired && preview.SkillsChanged && preview.SkillsUpdateNeeded, "preview change signals=%+v", preview)
	testutil.Require(t, preview.ModelFrontierSource == "custom" && preview.ModelFrontierAvailability == "unknown", "frontier mapping=%+v", preview)
	testutil.Require(t, preview.SelfInstallUpdateAvailable && preview.SelfInstallRollbackAvailable && preview.SelfInstallActiveSHA256 == "active" && preview.SelfInstallPreviousSHA256 == "previous", "preview self-install fields=%+v", preview)
	testutil.Require(t, result.SelfInstallUpdateAvailable && result.SelfInstallRollbackAvailable && result.SelfInstallActiveSHA256 == "active" && result.SelfInstallPreviousSHA256 == "previous", "result self-install fields=%+v", result)
	testutil.Require(t,
		result.Plan.ModelEfficient == "acme/fast" && result.Plan.ModelBalanced == "acme/balanced" && result.Plan.ModelFrontier == "acme/frontier" && result.Plan.ModelEfficientEffort == "low" && result.Plan.ModelBalancedEffort == "high" && result.Plan.ModelFrontierEffort == "ultra" &&
			result.Plan.ModelFrontierSource == "custom" && result.Plan.ModelFrontierAvailability == "unknown",
		"result plan lost model slots=%+v", result.Plan)
}

func TestTUISetupAssignmentTransportIsComparableValidatedAndCopied(t *testing.T) {
	var requestRows [tui.SetupModelAssignmentCount]tui.SetupModelAssignmentRequest
	for index, identity := range opencode.ModelAgentInventoryV3() {
		requestRows[index] = tui.SetupModelAssignmentRequest{
			ArtifactKey: identity.ArtifactKey, Provider: "acme", Reference: "acme/model",
			RequestedEffort: "medium", Source: "custom", Availability: "unknown",
		}
	}
	request := tui.SetupRequest{Workspace: "/workspace", Plan: "medium", ModelAssignments: &requestRows}
	_ = map[tui.SetupRequest]bool{request: true}
	options, err := tuiSetupOptions(request)
	testutil.Require(t, err == nil && options.Integration.ModelAssignments != nil && len(*options.Integration.ModelAssignments) == integration.ModelAssignmentCount, "options=%+v err=%v", options, err)
	resolved, resolveErr := sdd.ResolveOpenCodePlanV3(sdd.ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Assignments: *options.Integration.ModelAssignments, Provenance: sdd.ModelPlanCLI}, opencode.ModelAgentInventoryV3())
	first := (*options.Integration.ModelAssignments)[requestRows[0].ArtifactKey]
	testutil.Require(t, resolveErr == nil && first.Variant == "" && !first.VariantSpecified && resolved.Assignments[0].Variant == sdd.VariantMedium, "legacy variant changed config=%+v resolved=%+v err=%v", first, resolved.Assignments[0], resolveErr)
	firstKey := requestRows[0].ArtifactKey
	requestRows[0].Reference = "mutated/request"
	testutil.Require(t, (*options.Integration.ModelAssignments)[firstKey].Reference == "acme/model", "request aliases integration map: %+v", *options.Integration.ModelAssignments)
	assignment := (*options.Integration.ModelAssignments)[firstKey]
	assignment.Reference = "mutated/options"
	(*options.Integration.ModelAssignments)[firstKey] = assignment
	testutil.Require(t, requestRows[0].Reference == "mutated/request", "integration map aliases request rows: %+v", requestRows[0])

	duplicate := requestRows
	duplicate[1].ArtifactKey = duplicate[0].ArtifactKey
	_, err = tuiSetupOptions(tui.SetupRequest{Workspace: "/workspace", Plan: "medium", ModelAssignments: &duplicate})
	testutil.Require(t, err != nil, "duplicate assignments accepted")
	empty := requestRows
	empty[0].ArtifactKey = ""
	_, err = tuiSetupOptions(tui.SetupRequest{Workspace: "/workspace", Plan: "medium", ModelAssignments: &empty})
	testutil.Require(t, err != nil, "incomplete assignments accepted")
	invalid := requestRows
	invalid[0].Reference = "acme/model?query"
	_, err = tuiSetupOptions(tui.SetupRequest{Workspace: "/workspace", ModelAssignments: &invalid})
	testutil.Require(t, err != nil, "invalid reference accepted")
	runtime := &recordingTUISetupRuntime{}
	_, err = (tuiBackend{setup: runtime}).ApplySetup(context.Background(), tui.SetupRequest{Workspace: "/workspace", ModelAssignments: &invalid})
	testutil.Require(t, err != nil && runtime.applyOptions == (setupflow.Options{}), "invalid assignment reached apply: options=%+v err=%v", runtime.applyOptions, err)
}

func TestTUISetupPlanAssignmentRowsAreMappedAndCopied(t *testing.T) {
	var rows [integration.ModelAssignmentCount]sdd.OpenCodeAgentAssignmentV3
	rows[0] = sdd.OpenCodeAgentAssignmentV3{
		ArtifactKey: "agents/vgxness-manager.md", Role: sdd.RoleManager, Class: sdd.ManagedAgentClassCore,
		Provider: "acme", Model: "acme/frontier", RequestedEffort: sdd.EffortUltra, Effort: sdd.EffortHigh,
		Variant: sdd.VariantHigh, Degradation: sdd.Degradation{Degraded: true, Reason: "bounded"}, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown,
	}
	source := setupflow.Plan{Integration: integration.Result{ModelSchemaVersion: 3, ModelAssignments: &rows}}
	plan := tuiSetupPlan(source)
	testutil.Require(t, plan.ModelSchemaVersion == 3 && plan.ModelAssignments != nil, "plan=%+v", plan)
	row := plan.ModelAssignments[0]
	testutil.Require(t, row.ArtifactKey == rows[0].ArtifactKey && row.Role == string(rows[0].Role) && row.Class == string(rows[0].Class) && row.Provider == rows[0].Provider && row.Model == rows[0].Model && row.RequestedEffort == string(rows[0].RequestedEffort) && row.Effort == string(rows[0].Effort) && row.Variant == string(rows[0].Variant) && row.Degraded && row.DegradationReason == "bounded" && row.Source == string(rows[0].Source) && row.Availability == string(rows[0].Availability), "row=%+v", row)
	plan.ModelAssignments[0].Model = "mutated/tui"
	testutil.Require(t, rows[0].Model == "acme/frontier", "TUI plan aliases integration rows: %+v", rows[0])
}

func TestTUISetupVariantsMapBothProfileSchemas(t *testing.T) {
	request := tui.SetupRequest{Workspace: "/workspace", Plan: "medium", ModelEfficientVariant: "xhigh", ModelBalancedVariant: "max", ModelFrontierVariant: "", ModelVariantsSpecified: true}
	options, err := tuiSetupOptions(request)
	testutil.Require(t, err == nil && options.Integration.ModelVariantsSpecified && options.Integration.ModelEfficientVariant == "xhigh" && options.Integration.ModelBalancedVariant == "max" && options.Integration.ModelFrontierVariant == "", "options=%+v err=%v", options, err)
	plan := tuiSetupPlan(setupflow.Plan{Integration: integration.Result{ModelSchemaVersion: 2, ModelEfficient: "acme/fast", ModelBalanced: "acme/balanced", ModelFrontier: "acme/default", ModelEfficientVariant: "xhigh", ModelBalancedVariant: "max", ModelVariantsSpecified: true}})
	testutil.Require(t, plan.ModelEfficientVariant == "xhigh" && plan.ModelBalancedVariant == "max" && plan.ModelVariantsSpecified, "plan=%+v", plan)
}

func TestTUIBackendCatalogMapsNeutralRowsAndRefreshFlag(t *testing.T) {
	catalog := &recordingCatalog{snapshot: modelcatalog.Snapshot{Models: []string{"acme/a:b@c+d", "other/nested/model"}, Variants: map[string][]string{"acme/a:b@c+d": {"xhigh", "max"}, "other/nested/model": {}}}}
	backend := tuiBackend{catalog: catalog}
	rows, err := backend.ModelCatalog(context.Background(), false)
	testutil.Require(t, err == nil && !catalog.refresh && catalog.discovers == 1 && catalog.refreshes == 0 && len(rows) == 2 && rows[0].Provider == "acme" && rows[0].Reference == "acme/a:b@c+d" && len(rows[0].Variants) == 2 && rows[0].Variants[0] == "xhigh" && rows[0].Variants[1] == "max" && rows[0].Source == "custom" && rows[0].Availability == "unknown", "rows=%+v err=%v", rows, err)
	var requestRows [tui.SetupModelAssignmentCount]tui.SetupModelAssignmentRequest
	for index, identity := range opencode.ModelAgentInventoryV3() {
		requestRows[index] = tui.SetupModelAssignmentRequest{ArtifactKey: identity.ArtifactKey, Provider: rows[0].Provider, Reference: rows[0].Reference, RequestedEffort: "ultra", Source: rows[0].Source, Availability: rows[0].Availability}
	}
	options, err := tuiSetupOptions(tui.SetupRequest{Workspace: "/workspace", ModelAssignments: &requestRows})
	resolved, err := sdd.ResolveOpenCodePlanV3(sdd.ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Assignments: *options.Integration.ModelAssignments, Provenance: sdd.ModelPlanCLI}, opencode.ModelAgentInventoryV3())
	testutil.Require(t, err == nil && resolved.Assignments[0].Variant == sdd.VariantXHigh && resolved.Assignments[0].Effort == sdd.EffortUltra && !resolved.Assignments[0].Degradation.Degraded, "resolved=%+v err=%v", resolved, err)
	rows, err = backend.ModelCatalog(context.Background(), true)
	testutil.Require(t, err == nil && catalog.refresh && catalog.discovers == 1 && catalog.refreshes == 1 && rows[0].Reference == "acme/a:b@c+d", "refreshed rows=%+v err=%v", rows, err)
}
