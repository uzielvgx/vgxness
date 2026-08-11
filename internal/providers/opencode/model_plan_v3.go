package opencode

import "github.com/vgxness/vgxness/internal/sdd"

var modelAgentInventoryV3 = []sdd.ManagedAgentIdentity{
	{ArtifactKey: "agents/" + managerAgentName, Role: sdd.RoleManager, Class: sdd.ManagedAgentClassCore},
	{ArtifactKey: "agents/" + exploreAgentName, Role: sdd.RoleResearch, Class: sdd.ManagedAgentClassCore},
	{ArtifactKey: "agents/" + generalAgentName, Role: sdd.RoleImplementation, Class: sdd.ManagedAgentClassCore},
	{ArtifactKey: "agents/" + verifierAgentName, Role: sdd.RoleVerification, Class: sdd.ManagedAgentClassCore},
	{ArtifactKey: "agents/" + reviewRiskName, Role: sdd.RoleRisk, Class: sdd.ManagedAgentClassReview},
	{ArtifactKey: "agents/" + reviewReadabilityName, Role: sdd.RoleReadability, Class: sdd.ManagedAgentClassReview},
	{ArtifactKey: "agents/" + reviewReliabilityName, Role: sdd.RoleReliability, Class: sdd.ManagedAgentClassReview},
	{ArtifactKey: "agents/" + reviewResilienceName, Role: sdd.RoleResilience, Class: sdd.ManagedAgentClassReview},
	{ArtifactKey: "agents/" + reviewRefuterName, Role: sdd.RoleRefuter, Class: sdd.ManagedAgentClassReview},
	{ArtifactKey: "agents/" + sddResearchName, Role: sdd.RoleResearch, Class: sdd.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddProposalName, Role: sdd.RoleProposal, Class: sdd.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddSpecName, Role: sdd.RoleSpec, Class: sdd.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddDesignName, Role: sdd.RoleDesign, Class: sdd.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddTasksName, Role: sdd.RoleTasks, Class: sdd.ManagedAgentClassSDD},
	{ArtifactKey: "agents/" + sddApplyName, Role: sdd.RoleApply, Class: sdd.ManagedAgentClassSDD},
}

// ModelAgentInventoryV3 returns the canonical ordered OpenCode managed-agent
// identities. The returned slice is safe for callers to modify.
func ModelAgentInventoryV3() []sdd.ManagedAgentIdentity {
	return append([]sdd.ManagedAgentIdentity(nil), modelAgentInventoryV3...)
}

func ResolveModelPlanV3(config sdd.ModelPlanConfigV3) (sdd.OpenCodePlanV3, error) {
	return sdd.ResolveOpenCodePlanV3(config, modelAgentInventoryV3)
}
