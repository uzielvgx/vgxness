package opencode

import (
	"bytes"
	"errors"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestSchemaV2ManifestRecognizesExactV47PredecessorOnly(t *testing.T) {
	config := schemaV2TestConfig(t)
	current, err := buildModelPlanBundleV2(config)
	if err != nil {
		t.Fatal(err)
	}
	v47, err := previousV47ModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	_, recognized, err := parseInstalledModelPlanManifest(v47.manifest)
	if err != nil || !bytes.Equal(recognized.manifest, v47.manifest) {
		t.Fatalf("v47 schema-v2 manifest rejected: err=%v", err)
	}
	mutated := mutateManifestDigest(t, v47, managerAgentName)
	if _, _, err := parseInstalledModelPlanManifest(mutated); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("mutated v47 schema-v2 manifest error=%v, want drift", err)
	}
}

func TestSchemaV2RecognizesImmediateProfileManifest(t *testing.T) {
	current := mustBuildModelPlanV2(t, schemaV2TestConfig(t))
	predecessor, err := previousActiveProfilesModelPlanBundle(current)
	if err != nil {
		t.Fatal(err)
	}
	if _, recognized, err := parseInstalledModelPlanManifest(predecessor.manifest); err != nil || !bytes.Equal(recognized.manifest, predecessor.manifest) {
		t.Fatalf("schema-v2 immediate profile manifest rejected: %v", err)
	}
	if _, _, err := parseInstalledModelPlanManifest(mutateManifestDigest(t, predecessor, generalAgentName)); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("mutated schema-v2 immediate profile manifest error=%v, want drift", err)
	}
}

func TestSchemaV2ImmediatePromptPredecessorKeepsModelBindings(t *testing.T) {
	for _, current := range []modelPlanBundle{
		mustBuildModelPlanV2(t, schemaV2TestConfig(t)),
		mustRequestModelPlan(t, integration.Options{
			ModelPlan: sdd.PlanHigh, ModelEfficient: "openai/gpt-5.6-luna", ModelBalanced: "anthropic/claude-sonnet", ModelFrontier: "acme/frontier",
			ModelEfficientEffort: sdd.EffortLow, ModelBalancedEffort: sdd.EffortHigh, ModelFrontierEffort: sdd.EffortUltra,
			ModelEfficientVariant: "xhigh", ModelBalancedVariant: "max", ModelVariantsSpecified: true,
		}),
	} {
		predecessor, err := previousV49ModelPlanBundle(current)
		if err != nil {
			t.Fatal(err)
		}
		if _, recognized, err := parseInstalledModelPlanManifest(predecessor.manifest); err != nil || !bytes.Equal(recognized.manifest, predecessor.manifest) {
			t.Fatalf("schema-v2 immediate predecessor manifest rejected: %v", err)
		}
		if _, err := managerPredecessors(current); err != nil {
			t.Fatalf("schema-v2 manager predecessor candidates: %v", err)
		}
		for name, marker := range map[string]string{
			managerAgentName: "artifact: opencode-agent/vgxness-manager; version: 49",
			generalAgentName: "artifact: opencode-agent/general; version: 6",
			exploreAgentName: "artifact: opencode-agent/explore; version: 2",
		} {
			if !bytes.Contains(predecessor.agents[name], []byte(marker)) || bytes.Contains(predecessor.agents[name], []byte("Context Capsule v1")) {
				t.Errorf("schema-v2 %s is not the exact immediate predecessor", name)
			}
		}
	}
}

func mustBuildModelPlanV2(t *testing.T, config sdd.ModelPlanConfigV2) modelPlanBundle {
	t.Helper()
	bundle, err := buildModelPlanBundleV2(config)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func mustRequestModelPlan(t *testing.T, options integration.Options) modelPlanBundle {
	t.Helper()
	bundle, err := requestedModelPlan(options, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func schemaV2TestConfig(t *testing.T) sdd.ModelPlanConfigV2 {
	t.Helper()
	config, err := sdd.NewModelPlanConfigV2(sdd.PlanMedium,
		sdd.ModelSlotConfig{Reference: "alpha/efficient", RequestedEffort: sdd.EffortLow, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown},
		sdd.ModelSlotConfig{Reference: "beta/balanced", RequestedEffort: sdd.EffortMedium, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown},
		sdd.ModelSlotConfig{Reference: "gamma/frontier", RequestedEffort: sdd.EffortHigh, Source: sdd.ModelSlotCustom, Availability: sdd.ModelSlotUnknown},
	)
	if err != nil {
		t.Fatal(err)
	}
	config.Provenance = sdd.ModelPlanCLI
	return config
}

func mutateManifestDigest(t *testing.T, bundle modelPlanBundle, agent string) []byte {
	t.Helper()
	digest := []byte(artifactSHA256(bundle.agents[agent]))
	if bytes.Count(bundle.manifest, digest) != 1 {
		t.Fatalf("manifest digest count for %s = %d", agent, bytes.Count(bundle.manifest, digest))
	}
	replacement := append([]byte(nil), digest...)
	if replacement[0] == '0' {
		replacement[0] = '1'
	} else {
		replacement[0] = '0'
	}
	return bytes.Replace(bundle.manifest, digest, replacement, 1)
}
