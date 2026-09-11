package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
)

func TestCAREInventoryUsesThreeCurrentRolesOnly(t *testing.T) {
	got := ModelAgentInventoryV3()
	want := map[string]modelplan.Role{
		"agents/vgxness-care-reviewer.md":   modelplan.RoleCAREReviewer,
		"agents/vgxness-care-specialist.md": modelplan.RoleCARESpecialist,
		"agents/vgxness-care-challenger.md": modelplan.RoleCAREChallenger,
	}
	seen := map[string]modelplan.Role{}
	for _, item := range got {
		seen[item.ArtifactKey] = item.Role
	}
	if len(got) != 7 {
		t.Errorf("current OpenCode inventory has %d agents, want 7", len(got))
	}
	for path, role := range want {
		if seen[path] != role {
			t.Errorf("%s = %s, want %s", path, seen[path], role)
		}
	}
	for _, legacy := range []string{"risk", "readability", "reliability", "resilience", "refuter"} {
		if _, ok := seen["agents/vgxness-review-"+legacy+".md"]; ok {
			t.Errorf("legacy %s artifact remains current", legacy)
		}
	}
}

func completeModelAssignmentsV3() map[string]modelplan.ManagedAgentModelConfig {
	assignments := make(map[string]modelplan.ManagedAgentModelConfig, len(modelAgentInventoryV3))
	efforts := []modelplan.Effort{modelplan.EffortLow, modelplan.EffortMedium, modelplan.EffortHigh, modelplan.EffortUltra}
	for index, identity := range modelAgentInventoryV3 {
		assignments[identity.ArtifactKey] = modelplan.ManagedAgentModelConfig{
			Provider: "acme", Reference: fmt.Sprintf("acme/model-%02d", index), RequestedEffort: efforts[index%len(efforts)],
			Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown,
		}
	}
	return assignments
}

func TestModelAgentInventoryV3IsCanonical(t *testing.T) {
	want := []modelplan.ManagedAgentIdentity{
		{ArtifactKey: "agents/vgxness-manager.md", Role: modelplan.RoleManager, Class: modelplan.ManagedAgentClassCore},
		{ArtifactKey: "agents/explore.md", Role: modelplan.RoleResearch, Class: modelplan.ManagedAgentClassCore},
		{ArtifactKey: "agents/general.md", Role: modelplan.RoleImplementation, Class: modelplan.ManagedAgentClassCore},
		{ArtifactKey: "agents/vgxness-verifier.md", Role: modelplan.RoleVerification, Class: modelplan.ManagedAgentClassCore},
		{ArtifactKey: "agents/vgxness-care-reviewer.md", Role: modelplan.RoleCAREReviewer, Class: modelplan.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-care-specialist.md", Role: modelplan.RoleCARESpecialist, Class: modelplan.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-care-challenger.md", Role: modelplan.RoleCAREChallenger, Class: modelplan.ManagedAgentClassReview},
	}
	if len(want) != integration.ModelAssignmentCount {
		t.Fatalf("inventory count=%d transport count=%d", len(want), integration.ModelAssignmentCount)
	}
	if got := ModelAgentInventoryV3(); !reflect.DeepEqual(got, want) {
		t.Fatalf("inventory=\n%+v\nwant=\n%+v", got, want)
	}
	got := ModelAgentInventoryV3()
	got[0].ArtifactKey = "mutated"
	if ModelAgentInventoryV3()[0].ArtifactKey != want[0].ArtifactKey {
		t.Fatal("inventory escaped by reference")
	}
}

func TestResultModelAssignmentsRejectsNonCanonicalCountAndCopies(t *testing.T) {
	if _, err := resultModelAssignments(make([]modelplan.OpenCodeAgentAssignmentV3, integration.ModelAssignmentCount-1)); !errors.Is(err, integration.ErrInvalid) {
		t.Fatalf("short resolved rows accepted: %v", err)
	}
	rows := make([]modelplan.OpenCodeAgentAssignmentV3, integration.ModelAssignmentCount)
	rows[0].ArtifactKey = "agents/original.md"
	result, err := resultModelAssignments(rows)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].ArtifactKey = "agents/mutated.md"
	if result[0].ArtifactKey != "agents/original.md" {
		t.Fatal("resolved rows escaped by reference")
	}
}

func TestResolveModelPlanV3SupportsHomogeneousAndThreeProviderAssignments(t *testing.T) {
	inventory := ModelAgentInventoryV3()
	assignments := make(map[string]modelplan.ManagedAgentModelConfig, len(inventory))
	efforts := []modelplan.Effort{modelplan.EffortLow, modelplan.EffortMedium, modelplan.EffortHigh, modelplan.EffortUltra}
	for index, identity := range inventory {
		provider := []string{"alpha", "beta", "gamma"}[index%3]
		assignments[identity.ArtifactKey] = modelplan.ManagedAgentModelConfig{
			Provider: provider, Reference: provider + "/model", RequestedEffort: efforts[index%len(efforts)],
			Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown,
		}
	}
	config := modelplan.ModelPlanConfigV3{SchemaVersion: 3, Provider: "mixed", Provenance: modelplan.ModelPlanCLI, Assignments: assignments}
	resolved, err := ResolveModelPlanV3(config)
	if err != nil || resolved.Provider != "mixed" || len(resolved.Assignments) != integration.ModelAssignmentCount {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	for index, assignment := range resolved.Assignments {
		want := assignments[inventory[index].ArtifactKey]
		if assignment.ArtifactKey != inventory[index].ArtifactKey || assignment.Provider != want.Provider || assignment.Model != want.Reference || assignment.RequestedEffort != want.RequestedEffort ||
			assignment.Effort != want.RequestedEffort || assignment.Variant != modelplan.OpenCodeVariantForEffort(want.RequestedEffort) || assignment.Degradation.Degraded {
			t.Fatalf("assignment %d=%+v want=%+v", index, assignment, want)
		}
	}

	for key, assignment := range assignments {
		assignment.Provider, assignment.Reference = "solo", "solo/model"
		assignments[key] = assignment
	}
	config.Provider = "solo"
	if homogeneous, err := ResolveModelPlanV3(config); err != nil || homogeneous.Provider != "solo" {
		t.Fatalf("homogeneous=%+v err=%v", homogeneous, err)
	}

	for key, assignment := range assignments {
		assignment.Provider, assignment.Reference = "openai", "openai/custom"
		assignments[key] = assignment
	}
	catalogKey := inventory[0].ArtifactKey
	assignments[catalogKey] = modelplan.ManagedAgentModelConfig{
		Provider: "openai", Reference: "openai/gpt-5.6-luna", RequestedEffort: modelplan.EffortUltra,
		Source: modelplan.ModelSlotCatalog, Availability: modelplan.ModelSlotCatalogKnown,
	}
	config.Provider = "openai"
	catalog, err := ResolveModelPlanV3(config)
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.Assignments[0]
	if got.ArtifactKey != catalogKey || got.Effort != modelplan.EffortUltra || got.Variant != modelplan.VariantXHigh || got.Degradation.Degraded {
		t.Fatalf("catalog effective assignment=%+v", got)
	}
}

func TestResolveModelPlanV3KeepsDuplicateRolePeersDistinct(t *testing.T) {
	assignments := make(map[string]modelplan.ManagedAgentModelConfig, integration.ModelAssignmentCount)
	for _, identity := range modelAgentInventoryV3 {
		assignments[identity.ArtifactKey] = modelplan.ManagedAgentModelConfig{Provider: "acme", Reference: "acme/default", RequestedEffort: modelplan.EffortMedium, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown}
	}
	explore := assignments["agents/explore.md"]
	explore.Reference = "acme/explore"
	assignments["agents/explore.md"] = explore
	research := assignments["agents/vgxness-sdd-research.md"]
	research.Reference = "acme/sdd-research"
	assignments["agents/vgxness-sdd-research.md"] = research

	resolved, err := modelplan.ResolveOpenCodePlanV3(modelplan.ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Provenance: modelplan.ModelPlanCLI, Assignments: assignments}, modelAgentInventoryV3)
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]string{}
	for _, assignment := range resolved.Assignments {
		models[assignment.ArtifactKey] = assignment.Model
	}
	if models["agents/explore.md"] == models["agents/vgxness-sdd-research.md"] {
		t.Fatalf("research peers collapsed: %+v", models)
	}
}

func TestRequestedModelPlanV3RendersArtifactAssignmentsAndStrictManifest(t *testing.T) {
	assignments := completeModelAssignmentsV3()
	for key, assignment := range assignments {
		assignment.Variant = "thinking"
		assignments[key] = assignment
	}
	bundle, err := requestedModelPlan(integration.Options{ModelAssignments: &assignments}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.configV3 == nil || bundle.resolvedV3 == nil || bundle.configV2 != nil || bundle.resolvedV2 != nil || len(bundle.resolvedV3.Assignments) != integration.ModelAssignmentCount {
		t.Fatalf("bundle=%+v", bundle)
	}
	for _, row := range bundle.resolvedV3.Assignments {
		if strings.Contains(row.ArtifactKey, "vgxness-sdd-") {
			continue
		}
		name := strings.TrimPrefix(row.ArtifactKey, "agents/")
		content := bundle.agents[name]
		if !bytes.Contains(content, []byte("model: "+row.Model+"\nvariant: "+string(row.Variant)+"\n")) {
			t.Fatalf("%s not rendered with %+v: %s", row.ArtifactKey, row, content)
		}
	}
	if bytes.Equal(bundle.agents[exploreAgentName], bundle.agents[sddResearchName]) {
		t.Fatal("duplicate-role artifacts collapsed")
	}

	var document map[string]any
	if err := json.Unmarshal(bundle.manifest, &document); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"artifacts", "configV3", "managedBy", "resolvedV3", "schemaVersion"}
	gotKeys := make([]string, 0, len(document))
	for key := range document {
		gotKeys = append(gotKeys, key)
	}
	if !reflect.DeepEqual(sortedStrings(gotKeys), wantKeys) || document["schemaVersion"] != float64(3) {
		t.Fatalf("manifest keys=%v document=%v", gotKeys, document)
	}

	for name, mutate := range map[string]func(map[string]any){
		"unknown":          func(value map[string]any) { value["unknown"] = true },
		"schema confusion": func(value map[string]any) { value["config"] = modelplan.DefaultModelPlanConfig() },
		"nil artifacts":    func(value map[string]any) { value["artifacts"] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(bundle.manifest, &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeModelPlanManifest(data); !errors.Is(err, integration.ErrDrift) {
				t.Fatalf("malformed v3 accepted: %v", err)
			}
		})
	}
}

func TestRequestedModelPlanV3OmitsEmptyVariant(t *testing.T) {
	assignments := completeModelAssignmentsV3()
	for key, assignment := range assignments {
		assignment.VariantSpecified = true
		assignments[key] = assignment
	}
	bundle, err := requestedModelPlan(integration.Options{ModelAssignments: &assignments}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range bundle.agents {
		if bytes.Contains(content, []byte("variant:")) {
			t.Fatalf("%s renders a provider-default variant: %s", name, content)
		}
	}
}

func TestRequestedModelPlanV3SlotFlagsRequireExactInstalledProjection(t *testing.T) {
	options := integration.Options{
		ModelEfficient: "openai/gpt-5.6-luna", ModelBalanced: "anthropic/claude-sonnet", ModelFrontier: "acme/frontier",
		ModelEfficientEffort: modelplan.EffortLow, ModelBalancedEffort: modelplan.EffortHigh, ModelFrontierEffort: modelplan.EffortUltra,
	}
	directory := t.TempDir()
	first, err := requestedModelPlan(options, directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "vgxness"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "vgxness", modelPlanManifestName), first.manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := requestedModelPlan(options, directory)
	if err != nil || !bytes.Equal(second.manifest, first.manifest) {
		t.Fatalf("same slot flags did not regenerate installed v3 projection: err=%v", err)
	}
	options.ModelFrontier = "other/frontier"
	if _, err := requestedModelPlan(options, directory); !errors.Is(err, integration.ErrInvalid) {
		t.Fatalf("incompatible slot flags error=%v, want invalid", err)
	}
}

func TestRequestedModelPlanSameProviderVariantsProjectToV3AndRenderVerbatim(t *testing.T) {
	bundle, err := requestedModelPlan(integration.Options{
		ModelPlan:              modelplan.PlanMedium,
		ModelEfficient:         "openai/gpt-5.6-luna",
		ModelBalanced:          "openai/gpt-5.6-terra",
		ModelFrontier:          "openai/gpt-5.6-sol",
		ModelEfficientVariant:  "xhigh",
		ModelBalancedVariant:   "max",
		ModelFrontierVariant:   "none",
		ModelVariantsSpecified: true,
	}, t.TempDir())
	if err != nil || bundle.configV2 != nil || bundle.configV3 == nil {
		t.Fatalf("bundle=%+v err=%v", bundle, err)
	}
	for _, variant := range []string{"xhigh", "max", "none"} {
		found := false
		for _, content := range bundle.agents {
			found = found || bytes.Contains(content, []byte("variant: "+variant+"\n"))
		}
		if !found {
			t.Fatalf("variant %q was not rendered verbatim", variant)
		}
	}
}

func TestRequestedModelPlanV3OmitsExplicitEmptyVariants(t *testing.T) {
	bundle, err := requestedModelPlan(integration.Options{ModelVariantsSpecified: true}, t.TempDir())
	if err != nil || bundle.configV3 == nil || bundle.configV2 != nil {
		t.Fatalf("bundle=%+v err=%v", bundle, err)
	}
	for name, content := range bundle.agents {
		if bytes.Contains(content, []byte("variant:")) {
			t.Fatalf("%s renders explicit empty variant: %s", name, content)
		}
	}
}

func TestRequestedModelPlanV2ReferenceOverrideClearsLegacyVariant(t *testing.T) {
	installed, err := modelplan.NewModelPlanConfigV2(modelplan.PlanMedium,
		modelplan.ModelSlotConfig{Reference: "alpha/old", RequestedEffort: modelplan.EffortLow, Variant: "max", VariantSpecified: true, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
		modelplan.ModelSlotConfig{Reference: "beta/balanced", RequestedEffort: modelplan.EffortMedium, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
		modelplan.ModelSlotConfig{Reference: "gamma/frontier", RequestedEffort: modelplan.EffortHigh, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
	)
	if err != nil {
		t.Fatal(err)
	}
	installed.Provenance = modelplan.ModelPlanCLI
	encoded, err := buildModelPlanBundleV2(installed)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "vgxness"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vgxness", modelPlanManifestName), encoded.manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := requestedModelPlan(integration.Options{ModelEfficient: "alpha/new"}, root)
	if err != nil {
		t.Fatal(err)
	}
	slot := bundle.configV2.Slots[modelplan.CapabilityEfficient]
	if slot.Reference != "alpha/new" || slot.Variant != "" || slot.VariantSpecified {
		t.Fatalf("legacy variant survived reference override: %+v", slot)
	}
}

func TestRequestedModelPlanV3RejectsIncompleteAssignments(t *testing.T) {
	for name, mutate := range map[string]func(map[string]modelplan.ManagedAgentModelConfig){
		"missing": func(assignments map[string]modelplan.ManagedAgentModelConfig) {
			delete(assignments, modelAgentInventoryV3[0].ArtifactKey)
		},
		"extra": func(assignments map[string]modelplan.ManagedAgentModelConfig) {
			assignments["agents/extra.md"] = assignments[modelAgentInventoryV3[0].ArtifactKey]
		},
	} {
		t.Run(name, func(t *testing.T) {
			assignments := completeModelAssignmentsV3()
			mutate(assignments)
			_, err := requestedModelPlan(integration.Options{ModelAssignments: &assignments}, t.TempDir())
			if !errors.Is(err, integration.ErrInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRequestedModelPlanProjectsFreshDefaultsAndSlotsToV3(t *testing.T) {
	defaults, err := requestedModelPlan(integration.Options{}, t.TempDir())
	if err != nil || defaults.configV3 == nil || defaults.configV2 != nil || defaults.config.SchemaVersion != 0 || len(defaults.configV3.Assignments) != integration.ModelAssignmentCount {
		t.Fatalf("fresh defaults did not select v3: bundle=%+v err=%v", defaults, err)
	}

	slots := integration.Options{
		ModelPlan:      modelplan.PlanHigh,
		ModelEfficient: "alpha/efficient", ModelBalanced: "beta/balanced", ModelFrontier: "gamma/frontier",
		ModelEfficientEffort: modelplan.EffortLow, ModelBalancedEffort: modelplan.EffortHigh, ModelFrontierEffort: modelplan.EffortUltra,
		ModelEfficientVariant: "thinking", ModelBalancedVariant: "max", ModelFrontierVariant: "xhigh", ModelVariantsSpecified: true,
	}
	bundle, err := requestedModelPlan(slots, t.TempDir())
	if err != nil || bundle.configV3 == nil || bundle.configV2 != nil || len(bundle.configV3.Assignments) != integration.ModelAssignmentCount {
		t.Fatalf("fresh slots did not select v3: bundle=%+v err=%v", bundle, err)
	}
	for _, identity := range ModelAgentInventoryV3() {
		assignment := bundle.configV3.Assignments[identity.ArtifactKey]
		if assignment.Provider == "" || assignment.Reference == "" || assignment.RequestedEffort == "" || !assignment.VariantSpecified || assignment.Source == "" || assignment.Availability == "" {
			t.Fatalf("%s lost slot metadata: %+v", identity.ArtifactKey, assignment)
		}
	}
	if _, ok := bundle.configV3.Assignments["agents/explore.md"]; !ok {
		t.Fatal("core research artifact is missing")
	}
	if _, ok := bundle.configV3.Assignments["agents/vgxness-sdd-research.md"]; ok {
		t.Fatal("retired research artifact remains in a new plan")
	}

	var assignments map[string]modelplan.ManagedAgentModelConfig
	if _, err := requestedModelPlan(integration.Options{ModelAssignments: &assignments}, t.TempDir()); !errors.Is(err, integration.ErrInvalid) {
		t.Fatalf("explicit nil underlying map accepted: %v", err)
	}
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	for index := 1; index < len(result); index++ {
		for cursor := index; cursor > 0 && result[cursor] < result[cursor-1]; cursor-- {
			result[cursor], result[cursor-1] = result[cursor-1], result[cursor]
		}
	}
	return result
}
