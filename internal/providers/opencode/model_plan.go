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
	modelPlanManifestName                                   = "model-plan.json"
	sddResearchName                                         = "vgxness-sdd-research.md"
	sddProposalName                                         = "vgxness-sdd-proposal.md"
	sddSpecName                                             = "vgxness-sdd-spec.md"
	sddDesignName                                           = "vgxness-sdd-design.md"
	sddTasksName                                            = "vgxness-sdd-tasks.md"
	sddApplyName                                            = "vgxness-sdd-apply.md"
	sddReadOnlyTargetVersion, sddReadOnlyPredecessorVersion = 4, 3
	sddApplyTargetVersion, sddApplyPredecessorVersion       = 4, 3
	generalCurrentMarker                                    = "artifact: opencode-agent/general; version: 6"
	generalPreviousMarker                                   = "artifact: opencode-agent/general; version: 5"
	verifierCurrentMarker                                   = "artifact: opencode-agent/vgxness-verifier; version: 4"
	verifierPreviousMarker                                  = "artifact: opencode-agent/vgxness-verifier; version: 3"
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
	manifest, err := decodeModelPlanManifest(data)
	if err != nil {
		return modelPlanManifest{}, modelPlanBundle{}, err
	}
	bundle, err := modelPlanBundleForManifest(data, manifest.Config)
	return manifest, bundle, err
}

func parseModelPlanManifest(data []byte) (modelPlanManifest, error) {
	manifest, err := decodeModelPlanManifest(data)
	if err != nil {
		return modelPlanManifest{}, err
	}
	if _, err := modelPlanBundleForManifest(data, manifest.Config); err != nil {
		return modelPlanManifest{}, err
	}
	return manifest, nil
}

func decodeModelPlanManifest(data []byte) (modelPlanManifest, error) {
	var manifest modelPlanManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) == nil || manifest.SchemaVersion != 1 || manifest.ManagedBy != "vgxness" {
		return modelPlanManifest{}, integration.ErrDrift
	}
	return manifest, nil
}

func modelPlanBundleForManifest(data []byte, config sdd.ModelPlanConfig) (modelPlanBundle, error) {
	current, err := buildModelPlanBundle(config)
	if err != nil {
		return modelPlanBundle{}, integration.ErrDrift
	}
	if bytes.Equal(current.manifest, data) {
		return current, nil
	}
	candidates, err := predecessorBundles(current)
	if err != nil {
		return modelPlanBundle{}, integration.ErrDrift
	}
	for _, candidate := range candidates {
		if bytes.Equal(candidate.manifest, data) {
			return candidate, nil
		}
	}
	return modelPlanBundle{}, integration.ErrDrift
}

func predecessorBundles(current modelPlanBundle) ([]modelPlanBundle, error) {
	broadPredecessor, err := previousBroadPermissionModelPlanBundle(current)
	if err != nil {
		return nil, err
	}
	v45, err := previousV45ModelPlanBundle(current)
	if err != nil {
		return nil, err
	}
	v44, err := previousV44ModelPlanBundle(current)
	if err != nil {
		return nil, err
	}
	v43, err := previousV43ModelPlanBundle(v44)
	if err != nil {
		return nil, err
	}
	managerV42, err := previousManagerModelPlanBundleV42(v43)
	if err != nil {
		return nil, err
	}
	managerV41, err := previousManagerModelPlanBundleV41(managerV42)
	if err != nil {
		return nil, err
	}
	managerV40, err := previousManagerModelPlanBundleV40(managerV41)
	if err != nil {
		return nil, err
	}
	managerV39, err := previousManagerModelPlanBundleV39(managerV40)
	if err != nil {
		return nil, err
	}
	withExplore := make([]modelPlanBundle, 0, 16)
	for _, manager := range []modelPlanBundle{current, broadPredecessor, v45, v44, v43, managerV42, managerV41, managerV40, managerV39} {
		withExplore = append(withExplore, manager)
		explore, err := previousExploreModelPlanBundle(manager)
		if err != nil {
			return nil, err
		}
		withExplore = append(withExplore, explore)
	}
	candidates := make([]modelPlanBundle, 0, 48)
	for _, candidate := range withExplore {
		candidates = append(candidates, candidate)
		sddBundle, err := previousSDDModelPlanBundle(candidate)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, sddBundle)
		legacySDDBundle, err := previousSDDModelPlanBundleV2(candidate)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, legacySDDBundle)
	}
	unique := make([]modelPlanBundle, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		digest := artifactSHA256(candidate.manifest)
		if !seen[digest] {
			unique, seen[digest] = append(unique, candidate), true
		}
	}
	return unique, nil
}

func managerPredecessors(current modelPlanBundle) ([][]byte, error) {
	v45, err := previousV45ModelPlanBundle(current)
	if err != nil {
		return nil, err
	}
	v44, err := previousV44ModelPlanBundle(current)
	if err != nil {
		return nil, err
	}
	v43, err := previousV43ModelPlanBundle(v44)
	if err != nil {
		return nil, err
	}
	v42, err := previousManagerModelPlanBundleV42(v43)
	if err != nil {
		return nil, err
	}
	v41, err := previousManagerModelPlanBundleV41(v42)
	if err != nil {
		return nil, err
	}
	v40, err := previousManagerModelPlanBundleV40(v41)
	if err != nil {
		return nil, err
	}
	v39, err := previousManagerModelPlanBundleV39(v40)
	if err != nil {
		return nil, err
	}
	return [][]byte{v45.agents[managerAgentName], v44.agents[managerAgentName], v43.agents[managerAgentName], v42.agents[managerAgentName], v41.agents[managerAgentName], v40.agents[managerAgentName], v39.agents[managerAgentName]}, nil
}

func previousV45ModelPlanBundle(current modelPlanBundle) (modelPlanBundle, error) {
	return fullModelPlanBundle(current.config, current.resolved, previousManagerPromptV45, "artifact: opencode-agent/vgxness-manager; version: 45", previousGeneralPromptV4, "artifact: opencode-agent/general; version: 4", canonicalVerifierPrompt, "artifact: opencode-agent/vgxness-verifier; version: 3", currentReviewPrompts())
}

func previousV44ModelPlanBundle(current modelPlanBundle) (modelPlanBundle, error) {
	return fullModelPlanBundle(current.config, current.resolved, previousManagerPromptV44, "artifact: opencode-agent/vgxness-manager; version: 44", previousGeneralPromptV3, "artifact: opencode-agent/general; version: 3", canonicalVerifierPrompt, "artifact: opencode-agent/vgxness-verifier; version: 3", currentReviewPrompts())
}

func previousV43ModelPlanBundle(current modelPlanBundle) (modelPlanBundle, error) {
	return fullModelPlanBundle(current.config, current.resolved, previousManagerPromptV43, "artifact: opencode-agent/vgxness-manager; version: 43", previousGeneralPromptV2, "artifact: opencode-agent/general; version: 2", previousVerifierPromptV2, "artifact: opencode-agent/vgxness-verifier; version: 2", previousReviewPromptsV2())
}

func previousManagerModelPlanBundleV42(current modelPlanBundle) (modelPlanBundle, error) {
	manager, err := bindManagerTemplate(previousManagerPromptV42, "artifact: opencode-agent/vgxness-manager; version: 42", current.resolved.Roles[sdd.RoleManager])
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents := cloneAgents(current.agents)
	agents[managerAgentName] = manager
	return encodeModelPlanBundle(current.config, current.resolved, agents)
}

func previousManagerModelPlanBundleV41(current modelPlanBundle) (modelPlanBundle, error) {
	manager, err := bindManagerTemplate(previousManagerPromptV41, "artifact: opencode-agent/vgxness-manager; version: 41", current.resolved.Roles[sdd.RoleManager])
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents := cloneAgents(current.agents)
	agents[managerAgentName] = manager
	return encodeModelPlanBundle(current.config, current.resolved, agents)
}

func previousManagerModelPlanBundleV40(current modelPlanBundle) (modelPlanBundle, error) {
	manager, err := bindManagerTemplate(previousManagerPromptV40, "artifact: opencode-agent/vgxness-manager; version: 40", current.resolved.Roles[sdd.RoleManager])
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents := cloneAgents(current.agents)
	agents[managerAgentName] = manager
	return encodeModelPlanBundle(current.config, current.resolved, agents)
}

func previousManagerModelPlanBundleV39(current modelPlanBundle) (modelPlanBundle, error) {
	manager, err := bindManagerTemplate(previousManagerPromptV39, "artifact: opencode-agent/vgxness-manager; version: 39", current.resolved.Roles[sdd.RoleManager])
	if err != nil {
		return modelPlanBundle{}, err
	}
	agents := cloneAgents(current.agents)
	agents[managerAgentName] = manager
	return encodeModelPlanBundle(current.config, current.resolved, agents)
}

func cloneAgents(current map[string][]byte) map[string][]byte {
	agents := make(map[string][]byte, len(current))
	for name, content := range current {
		agents[name] = content
	}
	return agents
}

func previousSDDModelPlanBundle(current modelPlanBundle) (modelPlanBundle, error) {
	agents := make(map[string][]byte, len(current.agents))
	for name, content := range current.agents {
		agents[name] = content
	}
	if strings.Contains(string(agents[generalAgentName]), generalCurrentMarker) {
		agents[generalAgentName] = previousGeneralPredecessor(agents[generalAgentName])
		if len(agents[generalAgentName]) == 0 {
			return modelPlanBundle{}, integration.ErrInvalid
		}
	}
	if strings.Contains(string(agents[verifierAgentName]), verifierCurrentMarker) {
		agents[verifierAgentName] = previousVerifierPredecessor(agents[verifierAgentName])
		if len(agents[verifierAgentName]) == 0 {
			return modelPlanBundle{}, integration.ErrInvalid
		}
	}
	for _, profile := range []struct {
		name string
		role sdd.Role
	}{
		{sddResearchName, sdd.RoleResearch}, {sddProposalName, sdd.RoleProposal}, {sddSpecName, sdd.RoleSpec}, {sddDesignName, sdd.RoleDesign}, {sddTasksName, sdd.RoleTasks},
	} {
		agents[profile.name] = previousSDDAgentPredecessor(profile.role, agents[profile.name])
		if len(agents[profile.name]) == 0 {
			return modelPlanBundle{}, integration.ErrInvalid
		}
	}
	return encodeModelPlanBundle(current.config, current.resolved, agents)
}

func previousBroadPermissionModelPlanBundle(current modelPlanBundle) (modelPlanBundle, error) {
	agents := cloneAgents(current.agents)
	agents[generalAgentName] = previousGeneralPredecessor(agents[generalAgentName])
	agents[verifierAgentName] = previousVerifierPredecessor(agents[verifierAgentName])
	if len(agents[generalAgentName]) == 0 || len(agents[verifierAgentName]) == 0 {
		return modelPlanBundle{}, integration.ErrInvalid
	}
	return encodeModelPlanBundle(current.config, current.resolved, agents)
}

func previousSDDModelPlanBundleV2(current modelPlanBundle) (modelPlanBundle, error) {
	predecessor, err := previousSDDModelPlanBundle(current)
	if err != nil {
		return modelPlanBundle{}, err
	}
	for _, profile := range []struct {
		name string
		role sdd.Role
	}{
		{sddResearchName, sdd.RoleResearch}, {sddProposalName, sdd.RoleProposal}, {sddSpecName, sdd.RoleSpec}, {sddDesignName, sdd.RoleDesign}, {sddTasksName, sdd.RoleTasks}, {sddApplyName, sdd.RoleApply},
	} {
		predecessor.agents[profile.name] = legacySDDAgentPredecessor(profile.role, predecessor.agents[profile.name])
		if len(predecessor.agents[profile.name]) == 0 {
			return modelPlanBundle{}, integration.ErrInvalid
		}
	}
	return encodeModelPlanBundle(predecessor.config, predecessor.resolved, predecessor.agents)
}

func previousExploreModelPlanBundle(current modelPlanBundle) (modelPlanBundle, error) {
	predecessor := previousExplorePredecessor(current.agents[exploreAgentName])
	if len(predecessor) == 0 {
		return modelPlanBundle{}, integration.ErrInvalid
	}
	agents := make(map[string][]byte, len(current.agents))
	for name, content := range current.agents {
		agents[name] = content
	}
	agents[exploreAgentName] = predecessor
	return encodeModelPlanBundle(current.config, current.resolved, agents)
}

func modelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	return fullModelBoundAgents(plan, bindManager, canonicalGeneralPrompt, generalPreviousMarker, canonicalVerifierPrompt, verifierPreviousMarker, currentReviewPrompts(), true)
}

func fullModelPlanBundle(config sdd.ModelPlanConfig, resolved sdd.OpenCodePlan, managerBase, managerMarker, generalBase, generalMarker, verifierBase, verifierMarker string, reviews map[string]string) (modelPlanBundle, error) {
	managerBinder := func(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
		return bindManagerTemplate(managerBase, managerMarker, assignment)
	}
	agents, err := fullModelBoundAgents(resolved, managerBinder, generalBase, generalMarker, verifierBase, verifierMarker, reviews, false)
	if err != nil {
		return modelPlanBundle{}, err
	}
	return encodeModelPlanBundle(config, resolved, agents)
}

func fullModelBoundAgents(plan sdd.OpenCodePlan, managerBinder func(sdd.OpenCodeRoleAssignment) ([]byte, error), generalBase, generalMarker, verifierBase, verifierMarker string, baseReviews map[string]string, protectDurableMutations bool) (map[string][]byte, error) {
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
	generalNext := generalMarker
	if protectDurableMutations {
		generalNext = generalCurrentMarker
	}
	general, err := bindProfile(generalBase, generalMarker, generalNext, plan.Roles[sdd.RoleImplementation], protectDurableMutations)
	if err != nil {
		return nil, err
	}
	agents[generalAgentName] = general
	verifierNext := verifierMarker
	if protectDurableMutations {
		verifierNext = verifierCurrentMarker
	}
	verifier, err := bindProfile(verifierBase, verifierMarker, verifierNext, plan.Roles[sdd.RoleVerification], protectDurableMutations)
	if err != nil {
		return nil, err
	}
	agents[verifierAgentName] = verifier
	for name, base := range baseReviews {
		content, bindErr := bindAgent(base, assignments[name], plan.Roles[assignments[name]])
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

func bindManager(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	return bindManagerTemplate(canonicalManagerPrompt, "artifact: opencode-agent/vgxness-manager; version: 46", assignment)
}

func currentReviewPrompts() map[string]string {
	return map[string]string{reviewRiskName: reviewRiskPrompt, reviewReadabilityName: reviewReadabilityPrompt, reviewReliabilityName: reviewReliabilityPrompt, reviewResilienceName: reviewResiliencePrompt, reviewRefuterName: reviewRefuterPrompt}
}
func previousReviewPromptsV2() map[string]string {
	return map[string]string{reviewRiskName: previousReviewRiskPromptV2, reviewReadabilityName: previousReviewReadabilityPromptV2, reviewReliabilityName: previousReviewReliabilityPromptV2, reviewResilienceName: previousReviewResiliencePromptV2, reviewRefuterName: previousReviewRefuterPromptV2}
}

var compactProtocolAgentNames = []string{generalAgentName, verifierAgentName, reviewRiskName, reviewReadabilityName, reviewReliabilityName, reviewResilienceName, reviewRefuterName}

func bindManagerTemplate(base, marker string, assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	value := base
	anchor := "color: primary\n"
	if strings.Count(value, anchor) != 1 || strings.Count(value, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value = strings.Replace(value, anchor, fmt.Sprintf("color: primary\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
	return []byte(value), nil
}

func bindProfile(base, marker, nextMarker string, assignment sdd.OpenCodeRoleAssignment, protectDurableMutations bool) ([]byte, error) {
	anchor := "mode: subagent\n"
	permission := "  \"*\": allow\n"
	if strings.Count(base, anchor) != 1 || strings.Count(base, marker) != 1 || strings.Count(base, permission) != 1 {
		return nil, integration.ErrInvalid
	}
	value := strings.Replace(base, anchor, fmt.Sprintf("mode: subagent\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
	value = strings.Replace(value, marker, nextMarker, 1)
	if !protectDurableMutations {
		return []byte(value), nil
	}
	// OpenCode has no trusted MCP caller identity. Host/operator full-mode choice
	// remains the authority boundary; these managed profiles deny durable writes.
	value = strings.Replace(value, permission, permission+durableMutationDenies, 1)
	return []byte(value), nil
}

func previousGeneralPredecessor(current []byte) []byte {
	return derivePredecessor(current, []textReplacement{{old: generalCurrentMarker, new: generalPreviousMarker}, {old: durableMutationDenies, new: ""}})
}
func previousVerifierPredecessor(current []byte) []byte {
	return derivePredecessor(current, []textReplacement{{old: verifierCurrentMarker, new: verifierPreviousMarker}, {old: durableMutationDenies, new: ""}})
}

const durableMutationDenies = "  vgxness_memory_save: deny\n  vgxness_memory_forget: deny\n  vgxness_sdd_create: deny\n  vgxness_sdd_set_interaction_mode: deny\n  vgxness_sdd_save_revision: deny\n  vgxness_sdd_accept_revision: deny\n  vgxness_sdd_transition: deny\n  vgxness_sdd_record_projection: deny\n"

func bindExplore(assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	value := explorePrompt
	anchor := "mode: subagent\n"
	marker := "artifact: opencode-agent/explore; version: 2"
	if strings.Count(value, anchor) != 1 || strings.Count(value, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value = strings.Replace(value, anchor, fmt.Sprintf("mode: subagent\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
	return []byte(value), nil
}

func previousExplorePredecessor(current []byte) []byte {
	return derivePredecessor(current, []textReplacement{
		{old: "codegraph_codegraph_explore: allow", new: "codegraph_explore: allow"},
		{old: "artifact: opencode-agent/explore; version: 2", new: "artifact: opencode-agent/explore; version: 1"},
		{old: "Use codegraph_codegraph_explore first", new: "Use codegraph_explore first"},
	})
}

func bindAgent(base string, role sdd.Role, assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	value := base
	marker := fmt.Sprintf("artifact: opencode-agent/vgxness-review-%s; version:", role)
	if strings.Count(value, "hidden: true\n") != 1 || strings.Count(value, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value = strings.Replace(value, "hidden: true\n", fmt.Sprintf("hidden: true\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
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

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-apply; version: %d -->

You are the read-only implementation and patch composer for one accepted SDD tasks revision. Compose a hash-bound candidate. Reject a mission unless it contains exact change ID, task IDs, accepted task revision ID and SHA-256 digest, every accepted input revision ID and digest, exact relevant native skill names, allowed paths with current content hashes, acceptance criteria, exact validation commands, and required RED/TDD evidence.

Inspect only the accepted scope. Do not edit, execute shell commands or tests, delegate, ask questions, persist memory, call SDD write or lifecycle tools, select models, install packages, use network, commit, push, or alter OpenSpec projections. Produce a bounded patch proposal whose paths stay within the mission and whose expected original hashes prevent stale application. Preserve the RED/GREEN plan and identify exact developmental and final validation commands. The manager validates bindings and hashes; managed general performs workspace writes and exact OpenSpec or hybrid projection writes; verifier executes final validation; reviewers assess the same frozen candidate; the manager saves or accepts revisions, records projections, and advances lifecycle state.

Return exactly one compact JSON object and no Markdown:
{"status":"complete|blocked","proposedChanges":[{"path":"allowed path","expectedSHA256":"current file digest","patch":"bounded exact proposed change"}],"validationPlan":[{"command":"exact command","purpose":"RED|GREEN|regression|static"}],"tddEvidence":{"redPlan":"expected pre-change failure","greenPlan":"expected post-change pass"},"summary":"bounded implementation rationale","blockers":["blocking fact"]}
	`, assignment.Model, assignment.Variant, sddApplyTargetVersion) + sddSkillLoadingContract
	}
	return readOnlySDDAgentPrompt(role, assignment)
}

func readOnlySDDAgentPrompt(role sdd.Role, assignment sdd.OpenCodeRoleAssignment) string {
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

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-%s; version: %d -->

You are the read-only SDD %s agent. Accept one exact manager mission bound to a change ID, accepted input revision IDs and SHA-256 digests, requested artifact, evidence scope, and return contract. Read only the workspace and bounded SDD records needed for that artifact.

Do not delegate, ask questions, edit files, execute shell commands, persist memory, save or accept revisions, record projections, transition phases, route work, or select models. Return bounded evidence and candidate artifact content to the manager. The manager alone validates, persists, accepts, and advances SDD lifecycle state.
`, role, assignment.Model, assignment.Variant, role, sddReadOnlyTargetVersion, role) + sddSkillLoadingContract + phaseAgentContract(role)
}

const sddSkillLoadingContract = `

Mission schema requires "skills":["exact relevant native skill name"]. The exact skill list is required; an empty list is allowed only when the manager determined none apply. Load every supplied applicable native skill with the skill tool before phase work. Do not discover, invent, or self-route skills. If a supplied skill cannot be loaded, report it as unavailable in the bounded result.
`

func previousSDDAgentPredecessor(role sdd.Role, current []byte) []byte {
	target, prior := sddReadOnlyTargetVersion, sddReadOnlyPredecessorVersion
	if role == sdd.RoleApply {
		target, prior = sddApplyTargetVersion, sddApplyPredecessorVersion
	}
	replacements := []textReplacement{{old: fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: %d", role, target), new: fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: %d", role, prior)}}
	if role == sdd.RoleApply {
		replacements = append(replacements, textReplacement{old: sddSkillLoadingContract, new: ""}, textReplacement{old: ", exact relevant native skill names, allowed paths", new: ", allowed paths"})
		return derivePredecessor(current, replacements)
	}
	if role == sdd.RoleResearch {
		replacements = append(replacements, textReplacement{old: researchBootstrapPhaseAgentContract(), new: legacyPhaseAgentContract(role)})
	}
	return derivePredecessor(current, replacements)
}

func legacySDDAgentPredecessor(role sdd.Role, current []byte) []byte {
	if role == sdd.RoleApply {
		return derivePredecessor(current, []textReplacement{
			{old: fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: %d", role, sddApplyTargetVersion), new: fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: %d", role, sddApplyPredecessorVersion)},
			{old: sddSkillLoadingContract, new: ""},
			{old: ", exact relevant native skill names, allowed paths", new: ", allowed paths"},
		})
	}
	return derivePredecessor(current, []textReplacement{
		{old: fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: %d", role, sddReadOnlyPredecessorVersion), new: fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: %d", role, sddReadOnlyPredecessorVersion-1)},
		{old: sddSkillLoadingContract, new: ""},
		{old: `,"skills":["exact relevant native skill name"]`, new: ""},
	})
}

func phaseAgentContract(role sdd.Role) string {
	if role == sdd.RoleResearch {
		return researchBootstrapPhaseAgentContract()
	}
	return legacyPhaseAgentContract(role)
}

func researchBootstrapPhaseAgentContract() string {
	return `

Mission schema: {"changeId":"exact ID","artifact":"explore","acceptedInputs":[],"skills":["exact relevant native skill name"],"evidenceScope":["bounded path or question"],"constraints":["constraint"],"returnContract":"phase-candidate-v1"}. acceptedInputs:[] is permitted only for the first-phase research/explore bootstrap. Reject non-empty or fabricated bootstrap inputs, and reject a mission with missing, stale, contradictory, or broader inputs.

Phase objective: Establish repository evidence, constraints, affected surfaces, unknowns, and decisions needed before proposing a change.

Return exactly one compact JSON object and no Markdown:
{"status":"complete|blocked","candidateContent":"complete artifact candidate or empty when blocked","evidence":["path:line or exact observation"],"openQuestions":["unresolved consequential question"],"blockers":["blocking fact"]}
`
}

func legacyPhaseAgentContract(role sdd.Role) string {
	objective := map[sdd.Role]string{
		sdd.RoleResearch: "Establish repository evidence, constraints, affected surfaces, unknowns, and decisions needed before proposing a change.",
		sdd.RoleProposal: "Define the problem, intended outcomes, scope, non-goals, risks, and measurable success criteria.",
		sdd.RoleSpec:     "Define normative behavior, scenarios, edge cases, failure behavior, and testable acceptance criteria without choosing incidental implementation detail.",
		sdd.RoleDesign:   "Define architecture, data and control flow, interfaces, safety boundaries, migration or rollback needs, and verification strategy against accepted specifications.",
		sdd.RoleTasks:    "Produce ordered implementation tasks with stable IDs, dependencies, allowed paths, RED or regression evidence, validation commands, and completion criteria.",
	}[role]
	return fmt.Sprintf(`

Mission schema: {"changeId":"exact ID","artifact":"%s","acceptedInputs":[{"artifactId":"exact ID","revisionId":"exact ID","digest":"sha256"}],"skills":["exact relevant native skill name"],"evidenceScope":["bounded path or question"],"constraints":["constraint"],"returnContract":"phase-candidate-v1"}. Reject a mission with missing, stale, contradictory, or broader inputs.

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
