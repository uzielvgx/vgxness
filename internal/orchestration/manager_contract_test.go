package orchestration

import "testing"

func TestManagerContractIsCanonicalAndComplete(t *testing.T) {
	c, err := LoadManagerContract()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Roles) != 12 || ManagerContractDigest() == "" {
		t.Fatal("unexpected registry")
	}
	for _, name := range []string{"explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger", "sdd-research", "sdd-proposal", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply"} {
		if _, ok := c.Role(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}
	if !c.CanWrite("general") || !c.CanWrite("sdd-apply") || c.CanWrite("explore") {
		t.Fatal("write authority drift")
	}
}
