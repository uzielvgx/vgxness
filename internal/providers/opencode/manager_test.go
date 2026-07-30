package opencode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

const expectedAutonomousStackedPRFrontmatter = `---
name: vgxness-autonomous-stacked-pr
description: Use when autonomously delivering an eligible change as one review-ready pull request or a linear stack with native git and gh.
---`

func TestCurrentBundleUsesCanonicalManagerAndKeepsSkillOutsideModelPlan(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if len(bundle.agents) != 15 || len(bundle.resolved.Roles) != 14 {
		t.Fatalf("current bundle agents=%d roles=%d", len(bundle.agents), len(bundle.resolved.Roles))
	}
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 35",
		"vgxness-autonomous-stacked-pr", "routine autonomous delivery",
		"sole Git and GitHub actor", "delegated implementation worker",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("canonical current manager missing %q", required)
		}
	}
	if _, exists := bundle.agents[autonomousStackedPRSkillName]; exists {
		t.Fatal("managed skill was added to model-bound agents")
	}
	if bytes.Contains(bundle.manifest, []byte(autonomousStackedPRSkillName)) || bytes.Contains(bundle.manifest, []byte("skills/")) {
		t.Fatal("managed skill was added to model-plan manifest")
	}
}

func TestAutonomousStackedPRSkillHasExactIdentityAndNativePolicy(t *testing.T) {
	skill := autonomousStackedPRSkill
	if !strings.HasPrefix(skill, expectedAutonomousStackedPRFrontmatter+"\n\n") {
		t.Fatalf("skill frontmatter differs from accepted identity:\n%s", skill)
	}
	for _, required := range []string{
		"artifact: opencode-skill/vgxness-autonomous-stacked-pr; version: 1",
		"400 effective changed lines", "more than 800 effective changed lines",
		"explicit task override", "durable project memory default",
		"git diff --numstat", "48 characters", "vgxness/<delivery-id>/slice-<ordinal>",
		"use `task` if empty", "estimate guides only the initial plan", "actual measurement supersedes the estimate",
		"actual total is more than 800 effective changed lines",
		"one clean checkout", "linear immediate-parent topology", "no automatic cleanup",
		"read-only resumption", "local-only", "no commit", "no push", "no PR",
		"Initial branch creation uses the announced estimate", "re-plan before staging, commit, push, or PR creation",
		"fresh branch", "normal commit", "first push", "non-draft pull request",
		`git switch -c <head> <verified-start-commit>`,
		`git push --set-upstream <verified-remote> <head>`,
		`gh pr create --head <head> --base <base> --title "<title>" --body "<body>"`,
		`<type>(<scope>): <summary> [slice <ordinal>/<total>]`,
		`<summary> [<ordinal>/<total>]`,
		`Stack: <delivery-id>, Slice: <ordinal>/<total>, Base: <base>, Head: <head>, Depends-On: <previous-PR-URL-or-none>`,
		"safe single argument", "OpenCode command globs do not prove argv semantics",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("stacked-PR skill missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"worktree add", "worktree remove", "--force", "--amend", "gh pr edit", "gh pr merge",
		"persistent delivery state", "opencode.json", "custom Git tool", "custom GitHub tool",
		"before the first Git mutation",
		`Stack: <delivery-id>; Slice: <ordinal>/<total>; Base: <base>; Head: <head>; Depends-On: <previous-PR-URL-or-none>`,
	} {
		if strings.Contains(skill, forbidden) {
			t.Errorf("stacked-PR skill contains unsupported operation %q", forbidden)
		}
	}
}

func TestPrimaryAgentsAllowEveryCapability(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
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
		if strings.Contains(frontmatter, ": deny") || strings.Contains(frontmatter, ": ask") {
			t.Errorf("%s retains a permission that contradicts global allow", name)
		}
	}
}
