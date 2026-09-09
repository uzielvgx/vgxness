package opencode

import (
	"bytes"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/orchestration"
	"github.com/vgxness/vgxness/internal/sdd"
	"strings"
)

const sharedManagerMarker = "artifact: opencode-agent/vgxness-manager; version: 61"

// The original modelBoundAgents/bindManager paths are frozen v60 rendering.
// Current packages consume their native headers, replacing ALL policy bodies.
func buildModelPlanBundle(c sdd.ModelPlanConfig) (modelPlanBundle, error) {
	b, e := buildV60ModelPlanBundle(c)
	if e != nil {
		return b, e
	}
	return sharedManagerBundle(b)
}
func buildModelPlanBundleV2(c sdd.ModelPlanConfigV2) (modelPlanBundle, error) {
	b, e := buildV60ModelPlanBundleV2(c)
	if e != nil {
		return b, e
	}
	return sharedManagerBundle(b)
}
func buildModelPlanBundleV3(c sdd.ModelPlanConfigV3) (modelPlanBundle, error) {
	b, e := buildV60ModelPlanBundleV3(c)
	if e != nil {
		return b, e
	}
	return sharedManagerBundle(b)
}
func isSharedManagerBundle(b modelPlanBundle) bool {
	return bytes.Contains(b.agents[managerAgentName], []byte(sharedManagerMarker))
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
	agents := map[string][]byte{}
	if len(old.agents) != len(c.Roles)+1 {
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
			return modelPlanBundle{}, integration.ErrInvalid
		}
		body := r.Instructions
		if id == "manager" {
			marker = strings.Replace(marker, managerCurrentMarker, sharedManagerMarker, 1)
			body = c.RenderManagerSections()
		}
		agents[name] = []byte(header + "\n" + marker + "\n\n" + body + "\n\n# Native OpenCode adapter\nUse the native model assignment and permission map above. Native task targets are explore, general, vgxness-verifier, vgxness-care-reviewer, vgxness-care-specialist, vgxness-care-challenger and vgxness-sdd-{research,proposal,spec,design,tasks,apply}. Load applicable skills through the native skill tool. Memory and SDD use configured VGXNESS MCP tools; only Manager may mutate lifecycle or perform authorized Git delivery. Missing native transport/authentication is an unavailable dependency. Native capabilities never grant authorization.\nContract identity: " + c.Identity + "; content SHA256: " + orchestration.ManagerContractDigest() + "\n")
	}
	return encodeLike(old, agents)
}
