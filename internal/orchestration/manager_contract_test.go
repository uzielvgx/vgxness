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
		"Direct read-only work and trivial local changes that are low risk may proceed with pertinent inspection or checks, without a formal plan, delegation, RED, or CARE ritual.",
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
	general, ok := c.Role("general")
	if !ok || !strings.Contains(general.Instructions, "report changed paths and the evidence actually obtained and its limits") {
		t.Fatal("general policy lacks changed-path evidence")
	}
	if strings.Contains(general.Instructions, "report changed paths, RED/GREEN evidence") {
		t.Fatal("general policy requires obsolete RED/GREEN reporting")
	}
}
