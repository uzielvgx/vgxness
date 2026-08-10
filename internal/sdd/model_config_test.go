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

func TestResolveOpenCodePlanV2MixedProvidersAndIndependentEfforts(t *testing.T) {
	config := ModelPlanConfigV2{SchemaVersion: 2, ActivePlan: PlanMedium, Provider: "mixed", Provenance: ModelPlanCLI, Slots: map[Capability]ModelSlotConfig{
		CapabilityEfficient: {Reference: "openai/gpt-5.6-luna", RequestedEffort: EffortLow, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
		CapabilityBalanced:  {Reference: "acme/balanced", RequestedEffort: EffortUltra, Source: ModelSlotCustom, Availability: ModelSlotUnknown},
		CapabilityFrontier:  {Reference: "openai/gpt-5.6-sol", RequestedEffort: EffortUltra, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
	}}
	resolved, err := ResolveOpenCodePlanV2(config)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Provider != "mixed" || resolved.Roles[RoleResearch].Provider != "openai" || resolved.Roles[RoleManager].Provider != "openai" {
		t.Fatalf("providers not preserved: %+v", resolved)
	}
	if got := resolved.Roles[RoleProposal]; got.Provider != "acme" || got.Model != "acme/balanced" || got.Effort != EffortUltra || got.Degradation.Degraded {
		t.Fatalf("custom assignment=%+v", got)
	}
	if got := resolved.Roles[RoleManager]; got.Effort != EffortUltra || got.Degradation.Degraded {
		t.Fatalf("frontier assignment=%+v", got)
	}
}

func TestResolveOpenCodePlanV2Validation(t *testing.T) {
	base := DefaultModelPlanConfigV2()
	delete(base.Slots, CapabilityFrontier)
	if _, err := ResolveOpenCodePlanV2(base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing slot error=%v", err)
	}
	base = DefaultModelPlanConfigV2()
	base.Slots[CapabilityEfficient] = ModelSlotConfig{Reference: "acme/fast", RequestedEffort: EffortLow, Source: ModelSlotCustom, Availability: ModelSlotCatalogKnown}
	if _, err := ResolveOpenCodePlanV2(base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("custom availability error=%v", err)
	}
	base = DefaultModelPlanConfigV2()
	base.Slots[Capability("extra")] = base.Slots[CapabilityEfficient]
	if _, err := ResolveOpenCodePlanV2(base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("extra slot error=%v", err)
	}
	if _, err := NewModelPlanConfig(PlanMedium, "openai/fast", "acme/balanced", "openai/frontier"); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("v1 mixed provider error=%v", err)
	}
	if _, err := ResolveOpenCodePlan(DefaultModelPlanConfig()); err != nil {
		t.Fatalf("v1 regression: %v", err)
	}
}

func TestModelPlanConfigV2ProviderSummaryIsDerivedAndValidated(t *testing.T) {
	defaults := DefaultModelPlanConfigV2()
	if defaults.Provider != "openai" {
		t.Fatalf("default provider=%q", defaults.Provider)
	}
	mixed, err := NewModelPlanConfigV2(PlanMedium,
		ModelSlotConfig{Reference: "openai/gpt-5.6-luna", RequestedEffort: EffortLow, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
		ModelSlotConfig{Reference: "acme/balanced", RequestedEffort: EffortMedium, Source: ModelSlotCustom, Availability: ModelSlotUnknown},
		ModelSlotConfig{Reference: "openai/gpt-5.6-sol", RequestedEffort: EffortHigh, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
	)
	if err != nil || mixed.Provider != "mixed" {
		t.Fatalf("mixed config=%+v err=%v", mixed, err)
	}
	mixed.Provider = "openai"
	if _, err := ResolveOpenCodePlanV2(mixed); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("serialized provider mismatch error=%v", err)
	}
}

func TestResolveOpenCodePlanV2UsesAuthoritativeSlotEfforts(t *testing.T) {
	config := DefaultModelPlanConfigV2()
	config.ActivePlan = PlanHigh
	resolved, err := ResolveOpenCodePlanV2(config)
	if err != nil {
		t.Fatal(err)
	}
	manager := resolved.Roles[RoleManager]
	if manager.RequestedEffort != EffortMedium || manager.Effort != EffortMedium || manager.Strength != capabilityRank(CapabilityFrontier)*10+effortRank(EffortMedium) {
		t.Fatalf("manager=%+v", manager)
	}
	for capability, slot := range config.Slots {
		if resolved.Slots[capability].RequestedEffort != slot.RequestedEffort {
			t.Fatalf("%s effort mutated: got %s want %s", capability, resolved.Slots[capability].RequestedEffort, slot.RequestedEffort)
		}
	}
}
