package sdd

import "testing"

func TestPiTypeScriptModelSlotsAndInputDigest(t *testing.T) {
	inputs := []RevisionBinding{{ArtifactID: "b", RevisionID: "2", Digest: ContentDigest([]byte("b"))}, {ArtifactID: "a", RevisionID: "1", Digest: ContentDigest([]byte("a"))}}
	if got := InputRevisionDigest(inputs); !got.Valid() {
		t.Fatal("input digest must be a lowercase SHA-256 digest")
	}
	resolved, err := ResolveModelPlan(Catalog{Provider: "p", Models: []Model{{Provider: "p", ID: "one", Name: "One", Capability: CapabilityFrontier, SupportedEfforts: []Effort{EffortLow}}}}, PlanLow)
	if err != nil || resolved.Slots[CapabilityEfficient].ID != "one" || resolved.Slots[CapabilityFrontier].ID != "one" {
		t.Fatalf("unexpected resolver result: %#v, %v", resolved, err)
	}
}
