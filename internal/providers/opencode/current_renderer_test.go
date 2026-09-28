package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/modelplan"
)

func TestNativeHeadersRequireCurrentCodegraphAndVerifierDenials(t *testing.T) {
	bundle, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"vgxness-care-challenger.md", "vgxness-care-reviewer.md", "vgxness-care-specialist.md"} {
		text := string(bundle.agents[name])
		if !strings.Contains(text, "codegraph_codegraph_explore: allow") {
			t.Errorf("%s lacks the current Codegraph exploration override", name)
		}
		if strings.Contains(text, "\n  codegraph_explore: allow") {
			t.Errorf("%s retains the stale Codegraph tool name", name)
		}
	}
	verifier := string(bundle.agents["vgxness-verifier.md"])
	if !strings.Contains(verifier, "edit: deny") || !strings.Contains(verifier, "task: deny") {
		t.Error("verifier header must explicitly deny edit and task")
	}
	manager := string(bundle.agents["vgxness-manager.md"])
	for _, want := range []string{
		"Classify the request as a direct question, a routine operational job, a bounded project read, implementation, or a higher-risk change before acting.",
		"perform the necessary bounded inspection and execution yourself",
		"use available native inspection tools",
		"the explore role stays read-only for a bounded mission carrying an explicit child nonce and testable criteria",
		"never infer production or reset authorization from a development-scoped request",
		"Inspect the exact evidence returned by explore, workers, or your own bounded direct inspection before deciding",
	} {
		if !strings.Contains(manager, want) {
			t.Errorf("manager prompt lacks adaptive direct-work policy %q", want)
		}
	}
	for _, forbidden := range []string{"Delegate all project code exploration", "with no simple exception", "does not browse project itself"} {
		if strings.Contains(manager, forbidden) {
			t.Errorf("manager prompt retains the removed broad exploration block %q", forbidden)
		}
	}
	if !strings.Contains(manager, "local Git queries (status, diff, log, refs, tracking, conflicts)") {
		t.Error("manager prompt must retain the bounded operational-inspection authority")
	}
	if strings.Contains(manager, "including status checks, reviews, and read-only diagnosis; no simple exception") {
		t.Error("manager prompt retains the over-broad all-inspection delegation")
	}
	if strings.Contains(manager, "without a formal plan, delegation, RED, or CARE ritual") {
		t.Error("manager prompt retains the removed broad no-delegation exception")
	}
	if !strings.Contains(manager, "not a hard sandbox") {
		t.Error("native adapter must state that shell restrictions are not a hard sandbox")
	}
	if !strings.Contains(manager, "skills registry search --workspace") || !strings.Contains(manager, "mcp.vgxness.command[0]") {
		t.Error("manager prompt lacks the authorized absolute-launcher registry query guidance")
	}
	if !strings.Contains(manager, "never search PATH") || !strings.Contains(manager, "report the dependency unavailable instead of browsing the repository") {
		t.Error("manager registry guidance must forbid PATH guessing and silent repo browsing")
	}
	if !strings.Contains(manager, "Delegate broad or parallel project code exploration to explore through the native task tool when the shared contract calls for it") {
		t.Error("adapter must delegate broad project code exploration explicitly")
	}
	if !strings.Contains(manager, "may itself perform the bounded, necessary inspection and execution the shared contract permits") {
		t.Error("adapter must permit the bounded direct-work exception")
	}
	if strings.Contains(manager, "Delegate all project exploration to explore through the native task tool") {
		t.Error("adapter retains the old blanket all-exploration delegation")
	}
	if !strings.Contains(manager, "narrow operational-inspection exception defined by the shared contract") {
		t.Error("adapter must reference the Manager operational-inspection exception")
	}
	if !strings.Contains(manager, "only the read-only explore and CARE roles declare an explicit external_directory grant") {
		t.Error("adapter must qualify external grants to the read-only explore and CARE roles")
	}
	if !strings.Contains(manager, "For explore and CARE, the full home, provider configuration that may contain secrets, and other external paths remain outside that grant") {
		t.Error("adapter must scope the outside-grant restriction to explore and CARE")
	}
	if strings.Contains(manager, "No role is granted the full home") {
		t.Error("adapter retains the incorrect all-role denial claim")
	}
	if !strings.Contains(manager, "Do not infer a denial for another role from these read-only-role restrictions") {
		t.Error("adapter must warn against inferring denials for other roles")
	}
	if !strings.Contains(manager, "keep a durable, in-repository Markdown plan under docs/implementations/") {
		t.Error("manager prompt lacks the persistent Markdown plan policy")
	}
	if !strings.Contains(manager, "native todowrite tool as a session view") {
		t.Error("OpenCode adapter lacks the todowrite session-view projection")
	}
	if !strings.Contains(manager, "keep at most one active plan per session (zero when there is no work)") {
		t.Error("manager prompt lacks the at-most-one-active-plan policy with the zero-when-idle qualifier")
	}
	if strings.Contains(manager, "exactly one active plan per session") {
		t.Error("manager prompt retains the ambiguous always-one plan summary")
	}
	if !strings.Contains(manager, "never promise a runtime synchronizer or atomic cross-tool state") {
		t.Error("manager prompt lacks the honest-degradation planning limit")
	}
	if !strings.Contains(manager, "Ask consequential blocking decisions before closing the plan or implementing any part that depends on them") {
		t.Error("manager prompt lacks the specific blocking-decision requirement")
	}
	if strings.Contains(manager, "do not block authorized work on a pending decision") || strings.Contains(manager, "record an explicit assumption and continue") {
		t.Error("manager prompt retains the unsafe pending-decision assumption clause")
	}
	for _, schema := range []string{
		"# Implementation plan records",
		"Task states are pending, in_progress, blocked, done, cancelled",
		"Plan states are pending, active, paused, closed, cancelled",
		"exactly one plan is active while executing, at most one is active otherwise",
		"progress.md records decisions, blockers, and the single next action, and must not duplicate the task table",
		"validation.md records the exact candidate",
		"sufficient on their own: a new project needs only the workspace, not any repository documentation",
	} {
		if !strings.Contains(manager, schema) {
			t.Errorf("manager prompt lacks embedded plan-record schema %q", schema)
		}
	}
}

func TestReadOnlyRolesScopeExternalSkillRoots(t *testing.T) {
	bundle, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	const grant = "  external_directory:\n    \"~/.agents/skills/**\": allow"
	readOnly := []string{"explore.md", "vgxness-care-challenger.md", "vgxness-care-reviewer.md", "vgxness-care-specialist.md"}
	for _, name := range readOnly {
		text := string(bundle.agents[name])
		if !strings.Contains(text, grant) {
			t.Errorf("%s lacks the scoped skill-root external_directory grant", name)
		}
		for _, forbidden := range []string{`"~"`, `"~/**"`, `"~/.config`, `"~/.ssh`, "$HOME"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s grants an over-broad external path %q", name, forbidden)
			}
		}
	}
	if !strings.Contains(string(bundle.agents["explore.md"]), "webfetch: allow") {
		t.Error("explore must allow public documentation webfetch")
	}
	for _, name := range []string{"vgxness-care-challenger.md", "vgxness-care-reviewer.md", "vgxness-care-specialist.md"} {
		if strings.Contains(string(bundle.agents[name]), "webfetch: allow") {
			t.Errorf("%s must not be granted webfetch", name)
		}
	}
	for _, name := range []string{"general.md", "vgxness-verifier.md", "vgxness-manager.md"} {
		frontmatter := strings.SplitN(string(bundle.agents[name]), "---", 3)[1]
		if strings.Contains(frontmatter, "external_directory") {
			t.Errorf("%s frontmatter must not carry the read-only external grant", name)
		}
	}
}

func TestContractPermissionParsingSupportsNestedAndLastMatch(t *testing.T) {
	content := []byte("---\ndescription: x\npermission:\n  \"*\": deny\n  read: allow\n  external_directory:\n    \"~/.agents/skills/**\": allow\n    \"~/.agents/skills/private/**\": ask\n---\n")
	rules, err := parseContractPermissions(content)
	if err != nil {
		t.Fatal(err)
	}
	if got := effectiveManagedPermission(rules, "read"); got != "allow" {
		t.Fatalf("read=%q, want allow (last match beats wildcard)", got)
	}
	if got := effectiveManagedPermission(rules, "bash"); got != "deny" {
		t.Fatalf("bash=%q, want deny from wildcard", got)
	}
	if got := effectiveExternalPermission(rules, "~/.agents/skills/a/SKILL.md"); got != "allow" {
		t.Fatalf("skill root=%q, want allow", got)
	}
	if got := effectiveExternalPermission(rules, "~/.agents/skills/private/x"); got != "ask" {
		t.Fatalf("nested last match=%q, want ask", got)
	}
	if got := effectiveExternalPermission(rules, "~/.config/opencode/opencode.json"); got != "" {
		t.Fatalf("unlisted external path=%q, want unknown", got)
	}
	if _, err := parseContractPermissions([]byte("---\npermission:\n  read: maybe\n---\n")); err == nil {
		t.Fatal("invalid permission value was accepted")
	}
}

func TestManagedFrontmatterPermissionsModelScopedExternalAccess(t *testing.T) {
	bundle, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"explore.md", "vgxness-care-reviewer.md"} {
		rules := managedPermissions(t, bundle.agents[name])
		if got := effectiveManagedPermission(rules, "bash"); got != "deny" {
			t.Errorf("%s bash=%q, want deny", name, got)
		}
		if got := effectiveExternalPermission(rules, "~/.agents/skills/x/SKILL.md"); got != "allow" {
			t.Errorf("%s skill root=%q, want allow", name, got)
		}
		if got := effectiveExternalPermission(rules, "~/.config/opencode/opencode.json"); got != "" {
			t.Errorf("%s provider config=%q, want unknown (denied by default)", name, got)
		}
	}
}

func TestCurrentRendererPreservesNativeArtifacts(t *testing.T) {
	expected := map[modelplan.Plan]map[string]string{
		"high": {
			"explore.md":                 "124c4e3e545dd7974a3c168113de7a7158c2ac6b7c0f351abe1bb82e22ceafc4",
			"general.md":                 "bc194f45dcb0c6c51c3c5cc19cd1a2ba8c998c3f1d7c819d3c637fd0bbaa9ffd",
			"vgxness-care-challenger.md": "b86347546a7f6e69c264a7d1dac1509e8bff6d609fb0a07f4fba1bc5053d1d7c",
			"vgxness-care-reviewer.md":   "200325a4ffa83a6eb1f69be910bf729336941b8c996c84c9189be63cfb1419c3",
			"vgxness-care-specialist.md": "5cedeb98375500faa8e932bd47a3315dc2f7e7712d0a9cd5cf4fb4c67003a7cb",
			"vgxness-manager.md":         "37f3711f275d23f672db1e2b8a07eff4f42fdb9e00409d566d798b00b319b112",
			"vgxness-verifier.md":        "4732f2e2b26c414028155b1c9cfabdbc1962691160a153617d8e03be2c165a7e",
		},
		"low": {
			"explore.md":                 "b82c4a20deb379d1b812a358f7b50414fcad0ce927515bb082172747e6301b98",
			"general.md":                 "0f141c8e8309c06c9a195fb95dc193d1f8e55cdf0f5703ba6067698568209895",
			"vgxness-care-challenger.md": "880f3b5abd7f999bb96bdd88a3d32a10b5eabf75c778f03c32db60892c461b3b",
			"vgxness-care-reviewer.md":   "47763e3caad34dedb487c6c58e4f320b520ec7163fc18811e69cfeba65fc5aad",
			"vgxness-care-specialist.md": "623873f6c266436667dc1bc56a8ae03d985b9be6e2d17833c204daed90bd5870",
			"vgxness-manager.md":         "fa03bd41d0c24568ffd58654d061fe5780471eb2bac0010faca29763a1294927",
			"vgxness-verifier.md":        "5ac1b82c721f49124253935e5f18240f4b4f0fe2fb58b3c94668c8aefd24886d",
		},
		"medium": {
			"explore.md":                 "ca8f5404645068eb54537e9ef1eef509639ba5bee23cdc4bba6df7f112e6dbfc",
			"general.md":                 "eab8a3aea73cf0597e06cc6a920264caa53e722f2a2eab0c7bd330af4d25e46f",
			"vgxness-care-challenger.md": "34c121617d905c9504692d13aa2a4f2ef58b07af403347e568aaa9ca2e0055b0",
			"vgxness-care-reviewer.md":   "f4e745dfbb6432db9558106620130d24726dab94a8496c6733bba0804207c04a",
			"vgxness-care-specialist.md": "a399d7f6953ddf80b28c7ad3f3c69b93359206073f5aeff13552a001a1be245f",
			"vgxness-manager.md":         "20831d1b34d41306575940cf305ca3e32544c32ce2fd12ace36c5ec1b22309c3",
			"vgxness-verifier.md":        "f956a3cc971348dd90ac13c1ed7499474b9c629673f40c68d9bf9f25198b4654",
		},
		"ultra": {
			"explore.md":                 "a130842bf86ba87455d95a1d1e27d755dbd3e28c0e19fc5e9e923386783707f1",
			"general.md":                 "bc194f45dcb0c6c51c3c5cc19cd1a2ba8c998c3f1d7c819d3c637fd0bbaa9ffd",
			"vgxness-care-challenger.md": "b86347546a7f6e69c264a7d1dac1509e8bff6d609fb0a07f4fba1bc5053d1d7c",
			"vgxness-care-reviewer.md":   "200325a4ffa83a6eb1f69be910bf729336941b8c996c84c9189be63cfb1419c3",
			"vgxness-care-specialist.md": "5cedeb98375500faa8e932bd47a3315dc2f7e7712d0a9cd5cf4fb4c67003a7cb",
			"vgxness-manager.md":         "37f3711f275d23f672db1e2b8a07eff4f42fdb9e00409d566d798b00b319b112",
			"vgxness-verifier.md":        "493dd3f46c13365efe1458f3197efa7d145cd3e11140da558b3686d421e60019",
		},
	}
	for plan, want := range expected {
		c := modelplan.DefaultModelPlanConfig()
		c.ActivePlan = plan
		b, err := buildModelPlanBundle(c)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.agents) != 7 {
			t.Fatal("unexpected agents")
		}
		for name, digest := range want {
			sum := sha256.Sum256(b.agents[name])
			got := hex.EncodeToString(sum[:])
			if got != digest {
				t.Errorf("%s/%s changed native artifact: want %s, got %s", plan, name, digest, got)
			}
		}
	}
}
