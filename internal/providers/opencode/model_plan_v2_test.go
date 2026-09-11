package opencode

import (
	"bytes"

	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
)

func mustBuildModelPlanV2(t *testing.T, config modelplan.ModelPlanConfigV2) modelPlanBundle {
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

func schemaV2TestConfig(t *testing.T) modelplan.ModelPlanConfigV2 {
	t.Helper()
	config, err := modelplan.NewModelPlanConfigV2(modelplan.PlanMedium,
		modelplan.ModelSlotConfig{Reference: "alpha/efficient", RequestedEffort: modelplan.EffortLow, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
		modelplan.ModelSlotConfig{Reference: "beta/balanced", RequestedEffort: modelplan.EffortMedium, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
		modelplan.ModelSlotConfig{Reference: "gamma/frontier", RequestedEffort: modelplan.EffortHigh, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
	)
	if err != nil {
		t.Fatal(err)
	}
	config.Provenance = modelplan.ModelPlanCLI
	return config
}

func schemaV2ImmediatePromptPredecessorConfig(t *testing.T) modelplan.ModelPlanConfigV2 {
	t.Helper()
	config, err := modelplan.NewModelPlanConfigV2(modelplan.PlanHigh,
		modelplan.ModelSlotConfig{Reference: "openai/gpt-5.6-luna", RequestedEffort: modelplan.EffortLow, Variant: "xhigh", VariantSpecified: true, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
		modelplan.ModelSlotConfig{Reference: "anthropic/claude-sonnet", RequestedEffort: modelplan.EffortHigh, Variant: "max", VariantSpecified: true, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
		modelplan.ModelSlotConfig{Reference: "acme/frontier", RequestedEffort: modelplan.EffortUltra, VariantSpecified: true, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown},
	)
	if err != nil {
		t.Fatal(err)
	}
	config.Provenance = modelplan.ModelPlanCLI
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
