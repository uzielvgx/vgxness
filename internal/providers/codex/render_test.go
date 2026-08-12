package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
)

func TestRenderPlanUsesSharedModelMatrix(t *testing.T) {
	roles := map[string]sdd.Role{
		"agents/explore.toml":      sdd.RoleResearch,
		"agents/general.toml":      sdd.RoleImplementation,
		"agents/verifier.toml":     sdd.RoleVerification,
		"agents/risk.toml":         sdd.RoleRisk,
		"agents/readability.toml":  sdd.RoleReadability,
		"agents/reliability.toml":  sdd.RoleReliability,
		"agents/resilience.toml":   sdd.RoleResilience,
		"agents/refuter.toml":      sdd.RoleRefuter,
		"agents/sdd-research.toml": sdd.RoleResearch,
		"agents/sdd-proposal.toml": sdd.RoleProposal,
		"agents/sdd-spec.toml":     sdd.RoleSpec,
		"agents/sdd-design.toml":   sdd.RoleDesign,
		"agents/sdd-tasks.toml":    sdd.RoleTasks,
		"agents/sdd-apply.toml":    sdd.RoleApply,
	}
	for _, plan := range []sdd.Plan{sdd.PlanLow, sdd.PlanMedium, sdd.PlanHigh, sdd.PlanUltra} {
		pkg, err := RenderPlan("v1.2.3", plan)
		if err != nil {
			t.Fatalf("RenderPlan(%s): %v", plan, err)
		}
		config := sdd.DefaultModelPlanConfig()
		config.ActivePlan = plan
		resolved, err := sdd.ResolveOpenCodePlan(config)
		if err != nil {
			t.Fatal(err)
		}
		for path, role := range roles {
			assignment := resolved.Roles[role]
			content := string(artifact(t, pkg, path).Bytes)
			model := strings.TrimPrefix(assignment.Model, "openai/")
			if !strings.Contains(content, `model = "`+model+`"`) || !strings.Contains(content, `model_reasoning_effort = "`+string(assignment.Variant)+`"`) {
				t.Fatalf("%s %s does not match %+v: %s", plan, path, assignment, content)
			}
		}
	}
}

func TestLegacyStaticPackageHasFrozenAggregateSHA256(t *testing.T) {
	pkg, err := renderLegacy("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	const want = "00849e83cfaf8533b704a064a97660b30b2652e30f8ea937c72377a91a6ffb07"
	if pkg.SHA256 != want {
		t.Fatalf("legacy aggregate SHA-256 = %s, want %s", pkg.SHA256, want)
	}
}

func TestRenderProducesNativeCodexProjection(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{
		"AGENTS.md",
		"agents/explore.toml",
		"agents/general.toml",
		"agents/readability.toml",
		"agents/refuter.toml",
		"agents/reliability.toml",
		"agents/resilience.toml",
		"agents/risk.toml",
		"agents/sdd-apply.toml",
		"agents/sdd-design.toml",
		"agents/sdd-proposal.toml",
		"agents/sdd-research.toml",
		"agents/sdd-spec.toml",
		"agents/sdd-tasks.toml",
		"agents/verifier.toml",
	}
	if got := artifactPaths(pkg.Artifacts); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("paths = %v, want %v", got, wantPaths)
	}
	if strings.Contains(string(artifact(t, pkg, "AGENTS.md").Bytes), "OpenCode") {
		t.Fatal("manager instructions name an unavailable OpenCode tool")
	}
	for _, item := range pkg.Artifacts {
		if strings.Contains(item.Path, ".codex-plugin") || item.Path == ".mcp.json" {
			t.Fatalf("unexpected plugin artifact %q", item.Path)
		}
	}
}

func TestManagerInstructionsCoverOpenCodeV46EquivalentBoundaries(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	content := string(artifact(t, pkg, "AGENTS.md").Bytes)
	for _, required := range []string{
		"routing", "launch log", "interaction mode", "Child Return Envelope v1",
		"Evidence Receipt v1", "CodeGraph", "memory_recent", "memory_search", "memory_save",
		"freeze", "INCONCLUSIVE", "reviewers", "SDD", "IMPLEMENTED", "VERIFIED",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("manager instructions lack %q", required)
		}
	}
	for _, unavailable := range []string{"todowrite", "question tool", "automatically injected"} {
		if strings.Contains(content, unavailable) {
			t.Errorf("manager instructions claim unavailable Codex behavior %q", unavailable)
		}
	}
	for _, required := range []string{
		"load stacked-pr and complete its required pre-write gate before any delegated workspace write or branch creation",
		"Eligibility and narrowing restrictions come from stacked-pr",
		"<!-- managed-by: vgxness; artifact: global-skill/sdd-lifecycle; version: 1 -->",
		"Block if provenance, source, scope, marker, or loading cannot be verified, or if a same-name/project-local skill collides",
		"never fall back inline or accept a local skill with the same name",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("manager instructions lack exact safeguard %q", required)
		}
	}
}

func TestRenderProfilesUseNativeFieldsAndRoleBoundaries(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	protectedTools := []string{"memory_save", "memory_forget", "sdd_create", "sdd_set_interaction_mode", "sdd_transition", "sdd_save_revision", "sdd_accept_revision", "sdd_record_projection"}
	for _, item := range pkg.Artifacts[1:] {
		content := string(item.Bytes)
		for _, field := range []string{"name = ", "description = ", "developer_instructions = ", "model = ", "model_reasoning_effort = ", "sandbox_mode = "} {
			if !strings.Contains(content, field) {
				t.Errorf("%s lacks %q", item.Path, field)
			}
		}
		if !strings.Contains(content, "[mcp_servers.vgxness]") || !strings.Contains(content, "enabled_tools = [") || strings.Contains(content, "mcp_servers = []") {
			t.Errorf("%s lacks a Codex MCP server table with an enabled-tools list", item.Path)
		}
		if !strings.Contains(content, "[mcp_servers.vgxness]\ncommand = \"vgxness\"\nargs = [\"mcp\", \"--full\"]\nenabled_tools = [") {
			t.Errorf("%s lacks a self-contained full VGXNESS MCP table", item.Path)
		}
		if strings.Contains(content, "OpenCode") || strings.Contains(content, "todowrite") || strings.Contains(content, "codegraph_codegraph_explore") {
			t.Errorf("%s names an unavailable OpenCode-only tool", item.Path)
		}
		for _, tool := range protectedTools {
			if strings.Contains(content, tool) {
				t.Errorf("%s exposes protected tool %q", item.Path, tool)
			}
		}
		if strings.Contains(content, `sandbox_mode = "read-only"`) {
			for _, tool := range protectedTools {
				if strings.Contains(content, `enabled_tools = [`) && strings.Contains(content, `"`+tool+`"`) {
					t.Errorf("read-only %s allowlists protected tool %q", item.Path, tool)
				}
			}
		}
	}
	for _, path := range []string{"agents/explore.toml", "agents/verifier.toml", "agents/risk.toml", "agents/readability.toml", "agents/reliability.toml", "agents/resilience.toml", "agents/refuter.toml", "agents/sdd-research.toml", "agents/sdd-proposal.toml", "agents/sdd-spec.toml", "agents/sdd-design.toml", "agents/sdd-tasks.toml"} {
		content := string(artifact(t, pkg, path).Bytes)
		if !strings.Contains(content, "sandbox_mode = \"read-only\"") {
			t.Errorf("%s is not read-only", path)
		}
	}
	for _, path := range []string{"agents/explore.toml", "agents/risk.toml", "agents/readability.toml", "agents/reliability.toml", "agents/resilience.toml", "agents/refuter.toml"} {
		if content := string(artifact(t, pkg, path).Bytes); !strings.Contains(content, `enabled_tools = ["memory_recent", "memory_search", "memory_get"]`) {
			t.Errorf("%s lacks the exact protected memory-read allowlist", path)
		}
	}
	sddTools := `enabled_tools = ["memory_recent", "memory_search", "memory_get", "sdd_list", "sdd_get", "sdd_get_revision", "sdd_list_revisions", "sdd_render_projection", "sdd_compare_projection", "sdd_projection_status"]`
	for _, path := range []string{"agents/general.toml", "agents/verifier.toml"} {
		if content := string(artifact(t, pkg, path).Bytes); !strings.Contains(content, "enabled_tools = []") {
			t.Errorf("%s must not expose MCP tools", path)
		}
	}
	for _, path := range []string{"agents/sdd-research.toml", "agents/sdd-proposal.toml", "agents/sdd-spec.toml", "agents/sdd-design.toml", "agents/sdd-tasks.toml", "agents/sdd-apply.toml"} {
		if content := string(artifact(t, pkg, path).Bytes); !strings.Contains(content, sddTools) {
			t.Errorf("%s lacks the exact protected SDD-read allowlist", path)
		}
	}
	if content := string(artifact(t, pkg, "agents/general.toml").Bytes); !strings.Contains(content, "sandbox_mode = \"workspace-write\"") || !strings.Contains(content, "must not own the SDD lifecycle") || !strings.Contains(content, "model = \"gpt-5.6-terra\"") || !strings.Contains(content, "model_reasoning_effort = \"medium\"") {
		t.Error("general profile does not retain its workspace-only boundary")
	}
	if content := string(artifact(t, pkg, "agents/explore.toml").Bytes); !strings.Contains(content, "model = \"gpt-5.6-luna\"") || !strings.Contains(content, "model_reasoning_effort = \"medium\"") {
		t.Error("explore profile does not use the supported read-heavy model")
	}
	for path, model := range map[string]string{"agents/verifier.toml": "gpt-5.6-luna", "agents/sdd-spec.toml": "gpt-5.6-terra", "agents/sdd-design.toml": "gpt-5.6-sol", "agents/sdd-apply.toml": "gpt-5.6-terra"} {
		if content := string(artifact(t, pkg, path).Bytes); !strings.Contains(content, `model = "`+model+`"`) {
			t.Errorf("%s does not use the medium-plan model %s", path, model)
		}
	}
	if content := string(artifact(t, pkg, "AGENTS.md").Bytes); !strings.Contains(content, "artifact: codex-agent/manager; version: 3") || !strings.Contains(content, "custom agents") || !strings.Contains(content, "sole SDD lifecycle") || !strings.Contains(content, "~/.agents/skills") || !strings.Contains(content, "managed native global catalog") || !strings.Contains(content, "third-party and unknown skills are untrusted") || !strings.Contains(content, "stacked-pr") || !strings.Contains(content, "sdd-lifecycle") || len(content) > 32<<10 {
		t.Error("manager instructions do not use native delegation and lifecycle authority")
	}
}

func TestPackageValidateRejectsCallerMutationsAndStaleDigests(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := pkg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	pkg.Artifacts[0].Bytes[0] = 'X'
	if err := pkg.Validate(); err == nil {
		t.Fatal("Validate accepted caller mutation with a stale digest")
	}
	pkg.SHA256 = aggregate(pkg.Artifacts)
	if err := pkg.Validate(); err == nil {
		t.Fatal("Validate accepted caller mutation after digest recomputation")
	}
}

func TestRenderIsDeterministicAndCopiesArtifacts(t *testing.T) {
	first, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("renders differ: %#v != %#v", first, second)
	}
	if got, want := first.SHA256, aggregate(first.Artifacts); got != want {
		t.Fatalf("aggregate SHA-256 = %q, want %q", got, want)
	}
	first.Artifacts[0].Bytes[0] = 'X'
	third, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Artifacts[0].Bytes, third.Artifacts[0].Bytes) {
		t.Fatal("mutating returned artifact changed a later render")
	}
}

func TestRenderRejectsInvalidVersions(t *testing.T) {
	for _, version := range []string{"", "dev", "1.2.3", "v1.2", "v1.2.3/../../x", "v01.2.3", "v1.2.3-", "v1.2.3-alpha..1", "v1.2.3-01", "v1.2.3+build.1"} {
		t.Run(version, func(t *testing.T) {
			if _, err := Render(version); err == nil {
				t.Fatalf("Render(%q) succeeded", version)
			}
		})
	}
}

func TestValidateRelativePathRejectsTraversal(t *testing.T) {
	for _, path := range []string{"", "/absolute", "../escape", "nested/../../escape", `nested\\escape`, "."} {
		t.Run(path, func(t *testing.T) {
			if err := validateRelativePath(path); err == nil {
				t.Fatalf("validateRelativePath(%q) succeeded", path)
			}
		})
	}
}

func artifactPaths(artifacts []Artifact) []string {
	paths := make([]string, len(artifacts))
	for i, artifact := range artifacts {
		paths[i] = artifact.Path
	}
	return paths
}

func artifact(t *testing.T, pkg Package, path string) Artifact {
	t.Helper()
	for _, item := range pkg.Artifacts {
		if item.Path == path {
			return item
		}
	}
	t.Fatalf("artifact %q not found", path)
	return Artifact{}
}

func aggregate(artifacts []Artifact) string {
	hash := sha256.New()
	for _, artifact := range artifacts {
		hash.Write([]byte(artifact.Path))
		hash.Write([]byte{0})
		hash.Write(artifact.Bytes)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
