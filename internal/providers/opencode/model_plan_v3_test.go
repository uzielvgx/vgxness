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

func TestCAREInventoryUsesThreeCurrentRolesOnly(t *testing.T) {
	got := ModelAgentInventoryV3()
	want := map[string]sdd.Role{
		"agents/vgxness-care-reviewer.md":   sdd.RoleCAREReviewer,
		"agents/vgxness-care-specialist.md": sdd.RoleCARESpecialist,
		"agents/vgxness-care-challenger.md": sdd.RoleCAREChallenger,
	}
	seen := map[string]sdd.Role{}
	for _, item := range got {
		seen[item.ArtifactKey] = item.Role
	}
	if len(got) != 13 {
		t.Errorf("current OpenCode inventory has %d agents, want 13", len(got))
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
		{ArtifactKey: "agents/vgxness-care-reviewer.md", Role: sdd.RoleCAREReviewer, Class: sdd.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-care-specialist.md", Role: sdd.RoleCARESpecialist, Class: sdd.ManagedAgentClassReview},
		{ArtifactKey: "agents/vgxness-care-challenger.md", Role: sdd.RoleCAREChallenger, Class: sdd.ManagedAgentClassReview},
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

func TestRequestedModelPlanSameProviderVariantsProjectToV3AndRenderVerbatim(t *testing.T) {
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

func TestV46ManagerPredecessorPreservesTrustedV1Digest(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := previousV46ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	if digest := artifactSHA256(bundle.agents[managerAgentName]); digest != "b264537fd4835478abf416a3ff54ca2901c5e787e9d3f55f924dbb3f5eddc91e" {
		t.Fatalf("trusted manager digest=%s", digest)
	}
}

func TestV46PredecessorIsRecognizedWithAndWithoutManifest(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	v46, err := previousV46ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	if recognized, err := modelPlanBundleForManifest(v46.manifest, current.config); err != nil || artifactSHA256(recognized.agents[managerAgentName]) != artifactSHA256(v46.agents[managerAgentName]) {
		t.Fatalf("manifest recognition = %v, %v", recognized, err)
	}
	predecessors, err := managerPredecessors(current)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, predecessor := range predecessors {
		found = found || artifactSHA256(predecessor) == artifactSHA256(v46.agents[managerAgentName])
	}
	if !found {
		t.Fatal("manifestless predecessor recognition lacks exact v46 manager")
	}
}

func TestV47ManagerPredecessorHasFrozenSHA256(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	v47, err := previousV47ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := artifactSHA256(v47.agents[managerAgentName]), "dcfeebcada320417e5e059dff19cfc74d7b5813167bf73a34a105805ac99f4a5"; got != want {
		t.Fatalf("manager v47 SHA-256 = %s, want %s", got, want)
	}
}

func TestSchemaV3ManifestRecognizesExactV47PredecessorOnly(t *testing.T) {
	// Fixed-lens v47 is a historical V2 package. Do not derive it from the
	// current thirteen-agent CARE V3 inventory.
	current := mustBuildModelPlanV2(t, schemaV2TestConfig(t))
	v47, err := previousV47ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	_, recognized, err := parseInstalledModelPlanManifest(v47.manifest)
	if err != nil || !bytes.Equal(recognized.manifest, v47.manifest) {
		t.Fatalf("v47 schema-v3 manifest rejected: err=%v", err)
	}
	mutated := mutateManifestDigest(t, v47, managerAgentName)
	if _, _, err := parseInstalledModelPlanManifest(mutated); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("mutated v47 schema-v3 manifest error=%v, want drift", err)
	}
}

func TestSchemaV3RecognizesImmediateProfileManifest(t *testing.T) {
	config := sdd.ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Provenance: sdd.ModelPlanCLI, Assignments: completeModelAssignmentsV3()}
	current, err := buildModelPlanBundleV3(config)
	if err != nil {
		t.Fatal(err)
	}
	predecessor, err := immediatePredecessor(current)
	if err != nil {
		t.Fatal(err)
	}
	if _, recognized, err := parseInstalledModelPlanManifest(predecessor.manifest); err != nil || !bytes.Equal(recognized.manifest, predecessor.manifest) {
		t.Fatalf("schema-v3 immediate profile manifest rejected: %v", err)
	}
	if _, _, err := parseInstalledModelPlanManifest(mutateManifestDigest(t, predecessor, generalAgentName)); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("mutated schema-v3 immediate profile manifest error=%v, want drift", err)
	}
}

func TestSchemaV3RecognizesImmediatePromptPredecessorsWithoutNewContext(t *testing.T) {
	// The prompt predecessor is retained as an exact fixed-lens V2 package;
	// current V3 assignments must never be expanded into legacy review roles.
	current := mustBuildModelPlanV2(t, schemaV2TestConfig(t))
	immediate, err := previousV49ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	if _, recognized, err := parseInstalledModelPlanManifest(immediate.manifest); err != nil || !bytes.Equal(recognized.manifest, immediate.manifest) {
		t.Fatalf("schema-v3 immediate predecessor manifest rejected: %v", err)
	}
	for name, marker := range map[string]string{
		managerAgentName: "artifact: opencode-agent/vgxness-manager; version: 49",
		generalAgentName: "artifact: opencode-agent/general; version: 6",
		exploreAgentName: "artifact: opencode-agent/explore; version: 2",
	} {
		content := immediate.agents[name]
		if !bytes.Contains(content, []byte(marker)) || bytes.Contains(content, []byte("Context Capsule v1")) {
			t.Errorf("historical %s is not the exact immediate predecessor", name)
		}
	}
}

func TestSchemaV3PredecessorRecognizesV53V6BeforeOlderTransitions(t *testing.T) {
	config := sdd.ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Provenance: sdd.ModelPlanCLI, Assignments: completeModelAssignmentsV3()}
	current, err := buildModelPlanBundleV3(config)
	if err != nil {
		t.Fatal(err)
	}
	predecessor, err := immediatePredecessor(current)
	if err != nil {
		t.Fatal(err)
	}
	if _, recognized, err := parseInstalledModelPlanManifest(predecessor.manifest); err != nil || !bytes.Equal(recognized.manifest, predecessor.manifest) {
		t.Fatalf("schema-v3 v53/v6 predecessor rejected: %v", err)
	}
	for name, marker := range map[string]string{
		managerAgentName:  "artifact: opencode-agent/vgxness-manager; version: 53",
		verifierAgentName: "artifact: opencode-agent/vgxness-verifier; version: 6",
	} {
		candidates, err := modelBoundAgentPredecessorCandidatesV3(*current.resolvedV3, name)
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) == 0 || !bytes.Contains(candidates[0], []byte(marker)) {
			t.Fatalf("%s does not expose its immediate marker predecessor first", name)
		}
	}
}

func TestModelBoundV3V46ManagerPredecessorIsExact(t *testing.T) {
	plan, err := ResolveModelPlanV3(sdd.ModelPlanConfigV3{
		SchemaVersion: 3,
		Provider:      "acme",
		Provenance:    sdd.ModelPlanCLI,
		Assignments:   completeModelAssignmentsV3(),
	})
	if err != nil {
		t.Fatal(err)
	}
	assignments, err := modelBoundAssignmentsV3(plan)
	if err != nil {
		t.Fatal(err)
	}
	current, err := bindManager(assignments[managerAgentName])
	if err != nil {
		t.Fatal(err)
	}
	v49 := previousManagerV49(current)
	expected := []byte(legacyManagerPrompt(string(v49)))
	predecessors, err := modelBoundAgentPredecessorsV3(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, predecessor := range predecessors[managerAgentName] {
		if bytes.Equal(predecessor, expected) {
			return
		}
	}
	t.Fatal("manifestless v3 predecessors lack exact trusted v46 manager")
}

func TestPreConsolidationV1MediumBundleIsExactAndRejectsMutations(t *testing.T) {
	bundle, err := preConsolidationV1MediumBundle()
	if err != nil {
		t.Fatal(err)
	}
	if digest := artifactSHA256(bundle.agents[managerAgentName]); digest != "d31f50a0a2edb950362240c34deb5ed24a2c58e61339d72e9ed102ecda3b4e55" {
		t.Fatalf("manager digest=%s", digest)
	}
	for name, want := range map[string]string{
		exploreAgentName: "7b6cf0", generalAgentName: "0b9442", reviewReadabilityName: "691d6d", reviewRefuterName: "ee35fc",
		reviewReliabilityName: "48de5a", reviewResilienceName: "c6a7f0", reviewRiskName: "be3b78", sddApplyName: "b36f80",
		sddDesignName: "c7dc18", sddProposalName: "17e203", sddResearchName: "0d5246", sddSpecName: "91cd90",
		sddTasksName: "1ce6e2", verifierAgentName: "1e83df",
	} {
		if got := artifactSHA256(bundle.agents[name]); !strings.HasPrefix(got, want) {
			t.Fatalf("%s digest=%s, want prefix %s", name, got, want)
		}
	}
	if digest := artifactSHA256(bundle.manifest); digest != "bf07e85359a185f4ce05e642b8b6acba950ebb954211549c7a870d8de330364c" {
		t.Fatalf("default-openai-catalog manifest digest=%s", digest)
	}
	if _, err := modelPlanBundleForManifest(bundle.manifest, bundle.config); err != nil {
		t.Fatalf("exact predecessor manifest rejected: %v", err)
	}
	setupCLI := bundle.config
	setupCLI.Provenance = sdd.ModelPlanCLI
	setup, err := preConsolidationV1MediumBundleForConfig(setupCLI)
	if err != nil {
		t.Fatal(err)
	}
	if digest := artifactSHA256(setup.manifest); digest != "6cec48b749f130a6a8fd3221f47188e64dc251f18997771448622c127d24c4d0" {
		t.Fatalf("setup-cli manifest digest=%s", digest)
	}
	if _, err := modelPlanBundleForManifest(setup.manifest, setup.config); err != nil {
		t.Fatalf("setup-cli predecessor manifest rejected: %v", err)
	}
	unknown := setup.config
	unknown.Provenance = "unknown"
	unknownManifest := bytes.Replace(setup.manifest, []byte(`"provenance": "setup-cli"`), []byte(`"provenance": "unknown"`), -1)
	if _, err := modelPlanBundleForManifest(unknownManifest, unknown); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("unknown provenance accepted: %v", err)
	}
	modified := append([]byte(nil), bundle.manifest...)
	modified[len(modified)-2] ^= 1
	if _, err := modelPlanBundleForManifest(modified, bundle.config); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("modified predecessor manifest accepted: %v", err)
	}
}

func TestV52PredecessorPackageRequiresExactBytes(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	predecessor, err := previousV52ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := artifactSHA256(predecessor.agents[managerAgentName]), "759932caf84ef663a7fa5337b249afc27fb5cba2f861e4be56f175279b9575be"; got != want {
		t.Fatalf("manager v52 SHA-256 = %s, want %s", got, want)
	}
	if _, recognized, err := parseInstalledModelPlanManifest(predecessor.manifest); err != nil || !bytes.Equal(recognized.manifest, predecessor.manifest) {
		t.Fatalf("exact v52 package rejected: %v", err)
	}
	if _, _, err := parseInstalledModelPlanManifest(mutateManifestDigest(t, predecessor, generalAgentName)); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("altered v52 package accepted: %v", err)
	}
	mixed := cloneAgents(predecessor.agents)
	v51, err := previousV51ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	mixed[managerAgentName] = v51.agents[managerAgentName]
	mixedBundle, err := encodeLike(predecessor, mixed)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseInstalledModelPlanManifest(mixedBundle.manifest); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("mixed v52 package accepted: %v", err)
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

func TestRequestedModelPlanProjectsFreshDefaultsAndSlotsToV3(t *testing.T) {
	defaults, err := requestedModelPlan(integration.Options{}, t.TempDir())
	if err != nil || defaults.configV3 == nil || defaults.configV2 != nil || defaults.config.SchemaVersion != 0 || len(defaults.configV3.Assignments) != integration.ModelAssignmentCount {
		t.Fatalf("fresh defaults did not select v3: bundle=%+v err=%v", defaults, err)
	}

	slots := integration.Options{
		ModelPlan:      sdd.PlanHigh,
		ModelEfficient: "alpha/efficient", ModelBalanced: "beta/balanced", ModelFrontier: "gamma/frontier",
		ModelEfficientEffort: sdd.EffortLow, ModelBalancedEffort: sdd.EffortHigh, ModelFrontierEffort: sdd.EffortUltra,
		ModelEfficientVariant: "thinking", ModelBalancedVariant: "max", ModelFrontierVariant: "xhigh", ModelVariantsSpecified: true,
	}
	bundle, err := requestedModelPlan(slots, t.TempDir())
	if err != nil || bundle.configV3 == nil || bundle.configV2 != nil || len(bundle.configV3.Assignments) != integration.ModelAssignmentCount {
		t.Fatalf("fresh slots did not select v3: bundle=%+v err=%v", bundle, err)
	}
	for _, identity := range modelAgentInventoryV3 {
		assignment := bundle.configV3.Assignments[identity.ArtifactKey]
		if assignment.Provider == "" || assignment.Reference == "" || assignment.RequestedEffort == "" || !assignment.VariantSpecified || assignment.Source == "" || assignment.Availability == "" {
			t.Fatalf("%s lost slot metadata: %+v", identity.ArtifactKey, assignment)
		}
	}
	if _, ok := bundle.configV3.Assignments["agents/explore.md"]; !ok {
		t.Fatal("core research artifact is missing")
	}
	if _, ok := bundle.configV3.Assignments["agents/vgxness-sdd-research.md"]; !ok {
		t.Fatal("SDD research artifact is missing")
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
