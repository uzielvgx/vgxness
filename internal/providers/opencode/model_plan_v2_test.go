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
