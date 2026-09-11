package opencode

import (
	"errors"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

// Current native headers contain permissions and presentation only. Policy comes
// exclusively from the current shared contract, never a predecessor renderer.
var currentNativeHeaders = map[string]string{
	"explore.md": `---
description: VGXNESS-managed read-only repository exploration
mode: subagent
model: {{model}}
{{variant}}permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_codegraph_explore: allow
---
`,
	"general.md": `---
description: VGXNESS-managed general implementation worker
mode: subagent
model: {{model}}
{{variant}}hidden: true
permission:
  "*": allow
  vgxness_memory_save: deny
  vgxness_memory_forget: deny
  vgxness_memory_session_summary: deny
  vgxness_memory_update: deny
  vgxness_sdd_create: deny
  vgxness_sdd_set_interaction_mode: deny
  vgxness_sdd_save_revision: deny
  vgxness_sdd_accept_revision: deny
  vgxness_sdd_transition: deny
  vgxness_sdd_record_projection: deny
---
`,
	"vgxness-care-challenger.md": `---
description: CARE challenger for typed material targets
mode: subagent
hidden: true
model: {{model}}
{{variant}}permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---
`,
	"vgxness-care-reviewer.md": `---
description: CARE reviewer for a frozen candidate
mode: subagent
hidden: true
model: {{model}}
{{variant}}permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---
`,
	"vgxness-care-specialist.md": `---
description: CARE specialist for one bounded assurance domain
mode: subagent
hidden: true
model: {{model}}
{{variant}}permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---
`,
	"vgxness-manager.md": `---
description: VGXNESS manager - OpenCode orchestration, lifecycle, and native delivery authority
mode: primary
color: primary
model: {{model}}
{{variant}}permission:
  "*": allow
---
`,
	"vgxness-verifier.md": `---
description: VGXNESS independent final executable verifier for one frozen candidate
mode: subagent
model: {{model}}
{{variant}}hidden: true
permission:
  "*": allow
  vgxness_memory_save: deny
  vgxness_memory_forget: deny
  vgxness_memory_session_summary: deny
  vgxness_memory_update: deny
  vgxness_sdd_create: deny
  vgxness_sdd_set_interaction_mode: deny
  vgxness_sdd_save_revision: deny
  vgxness_sdd_accept_revision: deny
  vgxness_sdd_transition: deny
  vgxness_sdd_record_projection: deny
---
`,
}
var currentNativeMarkers = map[string]string{
	"explore.md":                 "<!-- managed-by: vgxness; artifact: opencode-agent/explore; version: 4 -->",
	"general.md":                 "<!-- managed-by: vgxness; artifact: opencode-agent/general; version: 10 -->",
	"vgxness-care-challenger.md": "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-challenger; version: 2 -->",
	"vgxness-care-reviewer.md":   "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-reviewer; version: 2 -->",
	"vgxness-care-specialist.md": "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-specialist; version: 2 -->",
	"vgxness-manager.md":         "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-manager; version: 62 -->",
	"vgxness-verifier.md":        "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-verifier; version: 7 -->",
}

const currentNativeAdapter = `

# Native OpenCode adapter
Use the native model assignment and permission map above. Native task targets are explore, general, vgxness-verifier, vgxness-care-reviewer, vgxness-care-specialist, vgxness-care-challenger. Load applicable skills through the native skill tool. Memory uses configured VGXNESS MCP tools; only Manager performs authorized Git delivery. Missing native transport/authentication is an unavailable dependency. Native capabilities never grant authorization.`

func renderCurrentAgents(assignments map[string]modelplan.OpenCodeRoleAssignment) (map[string][]byte, error) {
	contract, err := orchestration.LoadManagerContract()
	if err != nil {
		return nil, err
	}
	agents := make(map[string][]byte, len(currentNativeHeaders))
	for name, header := range currentNativeHeaders {
		assignment, ok := assignments[name]
		if !ok || assignment.Model == "" || strings.ContainsAny(assignment.Model+string(assignment.Variant), "\r\n") {
			return nil, integration.ErrInvalid
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "vgxness-"), ".md")
		role, ok := contract.Role(id)
		if !ok {
			return nil, integration.ErrInvalid
		}
		body := role.Instructions
		if id == "manager" {
			body = contract.RenderManagerSections()
		}
		variant := ""
		if assignment.Variant != "" {
			variant = "variant: " + string(assignment.Variant) + "\n"
		}
		header = strings.NewReplacer("{{model}}", assignment.Model, "{{variant}}", variant).Replace(header)
		agents[name] = []byte(header + "\n" + currentNativeMarkers[name] + "\n\n" + body + currentNativeAdapter + "\nContract identity: " + contract.Identity + "; content SHA256: " + orchestration.ManagerContractDigest() + "\n")
	}
	return agents, nil
}
func currentRoleAssignments(roles map[modelplan.Role]modelplan.OpenCodeRoleAssignment) map[string]modelplan.OpenCodeRoleAssignment {
	out := map[string]modelplan.OpenCodeRoleAssignment{}
	for _, identity := range ModelAgentInventoryV3() {
		out[strings.TrimPrefix(identity.ArtifactKey, "agents/")] = roles[identity.Role]
	}
	return out
}
func buildModelPlanBundle(c modelplan.ModelPlanConfig) (modelPlanBundle, error) {
	p, e := modelplan.ResolveOpenCodePlan(c)
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	agents, e := renderCurrentAgents(currentRoleAssignments(p.Roles))
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	return encodeModelPlanBundle(c, p, agents)
}
func buildModelPlanBundleV2(c modelplan.ModelPlanConfigV2) (modelPlanBundle, error) {
	p, e := modelplan.ResolveOpenCodePlanV2(c)
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	roles := map[modelplan.Role]modelplan.OpenCodeRoleAssignment{}
	for role, a := range p.Roles {
		roles[role] = modelplan.OpenCodeRoleAssignment{Role: role, Model: a.Model, Variant: a.Variant}
	}
	agents, e := renderCurrentAgents(currentRoleAssignments(roles))
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	return encodeModelPlanBundleV2(c, p, agents)
}
func buildModelPlanBundleV3(c modelplan.ModelPlanConfigV3) (modelPlanBundle, error) {
	inventory := ModelAgentInventoryV3()
	if len(c.Assignments) == 13 {
		inventory = modelAgentInventoryV3
	}
	p, e := modelplan.ResolveOpenCodePlanV3(c, inventory)
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	assignments := map[string]modelplan.OpenCodeRoleAssignment{}
	for _, a := range p.Assignments {
		assignments[strings.TrimPrefix(a.ArtifactKey, "agents/")] = modelplan.OpenCodeRoleAssignment{Role: a.Role, Model: a.Model, RequestedEffort: a.RequestedEffort, Effort: a.Effort, Variant: a.Variant}
	}
	agents, e := renderCurrentAgents(assignments)
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	return encodeModelPlanBundleV3(c, p, agents)
}
