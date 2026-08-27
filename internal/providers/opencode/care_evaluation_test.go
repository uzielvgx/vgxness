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
		"docs/care.md":                  {"CARE is the default for non-exempt work", "Standard allocates a reviewer", "elevated allocates a reviewer plus specialist", "critical allocates all three roles", "required attempts are respectively 3, 4, and 5", "exact frozen Review Binding", "exactly one bounded manager-assigned assurance domain", "at most five stable typed targets", "PASS, FAIL, or INCONCLUSIVE", "never invents claims, findings, scope, or fixes", "OpenCode current identity is CARE v2 with Manager59", "Codex current identity is Manager18", "no current fixed-lens aliases"},
		"docs/care-evaluation.md":       {"Direct covers no-tool conversation, writing, and planning", "Assisted covers bounded exact reads and evidence work", "authorized actions", "ordinary engineering", "assured high-risk work", "positive routing", "negative non-activation", "ambiguous requests", "adversarial", "coexistence", "critical cases"},
		"docs/orchestration-flow.md":    {"CARE records the route, risk, evidence ledger"},
		"docs/opencode-integration.md":  {"17 managed artifacts", "13 agents", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "Manager59", "immediate predecessor is exact CARE-v2 Manager58", "CARE-v1 Manager58 and CARE-v1 Manager57", "OpenCode v56/verifier-v6", "verifier v7", "three CARE v2 roles", "six SDD roles"},
		"docs/codex-integration.md":     {"12 delegated profiles", "Manager v18", "parity OpenCode manager v59", "care-reviewer", "care-specialist", "care-challenger"},
		"docs/opencode-setup-wizard.md": {"17 managed artifacts", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "13 agents", "Manager59", "OpenCode immediate CARE-v2/Manager58", "Runtime evidence is observed on macOS only"},
		"docs/go-implementation.md":     {"17 managed artifacts", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "OpenCode current CARE-v2 Manager59 roles", "immediate CARE-v2/Manager58 predecessor", "CARE-v1/Manager58 and CARE-v1/Manager57", "OpenCode v56/verifier-v6 deeper lifecycle identity", "Codex current Manager18", "Codex Manager17 as immediate predecessor", "Manager16 and deeper Manager15/v14", "12 delegated profiles", "not a Go provider runtime or a new schema/transport surface"},
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
		for _, stale := range []string{"Current delivery policy is manager v58", "The current generated manager is v17", "OpenCode current is CARE v2 with Manager58", "OpenCode immediate predecessor is exact CARE-v1 with Manager58", "OpenCode immediate CARE-v1/Manager58 predecessor", "Codex current Manager17", "Codex immediate predecessor is Manager16", "Current delivery policy is manager v55", "The current generated manager is v15", "The exact manager-v54 package is the immediate predecessor", "its exact v14 artifact is the immediate predecessor", "manager v55 with global tool permission", "Manager v15 shares OpenCode v55's provider-neutral prompt contract", "manager v15 has OpenCode v55 parity", "For an eligible implementation task, manager v55", "The integration is installed only when manager v55"} {
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
