package sdd

import (
	"errors"
	"testing"
)

func TestApprovedRoleMatrices(t *testing.T) {
	for _, plan := range []Plan{PlanLow, PlanMedium, PlanHigh} {
		matrix, err := RoleMatrix(plan)
		if err != nil {
			t.Fatalf("RoleMatrix(%s): %v", plan, err)
		}
		if len(matrix) != len(AllRoles()) {
			t.Fatalf("RoleMatrix(%s) has %d roles, want %d", plan, len(matrix), len(AllRoles()))
		}
		manager := matrix[RoleManager]
		for role, assignment := range matrix {
			if role != RoleManager && assignment.Strength() >= manager.Strength() {
				t.Fatalf("%s %s assignment %+v is not weaker than manager %+v", plan, role, assignment, manager)
			}
		}
	}
	matrix, _ := RoleMatrix(PlanHigh)
	frontier := 0
	for _, assignment := range matrix {
		if assignment.Capability == CapabilityFrontier {
			frontier++
		}
	}
	if frontier == len(matrix) {
		t.Fatal("high plan must not assign frontier to every role")
	}
}

func TestResolveModelPlanAndEffortDegradation(t *testing.T) {
	catalog := DefaultOpenAICatalog()
	for index := range catalog.Models {
		if catalog.Models[index].Capability == CapabilityFrontier {
			catalog.Models[index].SupportedEfforts = []Effort{EffortLow, EffortHigh}
		}
	}
	resolved, err := ResolveModelPlan(catalog, PlanHigh)
	if err != nil {
		t.Fatal(err)
	}
	manager := resolved.Roles[RoleManager]
	if manager.Model.Name != "Sol" || manager.RequestedEffort != EffortUltra || manager.Effort != EffortHigh || !manager.Degradation.Degraded {
		t.Fatalf("unexpected manager resolution: %+v", manager)
	}
	if manager.Degradation.Reason == "" {
		t.Fatal("effort degradation must be explicit")
	}
}

func TestResolveModelPlanProviderAndUnknownCatalogRules(t *testing.T) {
	_, err := ResolveModelPlan(Catalog{Provider: "one", Models: []Model{{Provider: "two", ID: "model", Name: "Model", SupportedEfforts: []Effort{EffortLow}}}}, PlanLow)
	if !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("cross-provider model error = %v", err)
	}

	resolved, err := ResolveModelPlan(Catalog{Provider: "local", Models: []Model{{Provider: "local", ID: "only", Name: "Only", SupportedEfforts: []Effort{EffortMedium}}}}, PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Slots) != 3 {
		t.Fatalf("single model mapped to %d slots, want 3", len(resolved.Slots))
	}
	for _, role := range AllRoles() {
		if resolved.Roles[role].Model.ID != "only" {
			t.Fatalf("role %s did not use the only model", role)
		}
	}
}

func TestDefaultOpenAICatalog(t *testing.T) {
	catalog := DefaultOpenAICatalog()
	if catalog.Provider != "openai" || len(catalog.Models) != 3 {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	want := []string{"Luna Fast", "Terra", "Sol"}
	for index, name := range want {
		if catalog.Models[index].Name != name {
			t.Fatalf("model %d = %q, want %q", index, catalog.Models[index].Name, name)
		}
	}
}
