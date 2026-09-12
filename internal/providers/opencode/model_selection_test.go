package opencode

import (
	"github.com/vgxness/vgxness/internal/integration"
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
