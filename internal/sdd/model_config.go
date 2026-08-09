package sdd

import (
	"fmt"
	"strings"
)

type ModelPlanProvenance string

const (
	ModelPlanDefault ModelPlanProvenance = "default-openai-catalog"
	ModelPlanCLI     ModelPlanProvenance = "setup-cli"
)

type OpenCodeVariant string

const (
	VariantLow    OpenCodeVariant = "low"
	VariantMedium OpenCodeVariant = "medium"
	VariantHigh   OpenCodeVariant = "high"
	VariantXHigh  OpenCodeVariant = "xhigh"
)

type ModelPlanConfig struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ActivePlan    Plan                `json:"activePlan"`
	Provider      string              `json:"provider"`
	Efficient     string              `json:"efficient"`
	Balanced      string              `json:"balanced"`
	Frontier      string              `json:"frontier"`
	Provenance    ModelPlanProvenance `json:"provenance"`
}

type OpenCodeRoleAssignment struct {
	Role            Role            `json:"role"`
	Capability      Capability      `json:"capability"`
	Model           string          `json:"model"`
	RequestedEffort Effort          `json:"requestedEffort"`
	Effort          Effort          `json:"effort"`
	Variant         OpenCodeVariant `json:"variant"`
	Degradation     Degradation     `json:"degradation"`
	Strength        int             `json:"strength"`
}

type OpenCodePlan struct {
	SchemaVersion int                             `json:"schemaVersion"`
	ActivePlan    Plan                            `json:"activePlan"`
	Provider      string                          `json:"provider"`
	Slots         map[Capability]string           `json:"slots"`
	Roles         map[Role]OpenCodeRoleAssignment `json:"roles"`
	Provenance    ModelPlanProvenance             `json:"provenance"`
}

func DefaultModelPlanConfig() ModelPlanConfig {
	return ModelPlanConfig{
		SchemaVersion: 1, ActivePlan: PlanMedium, Provider: "openai",
		Efficient: "openai/gpt-5.6-luna", Balanced: "openai/gpt-5.6-terra", Frontier: "openai/gpt-5.6-sol",
		Provenance: ModelPlanDefault,
	}
}

func NewModelPlanConfig(plan Plan, efficient, balanced, frontier string) (ModelPlanConfig, error) {
	provider, err := modelProvider(efficient)
	if err != nil {
		return ModelPlanConfig{}, err
	}
	config := ModelPlanConfig{SchemaVersion: 1, ActivePlan: plan, Provider: provider, Efficient: efficient, Balanced: balanced, Frontier: frontier, Provenance: ModelPlanCLI}
	provider, err = validateModelPlanConfig(config)
	if err != nil {
		return ModelPlanConfig{}, err
	}
	config.Provider = provider
	return config, nil
}

func ResolveOpenCodePlan(config ModelPlanConfig) (OpenCodePlan, error) {
	provider, err := validateModelPlanConfig(config)
	if err != nil {
		return OpenCodePlan{}, err
	}
	defaults := DefaultOpenAICatalog()
	models := make([]Model, len(defaults.Models))
	copy(models, defaults.Models)
	refs := map[Capability]string{CapabilityEfficient: config.Efficient, CapabilityBalanced: config.Balanced, CapabilityFrontier: config.Frontier}
	for index := range models {
		models[index].Provider = provider
		models[index].ID = refs[models[index].Capability]
		models[index].Name = models[index].ID
	}
	resolved, err := ResolveModelPlan(Catalog{Provider: provider, Models: models}, config.ActivePlan)
	if err != nil {
		return OpenCodePlan{}, err
	}
	provenance := config.Provenance
	if provenance == "" {
		provenance = ModelPlanCLI
	}
	result := OpenCodePlan{
		SchemaVersion: 1, ActivePlan: config.ActivePlan, Provider: provider,
		Slots: map[Capability]string{CapabilityEfficient: config.Efficient, CapabilityBalanced: config.Balanced, CapabilityFrontier: config.Frontier},
		Roles: make(map[Role]OpenCodeRoleAssignment, len(resolved.Roles)), Provenance: provenance,
	}
	matrix, _ := RoleMatrix(config.ActivePlan)
	for role, assignment := range resolved.Roles {
		result.Roles[role] = OpenCodeRoleAssignment{
			Role: role, Capability: assignment.Capability, Model: assignment.Model.ID,
			RequestedEffort: assignment.RequestedEffort, Effort: assignment.Effort,
			Variant: OpenCodeVariantForEffort(assignment.Effort), Degradation: assignment.Degradation,
			Strength: matrix[role].Strength(),
		}
	}
	return result, nil
}

func OpenCodeVariantForEffort(effort Effort) OpenCodeVariant {
	switch effort {
	case EffortLow:
		return VariantLow
	case EffortMedium:
		return VariantMedium
	case EffortHigh:
		return VariantHigh
	case EffortUltra:
		return VariantXHigh
	default:
		return ""
	}
}

func validateModelPlanConfig(config ModelPlanConfig) (string, error) {
	if config.SchemaVersion != 1 || !config.ActivePlan.Valid() || config.Provider == "" || config.Provenance != ModelPlanDefault && config.Provenance != ModelPlanCLI {
		return "", ErrInvalid
	}
	var provider string
	for _, reference := range []string{config.Efficient, config.Balanced, config.Frontier} {
		current, err := modelProvider(reference)
		if err != nil {
			return "", err
		}
		if provider == "" {
			provider = current
		} else if provider != current {
			return "", fmt.Errorf("%w: model slots use %q and %q", ErrProviderMismatch, provider, current)
		}
	}
	if config.Provider != provider {
		return "", fmt.Errorf("%w: configured provider %q does not match slots", ErrProviderMismatch, config.Provider)
	}
	return provider, nil
}

func modelProvider(reference string) (string, error) {
	if reference == "" || len(reference) > 256 || strings.TrimSpace(reference) != reference || strings.ContainsAny(reference, "?#@=\\\r\n\t") {
		return "", ErrInvalid
	}
	provider, model, ok := strings.Cut(reference, "/")
	if !ok || provider == "" || model == "" || !safeModelPart(provider, false) || !safeModelPart(model, true) {
		return "", ErrInvalid
	}
	return provider, nil
}

func safeModelPart(value string, slash bool) bool {
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' || slash && character == '/' {
			continue
		}
		return false
	}
	return true
}
