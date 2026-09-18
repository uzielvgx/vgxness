package opencode

import (
	"github.com/vgxness/vgxness/internal/agentmodels"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"testing"
)

func TestFreshOpenCodeUsesOneModelWithoutPlan(t *testing.T) {
	bundle, err := requestedModelPlan(integration.Options{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.configV3 == nil || len(bundle.configV3.Assignments) != 7 {
		t.Fatal("missing explicit default assignments")
	}
	model := ""
	for _, a := range bundle.configV3.Assignments {
		if model == "" {
			model = a.Reference
		}
		if a.Reference != model || a.Variant != "" || !a.VariantSpecified {
			t.Fatalf("default still uses a capability plan: %+v", a)
		}
	}
}

func TestExplicitModelsPreservesDiscoveredVariantTokens(t *testing.T) {
	config, err := agentmodels.Single("openai/gpt-5.6-terra", "xhigh")
	if err != nil {
		t.Fatal(err)
	}
	for role, assignment := range config.Assignments {
		assignment.Variant = "max"
		config.Assignments[role] = assignment
	}
	models, err := ExplicitModels(config)
	if err != nil {
		t.Fatal(err)
	}
	manager := models["agents/vgxness-manager.md"]
	if manager.Reference != "openai/gpt-5.6-terra" || manager.RequestedEffort != modelplan.EffortUltra || manager.Variant != modelplan.OpenCodeVariant("max") || !manager.VariantSpecified {
		t.Fatalf("explicit discovered variant was not preserved: %+v", manager)
	}

	derived, err := agentmodels.Single("acme/fast", "xhigh")
	if err != nil {
		t.Fatal(err)
	}
	models, err = ExplicitModels(derived)
	if err != nil {
		t.Fatal(err)
	}
	if manager := models["agents/vgxness-manager.md"]; manager.Variant != modelplan.VariantXHigh {
		t.Fatalf("effort-derived variant changed: %+v", manager)
	}
}
