package opencode

import (
	"bytes"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
	"strings"
)

const sharedManagerMarker = "artifact: opencode-agent/vgxness-manager; version: 62"

// The original modelBoundAgents/bindManager paths are frozen v60 rendering.
// Current packages consume their native headers, replacing ALL policy bodies.
func buildModelPlanBundle(c modelplan.ModelPlanConfig) (modelPlanBundle, error) {
	b, e := buildV60ModelPlanBundle(c)
	if e != nil {
		return b, e
	}
	return sharedManagerBundle(b)
}
func buildModelPlanBundleV2(c modelplan.ModelPlanConfigV2) (modelPlanBundle, error) {
	b, e := buildV60ModelPlanBundleV2(c)
	if e != nil {
		return b, e
	}
	return sharedManagerBundle(b)
}
func buildModelPlanBundleV3(c modelplan.ModelPlanConfigV3) (modelPlanBundle, error) {
	b, e := buildV60ModelPlanBundleV3(c)
	if e != nil {
		return b, e
	}
	b, e = sharedManagerBundle(b)
	if e != nil {
		return b, e
	}
	if len(c.Assignments) == 7 {
		resolved, e := modelplan.ResolveOpenCodePlanV3(c, ModelAgentInventoryV3())
		if e != nil {
			return modelPlanBundle{}, e
		}
		return encodeModelPlanBundleV3(c, resolved, b.agents)
	}
	return b, nil
}

// Legacy slots exist only while reconstructing immutable predecessor bytes.
func expandLegacyModelConfig(c modelplan.ModelPlanConfigV3) (modelplan.ModelPlanConfigV3, error) {
	if len(c.Assignments) != 7 {
		return c, nil
	}
	if _, e := modelplan.ResolveOpenCodePlanV3(c, ModelAgentInventoryV3()); e != nil {
		return c, e
	}
	assignments := make(map[string]modelplan.ManagedAgentModelConfig, 13)
	for k, v := range c.Assignments {
		assignments[k] = v
	}
	for _, identity := range modelAgentInventoryV3[7:] {
		source := "agents/general.md"
		if identity.Role == modelplan.RoleResearch {
			source = "agents/explore.md"
		}
		assignments[identity.ArtifactKey] = assignments[source]
	}
	c.Assignments = assignments
	return c, nil
}
func currentModelConfig(c modelplan.ModelPlanConfigV3) modelplan.ModelPlanConfigV3 {
	assignments := make(map[string]modelplan.ManagedAgentModelConfig, len(c.Assignments))
	for k, v := range c.Assignments {
		assignments[k] = v
	}
	for _, identity := range modelAgentInventoryV3[7:] {
		delete(assignments, identity.ArtifactKey)
	}
	c.Assignments = assignments
	return c
}

func isSharedManagerBundle(b modelPlanBundle) bool {
	return bytes.Contains(b.agents[managerAgentName], []byte(sharedManagerMarker)) || bytes.Contains(b.agents[managerAgentName], []byte("artifact: opencode-agent/vgxness-manager; version: 61"))
}
func managerV60Bundle(b modelPlanBundle) (modelPlanBundle, error) {
	if !isSharedManagerBundle(b) {
		return b, nil
	}
	if b.configV3 != nil {
		return buildV60ModelPlanBundleV3(*b.configV3)
	}
	if b.configV2 != nil {
		return buildV60ModelPlanBundleV2(*b.configV2)
	}
	return buildV60ModelPlanBundle(b.config)
}
func sharedManagerBundle(old modelPlanBundle) (modelPlanBundle, error) {
	c, e := orchestration.LoadManagerContract()
	if e != nil {
		return modelPlanBundle{}, e
	}
	return sharedBundleForContract(old, c, false)
}
func sharedBundleForContract(old modelPlanBundle, c orchestration.ManagerContract, historical bool) (modelPlanBundle, error) {
	agents := map[string][]byte{}
	if len(old.agents) != 13 {
		return modelPlanBundle{}, integration.ErrInvalid
	}
	for name, data := range old.agents {
		text := string(data)
		at := strings.Index(text, "\n---\n")
		if !strings.HasPrefix(text, "---\n") || at < 0 {
			return modelPlanBundle{}, integration.ErrInvalid
		}
		header := text[:at+5]
		start := strings.Index(text, "<!-- managed-by: vgxness;")
		if start < 0 {
			return modelPlanBundle{}, integration.ErrInvalid
		}
		end := strings.Index(text[start:], "-->")
		if end < 0 {
			return modelPlanBundle{}, integration.ErrInvalid
		}
		marker := text[start : start+end+3]
		id := strings.TrimSuffix(strings.TrimPrefix(name, "vgxness-"), ".md")
		r, ok := c.Role(id)
		if !ok {
			if !historical && strings.HasPrefix(id, "sdd-") {
				continue
			}
			return modelPlanBundle{}, integration.ErrInvalid
		}
		body := r.Instructions
		if id == "manager" {
			selectedMarker := sharedManagerMarker
			if historical {
				selectedMarker = "artifact: opencode-agent/vgxness-manager; version: 61"
			}
			marker = strings.Replace(marker, managerCurrentMarker, selectedMarker, 1)
			body = c.RenderManagerSections()
		}
		digest := orchestration.ManagerContractDigest()
		if historical {
			digest = orchestration.PreviousManagerContractDigest()
		}
		agents[name] = []byte(header + "\n" + marker + "\n\n" + body + "\n\n# Native OpenCode adapter\nUse the native model assignment and permission map above. Native task targets are explore, general, vgxness-verifier, vgxness-care-reviewer, vgxness-care-specialist, vgxness-care-challenger and vgxness-sdd-{research,proposal,spec,design,tasks,apply}. Load applicable skills through the native skill tool. Memory and SDD use configured VGXNESS MCP tools; only Manager may mutate lifecycle or perform authorized Git delivery. Missing native transport/authentication is an unavailable dependency. Native capabilities never grant authorization.\nContract identity: " + c.Identity + "; content SHA256: " + digest + "\n")
		if !historical {
			text := string(agents[name])
			text = strings.ReplaceAll(text, " and vgxness-sdd-{research,proposal,spec,design,tasks,apply}", "")
			text = strings.ReplaceAll(text, "Memory and SDD use configured VGXNESS MCP tools; only Manager may mutate lifecycle or perform authorized Git delivery.", "Memory uses configured VGXNESS MCP tools; only Manager performs authorized Git delivery.")
			agents[name] = []byte(text)
		}
	}
	return encodeLike(old, agents)
}
