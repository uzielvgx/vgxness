package opencode

import "github.com/vgxness/vgxness/internal/modelplan"

var modelAgentInventoryV3 = []modelplan.ManagedAgentIdentity{
	{ArtifactKey: "agents/" + managerAgentName, Role: modelplan.RoleManager, Class: modelplan.ManagedAgentClassCore},
	{ArtifactKey: "agents/" + exploreAgentName, Role: modelplan.RoleResearch, Class: modelplan.ManagedAgentClassCore},
	{ArtifactKey: "agents/" + generalAgentName, Role: modelplan.RoleImplementation, Class: modelplan.ManagedAgentClassCore},
	{ArtifactKey: "agents/" + verifierAgentName, Role: modelplan.RoleVerification, Class: modelplan.ManagedAgentClassCore},
	{ArtifactKey: "agents/vgxness-care-reviewer.md", Role: modelplan.RoleCAREReviewer, Class: modelplan.ManagedAgentClassReview},
	{ArtifactKey: "agents/vgxness-care-specialist.md", Role: modelplan.RoleCARESpecialist, Class: modelplan.ManagedAgentClassReview},
	{ArtifactKey: "agents/vgxness-care-challenger.md", Role: modelplan.RoleCAREChallenger, Class: modelplan.ManagedAgentClassReview},
	{ArtifactKey: "agents/" + sddResearchName, Role: modelplan.RoleResearch, Class: modelplan.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddProposalName, Role: modelplan.RoleProposal, Class: modelplan.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddSpecName, Role: modelplan.RoleSpec, Class: modelplan.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddDesignName, Role: modelplan.RoleDesign, Class: modelplan.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddTasksName, Role: modelplan.RoleTasks, Class: modelplan.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddApplyName, Role: modelplan.RoleApply, Class: modelplan.ManagedAgentClassSDD},
}

// ModelAgentInventoryV3 returns the canonical ordered OpenCode managed-agent
// identities. The returned slice is safe for callers to modify.
func ModelAgentInventoryV3() []modelplan.ManagedAgentIdentity {
	return append([]modelplan.ManagedAgentIdentity(nil), modelAgentInventoryV3[:7]...)
}

func ResolveModelPlanV3(config modelplan.ModelPlanConfigV3) (modelplan.OpenCodePlanV3, error) {
	return modelplan.ResolveOpenCodePlanV3(config, ModelAgentInventoryV3())
}
