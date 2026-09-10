package opencode

import (
	"bytes"

	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
	"github.com/vgxness/vgxness/internal/testutil"
)

const expectedAutonomousStackedPRFrontmatter = `---
name: vgxness-autonomous-stacked-pr
description: Use when autonomously delivering an eligible change as one review-ready pull request or a linear stack with native git and gh.
---`

func TestCurrentBundleUsesCanonicalManagerAndKeepsSkillOutsideModelPlan(t *testing.T) {
	bundle, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if len(bundle.agents) != 7 || len(bundle.resolved.Roles) != 12 {
		t.Fatalf("current bundle agents=%d roles=%d", len(bundle.agents), len(bundle.resolved.Roles))
	}
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 62",
		"# Native OpenCode adapter",
		"Contract identity: vgxness-orchestration/v1",
		"# Delegated role contract",
		"Authority: may write only within its bounded mission.",
		"Authority: read-only.",
		"The structured SDD lifecycle is retired",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("canonical current manager missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"git push <remote> --delete <head>", "automatically load `vgxness-autonomous-stacked-pr`",
		"--force-with-lease=refs/heads/<head>:", "gh pr merge <number> --repo <repository>",
		"expected base-tip OID", "Before each merge, read back",
	} {
		if strings.Contains(manager, forbidden) {
			t.Errorf("canonical current manager retains forbidden cleanup %q", forbidden)
		}
	}
	contract, err := orchestration.LoadManagerContract()
	testutil.NoError(t, err)
	if !strings.Contains(manager, contract.RenderManagerSections()) {
		t.Error("current manager does not render the canonical contract")
	}
	if _, exists := bundle.agents[autonomousStackedPRSkillName]; exists {
		t.Fatal("managed skill was added to model-bound agents")
	}
	if bytes.Contains(bundle.manifest, []byte(autonomousStackedPRSkillName)) || bytes.Contains(bundle.manifest, []byte("skills/")) {
		t.Fatal("managed skill was added to model-plan manifest")
	}
}

func TestManagerRetainsAuthorityWhileBroadProfilesDenyDurableMutations(t *testing.T) {
	bundle, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	for _, name := range []string{managerAgentName, generalAgentName, verifierAgentName} {
		parts := strings.SplitN(string(bundle.agents[name]), "---", 3)
		if len(parts) != 3 {
			t.Fatalf("%s frontmatter is malformed", name)
		}
		frontmatter := parts[1]
		if !strings.Contains(frontmatter, "permission:\n  \"*\": allow\n") {
			t.Errorf("%s does not grant global allow", name)
		}
		if name == managerAgentName && (strings.Contains(frontmatter, ": deny") || strings.Contains(frontmatter, ": ask")) {
			t.Errorf("%s retains a permission that contradicts manager authority", name)
		}
		if name != managerAgentName && !strings.Contains(frontmatter, "vgxness_sdd_record_projection: deny") {
			t.Errorf("%s does not deny durable VGXNESS mutation", name)
		}
	}
}
