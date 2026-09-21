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
			"explore.md":                 "6c816fd7886dec56228cc57a2b98c5f07674449b76ec15fde693c649a2f4e010",
			"general.md":                 "7d5448de9b484c60066a92ed39784d42338cd49ec8021fd3c650b42e3fc7181a",
			"vgxness-care-challenger.md": "fbe13ae57baa4e7cdc6cd16ab7c645a38738b90db70749e784893c15708d2867",
			"vgxness-care-reviewer.md":   "99f28567776d2751c5f021abd0382cda3bdacd739ca5f34ba2a00c386410f317",
			"vgxness-care-specialist.md": "1d288746192c667bb407218d011b3b20e32dc3072e4f296ad43d80a628fcc4a1",
			"vgxness-manager.md":         "571acdfa5a4cc1bfd62d8a0590c027a097370696b3696fa2680bde203a5b12ba",
			"vgxness-verifier.md":        "2796ea7e3ebf7968fe77e11a0c621574ae69ad1c3342fe620674ab47ae3f42f5",
		},
		"low": {
			"explore.md":                 "2eb69018dac00689a96a11740d1fc6ebab108bcce122c1290ed365f2b04e7c23",
			"general.md":                 "4ce0cb5c295c48766a408194a65c0a333140aaeb7a7ace8d628618c0011f2ba4",
			"vgxness-care-challenger.md": "e2b88eaf711cc3d13917ef2a8aff17f9189203ad213505781c211b1e1ea58eea",
			"vgxness-care-reviewer.md":   "04038f9d80964258ccb4df88742a73917911aa3b21ec820d0803103f8eace6ee",
			"vgxness-care-specialist.md": "31c6037686b2125f65a4a345d9228812db5c845d06e330f5f362c4ffd2cb67be",
			"vgxness-manager.md":         "bb9194d43aa4eedf11155a646b2370c4b538615b8914bf23e46d5c16acf8c3d9",
			"vgxness-verifier.md":        "cd9a043d15c5b0f2907ac23241b9fc6d1a8af1394840d0a44b6f5c6129af007d",
		},
		"medium": {
			"explore.md":                 "f8d8d455c26ba3bd9b2ba6381dee99193e828d2aed7172ffaa21da993ec16a39",
			"general.md":                 "b8e419c83ec79891c861ccbc5132cc25d9d0dbdac0d29e3060c46699b8d96a84",
			"vgxness-care-challenger.md": "8296aab2565259a15fe00ebe39dcfd662e0f9f3d296866c4f64adf2f73797200",
			"vgxness-care-reviewer.md":   "4e3328ee179975862978944778a345b077bcb9c6cba04d0ab2b324f318098f85",
			"vgxness-care-specialist.md": "0a6b3ca6be5cc0cb8125864b7026bc8147703ab00253327a11a70f36818f3931",
			"vgxness-manager.md":         "c7dcd0957f58ea8e72ff5364550083f3386173589cad8b100e454d291698468d",
			"vgxness-verifier.md":        "35e817af7636dcf8e94035819dfe7690211a5d0a9a2450de9aef8f29e3dc3b95",
		},
		"ultra": {
			"explore.md":                 "d78802fe37975d72aafd5b1f126152521880c13557bd643f171162f6ecf7523b",
			"general.md":                 "7d5448de9b484c60066a92ed39784d42338cd49ec8021fd3c650b42e3fc7181a",
			"vgxness-care-challenger.md": "fbe13ae57baa4e7cdc6cd16ab7c645a38738b90db70749e784893c15708d2867",
			"vgxness-care-reviewer.md":   "99f28567776d2751c5f021abd0382cda3bdacd739ca5f34ba2a00c386410f317",
			"vgxness-care-specialist.md": "1d288746192c667bb407218d011b3b20e32dc3072e4f296ad43d80a628fcc4a1",
			"vgxness-manager.md":         "571acdfa5a4cc1bfd62d8a0590c027a097370696b3696fa2680bde203a5b12ba",
			"vgxness-verifier.md":        "d79ab412f80850f81aeda2da345b86300bc5befe73693aaf0068e23be37d31da",
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
