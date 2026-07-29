package sdd

import (
	"errors"
	"testing"
)

func TestBoundedValues(t *testing.T) {
	tests := []struct {
		name  string
		valid interface{ Valid() bool }
		want  bool
	}{
		{"backend", BackendHybrid, true},
		{"backend unknown", Backend("other"), false},
		{"interaction", InteractionInteractive, true},
		{"phase", PhaseVerify, true},
		{"change status", ChangeActive, true},
		{"artifact status", ArtifactStale, true},
		{"revision status", RevisionAccepted, true},
		{"projection status", ProjectionDrift, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.valid.Valid(); got != test.want {
				t.Fatalf("Valid() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidatePhaseTransition(t *testing.T) {
	tests := []struct {
		from, to Phase
		valid    bool
	}{
		{PhaseExplore, PhaseProposal, true},
		{PhaseProposal, PhaseSpec, true},
		{PhaseSpec, PhaseDesign, true},
		{PhaseDesign, PhaseTasks, true},
		{PhaseTasks, PhaseApply, true},
		{PhaseApply, PhaseVerify, true},
		{PhaseVerify, PhaseComplete, true},
		{PhaseExplore, PhaseSpec, false},
		{PhaseVerify, PhaseApply, false},
		{PhaseComplete, PhaseComplete, false},
	}
	for _, test := range tests {
		err := ValidatePhaseTransition(test.from, test.to)
		if test.valid && err != nil {
			t.Fatalf("%s -> %s rejected: %v", test.from, test.to, err)
		}
		if !test.valid && !errors.Is(err, ErrIllegalTransition) {
			t.Fatalf("%s -> %s error = %v, want illegal transition", test.from, test.to, err)
		}
	}
}

func TestDownstreamPhase(t *testing.T) {
	if !IsDownstream(PhaseSpec, PhaseDesign) || !IsDownstream(PhaseSpec, PhaseVerify) {
		t.Fatal("later phases must be downstream")
	}
	if IsDownstream(PhaseSpec, PhaseProposal) || IsDownstream(PhaseSpec, PhaseSpec) {
		t.Fatal("earlier or equal phases must not be downstream")
	}
}

func TestDigestsAreCanonicalAndBound(t *testing.T) {
	content := []byte("proposal\n")
	digest := ContentDigest(content)
	if !digest.Valid() || digest != ContentDigest(append([]byte(nil), content...)) {
		t.Fatalf("invalid stable content digest %q", digest)
	}
	a := RevisionBinding{ArtifactID: "artifact-a", RevisionID: "revision-a", Digest: digest}
	b := RevisionBinding{ArtifactID: "artifact-b", RevisionID: "revision-b", Digest: ContentDigest([]byte("b"))}
	if InputRevisionDigest([]RevisionBinding{a, b}) != InputRevisionDigest([]RevisionBinding{b, a}) {
		t.Fatal("input digest must be order independent")
	}
	changed := b
	changed.RevisionID = "revision-c"
	if InputRevisionDigest([]RevisionBinding{a, b}) == InputRevisionDigest([]RevisionBinding{a, changed}) {
		t.Fatal("input digest did not bind the revision identity")
	}
}
