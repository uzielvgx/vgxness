package codex

// Current native metadata; policy is supplied by the shared Manager contract.
var currentNativeProfiles = []profile{
	{path: "agents/explore.toml", name: "explore", description: "Read-only repository exploration", sandbox: "read-only", mcpTools: []string{"memory_recent", "memory_search", "memory_get"}},
	{path: "agents/general.toml", name: "general", description: "Authorized non-SDD workspace implementation", sandbox: "workspace-write", mcpTools: nil},
	{path: "agents/verifier.toml", name: "verifier", description: "Independent frozen-candidate validation", sandbox: "read-only", mcpTools: nil},
	{path: "agents/care-reviewer.toml", name: "care-reviewer", description: "Primary CARE evidence review", sandbox: "read-only", mcpTools: []string{"memory_recent", "memory_search", "memory_get"}},
	{path: "agents/care-specialist.toml", name: "care-specialist", description: "Bounded CARE domain examination", sandbox: "read-only", mcpTools: []string{"memory_recent", "memory_search", "memory_get"}},
	{path: "agents/care-challenger.toml", name: "care-challenger", description: "Typed CARE challenge", sandbox: "read-only", mcpTools: []string{"memory_recent", "memory_search", "memory_get"}},
}
