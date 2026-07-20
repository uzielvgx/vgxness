package contracts

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSchema_MemoryWriteRecords(t *testing.T) {
	for _, source := range []struct{ file, definition string }{{"run.schema.json", "memoryWrite"}, {"run-event.schema.json", "memoryWriteRef"}} {
		data, err := os.ReadFile("../../docs/schemas/" + source.file)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Defs map[string]struct {
				Properties map[string]struct {
					Type      string `json:"type"`
					MinLength int    `json:"minLength"`
					Const     string `json:"const"`
				} `json:"properties"`
			} `json:"$defs"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		contract := document.Defs[source.definition]
		accepts := func(id, backend string) bool {
			return contract.Properties["id"].Type == "string" && len(id) >= contract.Properties["id"].MinLength && backend == contract.Properties["backend"].Const
		}
		for _, record := range []struct {
			name, id, backend string
			want              bool
		}{{"variable id", "obs-17", "memory", true}, {"legacy backend", "obs-17", "engram", false}, {"empty id", "", "memory", false}} {
			if got := accepts(record.id, record.backend); got != record.want {
				t.Errorf("%s/%s: got %t, want %t", source.file, record.name, got, record.want)
			}
		}
	}
	legacyErr := ValidateOwnedMemoryBackend("engram")
	if ValidateOwnedMemoryBackend("memory") != nil || legacyErr == nil || !strings.Contains(legacyErr.Error(), `use "memory"`) || ValidateExternalReference("engram", "obs-1") != nil {
		t.Fatalf("provider-neutral backend contract failed: %v", legacyErr)
	}
}
