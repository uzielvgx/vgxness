package orchestration

import (
	"reflect"
	"testing"
)

func TestCARECatalogIsGovernedAndNonEmpty(t *testing.T) {
	fn := reflect.ValueOf(GovernedRiskCatalog)
	if fn.Kind() != reflect.Func || fn.Type().NumIn() != 0 || fn.Type().NumOut() != 2 || !fn.Type().Out(1).Implements(reflect.TypeFor[error]()) {
		t.Fatalf("GovernedRiskCatalog signature = %s, want func() (RiskCatalog, error)", fn.Type())
	}
	result := fn.Call(nil)
	if !result[1].IsNil() {
		t.Fatalf("GovernedRiskCatalog() error = %v", result[1].Interface())
	}
	catalog := result[0]
	if catalog.Kind() == reflect.Pointer {
		catalog = catalog.Elem()
	}
	if catalog.Kind() != reflect.Struct || catalog.FieldByName("CatalogID").String() == "" || catalog.FieldByName("Risks").Len() == 0 {
		t.Fatalf("governed catalog must have an ID and at least one risk: %#v", result[0].Interface())
	}
}
