package opencode

import (
	"bytes"
	"os"
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
		"artifact: opencode-agent/vgxness-manager; version: 43",
		"Load `sdd-lifecycle` before creating an accepted SDD change.",
		"If `sdd-lifecycle` is unavailable or fails to load, block the SDD request.",
		"managed global portable catalog",
		"<!-- managed-by: vgxness; artifact: global-skill/sdd-lifecycle; version: 1 -->",
		"same-name/project-local skill collides",
		"Never fall back inline or accept a local skill with the same name.",
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
	t.Logf("manager prompt bytes=%d newlines=%d", len(prompt), strings.Count(prompt, "\n"))

	// V43 preserves the fixed generated-prompt budget.
	const maxManagerPromptBytes = 15_000
	const maxManagerPromptLines = 105
	if bytes := len(prompt); bytes > maxManagerPromptBytes {
		t.Errorf("manager prompt bytes=%d, budget=%d", bytes, maxManagerPromptBytes)
	}
	if lines := strings.Count(prompt, "\n"); lines > maxManagerPromptLines {
		t.Errorf("manager prompt lines=%d, budget=%d", lines, maxManagerPromptLines)
	}
	for _, required := range []string{
		"Apply validation and lifecycle ceremony proportionally to risk and scope; small authorized repository changes remain delegated to managed general and do not imply SDD or delivery.",
		"sole orchestration and SDD lifecycle authority",
		"Briefly disclose material assumptions.",
		"A task override applies only to the current request and never changes the project default.",
		"Treat an answer as a session decision and do not ask it again.",
		"A question never grants permission or overrides a denial.",
		"When a consequential ambiguity remains unresolved, choose a safe reversible default when available or remain blocked; never continue through unsafe, irreversible, unauthorized, or consequential ambiguity.",
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
	v41, err := previousManagerModelPlanBundleV41(current)
	testutil.NoError(t, err)
	v40, err := previousManagerModelPlanBundleV40(v41)
	testutil.NoError(t, err)
	predecessor, err := previousManagerModelPlanBundleV39(v40)
	testutil.NoError(t, err)
	if !bytes.Contains(predecessor.agents[managerAgentName], []byte("version: 39")) || bytes.Equal(predecessor.agents[managerAgentName], current.agents[managerAgentName]) {
		t.Fatal("manager predecessor was not exactly bound from the v39 template")
	}
}

func TestManagerV40PredecessorIsExactBaseTemplateBoundToCurrentRole(t *testing.T) {
	if digest := artifactSHA256([]byte(previousManagerPromptV40)); digest != "e13863fd3abe4354d2319d1ee2ae0105c7bc1844842a0765b697dd11f93a3cf2" {
		t.Fatalf("manager v40 template digest=%s", digest)
	}
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	v41, err := previousManagerModelPlanBundleV41(current)
	testutil.NoError(t, err)
	predecessor, err := previousManagerModelPlanBundleV40(v41)
	testutil.NoError(t, err)
	if !bytes.Contains(predecessor.agents[managerAgentName], []byte("version: 40")) || bytes.Equal(predecessor.agents[managerAgentName], current.agents[managerAgentName]) {
		t.Fatal("manager predecessor was not exactly bound from the v40 template")
	}
}

func TestManagerV41PredecessorIsExactBaseTemplateBoundToCurrentRole(t *testing.T) {
	if digest := artifactSHA256([]byte(previousManagerPromptV41)); digest != "28568b2ec532c4eded63fe62531f3601ef80f2e83077cc912e3017bcf3311358" {
		t.Fatalf("manager v41 template digest=%s", digest)
	}
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessor, err := previousManagerModelPlanBundleV41(current)
	testutil.NoError(t, err)
	if !bytes.Contains(predecessor.agents[managerAgentName], []byte("version: 41")) || bytes.Equal(predecessor.agents[managerAgentName], current.agents[managerAgentName]) {
		t.Fatal("manager predecessor was not exactly bound from the v41 template")
	}
}

func TestManagerV42PredecessorIsExactBaseTemplateBoundToCurrentRole(t *testing.T) {
	if digest := artifactSHA256([]byte(previousManagerPromptV42)); digest != "24ca61ef6f7642660a8ff32325c8d32df4b962a0b47df830d08a239e15f54bd3" {
		t.Fatalf("manager v42 template digest=%s", digest)
	}
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessor, err := previousManagerModelPlanBundleV42(current)
	testutil.NoError(t, err)
	if !bytes.Contains(predecessor.agents[managerAgentName], []byte("version: 42")) || bytes.Equal(predecessor.agents[managerAgentName], current.agents[managerAgentName]) {
		t.Fatal("manager predecessor was not exactly bound from the v42 template")
	}
	recognized, err := modelPlanBundleForManifest(predecessor.manifest, sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if !bytes.Equal(recognized.agents[managerAgentName], predecessor.agents[managerAgentName]) {
		t.Fatal("manager v42 manifest was not recognized as a predecessor")
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

func TestManagerPredecessorTemplatesRetainAcceptedDigests(t *testing.T) {
	for path, expected := range map[string]string{
		"templates/manager.v42.md": "24ca61ef6f7642660a8ff32325c8d32df4b962a0b47df830d08a239e15f54bd3",
		"templates/manager.v39.md": "0e99ea9e8ecb8e51d80663543956e47cbc041177c6065535a39bf2cbf9767552",
		"templates/manager.v40.md": "e13863fd3abe4354d2319d1ee2ae0105c7bc1844842a0765b697dd11f93a3cf2",
		"templates/manager.v41.md": "28568b2ec532c4eded63fe62531f3601ef80f2e83077cc912e3017bcf3311358",
	} {
		content, err := os.ReadFile(path)
		testutil.NoError(t, err)
		if actual := artifactSHA256(content); actual != expected {
			t.Errorf("%s digest=%s", path, actual)
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
