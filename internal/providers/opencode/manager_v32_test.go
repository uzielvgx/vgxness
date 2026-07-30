package opencode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestManagerV34IsReadOnlyLifecycleAuthority(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	prompt := string(bundle.agents[managerAgentName])
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 34",
		`"*": deny`, "read: allow", "grep: allow", "glob: allow", "list: allow", "skill: allow", "codegraph_explore: allow",
		"edit: deny", "question: allow", "todowrite: allow", `"git log --oneline -10": allow`, `"git show --stat": allow`,
		"general: allow", "vgxness-verifier: allow", "explore: allow", "vgxness-review-risk: allow", "vgxness-review-refuter: allow",
		"vgxness-sdd-research: allow", "vgxness-sdd-apply: allow", "vgxness_sdd_accept_revision: allow", "vgxness_sdd_transition: allow",
		"general for all authorized workspace writing", "verifier for independent final executable validation", "same frozen candidate",
		"goal, scope, nonGoals, acceptanceCriteria, evidenceScope, validation, and stopCondition",
		"in-session launch log keyed by normalized goal and scope", "Never launch the same task twice",
		"Zero lenses", "One dominant lens", "Four lenses", "permissions, authentication, secrets", "same frozen candidate",
		"severe inferential findings", "one batch", "at most one correction transaction and one scoped validation",
		"unavailable, missing, or stale", "continue with native reads and search without blocking",
		"automatically injected recent-memory reference block", "only when that bounded context block is absent or unavailable",
		"installation, permissions, durability, or shared contracts", "repository-confined `go fmt ./...` command and focused tests before freeze", "verifier to run go test ./... and go vet ./...",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("manager missing %q", required)
		}
	}
	for _, forbidden := range []string{"edit: allow", `"go test *": allow`, "Use a fresh general worker for execution-heavy verification", "manager is the sole workspace writer", "run the RED/GREEN tests and other validation directly"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("manager retains writer/verifier authority %q", forbidden)
		}
	}
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"git `) && strings.HasSuffix(line, `": allow`) && (strings.Contains(line, "*") || strings.Contains(line, "--output") || strings.ContainsAny(line, ">|")) {
			t.Errorf("manager has unsafe read-only Git allow %q", line)
		}
	}
	for _, forbidden := range []string{`"git log*": allow`, `"git show*": allow`, `"git rev-parse*": allow`, `"git ls-files*": allow`, `"git cat-file*": allow`} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("manager retains broad Git permission %q", forbidden)
		}
	}
}

func TestManagedGeneralAndVerifierLeastPrivilege(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if len(bundle.agents) != 15 {
		t.Fatalf("agents = %d, want 15", len(bundle.agents))
	}

	general := string(bundle.agents[generalAgentName])
	implementation := bundle.resolved.Roles[sdd.RoleImplementation]
	for _, required := range []string{
		"artifact: opencode-agent/general; version: 2", "mode: subagent", "hidden: true",
		"model: " + implementation.Model, "variant: " + string(implementation.Variant), `"*": deny`,
		"read: allow", "grep: allow", "glob: allow", "list: allow", "skill: allow", "codegraph_explore: allow", "edit: allow",
		"task: deny", "question: deny", "external_directory: deny", "webfetch: deny", "websearch: deny",
		`"go test*": allow`, `"git *": deny`, `"npm install*": deny`,
		`"go fmt ./...": allow`, `"go build ./...": allow`, `"git diff --no-index*": deny`,
		"sole ordinary workspace writer", "mission-authorized", "Do not accept revisions, transition phases, or record projections",
		"Normal implementation missions do not require SDD revision bindings or file hashes",
		"unsupported mutating or generator command", "return BLOCKED",
	} {
		if !strings.Contains(general, required) {
			t.Errorf("general missing %q", required)
		}
	}
	for _, forbidden := range []string{"task: allow", "question: allow", "webfetch: allow", "vgxness_memory_save: allow", "vgxness_sdd_transition: allow", `"gofmt *": allow`, `"go generate*": allow`, `"go build*": allow`} {
		if strings.Contains(general, forbidden) {
			t.Errorf("general allows %q", forbidden)
		}
	}
	assertExactGitInspectionPermissions(t, "general", general)

	verifier := string(bundle.agents[verifierAgentName])
	verification := bundle.resolved.Roles[sdd.RoleVerification]
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-verifier; version: 2", "mode: subagent", "hidden: true",
		"model: " + verification.Model, "variant: " + string(verification.Variant), `"*": deny`,
		"read: allow", "grep: allow", "glob: allow", "list: allow", "skill: allow", "codegraph_explore: allow",
		"edit: deny", "task: deny", "question: deny", "external_directory: deny", "webfetch: deny", "websearch: deny",
		`"go build ./...": allow`,
		"frozen candidate digest before and after", "exact permitted commands", "PASS|FAIL|INCONCLUSIVE", "no fix, generator, install, snapshot-update",
	} {
		if !strings.Contains(verifier, required) {
			t.Errorf("verifier missing %q", required)
		}
	}
	for _, forbidden := range []string{"edit: allow", "task: allow", "question: allow", "webfetch: allow", "vgxness_memory_search: allow", "vgxness_sdd_get: allow", `"go build*": allow`, `"go fmt`, `"gofmt`, `"go generate`} {
		if strings.Contains(verifier, forbidden) {
			t.Errorf("verifier allows %q", forbidden)
		}
	}
	assertExactGitInspectionPermissions(t, "verifier", verifier)
}

func assertExactGitInspectionPermissions(t *testing.T, name, prompt string) {
	t.Helper()
	for _, required := range []string{
		`"git status": allow`, `"git status --short": allow`, `"git status --porcelain": allow`,
		`"git diff": allow`, `"git diff --stat": allow`, `"git diff --name-only": allow`, `"git diff --check": allow`, `"git diff --cached": allow`,
		`"git diff --no-index*": deny`,
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("%s missing exact Git permission %q", name, required)
		}
	}
	for _, forbidden := range []string{`"git status*": allow`, `"git diff*": allow`} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("%s retains broad Git permission %q", name, forbidden)
		}
	}
}

func TestHistoricalModelPlanRecognizesExactV31(t *testing.T) {
	previous, err := buildV31ModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	if len(previous.agents) != 13 || !bytes.Contains(previous.agents[managerAgentName], []byte("version: 31")) {
		t.Fatalf("invalid v31 fixture: agents=%d", len(previous.agents))
	}
	_, parsed, err := parseInstalledModelPlanManifest(previous.manifest)
	testutil.NoError(t, err)
	if !bytes.Equal(parsed.manifest, previous.manifest) {
		t.Fatal("v31 manifest did not round trip byte-exactly")
	}
}

func TestV32HighPlanMatchesInstalledBundle(t *testing.T) {
	config := sdd.DefaultModelPlanConfig()
	config.ActivePlan = sdd.PlanHigh
	config.Provenance = sdd.ModelPlanCLI
	bundle, err := buildV32ModelPlanBundle(config)
	testutil.NoError(t, err)
	if got := artifactSHA256(bundle.manifest); got != "d41137a83ee57125c3c6371961990d2eba60400af6b7230b9b87cb40501835df" {
		t.Fatalf("high-plan v32 manifest sha256=%s", got)
	}
	if got := artifactSHA256(bundle.agents[managerAgentName]); got != "d2491649ce3dc56f8299d77e532f3f3e5cf8a64a65874024d4ac2ef5eeb7b404" {
		t.Fatalf("high-plan v32 manager sha256=%s", got)
	}
	_, parsed, err := parseInstalledModelPlanManifest(bundle.manifest)
	testutil.NoError(t, err)
	if !bytes.Equal(parsed.manifest, bundle.manifest) {
		t.Fatal("installed high-plan v32 identity did not round trip")
	}
}

func TestV32HistoricalManifestDigestsAcrossPlans(t *testing.T) {
	want := map[sdd.Plan]string{
		sdd.PlanLow:    "e640ad64d55866b18c60fe47ef57cd3f37f512623a752216c00370edf5801509",
		sdd.PlanMedium: "25fc7ef2ffef4c43feb529bf3ff7502b1216dfaaa2a16df2f7e8b44c54fb9890",
		sdd.PlanHigh:   "d41137a83ee57125c3c6371961990d2eba60400af6b7230b9b87cb40501835df",
	}
	for plan, expected := range want {
		config := sdd.DefaultModelPlanConfig()
		config.ActivePlan = plan
		config.Provenance = sdd.ModelPlanCLI
		bundle, err := buildV32ModelPlanBundle(config)
		testutil.NoError(t, err)
		if len(bundle.agents) != 15 || len(bundle.resolved.Roles) != 14 {
			t.Fatalf("%s v32 bundle agents=%d roles=%d", plan, len(bundle.agents), len(bundle.resolved.Roles))
		}
		if got := artifactSHA256(bundle.manifest); got != expected {
			t.Errorf("%s v32 manifest sha256=%s, want %s", plan, got, expected)
		}
		for name, marker := range map[string]string{
			managerAgentName:  "artifact: opencode-agent/vgxness-manager; version: 32",
			generalAgentName:  "artifact: opencode-agent/general; version: 1",
			verifierAgentName: "artifact: opencode-agent/vgxness-verifier; version: 1",
			sddApplyName:      "artifact: opencode-agent/vgxness-sdd-apply; version: 3",
		} {
			if !bytes.Contains(bundle.agents[name], []byte(marker)) {
				t.Errorf("%s v32 agent %s missing %q", plan, name, marker)
			}
		}
	}
}

func TestSDDChildrenCannotAcquireWriterOrLifecycleAuthority(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	for _, name := range []string{sddResearchName, sddProposalName, sddSpecName, sddDesignName, sddTasksName, sddApplyName} {
		profile := string(bundle.agents[name])
		for _, forbidden := range []string{"general: allow", "vgxness-verifier: allow", "vgxness_sdd_save_revision: allow", "vgxness_sdd_accept_revision: allow", "vgxness_sdd_transition: allow", "vgxness_sdd_record_projection: allow"} {
			if strings.Contains(profile, forbidden) {
				t.Errorf("%s acquired authority %q", name, forbidden)
			}
		}
	}
	apply := string(bundle.agents[sddApplyName])
	for _, required := range []string{"artifact: opencode-agent/vgxness-sdd-apply; version: 3", "hash-bound candidate", "managed general performs workspace writes", "verifier executes final validation"} {
		if !strings.Contains(apply, required) {
			t.Errorf("apply missing handoff %q", required)
		}
	}
}
