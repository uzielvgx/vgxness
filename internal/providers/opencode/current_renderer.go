package opencode

import (
	"errors"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

// nativeSkillsExternalDirectory is the single scoped external grant shared by the
// read-only roles: only the conventional skill root, never the whole home or a
// provider configuration that may contain secrets. It is added to current
// headers and stripped from the historical pre-adaptive headers so those exact
// legacy bytes remain recognizable.
const nativeSkillsExternalDirectory = "  external_directory:\n    \"~/.agents/skills/**\": allow\n"

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
  webfetch: allow
  external_directory:
    "~/.agents/skills/**": allow
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
  codegraph_codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
  external_directory:
    "~/.agents/skills/**": allow
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
  codegraph_codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
  external_directory:
    "~/.agents/skills/**": allow
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
  codegraph_codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
  external_directory:
    "~/.agents/skills/**": allow
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
  edit: deny
  task: deny
---
`,
}

var currentNativeMarkers = map[string]string{
	"explore.md":                 "<!-- managed-by: vgxness; artifact: opencode-agent/explore; version: 5 -->",
	"general.md":                 "<!-- managed-by: vgxness; artifact: opencode-agent/general; version: 10 -->",
	"vgxness-care-challenger.md": "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-challenger; version: 4 -->",
	"vgxness-care-reviewer.md":   "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-reviewer; version: 4 -->",
	"vgxness-care-specialist.md": "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-specialist; version: 4 -->",
	"vgxness-manager.md":         "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-manager; version: 62 -->",
	"vgxness-verifier.md":        "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-verifier; version: 8 -->",
}

const currentNativeAdapter = `

# Native OpenCode adapter
Use the native model assignment and permission map above. Native task targets are explore, general, vgxness-verifier, vgxness-care-reviewer, vgxness-care-specialist, vgxness-care-challenger. Delegate all project code exploration to explore through the native task tool; the Manager keeps only the narrow operational-inspection exception defined by the shared contract and never uses it for general exploration. Load a skill only for your own action and within the native permission map; each role loads only the skills for its own action and workers load only the scoped selection delegated to them, never the whole catalog or a skill body on another role's behalf. Memory uses configured VGXNESS MCP tools; only Manager performs authorized Git delivery. Missing native transport/authentication is an unavailable dependency. Native capabilities never grant authorization. A shell is not a read-only guarantee: verifier/CARE checks are contractually non-mutating, but native prompt or permission restrictions are not a hard sandbox.
Registry metadata is an authorized Manager orchestration query, not project exploration. The Manager may read only the absolute VGXNESS launcher already bound as the managed vgxness MCP server command, from the active OpenCode configuration at its evidenced location (mcp.vgxness.command[0]); that single binding read is an authorized bounded orchestration read. Never assume a default config path, search PATH, dump secrets, or fall back silently to a different launcher. Run "<absolute launcher>" skills registry search --workspace <workspace> --query <term> --limit <n> --json, or the same launcher with skills registry resolve --name <id-or-absolute-path> --json. Consume bounded metadata only (id, name, description, status, sha256); never read a skill body, and if the launcher or subcommand is unavailable report the dependency unavailable instead of browsing the repository. External file access is scoped per role: only the read-only explore and CARE roles declare an explicit external_directory grant for the conventional skill root (~/.agents/skills/**); the manager, general, and verifier roles keep their own declared permission maps and receive no additional external grant. For explore and CARE, the full home, provider configuration that may contain secrets, and other external paths remain outside that grant; custom roots require an explicit external_directory binding. Do not infer a denial for another role from these read-only-role restrictions; distinguish its declared capabilities, task authorization, and any observed host denial.`

// preAdaptiveNativeAdapter, preAdaptiveNativeHeaders, and
// preAdaptiveNativeMarkers preserve the exact Manager61-era native bytes so an
// unreceipted pre-adaptive install is still recognized without rewriting the
// historical goldens. Current installs use the current values above.
const preAdaptiveNativeAdapter = `

# Native OpenCode adapter
Use the native model assignment and permission map above. Native task targets are explore, general, vgxness-verifier, vgxness-care-reviewer, vgxness-care-specialist, vgxness-care-challenger. Load applicable skills through the native skill tool. Memory uses configured VGXNESS MCP tools; only Manager performs authorized Git delivery. Missing native transport/authentication is an unavailable dependency. Native capabilities never grant authorization.`

var preAdaptiveNativeHeaders = func() map[string]string {
	out := make(map[string]string, len(currentNativeHeaders))
	for name, header := range currentNativeHeaders {
		out[name] = header
	}
	for _, name := range []string{"vgxness-care-challenger.md", "vgxness-care-reviewer.md", "vgxness-care-specialist.md"} {
		out[name] = strings.Replace(out[name], "codegraph_codegraph_explore: allow", "codegraph_explore: allow", 1)
	}
	out["vgxness-verifier.md"] = strings.Replace(out["vgxness-verifier.md"], "  edit: deny\n  task: deny\n", "", 1)
	// The historical pre-adaptive headers predate the scoped skill-root and
	// public web grants, so strip the current additions to keep those exact
	// bytes recognized without rewriting the historical bootstrap goldens.
	for _, name := range []string{"explore.md", "vgxness-care-challenger.md", "vgxness-care-reviewer.md", "vgxness-care-specialist.md"} {
		out[name] = strings.Replace(out[name], nativeSkillsExternalDirectory, "", 1)
	}
	out["explore.md"] = strings.Replace(out["explore.md"], "  webfetch: allow\n", "", 1)
	return out
}()

var preAdaptiveNativeMarkers = func() map[string]string {
	out := make(map[string]string, len(currentNativeMarkers))
	for name, marker := range currentNativeMarkers {
		out[name] = marker
	}
	out["vgxness-care-challenger.md"] = "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-challenger; version: 2 -->"
	out["vgxness-care-reviewer.md"] = "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-reviewer; version: 2 -->"
	out["vgxness-care-specialist.md"] = "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-care-specialist; version: 2 -->"
	out["vgxness-verifier.md"] = "<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-verifier; version: 7 -->"
	out["explore.md"] = "<!-- managed-by: vgxness; artifact: opencode-agent/explore; version: 4 -->"
	return out
}()

func renderCurrentAgents(assignments map[string]modelplan.OpenCodeRoleAssignment) (map[string][]byte, error) {
	contract, err := orchestration.LoadManagerContract()
	if err != nil {
		return nil, err
	}
	return renderAgentsForContract(assignments, contract, orchestration.ManagerContractDigest(), currentNativeHeaders, currentNativeMarkers, currentNativeAdapter)
}

func renderBootstrapAgents(assignments map[string]modelplan.OpenCodeRoleAssignment) (map[string][]byte, error) {
	contract, err := orchestration.LoadPreAdaptiveManagerContract()
	if err != nil {
		return nil, err
	}
	return renderAgentsForContract(assignments, contract, orchestration.PreAdaptiveManagerContractDigest(), preAdaptiveNativeHeaders, preAdaptiveNativeMarkers, preAdaptiveNativeAdapter)
}

func renderAgentsForContract(assignments map[string]modelplan.OpenCodeRoleAssignment, contract orchestration.ManagerContract, digest string, headers, markers map[string]string, adapter string) (map[string][]byte, error) {
	agents := make(map[string][]byte, len(headers))
	for name, header := range headers {
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
		agents[name] = []byte(header + "\n" + markers[name] + "\n\n" + body + adapter + "\nContract identity: " + contract.Identity + "; content SHA256: " + digest + "\n")
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
func buildBootstrapModelPlanBundle(c modelplan.ModelPlanConfig) (modelPlanBundle, error) {
	p, e := modelplan.ResolveOpenCodePlan(c)
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	agents, e := renderBootstrapAgents(currentRoleAssignments(p.Roles))
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
func buildBootstrapModelPlanBundleV2(c modelplan.ModelPlanConfigV2) (modelPlanBundle, error) {
	p, e := modelplan.ResolveOpenCodePlanV2(c)
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	roles := map[modelplan.Role]modelplan.OpenCodeRoleAssignment{}
	for role, a := range p.Roles {
		roles[role] = modelplan.OpenCodeRoleAssignment{Role: role, Model: a.Model, Variant: a.Variant}
	}
	agents, e := renderBootstrapAgents(currentRoleAssignments(roles))
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
func buildBootstrapModelPlanBundleV3(c modelplan.ModelPlanConfigV3) (modelPlanBundle, error) {
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
	agents, e := renderBootstrapAgents(assignments)
	if e != nil {
		return modelPlanBundle{}, errors.Join(integration.ErrInvalid, e)
	}
	return encodeModelPlanBundleV3(c, p, agents)
}
