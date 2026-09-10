package opencode

import (
	"encoding/json"
	"github.com/vgxness/vgxness/internal/modelplan"

	"os"
	"strings"
	"testing"
)

func TestNativeSharedDevelopmentScenarios(t *testing.T) {
	raw, e := os.ReadFile("../../orchestration/testdata/manager-scenarios.json")
	if e != nil {
		t.Fatal(e)
	}
	var corpus struct {
		Cases []struct{ ID, Fragment string }
	}
	if e = json.Unmarshal(raw, &corpus); e != nil {
		t.Fatal(e)
	}
	p, e := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if e != nil {
		t.Fatal(e)
	}
	text := string(p.agents[managerAgentName])
	if len(corpus.Cases) != 11 {
		t.Fatal("missing scenarios")
	}
	for _, scenario := range corpus.Cases {
		if !strings.Contains(text, scenario.Fragment) {
			t.Errorf("native projection lacks scenario %s", scenario.ID)
		}
	}
}
