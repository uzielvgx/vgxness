package orchestration

import (
	"strings"
	"testing"
)

func TestActiveContractRetiresSDD(t *testing.T) {
	c, err := LoadManagerContract()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range c.Roles {
		if strings.HasPrefix(r.ID, "sdd-") {
			t.Fatalf("retired worker remains active: %s", r.ID)
		}
	}
	if !c.CanWrite("general") || c.CanWrite("sdd-apply") {
		t.Fatal("ordinary writer authority required")
	}
}
