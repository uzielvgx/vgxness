package orchestration

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestPreAdaptiveManagerContractSnapshotIdentity(t *testing.T) {
	const rawSHA256 = "24fb8242b8d3a41bebad7f5247486fbbec400578a4471bf09c4472a9281b8d82"
	const canonicalSHA256 = "e19c977b0b6898cf8a4bedac894dcb1f27c8fb71e3b3797171ebef5cbf809226"
	sum := sha256.Sum256(preAdaptiveManagerContractBytes)
	if got := hex.EncodeToString(sum[:]); got != rawSHA256 {
		t.Fatalf("pre-adaptive snapshot bytes = %s, want %s", got, rawSHA256)
	}
	if got := PreAdaptiveManagerContractDigest(); got != canonicalSHA256 {
		t.Fatalf("pre-adaptive snapshot canonical digest = %s, want %s", got, canonicalSHA256)
	}
	if _, err := LoadPreAdaptiveManagerContract(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerContractIsCanonicalAndComplete(t *testing.T) {
	c, err := LoadManagerContract()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Roles) != 6 || ManagerContractDigest() == "" {
		t.Fatal("unexpected registry")
	}
	for _, name := range []string{"explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger"} {
		if _, ok := c.Role(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}
	if !c.CanWrite("general") || c.CanWrite("sdd-apply") || c.CanWrite("explore") {
		t.Fatal("write authority drift")
	}
}

func TestManagerContractUsesAdaptiveFlowAndTDDPolicy(t *testing.T) {
	c, err := LoadManagerContract()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"Choose the lightest sufficient flow based on affected behavior, uncertainty, blast radius, reversibility, and risk—not diff size.",
		"Delegate all project code exploration to the explore role: reading files, searching, listing, and read-only diagnosis of repository content, with no simple exception.",
		"You retain a narrow, explicit authority for operational inspection that explore cannot perform because it has no shell",
		"local Git queries (status, diff, log, refs, tracking, conflicts)",
		"reading only the active host configuration binding required for registry and launcher bootstrap",
		"never for general parallel exploration, never to bypass a native deny",
		"observed denied only when a real tool error proved it, unavailable when the dependency or transport is absent, contractual abstention when your role forbids the action, and not checked when you did not inspect it",
		"never request approval for routine inspection, and never bypass a denial with Python or shell",
		"read only the already-bound vgxness launcher path from the active host configuration at its evidenced location",
		"never search PATH, never dump secrets, and never fall back silently to a different launcher",
		"delegate the selected name, sha256, and resource paths to the worker, which reads and validates the body",
		"Never load the whole catalog or a skill body on the worker's behalf",
		"Do not ask routine approvals for that choice.",
		"For substantive behavior changes, cross-cutting work, elevated risk, or explicit delivery, require independent verification and applicable CARE review of the same exact candidate.",
		"Escalate when risk is discovered.",
		"never omit an explicit user, repository, or delivery gate.",
		"TDD is optional and preferred for reproducible defects or clear contracts when useful; do not manufacture RED evidence.",
		"expected failure was observed before the change",
		"Refactors use existing regressions and may add coverage where it is missing.",
		"Tests and CARE are complementary, not interchangeable.",
	} {
		if !strings.Contains(c.Manager.Instructions, fragment) {
			t.Errorf("manager policy lacks %q", fragment)
		}
	}
	if strings.Contains(c.Manager.Instructions, "without a formal plan, delegation, RED, or CARE ritual") {
		t.Fatal("manager policy retains the removed broad no-delegation exception")
	}
	for _, forbidden := range []string{
		"including status checks, reviews, and read-only diagnosis; no simple exception",
		"do not browse the project yourself",
	} {
		if strings.Contains(c.Manager.Instructions, forbidden) {
			t.Fatalf("manager policy retains the over-broad exploration block %q", forbidden)
		}
	}
	if strings.Contains(c.Manager.Instructions, "Load only a relevant managed skill") {
		t.Fatal("manager policy retains the broad pre-task skill load")
	}
	verifier, ok := c.Role("verifier")
	if !ok || !strings.Contains(verifier.Instructions, "shell access is not a read-only guarantee") || !strings.Contains(verifier.Instructions, "You must not edit files, delegate tasks") {
		t.Fatal("verifier policy lacks explicit non-mutation and shell-isolation limits")
	}
	for _, care := range []string{"care-reviewer", "care-specialist", "care-challenger"} {
		role, ok := c.Role(care)
		if !ok || !strings.Contains(role.Instructions, "use its current tool and headers") {
			t.Fatalf("%s policy lacks current Codegraph guidance", care)
		}
	}
	general, ok := c.Role("general")
	if !ok || !strings.Contains(general.Instructions, "report changed paths and the evidence actually obtained and its limits") {
		t.Fatal("general policy lacks changed-path evidence")
	}
	if strings.Contains(general.Instructions, "report changed paths, RED/GREEN evidence") {
		t.Fatal("general policy requires obsolete RED/GREEN reporting")
	}
}
