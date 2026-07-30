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
	manifest, err := parseModelPlanManifest(data)
	if err != nil {
		return modelPlanManifest{}, modelPlanBundle{}, err
	}
	bundle, err := buildModelPlanBundle(manifest.Config)
	return manifest, bundle, err
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

func modelBoundAgents(plan sdd.OpenCodePlan) (map[string][]byte, error) {
	return fullModelBoundAgents(plan, bindManager, canonicalGeneralPrompt, "artifact: opencode-agent/general; version: 2", canonicalVerifierPrompt, "artifact: opencode-agent/vgxness-verifier; version: 2")
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
	return bindManagerTemplate(canonicalManagerPrompt, "artifact: opencode-agent/vgxness-manager; version: 35", assignment)
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

func bindProfile(base, marker string, assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	anchor := "mode: subagent\n"
	if strings.Count(base, anchor) != 1 || strings.Count(base, marker) != 1 {
		return nil, integration.ErrInvalid
	}
	value := strings.Replace(base, anchor, fmt.Sprintf("mode: subagent\nmodel: %s\nvariant: %s\n", assignment.Model, assignment.Variant), 1)
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

func bindAgent(base string, role sdd.Role, assignment sdd.OpenCodeRoleAssignment) ([]byte, error) {
	value := base
	marker := fmt.Sprintf("artifact: opencode-agent/vgxness-review-%s; version: 2", role)
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

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-apply; version: 3 -->

You are the read-only implementation and patch composer for one accepted SDD tasks revision. Compose a hash-bound candidate. Reject a mission unless it contains exact change ID, task IDs, accepted task revision ID and SHA-256 digest, every accepted input revision ID and digest, allowed paths with current content hashes, acceptance criteria, exact validation commands, and required RED/TDD evidence.

Inspect only the accepted scope. Do not edit, execute shell commands or tests, delegate, ask questions, persist memory, call SDD write or lifecycle tools, select models, install packages, use network, commit, push, or alter OpenSpec projections. Produce a bounded patch proposal whose paths stay within the mission and whose expected original hashes prevent stale application. Preserve the RED/GREEN plan and identify exact developmental and final validation commands. The manager validates bindings and hashes; managed general performs workspace writes and exact OpenSpec or hybrid projection writes; verifier executes final validation; reviewers assess the same frozen candidate; the manager saves or accepts revisions, records projections, and advances lifecycle state.

Return exactly one compact JSON object and no Markdown:
{"status":"complete|blocked","proposedChanges":[{"path":"allowed path","expectedSHA256":"current file digest","patch":"bounded exact proposed change"}],"validationPlan":[{"command":"exact command","purpose":"RED|GREEN|regression|static"}],"tddEvidence":{"redPlan":"expected pre-change failure","greenPlan":"expected post-change pass"},"summary":"bounded implementation rationale","blockers":["blocking fact"]}
	`, assignment.Model, assignment.Variant)
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

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-sdd-%s; version: 2 -->

You are the read-only SDD %s agent. Accept one exact manager mission bound to a change ID, accepted input revision IDs and SHA-256 digests, requested artifact, evidence scope, and return contract. Read only the workspace and bounded SDD records needed for that artifact.

Do not delegate, ask questions, edit files, execute shell commands, persist memory, save or accept revisions, record projections, transition phases, route work, or select models. Return bounded evidence and candidate artifact content to the manager. The manager alone validates, persists, accepts, and advances SDD lifecycle state.
`, role, assignment.Model, assignment.Variant, role, role) + phaseAgentContract(role)
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
