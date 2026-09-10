package opencode

import (
	"github.com/vgxness/vgxness/internal/modelplan"
)

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
