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

type ModelSlotSource string

const (
	ModelSlotCatalog ModelSlotSource = "catalog"
	ModelSlotCustom  ModelSlotSource = "custom"
)

type ModelSlotAvailability string

const (
	ModelSlotCatalogKnown ModelSlotAvailability = "catalog-known"
	ModelSlotUnknown      ModelSlotAvailability = "unknown"
)

// ModelSlotConfig describes one authoritative v2 capability slot. Custom
// references are syntactically validated only; they do not assert provider
// authorization or catalog support.
type ModelSlotConfig struct {
	Reference       string                `json:"reference"`
	RequestedEffort Effort                `json:"requestedEffort"`
	Source          ModelSlotSource       `json:"source"`
	Availability    ModelSlotAvailability `json:"availability"`
}

type ModelPlanConfigV2 struct {
	SchemaVersion int                            `json:"schemaVersion"`
	ActivePlan    Plan                           `json:"activePlan"`
	Provider      string                         `json:"provider"`
	Slots         map[Capability]ModelSlotConfig `json:"slots"`
	Provenance    ModelPlanProvenance            `json:"provenance"`
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

type OpenCodeRoleAssignmentV2 struct {
	Role            Role            `json:"role"`
	Capability      Capability      `json:"capability"`
	Provider        string          `json:"provider"`
	Model           string          `json:"model"`
	RequestedEffort Effort          `json:"requestedEffort"`
	Effort          Effort          `json:"effort"`
	Variant         OpenCodeVariant `json:"variant"`
	Degradation     Degradation     `json:"degradation"`
	Strength        int             `json:"strength"`
}

type OpenCodePlanV2 struct {
	SchemaVersion int                               `json:"schemaVersion"`
	ActivePlan    Plan                              `json:"activePlan"`
	Provider      string                            `json:"provider"`
	Slots         map[Capability]ModelSlotConfig    `json:"slots"`
	Roles         map[Role]OpenCodeRoleAssignmentV2 `json:"roles"`
	Provenance    ModelPlanProvenance               `json:"provenance"`
}

type ManagedAgentClass string

const (
	ManagedAgentClassCore   ManagedAgentClass = "core"
	ManagedAgentClassReview ManagedAgentClass = "review"
	ManagedAgentClassSDD    ManagedAgentClass = "sdd"
)

func (class ManagedAgentClass) Valid() bool {
	return class == ManagedAgentClassCore || class == ManagedAgentClassReview || class == ManagedAgentClassSDD
}

// ManagedAgentIdentity is provider-owned metadata for one stable managed
// artifact. ArtifactKey, rather than Role, is the assignment identity.
type ManagedAgentIdentity struct {
	ArtifactKey string            `json:"artifactKey"`
	Role        Role              `json:"role"`
	Class       ManagedAgentClass `json:"class"`
}

type ManagedAgentModelConfig struct {
	Provider        string                `json:"provider"`
	Reference       string                `json:"reference"`
	RequestedEffort Effort                `json:"requestedEffort"`
	Source          ModelSlotSource       `json:"source"`
	Availability    ModelSlotAvailability `json:"availability"`
}

type ModelPlanConfigV3 struct {
	SchemaVersion int                                `json:"schemaVersion"`
	Provider      string                             `json:"provider"`
	Assignments   map[string]ManagedAgentModelConfig `json:"assignments"`
	Provenance    ModelPlanProvenance                `json:"provenance"`
}

type OpenCodeAgentAssignmentV3 struct {
	ArtifactKey     string                `json:"artifactKey"`
	Role            Role                  `json:"role"`
	Class           ManagedAgentClass     `json:"class"`
	Provider        string                `json:"provider"`
	Model           string                `json:"model"`
	RequestedEffort Effort                `json:"requestedEffort"`
	Effort          Effort                `json:"effort"`
	Variant         OpenCodeVariant       `json:"variant"`
	Degradation     Degradation           `json:"degradation"`
	Source          ModelSlotSource       `json:"source"`
	Availability    ModelSlotAvailability `json:"availability"`
}

type OpenCodePlanV3 struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Provider      string                      `json:"provider"`
	Assignments   []OpenCodeAgentAssignmentV3 `json:"assignments"`
	Provenance    ModelPlanProvenance         `json:"provenance"`
}

func DefaultModelPlanConfig() ModelPlanConfig {
	return ModelPlanConfig{
		SchemaVersion: 1, ActivePlan: PlanMedium, Provider: "openai",
		Efficient: "openai/gpt-5.6-luna", Balanced: "openai/gpt-5.6-terra", Frontier: "openai/gpt-5.6-sol",
		Provenance: ModelPlanDefault,
	}
}

func DefaultModelPlanConfigV2() ModelPlanConfigV2 {
	return ModelPlanConfigV2{
		SchemaVersion: 2, ActivePlan: PlanMedium, Provider: "openai", Provenance: ModelPlanDefault,
		Slots: map[Capability]ModelSlotConfig{
			CapabilityEfficient: {Reference: "openai/gpt-5.6-luna", RequestedEffort: EffortMedium, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
			CapabilityBalanced:  {Reference: "openai/gpt-5.6-terra", RequestedEffort: EffortMedium, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
			CapabilityFrontier:  {Reference: "openai/gpt-5.6-sol", RequestedEffort: EffortMedium, Source: ModelSlotCatalog, Availability: ModelSlotCatalogKnown},
		},
	}
}

func NewModelPlanConfigV2(plan Plan, efficient, balanced, frontier ModelSlotConfig) (ModelPlanConfigV2, error) {
	config := ModelPlanConfigV2{SchemaVersion: 2, ActivePlan: plan, Provenance: ModelPlanCLI, Slots: map[Capability]ModelSlotConfig{
		CapabilityEfficient: efficient, CapabilityBalanced: balanced, CapabilityFrontier: frontier,
	}}
	provider, err := modelPlanConfigV2Provider(config)
	if err != nil {
		return ModelPlanConfigV2{}, err
	}
	config.Provider = provider
	if _, err := ResolveOpenCodePlanV2(config); err != nil {
		return ModelPlanConfigV2{}, err
	}
	return config, nil
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

func ResolveOpenCodePlanV2(config ModelPlanConfigV2) (OpenCodePlanV2, error) {
	if err := validateModelPlanConfigV2(config); err != nil {
		return OpenCodePlanV2{}, err
	}
	providers := make(map[string]bool, len(config.Slots))
	resolvedSlots := make(map[Capability]ModelSlotConfig, len(config.Slots))
	effective := make(map[Capability]Effort, len(config.Slots))
	degradations := make(map[Capability]Degradation, len(config.Slots))
	for capability, slot := range config.Slots {
		provider, _ := modelProvider(slot.Reference)
		providers[provider] = true
		resolvedSlots[capability] = slot
		effective[capability] = slot.RequestedEffort
		if slot.Source == ModelSlotCatalog {
			model := catalogModel(capability, slot.Reference)
			effort, degraded := resolveEffort(model.SupportedEfforts, slot.RequestedEffort)
			effective[capability] = effort
			if degraded {
				degradations[capability] = Degradation{Degraded: true, Reason: fmt.Sprintf("requested effort %s is unsupported by %s; using highest declared effort %s", slot.RequestedEffort, slot.Reference, effort)}
			}
		}
	}
	provider := "mixed"
	if len(providers) == 1 {
		for value := range providers {
			provider = value
		}
	}
	provenance := config.Provenance
	if provenance == "" {
		provenance = ModelPlanCLI
	}
	matrix, _ := RoleMatrix(config.ActivePlan)
	result := OpenCodePlanV2{SchemaVersion: 2, ActivePlan: config.ActivePlan, Provider: provider, Slots: resolvedSlots, Roles: make(map[Role]OpenCodeRoleAssignmentV2, len(matrix)), Provenance: provenance}
	for role, assignment := range matrix {
		slot := resolvedSlots[assignment.Capability]
		slotProvider, _ := modelProvider(slot.Reference)
		result.Roles[role] = OpenCodeRoleAssignmentV2{
			Role: role, Capability: assignment.Capability, Provider: slotProvider, Model: slot.Reference,
			RequestedEffort: slot.RequestedEffort, Effort: effective[assignment.Capability],
			Variant: OpenCodeVariantForEffort(effective[assignment.Capability]), Degradation: degradations[assignment.Capability],
			Strength: RoleAssignment{Capability: assignment.Capability, Effort: effective[assignment.Capability]}.Strength(),
		}
	}
	return result, nil
}

// ResolveOpenCodePlanV3 validates the keyed configuration and emits resolved
// assignments in inventory order. Callers must supply their canonical managed
// artifact inventory; map iteration cannot influence the result.
func ResolveOpenCodePlanV3(config ModelPlanConfigV3, inventory []ManagedAgentIdentity) (OpenCodePlanV3, error) {
	if config.SchemaVersion != 3 || config.Provenance != ModelPlanDefault && config.Provenance != ModelPlanCLI || len(inventory) == 0 || len(config.Assignments) != len(inventory) {
		return OpenCodePlanV3{}, ErrInvalid
	}

	seen := make(map[string]bool, len(inventory))
	resolved := make([]OpenCodeAgentAssignmentV3, 0, len(inventory))
	summary := ""
	for _, identity := range inventory {
		if !validManagedAgentIdentity(identity) || seen[identity.ArtifactKey] {
			return OpenCodePlanV3{}, ErrInvalid
		}
		seen[identity.ArtifactKey] = true
		assignment, ok := config.Assignments[identity.ArtifactKey]
		if !ok {
			return OpenCodePlanV3{}, ErrInvalid
		}
		provider, effort, degradation, err := resolveManagedAgentModel(assignment)
		if err != nil {
			return OpenCodePlanV3{}, err
		}
		if summary == "" {
			summary = provider
		} else if summary != provider {
			summary = "mixed"
		}
		resolved = append(resolved, OpenCodeAgentAssignmentV3{
			ArtifactKey: identity.ArtifactKey, Role: identity.Role, Class: identity.Class,
			Provider: provider, Model: assignment.Reference, RequestedEffort: assignment.RequestedEffort,
			Effort: effort, Variant: OpenCodeVariantForEffort(effort), Degradation: degradation,
			Source: assignment.Source, Availability: assignment.Availability,
		})
	}
	if config.Provider != summary {
		return OpenCodePlanV3{}, fmt.Errorf("%w: configured provider %q does not match assignments", ErrProviderMismatch, config.Provider)
	}
	return OpenCodePlanV3{SchemaVersion: 3, Provider: summary, Assignments: resolved, Provenance: config.Provenance}, nil
}

func resolveManagedAgentModel(assignment ManagedAgentModelConfig) (string, Effort, Degradation, error) {
	provider, err := modelProvider(assignment.Reference)
	if err != nil || !validV3ModelReference(assignment.Reference) || !assignment.RequestedEffort.Valid() {
		return "", "", Degradation{}, ErrInvalid
	}
	if assignment.Provider != provider {
		return "", "", Degradation{}, fmt.Errorf("%w: assignment provider %q does not match reference", ErrProviderMismatch, assignment.Provider)
	}
	effort := assignment.RequestedEffort
	degradation := Degradation{}
	switch assignment.Source {
	case ModelSlotCatalog:
		model := catalogModelByReference(assignment.Reference)
		if assignment.Availability != ModelSlotCatalogKnown || model.ID == "" {
			return "", "", Degradation{}, ErrInvalid
		}
		var degraded bool
		effort, degraded = resolveEffort(model.SupportedEfforts, assignment.RequestedEffort)
		if degraded {
			degradation = Degradation{Degraded: true, Reason: fmt.Sprintf("requested effort %s is unsupported by %s; using highest declared effort %s", assignment.RequestedEffort, assignment.Reference, effort)}
		}
	case ModelSlotCustom:
		if assignment.Availability != ModelSlotUnknown {
			return "", "", Degradation{}, ErrInvalid
		}
	default:
		return "", "", Degradation{}, ErrInvalid
	}
	return provider, effort, degradation, nil
}

func validV3ModelReference(reference string) bool {
	for _, segment := range strings.Split(reference, "/") {
		if segment == "" {
			return false
		}
	}
	return true
}

func validManagedAgentIdentity(identity ManagedAgentIdentity) bool {
	if !identity.Class.Valid() || !validRole(identity.Role) || !strings.HasPrefix(identity.ArtifactKey, "agents/") || !strings.HasSuffix(identity.ArtifactKey, ".md") {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(identity.ArtifactKey, "agents/"), ".md")
	return name != "" && !strings.Contains(name, "/") && safeModelPart(name, false)
}

func validRole(role Role) bool {
	for _, candidate := range AllRoles() {
		if role == candidate {
			return true
		}
	}
	return false
}

func catalogModelByReference(reference string) Model {
	for _, model := range DefaultOpenAICatalog().Models {
		if model.ID == reference {
			return model
		}
	}
	return Model{}
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

func validateModelPlanConfigV2(config ModelPlanConfigV2) error {
	if config.SchemaVersion != 2 || !config.ActivePlan.Valid() || config.Provenance != ModelPlanDefault && config.Provenance != ModelPlanCLI || len(config.Slots) != 3 {
		return ErrInvalid
	}
	for _, capability := range []Capability{CapabilityEfficient, CapabilityBalanced, CapabilityFrontier} {
		slot, ok := config.Slots[capability]
		if !ok || !slot.RequestedEffort.Valid() {
			return ErrInvalid
		}
		if _, err := modelProvider(slot.Reference); err != nil {
			return err
		}
		switch slot.Source {
		case ModelSlotCatalog:
			if slot.Availability != ModelSlotCatalogKnown || catalogModel(capability, slot.Reference).ID == "" {
				return ErrInvalid
			}
		case ModelSlotCustom:
			if slot.Availability != ModelSlotUnknown {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	provider, err := modelPlanConfigV2Provider(config)
	if err != nil {
		return err
	}
	if config.Provider != provider {
		return fmt.Errorf("%w: configured provider %q does not match slots", ErrProviderMismatch, config.Provider)
	}
	return nil
}

// modelPlanConfigV2Provider derives the serialized summary from authoritative
// slot references. ActivePlan selects roles only; it never changes slot effort.
func modelPlanConfigV2Provider(config ModelPlanConfigV2) (string, error) {
	providers := make(map[string]bool, len(config.Slots))
	for _, capability := range []Capability{CapabilityEfficient, CapabilityBalanced, CapabilityFrontier} {
		slot, ok := config.Slots[capability]
		if !ok {
			return "", ErrInvalid
		}
		provider, err := modelProvider(slot.Reference)
		if err != nil {
			return "", err
		}
		providers[provider] = true
	}
	if len(providers) != 1 {
		return "mixed", nil
	}
	for provider := range providers {
		return provider, nil
	}
	return "", ErrInvalid
}

func catalogModel(capability Capability, reference string) Model {
	for _, model := range DefaultOpenAICatalog().Models {
		if model.Capability == capability && model.ID == reference {
			return model
		}
	}
	return Model{}
}

func modelProvider(reference string) (string, error) {
	segments := strings.Split(reference, "/")
	if len(reference) > 512 || len(segments) < 2 || strings.HasPrefix(reference, "@") {
		return "", ErrInvalid
	}
	for _, segment := range segments {
		if segment == "" || len(segment) > 256 {
			return "", ErrInvalid
		}
		for _, character := range segment {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("-_.:@+", character) {
				continue
			}
			return "", ErrInvalid
		}
	}
	return segments[0], nil
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
