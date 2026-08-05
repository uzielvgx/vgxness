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
		"artifact: opencode-agent/vgxness-manager; version: 40",
		"automatically load `stacked-pr`", "detailed operational delivery policy lives only in that loaded skill",
		"Before delegating any workspace write", "pre-write gate required by that skill",
		"`IMPLEMENTED`, `VERIFIED`, `DELIVERED`, `MERGED`, and `INSTALLED`", "never present an earlier state as a later one",
		"sole Git and GitHub actor", "delegated implementation worker",
		"Stop on ambiguity or a failed skill gate", "Do not commit or push without an explicit current-task request",
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
	if _, exists := bundle.agents[autonomousStackedPRSkillName]; exists {
		t.Fatal("managed skill was added to model-bound agents")
	}
	if bytes.Contains(bundle.manifest, []byte(autonomousStackedPRSkillName)) || bytes.Contains(bundle.manifest, []byte("skills/")) {
		t.Fatal("managed skill was added to model-plan manifest")
	}
}

func TestManagerPromptKeepsDeliveryAuthorityWithinStaticBudget(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	prompt := string(bundle.agents[managerAgentName])

	// The extracted Manager baseline is 16,370 bytes and 117 lines after model binding.
	// Leave 130 bytes and three lines of headroom for concise contract edits, while
	// preventing a return of the removed always-loaded delivery procedure.
	const maxManagerPromptBytes = 16_500
	const maxManagerPromptLines = 120
	if bytes := len(prompt); bytes > maxManagerPromptBytes {
		t.Errorf("manager prompt bytes=%d, budget=%d", bytes, maxManagerPromptBytes)
	}
	if lines := strings.Count(prompt, "\n"); lines > maxManagerPromptLines {
		t.Errorf("manager prompt lines=%d, budget=%d", lines, maxManagerPromptLines)
	}
	for _, required := range []string{
		"sole orchestration and SDD lifecycle authority",
		"automatically load `stacked-pr`",
		"pre-write gate required by that skill",
		"detailed operational delivery policy lives only in that loaded skill",
		"The manager is the sole Git and GitHub actor.",
		"IMPLEMENTED: intended workspace changes complete and developmental checks observed; not independently verified.",
		"Stop on ambiguity or a failed skill gate; do not invent a fallback delivery procedure.",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("manager prompt is missing delivery invariant %q", required)
		}
	}
}

func TestManagerV39PredecessorIsExactBaseTemplateBoundToCurrentRole(t *testing.T) {
	if digest := artifactSHA256([]byte(previousManagerPromptV39)); digest != "0e99ea9e8ecb8e51d80663543956e47cbc041177c6065535a39bf2cbf9767552" {
		t.Fatalf("manager v39 template digest=%s", digest)
	}
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessor, err := previousManagerModelPlanBundle(current)
	testutil.NoError(t, err)
	if !bytes.Contains(predecessor.agents[managerAgentName], []byte("version: 39")) || bytes.Equal(predecessor.agents[managerAgentName], current.agents[managerAgentName]) {
		t.Fatal("manager predecessor was not exactly bound from the v39 template")
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

func TestRetiredAutonomousStackedPRSkillKeepsHistoricalIdentity(t *testing.T) {
	skill := autonomousStackedPRSkill
	if !strings.HasPrefix(skill, expectedAutonomousStackedPRFrontmatter+"\n\n") {
		t.Fatalf("skill frontmatter differs from accepted identity:\n%s", skill)
	}
	if !strings.Contains(skill, "artifact: opencode-skill/vgxness-autonomous-stacked-pr; version: 3") {
		t.Fatal("retired skill lost its historical marker")
	}
}

func TestManagerMapsDeliveryMilestones(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
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
		"normal one-line commit, first push with `--set-upstream`", "--force-with-lease=refs/heads/<head>:",
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
