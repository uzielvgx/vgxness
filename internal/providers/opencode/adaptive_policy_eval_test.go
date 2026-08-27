package opencode

import (
	"encoding/json"
	"testing"

	"github.com/vgxness/vgxness/internal/orchestration"
)

type adaptiveEvalAsset struct {
	SchemaVersion int                `json:"schema_version"`
	Target        string             `json:"target"`
	Partition     string             `json:"partition"`
	Grading       string             `json:"grading"`
	Scope         string             `json:"evaluation_scope"`
	LabelStatus   string             `json:"label_status"`
	Cases         []adaptiveEvalCase `json:"cases"`
}

type adaptiveEvalCase struct {
	ID              string                         `json:"id"`
	Category        string                         `json:"category"`
	Request         string                         `json:"request"`
	Observables     []string                       `json:"observable_behavior"`
	Classification  orchestration.Classification   `json:"classification"`
	MemoryIntent    orchestration.MemoryIntent     `json:"memory_intent"`
	MemoryCandidate orchestration.MemoryCandidate  `json:"memory_candidate"`
	ExpectedPolicy  *orchestration.ExecutionPolicy `json:"expected_policy"`
	ExpectedMemory  *orchestration.MemoryPolicy    `json:"expected_memory"`
	ExpectInvalid   bool                           `json:"expect_invalid"`
}

func TestAdaptivePolicyEvaluationAssets(t *testing.T) {
	assets := []struct {
		name, partition string
		ids             []string
	}{
		{"adaptive-policy-dev-cases.json", "development", []string{"dev-long-trip-plan", "dev-email-draft", "dev-durable-preference-save", "dev-terminal-handoff-save", "dev-transient-chat-non-save", "dev-knowledge-lookup", "dev-complex-research", "dev-bounded-repository-read", "dev-repository-diagnosis", "dev-repository-edit"}},
		{"adaptive-policy-holdout-cases.json", "holdout", []string{"holdout-001", "holdout-002", "holdout-003"}},
	}
	seenIDs, categories := map[string]bool{}, map[string]bool{}
	for _, spec := range assets {
		data := []byte(readContextEvalFile(t, "testdata/"+spec.name))
		var asset adaptiveEvalAsset
		var raw struct {
			Cases []map[string]json.RawMessage `json:"cases"`
		}
		if err := json.Unmarshal(data, &asset); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if asset.SchemaVersion != 1 || asset.Target != "provider-neutral adaptive-policy/v1" || asset.Partition != spec.partition || len(asset.Cases) != len(spec.ids) || len(raw.Cases) != len(asset.Cases) {
			t.Fatalf("invalid %s asset identity: %+v", spec.partition, asset)
		}
		if spec.partition == "development" && (asset.Grading != "deterministic-assertions; no-model-self-grading" || asset.Scope != "policy assertions only; requests are not NLP results" || asset.LabelStatus != "labeled-development") {
			t.Fatalf("invalid development evaluation contract: %+v", asset)
		}
		if spec.partition == "holdout" && (asset.Grading != "external-independent-pending" || asset.Scope != "structure only; no in-repository adjudication" || asset.LabelStatus != "external independent labels and runs pending") {
			t.Fatalf("invalid holdout evaluation contract: %+v", asset)
		}
		for index, testCase := range asset.Cases {
			if !contains(spec.ids, testCase.ID) || seenIDs[testCase.ID] || testCase.Request == "" {
				t.Fatalf("invalid or duplicate adaptive case: %+v", testCase)
			}
			seenIDs[testCase.ID] = true
			if spec.partition == "holdout" {
				assertUnlabeledHoldoutCase(t, testCase, raw.Cases[index])
				continue
			}
			if testCase.Category == "" || len(testCase.Observables) != 0 || testCase.ExpectedPolicy == nil && testCase.ExpectedMemory == nil {
				t.Fatalf("invalid labeled development case: %+v", testCase)
			}
			categories[testCase.Category] = true
			if testCase.ExpectedPolicy != nil {
				assertRawKeys(t, raw.Cases[index]["expected_policy"], "route", "max_tools", "max_delegations", "use_todo", "authorization", "verification", "budget_accounting", "on_exhaustion")
				got, err := orchestration.PolicyFor(testCase.Classification)
				if (err != nil) != testCase.ExpectInvalid || got != *testCase.ExpectedPolicy {
					t.Errorf("case %q policy = %+v, %v; want %+v invalid=%t", testCase.ID, got, err, *testCase.ExpectedPolicy, testCase.ExpectInvalid)
				}
			}
			if testCase.ExpectedMemory != nil {
				assertRawKeys(t, raw.Cases[index]["expected_memory"], "decision", "max_tools", "autonomous", "automatic_cloud_sync", "requires_engineering")
				got, err := orchestration.MemoryPolicyFor(testCase.MemoryIntent, testCase.MemoryCandidate)
				if err != nil || got != *testCase.ExpectedMemory {
					t.Errorf("case %q memory = %+v, %v; want %+v", testCase.ID, got, err, *testCase.ExpectedMemory)
				}
			}
		}
	}
	for _, category := range []string{"daily-planning", "writing", "durable-memory", "terminal-memory", "transient-memory", "research", "repository"} {
		if !categories[category] {
			t.Errorf("development evaluation lacks %q coverage", category)
		}
	}
}

func assertUnlabeledHoldoutCase(t *testing.T, testCase adaptiveEvalCase, raw map[string]json.RawMessage) {
	t.Helper()
	if testCase.Category != "" || len(testCase.Observables) != 0 || testCase.ExpectedPolicy != nil || testCase.ExpectedMemory != nil || testCase.ExpectInvalid || testCase.Classification != (orchestration.Classification{}) || testCase.MemoryIntent != "" || testCase.MemoryCandidate != (orchestration.MemoryCandidate{}) {
		t.Fatalf("holdout case contains in-repository labels: %+v", testCase)
	}
	for _, key := range []string{"category", "observable_behavior", "classification", "memory_intent", "memory_candidate", "expected_policy", "expected_memory", "expect_invalid"} {
		if _, present := raw[key]; present {
			t.Errorf("holdout case %q discloses label field %q", testCase.ID, key)
		}
	}
}

func assertRawKeys(t *testing.T, data json.RawMessage, keys ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if len(data) == 0 || json.Unmarshal(data, &object) != nil {
		t.Fatalf("missing or malformed labeled policy: %s", data)
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Errorf("labeled policy omits required control %q: %s", key, data)
		}
	}
	if object == nil {
		t.Fatal("labeled policy must be an object")
	}
}
