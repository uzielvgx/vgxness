package orchestration

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalRoutingOrder(t *testing.T) {
	for _, test := range []struct {
		name  string
		input Request
		want  Route
	}{
		{"non-repository", Request{}, RouteDirect},
		{"exact local read", Request{Repository: true, ExactLocalRead: true}, RouteDirect},
		{"SDD wins all conflicts", Request{Repository: true, ExactLocalRead: true, SDDAccepted: true, Implementation: true}, RouteSDD},
		{"implementation beats direct", Request{Repository: true, ExactLocalRead: true, Implementation: true}, RouteGeneral},
		{"implementation", Request{Repository: true, Implementation: true}, RouteGeneral},
		{"repository fallback", Request{Repository: true}, RouteExplore},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := RouteFor(test.input); got != test.want {
				t.Fatalf("RouteFor(%+v) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestContractPolicyProjectsAdaptiveExecutionAndOrthogonalMemory(t *testing.T) {
	for _, required := range []string{
		"silently classify domain, operation, side effect, complexity, and risk without tools or delegation",
		"conversation, writing, translation, summarization, brainstorming, and no-effect planning use zero execution tools, skills, todos, delegation, or review",
		"bounded simple exact reads use at most three total tool attempts and no delegation or todo",
		"complex evidence research may use at most one read-only delegation",
		"including failures and retries",
		"halt and report before the next attempt would exceed it, with no silent escalation",
		"prompt-level instructions, not runtime enforcement",
		"Load a skill only when its specialized workflow materially improves quality, safety, or verification",
		"Use a todo only when execution state or user-visible tracking benefits",
		"intent-triggered recall rules remain unchanged",
		"at most one memory tool",
		"never save transient state, logs, secrets, or personal data",
		"automatically cloud-sync",
	} {
		if !strings.Contains(ContractPolicy, required) {
			t.Errorf("adaptive contract missing %q", required)
		}
	}
}

func TestContractPolicyProjectsCanonicalNumericBudgets(t *testing.T) {
	const want = "Canonical budgets: direct 0 tools/0 delegations; assisted simple 3 tools/0 delegations and complex 3 tools/1 delegation; action 6 tools/0 delegations; engineering 30 tools/5 delegations; assured 40 tools/5 delegations."
	if ContractBudgetPolicy != want || !strings.Contains(ContractPolicy, want) {
		t.Fatalf("numeric budget projection differs\npolicy: %q\ncontract: %q", ContractBudgetPolicy, ContractPolicy)
	}
}

func TestContractLimitsReadOnlyConcurrencyAndResumesBudgetWindows(t *testing.T) {
	for _, required := range []string{
		"at most five independent read-only agents concurrently", "never overlap workspace writers", "exhaustion is a checkpoint", "explicit user continuation opens a fresh same-route window", "scope, authorization, lineage, todos, candidate, and child context",
	} {
		if !strings.Contains(ContractPolicy, required) {
			t.Errorf("contract missing %q", required)
		}
	}
}

func TestReadinessContractsAreEvidenceOnlyAndPreserveExemptRoutes(t *testing.T) {
	for _, required := range []string{
		"readiness-envelope/v1", "Manager alone assembles and immediately revalidates", "never approval, authorization, validation, review, lifecycle authority, or host enforcement", "invalidate it when mission identity, scope, acceptance criteria, skills, permitted validation, candidate, provider artifact, target hash, or dependency changes", "Direct and exempt routes create no readiness envelope or ceremony",
	} {
		if !strings.Contains(ReadinessManagerContract, required) {
			t.Errorf("manager readiness contract missing %q", required)
		}
	}
	for _, required := range []string{"reject missing, stale, malformed, mismatched, BLOCKED, or INCONCLUSIVE readiness envelopes before writing", "echo the accepted envelopeDigest", "never approval or host enforcement"} {
		if !strings.Contains(ReadinessWriterContract, required) {
			t.Errorf("writer readiness contract missing %q", required)
		}
	}
}

func TestStructuralEvidenceReuseFallsBackWhenUnsafe(t *testing.T) {
	valid := StructuralEvidence{Contract: ContractIdentity, Query: "flow", Revision: "abc", Digest: strings.Repeat("a", 64), Source: "codegraph", Paths: []string{"a.go"}, Symbols: []string{"Run"}, CallPath: []string{"Run->Check"}}
	if !valid.ReusableFor("flow", "abc", strings.Repeat("a", 64)) || valid.FallbackRequired("flow", "abc", strings.Repeat("a", 64)) {
		t.Fatal("matching structural evidence should be reusable")
	}
	for _, test := range []struct {
		name                    string
		evidence                StructuralEvidence
		query, revision, digest string
	}{
		{"query mismatch", valid, "other", "abc", strings.Repeat("a", 64)},
		{"revision mismatch", valid, "flow", "old", strings.Repeat("a", 64)},
		{"digest mismatch", valid, "flow", "abc", strings.Repeat("b", 64)},
		{"stale", StructuralEvidence{Contract: ContractIdentity, Query: "flow", Revision: "abc", Digest: strings.Repeat("a", 64), Source: "codegraph", Paths: []string{"a.go"}, Symbols: []string{"Run"}, CallPath: []string{"Run->Check"}, Stale: true}, "flow", "abc", strings.Repeat("a", 64)},
		{"contradicted", StructuralEvidence{Contract: ContractIdentity, Query: "flow", Revision: "abc", Digest: strings.Repeat("a", 64), Source: "codegraph", Paths: []string{"a.go"}, Symbols: []string{"Run"}, CallPath: []string{"Run->Check"}, Contradicted: true}, "flow", "abc", strings.Repeat("a", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.evidence.ReusableFor(test.query, test.revision, test.digest) || !test.evidence.FallbackRequired(test.query, test.revision, test.digest) {
				t.Fatalf("unsafe evidence %+v did not require fallback", test.evidence)
			}
		})
	}
}

func TestStructuralEvidenceBounds(t *testing.T) {
	valid := StructuralEvidence{Contract: ContractIdentity, Query: "q", Revision: "r", Digest: strings.Repeat("a", 64), Source: "s", Paths: []string{"p"}, Symbols: []string{"S"}, CallPath: []string{"S->T"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.Paths = make([]string, maxEvidenceItems+1)
	for i := range valid.Paths {
		valid.Paths[i] = "p"
	}
	if err := valid.Validate(); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("Validate() = %v, want bounded-capsule error", err)
	}
}

func TestReviewDepth(t *testing.T) {
	for _, test := range []struct {
		paths []string
		want  ReviewDepth
	}{
		{[]string{"docs/guide.md", "diagram.png"}, ReviewNone},
		{[]string{"diagram.webp"}, ReviewNone},
		{[]string{"docs/security.md", "diagram.png"}, ReviewNone},
		{[]string{"internal/widget/render.go"}, ReviewOne},
		{[]string{"docs/guide.md", "internal/widget/render.go"}, ReviewOne},
		{[]string{"internal/author/data.go"}, ReviewOne},
		{[]string{"internal/permissions.go"}, ReviewFour},
		{[]string{"internal/widget/render.go", "internal/auth.go"}, ReviewFour},
		{[]string{"internal/auth.go", "internal/permissions.go", "internal/payment.go", "internal/shell.go"}, ReviewFour},
		{[]string{"internal/providers/opencode/integration.go", "internal/widget/render.go"}, ReviewFour},
		{[]string{"internal/author/data.go", "internal/commandment.go"}, ReviewOne},
		{[]string{"internal/installer/install.go"}, ReviewFour},
		{[]string{"internal/runtime/process.go"}, ReviewFour},
		{[]string{"internal/providers/opencode/integration.go"}, ReviewFour},
	} {
		if got := ReviewDepthFor(test.paths); got != test.want {
			t.Fatalf("ReviewDepthFor(%v) = %d, want %d", test.paths, got, test.want)
		}
	}
}
