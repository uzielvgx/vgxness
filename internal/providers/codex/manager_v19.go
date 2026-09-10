package codex

import (
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/orchestration"
	"github.com/vgxness/vgxness/internal/sdd"
)

// Manager19 is frozen exclusively for complete-package predecessor recognition.
func renderActiveV19(version string, plan sdd.Plan) (Package, error) {
	selected, e := profilesForPlan(plan)
	if e != nil {
		return Package{}, e
	}
	p, e := renderPackage(version, selected, plan, false)
	if e != nil {
		return Package{}, e
	}
	for i := range p.Artifacts {
		if p.Artifacts[i].Path == "AGENTS.md" {
			p.Artifacts[i].Bytes = []byte(activeV19ManagerInstructions())
		}
	}
	p.Artifacts = append(p.Artifacts, lifecycleArtifacts(p.version)...)
	p.current = true
	p.SHA256 = aggregateSHA256(p.Artifacts)
	return p, p.Validate()
}
func previousV20ManagerInstructions() string {
	c := orchestration.PreviousManagerContract()
	return "<!-- managed-by: vgxness; artifact: codex-agent/manager; version: 20; parity: opencode-v61 -->\n\n" + c.RenderManagerSections() + "\n# Native Codex adapter\nUse native Codex delegation with the exact configured agent_type matching the canonical role. Use native skills and the configured VGXNESS MCP memory/SDD tools. Native sandbox and tool permissions remain authoritative; missing tools or authentication are unavailable dependencies. Never treat native capability as user authorization.\nContract identity: " + c.Identity + "; content SHA256: " + orchestration.PreviousManagerContractDigest() + "\n"
}
func profilesFromContract(plan sdd.Plan, c orchestration.ManagerContract) ([]profile, error) {
	selected, e := profilesForPlan(plan)
	if e != nil {
		return nil, e
	}
	filtered := selected[:0]
	for _, item := range selected {
		if _, ok := c.Role(item.name); ok {
			filtered = append(filtered, item)
		}
	}
	selected = filtered
	for i := range selected {
		r, ok := c.Role(selected[i].name)
		if !ok {
			return nil, integration.ErrInvalid
		}
		selected[i].instructions = r.Instructions + "\n\nNative Codex adapter: use only the native tools and sandbox declared in this profile. Never spawn other agents or mutate lifecycle, Git delivery, memory, installation, or external state. Return the supplied mission and candidate bindings unchanged."
		selected[i].sandbox = "read-only"
		if r.WriteAuthority {
			selected[i].sandbox = "workspace-write"
		}
	}
	return selected, nil
}

func sharedProfilesForPlan(plan sdd.Plan) ([]profile, error) {
	c, e := orchestration.LoadManagerContract()
	if e != nil {
		return nil, e
	}
	return profilesFromContract(plan, c)
}
func activeManagerInstructions() string {
	c, e := orchestration.LoadManagerContract()
	if e != nil {
		return ""
	}
	return "<!-- managed-by: vgxness; artifact: codex-agent/manager; version: 21; parity: opencode-v62 -->\n\n" + c.RenderManagerSections() + "\n# Native Codex adapter\nUse native delegation with the exact configured agent_type matching the canonical role. Use native skills and configured VGXNESS MCP memory tools. Native sandbox and tool permissions remain authoritative. Missing tools or authentication are unavailable dependencies; capabilities never grant authorization.\nContract identity: " + c.Identity + "; content SHA256: " + orchestration.ManagerContractDigest() + "\n"
}
func renderActiveV20(version string, plan sdd.Plan) (Package, error) {
	profiles, e := profilesFromContract(plan, orchestration.PreviousManagerContract())
	if e != nil {
		return Package{}, e
	}
	p, e := renderPackage(version, profiles, plan, false)
	if e != nil {
		return p, e
	}
	p.Artifacts[0].Bytes = []byte(previousV20ManagerInstructions())
	p.Artifacts = append(p.Artifacts, lifecycleArtifacts(p.version)...)
	p.current = true
	p.SHA256 = aggregateSHA256(p.Artifacts)
	return p, p.Validate()
}
