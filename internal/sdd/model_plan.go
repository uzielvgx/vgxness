package sdd

import "fmt"

type Capability string

const (
	CapabilityEfficient Capability = "efficient"
	CapabilityBalanced  Capability = "balanced"
	CapabilityFrontier  Capability = "frontier"
)

func (value Capability) Valid() bool {
	return value == CapabilityEfficient || value == CapabilityBalanced || value == CapabilityFrontier
}

func capabilityRank(value Capability) int {
	switch value {
	case CapabilityEfficient:
		return 1
	case CapabilityBalanced:
		return 2
	case CapabilityFrontier:
		return 3
	default:
		return 0
	}
}

type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortUltra  Effort = "ultra"
)

func (value Effort) Valid() bool { return effortRank(value) > 0 }

func effortRank(value Effort) int {
	switch value {
	case EffortLow:
		return 1
	case EffortMedium:
		return 2
	case EffortHigh:
		return 3
	case EffortUltra:
		return 4
	default:
		return 0
	}
}

type Plan string

const (
	PlanLow    Plan = "low"
	PlanMedium Plan = "medium"
	PlanHigh   Plan = "high"
	PlanUltra  Plan = "ultra"
)

func (value Plan) Valid() bool {
	return value == PlanLow || value == PlanMedium || value == PlanHigh || value == PlanUltra
}

type Role string

const (
	RoleManager        Role = "manager"
	RoleResearch       Role = "research"
	RoleProposal       Role = "proposal"
	RoleSpec           Role = "spec"
	RoleDesign         Role = "design"
	RoleTasks          Role = "tasks"
	RoleApply          Role = "apply"
	RoleRisk           Role = "risk"
	RoleReadability    Role = "readability"
	RoleReliability    Role = "reliability"
	RoleResilience     Role = "resilience"
	RoleRefuter        Role = "refuter"
	RoleImplementation Role = "implementation"
	RoleVerification   Role = "verification"
)

var roles = []Role{RoleManager, RoleResearch, RoleProposal, RoleSpec, RoleDesign, RoleTasks, RoleApply, RoleRisk, RoleReadability, RoleReliability, RoleResilience, RoleRefuter, RoleImplementation, RoleVerification}

func AllRoles() []Role { return append([]Role(nil), roles...) }

type RoleAssignment struct {
	Capability Capability `json:"capability"`
	Effort     Effort     `json:"effort"`
}

func (assignment RoleAssignment) Strength() int {
	return capabilityRank(assignment.Capability)*10 + effortRank(assignment.Effort)
}

var roleMatrices = map[Plan]map[Role]RoleAssignment{
	PlanLow: {
		RoleManager: {CapabilityBalanced, EffortHigh}, RoleResearch: {CapabilityEfficient, EffortLow},
		RoleProposal: {CapabilityEfficient, EffortMedium}, RoleSpec: {CapabilityEfficient, EffortMedium},
		RoleDesign: {CapabilityBalanced, EffortMedium}, RoleTasks: {CapabilityEfficient, EffortLow},
		RoleApply: {CapabilityBalanced, EffortLow}, RoleRisk: {CapabilityEfficient, EffortMedium},
		RoleReadability: {CapabilityEfficient, EffortLow}, RoleReliability: {CapabilityEfficient, EffortMedium},
		RoleResilience: {CapabilityEfficient, EffortMedium}, RoleRefuter: {CapabilityBalanced, EffortMedium},
		RoleImplementation: {CapabilityBalanced, EffortLow}, RoleVerification: {CapabilityEfficient, EffortLow},
	},
	PlanMedium: {
		RoleManager: {CapabilityFrontier, EffortHigh}, RoleResearch: {CapabilityEfficient, EffortMedium},
		RoleProposal: {CapabilityBalanced, EffortMedium}, RoleSpec: {CapabilityBalanced, EffortHigh},
		RoleDesign: {CapabilityFrontier, EffortMedium}, RoleTasks: {CapabilityBalanced, EffortMedium},
		RoleApply: {CapabilityBalanced, EffortMedium}, RoleRisk: {CapabilityFrontier, EffortMedium},
		RoleReadability: {CapabilityEfficient, EffortMedium}, RoleReliability: {CapabilityBalanced, EffortHigh},
		RoleResilience: {CapabilityBalanced, EffortHigh}, RoleRefuter: {CapabilityFrontier, EffortMedium},
		RoleImplementation: {CapabilityBalanced, EffortMedium}, RoleVerification: {CapabilityEfficient, EffortMedium},
	},
	PlanHigh: {
		RoleManager: {CapabilityFrontier, EffortUltra}, RoleResearch: {CapabilityBalanced, EffortHigh},
		RoleProposal: {CapabilityBalanced, EffortHigh}, RoleSpec: {CapabilityFrontier, EffortHigh},
		RoleDesign: {CapabilityFrontier, EffortHigh}, RoleTasks: {CapabilityBalanced, EffortHigh},
		RoleApply: {CapabilityBalanced, EffortHigh}, RoleRisk: {CapabilityFrontier, EffortHigh},
		RoleReadability: {CapabilityEfficient, EffortHigh}, RoleReliability: {CapabilityFrontier, EffortHigh},
		RoleResilience: {CapabilityFrontier, EffortHigh}, RoleRefuter: {CapabilityFrontier, EffortHigh},
		RoleImplementation: {CapabilityFrontier, EffortHigh}, RoleVerification: {CapabilityBalanced, EffortHigh},
	},
	PlanUltra: {
		RoleManager: {CapabilityFrontier, EffortUltra}, RoleResearch: {CapabilityFrontier, EffortHigh},
		RoleProposal: {CapabilityFrontier, EffortHigh}, RoleSpec: {CapabilityFrontier, EffortHigh},
		RoleDesign: {CapabilityFrontier, EffortHigh}, RoleTasks: {CapabilityFrontier, EffortHigh},
		RoleApply: {CapabilityFrontier, EffortHigh}, RoleRisk: {CapabilityFrontier, EffortHigh},
		RoleReadability: {CapabilityBalanced, EffortHigh}, RoleReliability: {CapabilityFrontier, EffortHigh},
		RoleResilience: {CapabilityFrontier, EffortHigh}, RoleRefuter: {CapabilityFrontier, EffortHigh},
		RoleImplementation: {CapabilityFrontier, EffortHigh}, RoleVerification: {CapabilityFrontier, EffortHigh},
	},
}

func RoleMatrix(plan Plan) (map[Role]RoleAssignment, error) {
	matrix, ok := roleMatrices[plan]
	if !ok {
		return nil, ErrInvalid
	}
	result := make(map[Role]RoleAssignment, len(matrix))
	for role, assignment := range matrix {
		result[role] = assignment
	}
	return result, nil
}

type Model struct {
	Provider         string     `json:"provider"`
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Capability       Capability `json:"capability,omitempty"`
	SupportedEfforts []Effort   `json:"supportedEfforts"`
}

type Catalog struct {
	Provider string  `json:"provider"`
	Models   []Model `json:"models"`
}

type Degradation struct {
	Degraded bool   `json:"degraded"`
	Reason   string `json:"reason,omitempty"`
}

type ResolvedAssignment struct {
	Role            Role        `json:"role"`
	Capability      Capability  `json:"capability"`
	Model           Model       `json:"model"`
	RequestedEffort Effort      `json:"requestedEffort"`
	Effort          Effort      `json:"effort"`
	Degradation     Degradation `json:"degradation"`
}

type ResolvedPlan struct {
	Provider string                      `json:"provider"`
	Plan     Plan                        `json:"plan"`
	Slots    map[Capability]Model        `json:"slots"`
	Roles    map[Role]ResolvedAssignment `json:"roles"`
}

func DefaultOpenAICatalog() Catalog {
	return Catalog{Provider: "openai", Models: []Model{
		{Provider: "openai", ID: "openai/gpt-5.6-luna", Name: "Luna", Capability: CapabilityEfficient, SupportedEfforts: []Effort{EffortLow, EffortMedium, EffortHigh, EffortUltra}},
		{Provider: "openai", ID: "openai/gpt-5.6-terra", Name: "Terra", Capability: CapabilityBalanced, SupportedEfforts: []Effort{EffortLow, EffortMedium, EffortHigh, EffortUltra}},
		{Provider: "openai", ID: "openai/gpt-5.6-sol", Name: "Sol", Capability: CapabilityFrontier, SupportedEfforts: []Effort{EffortLow, EffortMedium, EffortHigh, EffortUltra}},
	}}
}

func ResolveModelPlan(catalog Catalog, plan Plan) (ResolvedPlan, error) {
	if !validText(catalog.Provider, 128) || len(catalog.Models) == 0 || !plan.Valid() {
		return ResolvedPlan{}, ErrInvalid
	}
	for _, model := range catalog.Models {
		if model.Provider != catalog.Provider {
			return ResolvedPlan{}, fmt.Errorf("%w: catalog %q contains %q", ErrProviderMismatch, catalog.Provider, model.Provider)
		}
		if !validText(model.ID, 256) || !validText(model.Name, 256) || len(model.SupportedEfforts) == 0 {
			return ResolvedPlan{}, ErrInvalid
		}
		if model.Capability != "" && !model.Capability.Valid() {
			return ResolvedPlan{}, ErrInvalid
		}
		for _, effort := range model.SupportedEfforts {
			if !effort.Valid() {
				return ResolvedPlan{}, ErrInvalid
			}
		}
	}
	models := catalog.Models
	if len(models) > 3 {
		models = models[:3]
	}
	slots := resolveSlots(models)
	matrix, _ := RoleMatrix(plan)
	resolved := ResolvedPlan{Provider: catalog.Provider, Plan: plan, Slots: slots, Roles: make(map[Role]ResolvedAssignment, len(matrix))}
	for _, role := range roles {
		requested := matrix[role]
		model := slots[requested.Capability]
		effort, degraded := resolveEffort(model.SupportedEfforts, requested.Effort)
		assignment := ResolvedAssignment{Role: role, Capability: requested.Capability, Model: model, RequestedEffort: requested.Effort, Effort: effort}
		if degraded {
			assignment.Degradation = Degradation{Degraded: true, Reason: fmt.Sprintf("requested effort %s is unsupported by %s; using highest declared effort %s", requested.Effort, model.ID, effort)}
		}
		resolved.Roles[role] = assignment
	}
	return resolved, nil
}

func resolveSlots(models []Model) map[Capability]Model {
	capabilities := []Capability{CapabilityEfficient, CapabilityBalanced, CapabilityFrontier}
	slots := make(map[Capability]Model, 3)
	used := make(map[int]bool, len(models))
	for index, model := range models {
		if model.Capability.Valid() {
			if _, exists := slots[model.Capability]; !exists {
				slots[model.Capability] = model
				used[index] = true
			}
		}
	}
	for _, capability := range capabilities {
		if _, exists := slots[capability]; exists {
			continue
		}
		for index, model := range models {
			if !used[index] {
				slots[capability] = model
				used[index] = true
				break
			}
		}
	}
	for index, capability := range capabilities {
		if _, exists := slots[capability]; !exists {
			modelIndex := index
			if modelIndex >= len(models) {
				modelIndex = len(models) - 1
			}
			slots[capability] = models[modelIndex]
		}
	}
	return slots
}

func resolveEffort(supported []Effort, requested Effort) (Effort, bool) {
	highest := supported[0]
	for _, effort := range supported {
		if effort == requested {
			return requested, false
		}
		if effortRank(effort) > effortRank(highest) {
			highest = effort
		}
	}
	return highest, true
}
