package sdd

import (
	"errors"
	"testing"
)

func TestDefaultModelPlanConfiguration(t *testing.T) {
	config := DefaultModelPlanConfig()
	if config.ActivePlan != PlanMedium || config.Provider != "openai" ||
		config.Efficient != "openai/gpt-5.6-luna" ||
		config.Balanced != "openai/gpt-5.6-terra" ||
		config.Frontier != "openai/gpt-5.6-sol" || config.Provenance != ModelPlanDefault {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	resolved, err := ResolveOpenCodePlan(config)
	if err != nil || len(resolved.Roles) != len(AllRoles()) {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	if resolved.Roles[RoleManager].Model != config.Frontier || resolved.Roles[RoleManager].Variant != VariantHigh {
		t.Fatalf("manager=%+v", resolved.Roles[RoleManager])
	}
}

func TestOpenCodePlanVariantsAndMatrices(t *testing.T) {
	for _, plan := range []Plan{PlanLow, PlanMedium, PlanHigh, PlanUltra} {
		config := DefaultModelPlanConfig()
		config.ActivePlan = plan
		resolved, err := ResolveOpenCodePlan(config)
		if err != nil {
			t.Fatalf("resolve %s: %v", plan, err)
		}
		manager := resolved.Roles[RoleManager]
		for role, assignment := range resolved.Roles {
			if role != RoleManager && assignment.Strength >= manager.Strength {
				t.Fatalf("%s role %s is not weaker than manager: %+v >= %+v", plan, role, assignment, manager)
			}
			if assignment.Variant != VariantLow && assignment.Variant != VariantMedium && assignment.Variant != VariantHigh && assignment.Variant != VariantXHigh {
				t.Fatalf("unsupported variant: %+v", assignment)
			}
		}
	}
	high := DefaultModelPlanConfig()
	high.ActivePlan = PlanHigh
	resolved, _ := ResolveOpenCodePlan(high)
	mixed := false
	for _, assignment := range resolved.Roles {
		if assignment.Capability != CapabilityFrontier {
			mixed = true
		}
	}
	if !mixed || resolved.Roles[RoleManager].Variant != VariantXHigh {
		t.Fatalf("high plan is not mixed or manager is not xhigh: %+v", resolved)
	}
	readability := resolved.Roles[RoleReadability]
	if readability.Degradation.Degraded || readability.RequestedEffort != EffortHigh || readability.Effort != EffortHigh || readability.Variant != VariantHigh {
		t.Fatalf("efficient-slot degradation was not preserved: %+v", readability)
	}
}

func TestModelPlanCustomSlotsAndValidation(t *testing.T) {
	config, err := NewModelPlanConfig(PlanHigh, "acme/fast", "acme/balanced", "acme/frontier")
	if err != nil || config.Provider != "acme" || config.Provenance != ModelPlanCLI {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	resolved, err := ResolveOpenCodePlan(config)
	if err != nil || resolved.Slots[CapabilityEfficient] != "acme/fast" || resolved.Slots[CapabilityFrontier] != "acme/frontier" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	for _, invalid := range []ModelPlanConfig{
		{ActivePlan: PlanMedium, Efficient: "openai/fast", Balanced: "other/balanced", Frontier: "openai/frontier"},
		{ActivePlan: PlanMedium, Efficient: "openai/fast?key=secret", Balanced: "openai/balanced", Frontier: "openai/frontier"},
		{ActivePlan: PlanMedium, Efficient: "https://model", Balanced: "openai/balanced", Frontier: "openai/frontier"},
		{ActivePlan: Plan("other"), Efficient: "openai/fast", Balanced: "openai/balanced", Frontier: "openai/frontier"},
	} {
		if _, err := ResolveOpenCodePlan(invalid); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrProviderMismatch) {
			t.Fatalf("invalid config accepted: %+v err=%v", invalid, err)
		}
	}
}

func TestUltraMapsConservativelyToXHigh(t *testing.T) {
	if got := OpenCodeVariantForEffort(EffortUltra); got != VariantXHigh {
		t.Fatalf("ultra variant=%q", got)
	}
}
