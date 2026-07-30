package opencode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestV34BundleUsesManagerV34WithoutDeliveryAgent(t *testing.T) {
	bundle, err := buildV34ModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if len(bundle.agents) != 15 || len(bundle.resolved.Roles) != 14 {
		t.Fatalf("current bundle agents=%d roles=%d", len(bundle.agents), len(bundle.resolved.Roles))
	}
	if _, exists := bundle.agents["vgxness-delivery.md"]; exists {
		t.Fatal("current bundle retains delivery agent")
	}
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 34", "edit: deny",
		"explicit current-task user request", "Authorization to implement is not authorization to deliver",
		"user-visible permission prompt is the final gate", "Preserve unrelated changes", "stage only intended paths",
		"inspect the cached diff", "normal commit", "requested branch", "Never proactively run destructive Git",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("manager v34 missing %q", required)
		}
	}
	for _, forbidden := range []string{"vgxness-delivery", "verifier v3", "staged candidate digest", "hash-object"} {
		if strings.Contains(manager, forbidden) {
			t.Errorf("manager retains cancelled delivery workflow %q", forbidden)
		}
	}
}

func TestManagerV34UsesGenericGitAskBeforeExactReadOnlyAllows(t *testing.T) {
	bundle, err := buildV34ModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	prompt := string(bundle.agents[managerAgentName])
	parts := strings.SplitN(prompt, "---", 3)
	if len(parts) != 3 {
		t.Fatal("manager frontmatter is malformed")
	}
	frontmatter := parts[1]
	askIndex := strings.Index(frontmatter, `"git *": ask`)
	if askIndex < 0 {
		t.Fatal("manager is missing generic Git ask policy")
	}
	for _, rule := range []string{
		`"git status": allow`, `"git status --short": allow`, `"git status --porcelain": allow`,
		`"git diff": allow`, `"git diff --stat": allow`, `"git diff --name-only": allow`, `"git diff --check": allow`, `"git diff --cached": allow`,
		`"git branch --show-current": allow`, `"git rev-parse HEAD": allow`, `"git log --oneline -10": allow`, `"git show --stat": allow`,
	} {
		index := strings.Index(frontmatter, rule)
		if index < 0 {
			t.Errorf("manager missing exact read-only allow %s", rule)
		} else if index < askIndex {
			t.Errorf("read-only allow %s does not override generic ask", rule)
		}
	}
	for _, forbidden := range []string{`"git add`, `"git commit`, `"git push`} {
		if strings.Contains(frontmatter, forbidden) {
			t.Errorf("manager retains command-specific mutation rule %q", forbidden)
		}
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"git `) && strings.HasSuffix(line, `": allow`) && (strings.Contains(line, "*") || strings.Contains(line, "--output") || strings.ContainsAny(line, ">|")) {
			t.Errorf("manager has unsafe read-only Git allow %q", line)
		}
	}
}

func TestV33HighPlanMatchesInstalledBundleExactly(t *testing.T) {
	config := sdd.DefaultModelPlanConfig()
	config.ActivePlan = sdd.PlanHigh
	config.Provenance = sdd.ModelPlanCLI
	bundle, err := buildV33ModelPlanBundle(config)
	testutil.NoError(t, err)
	if len(bundle.agents) != 15 || len(bundle.resolved.Roles) != 14 {
		t.Fatalf("v33 bundle agents=%d roles=%d", len(bundle.agents), len(bundle.resolved.Roles))
	}
	if got := artifactSHA256(bundle.manifest); got != "c02c80820e1bd7f4f0b2af893b644e455a2fb5ef8faeb2d11c35aa0ca7b5a041" {
		t.Fatalf("high-plan v33 manifest sha256=%s", got)
	}
	if got := artifactSHA256(bundle.agents[managerAgentName]); got != "6a764fec774e9baa2a8001330593c6164f55bd62365c4a1caeecbb3c604193b5" {
		t.Fatalf("high-plan v33 manager sha256=%s", got)
	}
	for name, marker := range map[string]string{
		managerAgentName:  "artifact: opencode-agent/vgxness-manager; version: 33",
		generalAgentName:  "artifact: opencode-agent/general; version: 2",
		verifierAgentName: "artifact: opencode-agent/vgxness-verifier; version: 2",
		sddApplyName:      "artifact: opencode-agent/vgxness-sdd-apply; version: 3",
	} {
		if !bytes.Contains(bundle.agents[name], []byte(marker)) {
			t.Errorf("v33 agent %s missing %q", name, marker)
		}
	}
	_, parsed, err := parseInstalledModelPlanManifest(bundle.manifest)
	testutil.NoError(t, err)
	if !bytes.Equal(parsed.manifest, bundle.manifest) {
		t.Fatal("installed high-plan v33 identity did not round trip")
	}
}
