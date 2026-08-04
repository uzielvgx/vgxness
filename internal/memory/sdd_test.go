package memory

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestMigrateV4ToV5PreservesMemoryAndAddsIsolatedSDD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(schemaV1 + schemaV2 + schemaV3 + schemaV4 + `
		PRAGMA user_version=4;
		INSERT INTO projects(id) VALUES('project-a');
		INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,title)
		VALUES('obs-old','project-a','project','learning','preserved token','test','active',1,1,'Preserved');
		INSERT INTO observations_fts(id,content) VALUES('obs-old','preserved token');`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())

	store := openPath(t, path)
	defer store.Close()
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 6, "health=%d err=%v", version, err)
	found, err := store.Search(context.Background(), Search{Project: "project-a", Query: "preserved"})
	testutil.Require(t, err == nil && len(found) == 1 && found[0].ID == "obs-old", "memory changed: %+v %v", found, err)
	var tables int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN ('sdd_changes','sdd_artifacts','sdd_revisions','sdd_revision_links','sdd_projections')`).Scan(&tables))
	testutil.Require(t, tables == 5, "SDD tables=%d", tables)
}

func TestSDDRepositoryLifecycleIsolationAndSummaryListing(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "feature-1", Title: "Feature", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionInteractive, Plan: sdd.PlanHigh})
	testutil.NoError(t, err)
	if change.StateVersion != 1 || change.Phase != sdd.PhaseExplore {
		t.Fatalf("unexpected change: %+v", change)
	}

	research, err := store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseExplore, Content: []byte("research body"), ExpectedStateVersion: 1})
	testutil.NoError(t, err)
	research, err = store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: research.ID, ExpectedStateVersion: research.StateVersion})
	testutil.NoError(t, err)
	if research.Status != sdd.RevisionAccepted || research.StateVersion != 3 {
		t.Fatalf("research not accepted: %+v", research)
	}
	revisions, err := store.ListRevisions(ctx, sdd.ListRevisionsRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseExplore})
	testutil.NoError(t, err)
	if len(revisions) != 1 || len(revisions[0].Content) != 0 || revisions[0].Digest != research.Digest {
		t.Fatalf("revision list is not summary-only: %+v", revisions)
	}
	loaded, err := store.GetRevision(ctx, sdd.GetRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: research.ID})
	testutil.NoError(t, err)
	if string(loaded.Content) != "research body" {
		t.Fatalf("full revision body missing: %+v", loaded)
	}

	observations, err := store.Recent(ctx, Recent{Project: "project-a", Scope: ScopeProject})
	testutil.Require(t, err == nil && len(observations) == 0, "SDD leaked into recent: %+v %v", observations, err)
	observations, err = store.Search(ctx, Search{Project: "project-a", Query: "proposal"})
	testutil.Require(t, err == nil && len(observations) == 0, "SDD leaked into search: %+v %v", observations, err)
	_, err = store.Forget(ctx, research.ID, "project-a", ScopeProject)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("memory forget reached SDD revision: %v", err)
	}
}

func TestSDDRepositoryRejectsInvalidConcurrencyAndBindings(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "feature-2", Title: "Feature", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanLow})
	testutil.NoError(t, err)
	_, err = store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseProposal, Content: []byte("content"), Digest: sdd.ContentDigest([]byte("wrong")), ExpectedStateVersion: 1})
	if !errors.Is(err, sdd.ErrConflict) {
		t.Fatalf("future phase error=%v", err)
	}
	_, err = store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseExplore, Content: []byte("content"), Digest: sdd.ContentDigest([]byte("wrong")), ExpectedStateVersion: 1})
	if !errors.Is(err, sdd.ErrDigestMismatch) {
		t.Fatalf("digest mismatch error=%v", err)
	}
	revision, err := store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseExplore, Content: []byte("content"), ExpectedStateVersion: 1})
	testutil.NoError(t, err)
	_, err = store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: revision.ID, ExpectedStateVersion: 1})
	if !errors.Is(err, sdd.ErrStaleState) {
		t.Fatalf("stale state error=%v", err)
	}
	revision, err = store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: revision.ID, ExpectedStateVersion: 2})
	testutil.NoError(t, err)

	change, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: revision.StateVersion})
	testutil.NoError(t, err)
	badInput := sdd.RevisionBinding{ArtifactID: revision.ArtifactID, RevisionID: revision.ID, Digest: sdd.ContentDigest([]byte("changed"))}
	_, err = store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseProposal, Content: []byte("proposal"), Inputs: []sdd.RevisionBinding{badInput}, ExpectedStateVersion: change.StateVersion})
	if !errors.Is(err, sdd.ErrInputsChanged) {
		t.Fatalf("changed inputs error=%v", err)
	}

	_, err = store.GetChange(ctx, sdd.GetChangeRequest{Project: "project-b", ID: change.ID})
	if !errors.Is(err, sdd.ErrNotFound) {
		t.Fatalf("project isolation error=%v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.ListChanges(cancelled, sdd.ListChangesRequest{Project: "project-a"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestSDDRepositoryTransitionsAndProjection(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "feature-3", Title: "Feature", Backend: sdd.BackendHybrid, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium})
	testutil.NoError(t, err)
	_, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, TargetPhase: sdd.PhaseSpec, ExpectedStateVersion: 1})
	if !errors.Is(err, sdd.ErrIllegalTransition) {
		t.Fatalf("illegal transition error=%v", err)
	}
	research, err := store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseExplore, Content: []byte("research"), ExpectedStateVersion: 1})
	testutil.NoError(t, err)
	research, err = store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: research.ID, ExpectedStateVersion: research.StateVersion})
	testutil.NoError(t, err)
	researchDocument, err := sdd.RenderOpenSpecProjection(research)
	testutil.NoError(t, err)
	researchProjection, err := store.RecordProjection(ctx, sdd.RecordProjectionRequest{Project: "project-a", ChangeID: change.ID, ArtifactID: research.ArtifactID, RevisionID: research.ID, Status: sdd.ProjectionCurrent, Digest: researchDocument.Digest, Location: researchDocument.RelativePath, ExpectedStateVersion: research.StateVersion})
	testutil.NoError(t, err)
	change, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: researchProjection.StateVersion})
	testutil.NoError(t, err)
	revision, err := store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseProposal, Content: []byte("proposal"), ExpectedStateVersion: change.StateVersion})
	testutil.NoError(t, err)
	revision, err = store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: revision.ID, ExpectedStateVersion: revision.StateVersion})
	testutil.NoError(t, err)
	missing, err := store.ProjectionStatus(ctx, sdd.ProjectionStatusRequest{Project: "project-a", ChangeID: change.ID, ArtifactID: revision.ArtifactID})
	testutil.NoError(t, err)
	if missing.Status != sdd.ProjectionAbsent {
		t.Fatalf("missing projection=%+v", missing)
	}
	document, err := sdd.RenderOpenSpecProjection(revision)
	testutil.NoError(t, err)
	projection, err := store.RecordProjection(ctx, sdd.RecordProjectionRequest{Project: "project-a", ChangeID: change.ID, ArtifactID: revision.ArtifactID, RevisionID: revision.ID, Status: sdd.ProjectionCurrent, Digest: document.Digest, Location: document.RelativePath, ExpectedStateVersion: revision.StateVersion})
	testutil.NoError(t, err)
	if projection.Status != sdd.ProjectionCurrent || projection.StateVersion != revision.StateVersion+1 {
		t.Fatalf("projection=%+v", projection)
	}
	change, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, Cancel: true, ExpectedStateVersion: projection.StateVersion})
	testutil.NoError(t, err)
	if change.Status != sdd.ChangeCancelled {
		t.Fatalf("cancelled change=%+v", change)
	}
}

func TestSDDRepositoryStoresOpenSpecRevisionsByExternalIdentityOnly(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "external-1", Title: "External", Backend: sdd.BackendOpenSpec, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium})
	testutil.NoError(t, err)
	location, err := sdd.OpenSpecProjectionPath(change.ID, sdd.PhaseExplore)
	testutil.NoError(t, err)
	content := []byte("external research")
	revision, err := store.SaveRevision(ctx, sdd.SaveRevisionRequest{
		Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseExplore,
		Content: content, Digest: sdd.ContentDigest(content), ExternalLocation: location, ExpectedStateVersion: 1,
	})
	testutil.NoError(t, err)
	if len(revision.Content) != 0 || revision.ExternalLocation != location || revision.Digest != sdd.ContentDigest(content) {
		t.Fatalf("external revision retained the wrong identity: %+v", revision)
	}
	var storedContent []byte
	var storedLocation sql.NullString
	testutil.NoError(t, store.db.QueryRow(`SELECT content,external_location FROM sdd_revisions WHERE id=?`, revision.ID).Scan(&storedContent, &storedLocation))
	if storedContent != nil || !storedLocation.Valid || storedLocation.String != location {
		t.Fatalf("external body was retained: content=%q location=%+v", storedContent, storedLocation)
	}
	loaded, err := store.GetRevision(ctx, sdd.GetRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: revision.ID})
	testutil.NoError(t, err)
	if len(loaded.Content) != 0 || loaded.ExternalLocation != location {
		t.Fatalf("loaded external revision=%+v", loaded)
	}
}

func TestSDDRepositoryUpdatesInteractionModeOptimistically(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "mode-1", Title: "Mode", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanLow})
	testutil.NoError(t, err)
	updated, err := store.UpdateInteractionMode(ctx, sdd.UpdateInteractionModeRequest{Project: "project-a", ChangeID: change.ID, InteractionMode: sdd.InteractionInteractive, ExpectedStateVersion: 1})
	testutil.NoError(t, err)
	if updated.InteractionMode != sdd.InteractionInteractive || updated.StateVersion != 2 {
		t.Fatalf("updated change=%+v", updated)
	}
	_, err = store.UpdateInteractionMode(ctx, sdd.UpdateInteractionModeRequest{Project: "project-a", ChangeID: change.ID, InteractionMode: sdd.InteractionAutomatic, ExpectedStateVersion: 1})
	if !errors.Is(err, sdd.ErrStaleState) {
		t.Fatalf("stale mode update error=%v", err)
	}
}

func TestSDDRepositoryRequiresAcceptedCurrentArtifactBeforeTransition(t *testing.T) {
	for _, backend := range []sdd.Backend{sdd.BackendMemory, sdd.BackendHybrid, sdd.BackendOpenSpec} {
		t.Run(string(backend), func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()
			change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "gated-" + string(backend), Title: "Gated", Backend: backend, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium})
			testutil.NoError(t, err)
			_, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: 1})
			if !errors.Is(err, sdd.ErrConflict) {
				t.Fatalf("transition without accepted artifact error=%v", err)
			}
			content := []byte("research")
			request := sdd.SaveRevisionRequest{Project: "project-a", ChangeID: change.ID, Artifact: sdd.PhaseExplore, Content: content, ExpectedStateVersion: 1}
			if backend == sdd.BackendOpenSpec {
				request.ExternalLocation, err = sdd.OpenSpecProjectionPath(change.ID, sdd.PhaseExplore)
				testutil.NoError(t, err)
			}
			revision, err := store.SaveRevision(ctx, request)
			testutil.NoError(t, err)
			_, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: revision.StateVersion})
			if !errors.Is(err, sdd.ErrConflict) {
				t.Fatalf("transition with candidate artifact error=%v", err)
			}
			revision, err = store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: revision.ID, ExpectedStateVersion: revision.StateVersion})
			testutil.NoError(t, err)
			stateVersion := revision.StateVersion
			if backend != sdd.BackendMemory {
				_, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: stateVersion})
				if !errors.Is(err, sdd.ErrConflict) {
					t.Fatalf("transition without current projection error=%v", err)
				}
				location := revision.ExternalLocation
				projectionDigest := revision.Digest
				if backend == sdd.BackendHybrid {
					document, renderErr := sdd.RenderOpenSpecProjection(revision)
					testutil.NoError(t, renderErr)
					location, projectionDigest = document.RelativePath, document.Digest
				}
				projection, recordErr := store.RecordProjection(ctx, sdd.RecordProjectionRequest{Project: "project-a", ChangeID: change.ID, ArtifactID: revision.ArtifactID, RevisionID: revision.ID, Status: sdd.ProjectionCurrent, Digest: projectionDigest, Location: location, ExpectedStateVersion: stateVersion})
				testutil.NoError(t, recordErr)
				stateVersion = projection.StateVersion
			}
			advanced, err := store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: "project-a", ChangeID: change.ID, TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: stateVersion})
			testutil.NoError(t, err)
			if advanced.Phase != sdd.PhaseProposal {
				t.Fatalf("advanced change=%+v", advanced)
			}
		})
	}
}

func TestSDDRepositoryCreateIsIdempotentPerProject(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	request := sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "stable-create-1", Title: "Feature", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium}
	first, err := store.CreateChange(ctx, request)
	testutil.NoError(t, err)
	second, err := store.CreateChange(ctx, request)
	testutil.NoError(t, err)
	if first.ID != second.ID || second.StateVersion != 1 {
		t.Fatalf("idempotent create changed identity: first=%+v second=%+v", first, second)
	}
	request.Title = "Different feature"
	_, err = store.CreateChange(ctx, request)
	if !errors.Is(err, sdd.ErrConflict) {
		t.Fatalf("changed idempotent payload error=%v", err)
	}
	request.Project, request.Title = "project-b", "Feature"
	other, err := store.CreateChange(ctx, request)
	testutil.NoError(t, err)
	if other.ID == first.ID {
		t.Fatalf("idempotency key leaked across projects: %+v", other)
	}
}

func TestSDDRepositoryRejectsAcceptingFuturePhaseRevision(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "future-accept-1", Title: "Feature", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium})
	testutil.NoError(t, err)
	digest := sdd.ContentDigest([]byte("future proposal"))
	inputDigest := sdd.InputRevisionDigest(nil)
	_, err = store.db.Exec(`INSERT INTO sdd_artifacts(id,project_id,change_id,phase,status,current_revision_id,created_at,updated_at) VALUES('artifact-future','project-a',?,'proposal','draft',NULL,1,1)`, change.ID)
	testutil.NoError(t, err)
	_, err = store.db.Exec(`INSERT INTO sdd_revisions(id,project_id,change_id,artifact_id,status,content,external_location,content_digest,input_digest,created_at,accepted_at) VALUES('revision-future','project-a',?,'artifact-future','candidate',?,NULL,?,?,1,NULL)`, change.ID, []byte("future proposal"), digest, inputDigest)
	testutil.NoError(t, err)
	_, err = store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: "project-a", ChangeID: change.ID, RevisionID: "revision-future", ExpectedStateVersion: 1})
	if !errors.Is(err, sdd.ErrConflict) {
		t.Fatalf("future revision acceptance error=%v", err)
	}
}

func TestSDDRepositorySequentialLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	change, err := store.CreateChange(ctx, sdd.CreateChangeRequest{Project: "project-a", IdempotencyKey: "sequential-1", Title: "Sequential", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanLow})
	testutil.NoError(t, err)
	for _, phase := range []sdd.Phase{sdd.PhaseExplore, sdd.PhaseProposal, sdd.PhaseSpec, sdd.PhaseDesign, sdd.PhaseTasks, sdd.PhaseApply, sdd.PhaseVerify} {
		revision, saveErr := store.SaveRevision(ctx, sdd.SaveRevisionRequest{Project: change.Project, ChangeID: change.ID, Artifact: phase, Content: []byte("artifact " + phase), ExpectedStateVersion: change.StateVersion})
		testutil.NoError(t, saveErr)
		revision, acceptErr := store.AcceptRevision(ctx, sdd.AcceptRevisionRequest{Project: change.Project, ChangeID: change.ID, RevisionID: revision.ID, ExpectedStateVersion: revision.StateVersion})
		testutil.NoError(t, acceptErr)
		target := sdd.PhaseComplete
		switch phase {
		case sdd.PhaseExplore:
			target = sdd.PhaseProposal
		case sdd.PhaseProposal:
			target = sdd.PhaseSpec
		case sdd.PhaseSpec:
			target = sdd.PhaseDesign
		case sdd.PhaseDesign:
			target = sdd.PhaseTasks
		case sdd.PhaseTasks:
			target = sdd.PhaseApply
		case sdd.PhaseApply:
			target = sdd.PhaseVerify
		}
		change, err = store.TransitionChange(ctx, sdd.TransitionChangeRequest{Project: change.Project, ChangeID: change.ID, TargetPhase: target, ExpectedStateVersion: revision.StateVersion})
		testutil.NoError(t, err)
	}
	if change.Phase != sdd.PhaseComplete || change.Status != sdd.ChangeCompleted {
		t.Fatalf("sequential lifecycle did not complete: %+v", change)
	}
}
