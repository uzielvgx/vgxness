package sdd

import (
	"errors"
	"testing"
)

func FuzzParseOpenSpecProjection(f *testing.F) {
	revision := acceptedProjectionRevision(PhaseProposal, "proposal")
	document, err := RenderOpenSpecProjection(revision)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(document.Content)
	f.Add([]byte("proposal only"))
	f.Add([]byte(openSpecHeaderPrefix))

	f.Fuzz(func(t *testing.T, input []byte) {
		parsed, err := ParseOpenSpecProjection(input)
		if err != nil {
			if !errors.Is(err, ErrMalformedProjection) && !errors.Is(err, ErrProjectionTooLarge) && !errors.Is(err, ErrDigestMismatch) {
				t.Fatalf("unexpected parse error: %v", err)
			}
			return
		}
		if !safeProjectionID(parsed.Metadata.ChangeID) || !safeProjectionID(parsed.Metadata.RevisionID) || !parsed.Metadata.Artifact.Valid() || parsed.Metadata.Artifact == PhaseComplete || !parsed.Metadata.ContentDigest.Valid() || !parsed.Metadata.InputDigest.Valid() {
			t.Fatalf("accepted unsafe projection: %+v", parsed.Metadata)
		}
		if ContentDigest(parsed.Content) != parsed.Metadata.ContentDigest {
			t.Fatal("accepted projection with invalid content digest")
		}
	})
}
