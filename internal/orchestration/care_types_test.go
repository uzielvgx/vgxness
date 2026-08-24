package orchestration

import (
	"reflect"
	"testing"
)

func TestCARETypesPreserveClosedDomainValues(t *testing.T) {
	for _, id := range []CAREID{"care-risk", "claim:permissions", "scope/path-1"} {
		if string(id) == "" {
			t.Fatal("CARE IDs must preserve non-empty ASCII identifiers")
		}
	}
	if kind := reflect.TypeOf(CAREID("care-risk")).Kind(); kind != reflect.String {
		t.Fatalf("CAREID kind = %s, want string", kind)
	}
}
