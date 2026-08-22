package opencode

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReadinessPolicyEvaluationAssets(t *testing.T) {
	for _, name := range []string{"readiness-policy-dev-cases.json", "readiness-policy-holdout-cases.json"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var asset map[string]any
		if err := json.Unmarshal(data, &asset); err != nil {
			t.Fatal(err)
		}
		if asset["partition"] == "" || asset["cases"] == nil {
			t.Fatalf("invalid %s", name)
		}
	}
}
