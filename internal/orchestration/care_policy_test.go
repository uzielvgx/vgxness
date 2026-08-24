package orchestration

import (
	"reflect"
	"testing"
)

func TestCAREPolicyExposesActivationDecision(t *testing.T) {
	fn := reflect.ValueOf(CAREActivation)
	if fn.Kind() != reflect.Func || fn.Type().NumOut() != 2 || !fn.Type().Out(1).Implements(reflect.TypeFor[error]()) {
		t.Fatalf("CAREActivation signature = %s, want an activation decision and error", fn.Type())
	}
	decision := fn.Type().Out(0)
	_, hasLevel := decision.FieldByName("Level")
	_, hasReasons := decision.FieldByName("Reasons")
	if decision.Kind() != reflect.Struct || !hasLevel || !hasReasons {
		t.Fatalf("CAREActivation decision %s must contain Level and Reasons", decision)
	}
}
