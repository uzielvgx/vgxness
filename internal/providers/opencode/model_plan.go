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
	"github.com/vgxness/vgxness/internal/sdd"
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
	SchemaVersion int                 `json:"schemaVersion"`
	ManagedBy     string              `json:"managedBy"`
	Config        sdd.ModelPlanConfig `json:"config"`
	Resolved      sdd.OpenCodePlan    `json:"resolved"`
	Artifacts     map[string]string   `json:"artifacts"`
}

type modelPlanBundle struct {
	config   sdd.ModelPlanConfig
	resolved sdd.OpenCodePlan
	agents   map[string][]byte
	manifest []byte
}

func buildModelPlanBundle(config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	resolved, err := sdd.ResolveOpenCodePlan(config)
	if err != nil {
		return modelPlanBundle{}, fmt.Errorf("%w: model plan", integration.ErrInvalid)
	}
	agents, err := modelBoundAgents(resolved)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func buildV32ModelPlanBundle(config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	resolved, err := resolveV33OpenCodePlan(config)
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents, err := v32ModelBoundAgents(resolved)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func buildV33ModelPlanBundle(config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	resolved, err := resolveV33OpenCodePlan(config)
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents, err := v33ModelBoundAgents(resolved)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func resolveV33OpenCodePlan(config sdd.ModelPlanConfig) (sdd.OpenCodePlan, error) {
	return sdd.ResolveOpenCodePlan(config)
}

func buildV31ModelPlanBundle(config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	resolved, err := resolveHistoricalOpenCodePlan(config)
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents, err := v31ModelBoundAgents(resolved)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func resolveHistoricalOpenCodePlan(config sdd.ModelPlanConfig) (sdd.OpenCodePlan, error) {
	resolved, err := resolveV33OpenCodePlan(config)
	if err != nil {
		return sdd.OpenCodePlan{}, err
	}
	delete(resolved.Roles, sdd.RoleImplementation)
	delete(resolved.Roles, sdd.RoleVerification)
	return resolved, nil
}

func encodeModelPlanBundle(config sdd.ModelPlanConfig, resolved sdd.OpenCodePlan, agents map[string][]byte) (modelPlanBundle, error) {
	manifest := modelPlanManifest{SchemaVersion: 1, ManagedBy: "vgxness", Config: config, Resolved: resolved, Artifacts: make(map[string]string, len(agents))}
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

func buildV30ModelPlanBundle(config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	resolved, err := resolveHistoricalOpenCodePlan(config)
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents, err := v30ModelBoundAgents(resolved)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func buildV29ModelPlanBundle(config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	resolved, err := resolveHistoricalOpenCodePlan(config)
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents, err := v29ModelBoundAgents(resolved)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func buildV28ModelPlanBundle(config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	resolved, err := resolveHistoricalOpenCodePlan(config)
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents, err := v28ModelBoundAgents(resolved)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func requestedModelPlan(options integration.Options, configDirectory string) (modelPlanBundle, error) {
	explicit := options.ModelPlan != "" || options.ModelEfficient != "" || options.ModelBalanced != "" || options.ModelFrontier != ""
	manifestPath := filepath.Join(configDirectory, "vgxness", modelPlanManifestName)
	base := sdd.DefaultModelPlanConfig()
	if data, err := readRegularFile(manifestPath); err == nil {
		installed, _, parseErr := parseInstalledModelPlanManifest(data)
		if parseErr != nil {
			return modelPlanBundle{}, fmt.Errorf("%w: model plan manifest", integration.ErrConflict)
		}
		base = installed.Config
	} else if !errors.Is(err, os.ErrNotExist) {
		return modelPlanBundle{}, fmt.Errorf("%w: model plan manifest", integration.ErrConflict)
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
	config, err := sdd.NewModelPlanConfig(plan, efficient, balanced, frontier)
	if err != nil {
		return modelPlanBundle{}, fmt.Errorf("%w: model plan", integration.ErrInvalid)
	}
	if !explicit {
		config.Provenance = base.Provenance
	}
	return buildModelPlanBundle(config)
}

func parseInstalledModelPlanManifest(data []byte) (modelPlanManifest, modelPlanBundle, error) {
	if manifest, err := parseModelPlanManifest(data); err == nil {
		bundle, buildErr := buildModelPlanBundle(manifest.Config)
		return manifest, bundle, buildErr
	}
	for _, build := range []func(sdd.ModelPlanConfig) (modelPlanBundle, error){buildV33ModelPlanBundle, buildV32ModelPlanBundle, buildV31ModelPlanBundle, buildV30ModelPlanBundle, buildV29ModelPlanBundle, buildV28ModelPlanBundle} {
		manifest, bundle, err := parseHistoricalModelPlanManifest(data, build)
		if err == nil {
			return manifest, bundle, nil
		}
	}
	return modelPlanManifest{}, modelPlanBundle{}, integration.ErrDrift
}

func parseModelPlanManifest(data []byte) (modelPlanManifest, error) {
	var manifest modelPlanManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) == nil || manifest.SchemaVersion != 1 || manifest.ManagedBy != "vgxness" {
		return modelPlanManifest{}, integration.ErrDrift
	}
	bundle, err := buildModelPlanBundle(manifest.Config)
	if err != nil || !bytes.Equal(bundle.manifest, data) {
		return modelPlanManifest{}, integration.ErrDrift
	}
	return manifest, nil
}

func parseHistoricalModelPlanManifest(data []byte, build func(sdd.ModelPlanConfig) (modelPlanBundle, error)) (modelPlanManifest, modelPlanBundle, error) {
	var manifest modelPlanManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) == nil || manifest.SchemaVersion != 1 || manifest.ManagedBy != "vgxness" {
		return modelPlanManifest{}, modelPlanBundle{}, integration.ErrDrift
	}
	bundle, err := build(manifest.Config)
	if err != nil || !bytes.Equal(bundle.manifest, data) {
		return modelPlanManifest{}, modelPlanBundle{}, integration.ErrDrift
	}
	return manifest, bundle, nil
}

func modelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	return fullModelBoundAgents(plan, bindManager, generalV2Prompt, "artifact: opencode-agent/general; version: 2", verifierV2Prompt, "artifact: opencode-agent/vgxness-verifier; version: 2")
}

func v33ModelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	return fullModelBoundAgents(plan, bindManagerV33, generalV2Prompt, "artifact: opencode-agent/general; version: 2", verifierV2Prompt, "artifact: opencode-agent/vgxness-verifier; version: 2")
}

func v32ModelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	return fullModelBoundAgents(plan, bindManagerV32, generalV1Prompt, "artifact: opencode-agent/general; version: 1", verifierV1Prompt, "artifact: opencode-agent/vgxness-verifier; version: 1")
}

func fullModelBoundAgents(plan sdd.OpenCodePlan, managerBinder func(sdd.OpenCodeRoleAssignment) ([]byte, error), generalBase, generalMarker, verifierBase, verifierMarker string) (map[string][]byte, error) {
	assignments := map[string]sdd.Role{
		managerAgentName: sdd.RoleManager,
		exploreAgentName: sdd.RoleResearch,
		generalAgentName: sdd.RoleImplementation, verifierAgentName: sdd.RoleVerification,
		reviewRiskName: sdd.RoleRisk, reviewReadabilityName: sdd.RoleReadability,
		reviewReliabilityName: sdd.RoleReliability, reviewResilienceName: sdd.RoleResilience,
		reviewRefuterName: sdd.RoleRefuter,
	}
	agents := make(map[string][]byte, 15)
	manager, err := managerBinder(plan.Roles[sdd.RoleManager])
	if err != nil {
		return nil, err
	}
	agents[managerAgentName] = manager
	explore, err := bindExplore(plan.Roles[sdd.RoleResearch])
	if err != nil {
		return nil, err
	}
	agents[exploreAgentName] = explore
	general, err := bindProfile(generalBase, generalMarker, plan.Roles[sdd.RoleImplementation])
	if err != nil {
		return nil, err
	}
	agents[generalAgentName] = general
	verifier, err := bindProfile(verifierBase, verifierMarker, plan.Roles[sdd.RoleVerification])
	if err != nil {
		return nil, err
	}
	agents[verifierAgentName] = verifier
	baseReviews := map[string]string{
		reviewRiskName: reviewRiskPrompt, reviewReadabilityName: reviewReadabilityPrompt,
		reviewReliabilityName: reviewReliabilityPrompt, reviewResilienceName: reviewResiliencePrompt,
		reviewRefuterName: reviewRefuterPrompt,
	}
	for name, base := range baseReviews {
		content, bindErr := bindAgent(base, assignments[name], plan.Roles[assignments[name]], 1, 2)
		if bindErr != nil {
			return nil, bindErr
		}
		agents[name] = content
	}
	for _, profile := range []struct {
		name string
		role sdd.Role
	}{
		{sddResearchName, sdd.RoleResearch}, {sddProposalName, sdd.RoleProposal}, {sddSpecName, sdd.RoleSpec},
		{sddDesignName, sdd.RoleDesign}, {sddTasksName, sdd.RoleTasks}, {sddApplyName, sdd.RoleApply},
	} {
		agents[profile.name] = []byte(sddAgentPrompt(profile.role, plan.Roles[profile.role]))
	}
	return agents, nil
}

func v31ModelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	assignments := map[string]sdd.Role{
		managerAgentName: sdd.RoleManager,
		exploreAgentName: sdd.RoleResearch,
		reviewRiskName:   sdd.RoleRisk, reviewReadabilityName: sdd.RoleReadability,
		reviewReliabilityName: sdd.RoleReliability, reviewResilienceName: sdd.RoleResilience,
		reviewRefuterName: sdd.RoleRefuter,
	}
	agents := make(map[string][]byte, 13)
	manager, err := bindManagerV31(plan.Roles[sdd.RoleManager])
	if err != nil {
		return nil, err
	}
	agents[managerAgentName] = manager
	explore, err := bindExplore(plan.Roles[sdd.RoleResearch])
	if err != nil {
		return nil, err
	}
	agents[exploreAgentName] = explore
	for name, base := range map[string]string{
		reviewRiskName: reviewRiskPrompt, reviewReadabilityName: reviewReadabilityPrompt,
		reviewReliabilityName: reviewReliabilityPrompt, reviewResilienceName: reviewResiliencePrompt,
		reviewRefuterName: reviewRefuterPrompt,
	} {
		content, bindErr := bindAgent(base, assignments[name], plan.Roles[assignments[name]], 1, 2)
		if bindErr != nil {
			return nil, bindErr
		}
		agents[name] = content
	}
	for _, profile := range []struct {
		name string
		role sdd.Role
	}{
		{sddResearchName, sdd.RoleResearch}, {sddProposalName, sdd.RoleProposal}, {sddSpecName, sdd.RoleSpec},
		{sddDesignName, sdd.RoleDesign}, {sddTasksName, sdd.RoleTasks}, {sddApplyName, sdd.RoleApply},
	} {
		agents[profile.name] = []byte(sddAgentPromptV2(profile.role, plan.Roles[profile.role]))
	}
	return agents, nil
}

func v30ModelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	assignments := map[string]sdd.Role{
		managerAgentName: sdd.RoleManager,
		reviewRiskName:   sdd.RoleRisk, reviewReadabilityName: sdd.RoleReadability,
		reviewReliabilityName: sdd.RoleReliability, reviewResilienceName: sdd.RoleResilience,
		reviewRefuterName: sdd.RoleRefuter,
	}
	agents := make(map[string][]byte, 12)
	manager, err := bindManagerV30(plan.Roles[sdd.RoleManager])
	if err != nil {
		return nil, err
	}
	agents[managerAgentName] = manager
	for name, base := range map[string]string{
		reviewRiskName: reviewRiskPrompt, reviewReadabilityName: reviewReadabilityPrompt,
		reviewReliabilityName: reviewReliabilityPrompt, reviewResilienceName: reviewResiliencePrompt,
		reviewRefuterName: reviewRefuterPrompt,
	} {
		content, bindErr := bindAgent(base, assignments[name], plan.Roles[assignments[name]], 1, 2)
		if bindErr != nil {
			return nil, bindErr
		}
		agents[name] = content
	}
	for _, profile := range []struct {
		name string
		role sdd.Role
	}{
		{sddResearchName, sdd.RoleResearch}, {sddProposalName, sdd.RoleProposal}, {sddSpecName, sdd.RoleSpec},
		{sddDesignName, sdd.RoleDesign}, {sddTasksName, sdd.RoleTasks}, {sddApplyName, sdd.RoleApply},
	} {
		agents[profile.name] = []byte(sddAgentPromptV2(profile.role, plan.Roles[profile.role]))
	}
	return agents, nil
}

func v29ModelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	assignments := map[string]sdd.Role{
		managerAgentName: sdd.RoleManager,
		reviewRiskName:   sdd.RoleRisk, reviewReadabilityName: sdd.RoleReadability,
		reviewReliabilityName: sdd.RoleReliability, reviewResilienceName: sdd.RoleResilience,
		reviewRefuterName: sdd.RoleRefuter,
	}
	agents := make(map[string][]byte, 12)
	manager, err := bindManagerV29(plan.Roles[sdd.RoleManager])
	if err != nil {
		return nil, err
	}
	agents[managerAgentName] = manager
	for name, base := range map[string]string{
		reviewRiskName: reviewRiskPrompt, reviewReadabilityName: reviewReadabilityPrompt,
		reviewReliabilityName: reviewReliabilityPrompt, reviewResilienceName: reviewResiliencePrompt,
		reviewRefuterName: reviewRefuterPrompt,
	} {
		content, bindErr := bindAgent(base, assignments[name], plan.Roles[assignments[name]], 1, 2)
		if bindErr != nil {
			return nil, bindErr
		}
		agents[name] = content
	}
	for _, profile := range []struct {
		name string
		role sdd.Role
	}{
		{sddResearchName, sdd.RoleResearch}, {sddProposalName, sdd.RoleProposal}, {sddSpecName, sdd.RoleSpec},
		{sddDesignName, sdd.RoleDesign}, {sddTasksName, sdd.RoleTasks}, {sddApplyName, sdd.RoleApply},
	} {
		agents[profile.name] = []byte(sddAgentPromptV2(profile.role, plan.Roles[profile.role]))
	}
	return agents, nil
}

func v28ModelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	assignments := map[string]sdd.Role{
		managerAgentName: sdd.RoleManager,
		reviewRiskName:   sdd.RoleRisk, reviewReadabilityName: sdd.RoleReadability,
		reviewReliabilityName: sdd.RoleReliability, reviewResilienceName: sdd.RoleResilience,
		reviewRefuterName: sdd.RoleRefuter,
	}
	agents := make(map[string][]byte, 12)
	manager, err := bindPreviousManager(plan.Roles[sdd.RoleManager])
	if err != nil {
		return nil, err
	}
	agents[managerAgentName] = manager
	for name, base := range map[string]string{
		reviewRiskName: reviewRiskPrompt, reviewReadabilityName: reviewReadabilityPrompt,
		reviewReliabilityName: reviewReliabilityPrompt, reviewResilienceName: reviewResiliencePrompt,
		reviewRefuterName: reviewRefuterPrompt,
	} {
		content, bindErr := bindAgent(base, assignments[name], plan.Roles[assignments[name]], 1, 2)
		if bindErr != nil {
			return nil, bindErr
		}
		agents[name] = content
	}
	for _, profile := range []struct {
		name string
		role sdd.Role
	}{
		{sddResearchName, sdd.RoleResearch}, {sddProposalName, sdd.RoleProposal}, {sddSpecName, sdd.RoleSpec},
		{sddDesignName, sdd.RoleDesign}, {sddTasksName, sdd.RoleTasks}, {sddApplyName, sdd.RoleApply},
	} {
		agents[profile.name] = []byte(sddAgentPromptV1(profile.role, plan.Roles[profile.role]))
	}
	return agents, nil
}

func bindManager(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	return bindManagerTemplate(managerV34Prompt, "artifact: opencode-agent/vgxness-manager; version: 34", assignment)
}

func bindManagerV33(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	return bindManagerTemplate(managerV33Prompt, "artifact: opencode-agent/vgxness-manager; version: 33", assignment)
}

func bindManagerV32(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	return bindManagerTemplate(managerV32Prompt, "artifact: opencode-agent/vgxness-manager; version: 32", assignment)
}

func bindManagerTemplate(base, marker string, assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	value := base
	anchor := "color: primary\n"
	if strings.Count(value, anchor) != 1 || strings.Count(value, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value = strings.Replace(value, anchor, fmt.Sprintf("color: primary\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
	return []byte(value), nil
}

func bindManagerV31(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	bound, err := bindManagerV30(assignment)
	if err != nil {
		return nil, err
	}
	value := string(bound)
	for _, replacement := range []textReplacement{
		{old: "artifact: opencode-agent/vgxness-manager; version: 30", new: "artifact: opencode-agent/vgxness-manager; version: 31"},
		{old: "use the built-in explore and general subagents through Task", new: "use the VGXNESS-managed explore override and built-in general subagent through Task"},
	} {
		if strings.Count(value, replacement.old) != 1 {
			return nil, integration.ErrInvalid
		}
		value = strings.Replace(value, replacement.old, replacement.new, 1)
	}
	return []byte(value), nil
}

func bindProfile(base, marker string, assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	anchor := "mode: subagent\n"
	if strings.Count(base, anchor) != 1 || strings.Count(base, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value := strings.Replace(base, anchor, fmt.Sprintf("mode: subagent\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
	return []byte(value), nil
}

func bindManagerV30(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	bound, err := bindManagerV29(assignment)
	if err != nil {
		return nil, err
	}
	value := string(bound)
	for _, replacement := range []textReplacement{
		{old: "artifact: opencode-agent/vgxness-manager; version: 29", new: "artifact: opencode-agent/vgxness-manager; version: 30"},
		{old: "# Native authority and delegation\n", new: managerEvidenceBoundedContract + "\n# Native authority and delegation\n"},
	} {
		if strings.Count(value, replacement.old) != 1 {
			return nil, integration.ErrInvalid
		}
		value = strings.Replace(value, replacement.old, replacement.new, 1)
	}
	return []byte(value), nil
}

func bindExplore(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	value := explorePrompt
	anchor := "mode: subagent\n"
	marker := "artifact: opencode-agent/explore; version: 1"
	if strings.Count(value, anchor) != 1 || strings.Count(value, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value = strings.Replace(value, anchor, fmt.Sprintf("mode: subagent\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
	return []byte(value), nil
}

func bindManagerV29(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	bound, err := bindManagerV28(assignment)
	if err != nil {
		return nil, err
	}
	return finalizeManager(bound, 28, 29)
}

func bindPreviousManager(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	bound, err := bindManagerV27(assignment)
	if err != nil {
		return nil, err
	}
	return finalizeManager(bound, 27, 28)
}

func finalizeManager(bound []byte, fromVersion, toVersion int) ([]byte, error) {
	value := string(bound)
	for _, replacement := range []textReplacement{
		{old: fmt.Sprintf("artifact: opencode-agent/vgxness-manager; version: %d", fromVersion), new: fmt.Sprintf("artifact: opencode-agent/vgxness-manager; version: %d", toVersion)},
		{old: "  vgxness_sdd_get: allow\n", new: "  vgxness_sdd_get: allow\n  vgxness_sdd_set_interaction_mode: allow\n"},
	} {
		if strings.Count(value, replacement.old) != 1 {
			return nil, integration.ErrInvalid
		}
		value = strings.Replace(value, replacement.old, replacement.new, 1)
	}
	return []byte(value + managerSDDLifecycleContract), nil
}

func bindManagerV28(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	return bindManagerVersion(managerPrompt, assignment, 27, 28)
}

func bindManagerV27(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	predecessors := previousManagerPrompts()
	if len(predecessors) == 0 {
		return nil, integration.ErrInvalid
	}
	return bindManagerVersion(string(predecessors[0]), assignment, 26, 27)
}

func bindManagerVersion(base string, assignment sdd.OpenCodeRoleAssignment, fromVersion, toVersion int) ([]byte, error) {
	value := base
	for _, replacement := range []textReplacement{
		{old: "color: primary\n", new: fmt.Sprintf("color: primary\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant)},
		{old: fmt.Sprintf("artifact: opencode-agent/vgxness-manager; version: %d", fromVersion), new: fmt.Sprintf("artifact: opencode-agent/vgxness-manager; version: %d", toVersion)},
		{old: "    general: allow\n", new: "    general: allow\n    vgxness-sdd-research: allow\n    vgxness-sdd-proposal: allow\n    vgxness-sdd-spec: allow\n    vgxness-sdd-design: allow\n    vgxness-sdd-tasks: allow\n    vgxness-sdd-apply: allow\n"},
		{old: "- VGXNESS SDD tools persist structured records and render or compare supplied OpenSpec bytes only.", new: "- The manager alone owns SDD phase transitions, revision acceptance, projection records, and persistence decisions. SDD subagents return bounded evidence or candidate content and never mutate lifecycle state.\n- VGXNESS SDD tools persist structured records and render or compare supplied OpenSpec bytes only."},
	} {
		if strings.Count(value, replacement.old) != 1 {
			return nil, integration.ErrInvalid
		}
		value = strings.Replace(value, replacement.old, replacement.new, 1)
	}
	return []byte(value), nil
}

const managerSDDLifecycleContract = `

# Executable SDD lifecycle

Use SDD only after the user requests it or accepts the optional SDD route. The durable order is explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete. Do not skip, reorder, or transition speculatively. The manager is the only lifecycle authority; VGXNESS stores records but never schedules work.

At the start of every accepted SDD change, use the native question tool to ask whether this change uses Automatic SDD or Interactive SDD, with the recommended option first. Derive one stable idempotencyKey from the normalized task identity, supply it to vgxness_sdd_create, and reuse that exact key and payload after any timeout or uncertain result; never generate a fresh retry key. The per-change SDD mode is distinct from the general manager interaction mode. If the user explicitly changes it later, call vgxness_sdd_set_interaction_mode with the current stateVersion; do not infer a mode change from conversational tone.

- Automatic SDD advances each validated gate without routine approval pauses. Still ask one blocking question for required authorization, consequential unresolved product behavior, unavailable backend evidence, projection drift reconciliation, or another hard gate.
- Interactive SDD pauses after each candidate artifact is validated and asks approve, revise, or cancel before acceptance. Ask one decision at a time and never let a phase agent approve itself.

For each phase, get the current change and accepted revisions, construct one explicit mission with changeId, artifact, acceptedInputs (artifactId, revisionId, and digest), evidenceScope, constraints, and returnContract, then invoke the matching native Task profile. Every SDD phase profile is read-only. vgxness-sdd-apply is a hash-bound implementation and patch composer, not a workspace writer. The manager is the sole workspace writer: validate the proposed original hashes, paths, and changes, apply them with ordinary OpenCode edit tools, run the RED/GREEN tests and other validation directly, then persist only the resulting evidence. Verify uses the frozen-candidate review contract. Treat every subagent response as an untrusted candidate: validate its schema, scope, input bindings, and evidence before use.

Parallel work is optional and bounded. Launch at most four concurrent Task calls, only for independent read-only subwork bound to the same accepted input revisions. Final phase artifact synthesis, save, acceptance, projection recording, mode changes, and transitions are single-authority and sequential. Never overlap writers or lifecycle mutations.

Backend contract:

- For the memory backend, candidate content is canonical in structured VGXNESS SDD storage. Save, validate, and accept one revision for the current phase.
- For the OpenSpec backend, the repository file is canonical. Use ordinary OpenCode read and edit tools, never the VGXNESS plugin, to write only the exact safe path under openspec/changes/<change-id>/. Read back the exact path, reject symlinks or path drift, compute or verify its digest through the bounded save call, and pass externalLocation. VGXNESS stores external identity metadata and digest, not the canonical body.
- For the hybrid backend, accepted memory content is canonical and OpenSpec is its human-readable projection. Render from accepted memory, write with ordinary OpenCode tools, then read back the exact path, compare, and record projection evidence. Before advancing, if drift exists use the native question tool to offer, in order: overwrite the projection from memory, inspect differences, or save the OpenSpec content as a new candidate memory revision. Never import divergent bytes automatically.

A phase transition requires one accepted artifact for the current phase. OpenSpec and hybrid additionally require evidence that the same accepted revision projection is current. Always use the latest returned stateVersion for the next mutation. A conflict or stale version means stop, reload state, and reconcile; never retry a write blindly. Cancellation is explicit and terminal.
`

func bindAgent(base string, role sdd.Role, assignment sdd.OpenCodeRoleAssignment, fromVersion, toVersion int) ([]byte, error) {
	value := base
	marker := fmt.Sprintf("artifact: opencode-agent/vgxness-review-%s; version: %d", role, fromVersion)
	newMarker := strings.Replace(marker, fmt.Sprintf("version: %d", fromVersion), fmt.Sprintf("version: %d", toVersion), 1)
	if strings.Count(value, "hidden: true\n") != 1 || strings.Count(value, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value = strings.Replace(value, "hidden: true\n", fmt.Sprintf("hidden: true\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
	value = strings.Replace(value, marker, newMarker, 1)
	return []byte(value), nil
}

func sddAgentPrompt(role sdd.Role, assignment sdd.OpenCodeRoleAssignment) string {
	if role == sdd.RoleApply {
		return fmt.Sprintf(`---
description: Native read-only SDD implementation and patch composer for one exact accepted task revision
mode: subagent
hidden: true
model: %s
variant: %s
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  edit: deny
  bash: deny
  question: deny
  task: deny
  webfetch: deny
  websearch: deny
  vgxness_sdd_list: allow
  vgxness_sdd_get: allow
  vgxness_sdd_get_revision: allow
  vgxness_sdd_list_revisions: allow
  vgxness_sdd_projection_status: allow
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-apply; version: 3 -->

You are the read-only implementation and patch composer for one accepted SDD tasks revision. Compose a hash-bound candidate. Reject a mission unless it contains exact change ID, task IDs, accepted task revision ID and SHA-256 digest, every accepted input revision ID and digest, allowed paths with current content hashes, acceptance criteria, exact validation commands, and required RED/TDD evidence.

Inspect only the accepted scope. Do not edit, execute shell commands or tests, delegate, ask questions, persist memory, call SDD write or lifecycle tools, select models, install packages, use network, commit, push, or alter OpenSpec projections. Produce a bounded patch proposal whose paths stay within the mission and whose expected original hashes prevent stale application. Preserve the RED/GREEN plan and identify exact developmental and final validation commands. The manager validates bindings and hashes; managed general performs workspace writes and exact OpenSpec or hybrid projection writes; verifier executes final validation; reviewers assess the same frozen candidate; the manager saves or accepts revisions, records projections, and advances lifecycle state.

Return exactly one compact JSON object and no Markdown:
{"status":"complete|blocked","proposedChanges":[{"path":"allowed path","expectedSHA256":"current file digest","patch":"bounded exact proposed change"}],"validationPlan":[{"command":"exact command","purpose":"RED|GREEN|regression|static"}],"tddEvidence":{"redPlan":"expected pre-change failure","greenPlan":"expected post-change pass"},"summary":"bounded implementation rationale","blockers":["blocking fact"]}
	`, assignment.Model, assignment.Variant)
	}
	return sddAgentPromptV2(role, assignment)
}

func sddAgentPromptV2(role sdd.Role, assignment sdd.OpenCodeRoleAssignment) string {
	if role == sdd.RoleApply {
		return fmt.Sprintf(`---
description: Native read-only SDD implementation and patch composer for one exact accepted task revision
mode: subagent
hidden: true
model: %s
variant: %s
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  edit: deny
  bash: deny
  question: deny
  task: deny
  webfetch: deny
  websearch: deny
  vgxness_sdd_list: allow
  vgxness_sdd_get: allow
  vgxness_sdd_get_revision: allow
  vgxness_sdd_list_revisions: allow
  vgxness_sdd_projection_status: allow
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-apply; version: 2 -->

You are the read-only implementation and patch composer for one accepted SDD tasks revision. Reject a mission unless it contains exact change ID, task IDs, accepted task revision ID and SHA-256 digest, every accepted input revision ID and digest, allowed paths with current content hashes, acceptance criteria, exact validation commands, and required RED/TDD evidence.

Inspect only the accepted scope. Do not edit, execute shell commands or tests, delegate, ask questions, persist memory, call SDD write or lifecycle tools, select models, install packages, use network, commit, push, or alter OpenSpec projections. Produce a bounded patch proposal whose paths stay within the mission and whose expected original hashes prevent stale application. Preserve the RED/GREEN plan and identify which exact commands the manager must run. The manager alone validates hashes, writes workspace files, runs tests, saves or accepts revisions, records projections, and advances lifecycle state.

Return exactly one compact JSON object and no Markdown:
{"status":"complete|blocked","proposedChanges":[{"path":"allowed path","expectedSHA256":"current file digest","patch":"bounded exact proposed change"}],"validationPlan":[{"command":"exact command","purpose":"RED|GREEN|regression|static"}],"tddEvidence":{"redPlan":"expected pre-change failure","greenPlan":"expected post-change pass"},"summary":"bounded implementation rationale","blockers":["blocking fact"]}
`, assignment.Model, assignment.Variant)
	}
	value := sddAgentPromptV1(role, assignment)
	value = strings.Replace(value, fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: 1", role), fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: 2", role), 1)
	return value + phaseAgentContract(role)
}

func sddAgentPromptV1(role sdd.Role, assignment sdd.OpenCodeRoleAssignment) string {
	if role == sdd.RoleApply {
		return fmt.Sprintf(`---
description: Native single-writer SDD apply agent for one exact accepted task revision
mode: subagent
hidden: true
model: %s
variant: %s
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  edit: allow
  question: deny
  task: deny
  webfetch: deny
  websearch: deny
  vgxness_sdd_list: allow
  vgxness_sdd_get: allow
  vgxness_sdd_get_revision: allow
  vgxness_sdd_list_revisions: allow
  vgxness_sdd_projection_status: allow
  bash:
    "*": deny
    "go test *": allow
    "go vet *": allow
    "npm test *": allow
    "npm run test *": allow
    "pnpm test *": allow
    "yarn test *": allow
    "bun test *": allow
    "pytest *": allow
    "cargo test *": allow
    "swift test *": allow
    "flutter test *": allow
    "dart test *": allow
    "./gradlew test*": allow
    "git status*": allow
    "git diff*": allow
    "git *": deny
    "git push*": deny
    "git commit*": deny
    "git reset*": deny
    "git clean*": deny
    "git checkout*": deny
    "git restore*": deny
    "curl *": deny
    "wget *": deny
    "npm install*": deny
    "npm ci*": deny
    "pnpm add*": deny
    "yarn add*": deny
    "bun add*": deny
    "pip install*": deny
    "uv add*": deny
    "go get*": deny
    "go install*": deny
    "gh *": deny
    "ssh *": deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-apply; version: 1 -->

You are the single writer for one accepted SDD tasks revision. Reject a mission unless it contains exact change ID, task IDs, accepted task revision ID and SHA-256 digest, every accepted input revision ID and digest, allowed paths, acceptance criteria, exact validation commands, and required RED/TDD evidence.

Edit only allowed paths. Do not delegate, ask questions, persist memory, call SDD write or lifecycle tools, select models, install packages, use network, commit, push, or alter OpenSpec projections. Run only mission-authorized tests and report changed paths, test results, and TDD evidence. Return candidate evidence to the manager; only the manager may save or accept revisions, record projections, or advance lifecycle state.
`, assignment.Model, assignment.Variant)
	}
	return fmt.Sprintf(`---
description: Native read-only SDD %s artifact agent
mode: subagent
hidden: true
model: %s
variant: %s
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_sdd_list: allow
  vgxness_sdd_get: allow
  vgxness_sdd_get_revision: allow
  vgxness_sdd_list_revisions: allow
  vgxness_sdd_projection_status: allow
  vgxness_sdd_render_projection: allow
  vgxness_sdd_compare_projection: allow
  edit: deny
  bash: deny
  question: deny
  task: deny
  webfetch: deny
  websearch: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-%s; version: 1 -->

You are the read-only SDD %s agent. Accept one exact manager mission bound to a change ID, accepted input revision IDs and SHA-256 digests, requested artifact, evidence scope, and return contract. Read only the workspace and bounded SDD records needed for that artifact.

Do not delegate, ask questions, edit files, execute shell commands, persist memory, save or accept revisions, record projections, transition phases, route work, or select models. Return bounded evidence and candidate artifact content to the manager. The manager alone validates, persists, accepts, and advances SDD lifecycle state.
`, role, assignment.Model, assignment.Variant, role, role)
}

func phaseAgentContract(role sdd.Role) string {
	objective := map[sdd.Role]string{
		sdd.RoleResearch: "Establish repository evidence, constraints, affected surfaces, unknowns, and decisions needed before proposing a change.",
		sdd.RoleProposal: "Define the problem, intended outcomes, scope, non-goals, risks, and measurable success criteria.",
		sdd.RoleSpec:     "Define normative behavior, scenarios, edge cases, failure behavior, and testable acceptance criteria without choosing incidental implementation detail.",
		sdd.RoleDesign:   "Define architecture, data and control flow, interfaces, safety boundaries, migration or rollback needs, and verification strategy against accepted specifications.",
		sdd.RoleTasks:    "Produce ordered implementation tasks with stable IDs, dependencies, allowed paths, RED or regression evidence, validation commands, and completion criteria.",
	}[role]
	return fmt.Sprintf(`

Mission schema: {"changeId":"exact ID","artifact":"%s","acceptedInputs":[{"artifactId":"exact ID","revisionId":"exact ID","digest":"sha256"}],"evidenceScope":["bounded path or question"],"constraints":["constraint"],"returnContract":"phase-candidate-v1"}. Reject a mission with missing, stale, contradictory, or broader inputs.

Phase objective: %s

Return exactly one compact JSON object and no Markdown:
{"status":"complete|blocked","candidateContent":"complete artifact candidate or empty when blocked","evidence":["path:line or exact observation"],"openQuestions":["unresolved consequential question"],"blockers":["blocking fact"]}
`, role, objective)
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
