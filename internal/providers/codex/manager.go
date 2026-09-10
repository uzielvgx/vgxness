package codex

import (
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

func profilesFromContract(plan modelplan.Plan, c orchestration.ManagerContract) ([]profile, error) {
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

func sharedProfilesForPlan(plan modelplan.Plan) ([]profile, error) {
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
