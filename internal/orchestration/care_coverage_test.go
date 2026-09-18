package orchestration

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type careCoverageCorpus struct {
	SchemaVersion string             `json:"schemaVersion"`
	EvidenceKind  string             `json:"evidenceKind"`
	Partition     string             `json:"partition"`
	Holdout       string             `json:"holdout"`
	Execution     string             `json:"execution"`
	Limitations   string             `json:"limitations"`
	Cases         []careCoverageCase `json:"cases"`
}

type careCoverageCase struct {
	ID             string   `json:"id"`
	Classification string   `json:"classification"`
	Roles          []string `json:"roles"`
	Criterion      string   `json:"criterion"`
	UsageScenario  string   `json:"usageScenario"`
	MaterialsGiven []struct {
		Kind     string `json:"kind"`
		Language string `json:"language"`
		Content  string `json:"content"`
	} `json:"materialsGiven"`
	NeededMaterials  []string `json:"neededMaterials"`
	PolicyAssertions []struct {
		Role     string `json:"role"`
		Fragment string `json:"fragment"`
	} `json:"policyAssertions"`
	ExpectedAssertions []string `json:"expectedAssertions"`
	ForbiddenBehavior  []string `json:"forbiddenBehavior"`
	Limits             string   `json:"limits"`
}

func TestCARECoverageContractConformance(t *testing.T) {
	raw, err := os.ReadFile("testdata/care-coverage-scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus careCoverageCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != "vgxness-care-coverage-scenarios/v1" || corpus.EvidenceKind != "deterministic-contract-conformance" || corpus.Partition != "development" || corpus.Holdout != "not protected holdout" || corpus.Execution != "unexecuted behavior" || !strings.Contains(corpus.Limitations, "not model behavior") {
		t.Fatal("invalid CARE development corpus metadata")
	}
	required := map[string]bool{
		"explicit-flag-vs-default": false, "ambient-state-coexistence": false,
		"newly-reachable-unchanged-path": false, "inaccessible-source-persuasive-summary": false,
		"regression-introduced-by-correction": false, "well-evidenced-small-change-negative-control": false,
		"missing-evidence-continuation": false, "infrastructure-retry": false,
	}
	classifications := map[string]bool{"coverage-variant": true, "dependency-expansion": true, "access-continuation": true, "correction-regression": true, "negative-control": true, "evidence-continuation": true, "infrastructure-continuation": true}
	materialKinds := map[string]bool{"code": true, "diff": true, "receipt": true, "summary": true, "metadata": true, "prior-finding": true, "fixture": true}
	c, err := LoadManagerContract()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, scenario := range corpus.Cases {
		if seen[scenario.ID] || scenario.ID == "" || !hasRequiredCase(required, scenario.ID) || !classifications[scenario.Classification] || scenario.Criterion == "" || scenario.UsageScenario == "" || scenario.Limits == "" || len(scenario.Roles) == 0 || len(scenario.MaterialsGiven) == 0 || scenario.NeededMaterials == nil || len(scenario.PolicyAssertions) == 0 || len(scenario.ExpectedAssertions) == 0 || len(scenario.ForbiddenBehavior) == 0 {
			t.Fatalf("invalid CARE scenario %q", scenario.ID)
		}
		seen[scenario.ID] = true
		for _, material := range scenario.MaterialsGiven {
			if !materialKinds[material.Kind] || material.Language == "" || material.Content == "" {
				t.Fatalf("invalid material for %s", scenario.ID)
			}
		}
		for _, expected := range append(scenario.ExpectedAssertions, scenario.ForbiddenBehavior...) {
			if expected == "" {
				t.Fatalf("empty expected or forbidden behavior for %s", scenario.ID)
			}
		}
		roles := map[string]bool{}
		for _, role := range scenario.Roles {
			if _, ok := c.Role(role); !ok || roles[role] {
				t.Fatalf("invalid role %q for %s", role, scenario.ID)
			}
			roles[role] = true
		}
		for _, assertion := range scenario.PolicyAssertions {
			role, ok := c.Role(assertion.Role)
			if !ok || !roles[assertion.Role] || assertion.Fragment == "" || !strings.Contains(role.Instructions, assertion.Fragment) {
				t.Fatalf("unbound policy assertion for %s", scenario.ID)
			}
		}
	}
	for id, present := range required {
		if !present {
			t.Errorf("missing required CARE scenario %s", id)
		}
	}
}

func hasRequiredCase(required map[string]bool, id string) bool {
	_, ok := required[id]
	if ok {
		required[id] = true
	}
	return ok
}

func TestCARECoverageRolePolicies(t *testing.T) {
	c, err := LoadManagerContract()
	if err != nil {
		t.Fatal(err)
	}
	policies := map[string][]string{
		"manager":         {"Before a CARE mission provide authorized access", "exact source or diff", "criterion to a usage scenario", "pertinent flags, defaults, and ambient state proportionately", "newly reachable pre-existing paths", "A source change invalidates prior candidate evidence; prior findings are context, not approval of a new candidate.", "re-evaluate conclusions contradicted by new evidence", "Classify continuations as defect, evidence, mission, access, infrastructure, or scope", "not retry until PASS", "elevated-risk production change"},
		"general":         {"Report scenarios covered and open assumptions."},
		"care-reviewer":   {"At the start confirm that supplied source and evidence are inspectable", "usage scenario, evidence/assertions, and limits", "newly reachable pre-existing paths", "After correction, retain earlier findings and recheck closures and regressions", "demonstrated findings with severity, path, and rationale", "Required criterion without evidence blocks PASS"},
		"care-specialist": {"exact source or diff", "Manager summary does not substitute", "usage scenario, evidence/assertions, and limits", "newly reachable pre-existing paths", "After correction, recheck previous finding closures and regressions", "A source change invalidates prior candidate evidence; prior findings are context, not approval of a new candidate.", "return PASS, FAIL, or INCONCLUSIVE without modifying files"},
		"care-challenger": {"only the assigned material conclusion", "inspectable exact supporting source and evidence", "summary does not substitute", "unavailable evidence is INCONCLUSIVE", "source change requires rebind to the new frozen candidate", "Report actual examined scope, evidence, and exclusions", "Never change the candidate."},
		"verifier":        {"return PASS, FAIL, or INCONCLUSIVE with concrete evidence", "Required criterion without evidence blocks PASS"},
	}
	for name, fragments := range policies {
		role, ok := c.Role(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		for _, fragment := range fragments {
			if !strings.Contains(role.Instructions, fragment) {
				t.Errorf("%s policy absent from %s", fragment, name)
			}
		}
	}
}
