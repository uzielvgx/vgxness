package sdd

import (
	"errors"
	"reflect"
	"strings"
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

func TestResolveOpenCodePlanV2KeepsExplicitEmptyVariants(t *testing.T) {
	config := DefaultModelPlanConfigV2()
	for capability, slot := range config.Slots {
		slot.VariantSpecified = true
		config.Slots[capability] = slot
	}
	resolved, err := ResolveOpenCodePlanV2(config)
	if err != nil {
		t.Fatal(err)
	}
	for role, assignment := range resolved.Roles {
		if assignment.Variant != "" {
			t.Fatalf("%s explicit empty variant=%q", role, assignment.Variant)
		}
	}
}

func TestResolveOpenCodePlanV3PreservesCanonicalOrderAndIndependentAssignments(t *testing.T) {
	inventory := []ManagedAgentIdentity{
		{ArtifactKey: "agents/first.md", Role: RoleResearch, Class: ManagedAgentClassCore},
		{ArtifactKey: "agents/second.md", Role: RoleResearch, Class: ManagedAgentClassSDD},
		{ArtifactKey: "agents/third.md", Role: RoleApply, Class: ManagedAgentClassSDD},
	}
	config := ModelPlanConfigV3{SchemaVersion: 3, Provider: "mixed", Provenance: ModelPlanCLI, Assignments: map[string]ManagedAgentModelConfig{
		"agents/third.md":  {Provider: "third", Reference: "third/ns:model@beta+fast", RequestedEffort: EffortUltra, Source: ModelSlotCustom, Availability: ModelSlotUnknown},
		"agents/first.md":  {Provider: "openai", Reference: "openai/gpt-5.6-luna", RequestedEffort: EffortLow, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
		"agents/second.md": {Provider: "second", Reference: "second/research", RequestedEffort: EffortHigh, Source: ModelSlotCustom, Availability: ModelSlotUnknown},
	}}

	resolved, err := ResolveOpenCodePlanV3(config, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Provider != "mixed" || resolved.SchemaVersion != 3 || resolved.Provenance != ModelPlanCLI {
		t.Fatalf("resolved metadata=%+v", resolved)
	}
	gotOrder := make([]string, 0, len(resolved.Assignments))
	for _, assignment := range resolved.Assignments {
		gotOrder = append(gotOrder, assignment.ArtifactKey)
		if assignment.Provider+"/" == assignment.Model {
			t.Fatalf("assignment was not independently resolved: %+v", assignment)
		}
	}
	if want := []string{"agents/first.md", "agents/second.md", "agents/third.md"}; !reflect.DeepEqual(gotOrder, want) {
		t.Fatalf("order=%v want=%v", gotOrder, want)
	}
	if resolved.Assignments[0].Role != resolved.Assignments[1].Role || resolved.Assignments[0].Model == resolved.Assignments[1].Model {
		t.Fatalf("duplicate-role identities collapsed: %+v", resolved.Assignments)
	}
	for reference, valid := range map[string]bool{"openai/gpt-5.6": true, "provider/nested/model": true, "openai/a:b@c+d": true, "p/" + strings.Repeat("a", 256) + "/" + strings.Repeat("b", 253): true, "": false, "provider": false, "@provider/model": false, "/model": false, "provider/": false, "provider//model": false, "p/a b": false, "p/a?b": false, "p/a#b": false, "p/a=b": false, `p/a\b`: false, "p/a\nb": false, "p/" + strings.Repeat("a", 257): false, "p/" + strings.Repeat("a", 256) + "/" + strings.Repeat("b", 254): false} {
		if _, err := modelProvider(reference); (err == nil) != valid {
			t.Fatalf("modelProvider(%q) error=%v, valid=%t", reference, err, valid)
		}
	}
}

func TestResolveOpenCodePlanV3PreservesVerbatimVariant(t *testing.T) {
	inventory := []ManagedAgentIdentity{{ArtifactKey: "agents/only.md", Role: RoleManager, Class: ManagedAgentClassCore}}
	config := ModelPlanConfigV3{SchemaVersion: 3, Provider: "openai", Provenance: ModelPlanCLI, Assignments: map[string]ManagedAgentModelConfig{
		"agents/only.md": {Provider: "openai", Reference: "openai/gpt-5.6-terra", RequestedEffort: EffortUltra, Variant: "max", Source: ModelSlotCustom, Availability: ModelSlotUnknown},
	}}
	resolved, err := ResolveOpenCodePlanV3(config, inventory)
	if err != nil || len(resolved.Assignments) != 1 || resolved.Assignments[0].Variant != "max" {
		t.Fatalf("ResolveOpenCodePlanV3() = (%+v, %v), want verbatim max", resolved, err)
	}
}

func TestResolveOpenCodePlanV3Validation(t *testing.T) {
	inventory := []ManagedAgentIdentity{{ArtifactKey: "agents/only.md", Role: RoleManager, Class: ManagedAgentClassCore}}
	validAssignment := ManagedAgentModelConfig{Provider: "acme", Reference: "acme/model", RequestedEffort: EffortMedium, Source: ModelSlotCustom, Availability: ModelSlotUnknown}
	valid := func() ModelPlanConfigV3 {
		return ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Provenance: ModelPlanCLI, Assignments: map[string]ManagedAgentModelConfig{"agents/only.md": validAssignment}}
	}
	tests := map[string]func() (ModelPlanConfigV3, []ManagedAgentIdentity){
		"wrong schema": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			c.SchemaVersion = 2
			return c, inventory
		},
		"missing": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			delete(c.Assignments, "agents/only.md")
			return c, inventory
		},
		"extra": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			c.Assignments["agents/extra.md"] = validAssignment
			return c, inventory
		},
		"provider mismatch": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Provider = "other"
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"malformed reference": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Reference = "https://acme/model"
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"empty middle reference segment": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Reference = "acme//model"
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"empty trailing reference segment": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Reference = "acme/model/"
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"empty nested reference segment": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Reference = "acme/model//variant"
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"summary mismatch": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			c.Provider = "other"
			return c, inventory
		},
		"invalid effort": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.RequestedEffort = ""
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"invalid source": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Source = "other"
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"custom known": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Availability = ModelSlotCatalogKnown
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"catalog unknown": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Source = ModelSlotCatalog
			a.Availability = ModelSlotUnknown
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"catalog unrecognized": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			c := valid()
			a := validAssignment
			a.Source = ModelSlotCatalog
			a.Availability = ModelSlotCatalogKnown
			c.Assignments["agents/only.md"] = a
			return c, inventory
		},
		"duplicate inventory": func() (ModelPlanConfigV3, []ManagedAgentIdentity) { return valid(), append(inventory, inventory[0]) },
		"empty inventory key": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			return valid(), []ManagedAgentIdentity{{Role: RoleManager, Class: ManagedAgentClassCore}}
		},
		"invalid inventory role": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			return valid(), []ManagedAgentIdentity{{ArtifactKey: "agents/only.md", Role: "", Class: ManagedAgentClassCore}}
		},
		"invalid inventory class": func() (ModelPlanConfigV3, []ManagedAgentIdentity) {
			return valid(), []ManagedAgentIdentity{{ArtifactKey: "agents/only.md", Role: RoleManager}}
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			config, candidateInventory := test()
			if _, err := ResolveOpenCodePlanV3(config, candidateInventory); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrProviderMismatch) {
				t.Fatalf("invalid v3 input accepted: err=%v", err)
			}
		})
	}
}
