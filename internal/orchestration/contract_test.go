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
