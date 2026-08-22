package orchestration

import (
	"bytes"
	"testing"
)

func TestCanonicalJSONAcceptsOnlyBoundedUnambiguousJSON(t *testing.T) {
	got, err := CanonicalJSON([]byte(`{"b":[2,1],"a":1}`))
	if err != nil || !bytes.Equal(got, []byte(`{"a":1,"b":[2,1]}`)) {
		t.Fatalf("canonical = %q, %v", got, err)
	}
	for _, bad := range [][]byte{nil, []byte(`{"a":1,"a":2}`), []byte(`{} {}`), []byte(`{"a":}`), []byte{0xff}, bytes.Repeat([]byte("x"), maxCanonicalBytes+1)} {
		if _, err := CanonicalJSON(bad); err == nil {
			t.Fatalf("accepted invalid canonical input %q", bad)
		}
	}
}

func TestCanonicalEnvelopeDigestBindsEveryFieldAndOmitsItself(t *testing.T) {
	in := completeBuildInput()
	e, _ := BuildReadiness(in)
	before := EnvelopeDigest(e)
	for _, mutate := range []func(*ReadinessEnvelope){
		func(v *ReadinessEnvelope) { v.SchemaVersion = "other" }, func(v *ReadinessEnvelope) { v.Activation = ActivationLight },
		func(v *ReadinessEnvelope) { v.Status = ReadinessBlocked }, func(v *ReadinessEnvelope) { v.MissionEvidence = []byte(`{"x":1}`) },
		func(v *ReadinessEnvelope) { v.Binding.TaskDigest = digest64('z') }, func(v *ReadinessEnvelope) { v.Scope.Paths[0] = "other.go" },
		func(v *ReadinessEnvelope) { v.Scope.Criteria[0] = "other" }, func(v *ReadinessEnvelope) { v.Scope.AuthorizationReference = "other" },
		func(v *ReadinessEnvelope) { v.Scope.PermittedValidation[0] = "other" }, func(v *ReadinessEnvelope) { v.Scope.Targets[0].NoSymlink = false },
		func(v *ReadinessEnvelope) { v.Evidence[0].ObservedResult = "other" }, func(v *ReadinessEnvelope) { v.Unknowns = []Unknown{{Question: "q"}} },
		func(v *ReadinessEnvelope) { v.Dependencies[0].Digest = digest64('z') }, func(v *ReadinessEnvelope) { v.RiskCategories[0] = "provider-template" },
	} {
		copy := e
		copy.Binding.Inputs = append([]AcceptedInput(nil), e.Binding.Inputs...)
		copy.Scope.Paths = append([]string(nil), e.Scope.Paths...)
		copy.Scope.Criteria = append([]string(nil), e.Scope.Criteria...)
		copy.Scope.PermittedValidation = append([]string(nil), e.Scope.PermittedValidation...)
		copy.Scope.Targets = append([]TargetIdentity(nil), e.Scope.Targets...)
		copy.Evidence = append([]EvidenceReceipt(nil), e.Evidence...)
		copy.Dependencies = append([]Dependency(nil), e.Dependencies...)
		copy.RiskCategories = append([]RiskCategory(nil), e.RiskCategories...)
		mutate(&copy)
		if after := EnvelopeDigest(copy); after == before {
			t.Fatal("bound field mutation did not change digest")
		}
	}
	canonical, err := CanonicalValue(e)
	if err != nil || bytes.Contains(canonical, []byte(`"envelopeDigest"`)) {
		t.Fatalf("digest field was included in its own hash: %s, %v", canonical, err)
	}
}
