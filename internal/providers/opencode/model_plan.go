package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
)

const (
	modelPlanManifestName = "model-plan.json"
	sddResearchName       = "vgxness-sdd-research.md"
	sddProposalName       = "vgxness-sdd-proposal.md"
	sddSpecName           = "vgxness-sdd-spec.md"
	sddDesignName         = "vgxness-sdd-design.md"
	sddTasksName          = "vgxness-sdd-tasks.md"
	sddApplyName          = "vgxness-sdd-apply.md"
)

type modelPlanManifest struct {
	SchemaVersion int                          `json:"schemaVersion"`
	ManagedBy     string                       `json:"managedBy"`
	Config        *modelplan.ModelPlanConfig   `json:"config,omitempty"`
	Resolved      *modelplan.OpenCodePlan      `json:"resolved,omitempty"`
	ConfigV2      *modelplan.ModelPlanConfigV2 `json:"configV2,omitempty"`
	ResolvedV2    *modelplan.OpenCodePlanV2    `json:"resolvedV2,omitempty"`
	ConfigV3      *modelplan.ModelPlanConfigV3 `json:"configV3,omitempty"`
	ResolvedV3    *modelplan.OpenCodePlanV3    `json:"resolvedV3,omitempty"`
	Artifacts     map[string]string            `json:"artifacts"`
}

type modelPlanBundle struct {
	config     modelplan.ModelPlanConfig
	resolved   modelplan.OpenCodePlan
	configV2   *modelplan.ModelPlanConfigV2
	resolvedV2 *modelplan.OpenCodePlanV2
	configV3   *modelplan.ModelPlanConfigV3
	resolvedV3 *modelplan.OpenCodePlanV3
	agents     map[string][]byte
	manifest   []byte
}

func encodeModelPlanBundle(config modelplan.ModelPlanConfig, resolved modelplan.OpenCodePlan, agents map[string][]byte) (modelPlanBundle, error) {
	manifest := modelPlanManifest{SchemaVersion: 1, ManagedBy: "vgxness", Config: &config, Resolved: &resolved, Artifacts: make(map[string]string, len(agents))}
	for name, content := range agents {
		manifest.Artifacts[filepath.ToSlash(filepath.Join("agents", name))] = artifactSHA256(content)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return modelPlanBundle{}, fmt.Errorf("%w: encode model plan", integration.ErrInvalid)
	}
	data = append(data, '\n')
	return modelPlanBundle{config: config, resolved: resolved, agents: agents, manifest: data}, nil
}

func encodeModelPlanBundleV2(config modelplan.ModelPlanConfigV2, resolved modelplan.OpenCodePlanV2, agents map[string][]byte) (modelPlanBundle, error) {
	manifest := modelPlanManifest{SchemaVersion: 2, ManagedBy: "vgxness", ConfigV2: &config, ResolvedV2: &resolved, Artifacts: make(map[string]string, len(agents))}
	for name, content := range agents {
		manifest.Artifacts[filepath.ToSlash(filepath.Join("agents", name))] = artifactSHA256(content)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return modelPlanBundle{}, fmt.Errorf("%w: encode model plan", integration.ErrInvalid)
	}
	data = append(data, '\n')
	return modelPlanBundle{configV2: &config, resolvedV2: &resolved, agents: agents, manifest: data}, nil
}

func encodeModelPlanBundleV3(config modelplan.ModelPlanConfigV3, resolved modelplan.OpenCodePlanV3, agents map[string][]byte) (modelPlanBundle, error) {
	manifest := modelPlanManifest{SchemaVersion: 3, ManagedBy: "vgxness", ConfigV3: &config, ResolvedV3: &resolved, Artifacts: make(map[string]string, len(agents))}
	for name, content := range agents {
		manifest.Artifacts[filepath.ToSlash(filepath.Join("agents", name))] = artifactSHA256(content)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return modelPlanBundle{}, fmt.Errorf("%w: encode model plan", integration.ErrInvalid)
	}
	data = append(data, '\n')
	return modelPlanBundle{configV3: &config, resolvedV3: &resolved, agents: agents, manifest: data}, nil
}

func requestedModelPlan(options integration.Options, configDirectory string) (modelPlanBundle, error) {
	return requestedModelPlanForMigration(options, configDirectory, false)
}

func requestedModelPlanForMigration(options integration.Options, configDirectory string, migrateInstalledV1 bool) (modelPlanBundle, error) {
	explicit := options.ModelPlan != "" || options.ModelEfficient != "" || options.ModelBalanced != "" || options.ModelFrontier != ""
	v3Requested := options.ModelAssignments != nil
	if v3Requested && (explicit || hasSlotEffort(options) || hasSlotVariant(options)) {
		return modelPlanBundle{}, fmt.Errorf("%w: per-agent assignments cannot be combined with model slots", integration.ErrInvalid)
	}
	manifestPath := filepath.Join(configDirectory, "vgxness", modelPlanManifestName)
	base := modelplan.DefaultModelPlanConfig()
	installedV1 := false
	var installedBundle modelPlanBundle
	var installedV2 *modelplan.ModelPlanConfigV2
	var installedV3 *modelplan.ModelPlanConfigV3
	if data, err := readRegularFile(manifestPath); err == nil {
		installed, bundle, parseErr := parseManagedModelPlan(configDirectory, data)
		if parseErr != nil {
			return modelPlanBundle{}, parseErr
		}
		installedBundle = bundle
		if installed.ConfigV3 != nil {
			installedV3 = installed.ConfigV3
		} else if installed.ConfigV2 != nil {
			installedV2 = installed.ConfigV2
		} else {
			base = *installed.Config
			installedV1 = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return modelPlanBundle{}, fmt.Errorf("%w: model plan manifest", integration.ErrConflict)
	}
	if v3Requested {
		assignments := make(map[string]modelplan.ManagedAgentModelConfig, len(*options.ModelAssignments))
		provider := ""
		for key, assignment := range *options.ModelAssignments {
			assignments[key] = assignment
			if provider == "" {
				provider = assignment.Provider
			} else if provider != assignment.Provider {
				provider = "mixed"
			}
		}
		return buildModelPlanBundleV3(currentModelConfig(modelplan.ModelPlanConfigV3{SchemaVersion: 3, Provider: provider, Assignments: assignments, Provenance: modelplan.ModelPlanCLI}))
	}
	if installedV3 != nil && !explicit && !hasSlotEffort(options) && !hasSlotVariant(options) {
		return buildModelPlanBundleV3(currentModelConfig(*installedV3))
	}
	if installedV2 != nil {
		if !explicit && !hasSlotEffort(options) && !hasSlotVariant(options) {
			return buildModelPlanBundleV2(*installedV2)
		}
		config, err := overrideModelPlanConfigV2(*installedV2, options)
		if err != nil {
			return modelPlanBundle{}, fmt.Errorf("%w: model plan", integration.ErrInvalid)
		}
		if config.Provider != "mixed" {
			return modelPlanBundle{}, fmt.Errorf("%w: v2 model plan must remain mixed", integration.ErrInvalid)
		}
		return buildModelPlanBundleV2(config)
	}
	plan := base.ActivePlan
	if options.ModelPlan != "" {
		plan = options.ModelPlan
	}
	efficient, balanced, frontier := base.Efficient, base.Balanced, base.Frontier
	if options.ModelEfficient != "" {
		efficient = options.ModelEfficient
	}
	if options.ModelBalanced != "" {
		balanced = options.ModelBalanced
	}
	if options.ModelFrontier != "" {
		frontier = options.ModelFrontier
	}
	if modelPlanV2Requested(efficient, balanced, frontier) || hasSlotVariant(options) {
		config, err := modelPlanConfigV2(options, plan, efficient, balanced, frontier)
		if err != nil {
			return modelPlanBundle{}, fmt.Errorf("%w: model plan", integration.ErrInvalid)
		}
		if !explicit && !hasSlotEffort(options) {
			config.Provenance = base.Provenance
		}
		candidate, candidateErr := buildModelPlanBundleV3(currentModelConfig(projectModelPlanV2ToV3(config)))
		return verifyInstalledV3SlotProjection(installedV3, installedBundle, candidate, candidateErr)
	}
	if hasSlotEffort(options) {
		config, err := modelPlanConfigV2(options, plan, efficient, balanced, frontier)
		if err != nil {
			return modelPlanBundle{}, fmt.Errorf("%w: model plan", integration.ErrInvalid)
		}
		if !explicit {
			config.Provenance = base.Provenance
		}
		candidate, candidateErr := buildModelPlanBundleV3(currentModelConfig(projectModelPlanV2ToV3(config)))
		return verifyInstalledV3SlotProjection(installedV3, installedBundle, candidate, candidateErr)
	}
	config, err := modelplan.NewModelPlanConfig(plan, efficient, balanced, frontier)
	if err != nil {
		return modelPlanBundle{}, fmt.Errorf("%w: model plan", integration.ErrInvalid)
	}
	if !explicit {
		config.Provenance = base.Provenance
	}
	if installedV1 && migrateInstalledV1 {
		migrateInstalledV1 = isExactSetupCLIV1Plan(base, installedBundle)
	}
	if installedV1 && !migrateInstalledV1 {
		return buildModelPlanBundle(config)
	}
	candidate, candidateErr := buildModelPlanBundleV3(currentModelConfig(projectModelPlanToV3(config)))
	return verifyInstalledV3SlotProjection(installedV3, installedBundle, candidate, candidateErr)
}

func isExactSetupCLIV1Plan(config modelplan.ModelPlanConfig, installed modelPlanBundle) bool {
	exact, err := buildModelPlanBundle(config)
	if err != nil {
		return false
	}
	defaults := modelplan.DefaultModelPlanConfig()
	return config.SchemaVersion == defaults.SchemaVersion &&
		config.Provider == defaults.Provider &&
		config.ActivePlan == defaults.ActivePlan &&
		config.Efficient == defaults.Efficient &&
		config.Balanced == defaults.Balanced &&
		config.Frontier == defaults.Frontier &&
		(config.Provenance == modelplan.ModelPlanDefault || config.Provenance == modelplan.ModelPlanCLI) &&
		bytes.Equal(installed.manifest, exact.manifest)
}

func verifyInstalledV3SlotProjection(installed *modelplan.ModelPlanConfigV3, existing, candidate modelPlanBundle, err error) (modelPlanBundle, error) {
	if err != nil || installed == nil {
		return candidate, err
	}
	if !bytes.Equal(candidate.manifest, existing.manifest) {
		return modelPlanBundle{}, fmt.Errorf("%w: slot flags differ from installed per-agent plan", integration.ErrInvalid)
	}
	return candidate, nil
}

func projectModelPlanToV3(config modelplan.ModelPlanConfig) modelplan.ModelPlanConfigV3 {
	plan, err := modelplan.ResolveOpenCodePlan(config)
	if err != nil {
		return modelplan.ModelPlanConfigV3{}
	}
	defaults := modelplan.DefaultModelPlanConfig()
	assignments := make(map[string]modelplan.ManagedAgentModelConfig, len(modelAgentInventoryV3))
	for _, identity := range modelAgentInventoryV3 {
		assignment, ok := plan.Roles[identity.Role]
		if !ok {
			return modelplan.ModelPlanConfigV3{}
		}
		provider := modelProvider(assignment.Model)
		if provider == "" {
			return modelplan.ModelPlanConfigV3{}
		}
		source, availability := modelplan.ModelSlotCustom, modelplan.ModelSlotUnknown
		if plan.Slots[assignment.Capability] == modelPlanReference(defaults, assignment.Capability) {
			source, availability = modelplan.ModelSlotCatalog, modelplan.ModelSlotCatalogKnown
		}
		assignments[identity.ArtifactKey] = modelplan.ManagedAgentModelConfig{
			Provider: provider, Reference: assignment.Model, RequestedEffort: assignment.RequestedEffort,
			Variant: assignment.Variant, Source: source, Availability: availability,
		}
	}
	return modelplan.ModelPlanConfigV3{SchemaVersion: 3, Provider: assignmentProviderSummary(assignments), Assignments: assignments, Provenance: config.Provenance}
}

func projectModelPlanV2ToV3(config modelplan.ModelPlanConfigV2) modelplan.ModelPlanConfigV3 {
	plan, err := modelplan.ResolveOpenCodePlanV2(config)
	if err != nil {
		return modelplan.ModelPlanConfigV3{}
	}
	assignments := make(map[string]modelplan.ManagedAgentModelConfig, len(modelAgentInventoryV3))
	for _, identity := range modelAgentInventoryV3 {
		assignment, ok := plan.Roles[identity.Role]
		if !ok {
			return modelplan.ModelPlanConfigV3{}
		}
		slot, ok := plan.Slots[assignment.Capability]
		if !ok || modelProvider(assignment.Model) == "" {
			return modelplan.ModelPlanConfigV3{}
		}
		assignments[identity.ArtifactKey] = modelplan.ManagedAgentModelConfig{
			Provider: assignment.Provider, Reference: assignment.Model, RequestedEffort: assignment.RequestedEffort,
			Variant: slot.Variant, VariantSpecified: slot.VariantSpecified, Source: slot.Source, Availability: slot.Availability,
		}
	}
	return modelplan.ModelPlanConfigV3{SchemaVersion: 3, Provider: assignmentProviderSummary(assignments), Assignments: assignments, Provenance: config.Provenance}
}

func modelPlanReference(config modelplan.ModelPlanConfig, capability modelplan.Capability) string {
	switch capability {
	case modelplan.CapabilityEfficient:
		return config.Efficient
	case modelplan.CapabilityBalanced:
		return config.Balanced
	case modelplan.CapabilityFrontier:
		return config.Frontier
	default:
		return ""
	}
}

func overrideModelPlanConfigV2(installed modelplan.ModelPlanConfigV2, options integration.Options) (modelplan.ModelPlanConfigV2, error) {
	plan := installed.ActivePlan
	if options.ModelPlan != "" {
		plan = options.ModelPlan
	}
	slots := make(map[modelplan.Capability]modelplan.ModelSlotConfig, len(installed.Slots))
	for capability, slot := range installed.Slots {
		slots[capability] = slot
	}
	for _, override := range []struct {
		capability modelplan.Capability
		reference  string
		effort     modelplan.Effort
		variant    modelplan.OpenCodeVariant
	}{
		{modelplan.CapabilityEfficient, options.ModelEfficient, options.ModelEfficientEffort, options.ModelEfficientVariant},
		{modelplan.CapabilityBalanced, options.ModelBalanced, options.ModelBalancedEffort, options.ModelBalancedVariant},
		{modelplan.CapabilityFrontier, options.ModelFrontier, options.ModelFrontierEffort, options.ModelFrontierVariant},
	} {
		slot := slots[override.capability]
		if override.reference != "" {
			slot.Reference = override.reference
			defaultSlot := modelplan.DefaultModelPlanConfigV2().Slots[override.capability]
			if slot.Reference == defaultSlot.Reference {
				slot.Source, slot.Availability = modelplan.ModelSlotCatalog, modelplan.ModelSlotCatalogKnown
			} else {
				slot.Source, slot.Availability = modelplan.ModelSlotCustom, modelplan.ModelSlotUnknown
			}
		}
		if override.effort != "" {
			if !override.effort.Valid() {
				return modelplan.ModelPlanConfigV2{}, integration.ErrInvalid
			}
			slot.RequestedEffort = override.effort
		}
		if options.ModelVariantsSpecified {
			slot.Variant, slot.VariantSpecified = override.variant, true
		} else if override.reference != "" {
			slot.Variant, slot.VariantSpecified = "", false
		}
		slots[override.capability] = slot
	}
	config, err := modelplan.NewModelPlanConfigV2(plan, slots[modelplan.CapabilityEfficient], slots[modelplan.CapabilityBalanced], slots[modelplan.CapabilityFrontier])
	if err != nil {
		return modelplan.ModelPlanConfigV2{}, err
	}
	config.Provenance = installed.Provenance
	return config, nil
}

func modelPlanV2Requested(efficient, balanced, frontier string) bool {
	return modelProvider(efficient) != modelProvider(balanced) || modelProvider(efficient) != modelProvider(frontier)
}

func hasSlotEffort(options integration.Options) bool {
	return options.ModelEfficientEffort != "" || options.ModelBalancedEffort != "" || options.ModelFrontierEffort != ""
}

func hasSlotVariant(options integration.Options) bool {
	return options.ModelVariantsSpecified || options.ModelEfficientVariant != "" || options.ModelBalancedVariant != "" || options.ModelFrontierVariant != ""
}

func modelProvider(reference string) string {
	provider, _, found := strings.Cut(reference, "/")
	if !found {
		return ""
	}
	return provider
}

func modelPlanConfigV2(options integration.Options, plan modelplan.Plan, efficient, balanced, frontier string) (modelplan.ModelPlanConfigV2, error) {
	defaults := modelplan.DefaultModelPlanConfigV2().Slots
	slots := []struct {
		capability modelplan.Capability
		reference  string
		effort     modelplan.Effort
		variant    modelplan.OpenCodeVariant
	}{
		{modelplan.CapabilityEfficient, efficient, options.ModelEfficientEffort, options.ModelEfficientVariant},
		{modelplan.CapabilityBalanced, balanced, options.ModelBalancedEffort, options.ModelBalancedVariant},
		{modelplan.CapabilityFrontier, frontier, options.ModelFrontierEffort, options.ModelFrontierVariant},
	}
	config := make([]modelplan.ModelSlotConfig, len(slots))
	for index, slot := range slots {
		if slot.effort == "" {
			if !options.ModelVariantsSpecified {
				return modelplan.ModelPlanConfigV2{}, integration.ErrInvalid
			}
			slot.effort = defaults[slot.capability].RequestedEffort
		}
		if !slot.effort.Valid() {
			return modelplan.ModelPlanConfigV2{}, integration.ErrInvalid
		}
		config[index] = modelplan.ModelSlotConfig{Reference: slot.reference, RequestedEffort: slot.effort, Variant: slot.variant, VariantSpecified: options.ModelVariantsSpecified || slot.variant != "", Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown}
		if slot.reference == defaults[slot.capability].Reference {
			config[index].Source = modelplan.ModelSlotCatalog
			config[index].Availability = modelplan.ModelSlotCatalogKnown
		}
	}
	return modelplan.NewModelPlanConfigV2(plan, config[0], config[1], config[2])
}

func parseInstalledModelPlanManifest(data []byte) (modelPlanManifest, modelPlanBundle, error) {
	manifest, err := decodeModelPlanManifest(data)
	if err != nil {
		return modelPlanManifest{}, modelPlanBundle{}, err
	}
	bundle, err := modelPlanBundleForDecodedManifest(data, manifest)
	return manifest, bundle, err
}

func decodeModelPlanManifest(data []byte) (modelPlanManifest, error) {
	var manifest modelPlanManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) == nil || manifest.ManagedBy != "vgxness" {
		return modelPlanManifest{}, integration.ErrDrift
	}
	switch manifest.SchemaVersion {
	case 1:
		if !manifestHasOnlyFields(data, "schemaVersion", "managedBy", "config", "resolved", "artifacts") || manifest.Config == nil || manifest.Resolved == nil || manifest.ConfigV2 != nil || manifest.ResolvedV2 != nil || manifest.ConfigV3 != nil || manifest.ResolvedV3 != nil || manifest.Artifacts == nil {
			return modelPlanManifest{}, integration.ErrDrift
		}
	case 2:
		if !manifestHasOnlyFields(data, "schemaVersion", "managedBy", "configV2", "resolvedV2", "artifacts") || manifest.Config != nil || manifest.Resolved != nil || manifest.ConfigV2 == nil || manifest.ResolvedV2 == nil || manifest.ConfigV3 != nil || manifest.ResolvedV3 != nil || manifest.Artifacts == nil {
			return modelPlanManifest{}, integration.ErrDrift
		}
	case 3:
		if !manifestHasOnlyFields(data, "schemaVersion", "managedBy", "configV3", "resolvedV3", "artifacts") || manifest.Config != nil || manifest.Resolved != nil || manifest.ConfigV2 != nil || manifest.ResolvedV2 != nil || manifest.ConfigV3 == nil || manifest.ResolvedV3 == nil || manifest.Artifacts == nil {
			return modelPlanManifest{}, integration.ErrDrift
		}
	default:
		return modelPlanManifest{}, integration.ErrDrift
	}
	return manifest, nil
}

func manifestHasOnlyFields(data []byte, expected ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != len(expected) {
		return false
	}
	for _, field := range expected {
		if _, ok := fields[field]; !ok {
			return false
		}
	}
	return true
}

func modelPlanBundleForDecodedManifest(data []byte, manifest modelPlanManifest) (modelPlanBundle, error) {
	if manifest.SchemaVersion == 3 {
		return modelPlanBundleForManifestV3(data, *manifest.ConfigV3)
	}
	if manifest.SchemaVersion == 2 {
		return modelPlanBundleForManifestV2(data, *manifest.ConfigV2)
	}
	return modelPlanBundleForManifest(data, *manifest.Config)
}

func modelPlanBundleForManifestV3(data []byte, config modelplan.ModelPlanConfigV3) (modelPlanBundle, error) {
	current, err := buildModelPlanBundleV3(config)
	if err != nil || !bytes.Equal(current.manifest, data) {
		return modelPlanBundle{}, integration.ErrDrift
	}
	return current, nil
}

func modelPlanBundleForManifestV2(data []byte, config modelplan.ModelPlanConfigV2) (modelPlanBundle, error) {
	current, err := buildModelPlanBundleV2(config)
	if err != nil || !bytes.Equal(current.manifest, data) {
		return modelPlanBundle{}, integration.ErrDrift
	}
	return current, nil
}

func modelPlanBundleForManifest(data []byte, config modelplan.ModelPlanConfig) (modelPlanBundle, error) {
	current, err := buildModelPlanBundle(config)
	if err != nil || !bytes.Equal(current.manifest, data) {
		return modelPlanBundle{}, integration.ErrDrift
	}
	return current, nil
}

func assignmentProviderSummary(assignments map[string]modelplan.ManagedAgentModelConfig) string {
	summary := ""
	for _, assignment := range assignments {
		if summary == "" {
			summary = assignment.Provider
		} else if summary != assignment.Provider {
			return "mixed"
		}
	}
	return summary
}

func installedModelPlan(configDirectory string) (modelPlanBundle, map[string][]byte, bool) {
	manifestPath := filepath.Join(configDirectory, "vgxness", modelPlanManifestName)
	data, err := readRegularFile(manifestPath)
	if err != nil {
		return modelPlanBundle{}, nil, false
	}
	_, bundle, err := parseInstalledModelPlanManifest(data)
	if err != nil {
		return modelPlanBundle{}, nil, false
	}
	current := make(map[string][]byte, len(bundle.agents)+1)
	for name, expected := range bundle.agents {
		path := filepath.Join(configDirectory, "agents", name)
		current[path] = expected
	}
	current[manifestPath] = data
	return bundle, current, true
}

func parseModelPlanManifest(data []byte) (modelPlanManifest, error) {
	manifest, err := decodeModelPlanManifest(data)
	if err != nil {
		return modelPlanManifest{}, err
	}
	if _, err := modelPlanBundleForDecodedManifest(data, manifest); err != nil {
		return modelPlanManifest{}, err
	}
	return manifest, nil
}
