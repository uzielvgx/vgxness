package orchestration

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSharedManagerDevelopmentScenarios(t *testing.T) {
	raw, e := os.ReadFile("testdata/manager-scenarios.json")
	if e != nil {
		t.Fatal(e)
	}
	var corpus struct {
		SchemaVersion string
		EvidenceKind  string
		Partition     string
		Cases         []struct{ ID, Role, Fragment string }
	}
	if e = json.Unmarshal(raw, &corpus); e != nil {
		t.Fatal(e)
	}
	if corpus.SchemaVersion != "vgxness-manager-scenarios/v1" || corpus.EvidenceKind != "deterministic-contract-conformance" || corpus.Partition != "development" || len(corpus.Cases) != 11 {
		t.Fatal("invalid development corpus")
	}
	c, e := LoadManagerContract()
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, scenario := range corpus.Cases {
		r, ok := c.Role(scenario.Role)
		if !ok || seen[scenario.ID] || scenario.ID == "" || scenario.Fragment == "" {
			t.Fatal("invalid scenario")
		}
		seen[scenario.ID] = true
		if !strings.Contains(r.Instructions, scenario.Fragment) {
			t.Errorf("%s policy absent from %s", scenario.ID, scenario.Role)
		}
	}
}
