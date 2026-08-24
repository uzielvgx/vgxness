package opencode

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/orchestration"
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
		"artifact: opencode-agent/vgxness-manager; version: 55",
		currentManagerCandidateCapsuleContract,
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
		"one exact Review Binding: candidateDigest, exact changedPaths, diffScope, and acceptanceCriteria",
		"Copy that exact Review Binding unchanged to verifier, every reviewer, refuter, and scoped validation",
		"A correction changes the candidate digest and invalidates all prior validation and review evidence",
		"Scoped validation receives correctionDelta only with the frozenLedger and the new exact Review Binding",
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
	if got := strings.Count(manager, "A correction changes the candidate digest and invalidates all prior validation and review evidence."); got != 1 {
		t.Errorf("correction invalidation rule count=%d, want 1", got)
	}
	if strings.Contains(manager, "A correction creates a new candidate digest and invalidates all prior validation and review evidence.") {
		t.Error("manager retains duplicate correction invalidation rule")
	}
	if _, exists := bundle.agents[autonomousStackedPRSkillName]; exists {
		t.Fatal("managed skill was added to model-bound agents")
	}
	if bytes.Contains(bundle.manifest, []byte(autonomousStackedPRSkillName)) || bytes.Contains(bundle.manifest, []byte("skills/")) {
		t.Fatal("managed skill was added to model-plan manifest")
	}
}

func TestVersionEvolutionImmediatePredecessorIsExactV54Package(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessor, err := immediatePredecessor(current)
	testutil.NoError(t, err)
	for name, markers := range map[string][2]string{managerAgentName: {"artifact: opencode-agent/vgxness-manager; version: 55", "artifact: opencode-agent/vgxness-manager; version: 54"}} {
		currentMarker, previousMarker := markers[0], markers[1]
		currentAgent, predecessorAgent := current.agents[name], predecessor.agents[name]
		if bytes.Count(currentAgent, []byte(currentMarker)) != 1 || bytes.Count(currentAgent, []byte(previousMarker)) != 0 {
			t.Fatalf("current %s marker cardinality is invalid", name)
		}
		if bytes.Count(predecessorAgent, []byte(previousMarker)) != 1 || bytes.Count(predecessorAgent, []byte(currentMarker)) != 0 {
			t.Fatalf("predecessor %s marker cardinality is invalid", name)
		}
		want := bytes.Replace(currentAgent, []byte(currentMarker), []byte(previousMarker), 1)
		want = bytes.Replace(want, []byte("\n\n"+currentManagerCandidateCapsuleContract), nil, 1)
		if !bytes.Equal(predecessorAgent, want) {
			t.Fatalf("%s predecessor changed bytes beyond its marker", name)
		}
	}
	for name, agent := range current.agents {
		if name != managerAgentName && !bytes.Equal(agent, predecessor.agents[name]) {
			t.Fatalf("unaffected agent %s changed in immediate predecessor", name)
		}
	}
}

func TestV54PreservesV53ThenV51ReliabilityPredecessorsAndSkillReceipts(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if !bytes.Contains(current.agents[reviewReliabilityName], []byte("artifact: opencode-agent/vgxness-review-reliability; version: 5")) {
		t.Fatal("current reliability identity is not v5")
	}
	for _, required := range []string{"Before candidate inspection", "receipt naming it and status loaded|unavailable", "missing/unavailable is INCONCLUSIVE"} {
		if !bytes.Contains(current.agents[reviewReliabilityName], []byte(required)) {
			t.Errorf("reliability contract missing %q", required)
		}
	}
	predecessor, err := previousV53ModelPlanBundle(current)
	testutil.NoError(t, err)
	if !bytes.Contains(predecessor.agents[managerAgentName], []byte("artifact: opencode-agent/vgxness-manager; version: 53")) || !bytes.Contains(predecessor.agents[reviewReliabilityName], []byte("artifact: opencode-agent/vgxness-review-reliability; version: 5")) {
		t.Fatal("exact v53/reliability-v5 deeper predecessor is not preserved")
	}
}

func TestV54UsesCompactProtocolAndReconstructsCompleteV45Bundle(t *testing.T) {

	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	for _, required := range []string{"version: 55", "Mission Instance v1", "Candidate Capsule v1", "Child Return Envelope v1", "Evidence Receipt v1", "8 KiB", "16 KiB", "verificationState"} {
		if !bytes.Contains(current.agents[managerAgentName], []byte(required)) {
			t.Errorf("manager missing compact protocol %q", required)
		}
	}
	v45, err := previousV45ModelPlanBundle(current)
	testutil.NoError(t, err)
	if got, minimum := len(predecessorBundlesMust(t, current)), 60; got < minimum {
		t.Fatalf("predecessor bundles=%d, want at least %d", got, minimum)
	}
	if !bytes.Contains(v45.agents[managerAgentName], []byte("artifact: opencode-agent/vgxness-manager; version: 45")) ||
		!bytes.Contains(v45.agents[generalAgentName], []byte("artifact: opencode-agent/general; version: 4")) {
		t.Fatal("v45 bundle did not use exact predecessor profiles")
	}
	v44, err := previousV44ModelPlanBundle(v45)
	testutil.NoError(t, err)
	v43, err := previousV43ModelPlanBundle(v44)
	testutil.NoError(t, err)
	for _, name := range compactProtocolAgentNames {
		if !bytes.Contains(v43.agents[name], []byte("version: 2")) || bytes.Equal(v43.agents[name], current.agents[name]) {
			t.Errorf("%s was not reconstructed from the exact v2 profile", name)
		}
	}
	for name, marker := range map[string]string{
		generalAgentName: "artifact: opencode-agent/general; version: 2", verifierAgentName: "artifact: opencode-agent/vgxness-verifier; version: 2",
		reviewRiskName: "artifact: opencode-agent/vgxness-review-risk; version: 2", reviewReadabilityName: "artifact: opencode-agent/vgxness-review-readability; version: 2",
		reviewReliabilityName: "artifact: opencode-agent/vgxness-review-reliability; version: 2", reviewResilienceName: "artifact: opencode-agent/vgxness-review-resilience; version: 2", reviewRefuterName: "artifact: opencode-agent/vgxness-review-refuter; version: 2",
	} {
		if !bytes.Contains(v43.agents[name], []byte(marker)) {
			t.Errorf("%s missing exact v2 marker %q", name, marker)
		}
	}
	if _, err := modelPlanBundleForManifest(v43.manifest, current.config); err != nil {
		t.Fatalf("v43 manifest not recognized: %v", err)
	}
}

func TestCurrentAndPredecessorProfileSnapshotsHaveFixedDigestsAndExactBoundShape(t *testing.T) {
	if got := artifactSHA256([]byte(previousManagerPromptV45)); got != "533e71583ebcb656d500bc3c944e6e077848c778977ec8ef8c7cdc9429e62b97" {
		t.Fatalf("manager v45 snapshot digest=%s", got)
	}
	if got := artifactSHA256([]byte(previousGeneralPromptV4)); got != "18d62d947e0755d63d3fa69bed7e3c4ac37ac84d249dfcbfe78c1917a4f7a91f" {
		t.Fatalf("general v4 snapshot digest=%s", got)
	}
	if got := artifactSHA256([]byte(previousManagerPromptV44)); got != "f97a49b2107ee8c257295e42c09540704074f032d8549ffca1604896ccac3965" {
		t.Fatalf("manager v44 snapshot digest=%s", got)
	}
	if got := artifactSHA256([]byte(previousGeneralPromptV3)); got != "616b7e5ff36848f505b87b94cc07caa0634c711db13914c30dc195c62bd467f1" {
		t.Fatalf("general v3 snapshot digest=%s", got)
	}
	if got := artifactSHA256([]byte(previousManagerPromptV43)); got != "edc4a9a651cc9d91db04d7036407e1c18d1c15eeccbbd548b6c5412d5f23d7eb" {
		t.Fatalf("manager v43 snapshot digest=%s", got)
	}
	for _, tc := range []struct {
		name, digest string
		base         string
		role         sdd.Role
	}{
		{"general", "17575c70cb52c372cd4e4bb469ee2e20f8b94bc32a3091df8120900f736e7a41", previousGeneralPromptV2, sdd.RoleImplementation},
		{"verifier", "c42af55db5a0da34d31f02367e303f14d77ea6a6cd36b56bf19559e293136b7f", previousVerifierPromptV2, sdd.RoleVerification},
		{"risk", "3499480ccd1c3d22e6aeae180898175bfedd50cd10c14703300b0648318e8ef7", previousReviewRiskPromptV2, sdd.RoleRisk},
		{"readability", "81cd2ed7d7487f74e561c43e14c02033a380553c5a05cb28f65c2b2d304a18bf", previousReviewReadabilityPromptV2, sdd.RoleReadability},
		{"reliability", "69d3293a7eaeccc02d27b38053d1fad54c5d316aec4fe5d0816f9d3719e51d5b", previousReviewReliabilityPromptV2, sdd.RoleReliability},
		{"resilience", "36b431d4bb055a1cbc83d93a637c2a136cc4e256383c1933d0934666c3158e40", previousReviewResiliencePromptV2, sdd.RoleResilience},
		{"refuter", "da8cb44eea50019ee84b439054254a98ab2077022e3998c5ac50db3a78c5c81f", previousReviewRefuterPromptV2, sdd.RoleRefuter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := artifactSHA256([]byte(tc.base)); got != tc.digest {
				t.Fatalf("snapshot digest=%s", got)
			}
			if _, _, ok := managedArtifactMarker([]byte(tc.base)); !ok {
				t.Fatal("snapshot marker is malformed")
			}
		})
	}
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	v44, err := previousV44ModelPlanBundle(current)
	testutil.NoError(t, err)
	v43, err := previousV43ModelPlanBundle(v44)
	testutil.NoError(t, err)
	for _, name := range compactProtocolAgentNames {
		if !isManagedPredecessor(v43.agents[name], current.agents[name], [][]byte{v43.agents[name]}, nil) {
			t.Fatalf("%s v43 stream is not an exact predecessor", name)
		}
	}
}

func predecessorBundlesMust(t *testing.T, current modelPlanBundle) []modelPlanBundle {
	t.Helper()
	bundles, err := predecessorBundles(current)
	testutil.NoError(t, err)
	return bundles
}

func managerPredecessorsMust(t *testing.T, current modelPlanBundle) [][]byte {
	t.Helper()
	predecessors, err := managerPredecessors(current)
	testutil.NoError(t, err)
	return predecessors
}

func previousManagerV42Must(t *testing.T, current modelPlanBundle) modelPlanBundle {
	t.Helper()
	bundle, err := previousManagerModelPlanBundleV42(current)
	testutil.NoError(t, err)
	return bundle
}

func TestManagerPromptKeepsDeliveryAuthorityWithinStaticBudget(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	prompt := string(bundle.agents[managerAgentName])
	t.Logf("manager prompt bytes=%d newlines=%d", len(prompt), strings.Count(prompt, "\n"))

	// Current manager permits bounded additive readiness content in the generated prompt.
	const maxManagerPromptBytes = 19_000
	const maxManagerPromptLines = 105
	if bytes := len(prompt); bytes > maxManagerPromptBytes {
		t.Errorf("manager prompt bytes=%d, budget=%d", bytes, maxManagerPromptBytes)
	}
	if lines := strings.Count(prompt, "\n"); lines > maxManagerPromptLines {
		t.Errorf("manager prompt lines=%d, budget=%d", lines, maxManagerPromptLines)
	}
	for _, required := range []string{
		"Apply ceremony proportionally: small authorized repository changes remain delegated and do not imply SDD or delivery.",
		"sole engineering, orchestration, SDD lifecycle, Git, and GitHub authority",
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
	v43, err := previousV43ModelPlanBundle(current)
	testutil.NoError(t, err)
	v41, err := previousManagerModelPlanBundleV41(previousManagerV42Must(t, v43))
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
	v43, err := previousV43ModelPlanBundle(current)
	testutil.NoError(t, err)
	v41, err := previousManagerModelPlanBundleV41(previousManagerV42Must(t, v43))
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
	v43, err := previousV43ModelPlanBundle(current)
	testutil.NoError(t, err)
	predecessor, err := previousManagerModelPlanBundleV41(previousManagerV42Must(t, v43))
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
	v43, err := previousV43ModelPlanBundle(current)
	testutil.NoError(t, err)
	predecessor, err := previousManagerModelPlanBundleV42(v43)
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
		"Apply the shared adaptive execution contract below before acting.",
		"Use Explore only for complex repository evidence or diagnosis that materially benefits from read-only separation.",
		"Use managed general as the delegated implementation worker for clear authorized repository implementation, including necessary diagnosis, edits, and developmental checks",
		"reserve Explore -> General for genuine ambiguity.",
		"Avoid repeating child source exploration. Direct source inspection is exceptional for contradictory or missing evidence, candidate-identity mismatch, or severe findings; exact diff, path, status, and command evidence inspection remains mandatory.",
		"Route structural CodeGraph work to the delegated worker and use one bounded codegraph_explore query before broad reads or search where applicable.",
		"If CodeGraph is unavailable, missing, or stale, the delegated worker continues with native reads and search without blocking; it reads any specifically reported stale files directly.",
		"assumptions/blockers only when present",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("manager prompt is missing delegation-first contract %q", required)
		}
	}
	for _, superseded := range []string{
		"Work directly only for conversation and non-repository explanations, decisions, orchestration, lifecycle/Git authority, and compact synthesis.",
		"Default repository questions and diagnosis-only work to Explore.",
		"Return exactly one compact Child Return Envelope v1 JSON object",
	} {
		if strings.Contains(prompt, superseded) {
			t.Errorf("manager prompt retains superseded direct repository-work contract %q", superseded)
		}
	}
}

func TestPhase1ManagerPreservesDelegatedContextAndBoundsExpertEnsemble(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	manager, general := string(bundle.agents[managerAgentName]), string(bundle.agents[generalAgentName])

	for _, required := range []string{
		"sole digest-computation owner for every non-SDD repository delegation",
		"goal, criteria, nonGoals, decisions, authorization, constraints, evidenceRefs, lineage, and contextDigest",
		"object keys sorted lexicographically, no insignificant whitespace, array order preserved, and the contextDigest field omitted",
		"compute lowercase SHA-256 with an available read-only local hashing capability before task launch",
		"Count this computation within the selected route budget",
		"If the capability is unavailable, do not delegate",
		"parentContextDigest equal to the accepted parent contextDigest",
		"at most two non-overlapping Explore advisory lenses only for high ambiguity or a concrete hot path",
		"preserve one delegation slot for General when implementation is required",
		"General receives one Manager synthesis bound to the accepted contextDigest",
		"Direct and simple no-delegation routes do not create a capsule",
		"SDD missions retain their stronger accepted artifact, revision, digest, and stateVersion bindings without duplicating this capsule",
		"prompt-level continuity and provenance contract, not runtime enforcement",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("manager missing phase-1 context contract %q", required)
		}
	}
	for _, required := range []string{
		"Require a Context Capsule v1 for every non-SDD repository mission",
		"Echo the accepted contextDigest unchanged",
		"Manager-attested digest",
		"require parentContextDigest to equal the previously accepted contextDigest",
		"Do not independently recompute",
		"Integrate the Manager's digest-bound synthesis",
	} {
		if !strings.Contains(general, required) {
			t.Errorf("general missing phase-1 context contract %q", required)
		}
	}
}

func TestGeneralV9RequiresConciseDecisiveReturns(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	general := string(bundle.agents[generalAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/general; version: 10",
		"Ordinary implementation returns are entire compact Child Return Envelope v1 JSON objects serialized as UTF-8 and target <=512 bytes with status, changed paths, exact checks/results, and blockers only when present.",
		"Candidate identity, authorization, acceptance, and INCONCLUSIVE evidence are mandatory only",
		"The <=16 KiB envelope applies only to full-assurance frozen, risky, verification, or SDD missions.",
	} {
		if !strings.Contains(general, required) {
			t.Errorf("general v9 missing compact return contract %q", required)
		}
	}
}

func TestManagerPredecessorTemplatesRetainAcceptedDigests(t *testing.T) {
	for path, expected := range map[string]string{
		"templates/manager.v45.md": "533e71583ebcb656d500bc3c944e6e077848c778977ec8ef8c7cdc9429e62b97",
		"templates/general.v4.md":  "18d62d947e0755d63d3fa69bed7e3c4ac37ac84d249dfcbfe78c1917a4f7a91f",
		"templates/manager.v44.md": "f97a49b2107ee8c257295e42c09540704074f032d8549ffca1604896ccac3965",
		"templates/general.v3.md":  "616b7e5ff36848f505b87b94cc07caa0634c711db13914c30dc195c62bd467f1",
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

func TestManagerRetainsAuthorityWhileBroadProfilesDenyDurableMutations(t *testing.T) {
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
		if name == managerAgentName && (strings.Contains(frontmatter, ": deny") || strings.Contains(frontmatter, ": ask")) {
			t.Errorf("%s retains a permission that contradicts manager authority", name)
		}
		if name != managerAgentName && !strings.Contains(frontmatter, "vgxness_sdd_record_projection: deny") {
			t.Errorf("%s does not deny durable VGXNESS mutation", name)
		}
	}
}

func TestReadinessV54RoutesAdaptivelyAndKeepsFullAssuranceExceptions(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 55",
		"bounded simple exact reads use at most three total tool attempts and no delegation or todo",
		"complex evidence research may use at most one read-only delegation",
		"For a disposable/local-only, non-delivery, low-risk bounded change with deterministic readback, one General mission plus Manager readback may conclude `IMPLEMENTED`; do not automatically freeze, invoke verifier/review, or claim `VERIFIED`.",
		"Full frozen-candidate verifier/review assurance remains mandatory for delivery, risk/hot paths, explicit independent-verification requests, contradictory evidence, and SDD handoffs.",
		"A second task call for the same goal requires an explicit blocker, new evidence, correction, or independent assurance; resume the same child where applicable and send only the delta.",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("manager v54 missing %q", required)
		}
	}
}

func TestManagerUsesIntentTriggeredMemoryWithAllThenAnyFallback(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"Recall from VGXNESS memory only when the request indicates prior project context may matter.",
		"Search with vgxness_memory_search using all-term matching first; retry with any-term matching only when all-term results are insufficient.",
		"Inspect bounded previews, then call vgxness_memory_get with an exact ID only for relevant full content.",
		"Call vgxness_memory_recent only for an explicit recent-work, session, or compaction-recovery request; never use it as a routine first action.",
		"Before vgxness_memory_save, confirm the memory is durable and evidence-backed, and reuse a stable topic for the same subject.",
		"Never save secrets, personal data, transient state, raw logs, or transcripts.",
		"Call vgxness_memory_forget only on an explicit user request.",
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("manager missing memory contract %q", required)
		}
	}
	for _, forbidden := range []string{"when bounded recent context is absent or material to the task", "recent as the first project-context action"} {
		if strings.Contains(manager, forbidden) {
			t.Errorf("manager retains routine recent-first policy %q", forbidden)
		}
	}
}

func TestReadinessGeneralV10UsesCompactOrdinaryMissionAndReturn(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	general := string(bundle.agents[generalAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/general; version: 10",
		"Ordinary bounded missions are entire compact JSON objects serialized as UTF-8 and target <=512 bytes",
		"Ordinary implementation returns are entire compact Child Return Envelope v1 JSON objects serialized as UTF-8 and target <=512 bytes with status, changed paths, exact checks/results, and blockers only when present.",
		"Candidate identity, authorization, acceptance, and INCONCLUSIVE evidence are mandatory only when supplied or required by a frozen, risky, verification, or SDD mission.",
		"The <=16 KiB envelope applies only to full-assurance frozen, risky, verification, or SDD missions.",
		"Malformed, stale, oversized, or missing required evidence remains BLOCKED.",
	} {
		if !strings.Contains(general, required) {
			t.Errorf("general v10 missing %q", required)
		}
	}
}

func TestReadinessSDDApplyIsExclusiveWorkspaceWriter(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	general, apply, manager := string(bundle.agents[generalAgentName]), string(bundle.agents[sddApplyName]), string(bundle.agents[managerAgentName])
	for _, required := range []string{"artifact: opencode-agent/general; version: 10", "Reject SDD implementation or projection missions", "non-SDD implementation worker", "readiness-envelope/v1", "echo the accepted envelopeDigest"} {
		if !strings.Contains(general, required) {
			t.Errorf("general missing exclusive-writer boundary %q", required)
		}
	}
	for _, required := range []string{"artifact: opencode-agent/vgxness-sdd-apply; version: 7", "exclusive SDD workspace and projection writer", "exact post-write SHA-256", "RED/GREEN evidence", "readiness-envelope/v1", "echo the accepted envelopeDigest"} {
		if !strings.Contains(apply, required) {
			t.Errorf("sdd-apply missing exclusive-writer boundary %q", required)
		}
	}
	if !strings.Contains(manager, "Route accepted SDD apply directly to vgxness-sdd-apply") {
		t.Error("manager does not route accepted SDD apply directly")
	}
}

func TestReadinessV54ManagerIsAdaptiveWithoutNegativeCeremonyAndRecognizesV53ThenV52(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	manager := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 55",
		"adaptive general-purpose partner",
		"When the engineering route activates",
		orchestration.ContractPolicy,
		orchestration.ContractBudgetPolicy,
	} {
		if !strings.Contains(manager, required) {
			t.Errorf("manager v54 missing %q", required)
		}
	}
	for _, forbidden := range []string{"Use todowrite for multiple meaningful steps", "Load every clearly applicable native skill", "sole orchestration and SDD lifecycle authority"} {
		if strings.Contains(manager, forbidden) {
			t.Errorf("manager v52 retains unconditional ceremony %q", forbidden)
		}
	}
	predecessors := managerPredecessorsMust(t, bundle)
	if len(predecessors) < 6 || !bytes.Contains(predecessors[0], []byte("artifact: opencode-agent/vgxness-manager; version: 54")) || !bytes.Contains(predecessors[1], []byte("artifact: opencode-agent/vgxness-manager; version: 53")) || !bytes.Contains(predecessors[2], []byte("artifact: opencode-agent/vgxness-manager; version: 52")) || !bytes.Contains(predecessors[2], []byte(adaptiveManagerMemoryPolicy)) || !bytes.Contains(predecessors[2], []byte("Context Capsule v1")) {
		t.Fatal("exact v54, v53, then v52 predecessors with context continuity are not first")
	}
	if !bytes.Contains(predecessors[5], []byte("artifact: opencode-agent/vgxness-manager; version: 49")) || !bytes.Contains(predecessors[5], []byte(adaptiveManagerMemoryPolicy)) || bytes.Contains(predecessors[5], []byte("Context Capsule v1")) {
		t.Fatal("exact context-free v49 predecessor with intent-triggered memory semantics is not sixth")
	}
}

func TestVersionEvolutionUsesOnlyManagedHEADPredecessors(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	for name, marker := range map[string]string{
		managerAgentName:      "artifact: opencode-agent/vgxness-manager; version: 55",
		generalAgentName:      "artifact: opencode-agent/general; version: 10",
		exploreAgentName:      "artifact: opencode-agent/explore; version: 4",
		verifierAgentName:     "artifact: opencode-agent/vgxness-verifier; version: 7",
		reviewRiskName:        "artifact: opencode-agent/vgxness-review-risk; version: 4",
		reviewReadabilityName: "artifact: opencode-agent/vgxness-review-readability; version: 4",
		reviewReliabilityName: "artifact: opencode-agent/vgxness-review-reliability; version: 5",
		reviewResilienceName:  "artifact: opencode-agent/vgxness-review-resilience; version: 4",
		reviewRefuterName:     "artifact: opencode-agent/vgxness-review-refuter; version: 4",
		sddApplyName:          "artifact: opencode-agent/vgxness-sdd-apply; version: 7",
	} {
		if !bytes.Contains(current.agents[name], []byte(marker)) {
			t.Errorf("current %s missing %q", name, marker)
		}
	}
	bundles := predecessorBundlesMust(t, current)
	foundHEAD, foundProfileHEAD, foundLegacyHEAD := false, false, false
	for _, candidate := range bundles {
		exactHEAD := bytes.Contains(candidate.agents[managerAgentName], []byte("version: 50")) && bytes.Contains(candidate.agents[generalAgentName], []byte("version: 8")) && bytes.Contains(candidate.agents[exploreAgentName], []byte("version: 4")) && bytes.Contains(candidate.agents[verifierAgentName], []byte("version: 6")) && bytes.Contains(candidate.agents[sddApplyName], []byte("version: 5"))
		profileHEAD := bytes.Contains(candidate.agents[managerAgentName], []byte("version: 50")) && bytes.Contains(candidate.agents[generalAgentName], []byte("version: 8")) && bytes.Contains(candidate.agents[exploreAgentName], []byte("version: 3")) && bytes.Contains(candidate.agents[verifierAgentName], []byte("version: 5")) && bytes.Contains(candidate.agents[sddApplyName], []byte("version: 5"))
		if bytes.Contains(candidate.agents[managerAgentName], []byte("version: 50")) && !exactHEAD && !profileHEAD {
			t.Error("unrecognized manager-v50 package is recognized as a predecessor")
		}
		foundHEAD = foundHEAD || exactHEAD
		foundProfileHEAD = foundProfileHEAD || profileHEAD
		foundLegacyHEAD = foundLegacyHEAD || bytes.Contains(candidate.agents[managerAgentName], []byte("version: 49")) && bytes.Contains(candidate.agents[generalAgentName], []byte("version: 6")) && bytes.Contains(candidate.agents[exploreAgentName], []byte("version: 2")) && bytes.Contains(candidate.agents[verifierAgentName], []byte("version: 4")) && bytes.Contains(candidate.agents[reviewRiskName], []byte("version: 3")) && bytes.Contains(candidate.agents[reviewRefuterName], []byte("version: 3"))
	}
	if !foundHEAD || !foundProfileHEAD || !foundLegacyHEAD {
		t.Fatal("missing exact former HEAD, profile HEAD, or legacy HEAD predecessor bundle")
	}
	rejectedGeneralV10 := bytes.Replace(current.agents[generalAgentName], []byte("artifact: opencode-agent/general; version: 10"), []byte("artifact: opencode-agent/general; version: 11"), 1)
	rejectedExploreV5 := bytes.Replace(current.agents[exploreAgentName], []byte("artifact: opencode-agent/explore; version: 4"), []byte("artifact: opencode-agent/explore; version: 5"), 1)
	if len(previousGeneralV7(rejectedGeneralV10)) != 0 || len(previousExploreV3(rejectedExploreV5)) != 0 {
		t.Fatal("rejected local General v10 or Explore v5 is recognized as managed")
	}
}

func TestCurrentProfileAndManagerPredecessorStepsAreConstructible(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if _, err = previousActiveProfilesModelPlanBundle(current); err != nil {
		t.Fatalf("active profiles: %v", err)
	}
	v49, err := previousV49ModelPlanBundle(current)
	if err != nil {
		t.Fatalf("v49: %v", err)
	}
	for name, build := range map[string]func() (modelPlanBundle, error){
		"v48":              func() (modelPlanBundle, error) { return previousV48ModelPlanBundle(v49) },
		"v47":              func() (modelPlanBundle, error) { return previousV47ModelPlanBundle(v49) },
		"v46":              func() (modelPlanBundle, error) { return previousV46ModelPlanBundle(v49) },
		"broad-permission": func() (modelPlanBundle, error) { return previousBroadPermissionModelPlanBundle(v49) },
	} {
		if _, buildErr := build(); buildErr != nil {
			t.Errorf("%s: %v", name, buildErr)
		}
	}
	v45, err := previousV45ModelPlanBundle(current)
	if err != nil {
		t.Fatalf("v45: %v", err)
	}
	v44, err := previousV44ModelPlanBundle(current)
	if err != nil {
		t.Fatalf("v44: %v", err)
	}
	for name, candidate := range map[string]modelPlanBundle{"v49": v49, "v45": v45, "v44": v44} {
		if _, exploreErr := previousExploreModelPlanBundle(candidate); exploreErr != nil {
			t.Errorf("%s explore: %v", name, exploreErr)
		}
		if _, sddErr := previousSDDModelPlanBundle(candidate); sddErr != nil {
			t.Errorf("%s sdd: %v", name, sddErr)
		}
	}
}
