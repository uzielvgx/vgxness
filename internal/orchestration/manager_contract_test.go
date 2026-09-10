package orchestration

import "testing"

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
