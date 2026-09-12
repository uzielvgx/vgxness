package opencode

import (
	"github.com/vgxness/vgxness/internal/agentmodels"
	"github.com/vgxness/vgxness/internal/modelplan"
	"strings"
)

// ExplicitModels translates user choices without a capability matrix. Unknown
// models retain unknown availability; setup does not authenticate providers.
func ExplicitModels(c agentmodels.Config) (map[string]modelplan.ManagedAgentModelConfig, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	result := map[string]modelplan.ManagedAgentModelConfig{}
	for i, identity := range ModelAgentInventoryV3() {
		a := c.Assignments[agentmodels.Roles[i]]
		effort, variant := modelplan.Effort(a.Effort), modelplan.OpenCodeVariant(a.Effort)
		switch a.Effort {
		case "off":
			effort, variant = modelplan.EffortLow, ""
		case "minimal":
			effort = modelplan.EffortLow
		case "xhigh":
			effort = modelplan.EffortUltra
		}
		result[identity.ArtifactKey] = modelplan.ManagedAgentModelConfig{Provider: strings.SplitN(a.Model, "/", 2)[0], Reference: a.Model, RequestedEffort: effort, Variant: variant, VariantSpecified: true, Source: modelplan.ModelSlotCustom, Availability: modelplan.ModelSlotUnknown}
	}
	return result, nil
}
