package sdd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func acceptedProjectionRevision(artifact Phase, content string) Revision {
	inputs := []RevisionBinding{{ArtifactID: "artifact-input", RevisionID: "revision-input", Digest: ContentDigest([]byte("input"))}}
	return Revision{
		ID: "revision-1", ChangeID: "change-1", ArtifactID: "artifact-1", Artifact: artifact,
		Status: RevisionAccepted, Content: []byte(content), Digest: ContentDigest([]byte(content)),
		Inputs: inputs, InputDigest: InputRevisionDigest(inputs),
	}
}

func TestOpenSpecProjectionRoundTripAndPaths(t *testing.T) {
	paths := map[Phase]string{
		PhaseExplore: "research.md", PhaseProposal: "proposal.md", PhaseSpec: "spec.md",
		PhaseDesign: "design.md", PhaseTasks: "tasks.md", PhaseApply: "apply-result.md", PhaseVerify: "verification.md",
	}
	for artifact, name := range paths {
		revision := acceptedProjectionRevision(artifact, "# Content\n")
		document, err := RenderOpenSpecProjection(revision)
		if err != nil {
			t.Fatalf("render %s: %v", artifact, err)
		}
		wantPath := "openspec/changes/change-1/" + name
		if document.RelativePath != wantPath || document.Digest != ContentDigest(document.Content) {
			t.Fatalf("document=%+v wantPath=%q", document, wantPath)
		}
		parsed, err := ParseOpenSpecProjection(document.Content)
		if err != nil {
			t.Fatalf("parse %s: %v", artifact, err)
		}
		if parsed.Metadata.ChangeID != revision.ChangeID || parsed.Metadata.Artifact != artifact || parsed.Metadata.RevisionID != revision.ID || !bytes.Equal(parsed.Content, revision.Content) {
			t.Fatalf("round trip mismatch: %+v", parsed)
		}
	}
}

func TestOpenSpecProjectionRejectsTamperMalformedAndDuplicateMetadata(t *testing.T) {
	revision := acceptedProjectionRevision(PhaseProposal, "proposal")
	document, err := RenderOpenSpecProjection(revision)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), document.Content...)
	tampered[len(tampered)-1] = 'x'
	if _, err = ParseOpenSpecProjection(tampered); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("tamper error=%v", err)
	}
	duplicate := bytes.Replace(document.Content, []byte("changeId: change-1\n"), []byte("changeId: change-1\nchangeId: change-1\n"), 1)
	if _, err = ParseOpenSpecProjection(duplicate); !errors.Is(err, ErrMalformedProjection) {
		t.Fatalf("duplicate metadata error=%v", err)
	}
	if _, err = ParseOpenSpecProjection([]byte("proposal only")); !errors.Is(err, ErrMalformedProjection) {
		t.Fatalf("malformed error=%v", err)
	}
}

func TestOpenSpecProjectionRejectsUnsafePathSizeAndWrongIdentity(t *testing.T) {
	for _, changeID := range []string{"", "../change", "/absolute", "change/child", ".", "change\\child"} {
		revision := acceptedProjectionRevision(PhaseProposal, "proposal")
		revision.ChangeID = changeID
		if _, err := RenderOpenSpecProjection(revision); !errors.Is(err, ErrUnsafeProjectionPath) {
			t.Fatalf("change %q error=%v", changeID, err)
		}
	}
	oversized := acceptedProjectionRevision(PhaseProposal, strings.Repeat("x", MaxOpenSpecContentBytes+1))
	if _, err := RenderOpenSpecProjection(oversized); !errors.Is(err, ErrProjectionTooLarge) {
		t.Fatalf("oversized error=%v", err)
	}

	revision := acceptedProjectionRevision(PhaseProposal, "proposal")
	document, _ := RenderOpenSpecProjection(revision)
	wrongPath := ProjectionInput{RelativePath: "openspec/changes/change-1/spec.md", Content: document.Content}
	if _, err := InspectOpenSpecProjection(revision, wrongPath); !errors.Is(err, ErrProjectionIdentity) {
		t.Fatalf("wrong path error=%v", err)
	}
	for _, unsafe := range []string{"/openspec/changes/change-1/proposal.md", "openspec/changes/../proposal.md", "openspec\\changes\\change-1\\proposal.md"} {
		if _, err := InspectOpenSpecProjection(revision, ProjectionInput{RelativePath: unsafe, Content: document.Content}); !errors.Is(err, ErrUnsafeProjectionPath) {
			t.Fatalf("unsafe input path %q error=%v", unsafe, err)
		}
	}
	symlink := ProjectionInput{RelativePath: document.RelativePath, Content: document.Content, Symlink: true}
	if _, err := InspectOpenSpecProjection(revision, symlink); !errors.Is(err, ErrUnsafeProjectionPath) {
		t.Fatalf("symlink error=%v", err)
	}

	for name, mutate := range map[string]func(*Revision){
		"change":   func(value *Revision) { value.ChangeID = "change-other" },
		"artifact": func(value *Revision) { value.Artifact = PhaseSpec },
		"revision": func(value *Revision) { value.ID = "revision-other" },
	} {
		observed := revision
		mutate(&observed)
		wrong, renderErr := RenderOpenSpecProjection(observed)
		if renderErr != nil {
			t.Fatalf("render wrong %s: %v", name, renderErr)
		}
		if _, err := InspectOpenSpecProjection(revision, ProjectionInput{RelativePath: document.RelativePath, Content: wrong.Content}); !errors.Is(err, ErrProjectionIdentity) {
			t.Fatalf("wrong %s error=%v", name, err)
		}
	}
	wrongInputs := revision
	wrongInputs.Inputs = []RevisionBinding{{ArtifactID: "artifact-other", RevisionID: "revision-other", Digest: ContentDigest([]byte("other"))}}
	wrongInputs.InputDigest = InputRevisionDigest(wrongInputs.Inputs)
	wrongInputDocument, err := RenderOpenSpecProjection(wrongInputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectOpenSpecProjection(revision, ProjectionInput{RelativePath: document.RelativePath, Content: wrongInputDocument.Content}); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("wrong inputs error=%v", err)
	}
}

func TestCompareOpenSpecProjectionStatesAndReconciliation(t *testing.T) {
	revision := acceptedProjectionRevision(PhaseDesign, "design v1")
	document, _ := RenderOpenSpecProjection(revision)
	synced, err := CompareOpenSpecProjection(revision, BackendHybrid, ProjectionInput{RelativePath: document.RelativePath, Content: document.Content})
	if err != nil || synced.State != DriftSynced || len(synced.Options) != 0 || !synced.MemoryCanonical {
		t.Fatalf("synced=%+v err=%v", synced, err)
	}
	missing, err := CompareOpenSpecProjection(revision, BackendHybrid, ProjectionInput{RelativePath: document.RelativePath, Missing: true})
	if err != nil || missing.State != DriftMissing || len(missing.Options) != 1 || missing.Options[0] != ReconcileRenderCanonical {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}

	divergentRevision := revision
	divergentRevision.Content = []byte("design edited in OpenSpec")
	divergentRevision.Digest = ContentDigest(divergentRevision.Content)
	divergent, _ := RenderOpenSpecProjection(divergentRevision)
	drifted, err := CompareOpenSpecProjection(revision, BackendHybrid, ProjectionInput{RelativePath: divergent.RelativePath, Content: divergent.Content})
	if err != nil || drifted.State != DriftDrifted || len(drifted.Options) != 3 || !drifted.RequiresSaveRevision || !bytes.Equal(drifted.CandidateContent, divergentRevision.Content) {
		t.Fatalf("drifted=%+v err=%v", drifted, err)
	}
	if drifted.Options[2] != ReconcileSaveCandidate {
		t.Fatalf("drift options=%v", drifted.Options)
	}
}

func TestRenderProjectionRequiresAcceptedSelfConsistentRevision(t *testing.T) {
	revision := acceptedProjectionRevision(PhaseProposal, "proposal")
	for name, mutate := range map[string]func(*Revision){
		"candidate":         func(value *Revision) { value.Status = RevisionCandidate },
		"content digest":    func(value *Revision) { value.Digest = ContentDigest([]byte("other")) },
		"input digest":      func(value *Revision) { value.InputDigest = ContentDigest([]byte("other")) },
		"complete artifact": func(value *Revision) { value.Artifact = PhaseComplete },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := revision
			mutate(&invalid)
			if _, err := RenderOpenSpecProjection(invalid); err == nil {
				t.Fatal("invalid revision rendered")
			}
		})
	}
}
