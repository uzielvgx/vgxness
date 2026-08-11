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
	"github.com/vgxness/vgxness/internal/sdd"
)

func completeModelAssignmentsV3() map[string]sdd.ManagedAgentModelConfig {
	assignments := make(map[string]sdd.ManagedAgentModelConfig, len(modelAgentInventoryV3))
	efforts := []sdd.Effort{sdd.EffortLow, sdd.EffortMedium, sdd.EffortHigh, sdd.EffortUltra}
	for index, identity := range modelAgentInventoryV3 {
		assignments[identity.ArtifactKey] = sdd.ManagedAgentModelConfig{
			Provider: "acme", Reference: fmt.Sprintf("acme/model-%02d", index), RequestedEffort: efforts[index%len(efforts)],
			Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown,
		}
	}
	return assignments
}

func TestModelAgentInventoryV3IsCanonical(t *testing.T) {
	want := []sdd.ManagedAgentIdentity{
		{ArtifactKey: "agents/vgxness-manager.md", Role: sdd.RoleManager, Class: sdd.ManagedAgentClassCore},
		{ArtifactKey: "agents/explore.md", Role: sdd.RoleResearch, Class: sdd.ManagedAgentClassCore},
		{ArtifactKey: "agents/general.md", Role: sdd.RoleImplementation, Class: sdd.ManagedAgentClassCore},
		{ArtifactKey: "agents/vgxness-verifier.md", Role: sdd.RoleVerification, Class: sdd.ManagedAgentClassCore},
		{ArtifactKey: "agents/vgxness-review-risk.md", Role: sdd.RoleRisk, Class: sdd.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-review-readability.md", Role: sdd.RoleReadability, Class: sdd.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-review-reliability.md", Role: sdd.RoleReliability, Class: sdd.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-review-resilience.md", Role: sdd.RoleResilience, Class: sdd.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-review-refuter.md", Role: sdd.RoleRefuter, Class: sdd.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-sdd-research.md", Role: sdd.RoleResearch, Class: sdd.ManagedAgentClassSDD},
		{ArtifactKey: "agents/vgxness-sdd-proposal.md", Role: sdd.RoleProposal, Class: sdd.ManagedAgentClassSDD},
		{ArtifactKey: "agents/vgxness-sdd-spec.md", Role: sdd.RoleSpec, Class: sdd.ManagedAgentClassSDD},
		{ArtifactKey: "agents/vgxness-sdd-design.md", Role: sdd.RoleDesign, Class: sdd.ManagedAgentClassSDD},
		{ArtifactKey: "agents/vgxness-sdd-tasks.md", Role: sdd.RoleTasks, Class: sdd.ManagedAgentClassSDD},
		{ArtifactKey: "agents/vgxness-sdd-apply.md", Role: sdd.RoleApply, Class: sdd.ManagedAgentClassSDD},
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
	if _, err := resultModelAssignments(make([]sdd.OpenCodeAgentAssignmentV3, integration.ModelAssignmentCount-1)); !errors.Is(err, integration.ErrInvalid) {
		t.Fatalf("short resolved rows accepted: %v", err)
	}
	rows := make([]sdd.OpenCodeAgentAssignmentV3, integration.ModelAssignmentCount)
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
	assignments := make(map[string]sdd.ManagedAgentModelConfig, len(inventory))
	efforts := []sdd.Effort{sdd.EffortLow, sdd.EffortMedium, sdd.EffortHigh, sdd.EffortUltra}
	for index, identity := range inventory {
		provider := []string{"alpha", "beta", "gamma"}[index%3]
		assignments[identity.ArtifactKey] = sdd.ManagedAgentModelConfig{
			Provider: provider, Reference: provider + "/model", RequestedEffort: efforts[index%len(efforts)],
			Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown,
		}
	}
	config := sdd.ModelPlanConfigV3{SchemaVersion: 3, Provider: "mixed", Provenance: sdd.ModelPlanCLI, Assignments: assignments}
	resolved, err := ResolveModelPlanV3(config)
	if err != nil || resolved.Provider != "mixed" || len(resolved.Assignments) != integration.ModelAssignmentCount {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	for index, assignment := range resolved.Assignments {
		want := assignments[inventory[index].ArtifactKey]
		if assignment.ArtifactKey != inventory[index].ArtifactKey || assignment.Provider != want.Provider || assignment.Model != want.Reference || assignment.RequestedEffort != want.RequestedEffort ||
			assignment.Effort != want.RequestedEffort || assignment.Variant != sdd.OpenCodeVariantForEffort(want.RequestedEffort) || assignment.Degradation.Degraded {
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
	assignments[catalogKey] = sdd.ManagedAgentModelConfig{
		Provider: "openai", Reference: "openai/gpt-5.6-luna", RequestedEffort: sdd.EffortUltra,
		Source: sdd.ModelSlotCatalog, Availability: sdd.ModelSlotCatalogKnown,
	}
	config.Provider = "openai"
	catalog, err := ResolveModelPlanV3(config)
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.Assignments[0]
	if got.ArtifactKey != catalogKey || got.Effort != sdd.EffortUltra || got.Variant != sdd.VariantXHigh || got.Degradation.Degraded {
		t.Fatalf("catalog effective assignment=%+v", got)
	}
}

func TestResolveModelPlanV3KeepsDuplicateRolePeersDistinct(t *testing.T) {
	assignments := make(map[string]sdd.ManagedAgentModelConfig, integration.ModelAssignmentCount)
	for _, identity := range ModelAgentInventoryV3() {
		assignments[identity.ArtifactKey] = sdd.ManagedAgentModelConfig{Provider: "acme", Reference: "acme/default", RequestedEffort: sdd.EffortMedium, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown}
	}
	explore := assignments["agents/explore.md"]
	explore.Reference = "acme/explore"
	assignments["agents/explore.md"] = explore
	research := assignments["agents/vgxness-sdd-research.md"]
	research.Reference = "acme/sdd-research"
	assignments["agents/vgxness-sdd-research.md"] = research

	resolved, err := ResolveModelPlanV3(sdd.ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Provenance: sdd.ModelPlanCLI, Assignments: assignments})
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
		"schema confusion": func(value map[string]any) { value["config"] = sdd.DefaultModelPlanConfig() },
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

func TestOmitEmptyVariantLinesRemovesOnlyFirstLine(t *testing.T) {
	agents := map[string][]byte{"agent.md": []byte("variant: \nvariant: \n")}
	got := omitEmptyVariantLines(agents)["agent.md"]
	if want := []byte("variant: \n"); !bytes.Equal(got, want) {
		t.Fatalf("content=%q want=%q", got, want)
	}
}

func TestRequestedModelPlanSameProviderVariantsUseV2AndRenderVerbatim(t *testing.T) {
	bundle, err := requestedModelPlan(integration.Options{
		ModelPlan:              sdd.PlanMedium,
		ModelEfficient:         "openai/gpt-5.6-luna",
		ModelBalanced:          "openai/gpt-5.6-terra",
		ModelFrontier:          "openai/gpt-5.6-sol",
		ModelEfficientVariant:  "xhigh",
		ModelBalancedVariant:   "max",
		ModelFrontierVariant:   "none",
		ModelVariantsSpecified: true,
	}, t.TempDir())
	if err != nil || bundle.configV2 == nil || bundle.configV3 != nil {
		t.Fatalf("bundle=%+v err=%v", bundle, err)
	}
	for capability, variant := range map[sdd.Capability]sdd.OpenCodeVariant{
		sdd.CapabilityEfficient: "xhigh", sdd.CapabilityBalanced: "max", sdd.CapabilityFrontier: "none",
	} {
		slot := bundle.configV2.Slots[capability]
		if slot.Variant != variant || !slot.VariantSpecified || slot.RequestedEffort != sdd.EffortMedium {
			t.Fatalf("%s slot=%+v", capability, slot)
		}
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

func TestRequestedModelPlanV2OmitsExplicitEmptyVariants(t *testing.T) {
	bundle, err := requestedModelPlan(integration.Options{ModelVariantsSpecified: true}, t.TempDir())
	if err != nil || bundle.configV2 == nil {
		t.Fatalf("bundle=%+v err=%v", bundle, err)
	}
	for name, content := range bundle.agents {
		if bytes.Contains(content, []byte("variant:")) {
			t.Fatalf("%s renders explicit empty variant: %s", name, content)
		}
	}
}

func TestRequestedModelPlanV2ReferenceOverrideClearsLegacyVariant(t *testing.T) {
	installed, err := sdd.NewModelPlanConfigV2(sdd.PlanMedium,
		sdd.ModelSlotConfig{Reference: "alpha/old", RequestedEffort: sdd.EffortLow, Variant: "max", VariantSpecified: true, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown},
		sdd.ModelSlotConfig{Reference: "beta/balanced", RequestedEffort: sdd.EffortMedium, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown},
		sdd.ModelSlotConfig{Reference: "gamma/frontier", RequestedEffort: sdd.EffortHigh, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown},
	)
	if err != nil {
		t.Fatal(err)
	}
	installed.Provenance = sdd.ModelPlanCLI
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
	slot := bundle.configV2.Slots[sdd.CapabilityEfficient]
	if slot.Reference != "alpha/new" || slot.Variant != "" || slot.VariantSpecified {
		t.Fatalf("legacy variant survived reference override: %+v", slot)
	}
}

func TestModelBoundAgentsPreserveTrustedV1Digest(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	if digest := artifactSHA256(bundle.agents[managerAgentName]); digest != "d31f50a0a2edb950362240c34deb5ed24a2c58e61339d72e9ed102ecda3b4e55" {
		t.Fatalf("trusted manager digest=%s", digest)
	}
}

func TestRequestedModelPlanV3RejectsIncompleteAssignments(t *testing.T) {
	for name, mutate := range map[string]func(map[string]sdd.ManagedAgentModelConfig){
		"missing": func(assignments map[string]sdd.ManagedAgentModelConfig) {
			delete(assignments, modelAgentInventoryV3[0].ArtifactKey)
		},
		"extra": func(assignments map[string]sdd.ManagedAgentModelConfig) {
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

func TestRequestedModelPlanV3PresenceDistinguishesNilPointer(t *testing.T) {
	legacy, err := requestedModelPlan(integration.Options{}, t.TempDir())
	if err != nil || legacy.configV3 != nil || legacy.config.SchemaVersion != 1 {
		t.Fatalf("nil pointer did not preserve legacy selection: bundle=%+v err=%v", legacy, err)
	}
	var assignments map[string]sdd.ManagedAgentModelConfig
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
