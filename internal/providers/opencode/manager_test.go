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
		"artifact: opencode-agent/vgxness-manager; version: 39",
		"automatically load `stacked-pr`", "routine autonomous delivery",
		"Before delegating any workspace write", "clean checkout/repository identity/intended-path/sizing/slice/fresh-branch gate",
		"`IMPLEMENTED`, `VERIFIED`, `DELIVERED`, `MERGED`, and `INSTALLED`", "never present an earlier state as a later one",
		"sole Git and GitHub actor", "delegated implementation worker",
		"current-task merge authorization", "original inspected base branch",
		"gh pr merge <number> --repo <repository> --merge --match-head-commit <expected-head-oid>",
		"verified GitHub `owner/repo` identity", "validated full commit OID",
		"expected base-tip OID", "before checks and again immediately before merge",
		"fast-forward", "no cleanup",
		"creating an additional checkout or worktree", "Switching the existing checkout back to the original base",
		"Existing remote branches and PRs are read-only resumption",
		"The only exception is the skill's bounded interrupted-local-slice recovery gate",
		"dirty worktrees outside that exact bounded interrupted-local-slice recovery gate",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("canonical current manager missing %q", required)
		}
	}
	for _, forbidden := range []string{"git push <remote> --delete <head>", "automatically load `vgxness-autonomous-stacked-pr`"} {
		if strings.Contains(manager, forbidden) {
			t.Errorf("canonical current manager retains forbidden cleanup %q", forbidden)
		}
	}
	if _, exists := bundle.agents[autonomousStackedPRSkillName]; exists {
		t.Fatal("managed skill was added to model-bound agents")
	}
	if bytes.Contains(bundle.manifest, []byte(autonomousStackedPRSkillName)) || bytes.Contains(bundle.manifest, []byte("skills/")) {
		t.Fatal("managed skill was added to model-plan manifest")
	}
}

func TestManagerPromptDelegatesRepositoryWorkWithoutDuplicatingChildExploration(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	prompt := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"Work directly only for conversation and non-repository explanations, decisions, orchestration, lifecycle/Git authority, and compact synthesis.",
		"Default repository questions and diagnosis-only work to Explore.",
		"Use managed general as the delegated implementation worker for clear authorized implementation; it owns necessary diagnosis, edits, and developmental checks, and the manager does not launch Explore first by default.",
		"Reserve Explore -> General for genuine ambiguity or diagnosis requiring separation.",
		"Avoid repeating child source exploration. Direct source inspection is exceptional for contradictory or missing evidence, candidate-identity mismatch, or severe findings; exact diff, path, status, and command evidence inspection remains mandatory.",
		"Route structural CodeGraph work to the delegated worker and use one bounded codegraph_explore query before broad reads or search where applicable.",
		"If CodeGraph is unavailable, missing, or stale, the delegated worker continues with native reads and search without blocking; it reads any specifically reported stale files directly.",
		"conclusions, decisive references or changed paths, exact commands and results, assumptions, and blockers",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("manager prompt is missing delegation-first contract %q", required)
		}
	}
	for _, superseded := range []string{
		"Work directly when the task fits the manager context, delegate when separation protects focus or independent evidence, validate candidate identity, and report outcomes.",
		"Work directly for explanation, bounded repository inspection, planning, decisions, and implementation that fit the manager context.",
		"Use Explore only for diagnosis-only work, structural discovery, or real ambiguity that needs bounded read-only investigation.",
	} {
		if strings.Contains(prompt, superseded) {
			t.Errorf("manager prompt retains superseded direct repository-work contract %q", superseded)
		}
	}
}

func TestAutonomousStackedPRSkillHasExactIdentityAndNativePolicy(t *testing.T) {
	skill := autonomousStackedPRSkill
	if !strings.HasPrefix(skill, expectedAutonomousStackedPRFrontmatter+"\n\n") {
		t.Fatalf("skill frontmatter differs from accepted identity:\n%s", skill)
	}
	for _, required := range []string{
		"artifact: opencode-skill/vgxness-autonomous-stacked-pr; version: 3",
		"Before any source write", "clean porcelain including untracked", "base/upstream/remote/repository/ref proof",
		"initial estimate and slice plan", "fresh branch before source writes",
		"After each bounded write transaction", "actual numeric size", "stop and replan before further writes",
		"explicit current-task user reauthorization", "deterministic expected branch name",
		"no upstream, live remote head, or PR", "no staged changes", "complete worktree-inclusive digest including untracked",
		"repeated focused checks, independent verification and selected review", "NEW PR",
		"Existing remote branches and PRs are read-only resumption", "never receive retroactive merge or cleanup authority",
		"400 effective changed lines", "more than 800 effective changed lines",
		"explicit task override", "durable project memory default",
		"git diff --numstat", "48 characters", "vgxness/<delivery-id>/slice-<ordinal>",
		"use `task` if empty", "estimate guides only the initial plan", "actual measurement supersedes the estimate",
		"actual total is more than 800 effective changed lines",
		"one clean checkout", "linear immediate-parent topology", "same original inspected base branch",
		"merge commits preserve predecessor commits", "narrow after earlier slices land",
		"no merge", "no cleanup", "current-task merge authorization",
		"read-only resumption", "local-only", "no commit", "no push", "no PR",
		"Initial branch creation uses the announced estimate", "re-plan before staging, commit, push, or PR creation",
		"fresh branch", "normal commit", "first push", "non-draft pull request",
		`git switch -c <head> <verified-start-commit>`,
		`git push --set-upstream --force-with-lease=refs/heads/<head>: <verified-remote> refs/heads/<head>:refs/heads/<head>`,
		`gh pr create --head <head> --base <base> --title "<title>" --body "<body>"`,
		`gh pr merge <number> --repo <repository> --merge --match-head-commit <expected-head-oid>`,
		"verified GitHub `owner/repo` identity", "validated full commit OID",
		"expected base-tip OID", "before checks and again immediately before merge",
		`gh pr checks <number> --repo <repository> --watch --fail-fast`,
		"positive decimal PR number", "expected head OID", "successful required checks",
		"predecessor merged state", "immediately before merge", "expected head/base/merge commit identity",
		"fast-forward from the verified remote-tracking base", "git branch -d", "remote delivery branches are left intact",
		"creating an additional checkout or worktree", "Switching the existing checkout back to the original base",
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
		"worktree add", "worktree remove", "--amend", "gh pr edit", "git push <remote> --delete <head>",
		"persistent delivery state", "opencode.json", "custom Git tool", "custom GitHub tool",
		"before the first Git mutation",
		`Stack: <delivery-id>; Slice: <ordinal>/<total>; Base: <base>; Head: <head>; Depends-On: <previous-PR-URL-or-none>`,
	} {
		if strings.Contains(skill, forbidden) {
			t.Errorf("stacked-PR skill contains unsupported operation %q", forbidden)
		}
	}
	const createOnlyLease = "git push --set-upstream --force-with-lease=refs/heads/<head>: <verified-remote> refs/heads/<head>:refs/heads/<head>"
	if !strings.Contains(skill, createOnlyLease) {
		t.Errorf("stacked-PR skill missing exact create-only lease push %q", createOnlyLease)
	}
	for remaining := skill; ; {
		index := strings.Index(remaining, "--force")
		if index < 0 {
			break
		}
		remaining = remaining[index:]
		if !strings.HasPrefix(remaining, "--force-with-lease=refs/heads/<head>:") {
			t.Error("stacked-PR skill contains a generic or ambiguous force form")
			break
		}
		remaining = remaining[len("--force"):]
	}
	for _, forbidden := range []string{
		"git push --set-upstream <verified-remote> <head>",
		"--force-with-lease=refs/heads/<head>:<expected-oid>",
	} {
		if strings.Contains(skill, forbidden) {
			t.Errorf("stacked-PR skill contains unsafe first-publication behavior %q", forbidden)
		}
	}
}

func TestAutonomousStackedPRSkillOrdersGatesAndConstrainsRecovery(t *testing.T) {
	skill := autonomousStackedPRSkill
	preWrite := "Before any source write, complete the clean checkout/repository identity/intended-path/sizing/slice/fresh-branch gate"
	branch := "Only then create that fresh branch before source writes."
	postImplementation := "After implementation, and before staging, commit, push, PR, or merge delivery mutations, require candidate identity, successful developmental checks, independent verification, and review outcome."
	for _, required := range []string{preWrite, branch, postImplementation} {
		if !strings.Contains(skill, required) {
			t.Errorf("stacked-PR skill missing gate phrase %q", required)
		}
	}
	if strings.Index(skill, preWrite) > strings.Index(skill, branch) || strings.Index(skill, branch) > strings.Index(skill, postImplementation) {
		t.Error("stacked-PR skill does not order pre-write, branch creation, and post-implementation gates")
	}
	for _, required := range []string{
		"current HEAD and the deterministic local `refs/heads/<head>` must each equal the exact verified base or immediate-predecessor full OID",
		"before any recovery write or delivery mutation",
		"Existing remote branches and PRs remain read-only",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("stacked-PR skill missing recovery constraint %q", required)
		}
	}
}

func TestManagerMapsDeliveryMilestonesAndFirstPublicationPolicy(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	manager := string(bundle.agents[managerAgentName])
	const createOnlyLease = "git push --set-upstream --force-with-lease=refs/heads/<head>: <verified-remote> refs/heads/<head>:refs/heads/<head>"
	for _, required := range []string{
		createOnlyLease,
		"all first remote publication, including clean and recovery paths",
		"must fail if the remote ref exists and must never overwrite or update an existing ref",
		"before any recovery write or delivery mutation, it requires current HEAD and the deterministic local branch ref to equal the exact verified base or immediate-predecessor full OID",
		"IMPLEMENTED: intended workspace changes complete and developmental checks observed; not independently verified.",
		"VERIFIED: exact frozen candidate passed independent verifier and required review.",
		"DELIVERED: exact commit was published and a new current-task PR was created and read back.",
		"MERGED: that PR was verified merged and base containment/readback succeeded.",
		"INSTALLED: merged version was installed and installation/handshake readback succeeded.",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("canonical current manager missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"normal one-line commit, first push with `--set-upstream`",
	} {
		if strings.Contains(manager, forbidden) {
			t.Errorf("canonical current manager retains unsafe publication behavior %q", forbidden)
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
