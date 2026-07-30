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

func TestCurrentBundleUsesManagerV35AndKeepsSkillOutsideModelPlan(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if len(bundle.agents) != 15 || len(bundle.resolved.Roles) != 14 {
		t.Fatalf("current bundle agents=%d roles=%d", len(bundle.agents), len(bundle.resolved.Roles))
	}
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 35",
		"vgxness-autonomous-stacked-pr", "routine autonomous delivery",
		"sole Git and GitHub actor", "managed general remains the sole workspace writer",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("manager v35 missing %q", required)
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

func TestManagerV35PermissionOrderAllowsOnlyRoutineNativeDelivery(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	parts := strings.SplitN(string(bundle.agents[managerAgentName]), "---", 3)
	if len(parts) != 3 {
		t.Fatal("manager frontmatter is malformed")
	}
	frontmatter := parts[1]
	gitDeny := strings.Index(frontmatter, `"git *": deny`)
	ghDeny := strings.Index(frontmatter, `"gh *": deny`)
	if gitDeny < 0 || ghDeny < 0 {
		t.Fatal("manager lacks broad native Git/GitHub denials")
	}
	readOnlyAllows := []string{
		`"git status --porcelain=v1 --untracked-files=all": allow`,
		`'git remote get-url *': allow`,
		`'git check-ref-format --branch *': allow`,
		`'git show-ref --verify --quiet refs/heads/*': allow`,
		`'git show-ref --verify --quiet refs/remotes/*/*': allow`,
		`'git cat-file -e *^{commit}': allow`,
		`'git merge-base --is-ancestor * *': allow`,
		`'git rev-list --left-right --count *...*': allow`,
		`'git ls-remote --exit-code --heads * *': allow`,
		`'git show --no-patch --format=%H *': allow`,
		`'git diff --name-only --no-ext-diff * -- *': allow`,
		`'git diff --name-status --no-ext-diff --find-renames=100% * -- *': allow`,
		`'git diff --raw --no-ext-diff --find-renames=100% * -- *': allow`,
		`'git diff --check --no-ext-diff * -- *': allow`,
		`'git diff --numstat --no-ext-diff --find-renames=100% * -- *': allow`,
		`'git diff --numstat --no-ext-diff --no-index /dev/null *': allow`,
		`"git diff --cached --name-only --no-ext-diff": allow`,
		`"git diff --cached --check --no-ext-diff": allow`,
		`"git diff --cached --numstat --no-ext-diff --find-renames=100%": allow`,
		`"git diff --cached --no-ext-diff --find-renames=100%": allow`,
		`"gh auth status": allow`,
		`"gh repo view --json nameWithOwner,url": allow`,
		`'gh pr list --head * --state all --json number,url,state,isDraft,headRefName,baseRefName,title,body': allow`,
		`'gh pr view * --json number,url,state,isDraft,headRefName,baseRefName,title,body': allow`,
		`'gh pr checks * --json name,state,bucket,link': allow`,
	}
	for _, rule := range readOnlyAllows {
		index := strings.Index(frontmatter, rule)
		if index < 0 || index < gitDeny || index < ghDeny {
			t.Errorf("read-only preflight/readback allow does not follow broad denials: %s", rule)
		}
	}
	allows := []string{
		`'git switch -c vgxness/*/slice-* *': allow`,
		`'git add -- *': allow`,
		`'git commit -m *': allow`,
		`'git push --set-upstream * vgxness/*/slice-*': allow`,
		`'gh pr create --head vgxness/*/slice-* --base * --title * --body *': allow`,
	}
	for _, rule := range allows {
		index := strings.Index(frontmatter, rule)
		if index < 0 {
			t.Errorf("manager missing routine allow %s", rule)
		} else if index < gitDeny || index < ghDeny {
			t.Errorf("routine allow does not follow broad denials: %s", rule)
		}
	}
	for allow, denial := range map[string]string{
		`'git switch -c vgxness/*/slice-* *': allow`:                                 `'git switch -c vgxness/*/slice-* * *': deny`,
		`'git add -- *': allow`:                                                      `'git add -- * --*': deny`,
		`'git commit -m *': allow`:                                                   `'git commit * --amend*': deny`,
		`'git push --set-upstream * vgxness/*/slice-*': allow`:                       `'git push --set-upstream * vgxness/*/slice-* *': deny`,
		`'gh pr create --head vgxness/*/slice-* --base * --title * --body *': allow`: `'gh pr create --head vgxness/*/slice-* --base * --title * --body * --*': deny`,
	} {
		allowIndex, denialIndex := strings.Index(frontmatter, allow), strings.Index(frontmatter, denial)
		if allowIndex < 0 || denialIndex <= allowIndex {
			t.Errorf("wildcard mutation allow lacks a later suffix denial: %s / %s", allow, denial)
		}
	}
	denials := map[string][]string{
		"switch": {
			`'git switch -c vgxness/*/slice-* * *': deny`, `'git switch -c vgxness/*/slice-* -*': deny`,
			`'git switch * --discard-changes*': deny`, `'git switch * --detach*': deny`, `'git switch * --orphan*': deny`,
			`'git switch * --merge*': deny`, `'git switch * --conflict*': deny`, `'git switch * --track*': deny`,
			`'git switch * --no-track*': deny`, `'git switch * --force-create*': deny`,
		},
		"commit": {
			`'git commit * --all*': deny`, `'git commit * -a*': deny`, `'git commit * --include*': deny`, `'git commit * -i*': deny`,
			`'git commit * --only*': deny`, `'git commit * -o*': deny`, `'git commit * --interactive*': deny`,
			`'git commit * --patch*': deny`, `'git commit * -p*': deny`, `'git commit * --amend*': deny`,
			`'git commit * --no-verify*': deny`, `'git commit * -n*': deny`, `'git commit * --reuse-message*': deny`, `'git commit * -C*': deny`,
			`'git commit * --reedit-message*': deny`, `'git commit * -c*': deny`, `'git commit * --file*': deny`, `'git commit * -F*': deny`,
			`'git commit * --fixup*': deny`, `'git commit * --squash*': deny`,
			`'git commit * --author*': deny`, `'git commit * --date*': deny`, `'git commit * --allow-empty*': deny`,
			`'git commit * --allow-empty-message*': deny`, `'git commit * --pathspec-from-file*': deny`, `'git commit * --pathspec-file-nul*': deny`,
		},
		"push": {
			`'git push * --force*': deny`, `'git push * -f*': deny`, `'git push * --delete*': deny`, `'git push * -d*': deny`, `'git push * --mirror*': deny`,
			`'git push * --all*': deny`, `'git push * --tags*': deny`, `'git push * --follow-tags*': deny`,
			`'git push * --prune*': deny`, `'git push * --no-verify*': deny`, `'git push * --dry-run*': deny`, `'git push * -n*': deny`,
			`'git push * --atomic*': deny`, `'git push * --signed*': deny`, `'git push * --push-option*': deny`, `'git push * -o*': deny`,
			`'git push * --receive-pack*': deny`, `'git push * --exec*': deny`, `'git push * --recurse-submodules*': deny`,
			`'git push --set-upstream -* vgxness/*/slice-*': deny`, `'git push --set-upstream * -*': deny`,
			`'git push --set-upstream * vgxness/*/slice-* *': deny`, `'git push *:*': deny`, `'git push * +*': deny`,
		},
		"pr": {
			`'gh pr create * --draft*': deny`, `'gh pr create * --recover*': deny`, `'gh pr create * --fill*': deny`,
			`'gh pr create * --editor*': deny`, `'gh pr create * --template*': deny`, `'gh pr create * --web*': deny`,
			`'gh pr create * --assignee*': deny`, `'gh pr create * --reviewer*': deny`, `'gh pr create * --label*': deny`,
			`'gh pr create * --milestone*': deny`, `'gh pr create * --project*': deny`, `'gh pr create * --dry-run*': deny`,
			`'gh pr create * --body-file*': deny`, `'gh pr create * --repo*': deny`,
			`'gh pr create * -d*': deny`, `'gh pr create * -f*': deny`, `'gh pr create * -w*': deny`,
			`'gh pr create * -a*': deny`, `'gh pr create * -r*': deny`, `'gh pr create * -l*': deny`,
			`'gh pr create * -m*': deny`, `'gh pr create * -p*': deny`, `'gh pr create * -T*': deny`,
			`'gh pr create --head vgxness/*/slice-* --base * --title * --body * --*': deny`,
			`'gh pr create --head vgxness/*/slice-* --base * --title * --body * -*': deny`,
		},
	}
	mutationIndexes := map[string]int{
		"switch": strings.Index(frontmatter, allows[0]), "commit": strings.Index(frontmatter, allows[2]),
		"push": strings.Index(frontmatter, allows[3]), "pr": strings.Index(frontmatter, allows[4]),
	}
	for group, rules := range denials {
		for _, rule := range rules {
			index := strings.Index(frontmatter, rule)
			if index <= mutationIndexes[group] {
				t.Errorf("%s denial is missing or does not follow its allow: %s", group, rule)
			}
		}
	}
	lastMutation := strings.Index(frontmatter, allows[len(allows)-1])
	for _, rule := range []string{
		`'git -c *': deny`, `'git --config-env *': deny`, `'git config *': deny`, `'gh config *': deny`,
		`'git * --output*': deny`, `'gh * --output*': deny`, `'* ; *': deny`, `'* && *': deny`,
		`'* || *': deny`, `'* | *': deny`, `'* > *': deny`, `'* >> *': deny`, `'* < *': deny`, `'* $(*': deny`,
	} {
		if index := strings.Index(frontmatter, rule); index <= lastMutation {
			t.Errorf("global shell/config/output denial is missing or precedes mutation allows: %s", rule)
		}
	}
	for _, forbidden := range []string{`"git *": allow`, `"gh *": allow`, `"gh pr *": allow`, `'git *': allow`, `'gh *': allow`, `'gh pr *': allow`} {
		if strings.Contains(frontmatter, forbidden) {
			t.Errorf("manager has generic mutation allow %s", forbidden)
		}
	}
	if strings.Contains(frontmatter, `'git worktree`) || strings.Contains(frontmatter, `'gh pr ready *': allow`) || strings.Contains(frontmatter, `'gh pr edit *': allow`) {
		t.Fatal("manager permissions include unsupported delivery mutation")
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ": allow") && strings.Contains(line, "git diff") && strings.Contains(line, "...HEAD") {
			t.Errorf("allowed worktree measurement excludes uncommitted candidate bytes: %s", line)
		}
		if strings.HasSuffix(line, ": allow") && strings.Contains(line, "gh pr create") && strings.Contains(line, "--fill") {
			t.Errorf("PR create allow uses fill instead of explicit metadata: %s", line)
		}
		if strings.HasSuffix(line, ": allow") && (strings.Contains(line, "git push -u ") || strings.Contains(line, "git push --set-upstream origin ")) {
			t.Errorf("push allow uses shorthand or hard-coded remote: %s", line)
		}
	}
}

func TestV34ModelPlanRemainsAnExactHistoricalPredecessor(t *testing.T) {
	config := sdd.DefaultModelPlanConfig()
	config.ActivePlan = sdd.PlanHigh
	config.Provenance = sdd.ModelPlanCLI
	bundle, err := buildV34ModelPlanBundle(config)
	testutil.NoError(t, err)
	if len(bundle.agents) != 15 || len(bundle.resolved.Roles) != 14 {
		t.Fatalf("v34 bundle agents=%d roles=%d", len(bundle.agents), len(bundle.resolved.Roles))
	}
	manager := bundle.agents[managerAgentName]
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 34",
		"model: openai/gpt-5.6-sol", "variant: xhigh",
	} {
		if !bytes.Contains(manager, []byte(required)) {
			t.Errorf("v34 manager missing %q", required)
		}
	}
	if bytes.Contains(manager, []byte("version: 35")) || bytes.Contains(bundle.manifest, []byte(autonomousStackedPRSkillName)) {
		t.Fatal("historical v34 bundle contains v35 bytes")
	}
	_, parsed, err := parseInstalledModelPlanManifest(bundle.manifest)
	testutil.NoError(t, err)
	if !bytes.Equal(parsed.manifest, bundle.manifest) || parsed.config != config {
		t.Fatal("installed v34 identity or model configuration did not round trip")
	}
}
