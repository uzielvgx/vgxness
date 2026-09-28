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
		Cases []struct{ ID, Fragment, Expect string }
	}
	if e = json.Unmarshal(raw, &corpus); e != nil {
		t.Fatal(e)
	}
	p, e := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if e != nil {
		t.Fatal(e)
	}
	text := string(p.agents[managerAgentName])
	if len(corpus.Cases) != 38 {
		t.Fatal("missing scenarios")
	}
	for _, scenario := range corpus.Cases {
		if scenario.Expect == "absent" {
			if strings.Contains(text, scenario.Fragment) {
				t.Errorf("native projection retains forbidden scenario %s", scenario.ID)
			}
			continue
		}
		if !strings.Contains(text, scenario.Fragment) {
			t.Errorf("native projection lacks scenario %s", scenario.ID)
		}
	}
}
