package opencode

import (
	"reflect"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
)

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
	if got := ModelAgentInventoryV3(); !reflect.DeepEqual(got, want) {
		t.Fatalf("inventory=\n%+v\nwant=\n%+v", got, want)
	}
	got := ModelAgentInventoryV3()
	got[0].ArtifactKey = "mutated"
	if ModelAgentInventoryV3()[0].ArtifactKey != want[0].ArtifactKey {
		t.Fatal("inventory escaped by reference")
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
	if err != nil || resolved.Provider != "mixed" || len(resolved.Assignments) != 15 {
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
	assignments := make(map[string]sdd.ManagedAgentModelConfig, 15)
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
