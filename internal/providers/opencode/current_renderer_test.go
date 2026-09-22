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
	if !strings.Contains(manager, "Delegate all project code exploration to the explore role: reading files, searching, listing, and read-only diagnosis of repository content, with no simple exception.") {
		t.Error("manager prompt must delegate all project code exploration with no simple exception")
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
	if !strings.Contains(manager, "Delegate all project code exploration to explore through the native task tool") {
		t.Error("adapter must delegate project code exploration explicitly")
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
			"explore.md":                 "9c3921056a34bb833494d739c3d6749f306591eeadd4e834c5579126a8c07695",
			"general.md":                 "c2d79a5cd5d3cffdd3ac581e1e02cbef0e5fb46c020a87066377809ce714e559",
			"vgxness-care-challenger.md": "f8f65e0f0f1dc034c1d315b11a227f86e8bc8684d6b731e0dbc0616821eace99",
			"vgxness-care-reviewer.md":   "881b9949c747289010698d026ebb334cbcece9c5038b7b303a4976df5e43d7e6",
			"vgxness-care-specialist.md": "1fa75e10daa4c7859a394f622a3a27da15cd51d7cd651581c9daf8137d9c4fc6",
			"vgxness-manager.md":         "710e4ca0f2248c998ab687a9696c3e3de6cb99ff4d48678a8ae6e8ead1427bbf",
			"vgxness-verifier.md":        "1de68ce285b71f8180412fc44021a546c5db02a6eeed561872f5e50e7ef656c0",
		},
		"low": {
			"explore.md":                 "e63f4ae8939bb16dc257a7d2cc534febb858835c6af069ce47c68f88eadf8bf1",
			"general.md":                 "3d4166c20a637263cdc2b2c4639c6d85128017d0576b12f25ff910e973f73324",
			"vgxness-care-challenger.md": "f10a9223ba470872ba861b488477056a3a4619d6d55b5a6c00e21f715677b3ce",
			"vgxness-care-reviewer.md":   "d654ed2a69a242aad2ee8dca8005e8cf56e8753a8f215a2d75d02fa0bac799ec",
			"vgxness-care-specialist.md": "703ee286f7c78a7b80739550c10ae2a834cdae544c3bd691398d0c2324938a7a",
			"vgxness-manager.md":         "3ebfa01cea5b67ab01101df37e45b7ffbdd56854f7ed6f075f1e136548b27fce",
			"vgxness-verifier.md":        "3f6ee703e645e8adaeccef19d1ab7a76ac4ba33fb2a3ee2e93c62ee6137f54e0",
		},
		"medium": {
			"explore.md":                 "f0e220837d0c6915cabf40a4ef442a4721bfde494cd4ba3dd0266e1d60e3e31a",
			"general.md":                 "970f6fbd79f69ef7dd502ece81c81a29c1a3ffda906a7377f44f3aa9b47aa03f",
			"vgxness-care-challenger.md": "b5a02d7f586483820e5e37722b217633006bce529afa2bec2e0e28af7dce9968",
			"vgxness-care-reviewer.md":   "5141cfefe9a375d80f09c6a5771d48d18ee8a8fbeef709b67a16c55b507e91a0",
			"vgxness-care-specialist.md": "790680d45e9db0b688758de2d1064f4066ec48a1fda93657901cefae2e99d7ed",
			"vgxness-manager.md":         "fbf11b15b13ce4c8021e27097aef61caa540e0635a7edd9cb506dbd22b2979e3",
			"vgxness-verifier.md":        "2f98e254b546dd15812e2ad33cac738aa1c7c0e0895135bef73affa343b247bf",
		},
		"ultra": {
			"explore.md":                 "898ad1e8b419a3206aafb50a70a0a75bbc235d1b3d4406f3490a28b228bb786e",
			"general.md":                 "c2d79a5cd5d3cffdd3ac581e1e02cbef0e5fb46c020a87066377809ce714e559",
			"vgxness-care-challenger.md": "f8f65e0f0f1dc034c1d315b11a227f86e8bc8684d6b731e0dbc0616821eace99",
			"vgxness-care-reviewer.md":   "881b9949c747289010698d026ebb334cbcece9c5038b7b303a4976df5e43d7e6",
			"vgxness-care-specialist.md": "1fa75e10daa4c7859a394f622a3a27da15cd51d7cd651581c9daf8137d9c4fc6",
			"vgxness-manager.md":         "710e4ca0f2248c998ab687a9696c3e3de6cb99ff4d48678a8ae6e8ead1427bbf",
			"vgxness-verifier.md":        "e4513fd3b4f67ebd3ee14b1174bc428e2f6cf866bf00e04b08114fee01ed36ec",
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
