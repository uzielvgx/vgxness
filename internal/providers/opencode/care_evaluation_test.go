package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCAREDocumentationContract(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	docs := []string{
		"docs/care.md",
		"docs/care-evaluation.md",
		"docs/orchestration-flow.md",
		"docs/opencode-integration.md",
		"docs/codex-integration.md",
		"docs/opencode-setup-wizard.md",
		"docs/go-implementation.md",
		"docs/self-install.md",
		"docs/legacy-compatibility.md",
	}
	boundary := []string{
		"independent evaluator outside the repository",
		"protected holdout registration, custody, partitioning, contents, labels, graders, digest computation, runs, evidence validation, and adjudication",
		"opaque, evaluator-issued, digest-bound evidence",
		"User-provided, repository-derived, fabricated, placeholder, manifest, or disclosed-holdout material cannot support protected-holdout adjudication.",
		"Missing, stale, malformed, mismatched, insufficient, or unavailable evidence is INCONCLUSIVE or BLOCKED, never PASS or VERIFIED.",
		"Repository tests establish static conformance only; they do not establish a protected-holdout result.",
	}
	for _, doc := range docs {
		body, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Errorf("read %s: %v", doc, err)
			continue
		}
		if doc == "docs/care.md" || doc == "docs/care-evaluation.md" {
			for _, phrase := range boundary {
				if !strings.Contains(string(body), phrase) {
					t.Errorf("%s is missing required CARE boundary wording: %q", doc, phrase)
				}
			}
		}
	}

	themes := map[string][]string{
		"docs/care.md":                  {"CARE is the default for non-exempt work", "Standard allocates a reviewer", "elevated allocates a reviewer plus specialist", "critical allocates all three roles", "required attempts are respectively 3, 4, and 5", "exact frozen Review Binding", "one bounded manager-assigned assurance domain", "at most five stable typed targets", "corroborated, refuted, or inconclusive", "without inventing scope, findings, or fixes", "OpenCode currently has 13 agents", "Codex currently has `AGENTS.md` plus 12 delegated profiles", "no current fixed-lens aliases"},
		"docs/care-evaluation.md":       {"Direct covers no-tool conversation, writing, and planning", "Assisted covers bounded exact reads and evidence work", "authorized actions", "ordinary engineering", "assured high-risk work", "positive routing", "negative non-activation", "ambiguous requests", "adversarial", "coexistence", "critical cases"},
		"docs/orchestration-flow.md":    {"CARE records the route, risk, evidence ledger"},
		"docs/opencode-integration.md":  {"16 managed artifacts", "13 agents", "manager v55", "manager-v54 package is the immediate predecessor", "manager-v53/verifier-v6", "verifier v7", "three CARE v1 roles", "six SDD roles"},
		"docs/codex-integration.md":     {"12 delegated profiles", "Manager v15", "OpenCode v55 parity", "care-reviewer", "care-specialist", "care-challenger"},
		"docs/opencode-setup-wizard.md": {"16 OpenCode-managed artifacts", "13 agents", "manager v55", "Runtime evidence is observed on macOS only"},
		"docs/go-implementation.md":     {"16 managed artifacts", "manager v55", "manager-v54 immediate predecessor", "manager-v53/verifier-v6 deeper lifecycle identity", "manager v15", "manager v14 artifact validated as its immediate historical predecessor", "manager v13 retained as a deeper lifecycle identity", "12 delegated profiles", "not a Go provider runtime or a new schema/transport surface"},
		"docs/self-install.md":          {"predecessors only for lifecycle and upgrade handling"},
		"docs/legacy-compatibility.md":  {"no current fixed-lens aliases"},
	}
	for doc, phrases := range themes {
		body, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Errorf("read %s: %v", doc, err)
			continue
		}
		for _, phrase := range phrases {
			if !strings.Contains(string(body), phrase) {
				t.Errorf("%s is missing CARE theme: %q", doc, phrase)
			}
		}
	}
	for _, doc := range []string{"docs/opencode-integration.md", "docs/codex-integration.md", "docs/opencode-setup-wizard.md", "docs/go-implementation.md"} {
		body, _ := os.ReadFile(filepath.Join(root, doc))
		for _, stale := range []string{"Current delivery policy is manager v53", "Current manager v13", "currently owns 18 managed artifacts", "currently owns only `AGENTS.md` and 14 files"} {
			if strings.Contains(string(body), stale) {
				t.Errorf("%s retains stale current guidance: %q", doc, stale)
			}
		}
	}

	manifest := filepath.Join(root, "internal/providers/opencode/testdata/care-policy-holdout-manifest.json")
	if _, err := os.Lstat(manifest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("forbidden repository holdout manifest must remain absent: %v", err)
	}
}
