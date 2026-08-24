package orchestration

import (
	"reflect"
	"testing"
)

func TestCAREContractExposesTypedMission(t *testing.T) {
	mission := reflect.TypeOf(CAREMission{})
	for _, field := range []string{"SchemaVersion", "Role", "MissionID", "ReplayNonce", "ReviewBinding", "CandidateIdentity", "CatalogRef", "ChangeProfileDigest", "AssurancePlanDigest", "Assignment", "Skills", "EvidenceScope", "CorrectionDelta"} {
		if _, ok := mission.FieldByName(field); !ok {
			t.Errorf("CAREMission lacks required contract field %q", field)
		}
	}
}
