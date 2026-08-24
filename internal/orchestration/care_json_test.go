package orchestration

import (
	"reflect"
	"testing"
)

func TestCAREJSONExposesCanonicalEncoder(t *testing.T) {
	fn := reflect.ValueOf(CanonicalCARE)
	if fn.Kind() != reflect.Func || fn.Type().NumIn() != 1 || fn.Type().NumOut() != 2 {
		t.Fatalf("CanonicalCARE signature = %s, want one value and bytes/error results", fn.Type())
	}
	if fn.Type().Out(0).Kind() != reflect.Slice || fn.Type().Out(0).Elem().Kind() != reflect.Uint8 || !fn.Type().Out(1).Implements(reflect.TypeFor[error]()) {
		t.Fatalf("CanonicalCARE signature = %s, want ([]byte, error)", fn.Type())
	}
}
