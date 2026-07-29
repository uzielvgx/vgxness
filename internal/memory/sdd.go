package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/sdd"
)

const sddChangeColumns = `id,project_id,idempotency_key,title,backend,interaction_mode,model_plan,phase,status,state_version,created_at,updated_at`

type sddQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) CreateChange(ctx context.Context, request sdd.CreateChangeRequest) (sdd.Change, error) {
	if err := request.Validate(); err != nil {
		return sdd.Change{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Change{}, err
	}
	id, err := newSDDID("change")
	if err != nil {
		return sdd.Change{}, fmt.Errorf("create SDD change id: %w", err)
	}
	now := s.now().UTC()
	change := sdd.Change{ID: id, Project: request.Project, Title: request.Title, Backend: request.Backend, InteractionMode: request.InteractionMode, Plan: request.Plan, Phase: sdd.PhaseExplore, Status: sdd.ChangeActive, StateVersion: 1, CreatedAt: now, UpdatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sdd.Change{}, sddStoreError(ctx, err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO projects(id) VALUES(?)`, request.Project); err == nil {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO sdd_changes(id,project_id,idempotency_key,title,backend,interaction_mode,model_plan,phase,status,state_version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, change.ID, change.Project, request.IdempotencyKey, change.Title, change.Backend, change.InteractionMode, change.Plan, change.Phase, change.Status, change.StateVersion, now.UnixNano(), now.UnixNano())
	}
	if err != nil {
		return sdd.Change{}, sddConflictOrStore(ctx, err)
	}
	existing, found, err := loadSDDChangeByIdempotencyKey(tx, ctx, request.Project, request.IdempotencyKey)
	if err != nil {
		return sdd.Change{}, err
	}
	if !found || existing.Title != request.Title || existing.Backend != request.Backend || existing.InteractionMode != request.InteractionMode || existing.Plan != request.Plan {
		return sdd.Change{}, fmt.Errorf("%w: create idempotency key", sdd.ErrConflict)
	}
	if err = commit(ctx, tx); err != nil {
		return sdd.Change{}, err
	}
	return existing, nil
}

func (s *Store) ListChanges(ctx context.Context, request sdd.ListChangesRequest) ([]sdd.Change, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	query := `SELECT ` + sddChangeColumns + ` FROM sdd_changes WHERE project_id=?`
	args := []any{request.Project}
	if request.Status != "" {
		query += ` AND status=?`
		args = append(args, request.Status)
	}
	query += ` ORDER BY updated_at DESC,id ASC LIMIT ?`
	limit := request.Limit
	if limit == 0 {
		limit = 20
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, sddStoreError(ctx, err)
	}
	defer rows.Close()
	changes := make([]sdd.Change, 0)
	for rows.Next() {
		change, scanErr := scanSDDChange(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("read SDD change: %w", ErrCorrupt)
		}
		changes = append(changes, change)
	}
	if err = rows.Err(); err != nil {
		return nil, sddStoreError(ctx, err)
	}
	return changes, nil
}

func (s *Store) GetChange(ctx context.Context, request sdd.GetChangeRequest) (sdd.Change, error) {
	if err := request.Validate(); err != nil {
		return sdd.Change{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Change{}, err
	}
	change, found, err := loadSDDChange(s.db, ctx, request.Project, request.ID)
	if err != nil {
		return sdd.Change{}, err
	}
	if !found {
		return sdd.Change{}, fmt.Errorf("%w: change", sdd.ErrNotFound)
	}
	return change, nil
}

func (s *Store) UpdateInteractionMode(ctx context.Context, request sdd.UpdateInteractionModeRequest) (sdd.Change, error) {
	if err := request.Validate(); err != nil {
		return sdd.Change{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Change{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sdd.Change{}, sddStoreError(ctx, err)
	}
	defer tx.Rollback()
	change, err := activeSDDChange(tx, ctx, request.Project, request.ChangeID, request.ExpectedStateVersion)
	if err != nil {
		return sdd.Change{}, err
	}
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE sdd_changes SET interaction_mode=?,state_version=state_version+1,updated_at=? WHERE id=? AND project_id=? AND state_version=? AND status=?`, request.InteractionMode, now.UnixNano(), change.ID, change.Project, request.ExpectedStateVersion, sdd.ChangeActive)
	if err != nil {
		return sdd.Change{}, sddStoreError(ctx, err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return sdd.Change{}, fmt.Errorf("%w: interaction mode", sdd.ErrStaleState)
	}
	if err = commit(ctx, tx); err != nil {
		return sdd.Change{}, err
	}
	change.InteractionMode = request.InteractionMode
	change.StateVersion++
	change.UpdatedAt = now
	return change, nil
}

func (s *Store) SaveRevision(ctx context.Context, request sdd.SaveRevisionRequest) (sdd.Revision, error) {
	if err := request.Validate(); err != nil {
		return sdd.Revision{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Revision{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sdd.Revision{}, sddStoreError(ctx, err)
	}
	defer tx.Rollback()
	change, err := activeSDDChange(tx, ctx, request.Project, request.ChangeID, request.ExpectedStateVersion)
	if err != nil {
		return sdd.Revision{}, err
	}
	if change.Phase == sdd.PhaseComplete || request.Artifact != change.Phase {
		return sdd.Revision{}, fmt.Errorf("%w: artifact phase does not match current change phase", sdd.ErrConflict)
	}
	contentDigest := sdd.ContentDigest(request.Content)
	if request.Digest != "" && request.Digest != contentDigest {
		return sdd.Revision{}, fmt.Errorf("%w: content", sdd.ErrDigestMismatch)
	}
	inputDigest := sdd.InputRevisionDigest(request.Inputs)
	if request.InputDigest != "" && request.InputDigest != inputDigest {
		return sdd.Revision{}, fmt.Errorf("%w: inputs", sdd.ErrDigestMismatch)
	}
	revisionID, err := newSDDID("revision")
	if err != nil {
		return sdd.Revision{}, fmt.Errorf("create SDD revision id: %w", err)
	}
	storedContent := request.Content
	externalLocation := request.ExternalLocation
	if change.Backend == sdd.BackendOpenSpec {
		expectedLocation, locationErr := sdd.OpenSpecProjectionPath(change.ID, request.Artifact)
		if locationErr != nil || externalLocation != expectedLocation {
			return sdd.Revision{}, fmt.Errorf("%w: external OpenSpec revision location", sdd.ErrInvalid)
		}
		storedContent = nil
	} else if externalLocation != "" {
		return sdd.Revision{}, fmt.Errorf("%w: external revision requires openspec backend", sdd.ErrInvalid)
	}

	artifact, found, err := loadSDDArtifactByPhase(tx, ctx, request.Project, request.ChangeID, request.Artifact)
	if err != nil {
		return sdd.Revision{}, err
	}
	now := s.now().UTC()
	if !found {
		artifactID, idErr := newSDDID("artifact")
		if idErr != nil {
			return sdd.Revision{}, fmt.Errorf("create SDD artifact id: %w", idErr)
		}
		artifact = sdd.Artifact{ID: artifactID, Project: request.Project, ChangeID: request.ChangeID, Phase: request.Artifact, Status: sdd.ArtifactDraft, CreatedAt: now, UpdatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO sdd_artifacts(id,project_id,change_id,phase,status,current_revision_id,created_at,updated_at) VALUES(?,?,?,?,?,NULL,?,?)`, artifact.ID, artifact.Project, artifact.ChangeID, artifact.Phase, artifact.Status, now.UnixNano(), now.UnixNano())
		if err != nil {
			return sdd.Revision{}, sddConflictOrStore(ctx, err)
		}
	}
	for _, input := range request.Inputs {
		var phase sdd.Phase
		var digest sdd.Digest
		err = tx.QueryRowContext(ctx, `
			SELECT a.phase,r.content_digest
			FROM sdd_artifacts a JOIN sdd_revisions r ON r.id=a.current_revision_id
			WHERE a.project_id=? AND a.change_id=? AND a.id=? AND a.current_revision_id=?
			  AND a.status=? AND r.project_id=a.project_id AND r.change_id=a.change_id AND r.status=?`, request.Project, request.ChangeID, input.ArtifactID, input.RevisionID, sdd.ArtifactAccepted, sdd.RevisionAccepted).Scan(&phase, &digest)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (digest != input.Digest || !sdd.IsDownstream(phase, request.Artifact)) {
			return sdd.Revision{}, fmt.Errorf("%w: %s", sdd.ErrInputsChanged, input.ArtifactID)
		}
		if err != nil {
			return sdd.Revision{}, sddStoreError(ctx, err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sdd_revisions(id,project_id,change_id,artifact_id,status,content,external_location,content_digest,input_digest,created_at,accepted_at) VALUES(?,?,?,?,?,?,?,?,?,?,NULL)`, revisionID, request.Project, request.ChangeID, artifact.ID, sdd.RevisionCandidate, storedContent, nullableText(externalLocation), contentDigest, inputDigest, now.UnixNano())
	if err != nil {
		return sdd.Revision{}, sddConflictOrStore(ctx, err)
	}
	for _, input := range request.Inputs {
		_, err = tx.ExecContext(ctx, `INSERT INTO sdd_revision_links(project_id,change_id,revision_id,input_artifact_id,input_revision_id,input_digest) VALUES(?,?,?,?,?,?)`, request.Project, request.ChangeID, revisionID, input.ArtifactID, input.RevisionID, input.Digest)
		if err != nil {
			return sdd.Revision{}, sddConflictOrStore(ctx, err)
		}
	}
	stateVersion, err := updateSDDVersion(ctx, tx, change, now)
	if err != nil {
		return sdd.Revision{}, err
	}
	if err = commit(ctx, tx); err != nil {
		return sdd.Revision{}, err
	}
	return sdd.Revision{ID: revisionID, Project: request.Project, ChangeID: request.ChangeID, ArtifactID: artifact.ID, Artifact: artifact.Phase, ArtifactStatus: artifact.Status, Status: sdd.RevisionCandidate, Content: append([]byte(nil), storedContent...), ExternalLocation: externalLocation, Digest: contentDigest, InputDigest: inputDigest, Inputs: append([]sdd.RevisionBinding(nil), request.Inputs...), StateVersion: stateVersion, CreatedAt: now}, nil
}

func (s *Store) GetRevision(ctx context.Context, request sdd.GetRevisionRequest) (sdd.Revision, error) {
	if err := request.Validate(); err != nil {
		return sdd.Revision{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Revision{}, err
	}
	revision, found, err := loadSDDRevision(s.db, ctx, request.Project, request.ChangeID, request.RevisionID)
	if err != nil {
		return sdd.Revision{}, err
	}
	if !found {
		return sdd.Revision{}, fmt.Errorf("%w: revision", sdd.ErrNotFound)
	}
	return revision, nil
}

func (s *Store) ListRevisions(ctx context.Context, request sdd.ListRevisionsRequest) ([]sdd.Revision, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	query := revisionSelect + ` WHERE r.project_id=? AND r.change_id=?`
	args := []any{request.Project, request.ChangeID}
	if request.Artifact != "" {
		query += ` AND a.phase=?`
		args = append(args, request.Artifact)
	}
	query += ` ORDER BY r.created_at DESC,r.id ASC LIMIT ?`
	limit := request.Limit
	if limit == 0 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, sddStoreError(ctx, err)
	}
	revisions := make([]sdd.Revision, 0)
	for rows.Next() {
		revision, scanErr := scanSDDRevision(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("read SDD revision: %w", ErrCorrupt)
		}
		revisions = append(revisions, revision)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, sddStoreError(ctx, err)
	}
	_ = rows.Close()
	for index := range revisions {
		inputs, inputErr := loadSDDInputs(s.db, ctx, revisions[index].Project, revisions[index].ChangeID, revisions[index].ID)
		if inputErr != nil {
			return nil, inputErr
		}
		revisions[index].Inputs = inputs
		revisions[index].Content = nil
	}
	return revisions, nil
}

func (s *Store) AcceptRevision(ctx context.Context, request sdd.AcceptRevisionRequest) (sdd.Revision, error) {
	if err := request.Validate(); err != nil {
		return sdd.Revision{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Revision{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sdd.Revision{}, sddStoreError(ctx, err)
	}
	defer tx.Rollback()
	change, err := activeSDDChange(tx, ctx, request.Project, request.ChangeID, request.ExpectedStateVersion)
	if err != nil {
		return sdd.Revision{}, err
	}
	revision, found, err := loadSDDRevision(tx, ctx, request.Project, request.ChangeID, request.RevisionID)
	if err != nil {
		return sdd.Revision{}, err
	}
	if !found {
		return sdd.Revision{}, fmt.Errorf("%w: revision", sdd.ErrNotFound)
	}
	if change.Phase == sdd.PhaseComplete || revision.Artifact != change.Phase {
		return sdd.Revision{}, fmt.Errorf("%w: revision phase does not match current change phase", sdd.ErrConflict)
	}
	if revision.Status == sdd.RevisionAccepted {
		return sdd.Revision{}, fmt.Errorf("%w: revision %s", sdd.ErrImmutable, revision.ID)
	}
	for _, input := range revision.Inputs {
		var currentRevision string
		var digest sdd.Digest
		err = tx.QueryRowContext(ctx, `SELECT a.current_revision_id,r.content_digest FROM sdd_artifacts a JOIN sdd_revisions r ON r.id=a.current_revision_id WHERE a.project_id=? AND a.change_id=? AND a.id=? AND a.status=?`, request.Project, request.ChangeID, input.ArtifactID, sdd.ArtifactAccepted).Scan(&currentRevision, &digest)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (currentRevision != input.RevisionID || digest != input.Digest) {
			return sdd.Revision{}, fmt.Errorf("%w: %s", sdd.ErrInputsChanged, input.ArtifactID)
		}
		if err != nil {
			return sdd.Revision{}, sddStoreError(ctx, err)
		}
	}
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE sdd_revisions SET status=?,accepted_at=? WHERE id=? AND project_id=? AND change_id=? AND status=?`, sdd.RevisionAccepted, now.UnixNano(), revision.ID, request.Project, request.ChangeID, sdd.RevisionCandidate)
	if err != nil {
		return sdd.Revision{}, sddStoreError(ctx, err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return sdd.Revision{}, fmt.Errorf("%w: revision acceptance", sdd.ErrConflict)
	}
	_, err = tx.ExecContext(ctx, `UPDATE sdd_artifacts SET status=?,current_revision_id=?,updated_at=? WHERE id=? AND project_id=? AND change_id=?`, sdd.ArtifactAccepted, revision.ID, now.UnixNano(), revision.ArtifactID, request.Project, request.ChangeID)
	if err != nil {
		return sdd.Revision{}, sddStoreError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, `
		WITH RECURSIVE downstream(id) AS (
			SELECT current.artifact_id
			FROM sdd_revisions current
			JOIN sdd_artifacts artifact ON artifact.current_revision_id=current.id
			JOIN sdd_revision_links link ON link.revision_id=current.id
			WHERE current.project_id=? AND current.change_id=? AND link.input_artifact_id=? AND link.input_revision_id<>?
			UNION
			SELECT current.artifact_id
			FROM sdd_revisions current
			JOIN sdd_artifacts artifact ON artifact.current_revision_id=current.id
			JOIN sdd_revision_links link ON link.revision_id=current.id
			JOIN downstream ON downstream.id=link.input_artifact_id
			WHERE current.project_id=? AND current.change_id=?
		)
		UPDATE sdd_artifacts SET status=?,updated_at=? WHERE project_id=? AND change_id=? AND id IN (SELECT id FROM downstream)`, request.Project, request.ChangeID, revision.ArtifactID, revision.ID, request.Project, request.ChangeID, sdd.ArtifactStale, now.UnixNano(), request.Project, request.ChangeID)
	if err != nil {
		return sdd.Revision{}, sddStoreError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE sdd_projections SET status=? WHERE project_id=? AND change_id=? AND (artifact_id=? OR artifact_id IN (SELECT id FROM sdd_artifacts WHERE project_id=? AND change_id=? AND status=?))`, sdd.ProjectionStale, request.Project, request.ChangeID, revision.ArtifactID, request.Project, request.ChangeID, sdd.ArtifactStale)
	if err != nil {
		return sdd.Revision{}, sddStoreError(ctx, err)
	}
	stateVersion, err := updateSDDVersion(ctx, tx, change, now)
	if err != nil {
		return sdd.Revision{}, err
	}
	if err = commit(ctx, tx); err != nil {
		return sdd.Revision{}, err
	}
	revision.Status = sdd.RevisionAccepted
	revision.ArtifactStatus = sdd.ArtifactAccepted
	revision.AcceptedAt = &now
	revision.StateVersion = stateVersion
	return revision, nil
}

func (s *Store) TransitionChange(ctx context.Context, request sdd.TransitionChangeRequest) (sdd.Change, error) {
	if err := request.Validate(); err != nil {
		return sdd.Change{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Change{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sdd.Change{}, sddStoreError(ctx, err)
	}
	defer tx.Rollback()
	change, err := activeSDDChange(tx, ctx, request.Project, request.ChangeID, request.ExpectedStateVersion)
	if err != nil {
		return sdd.Change{}, err
	}
	if request.Cancel {
		change.Status = sdd.ChangeCancelled
	} else {
		if err = sdd.ValidatePhaseTransition(change.Phase, request.TargetPhase); err != nil {
			return sdd.Change{}, err
		}
		if err = requireSDDPhaseGate(tx, ctx, change); err != nil {
			return sdd.Change{}, err
		}
		change.Phase = request.TargetPhase
		if change.Phase == sdd.PhaseComplete {
			change.Status = sdd.ChangeCompleted
		}
	}
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE sdd_changes SET phase=?,status=?,state_version=state_version+1,updated_at=? WHERE id=? AND project_id=? AND state_version=? AND status=?`, change.Phase, change.Status, now.UnixNano(), change.ID, change.Project, request.ExpectedStateVersion, sdd.ChangeActive)
	if err != nil {
		return sdd.Change{}, sddStoreError(ctx, err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return sdd.Change{}, fmt.Errorf("%w: change transition", sdd.ErrStaleState)
	}
	if err = commit(ctx, tx); err != nil {
		return sdd.Change{}, err
	}
	change.StateVersion++
	change.UpdatedAt = now
	return change, nil
}

func requireSDDPhaseGate(tx *sql.Tx, ctx context.Context, change sdd.Change) error {
	var artifactID, revisionID string
	err := tx.QueryRowContext(ctx, `
		SELECT a.id,a.current_revision_id
		FROM sdd_artifacts a JOIN sdd_revisions r ON r.id=a.current_revision_id
		WHERE a.project_id=? AND a.change_id=? AND a.phase=? AND a.status=? AND r.status=?`,
		change.Project, change.ID, change.Phase, sdd.ArtifactAccepted, sdd.RevisionAccepted,
	).Scan(&artifactID, &revisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: phase %s has no accepted artifact", sdd.ErrConflict, change.Phase)
	}
	if err != nil {
		return sddStoreError(ctx, err)
	}
	if change.Backend == sdd.BackendMemory {
		return nil
	}
	var projectionRevision string
	err = tx.QueryRowContext(ctx, `SELECT revision_id FROM sdd_projections WHERE project_id=? AND change_id=? AND artifact_id=? AND status=?`, change.Project, change.ID, artifactID, sdd.ProjectionCurrent).Scan(&projectionRevision)
	if errors.Is(err, sql.ErrNoRows) || err == nil && projectionRevision != revisionID {
		return fmt.Errorf("%w: phase %s projection is not current", sdd.ErrConflict, change.Phase)
	}
	if err != nil {
		return sddStoreError(ctx, err)
	}
	return nil
}

func (s *Store) ProjectionStatus(ctx context.Context, request sdd.ProjectionStatusRequest) (sdd.Projection, error) {
	if err := request.Validate(); err != nil {
		return sdd.Projection{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Projection{}, err
	}
	var projection sdd.Projection
	var recordedAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT p.project_id,p.change_id,p.artifact_id,p.revision_id,p.status,p.digest,p.location,c.state_version,p.recorded_at
		FROM sdd_projections p JOIN sdd_changes c ON c.id=p.change_id AND c.project_id=p.project_id
		WHERE p.project_id=? AND p.change_id=? AND p.artifact_id=?`, request.Project, request.ChangeID, request.ArtifactID).Scan(&projection.Project, &projection.ChangeID, &projection.ArtifactID, &projection.RevisionID, &projection.Status, &projection.Digest, &projection.Location, &projection.StateVersion, &recordedAt)
	if err == nil {
		at := time.Unix(0, recordedAt).UTC()
		projection.RecordedAt = &at
		return projection, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sdd.Projection{}, sddStoreError(ctx, err)
	}
	var stateVersion int64
	err = s.db.QueryRowContext(ctx, `SELECT c.state_version FROM sdd_artifacts a JOIN sdd_changes c ON c.id=a.change_id AND c.project_id=a.project_id WHERE a.project_id=? AND a.change_id=? AND a.id=?`, request.Project, request.ChangeID, request.ArtifactID).Scan(&stateVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return sdd.Projection{}, fmt.Errorf("%w: artifact", sdd.ErrNotFound)
	}
	if err != nil {
		return sdd.Projection{}, sddStoreError(ctx, err)
	}
	return sdd.Projection{Project: request.Project, ChangeID: request.ChangeID, ArtifactID: request.ArtifactID, Status: sdd.ProjectionAbsent, StateVersion: stateVersion}, nil
}

func (s *Store) RecordProjection(ctx context.Context, request sdd.RecordProjectionRequest) (sdd.Projection, error) {
	if err := request.Validate(); err != nil {
		return sdd.Projection{}, err
	}
	if err := cancelled(ctx); err != nil {
		return sdd.Projection{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sdd.Projection{}, sddStoreError(ctx, err)
	}
	defer tx.Rollback()
	change, err := activeSDDChange(tx, ctx, request.Project, request.ChangeID, request.ExpectedStateVersion)
	if err != nil {
		return sdd.Projection{}, err
	}
	var currentRevision string
	err = tx.QueryRowContext(ctx, `SELECT a.current_revision_id FROM sdd_artifacts a JOIN sdd_revisions r ON r.id=a.current_revision_id WHERE a.project_id=? AND a.change_id=? AND a.id=? AND r.id=? AND r.status=?`, request.Project, request.ChangeID, request.ArtifactID, request.RevisionID, sdd.RevisionAccepted).Scan(&currentRevision)
	if errors.Is(err, sql.ErrNoRows) || err == nil && currentRevision != request.RevisionID {
		return sdd.Projection{}, fmt.Errorf("%w: projection revision", sdd.ErrInputsChanged)
	}
	if err != nil {
		return sdd.Projection{}, sddStoreError(ctx, err)
	}
	if request.Status == sdd.ProjectionCurrent {
		revision, found, loadErr := loadSDDRevision(tx, ctx, request.Project, request.ChangeID, request.RevisionID)
		if loadErr != nil {
			return sdd.Projection{}, loadErr
		}
		if !found {
			return sdd.Projection{}, fmt.Errorf("%w: projection revision", sdd.ErrNotFound)
		}
		expectedLocation, locationErr := sdd.OpenSpecProjectionPath(change.ID, revision.Artifact)
		if locationErr != nil || request.Location != expectedLocation {
			return sdd.Projection{}, fmt.Errorf("%w: projection location", sdd.ErrInvalid)
		}
		expectedDigest := revision.Digest
		if change.Backend == sdd.BackendHybrid {
			document, renderErr := sdd.RenderOpenSpecProjection(revision)
			if renderErr != nil {
				return sdd.Projection{}, renderErr
			}
			expectedDigest = document.Digest
		}
		if request.Digest != expectedDigest {
			return sdd.Projection{}, fmt.Errorf("%w: projection", sdd.ErrDigestMismatch)
		}
	}
	now := s.now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sdd_projections(project_id,change_id,artifact_id,revision_id,status,digest,location,recorded_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(artifact_id) DO UPDATE SET revision_id=excluded.revision_id,status=excluded.status,digest=excluded.digest,location=excluded.location,recorded_at=excluded.recorded_at
		WHERE sdd_projections.project_id=excluded.project_id AND sdd_projections.change_id=excluded.change_id`, request.Project, request.ChangeID, request.ArtifactID, request.RevisionID, request.Status, request.Digest, request.Location, now.UnixNano())
	if err != nil {
		return sdd.Projection{}, sddConflictOrStore(ctx, err)
	}
	stateVersion, err := updateSDDVersion(ctx, tx, change, now)
	if err != nil {
		return sdd.Projection{}, err
	}
	if err = commit(ctx, tx); err != nil {
		return sdd.Projection{}, err
	}
	return sdd.Projection{Project: request.Project, ChangeID: request.ChangeID, ArtifactID: request.ArtifactID, RevisionID: request.RevisionID, Status: request.Status, Digest: request.Digest, Location: request.Location, StateVersion: stateVersion, RecordedAt: &now}, nil
}

func activeSDDChange(query sddQuerier, ctx context.Context, project, id string, expected int64) (sdd.Change, error) {
	change, found, err := loadSDDChange(query, ctx, project, id)
	if err != nil {
		return sdd.Change{}, err
	}
	if !found {
		return sdd.Change{}, fmt.Errorf("%w: change", sdd.ErrNotFound)
	}
	if change.Status == sdd.ChangeCancelled {
		return sdd.Change{}, sdd.ErrChangeCancelled
	}
	if change.Status != sdd.ChangeActive {
		return sdd.Change{}, fmt.Errorf("%w: change is %s", sdd.ErrConflict, change.Status)
	}
	if change.StateVersion != expected {
		return sdd.Change{}, fmt.Errorf("%w: expected %d, current %d", sdd.ErrStaleState, expected, change.StateVersion)
	}
	return change, nil
}

func updateSDDVersion(ctx context.Context, tx *sql.Tx, change sdd.Change, now time.Time) (int64, error) {
	result, err := tx.ExecContext(ctx, `UPDATE sdd_changes SET state_version=state_version+1,updated_at=? WHERE id=? AND project_id=? AND state_version=? AND status=?`, now.UnixNano(), change.ID, change.Project, change.StateVersion, sdd.ChangeActive)
	if err != nil {
		return 0, sddStoreError(ctx, err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return 0, fmt.Errorf("%w: change mutation", sdd.ErrStaleState)
	}
	return change.StateVersion + 1, nil
}

func loadSDDChange(query sddQuerier, ctx context.Context, project, id string) (sdd.Change, bool, error) {
	change, err := scanSDDChange(query.QueryRowContext(ctx, `SELECT `+sddChangeColumns+` FROM sdd_changes WHERE project_id=? AND id=?`, project, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sdd.Change{}, false, nil
	}
	if err != nil {
		return sdd.Change{}, false, sddStoreError(ctx, err)
	}
	return change, true, nil
}

func loadSDDChangeByIdempotencyKey(query sddQuerier, ctx context.Context, project, key string) (sdd.Change, bool, error) {
	change, err := scanSDDChange(query.QueryRowContext(ctx, `SELECT `+sddChangeColumns+` FROM sdd_changes WHERE project_id=? AND idempotency_key=?`, project, key))
	if errors.Is(err, sql.ErrNoRows) {
		return sdd.Change{}, false, nil
	}
	if err != nil {
		return sdd.Change{}, false, sddStoreError(ctx, err)
	}
	return change, true, nil
}

type sddScanner interface{ Scan(...any) error }

func scanSDDChange(scanner sddScanner) (sdd.Change, error) {
	var change sdd.Change
	var idempotencyKey string
	var createdAt, updatedAt int64
	err := scanner.Scan(&change.ID, &change.Project, &idempotencyKey, &change.Title, &change.Backend, &change.InteractionMode, &change.Plan, &change.Phase, &change.Status, &change.StateVersion, &createdAt, &updatedAt)
	if err == nil {
		change.CreatedAt = time.Unix(0, createdAt).UTC()
		change.UpdatedAt = time.Unix(0, updatedAt).UTC()
	}
	return change, err
}

func loadSDDArtifactByPhase(query sddQuerier, ctx context.Context, project, changeID string, phase sdd.Phase) (sdd.Artifact, bool, error) {
	var artifact sdd.Artifact
	var current sql.NullString
	var createdAt, updatedAt int64
	err := query.QueryRowContext(ctx, `SELECT id,project_id,change_id,phase,status,current_revision_id,created_at,updated_at FROM sdd_artifacts WHERE project_id=? AND change_id=? AND phase=?`, project, changeID, phase).Scan(&artifact.ID, &artifact.Project, &artifact.ChangeID, &artifact.Phase, &artifact.Status, &current, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return sdd.Artifact{}, false, nil
	}
	if err != nil {
		return sdd.Artifact{}, false, sddStoreError(ctx, err)
	}
	artifact.CurrentRevisionID = current.String
	artifact.CreatedAt = time.Unix(0, createdAt).UTC()
	artifact.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return artifact, true, nil
}

const revisionSelect = `
	SELECT r.id,r.project_id,r.change_id,r.artifact_id,a.phase,a.status,r.status,r.content,r.external_location,r.content_digest,r.input_digest,c.state_version,r.created_at,r.accepted_at
	FROM sdd_revisions r
	JOIN sdd_artifacts a ON a.id=r.artifact_id AND a.project_id=r.project_id AND a.change_id=r.change_id
	JOIN sdd_changes c ON c.id=r.change_id AND c.project_id=r.project_id`

func loadSDDRevision(query sddQuerier, ctx context.Context, project, changeID, revisionID string) (sdd.Revision, bool, error) {
	revision, err := scanSDDRevision(query.QueryRowContext(ctx, revisionSelect+` WHERE r.project_id=? AND r.change_id=? AND r.id=?`, project, changeID, revisionID))
	if errors.Is(err, sql.ErrNoRows) {
		return sdd.Revision{}, false, nil
	}
	if err != nil {
		return sdd.Revision{}, false, sddStoreError(ctx, err)
	}
	inputs, err := loadSDDInputs(query, ctx, project, changeID, revisionID)
	if err != nil {
		return sdd.Revision{}, false, err
	}
	revision.Inputs = inputs
	return revision, true, nil
}

func scanSDDRevision(scanner sddScanner) (sdd.Revision, error) {
	var revision sdd.Revision
	var createdAt int64
	var acceptedAt sql.NullInt64
	var externalLocation sql.NullString
	err := scanner.Scan(&revision.ID, &revision.Project, &revision.ChangeID, &revision.ArtifactID, &revision.Artifact, &revision.ArtifactStatus, &revision.Status, &revision.Content, &externalLocation, &revision.Digest, &revision.InputDigest, &revision.StateVersion, &createdAt, &acceptedAt)
	if err == nil {
		revision.Content = append([]byte(nil), revision.Content...)
		revision.ExternalLocation = externalLocation.String
		revision.CreatedAt = time.Unix(0, createdAt).UTC()
		if acceptedAt.Valid {
			at := time.Unix(0, acceptedAt.Int64).UTC()
			revision.AcceptedAt = &at
		}
	}
	return revision, err
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func loadSDDInputs(query sddQuerier, ctx context.Context, project, changeID, revisionID string) ([]sdd.RevisionBinding, error) {
	rows, err := query.QueryContext(ctx, `SELECT input_artifact_id,input_revision_id,input_digest FROM sdd_revision_links WHERE project_id=? AND change_id=? AND revision_id=? ORDER BY input_artifact_id,input_revision_id`, project, changeID, revisionID)
	if err != nil {
		return nil, sddStoreError(ctx, err)
	}
	defer rows.Close()
	inputs := make([]sdd.RevisionBinding, 0)
	for rows.Next() {
		var input sdd.RevisionBinding
		if err = rows.Scan(&input.ArtifactID, &input.RevisionID, &input.Digest); err != nil {
			return nil, sddStoreError(ctx, err)
		}
		inputs = append(inputs, input)
	}
	if err = rows.Err(); err != nil {
		return nil, sddStoreError(ctx, err)
	}
	return inputs, nil
}

func newSDDID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(value[:]), nil
}

func sddConflictOrStore(ctx context.Context, err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
		return fmt.Errorf("%w: SDD record already exists", sdd.ErrConflict)
	}
	return sddStoreError(ctx, err)
}

func sddStoreError(ctx context.Context, _ error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: SDD storage operation failed", ErrCorrupt)
}

var _ sdd.Repository = (*Store)(nil)
