package memory

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncservice"
)

// SyncProjectTopology is the complete local metadata snapshot used to plan a
// sync-project operation. It deliberately excludes workspace hashes and all
// record, payload, queue, and repair state.
type SyncProjectTopology struct {
	SchemaVersion              int
	PortableProjectID          string
	WorkspaceProjectID         string
	WorkspacePortableProjectID string
	BoundProjectID             string
	Cursor                     syncservice.Cursor
	HasCursor                  bool
	Transition                 SyncProjectTransitionResult
	HasTransition              bool
}

type syncProjectIdentity struct {
	portable, project, workspaceHash, source, boundAt string
}

func validSyncProjectIdentity(identity syncProjectIdentity) bool {
	if !projectIDPattern.MatchString(identity.portable) || identity.project == "" || identity.workspaceHash == "" || identity.source == "" {
		return false
	}
	workspaceHash, err := hex.DecodeString(identity.workspaceHash)
	if err != nil || len(workspaceHash) != sha256.Size || hex.EncodeToString(workspaceHash) != identity.workspaceHash {
		return false
	}
	boundAt, err := time.Parse(time.RFC3339Nano, identity.boundAt)
	return err == nil && !boundAt.IsZero() && identity.boundAt == boundAt.UTC().Format(time.RFC3339Nano)
}

// ReadSyncProjectTopology reads all local sync-project topology metadata in
// one read-only transaction. It never creates, repairs, or normalizes state.
func (s *Store) ReadSyncProjectTopology(ctx context.Context, workspace, portableProjectID string) (SyncProjectTopology, error) {
	if s == nil || ctx == nil || !projectIDPattern.MatchString(portableProjectID) {
		return SyncProjectTopology{}, fmt.Errorf("%w: sync project topology", ErrInvalid)
	}
	workspaceHash, err := portableWorkspaceHash(workspace)
	if err != nil {
		return SyncProjectTopology{}, err
	}
	if err := cancelled(ctx); err != nil {
		return SyncProjectTopology{}, err
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SyncProjectTopology{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	result := SyncProjectTopology{SchemaVersion: 1, PortableProjectID: portableProjectID}

	var rootExists int
	err = tx.QueryRowContext(ctx, `SELECT r.project_id,EXISTS(SELECT 1 FROM projects p WHERE p.id=r.project_id) FROM project_roots r WHERE r.workspace_hash=?`, workspaceHash).Scan(&result.WorkspaceProjectID, &rootExists)
	if errors.Is(err, sql.ErrNoRows) {
		result.WorkspaceProjectID = ""
	} else if err != nil {
		return SyncProjectTopology{}, writeError(ctx, err)
	} else if result.WorkspaceProjectID == "" || rootExists != 1 {
		return SyncProjectTopology{}, fmt.Errorf("%w: workspace project root", ErrCorrupt)
	}

	readIdentity := func(query, value string) (syncProjectIdentity, bool, error) {
		var identity syncProjectIdentity
		err := tx.QueryRowContext(ctx, query, value).Scan(&identity.portable, &identity.project, &identity.workspaceHash, &identity.source, &identity.boundAt)
		if errors.Is(err, sql.ErrNoRows) {
			return syncProjectIdentity{}, false, nil
		}
		if err != nil {
			return syncProjectIdentity{}, false, writeError(ctx, err)
		}
		if !validSyncProjectIdentity(identity) {
			return syncProjectIdentity{}, false, fmt.Errorf("%w: portable project identity", ErrCorrupt)
		}
		var projectExists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, identity.project).Scan(&projectExists); err != nil {
			return syncProjectIdentity{}, false, writeError(ctx, err)
		}
		if projectExists != 1 {
			return syncProjectIdentity{}, false, fmt.Errorf("%w: portable project identity", ErrCorrupt)
		}
		return identity, true, nil
	}
	workspaceIdentity, hasWorkspaceIdentity, err := readIdentity(`SELECT portable_id,project_id,workspace_hash,source,bound_at FROM portable_project_identities WHERE workspace_hash=?`, workspaceHash)
	if err != nil {
		return SyncProjectTopology{}, err
	}
	if hasWorkspaceIdentity {
		result.WorkspacePortableProjectID = workspaceIdentity.portable
		if result.WorkspaceProjectID == "" || workspaceIdentity.project != result.WorkspaceProjectID {
			return SyncProjectTopology{}, fmt.Errorf("%w: workspace portable project identity", ErrCorrupt)
		}
	}
	targetIdentity, hasTargetIdentity, err := readIdentity(`SELECT portable_id,project_id,workspace_hash,source,bound_at FROM portable_project_identities WHERE portable_id=?`, portableProjectID)
	if err != nil {
		return SyncProjectTopology{}, err
	}
	if hasTargetIdentity {
		result.BoundProjectID = targetIdentity.project
		if targetIdentity.workspaceHash == workspaceHash && (!hasWorkspaceIdentity || targetIdentity.portable != workspaceIdentity.portable || targetIdentity.project != result.WorkspaceProjectID) {
			return SyncProjectTopology{}, fmt.Errorf("%w: target portable project identity", ErrCorrupt)
		}
	}

	var cursorUpdatedAt int64
	err = tx.QueryRowContext(ctx, `SELECT history_id,position,watermark,updated_at FROM sync_project_cursor WHERE portable_project_id=?`, portableProjectID).Scan(&result.Cursor.HistoryID, &result.Cursor.Position, &result.Cursor.Watermark, &cursorUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		result.Cursor = syncservice.Cursor{}
	} else if err != nil {
		return SyncProjectTopology{}, writeError(ctx, err)
	} else if !hasTargetIdentity || !canonicalUUIDPattern.MatchString(result.Cursor.HistoryID) || result.Cursor.Position < 0 || result.Cursor.Watermark < result.Cursor.Position || cursorUpdatedAt <= 0 {
		return SyncProjectTopology{}, fmt.Errorf("%w: project pull cursor", ErrCorrupt)
	} else {
		result.HasCursor = true
	}

	var bound, mode, status string
	var createdAt int64
	var completedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT local_project_id,mode,status,created_at,completed_at FROM sync_project_transitions WHERE portable_project_id=?`, portableProjectID).Scan(&bound, &mode, &status, &createdAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// valid absence
	} else if err != nil {
		return SyncProjectTopology{}, writeError(ctx, err)
	} else if !hasTargetIdentity || bound == "" || bound != result.BoundProjectID || createdAt <= 0 || (mode != string(SyncProjectTransitionReseedSource) && mode != string(SyncProjectTransitionRejoinMerge)) || (status != SyncProjectTransitionPulling && status != SyncProjectTransitionPublishing && status != SyncProjectTransitionCompleted) || (mode == string(SyncProjectTransitionReseedSource) && status == SyncProjectTransitionPulling) || (status == SyncProjectTransitionCompleted && (!completedAt.Valid || completedAt.Int64 <= 0 || completedAt.Int64 < createdAt)) || (status != SyncProjectTransitionCompleted && completedAt.Valid) {
		return SyncProjectTopology{}, fmt.Errorf("%w: project sync transition", ErrCorrupt)
	} else {
		result.Transition = SyncProjectTransitionResult{SchemaVersion: 1, Mode: SyncProjectTransitionMode(mode), Status: status, TransitionIdentity: createdAt}
		result.HasTransition = true
	}
	if err := tx.Commit(); err != nil {
		return SyncProjectTopology{}, writeError(ctx, err)
	}
	return result, nil
}

// BackfillSyncProject explicitly queues legacy local records for one project.
// It does not contact a remote, mutate records, or change their sync versions.
func (s *Store) BackfillSyncProject(ctx context.Context, project string, limit int) (SyncBackfillResult, error) {
	if project == "" || limit < 1 || limit > 1000 {
		return SyncBackfillResult{}, fmt.Errorf("%w: project", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SyncBackfillResult{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	result := SyncBackfillResult{SchemaVersion: 1, Limit: limit}
	queued := 0
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT sync_version FROM projects WHERE id=?`, project).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return result, nil
	} else if err != nil || version < 0 {
		return SyncBackfillResult{}, writeError(ctx, err)
	}
	if err := activeProjectTransitionForLocalProject(ctx, tx, project); err != nil {
		return SyncBackfillResult{}, err
	}
	if version == 0 && queued < limit {
		mutation := syncservice.Mutation{MutationID: backfillMutationID("project", project), RecordID: project, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: project}}
		inserted, err := s.enqueueBackfillMutation(ctx, tx, mutation)
		if err != nil {
			return SyncBackfillResult{}, err
		}
		if inserted {
			queued++
			result.Projects++
			result.Queued++
		}
	}
	sessions, err := tx.QueryContext(ctx, `SELECT id,sync_version FROM sessions WHERE project_id=? ORDER BY id`, project)
	if err != nil {
		return SyncBackfillResult{}, writeError(ctx, err)
	}
	defer sessions.Close()
	for sessions.Next() {
		if queued >= limit {
			result.Remaining = true
			break
		}
		var id string
		var sessionVersion int64
		if err := sessions.Scan(&id, &sessionVersion); err != nil || sessionVersion < 0 {
			return SyncBackfillResult{}, writeError(ctx, err)
		}
		if sessionVersion != 0 {
			continue
		}
		mutation := syncservice.Mutation{MutationID: backfillMutationID("session", id), RecordID: id, RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: id, ProjectID: project}}
		inserted, err := s.enqueueBackfillMutation(ctx, tx, mutation)
		if err != nil {
			return SyncBackfillResult{}, err
		}
		if inserted {
			queued++
			result.Sessions++
			result.Queued++
		}
	}
	if err := sessions.Err(); err != nil {
		return SyncBackfillResult{}, writeError(ctx, err)
	}
	rows, err := tx.QueryContext(ctx, observationSelect+` WHERE o.project_id=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id) ORDER BY o.id`, project)
	if err != nil {
		return SyncBackfillResult{}, writeError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		if queued >= limit {
			result.Remaining = true
			break
		}
		item, err := scanObservation(rows)
		if err != nil {
			return SyncBackfillResult{}, err
		}
		var observationVersion int64
		if err := tx.QueryRowContext(ctx, `SELECT sync_version FROM observations WHERE id=?`, item.ID).Scan(&observationVersion); err != nil || observationVersion < 0 {
			return SyncBackfillResult{}, writeError(ctx, err)
		}
		if observationVersion != 0 {
			continue
		}
		snapshot := syncObservation(item)
		mutation := syncservice.Mutation{MutationID: backfillMutationID("observation", item.ID), RecordID: item.ID, RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationCreate, BaseVersion: 0, Observation: &snapshot}
		inserted, err := s.enqueueBackfillMutation(ctx, tx, mutation)
		if err != nil {
			return SyncBackfillResult{}, err
		}
		if inserted {
			queued++
			result.Observations++
			result.Queued++
		}
	}
	if err := rows.Err(); err != nil {
		return SyncBackfillResult{}, writeError(ctx, err)
	}
	return result, commit(ctx, tx)
}

// SyncProjectBackupIntent identifies the private snapshot reserved for a
// durable sync-project transition.
type SyncProjectBackupIntent struct {
	IntentID     string
	BackupPath   string
	BackupSHA256 []byte
}

// VerifySQLiteBackup validates an extant same-parent private SQLite snapshot
// before a retry reuses the durable backup intent's path.
func VerifySQLiteBackup(ctx context.Context, database, destination string) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if !filepath.IsAbs(database) || database != filepath.Clean(database) || !filepath.IsAbs(destination) || destination != filepath.Clean(destination) || database == destination || filepath.Dir(database) != filepath.Dir(destination) || rejectSymlink(database) != nil || rejectSymlink(destination) != nil {
		return fmt.Errorf("%w: backup paths", ErrInvalid)
	}
	parent, err := os.Lstat(filepath.Dir(database))
	if err != nil || parent.Mode()&os.ModeSymlink != 0 || !parent.IsDir() || !privateSQLiteBackupParent(parent.Mode()) {
		return fmt.Errorf("%w: backup parent", ErrCorrupt)
	}
	backup, err := os.Lstat(destination)
	if err != nil || !privateSQLiteBackupOutput(backup) {
		return fmt.Errorf("%w: backup destination", ErrCorrupt)
	}
	output, err := os.Open(destination)
	if err != nil {
		return fmt.Errorf("%w: backup destination", ErrCorrupt)
	}
	defer output.Close()
	if err := verifyPrivateSQLiteBackupOutput(output); err != nil {
		return err
	}
	version, err := HealthFile(ctx, destination)
	if err != nil || version != migrations[len(migrations)-1].version {
		return fmt.Errorf("%w: backup health", ErrCorrupt)
	}
	return nil
}

// VerifySQLiteBackupIntent verifies that an unsealed snapshot contains the
// exact durable intent which selected it. This permits a retry to seal and
// reuse the only snapshot produced after intent commit but before its seal;
// a healthy snapshot from any other database still fails closed.
func VerifySQLiteBackupIntent(ctx context.Context, database, destination, portableProject, localProject string, mode SyncProjectTransitionMode, intent SyncProjectBackupIntent) error {
	if err := VerifySQLiteBackup(ctx, database, destination); err != nil {
		return err
	}
	if !projectIDPattern.MatchString(portableProject) || localProject == "" || (mode != SyncProjectTransitionReseedSource && mode != SyncProjectTransitionRejoinMerge) || intent.IntentID == "" || intent.BackupPath != destination {
		return fmt.Errorf("%w: backup intent", ErrInvalid)
	}
	db, err := sql.Open("sqlite", sqliteReadURI(destination))
	if err != nil {
		return fmt.Errorf("%w: backup intent", ErrCorrupt)
	}
	defer db.Close()
	var storedID, storedPath, storedLocal, storedMode string
	err = db.QueryRowContext(ctx, `SELECT intent_id,backup_path,local_project_id,mode FROM sync_project_backup_intents WHERE portable_project_id=?`, portableProject).Scan(&storedID, &storedPath, &storedLocal, &storedMode)
	if err != nil || storedID != intent.IntentID || storedPath != intent.BackupPath || storedLocal != localProject || storedMode != string(mode) {
		return fmt.Errorf("%w: backup intent", ErrConflict)
	}
	return nil
}

func SQLiteBackupSHA256(ctx context.Context, path string) ([]byte, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: backup digest", ErrCorrupt)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}

// PrepareSyncProjectTransition atomically snapshots one bound project and
// prepares either a source reseed or a pull-first local merge. It is local-only.
func (s *Store) PrepareSyncProjectTransition(ctx context.Context, portableProject, localProject string, mode SyncProjectTransitionMode, confirmedRemoteEmpty bool) (SyncProjectTransitionResult, error) {
	return s.prepareSyncProjectTransition(ctx, portableProject, localProject, mode, confirmedRemoteEmpty, nil)
}

// PrepareSyncProjectTransitionWithBackupIntent atomically consumes the exact
// durable intent only when the local transition becomes durable.
func (s *Store) PrepareSyncProjectTransitionWithBackupIntent(ctx context.Context, portableProject, localProject string, mode SyncProjectTransitionMode, confirmedRemoteEmpty bool, intent SyncProjectBackupIntent) (SyncProjectTransitionResult, error) {
	return s.prepareSyncProjectTransition(ctx, portableProject, localProject, mode, confirmedRemoteEmpty, &intent)
}

func (s *Store) prepareSyncProjectTransition(ctx context.Context, portableProject, localProject string, mode SyncProjectTransitionMode, confirmedRemoteEmpty bool, intent *SyncProjectBackupIntent) (SyncProjectTransitionResult, error) {
	result := SyncProjectTransitionResult{SchemaVersion: 1, Mode: mode}
	if s == nil || s.readOnly || !projectIDPattern.MatchString(portableProject) || localProject == "" || mode != SyncProjectTransitionReseedSource && mode != SyncProjectTransitionRejoinMerge || mode == SyncProjectTransitionReseedSource && !confirmedRemoteEmpty {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return SyncProjectTransitionResult{}, err
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	var bound string
	err = conn.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE portable_id=?`, portableProject).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) || bound != localProject {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: portable project identity binding", ErrConflict)
	}
	if err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	exceptPortable := ""
	if intent != nil {
		exceptPortable = portableProject
	}
	if err = activeProjectTransitionForLocalProjectExceptIntent(ctx, conn, localProject, exceptPortable); err != nil {
		return SyncProjectTransitionResult{}, err
	}
	if intent != nil {
		var storedID, storedPath, storedLocal, storedMode string
		var storedDigest []byte
		err = conn.QueryRowContext(ctx, `SELECT intent_id,backup_path,backup_sha256,local_project_id,mode FROM sync_project_backup_intents WHERE portable_project_id=?`, portableProject).Scan(&storedID, &storedPath, &storedDigest, &storedLocal, &storedMode)
		if errors.Is(err, sql.ErrNoRows) || err != nil || len(storedDigest) != sha256.Size || !bytes.Equal(storedDigest, intent.BackupSHA256) || storedID != intent.IntentID || storedPath != intent.BackupPath || storedLocal != localProject || storedMode != string(mode) {
			return SyncProjectTransitionResult{}, fmt.Errorf("%w: project backup intent", ErrConflict)
		}
	}
	var pendingRepair int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_project_repairs WHERE portable_project_id=? AND status='pending'`, portableProject).Scan(&pendingRepair); err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	if pendingRepair != 0 {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: project repair is pending", ErrSyncProjectRepairPending)
	}
	var priorStatus string
	var priorCreatedAt int64
	err = conn.QueryRowContext(ctx, `SELECT status,created_at FROM sync_project_transitions WHERE portable_project_id=?`, portableProject).Scan(&priorStatus, &priorCreatedAt)
	if err == nil && priorStatus != SyncProjectTransitionCompleted {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition pending", ErrConflict)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	createdAt := now
	if priorStatus != "" && createdAt <= priorCreatedAt {
		if priorCreatedAt == int64(^uint64(0)>>1) {
			return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition generation", ErrCorrupt)
		}
		createdAt = priorCreatedAt + 1
	}
	var activeClaims int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox o JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id WHERE c.lease_until>? AND (o.record_kind='project' AND o.record_id=? OR o.record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=o.record_id AND s.project_id=?) OR o.record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=o.record_id AND n.project_id=?))`, now, localProject, localProject, localProject).Scan(&activeClaims); err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	if activeClaims != 0 {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: active project sync claim", ErrConflict)
	}
	mutations, err := projectTransitionMutations(ctx, conn, localProject)
	if err != nil {
		return SyncProjectTransitionResult{}, err
	}
	if len(mutations) == 0 || mutations[0].RecordKind != syncservice.RecordKindProject {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition source", ErrConflict)
	}
	if priorStatus != "" {
		if _, err = conn.ExecContext(ctx, `DELETE FROM sync_project_transitions WHERE portable_project_id=?`, portableProject); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
	}
	status := SyncProjectTransitionPulling
	if mode == SyncProjectTransitionReseedSource {
		status = SyncProjectTransitionPublishing
	}
	if intent != nil {
		if _, err = conn.ExecContext(ctx, `DELETE FROM sync_project_backup_intents WHERE portable_project_id=? AND intent_id=? AND backup_path=?`, portableProject, intent.IntentID, intent.BackupPath); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO sync_project_transitions(portable_project_id,local_project_id,mode,status,created_at,completed_at) VALUES(?,?,?,?,?,NULL)`, portableProject, localProject, mode, status, createdAt); err != nil {
		return SyncProjectTransitionResult{}, conflictOrWrite(ctx, err)
	}
	for _, mutation := range mutations {
		hash, hashErr := projectTransitionMutationHash(mutation)
		if hashErr != nil {
			return SyncProjectTransitionResult{}, hashErr
		}
		if _, err = conn.ExecContext(ctx, `INSERT INTO sync_project_transition_records(portable_project_id,record_kind,local_id,payload_hash,seen_remote) VALUES(?,?,?,?,0)`, portableProject, mutation.RecordKind, mutation.RecordID, hash); err != nil {
			return SyncProjectTransitionResult{}, conflictOrWrite(ctx, err)
		}
		projectTransitionCount(&result, mutation.RecordKind)
	}
	if err = clearProjectTransitionSyncState(ctx, conn, portableProject, localProject); err != nil {
		return SyncProjectTransitionResult{}, err
	}
	if mode == SyncProjectTransitionReseedSource {
		if _, err = conn.ExecContext(ctx, `UPDATE projects SET sync_version=0 WHERE id=?; UPDATE sessions SET sync_version=0 WHERE project_id=?; UPDATE observations SET sync_version=0 WHERE project_id=?`, localProject, localProject, localProject); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
		for _, mutation := range mutations {
			mutation.MutationID = ""
			if _, err = s.enqueueSyncOutbox(ctx, conn, mutation); err != nil {
				return SyncProjectTransitionResult{}, err
			}
			result.Queued++
		}
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	committed = true
	result.Status = status
	result.TransitionIdentity = createdAt
	return result, nil
}

// EnsureSyncProjectBackupIntent creates one durable backup destination before
// any local transition mutation. Retried calls return the original identity and
// path, while binding or mode changes fail closed.
func (s *Store) EnsureSyncProjectBackupIntent(ctx context.Context, portableProject, localProject string, mode SyncProjectTransitionMode, backupPath string) (SyncProjectBackupIntent, error) {
	if s == nil || s.readOnly || !projectIDPattern.MatchString(portableProject) || localProject == "" || (mode != SyncProjectTransitionReseedSource && mode != SyncProjectTransitionRejoinMerge) || !filepath.IsAbs(backupPath) || backupPath != filepath.Clean(backupPath) {
		return SyncProjectBackupIntent{}, fmt.Errorf("%w: project backup intent", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return SyncProjectBackupIntent{}, err
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return SyncProjectBackupIntent{}, fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return SyncProjectBackupIntent{}, writeError(ctx, err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return SyncProjectBackupIntent{}, writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	var bound string
	if err = conn.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE portable_id=?`, portableProject).Scan(&bound); errors.Is(err, sql.ErrNoRows) || bound != localProject {
		return SyncProjectBackupIntent{}, fmt.Errorf("%w: portable project identity binding", ErrConflict)
	} else if err != nil {
		return SyncProjectBackupIntent{}, writeError(ctx, err)
	}
	intent := SyncProjectBackupIntent{IntentID: "sync-project-backup:" + portableProject, BackupPath: backupPath}
	var storedLocal, storedMode string
	err = conn.QueryRowContext(ctx, `SELECT intent_id,backup_path,backup_sha256,local_project_id,mode FROM sync_project_backup_intents WHERE portable_project_id=?`, portableProject).Scan(&intent.IntentID, &intent.BackupPath, &intent.BackupSHA256, &storedLocal, &storedMode)
	if err == nil {
		if storedLocal != localProject || storedMode != string(mode) {
			return SyncProjectBackupIntent{}, fmt.Errorf("%w: project backup intent", ErrConflict)
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		intent.IntentID = "sync-project-backup:" + portableProject
		intent.BackupPath = backupPath
		if _, err = conn.ExecContext(ctx, `INSERT INTO sync_project_backup_intents(portable_project_id,local_project_id,mode,intent_id,backup_path,backup_sha256,created_at) VALUES(?,?,?,?,?,?,?)`, portableProject, localProject, mode, intent.IntentID, intent.BackupPath, nil, now); err != nil {
			return SyncProjectBackupIntent{}, conflictOrWrite(ctx, err)
		}
	} else {
		return SyncProjectBackupIntent{}, writeError(ctx, err)
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return SyncProjectBackupIntent{}, writeError(ctx, err)
	}
	committed = true
	return intent, nil
}

// SyncProjectBackupIntent reads the one exact durable intent for a strict
// project/mode binding without changing local state.
func (s *Store) SyncProjectBackupIntent(ctx context.Context, portableProject, localProject string, mode SyncProjectTransitionMode) (SyncProjectBackupIntent, bool, error) {
	if s == nil || !projectIDPattern.MatchString(portableProject) || localProject == "" || (mode != SyncProjectTransitionReseedSource && mode != SyncProjectTransitionRejoinMerge) {
		return SyncProjectBackupIntent{}, false, fmt.Errorf("%w: project backup intent", ErrInvalid)
	}
	var intent SyncProjectBackupIntent
	var bound, storedMode string
	err := s.db.QueryRowContext(ctx, `SELECT intent_id,backup_path,backup_sha256,local_project_id,mode FROM sync_project_backup_intents WHERE portable_project_id=?`, portableProject).Scan(&intent.IntentID, &intent.BackupPath, &intent.BackupSHA256, &bound, &storedMode)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncProjectBackupIntent{}, false, nil
	}
	if err != nil {
		return SyncProjectBackupIntent{}, false, writeError(ctx, err)
	}
	if bound != localProject || storedMode != string(mode) {
		return SyncProjectBackupIntent{}, false, fmt.Errorf("%w: project backup intent", ErrConflict)
	}
	return intent, true, nil
}

// SealSyncProjectBackupIntent records the exact healthy backup bytes once.
func (s *Store) SealSyncProjectBackupIntent(ctx context.Context, portableProject, localProject string, mode SyncProjectTransitionMode, intent SyncProjectBackupIntent, digest []byte) (SyncProjectBackupIntent, error) {
	if s == nil || s.readOnly || len(digest) != sha256.Size || intent.IntentID == "" || intent.BackupPath == "" {
		return SyncProjectBackupIntent{}, fmt.Errorf("%w: project backup intent", ErrInvalid)
	}
	result, found, err := s.SyncProjectBackupIntent(ctx, portableProject, localProject, mode)
	if err != nil || !found || result.IntentID != intent.IntentID || result.BackupPath != intent.BackupPath {
		return SyncProjectBackupIntent{}, fmt.Errorf("%w: project backup intent", ErrConflict)
	}
	if len(result.BackupSHA256) != 0 && !bytes.Equal(result.BackupSHA256, digest) {
		return SyncProjectBackupIntent{}, fmt.Errorf("%w: project backup intent", ErrConflict)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sync_project_backup_intents SET backup_sha256=? WHERE portable_project_id=? AND backup_sha256 IS NULL`, digest, portableProject)
	if err != nil {
		return SyncProjectBackupIntent{}, writeError(ctx, err)
	}
	result.BackupSHA256 = append([]byte(nil), digest...)
	return result, nil
}

type projectTransitionSQL interface {
	syncSQL
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func projectTransitionMutations(ctx context.Context, query projectTransitionSQL, project string) ([]syncservice.Mutation, error) {
	var version int64
	if err := query.QueryRowContext(ctx, `SELECT sync_version FROM projects WHERE id=?`, project).Scan(&version); err != nil || version < 0 {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: project sync transition source", ErrConflict)
		}
		return nil, writeError(ctx, err)
	}
	mutations := []syncservice.Mutation{{RecordID: project, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: project}}}
	rows, err := query.QueryContext(ctx, `SELECT id,sync_version FROM sessions WHERE project_id=? ORDER BY id`, project)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id, &version); err != nil || version < 0 {
			_ = rows.Close()
			return nil, writeError(ctx, err)
		}
		mutations = append(mutations, syncservice.Mutation{RecordID: id, RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: id, ProjectID: project}})
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, writeError(ctx, err)
	}
	_ = rows.Close()
	rows, err = query.QueryContext(ctx, observationSelect+` WHERE o.project_id=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id) ORDER BY o.id`, project)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	var observations []syncservice.Mutation
	for rows.Next() {
		item, scanErr := scanObservation(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		var itemVersion int64
		if err = query.QueryRowContext(ctx, `SELECT sync_version FROM observations WHERE id=?`, item.ID).Scan(&itemVersion); err != nil || itemVersion < 0 {
			_ = rows.Close()
			return nil, writeError(ctx, err)
		}
		snapshot := syncObservation(item)
		observations = append(observations, syncservice.Mutation{RecordID: item.ID, RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationCreate, Observation: &snapshot})
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, writeError(ctx, err)
	}
	_ = rows.Close()
	ordered, err := orderProjectTransitionObservations(observations)
	if err != nil {
		return nil, err
	}
	return append(mutations, ordered...), nil
}

func orderProjectTransitionObservations(observations []syncservice.Mutation) ([]syncservice.Mutation, error) {
	byID := make(map[string]int, len(observations))
	for index, mutation := range observations {
		if mutation.Observation == nil || mutation.RecordID == "" || mutation.Observation.ID != mutation.RecordID {
			return nil, fmt.Errorf("%w: project sync transition observation", ErrCorrupt)
		}
		if _, duplicate := byID[mutation.RecordID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate project sync transition observation", ErrCorrupt)
		}
		byID[mutation.RecordID] = index
	}
	indegree := make([]int, len(observations))
	dependents := make([][]int, len(observations))
	for index, mutation := range observations {
		for _, reference := range mutation.Observation.References {
			dependency, present := byID[reference]
			if !present {
				return nil, fmt.Errorf("%w: project sync transition observation prerequisite", ErrConflict)
			}
			indegree[index]++
			dependents[dependency] = append(dependents[dependency], index)
		}
	}
	ready := make(observationTransitionHeap, 0, len(observations))
	for index, dependencies := range indegree {
		if dependencies == 0 {
			heap.Push(&ready, index)
		}
	}
	ordered := make([]syncservice.Mutation, 0, len(observations))
	for ready.Len() != 0 {
		index := heap.Pop(&ready).(int)
		ordered = append(ordered, observations[index])
		for _, dependent := range dependents[index] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				heap.Push(&ready, dependent)
			}
		}
	}
	if len(ordered) != len(observations) {
		return nil, fmt.Errorf("%w: cyclic project sync transition observation prerequisites", ErrConflict)
	}
	return ordered, nil
}

type observationTransitionHeap []int

func (h observationTransitionHeap) Len() int           { return len(h) }
func (h observationTransitionHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h observationTransitionHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *observationTransitionHeap) Push(value any)    { *h = append(*h, value.(int)) }
func (h *observationTransitionHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	*h = old[:last]
	return value
}

func projectTransitionMutationHash(mutation syncservice.Mutation) ([]byte, error) {
	mutation.MutationID = ""
	payload, err := json.Marshal(mutation)
	if err != nil || len(payload) == 0 || len(payload) > maxSyncPayloadBytes {
		return nil, fmt.Errorf("%w: project sync transition snapshot", ErrInvalid)
	}
	hash := sha256.Sum256(payload)
	return hash[:], nil
}

func projectTransitionCount(result *SyncProjectTransitionResult, kind syncservice.RecordKind) {
	switch kind {
	case syncservice.RecordKindProject:
		result.Projects++
	case syncservice.RecordKindSession:
		result.Sessions++
	case syncservice.RecordKindObservation:
		result.Observations++
	}
}

func clearProjectTransitionSyncState(ctx context.Context, conn *sql.Conn, portableProject, localProject string) error {
	if _, err := conn.ExecContext(ctx, `DELETE FROM sync_outbox_claims WHERE mutation_id IN (SELECT o.mutation_id FROM sync_outbox o WHERE o.record_kind='project' AND o.record_id=? OR o.record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=o.record_id AND s.project_id=?) OR o.record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=o.record_id AND n.project_id=?))`, localProject, localProject, localProject); err != nil {
		return writeError(ctx, err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM sync_outbox WHERE record_kind='project' AND record_id=? OR record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=sync_outbox.record_id AND s.project_id=?) OR record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=sync_outbox.record_id AND n.project_id=?)`, localProject, localProject, localProject); err != nil {
		return writeError(ctx, err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM sync_project_repairs WHERE portable_project_id=?`, portableProject); err != nil {
		return writeError(ctx, err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM sync_push_results WHERE record_kind='project' AND record_id=? OR record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=sync_push_results.record_id AND s.project_id=?) OR record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=sync_push_results.record_id AND n.project_id=?)`, localProject, localProject, localProject); err != nil {
		return writeError(ctx, err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM sync_project_inbox WHERE portable_project_id=?; DELETE FROM sync_project_cursor WHERE portable_project_id=?`, portableProject, portableProject); err != nil {
		return writeError(ctx, err)
	}
	return nil
}

// SyncProjectTransition returns the exact active or completed transition for a
// strict portable/local binding. It never changes local state.
func (s *Store) SyncProjectTransition(ctx context.Context, portableProject, localProject string) (SyncProjectTransitionResult, bool, error) {
	if s == nil || !projectIDPattern.MatchString(portableProject) || localProject == "" {
		return SyncProjectTransitionResult{}, false, fmt.Errorf("%w: project sync transition", ErrInvalid)
	}
	var bound, mode, status string
	var createdAt int64
	var completedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT local_project_id,mode,status,created_at,completed_at FROM sync_project_transitions WHERE portable_project_id=?`, portableProject).Scan(&bound, &mode, &status, &createdAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncProjectTransitionResult{}, false, nil
	}
	if err != nil {
		return SyncProjectTransitionResult{}, false, writeError(ctx, err)
	}
	if bound != localProject || createdAt <= 0 || (mode != string(SyncProjectTransitionReseedSource) && mode != string(SyncProjectTransitionRejoinMerge)) || (status != SyncProjectTransitionPulling && status != SyncProjectTransitionPublishing && status != SyncProjectTransitionCompleted) || (mode == string(SyncProjectTransitionReseedSource) && status == SyncProjectTransitionPulling) || (status == SyncProjectTransitionCompleted && (!completedAt.Valid || completedAt.Int64 <= 0 || completedAt.Int64 < createdAt)) || (status != SyncProjectTransitionCompleted && completedAt.Valid) {
		return SyncProjectTransitionResult{}, false, fmt.Errorf("%w: project sync transition", ErrCorrupt)
	}
	return SyncProjectTransitionResult{SchemaVersion: 1, Mode: SyncProjectTransitionMode(mode), Status: status, TransitionIdentity: createdAt}, true, nil
}

// FinalizeSyncProjectTransition queues only local snapshots that were absent
// from a complete remote pull, or completes publishing after every echo lands.
func (s *Store) FinalizeSyncProjectTransition(ctx context.Context, portableProject, localProject string) (SyncProjectTransitionResult, error) {
	return s.finalizeSyncProjectTransition(ctx, portableProject, localProject, 0)
}

// FinalizeSyncProjectTransitionWithIdentity finalizes only the exact transition
// incarnation selected by a resume plan.
func (s *Store) FinalizeSyncProjectTransitionWithIdentity(ctx context.Context, portableProject, localProject string, transitionIdentity int64) (SyncProjectTransitionResult, error) {
	return s.finalizeSyncProjectTransition(ctx, portableProject, localProject, transitionIdentity)
}

func (s *Store) finalizeSyncProjectTransition(ctx context.Context, portableProject, localProject string, transitionIdentity int64) (SyncProjectTransitionResult, error) {
	if s == nil || s.readOnly || !projectIDPattern.MatchString(portableProject) || localProject == "" {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition", ErrInvalid)
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if transitionIdentity != 0 {
		if err = transitionIdentityMatches(ctx, conn, portableProject, localProject, transitionIdentity); err != nil {
			return SyncProjectTransitionResult{}, err
		}
	}
	var bound, mode, status string
	var createdAt int64
	if err = conn.QueryRowContext(ctx, `SELECT local_project_id,mode,status,created_at FROM sync_project_transitions WHERE portable_project_id=?`, portableProject).Scan(&bound, &mode, &status, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition", ErrNotFound)
		}
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	result := SyncProjectTransitionResult{SchemaVersion: 1, Mode: SyncProjectTransitionMode(mode), Status: status, TransitionIdentity: createdAt}
	if bound != localProject || createdAt <= 0 || result.Mode != SyncProjectTransitionReseedSource && result.Mode != SyncProjectTransitionRejoinMerge {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition", ErrCorrupt)
	}
	if now < createdAt {
		now = createdAt
	}
	if status == SyncProjectTransitionCompleted {
		if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
		committed = true
		return result, nil
	}
	var historyID string
	var position, watermark, cursorUpdated int64
	if err = conn.QueryRowContext(ctx, `SELECT history_id,position,watermark,updated_at FROM sync_project_cursor WHERE portable_project_id=?`, portableProject).Scan(&historyID, &position, &watermark, &cursorUpdated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SyncProjectTransitionResult{}, fmt.Errorf("%w: incomplete project transition pull", ErrConflict)
		}
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	if !canonicalUUIDPattern.MatchString(historyID) || position < 0 || watermark < 0 || position > watermark || cursorUpdated <= 0 {
		return SyncProjectTransitionResult{}, fmt.Errorf("%w: invalid project transition pull cursor", ErrConflict)
	}
	if position < watermark {
		if result.Mode != SyncProjectTransitionRejoinMerge || status != SyncProjectTransitionPublishing {
			return SyncProjectTransitionResult{}, fmt.Errorf("%w: incomplete project transition pull", ErrConflict)
		}
		if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
		committed = true
		return result, nil
	}
	if status == SyncProjectTransitionPulling {
		var projectSeen int
		if err = conn.QueryRowContext(ctx, `SELECT seen_remote FROM sync_project_transition_records WHERE portable_project_id=? AND record_kind='project' AND local_id=?`, portableProject, localProject).Scan(&projectSeen); err != nil || projectSeen != 1 {
			return SyncProjectTransitionResult{}, fmt.Errorf("%w: remote project is absent", ErrConflict)
		}
		rows, queryErr := conn.QueryContext(ctx, `SELECT record_kind,local_id,payload_hash FROM sync_project_transition_records WHERE portable_project_id=? AND seen_remote=0 ORDER BY CASE record_kind WHEN 'session' THEN 1 WHEN 'observation' THEN 2 ELSE 0 END,local_id`, portableProject)
		if queryErr != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, queryErr)
		}
		type unseenRecord struct {
			kind syncservice.RecordKind
			id   string
			hash []byte
		}
		unseen := make(map[string]unseenRecord)
		for rows.Next() {
			var record unseenRecord
			if queryErr = rows.Scan(&record.kind, &record.id, &record.hash); queryErr != nil || len(record.hash) != sha256.Size {
				_ = rows.Close()
				return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition snapshot", ErrCorrupt)
			}
			key := string(record.kind) + "\x00" + record.id
			if _, duplicate := unseen[key]; duplicate {
				_ = rows.Close()
				return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition snapshot", ErrCorrupt)
			}
			unseen[key] = record
		}
		if queryErr = rows.Err(); queryErr != nil {
			_ = rows.Close()
			return SyncProjectTransitionResult{}, writeError(ctx, queryErr)
		}
		_ = rows.Close()
		current, currentErr := projectTransitionMutations(ctx, conn, localProject)
		if currentErr != nil {
			return SyncProjectTransitionResult{}, currentErr
		}
		for _, mutation := range current {
			key := string(mutation.RecordKind) + "\x00" + mutation.RecordID
			record, present := unseen[key]
			if !present {
				continue
			}
			hash, hashErr := projectTransitionMutationHash(mutation)
			if hashErr != nil || !bytes.Equal(hash, record.hash) {
				return SyncProjectTransitionResult{}, fmt.Errorf("%w: local transition record changed", ErrConflict)
			}
			switch record.kind {
			case syncservice.RecordKindSession:
				_, err = conn.ExecContext(ctx, `UPDATE sessions SET sync_version=0 WHERE id=? AND project_id=?`, record.id, localProject)
			case syncservice.RecordKindObservation:
				_, err = conn.ExecContext(ctx, `UPDATE observations SET sync_version=0 WHERE id=? AND project_id=?`, record.id, localProject)
			default:
				return SyncProjectTransitionResult{}, fmt.Errorf("%w: project sync transition snapshot", ErrCorrupt)
			}
			if err != nil {
				return SyncProjectTransitionResult{}, writeError(ctx, err)
			}
			mutation.MutationID = ""
			if _, err = s.enqueueSyncOutbox(ctx, conn, mutation); err != nil {
				return SyncProjectTransitionResult{}, err
			}
			projectTransitionCount(&result, record.kind)
			result.Queued++
			delete(unseen, key)
		}
		if len(unseen) != 0 {
			return SyncProjectTransitionResult{}, fmt.Errorf("%w: local transition record changed", ErrConflict)
		}
		status = SyncProjectTransitionPublishing
		result.Status = status
		if _, err = conn.ExecContext(ctx, `UPDATE sync_project_transitions SET status='publishing' WHERE portable_project_id=? AND status='pulling'`, portableProject); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
	}
	if status == SyncProjectTransitionPublishing && result.Mode == SyncProjectTransitionRejoinMerge {
		if err = s.recoverRejectedProjectTransitionObservations(ctx, conn, portableProject, localProject, &result); err != nil {
			return SyncProjectTransitionResult{}, err
		}
	}
	var pending, unseen int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox o WHERE o.record_kind='project' AND o.record_id=? OR o.record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=o.record_id AND s.project_id=?) OR o.record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=o.record_id AND n.project_id=?)`, localProject, localProject, localProject).Scan(&pending); err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_project_transition_records WHERE portable_project_id=? AND seen_remote=0`, portableProject).Scan(&unseen); err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	if pending == 0 && unseen == 0 {
		if _, err = conn.ExecContext(ctx, `UPDATE sync_project_transitions SET status='completed',completed_at=? WHERE portable_project_id=? AND status='publishing'`, now, portableProject); err != nil {
			return SyncProjectTransitionResult{}, writeError(ctx, err)
		}
		result.Status = SyncProjectTransitionCompleted
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return SyncProjectTransitionResult{}, writeError(ctx, err)
	}
	committed = true
	return result, nil
}

func (s *Store) recoverRejectedProjectTransitionObservations(ctx context.Context, conn *sql.Conn, portableProject, localProject string, result *SyncProjectTransitionResult) error {
	rows, err := conn.QueryContext(ctx, `SELECT r.local_id,r.payload_hash FROM sync_project_transition_records r
		WHERE r.portable_project_id=? AND r.record_kind='observation' AND r.seen_remote=0
		AND NOT EXISTS (SELECT 1 FROM sync_outbox o WHERE o.record_kind='observation' AND o.record_id=r.local_id)
		ORDER BY r.local_id`, portableProject)
	if err != nil {
		return writeError(ctx, err)
	}
	candidates := make(map[string][]byte)
	for rows.Next() {
		var id string
		var hash []byte
		if err = rows.Scan(&id, &hash); err != nil || id == "" || len(hash) != sha256.Size {
			_ = rows.Close()
			return fmt.Errorf("%w: project sync transition recovery snapshot", ErrCorrupt)
		}
		if _, duplicate := candidates[id]; duplicate {
			_ = rows.Close()
			return fmt.Errorf("%w: project sync transition recovery snapshot", ErrCorrupt)
		}
		candidates[id] = append([]byte(nil), hash...)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return writeError(ctx, err)
	}
	_ = rows.Close()
	if len(candidates) == 0 {
		return nil
	}
	current, err := projectTransitionMutations(ctx, conn, localProject)
	if err != nil {
		return err
	}
	for _, mutation := range current {
		snapshotHash, recoverable := candidates[mutation.RecordID]
		if mutation.RecordKind != syncservice.RecordKindObservation || !recoverable {
			continue
		}
		if mutation.Kind != syncservice.MutationCreate || mutation.BaseVersion != 0 || mutation.Observation == nil || mutation.Observation.ProjectID != localProject {
			return fmt.Errorf("%w: project sync transition recovery observation", ErrConflict)
		}
		hash, hashErr := projectTransitionMutationHash(mutation)
		if hashErr != nil || !bytes.Equal(hash, snapshotHash) {
			return fmt.Errorf("%w: project sync transition recovery snapshot", ErrConflict)
		}
		var receipts, accepted int
		if err = conn.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(CASE WHEN disposition IN ('accepted','previously_accepted') THEN 1 ELSE 0 END),0)
			FROM sync_push_results WHERE record_kind='observation' AND record_id=? AND mutation_kind='create'`, mutation.RecordID).Scan(&receipts, &accepted); err != nil {
			return writeError(ctx, err)
		}
		if accepted != 0 {
			delete(candidates, mutation.RecordID)
			continue
		}
		if receipts != 1 {
			return fmt.Errorf("%w: project sync transition recovery receipt count", ErrConflict)
		}
		var receiptID, disposition, code string
		var retryable, canonical, base int64
		var sequence sql.NullInt64
		var receiptHash []byte
		if err = conn.QueryRowContext(ctx, `SELECT mutation_id,disposition,retryable,code,sequence,canonical_version,base_version,mutation_hash
			FROM sync_push_results WHERE record_kind='observation' AND record_id=? AND mutation_kind='create'`, mutation.RecordID).Scan(&receiptID, &disposition, &retryable, &code, &sequence, &canonical, &base, &receiptHash); err != nil {
			return writeError(ctx, err)
		}
		if !canonicalUUIDPattern.MatchString(receiptID) || disposition != string(syncservice.DispositionRejected) || retryable != 0 || code != "invalid_prerequisite" || sequence.Valid || canonical != 0 || base != 0 || len(receiptHash) != sha256.Size {
			return fmt.Errorf("%w: project sync transition recovery receipt", ErrConflict)
		}
		receiptMutation := mutation
		receiptMutation.MutationID = receiptID
		expectedReceiptHash, hashErr := syncMutationHash(receiptMutation)
		if hashErr != nil || !bytes.Equal(expectedReceiptHash, receiptHash) {
			return fmt.Errorf("%w: project sync transition recovery receipt", ErrConflict)
		}
		var version int64
		if err = conn.QueryRowContext(ctx, `SELECT sync_version FROM observations WHERE id=? AND project_id=?`, mutation.RecordID, localProject).Scan(&version); err != nil || version != 0 {
			return fmt.Errorf("%w: project sync transition recovery observation version", ErrConflict)
		}
		if mutation.Observation.SessionID != "" {
			var sessionVersion int64
			if err = conn.QueryRowContext(ctx, `SELECT sync_version FROM sessions WHERE id=? AND project_id=?`, mutation.Observation.SessionID, localProject).Scan(&sessionVersion); err != nil || sessionVersion <= 0 {
				return fmt.Errorf("%w: project sync transition recovery session prerequisite", ErrConflict)
			}
		}
		for _, reference := range mutation.Observation.References {
			var referenceProject string
			var referenceVersion int64
			if err = conn.QueryRowContext(ctx, `SELECT project_id,sync_version FROM observations WHERE id=?`, reference).Scan(&referenceProject, &referenceVersion); err != nil || referenceProject != localProject || referenceVersion < 0 {
				return fmt.Errorf("%w: project sync transition recovery observation prerequisite", ErrConflict)
			}
			if referenceVersion == 0 {
				var queued int
				if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox WHERE record_kind='observation' AND record_id=? AND mutation_kind='create' AND base_version=0`, reference).Scan(&queued); err != nil {
					return writeError(ctx, err)
				}
				if queued != 1 {
					return fmt.Errorf("%w: project sync transition recovery observation prerequisite", ErrConflict)
				}
			}
		}
		mutation.MutationID = ""
		if _, err = s.enqueueSyncOutbox(ctx, conn, mutation); err != nil {
			return err
		}
		result.Observations++
		result.Queued++
		delete(candidates, mutation.RecordID)
	}
	if len(candidates) != 0 {
		return fmt.Errorf("%w: project sync transition recovery observation", ErrConflict)
	}
	return nil
}

// RepairBoundProjectCreate records one operator-confirmed, local-only repair
// for a portable project that is absent remotely. It never resets local state
// or reads credentials; a later ordinary foreground sync sends the new create.
func (s *Store) RepairBoundProjectCreate(ctx context.Context, portableProject, localProject string, confirmedRemoteAbsent bool) (SyncProjectRepairResult, error) {
	if s == nil || s.readOnly || !confirmedRemoteAbsent || !projectIDPattern.MatchString(portableProject) || localProject == "" {
		return SyncProjectRepairResult{}, fmt.Errorf("%w: project sync repair", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return SyncProjectRepairResult{}, err
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return SyncProjectRepairResult{}, fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return SyncProjectRepairResult{}, writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	var status, existingLocal string
	err = conn.QueryRowContext(ctx, `SELECT status,local_project_id FROM sync_project_repairs WHERE portable_project_id=?`, portableProject).Scan(&status, &existingLocal)
	if err == nil {
		if existingLocal != localProject || (status != "pending" && status != "completed" && status != "rejected") {
			return SyncProjectRepairResult{}, fmt.Errorf("%w: project sync repair", ErrConflict)
		}
		if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
			return SyncProjectRepairResult{}, writeError(ctx, err)
		}
		committed = true
		return SyncProjectRepairResult{SchemaVersion: 1, Status: status}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	var bound string
	err = conn.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE portable_id=?`, portableProject).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) || bound != localProject {
		return SyncProjectRepairResult{}, fmt.Errorf("%w: portable project identity binding", ErrConflict)
	}
	if err != nil {
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	if err = activeProjectTransitionForLocalProject(ctx, conn, localProject); err != nil {
		return SyncProjectRepairResult{}, err
	}
	var version, originals int64
	if err = conn.QueryRowContext(ctx, `SELECT sync_version FROM projects WHERE id=?`, localProject).Scan(&version); err != nil || version != 1 {
		if errors.Is(err, sql.ErrNoRows) {
			return SyncProjectRepairResult{}, fmt.Errorf("%w: project sync repair", ErrConflict)
		}
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_push_results WHERE disposition='accepted' AND retryable=0 AND record_kind='project' AND record_id=? AND mutation_kind='create' AND base_version=0 AND canonical_version=1`, localProject).Scan(&originals); err != nil {
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	if originals != 1 {
		return SyncProjectRepairResult{}, fmt.Errorf("%w: project sync repair receipt", ErrConflict)
	}
	var activeClaims int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox o JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id WHERE c.lease_until>? AND (o.record_kind='project' AND o.record_id=? OR o.record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=o.record_id AND s.project_id=?) OR o.record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=o.record_id AND n.project_id=?))`, now, localProject, localProject, localProject).Scan(&activeClaims); err != nil {
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	if activeClaims != 0 {
		return SyncProjectRepairResult{}, fmt.Errorf("%w: active project sync claim", ErrConflict)
	}
	var original string
	if err = conn.QueryRowContext(ctx, `SELECT mutation_id FROM sync_push_results WHERE disposition='accepted' AND retryable=0 AND record_kind='project' AND record_id=? AND mutation_kind='create' AND base_version=0 AND canonical_version=1`, localProject).Scan(&original); err != nil || !canonicalUUIDPattern.MatchString(original) {
		return SyncProjectRepairResult{}, fmt.Errorf("%w: project sync repair receipt", ErrCorrupt)
	}
	id, err := newSyncUUID()
	if err != nil {
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	mutation := syncservice.Mutation{MutationID: id, RecordID: localProject, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: localProject}}
	if _, err = s.enqueueSyncOutbox(ctx, conn, mutation); err != nil {
		return SyncProjectRepairResult{}, err
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO sync_project_repairs(portable_project_id,local_project_id,original_mutation_id,repair_mutation_id,status,terminal_code,created_at,completed_at) VALUES(?,?,?,?, 'pending','',?,NULL)`, portableProject, localProject, original, id, now); err != nil {
		return SyncProjectRepairResult{}, conflictOrWrite(ctx, err)
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return SyncProjectRepairResult{}, writeError(ctx, err)
	}
	committed = true
	return SyncProjectRepairResult{SchemaVersion: 1, Status: "pending", Queued: 1}, nil
}

func pendingProjectRepairBarrier(ctx context.Context, tx *sql.Tx) error {
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_project_repairs WHERE status='pending'`).Scan(&pending); err != nil {
		return writeError(ctx, err)
	}
	if pending != 0 {
		return fmt.Errorf("%w: project repair is pending", ErrSyncProjectRepairPending)
	}
	return nil
}

type projectTransitionQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func activeProjectTransitionForLocalProject(ctx context.Context, query projectTransitionQuery, localProject string) error {
	return activeProjectTransitionForLocalProjectExceptIntent(ctx, query, localProject, "")
}

func activeProjectTransitionForLocalProjectExceptIntent(ctx context.Context, query projectTransitionQuery, localProject, exceptPortable string) error {
	var active int
	if err := query.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM sync_project_transitions WHERE local_project_id=? AND status IN ('pulling','publishing')) + (SELECT count(*) FROM sync_project_backup_intents WHERE local_project_id=? AND portable_project_id<>?)`, localProject, localProject, exceptPortable).Scan(&active); err != nil {
		return writeError(ctx, err)
	}
	if active != 0 {
		return fmt.Errorf("%w: project sync transition pending", ErrConflict)
	}
	return nil
}

func pendingProjectRepairForMutation(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation) error {
	var project string
	err := tx.QueryRowContext(ctx, `SELECT CASE ? WHEN 'project' THEN ? WHEN 'session' THEN (SELECT project_id FROM sessions WHERE id=?) WHEN 'observation' THEN (SELECT project_id FROM observations WHERE id=?) END`, mutation.RecordKind, mutation.RecordID, mutation.RecordID, mutation.RecordID).Scan(&project)
	if err != nil || project == "" {
		return fmt.Errorf("%w: sync mutation project", ErrCorrupt)
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_project_repairs WHERE status='pending' AND local_project_id=?`, project).Scan(&pending); err != nil {
		return writeError(ctx, err)
	}
	if pending != 0 {
		return fmt.Errorf("%w: project repair is pending", ErrSyncProjectRepairPending)
	}
	return nil
}

func (s *Store) pendingProjectRepairPreflight(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	if err = pendingProjectRepairBarrier(ctx, tx); err != nil {
		return err
	}
	return commit(ctx, tx)
}

func pendingProjectRepairForProject(ctx context.Context, tx *sql.Tx, portableProject, localProject string) error {
	var bound string
	err := tx.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE portable_id=?`, portableProject).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) || bound != localProject {
		return fmt.Errorf("%w: portable project identity binding", ErrConflict)
	}
	if err != nil {
		return writeError(ctx, err)
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM sync_project_repairs WHERE portable_project_id=? AND local_project_id=?`, portableProject, localProject).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return writeError(ctx, err)
	}
	if status == "pending" {
		return fmt.Errorf("%w: project repair is pending", ErrSyncProjectRepairPending)
	}
	if status != "completed" && status != "rejected" {
		return fmt.Errorf("%w: project sync repair", ErrCorrupt)
	}
	return nil
}

// PendingProjectRepair fails closed only while the exact bound repair is pending.
func (s *Store) PendingProjectRepair(ctx context.Context, portableProject, localProject string) error {
	mutationID, err := s.PendingProjectRepairMutation(ctx, portableProject, localProject)
	if err != nil {
		return err
	}
	if mutationID != "" {
		return fmt.Errorf("%w: project repair is pending", ErrSyncProjectRepairPending)
	}
	return nil
}

// PendingProjectRepairMutation returns only the exact pending replacement
// create for a strict portable/local binding; terminal and absent repairs yield
// an empty ID.
func (s *Store) PendingProjectRepairMutation(ctx context.Context, portableProject, localProject string) (string, error) {
	if s == nil || !projectIDPattern.MatchString(portableProject) || localProject == "" {
		return "", fmt.Errorf("%w: project sync repair", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", writeError(ctx, err)
	}
	defer tx.Rollback()
	var bound string
	err = tx.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE portable_id=?`, portableProject).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) || bound != localProject {
		return "", fmt.Errorf("%w: portable project identity binding", ErrConflict)
	}
	if err != nil {
		return "", writeError(ctx, err)
	}
	var mutationID, status string
	err = tx.QueryRowContext(ctx, `SELECT repair_mutation_id,status FROM sync_project_repairs WHERE portable_project_id=? AND local_project_id=?`, portableProject, localProject).Scan(&mutationID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", commit(ctx, tx)
	}
	if err != nil {
		return "", writeError(ctx, err)
	}
	if status == "pending" && canonicalUUIDPattern.MatchString(mutationID) {
		return mutationID, commit(ctx, tx)
	}
	if status != "completed" && status != "rejected" {
		return "", fmt.Errorf("%w: project sync repair", ErrCorrupt)
	}
	return "", commit(ctx, tx)
}

func backfillMutationID(kind, id string) string {
	sum := sha256.Sum256([]byte("vgxness/sync-backfill/v1\x00" + kind + "\x00" + id))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (s *Store) enqueueBackfillMutation(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation) (bool, error) {
	var rowID, baseVersion, payloadVersion, attempts, nextAttempt, created, updated int64
	var id, recordKind, recordID, kind, state, lastErrorCode string
	var payload []byte
	err := tx.QueryRowContext(ctx, `SELECT id,mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,last_error_code,next_attempt_at,created_at,updated_at FROM sync_outbox WHERE record_kind=? AND record_id=? AND mutation_kind='create' AND base_version=0 ORDER BY id LIMIT 1`, mutation.RecordKind, mutation.RecordID).Scan(&rowID, &id, &recordKind, &recordID, &kind, &baseVersion, &payloadVersion, &payload, &state, &attempts, &lastErrorCode, &nextAttempt, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.enqueueSyncOutbox(ctx, tx, mutation)
		return err == nil, err
	}
	if err != nil {
		return false, writeError(ctx, err)
	}
	if rowID < 1 || !canonicalUUIDPattern.MatchString(id) || len(payload) == 0 || len(payload) > maxSyncPayloadBytes {
		return false, fmt.Errorf("%w: invalid sync outbox payload", ErrCorrupt)
	}
	if state == string(SyncOutboxRetry) || attempts > 0 {
		return false, fmt.Errorf("%w: sync backfill was attempted", ErrConflict)
	}
	existing, err := decodeSyncOutboxEntry(id, recordKind, recordID, kind, baseVersion, payloadVersion, payload, state, attempts, lastErrorCode, nextAttempt, created, updated)
	if err != nil {
		return false, fmt.Errorf("%w: sync backfill identity", ErrConflict)
	}
	mutation.MutationID = id
	if !allowedSyncMutation(mutation) || syncservice.ValidateMutation(mutation) != nil {
		return false, fmt.Errorf("%w: invalid sync backfill mutation", ErrCorrupt)
	}
	expected, err := json.Marshal(mutation)
	if err != nil || len(expected) > maxSyncPayloadBytes {
		return false, fmt.Errorf("%w: invalid sync backfill payload", ErrCorrupt)
	}
	if bytes.Equal(payload, expected) {
		return false, nil
	}
	var claimHistory int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox_claims WHERE mutation_id=?`, id).Scan(&claimHistory); err != nil {
		return false, writeError(ctx, err)
	}
	if claimHistory != 0 {
		return false, fmt.Errorf("%w: sync backfill was claimed", ErrConflict)
	}
	if existing.Mutation.MutationID != mutation.MutationID || existing.Mutation.RecordKind != mutation.RecordKind || existing.Mutation.RecordID != mutation.RecordID || existing.Mutation.Kind != mutation.Kind || existing.Mutation.BaseVersion != mutation.BaseVersion {
		return false, fmt.Errorf("%w: sync backfill identity", ErrConflict)
	}
	result, err := tx.ExecContext(ctx, `UPDATE sync_outbox SET payload=? WHERE id=? AND mutation_id=? AND record_kind=? AND record_id=? AND mutation_kind=? AND base_version=? AND payload_version=? AND payload=? AND state=? AND attempts=? AND last_error_code=? AND next_attempt_at=? AND created_at=? AND updated_at=? AND NOT EXISTS (SELECT 1 FROM sync_outbox_claims WHERE mutation_id=?)`, expected, rowID, id, recordKind, recordID, kind, baseVersion, payloadVersion, payload, state, attempts, lastErrorCode, nextAttempt, created, updated, id)
	if err != nil {
		return false, writeError(ctx, err)
	}
	repaired, err := result.RowsAffected()
	if err != nil {
		return false, writeError(ctx, err)
	}
	if repaired != 1 {
		return false, fmt.Errorf("%w: sync backfill changed concurrently", ErrConflict)
	}
	return false, nil
}

const (
	maxSyncPayloadBytes = 1 << 20
	maxDueSyncOutbox    = 16
	maxBootstrapPulls   = 1024
)

var (
	canonicalUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	syncErrorCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	credentialProvider   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)
	credentialSegment    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	bearerReference      = regexp.MustCompile(`(?i)vgx1\.[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[a-z0-9_-]+`)
)

type SyncProfile struct {
	Enabled               bool
	Endpoint              string
	DeviceID              string
	CredentialRef         string
	PreviousCredentialRef string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// SyncCredentialStatus is a token-free description of credential availability.
type SyncCredentialStatus string

const (
	SyncCredentialNotConfigured SyncCredentialStatus = "not_configured"
	SyncCredentialAvailable     SyncCredentialStatus = "available"
	SyncCredentialMissing       SyncCredentialStatus = "missing"
	SyncCredentialUnavailable   SyncCredentialStatus = "unavailable"
	SyncCredentialInvalid       SyncCredentialStatus = "invalid"
)

// SyncConfigurationStatus is a read-only, token-free local enrollment summary.
type SyncConfigurationStatus struct {
	Configured bool                 `json:"configured"`
	Enabled    bool                 `json:"enabled"`
	Credential SyncCredentialStatus `json:"credential"`
}

// SyncStatus is a token-free summary of one bounded foreground synchronization.
type SyncStatus string

const (
	SyncStatusAbsent                SyncStatus = "absent"
	SyncStatusDisabled              SyncStatus = "disabled"
	SyncStatusUnavailable           SyncStatus = "unavailable"
	SyncStatusUnreachable           SyncStatus = "unreachable"
	SyncStatusCredentialMissing     SyncStatus = "credential_missing"
	SyncStatusCredentialUnavailable SyncStatus = "credential_unavailable"
	SyncStatusIncompatible          SyncStatus = "incompatible"
	SyncStatusInvalid               SyncStatus = "invalid"
	SyncStatusUnauthorized          SyncStatus = "unauthorized"
	SyncStatusPartial               SyncStatus = "partial"
	SyncStatusRejected              SyncStatus = "rejected"
	SyncStatusConflict              SyncStatus = "conflict"
	SyncStatusSynced                SyncStatus = "synced"
)

// SyncMode identifies the operation boundary that produced a sync result.
type SyncMode string

const (
	// SyncModeProjectBidirectional reports project-scoped foreground push and
	// pull without touching owner-global sync state.
	SyncModeProjectBidirectional SyncMode = "project_bidirectional"
)

// SyncResult contains only durable outcome counts; it never carries a credential.
type SyncResult struct {
	Mode               SyncMode   `json:"mode,omitempty"`
	Status             SyncStatus `json:"status"`
	Pushed             int        `json:"pushed"`
	PreviouslyAccepted int        `json:"previouslyAccepted"`
	Rejected           int        `json:"rejected"`
	Retried            int        `json:"retried"`
	Conflicts          int        `json:"conflicts"`
	Batches            int        `json:"batches"`
	FailureOperation   string     `json:"failureOperation,omitempty"`
	FailureHTTPStatus  int        `json:"failureHttpStatus,omitempty"`
	FailureClass       string     `json:"failureClass,omitempty"`
}

// SyncQueueSummary reports whether durable local work blocks ordinary bootstrap.
type SyncQueueSummary struct {
	Work     bool
	Conflict bool
}

func (s *Store) SyncQueueSummary(ctx context.Context) (SyncQueueSummary, error) {
	if s == nil || ctx == nil {
		return SyncQueueSummary{}, fmt.Errorf("%w: sync queue", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return SyncQueueSummary{}, err
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.closed || s.db == nil {
		return SyncQueueSummary{}, fmt.Errorf("%w: store closed", ErrCorrupt)
	}
	if s.readOnly {
		return SyncQueueSummary{}, fmt.Errorf("%w: store is read-only", ErrConflict)
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return SyncQueueSummary{}, fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	var work, conflict int
	err := s.db.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM sync_outbox) OR EXISTS(SELECT 1 FROM sync_outbox_claims WHERE lease_until>?),
		EXISTS(SELECT 1 FROM sync_conflicts WHERE status='unresolved')`, now).Scan(&work, &conflict)
	if err != nil {
		return SyncQueueSummary{}, writeError(ctx, err)
	}
	return SyncQueueSummary{Work: work != 0, Conflict: conflict != 0}, nil
}

// SyncQueueSummaryForProject reports durable work and conflicts owned by one
// project, without allowing other projects to block its foreground sync.
func (s *Store) SyncQueueSummaryForProject(ctx context.Context, project string) (SyncQueueSummary, error) {
	if s == nil || ctx == nil || project == "" {
		return SyncQueueSummary{}, fmt.Errorf("%w: sync project queue", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return SyncQueueSummary{}, err
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.closed || s.db == nil {
		return SyncQueueSummary{}, fmt.Errorf("%w: store closed", ErrCorrupt)
	}
	if s.readOnly {
		return SyncQueueSummary{}, fmt.Errorf("%w: store is read-only", ErrConflict)
	}
	owner := `(record_kind='project' AND record_id=?) OR (record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=record_id AND s.project_id=?)) OR (record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=record_id AND n.project_id=?))`
	var work, conflict int
	err := s.db.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM sync_outbox WHERE `+owner+`),
		EXISTS(SELECT 1 FROM sync_conflicts WHERE status='unresolved' AND (`+owner+`))`, project, project, project, project, project, project).Scan(&work, &conflict)
	if err != nil {
		return SyncQueueSummary{}, writeError(ctx, err)
	}
	return SyncQueueSummary{Work: work != 0, Conflict: conflict != 0}, nil
}

// PendingOwnConflictReceipts returns bounded, unmaterialized own conflicts that
// still have later local work for the same record.
func (s *Store) PendingOwnConflictReceipts(ctx context.Context) ([]string, error) {
	if s == nil || ctx == nil {
		return nil, fmt.Errorf("%w: own conflict recovery", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.closed || s.db == nil || s.readOnly {
		return nil, fmt.Errorf("%w: own conflict recovery", ErrConflict)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.mutation_id FROM sync_push_results r WHERE r.disposition='conflict' AND r.retryable=0 AND r.sequence>0 AND EXISTS (SELECT 1 FROM sync_outbox o WHERE o.record_kind=r.record_kind AND o.record_id=r.record_id) AND NOT EXISTS (SELECT 1 FROM sync_conflicts f WHERE f.competing_version_id=r.mutation_id) ORDER BY r.sequence,r.mutation_id LIMIT 16`)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil || !canonicalUUIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: own conflict receipt", ErrCorrupt)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, writeError(ctx, err)
	}
	return ids, nil
}

type SyncOutboxState string

const (
	SyncOutboxPending SyncOutboxState = "pending"
	SyncOutboxRetry   SyncOutboxState = "retry"
)

type SyncOutboxEntry struct {
	Mutation      syncservice.Mutation
	State         SyncOutboxState
	Attempts      int64
	LastErrorCode string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SyncOutboxDiagnostic is safe to use for local queue inspection. It never
// contains a mutation payload and therefore cannot be used to send work.
type SyncOutboxDiagnostic struct {
	MutationID    string
	RecordKind    syncservice.RecordKind
	RecordID      string
	State         SyncOutboxState
	Attempts      int64
	LastErrorCode string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SyncOutboxClaim is the sole exported path that returns a sendable mutation.
type SyncOutboxClaim struct {
	SyncOutboxEntry
	ClaimToken      string
	FirstClaimToken string
	FirstClaimedAt  time.Time
	ClaimedAt       time.Time
	LeaseUntil      time.Time
}

// TranslateSyncMutations creates a wire-only portable identity view. It never
// changes claimed mutations, outbox payload bytes, or local identifiers.
func (s *Store) TranslateSyncMutations(ctx context.Context, portableProjectID, expectedLocalProject string, mutations []syncservice.Mutation) ([]syncservice.Mutation, error) {
	return s.translateSyncMutations(ctx, portableProjectID, expectedLocalProject, nil, mutations)
}

// TranslateSyncMutationsForTransition creates a wire-only portable identity view
// only while the exact transition incarnation remains active.
func (s *Store) TranslateSyncMutationsForTransition(ctx context.Context, portableProjectID, expectedLocalProject string, transitionIdentity int64, mutations []syncservice.Mutation) ([]syncservice.Mutation, error) {
	return s.translateSyncMutations(ctx, portableProjectID, expectedLocalProject, &transitionIdentity, mutations)
}

func (s *Store) translateSyncMutations(ctx context.Context, portableProjectID, expectedLocalProject string, transitionIdentity *int64, mutations []syncservice.Mutation) ([]syncservice.Mutation, error) {
	if !projectIDPattern.MatchString(portableProjectID) || expectedLocalProject == "" {
		return nil, fmt.Errorf("%w: portable project identity", ErrInvalid)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return nil, writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if transitionIdentity != nil {
		if err := activeTransitionIdentityMatches(ctx, conn, portableProjectID, expectedLocalProject, *transitionIdentity); err != nil {
			return nil, err
		}
	}
	var boundProject string
	err = conn.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE portable_id=?`, portableProjectID).Scan(&boundProject)
	if errors.Is(err, sql.ErrNoRows) || boundProject != expectedLocalProject {
		return nil, fmt.Errorf("%w: portable project identity binding", ErrConflict)
	}
	if err != nil {
		return nil, writeError(ctx, err)
	}
	var device string
	if err := conn.QueryRowContext(ctx, `SELECT device_id FROM sync_profiles WHERE singleton=1`).Scan(&device); err != nil || !canonicalUUIDPattern.MatchString(device) {
		return nil, fmt.Errorf("%w: sync portable identity device", ErrCorrupt)
	}
	translated := make([]syncservice.Mutation, len(mutations))
	for i, mutation := range mutations {
		copy := cloneSyncMutation(mutation)
		if err := s.translateSyncMutation(ctx, conn, portableProjectID, expectedLocalProject, device, &copy); err != nil {
			return nil, err
		}
		if err := syncservice.ValidateMutation(copy); err != nil {
			return nil, fmt.Errorf("%w: translated mutation", ErrInvalid)
		}
		translated[i] = copy
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, writeError(ctx, err)
	}
	committed = true
	return translated, nil
}

func cloneSyncMutation(m syncservice.Mutation) syncservice.Mutation {
	copy := m
	if m.Project != nil {
		value := *m.Project
		copy.Project = &value
	}
	if m.Session != nil {
		value := *m.Session
		copy.Session = &value
	}
	if m.Observation != nil {
		value := *m.Observation
		value.References = append([]string(nil), m.Observation.References...)
		copy.Observation = &value
	}
	if m.Tombstone != nil {
		value := *m.Tombstone
		copy.Tombstone = &value
	}
	if m.Resolution != nil {
		value := *m.Resolution
		value.ConflictIDs = append([]string(nil), m.Resolution.ConflictIDs...)
		if m.Resolution.Observation != nil {
			observation := *m.Resolution.Observation
			observation.References = append([]string(nil), m.Resolution.Observation.References...)
			value.Observation = &observation
		}
		copy.Resolution = &value
	}
	return copy
}

type syncSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) translateSyncMutation(ctx context.Context, tx syncSQL, project, expectedLocalProject, device string, mutation *syncservice.Mutation) error {
	if mutation.Kind == syncservice.MutationResolve {
		return fmt.Errorf("%w: sync portable identity resolve", ErrInvalid)
	}
	mapID := func(kind syncservice.RecordKind, local string) (string, error) {
		return s.ensureSyncPortableIdentity(ctx, tx, project, kind, local, device)
	}
	switch mutation.RecordKind {
	case syncservice.RecordKindProject:
		if mutation.RecordID != expectedLocalProject || mutation.Project == nil || mutation.Project.ID != expectedLocalProject {
			return fmt.Errorf("%w: project mutation", ErrCorrupt)
		}
		mutation.RecordID, mutation.Project.ID = project, project
	case syncservice.RecordKindSession:
		if mutation.Session == nil || mutation.Session.ID != mutation.RecordID || mutation.Session.ProjectID != expectedLocalProject || !s.localSyncRecordBelongsToProject(ctx, tx, syncservice.RecordKindSession, mutation.RecordID, expectedLocalProject) {
			return fmt.Errorf("%w: session mutation", ErrCorrupt)
		}
		id, err := mapID(syncservice.RecordKindSession, mutation.RecordID)
		if err != nil {
			return err
		}
		mutation.RecordID, mutation.Session.ID, mutation.Session.ProjectID = id, id, project
	case syncservice.RecordKindObservation:
		localID := mutation.RecordID
		var id string
		var err error
		if mutation.Kind == syncservice.MutationTombstone {
			id, err = s.findSyncPortableIdentity(ctx, tx, project, syncservice.RecordKindObservation, localID)
		} else {
			id, err = mapID(syncservice.RecordKindObservation, localID)
		}
		if err != nil {
			return err
		}
		mutation.RecordID = id
		if mutation.Observation != nil {
			if mutation.Observation.ID != localID || mutation.Observation.ProjectID != expectedLocalProject || !s.localSyncRecordBelongsToProject(ctx, tx, syncservice.RecordKindObservation, localID, expectedLocalProject) {
				return fmt.Errorf("%w: observation mutation", ErrCorrupt)
			}
			mutation.Observation.ID, mutation.Observation.ProjectID = id, project
			if mutation.Observation.SessionID != "" {
				if !s.localSyncRecordBelongsToProject(ctx, tx, syncservice.RecordKindSession, mutation.Observation.SessionID, expectedLocalProject) {
					return fmt.Errorf("%w: observation session", ErrConflict)
				}
				mapped, err := mapID(syncservice.RecordKindSession, mutation.Observation.SessionID)
				if err != nil {
					return err
				}
				mutation.Observation.SessionID = mapped
			}
			for i, reference := range mutation.Observation.References {
				if !s.localSyncRecordBelongsToProject(ctx, tx, syncservice.RecordKindObservation, reference, expectedLocalProject) {
					return fmt.Errorf("%w: observation reference", ErrConflict)
				}
				mapped, err := mapID(syncservice.RecordKindObservation, reference)
				if err != nil {
					return err
				}
				mutation.Observation.References[i] = mapped
			}
		}
	default:
		return fmt.Errorf("%w: sync portable identity kind", ErrInvalid)
	}
	return nil
}

func (s *Store) localSyncRecordBelongsToProject(ctx context.Context, tx syncSQL, kind syncservice.RecordKind, id, project string) bool {
	table := ""
	switch kind {
	case syncservice.RecordKindSession:
		table = "sessions"
	case syncservice.RecordKindObservation:
		table = "observations"
	default:
		return false
	}
	var found string
	return tx.QueryRowContext(ctx, `SELECT project_id FROM `+table+` WHERE id=?`, id).Scan(&found) == nil && found == project
}

func (s *Store) findSyncPortableIdentity(ctx context.Context, tx syncSQL, project string, kind syncservice.RecordKind, local string) (string, error) {
	if len([]byte(local)) == 0 || len([]byte(local)) > 1024 {
		return "", fmt.Errorf("%w: sync portable identity", ErrInvalid)
	}
	var portable, origin string
	err := tx.QueryRowContext(ctx, `SELECT portable_id,origin_device_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND local_id=?`, project, kind, local).Scan(&portable, &origin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: sync portable identity unavailable", ErrNotFound)
	}
	if err != nil {
		return "", writeError(ctx, err)
	}
	if !canonicalUUIDPattern.MatchString(portable) || !canonicalUUIDPattern.MatchString(origin) {
		return "", fmt.Errorf("%w: sync portable identity", ErrCorrupt)
	}
	return s.validSyncPortableIdentity(ctx, tx, project, kind, local, portable, origin)
}

func (s *Store) ensureSyncPortableIdentity(ctx context.Context, tx syncSQL, project string, kind syncservice.RecordKind, local, device string) (string, error) {
	if len([]byte(local)) == 0 || len([]byte(local)) > 1024 || (kind != syncservice.RecordKindSession && kind != syncservice.RecordKindObservation) {
		return "", fmt.Errorf("%w: sync portable identity", ErrInvalid)
	}
	want := portableSyncUUID(project, string(kind), local)
	var existing, origin string
	err := tx.QueryRowContext(ctx, `SELECT portable_id,origin_device_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND local_id=?`, project, kind, local).Scan(&existing, &origin)
	if err == nil {
		if !canonicalUUIDPattern.MatchString(origin) {
			return "", fmt.Errorf("%w: sync portable identity", ErrCorrupt)
		}
		return s.validSyncPortableIdentity(ctx, tx, project, kind, local, existing, origin)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", writeError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO sync_portable_identities(portable_project_id,record_kind,local_id,portable_id,origin_device_id,created_at) VALUES(?,?,?,?,?,?)`, project, kind, local, want, device, s.now().UTC().UnixNano())
	if err != nil {
		return "", conflictOrWrite(ctx, err)
	}
	err = tx.QueryRowContext(ctx, `SELECT portable_id,origin_device_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND local_id=?`, project, kind, local).Scan(&existing, &origin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: sync portable identity collision", ErrConflict)
		}
		return "", writeError(ctx, err)
	}
	if !canonicalUUIDPattern.MatchString(origin) {
		return "", fmt.Errorf("%w: sync portable identity", ErrCorrupt)
	}
	if !canonicalUUIDPattern.MatchString(existing) {
		return "", fmt.Errorf("%w: sync portable identity collision", ErrConflict)
	}
	return s.validSyncPortableIdentity(ctx, tx, project, kind, local, existing, origin)
}

func (s *Store) validSyncPortableIdentity(ctx context.Context, tx syncSQL, project string, kind syncservice.RecordKind, local, portable, origin string) (string, error) {
	var adoptedWire, device string
	var adoptedAt int64
	err := tx.QueryRowContext(ctx, `SELECT portable_id,adopting_device_id,adopted_at FROM sync_portable_identity_adoptions WHERE portable_project_id=? AND record_kind=? AND local_id=?`, project, kind, local).Scan(&adoptedWire, &device, &adoptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if portable != portableSyncUUID(project, string(kind), local) {
			return "", fmt.Errorf("%w: sync portable identity", ErrCorrupt)
		}
		return portable, nil
	}
	if err != nil {
		return "", writeError(ctx, err)
	}
	if !canonicalUUIDPattern.MatchString(origin) || !canonicalUUIDPattern.MatchString(adoptedWire) || !canonicalUUIDPattern.MatchString(device) || adoptedAt <= 0 || adoptedWire != portable || device != origin || local != adoptedSyncLocalID(kind, adoptedWire) {
		return "", fmt.Errorf("%w: sync portable identity adoption", ErrCorrupt)
	}
	return adoptedWire, nil
}

func adoptedSyncLocalID(kind syncservice.RecordKind, wireID string) string {
	return "sync-adopted:" + string(kind) + ":" + wireID
}

// AdoptSyncPortableIdentity reserves a stable local ID for a session or
// observation received under a remote portable wire identity. It records only
// identity provenance; materializing the referenced record remains pull work.
func (s *Store) AdoptSyncPortableIdentity(ctx context.Context, portableProject, expectedLocalProject string, kind syncservice.RecordKind, wireID string) (string, error) {
	if s == nil || s.readOnly || !projectIDPattern.MatchString(portableProject) || expectedLocalProject == "" || !canonicalUUIDPattern.MatchString(wireID) || (kind != syncservice.RecordKindSession && kind != syncservice.RecordKindObservation) {
		return "", fmt.Errorf("%w: sync portable identity adoption", ErrInvalid)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", writeError(ctx, err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return "", writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	var boundProject string
	err = conn.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE portable_id=?`, portableProject).Scan(&boundProject)
	if errors.Is(err, sql.ErrNoRows) || boundProject != expectedLocalProject {
		return "", fmt.Errorf("%w: portable project identity binding", ErrConflict)
	}
	if err != nil {
		return "", writeError(ctx, err)
	}
	profile, found, err := readStoredSyncProfile(ctx, conn)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%w: sync profile", ErrNotFound)
	}
	if !profile.Enabled {
		return "", fmt.Errorf("%w: sync profile disabled", ErrConflict)
	}
	device := profile.DeviceID
	local := adoptedSyncLocalID(kind, wireID)
	var existing, origin string
	err = conn.QueryRowContext(ctx, `SELECT portable_id,origin_device_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND local_id=?`, portableProject, kind, local).Scan(&existing, &origin)
	if err == nil {
		if !canonicalUUIDPattern.MatchString(origin) || existing != wireID {
			return "", fmt.Errorf("%w: sync portable identity collision", ErrConflict)
		}
		if _, err = s.validSyncPortableIdentity(ctx, conn, portableProject, kind, local, existing, origin); err != nil {
			return "", err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", writeError(ctx, err)
	} else {
		table := "sessions"
		if kind == syncservice.RecordKindObservation {
			table = "observations"
		}
		var occupied int
		if err = conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=?)`, local).Scan(&occupied); err != nil {
			return "", writeError(ctx, err)
		}
		if occupied != 0 {
			return "", fmt.Errorf("%w: sync portable identity local collision", ErrConflict)
		}
		now := s.now().UTC().UnixNano()
		if _, err = conn.ExecContext(ctx, `INSERT INTO sync_portable_identities(portable_project_id,record_kind,local_id,portable_id,origin_device_id,created_at) VALUES(?,?,?,?,?,?)`, portableProject, kind, local, wireID, device, now); err != nil {
			return "", conflictOrWrite(ctx, err)
		}
		if _, err = conn.ExecContext(ctx, `INSERT INTO sync_portable_identity_adoptions(portable_project_id,record_kind,local_id,portable_id,adopting_device_id,adopted_at) VALUES(?,?,?,?,?,?)`, portableProject, kind, local, wireID, device, now); err != nil {
			return "", conflictOrWrite(ctx, err)
		}
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return "", writeError(ctx, err)
	}
	committed = true
	return local, nil
}

func portableSyncUUID(project, kind, local string) string {
	sum := sha256.Sum256([]byte("vgxness/sync-portable-identity/v1\x00" + project + "\x00" + kind + "\x00" + local))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// LocalSyncPortableIdentity resolves retained deterministic or adopted wire IDs
// to local IDs. It only resolves identity provenance; it never materializes a
// record, and reference-before-target pull remains unsupported.
func (s *Store) LocalSyncPortableIdentity(ctx context.Context, project string, kind syncservice.RecordKind, portableID string) (string, bool, error) {
	if !projectIDPattern.MatchString(project) || !canonicalUUIDPattern.MatchString(portableID) || (kind != syncservice.RecordKindSession && kind != syncservice.RecordKindObservation) {
		return "", false, fmt.Errorf("%w: sync portable identity", ErrInvalid)
	}
	var local, origin string
	err := s.db.QueryRowContext(ctx, `SELECT local_id,origin_device_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND portable_id=?`, project, kind, portableID).Scan(&local, &origin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, writeError(ctx, err)
	}
	if len([]byte(local)) == 0 || len([]byte(local)) > 1024 || !canonicalUUIDPattern.MatchString(origin) {
		return "", false, fmt.Errorf("%w: sync portable identity", ErrCorrupt)
	}
	got, err := s.validSyncPortableIdentity(ctx, s.db, project, kind, local, portableID, origin)
	if err != nil || got != portableID {
		if err != nil {
			return "", false, err
		}
		return "", false, fmt.Errorf("%w: sync portable identity", ErrCorrupt)
	}
	return local, true, nil
}

// BootstrapCheckpoint is the durable, caller-owned progress marker for a pull.
type BootstrapCheckpoint struct {
	HistoryID string `json:"history_id"`
	Position  int64  `json:"position"`
	Watermark int64  `json:"watermark"`
	Phase     string `json:"-"`
}

type pulledPageMode uint8

const (
	pulledPageDefault pulledPageMode = iota
	pulledPageOwnConflict
	pulledPageResolution
)

func (mode pulledPageMode) allowsPendingBootstrap() bool { return mode != pulledPageDefault }
func (mode pulledPageMode) stopsAtPending() bool         { return mode == pulledPageResolution }
func (mode pulledPageMode) allowsResolutionAdvance() bool {
	return mode == pulledPageResolution
}

// BootstrapRemote owns any credentials needed for its authenticated calls.
// Store never persists or retains the remote implementation.
type BootstrapRemote interface {
	Discover(context.Context) (syncservice.Discovery, error)
	Pull(context.Context, syncservice.Cursor, int) (syncservice.PullPage, error)
}

type bootstrapState struct {
	cursor     *syncservice.Cursor
	checkpoint *BootstrapCheckpoint
}

// BootstrapSync atomically materializes the discovered remote history in
// bounded pages. Every remote call happens after a short durable recheck.
func (s *Store) BootstrapSync(ctx context.Context, remote BootstrapRemote) error {
	if s == nil || remote == nil || ctx == nil {
		return fmt.Errorf("%w: bootstrap remote", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := s.pendingProjectRepairPreflight(ctx); err != nil {
		return err
	}
	if _, err := s.bootstrapState(ctx); err != nil {
		return err
	}
	discovery, err := remote.Discover(ctx)
	if err != nil {
		return err
	}
	if syncservice.ValidateDiscovery(discovery) != nil {
		return fmt.Errorf("%w: invalid bootstrap discovery", ErrInvalid)
	}
	for pulls := 0; pulls < maxBootstrapPulls; pulls++ {
		state, err := s.bootstrapState(ctx)
		if err != nil {
			return err
		}
		complete, err := state.forHistory(discovery.HistoryID)
		if err != nil {
			return err
		}
		if complete {
			return nil
		}
		cursor := syncservice.Cursor{HistoryID: discovery.HistoryID}
		if state.checkpoint != nil {
			cursor.Position = state.checkpoint.Position
			cursor.Watermark = state.checkpoint.Watermark
		}
		if err = s.pendingProjectRepairPreflight(ctx); err != nil {
			return err
		}
		page, err := remote.Pull(ctx, cursor, syncapi.DefaultPullLimit)
		if err != nil {
			return err
		}
		if err := validateBootstrapPage(page, cursor); err != nil {
			return err
		}
		phase := "observations"
		if page.Cursor.Position == page.Cursor.Watermark {
			phase = "complete"
		}
		checkpoint := &BootstrapCheckpoint{HistoryID: discovery.HistoryID, Position: page.Cursor.Position, Watermark: page.Cursor.Watermark, Phase: phase}
		if err := s.ApplyPulledPage(ctx, page, checkpoint); err != nil {
			if errors.Is(err, ErrCorrupt) {
				if state, rereadErr := s.bootstrapState(ctx); rereadErr == nil {
					if complete, matchErr := state.forHistory(discovery.HistoryID); matchErr == nil && complete {
						return nil
					}
					return fmt.Errorf("%w: concurrent bootstrap", ErrConflict)
				}
			}
			return err
		}
		if phase == "complete" {
			return nil
		}
	}
	return fmt.Errorf("%w: bootstrap pull limit", ErrConflict)
}

// BootstrapOwnConflict materializes one receipt-authenticated local conflict
// without weakening BootstrapSync's global pending-work guard. It stops at the
// exact conflict receipt; later history remains for ordinary bootstrap.
func (s *Store) BootstrapOwnConflict(ctx context.Context, remote BootstrapRemote, mutationID string) error {
	if s == nil || remote == nil || ctx == nil || !canonicalUUIDPattern.MatchString(mutationID) {
		return fmt.Errorf("%w: own conflict bootstrap", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := s.pendingProjectRepairPreflight(ctx); err != nil {
		return err
	}
	sequence, err := s.ownConflictReceiptSequence(ctx, mutationID)
	if err != nil {
		return err
	}
	discovery, err := remote.Discover(ctx)
	if err != nil {
		return err
	}
	if syncservice.ValidateDiscovery(discovery) != nil {
		return fmt.Errorf("%w: invalid bootstrap discovery", ErrInvalid)
	}
	for pulls := 0; pulls < maxBootstrapPulls; pulls++ {
		cursor, err := s.ownConflictCursor(ctx, discovery.HistoryID)
		if err != nil {
			return err
		}
		if cursor.Position >= sequence {
			return fmt.Errorf("%w: own conflict history", ErrConflict)
		}
		if err = s.pendingProjectRepairPreflight(ctx); err != nil {
			return err
		}
		page, err := remote.Pull(ctx, cursor, syncapi.DefaultPullLimit)
		if err != nil {
			return err
		}
		if err = validateBootstrapPage(page, cursor); err != nil {
			return err
		}
		if len(page.Changes) == 0 {
			return fmt.Errorf("%w: own conflict history", ErrConflict)
		}
		if page.Cursor.Position < sequence {
			checkpoint := &BootstrapCheckpoint{HistoryID: discovery.HistoryID, Position: page.Cursor.Position, Watermark: page.Cursor.Watermark, Phase: "observations"}
			if err = s.applyPulledPage(ctx, page, checkpoint, pulledPageOwnConflict); err != nil {
				return err
			}
			continue
		}
		index := int(sequence - page.Changes[0].Sequence)
		if index < 0 || index >= len(page.Changes) || page.Changes[index].Mutation.MutationID != mutationID {
			return fmt.Errorf("%w: own conflict history", ErrConflict)
		}
		if index > 0 {
			before := page
			before.Changes = append([]syncservice.Change(nil), page.Changes[:index]...)
			before.Cursor.Position = before.Changes[len(before.Changes)-1].Sequence
			before.HasMore = true
			checkpoint := &BootstrapCheckpoint{HistoryID: discovery.HistoryID, Position: before.Cursor.Position, Watermark: before.Cursor.Watermark, Phase: "observations"}
			if err = s.applyPulledPage(ctx, before, checkpoint, pulledPageOwnConflict); err != nil {
				return err
			}
		}
		prefix := page
		prefix.Changes = []syncservice.Change{page.Changes[index]}
		prefix.Cursor.Position = sequence
		prefix.Cursor.Watermark = page.Cursor.Watermark
		prefix.HasMore = sequence < page.Cursor.Watermark
		return s.applyOwnConflictPage(ctx, prefix, mutationID)
	}
	return fmt.Errorf("%w: bootstrap pull limit", ErrConflict)
}

// PullConflictResolutions applies one bounded remote page while local work is
// pending. Only its safe contiguous prefix is committed.
func (s *Store) PullConflictResolutions(ctx context.Context, remote BootstrapRemote) error {
	if s == nil || remote == nil || ctx == nil {
		return fmt.Errorf("%w: conflict resolution pull", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := s.pendingProjectRepairPreflight(ctx); err != nil {
		return err
	}
	discovery, err := remote.Discover(ctx)
	if err != nil {
		return err
	}
	if syncservice.ValidateDiscovery(discovery) != nil {
		return fmt.Errorf("%w: invalid resolution discovery", ErrInvalid)
	}
	cursor, err := s.conflictResolutionCursor(ctx, discovery.HistoryID)
	if err != nil {
		return err
	}
	if err = s.pendingProjectRepairPreflight(ctx); err != nil {
		return err
	}
	page, err := remote.Pull(ctx, cursor, syncapi.DefaultPullLimit)
	if err != nil {
		return err
	}
	if err = validateBootstrapPage(page, cursor); err != nil {
		return err
	}
	if len(page.Changes) == 0 {
		return fmt.Errorf("%w: conflict resolution history", ErrConflict)
	}
	hasResolution := false
	for _, change := range page.Changes {
		hasResolution = hasResolution || change.Mutation.Kind == syncservice.MutationResolve
	}
	checkpoint := &BootstrapCheckpoint{HistoryID: page.Cursor.HistoryID, Position: page.Cursor.Position, Watermark: page.Cursor.Watermark, Phase: "observations"}
	if page.Cursor.Position == page.Cursor.Watermark {
		checkpoint.Phase = "complete"
	}
	if err = s.applyPulledPage(ctx, page, checkpoint, pulledPageResolution); err != nil {
		return err
	}
	if !hasResolution {
		return fmt.Errorf("%w: conflict resolution absent", ErrConflict)
	}
	return nil
}

func (s *Store) conflictResolutionCursor(ctx context.Context, historyID string) (syncservice.Cursor, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.closed || s.db == nil || s.readOnly {
		return syncservice.Cursor{}, fmt.Errorf("%w: conflict resolution pull", ErrConflict)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return syncservice.Cursor{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	state, err := readBootstrapState(ctx, tx)
	if err != nil {
		return syncservice.Cursor{}, err
	}
	if err = state.validateLocal(); err != nil || state.cursor != nil && state.cursor.HistoryID != historyID {
		return syncservice.Cursor{}, fmt.Errorf("%w: conflict resolution history", ErrConflict)
	}
	var unresolved int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_conflicts WHERE status='unresolved'`).Scan(&unresolved); err != nil {
		return syncservice.Cursor{}, writeError(ctx, err)
	}
	if unresolved == 0 {
		return syncservice.Cursor{}, fmt.Errorf("%w: unresolved conflicts", ErrNotFound)
	}
	if err = commit(ctx, tx); err != nil {
		return syncservice.Cursor{}, err
	}
	cursor := syncservice.Cursor{HistoryID: historyID}
	if state.cursor != nil {
		cursor.Position = state.cursor.Position
	}
	return cursor, nil
}

func (s *Store) ownConflictReceiptSequence(ctx context.Context, mutationID string) (int64, error) {
	var disposition string
	var retryable, sequence int64
	err := s.db.QueryRowContext(ctx, `SELECT disposition,retryable,sequence FROM sync_push_results WHERE mutation_id=?`, mutationID).Scan(&disposition, &retryable, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: own conflict receipt", ErrNotFound)
	}
	if err != nil {
		return 0, writeError(ctx, err)
	}
	if disposition != string(syncservice.DispositionConflict) || retryable != 0 || sequence < 1 {
		return 0, fmt.Errorf("%w: own conflict receipt", ErrConflict)
	}
	return sequence, nil
}

func (s *Store) ownConflictCursor(ctx context.Context, historyID string) (syncservice.Cursor, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.closed || s.db == nil {
		return syncservice.Cursor{}, fmt.Errorf("%w: store closed", ErrCorrupt)
	}
	if s.readOnly {
		return syncservice.Cursor{}, fmt.Errorf("%w: store is read-only", ErrConflict)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return syncservice.Cursor{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	state, err := readBootstrapState(ctx, tx)
	if err != nil {
		return syncservice.Cursor{}, err
	}
	if state.cursor != nil && state.cursor.HistoryID != historyID {
		return syncservice.Cursor{}, fmt.Errorf("%w: own conflict bootstrap state", ErrConflict)
	}
	if state.checkpoint != nil {
		complete, stateErr := state.forHistory(historyID)
		if stateErr != nil || complete {
			return syncservice.Cursor{}, fmt.Errorf("%w: own conflict bootstrap state", ErrConflict)
		}
	}
	if err = commit(ctx, tx); err != nil {
		return syncservice.Cursor{}, err
	}
	cursor := syncservice.Cursor{HistoryID: historyID}
	if state.cursor != nil {
		cursor.Position = state.cursor.Position
	}
	if state.checkpoint != nil {
		cursor.Watermark = state.checkpoint.Watermark
	}
	return cursor, nil
}

func (s *Store) applyOwnConflictPage(ctx context.Context, page syncservice.PullPage, mutationID string) error {
	if err := cancelled(ctx); err != nil || validatePulledPage(page, nil) != nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: invalid own conflict page", ErrInvalid)
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.syncInbox.known = false
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	if err = pendingProjectRepairBarrier(ctx, tx); err != nil {
		return err
	}
	epoch, err := syncDataVersion(ctx, tx)
	if err != nil {
		s.syncInbox.known = false
		return err
	}
	first := page.Changes[0]
	hash, _ := hex.DecodeString(first.ChangeHash)
	position, _, err := pulledCursor(ctx, tx, page.Cursor.HistoryID, first.Sequence, hash)
	if err != nil || first.Sequence != position+1 || len(page.Changes) != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: own conflict page gap", ErrConflict)
	}
	if first.Mutation.MutationID != mutationID || first.ChangeDisposition != syncservice.ChangeDispositionConflict {
		return fmt.Errorf("%w: own conflict receipt", ErrConflict)
	}
	own, err := ownPulledReceipt(ctx, tx, first)
	if err != nil || !own {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: own conflict receipt", ErrConflict)
	}
	if err = s.rebasePendingConflictOutbox(ctx, tx, first.Mutation, first.CanonicalVersion); err != nil {
		return err
	}
	if err = s.applyPulledConflict(ctx, tx, page.Cursor.HistoryID, first); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_inbox(history_id,seq,change_hash,applied_at) VALUES(?,?,?,?)`, page.Cursor.HistoryID, first.Sequence, hash, now); err != nil {
		s.syncInbox.known = false
		return writeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_cursor(singleton,history_id,position,updated_at) VALUES(1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET history_id=excluded.history_id,position=excluded.position,updated_at=excluded.updated_at`, page.Cursor.HistoryID, first.Sequence, now); err != nil {
		return writeError(ctx, err)
	}
	phase := "observations"
	if first.Sequence == page.Cursor.Watermark {
		phase = "complete"
	}
	next := BootstrapCheckpoint{HistoryID: page.Cursor.HistoryID, Position: first.Sequence, Watermark: page.Cursor.Watermark, Phase: phase}
	if err = verifyBootstrapCheckpoint(ctx, tx, next, page.Cursor.HistoryID, position, false); err != nil {
		var existing int
		if queryErr := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_bootstrap`).Scan(&existing); queryErr != nil || existing != 0 {
			return err
		}
	}
	checkpoint, marshalErr := json.Marshal(next)
	if marshalErr != nil || len(checkpoint) == 0 || len(checkpoint) > maxSyncPayloadBytes {
		return fmt.Errorf("%w: invalid bootstrap checkpoint", ErrInvalid)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_bootstrap(singleton,phase,payload_version,checkpoint,created_at,updated_at) VALUES(1,?,1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET phase=excluded.phase,checkpoint=excluded.checkpoint,updated_at=excluded.updated_at`, phase, checkpoint, now, now); err != nil {
		return writeError(ctx, err)
	}
	if err = commit(ctx, tx); err != nil {
		return err
	}
	s.syncInbox = syncInboxCache{known: true, dataVersion: epoch, historyID: page.Cursor.HistoryID, position: first.Sequence}
	return nil
}

func (s *Store) rebasePendingConflictOutbox(ctx context.Context, tx *sql.Tx, completed syncservice.Mutation, version int64) error {
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	var claimed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox o JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id WHERE o.record_kind=? AND o.record_id=? AND c.lease_until>? AND c.claimed_at<=?`, completed.RecordKind, completed.RecordID, now, now).Scan(&claimed); err != nil {
		return writeError(ctx, err)
	}
	if claimed != 0 {
		return fmt.Errorf("%w: later sync work is claimed", ErrConflict)
	}
	rows, err := tx.QueryContext(ctx, `SELECT mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,last_error_code,next_attempt_at,created_at,updated_at FROM sync_outbox WHERE record_kind=? AND record_id=? ORDER BY created_at DESC,id DESC`, completed.RecordKind, completed.RecordID)
	if err != nil {
		return writeError(ctx, err)
	}
	defer rows.Close()
	var later []SyncOutboxEntry
	for rows.Next() {
		var id, kind, recordID, mutationKind, state, code string
		var base, payloadVersion, attempts, next, created, updated int64
		var payload []byte
		if err = rows.Scan(&id, &kind, &recordID, &mutationKind, &base, &payloadVersion, &payload, &state, &attempts, &code, &next, &created, &updated); err != nil {
			return writeError(ctx, err)
		}
		entry, decodeErr := decodeSyncOutboxEntry(id, kind, recordID, mutationKind, base, payloadVersion, payload, state, attempts, code, next, created, updated)
		if decodeErr != nil {
			return decodeErr
		}
		later = append(later, entry)
	}
	if err = rows.Err(); err != nil {
		return writeError(ctx, err)
	}
	if len(later) == 0 {
		return nil
	}
	return rebaseSyncOutboxEntries(ctx, tx, later, version)
}

func rebaseSyncOutboxEntries(ctx context.Context, tx *sql.Tx, later []SyncOutboxEntry, version int64) error {
	latest := later[0].Mutation
	latest.BaseVersion = version
	if latest.Kind == syncservice.MutationCreate {
		if latest.RecordKind == syncservice.RecordKindObservation && latest.Observation != nil && latest.Observation.Lifecycle == syncservice.LifecycleArchived {
			latest.Kind = syncservice.MutationArchive
		} else {
			latest.Kind = syncservice.MutationUpdate
		}
	}
	if syncservice.ValidateMutation(latest) != nil {
		return fmt.Errorf("%w: rebased sync mutation", ErrConflict)
	}
	payload, err := json.Marshal(latest)
	if err != nil || len(payload) > maxSyncPayloadBytes {
		return fmt.Errorf("%w: rebased sync payload", ErrConflict)
	}
	for _, entry := range later[1:] {
		if _, err = tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE mutation_id=?`, entry.Mutation.MutationID); err != nil {
			return writeError(ctx, err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE sync_outbox SET mutation_kind=?,base_version=?,payload=? WHERE mutation_id=?`, latest.Kind, latest.BaseVersion, payload, latest.MutationID)
	return writeApplyError(ctx, err)
}

func (s *Store) bootstrapState(ctx context.Context) (bootstrapState, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.closed || s.db == nil {
		return bootstrapState{}, fmt.Errorf("%w: store closed", ErrCorrupt)
	}
	if s.readOnly {
		return bootstrapState{}, fmt.Errorf("%w: store is read-only", ErrConflict)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return bootstrapState{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox WHERE state IN ('pending','retry')`).Scan(&pending); err != nil {
		return bootstrapState{}, writeError(ctx, err)
	}
	if pending != 0 {
		return bootstrapState{}, fmt.Errorf("%w: local sync work is pending", ErrConflict)
	}
	state, err := readBootstrapState(ctx, tx)
	if err != nil {
		return bootstrapState{}, err
	}
	if err = state.validateLocal(); err != nil {
		return bootstrapState{}, err
	}
	if err = commit(ctx, tx); err != nil {
		return bootstrapState{}, err
	}
	return state, nil
}

func readBootstrapState(ctx context.Context, tx *sql.Tx) (bootstrapState, error) {
	var state bootstrapState
	var history, historyType, positionType, updatedType string
	var position, updated int64
	err := tx.QueryRowContext(ctx, `SELECT history_id,position,updated_at,typeof(history_id),typeof(position),typeof(updated_at) FROM sync_cursor WHERE singleton=1`).Scan(&history, &position, &updated, &historyType, &positionType, &updatedType)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, writeError(ctx, err)
	}
	if err == nil {
		if historyType != "text" || positionType != "integer" || updatedType != "integer" || !canonicalUUIDPattern.MatchString(history) || position < 1 || !validStoredSyncTime(updated, time.Unix(0, updated)) {
			return state, fmt.Errorf("%w: invalid bootstrap cursor", ErrCorrupt)
		}
		if err = pulledInboxConsistent(ctx, tx, history, position); err != nil {
			return state, err
		}
		state.cursor = &syncservice.Cursor{HistoryID: history, Position: position}
	} else if err = pulledInboxEmpty(ctx, tx); err != nil {
		return state, err
	}
	var phase, phaseType, versionType, payloadType string
	var version int64
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT phase,payload_version,checkpoint,typeof(phase),typeof(payload_version),typeof(checkpoint) FROM sync_bootstrap WHERE singleton=1`).Scan(&phase, &version, &payload, &phaseType, &versionType, &payloadType)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, writeError(ctx, err)
	}
	if phaseType != "text" || versionType != "integer" || payloadType != "blob" || version != 1 || checkpointPhaseRank(phase) == 0 || len(payload) == 0 || len(payload) > maxSyncPayloadBytes {
		return state, fmt.Errorf("%w: invalid bootstrap checkpoint", ErrCorrupt)
	}
	var checkpoint BootstrapCheckpoint
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&checkpoint) != nil || decoder.Decode(&extra) != io.EOF || checkpoint.Phase != "" || !canonicalUUIDPattern.MatchString(checkpoint.HistoryID) || checkpoint.Position < 0 || checkpoint.Watermark < checkpoint.Position {
		return state, fmt.Errorf("%w: invalid bootstrap checkpoint", ErrCorrupt)
	}
	checkpoint.Phase = phase
	state.checkpoint = &checkpoint
	return state, nil
}

func (s bootstrapState) forHistory(history string) (bool, error) {
	if s.cursor != nil && s.cursor.HistoryID != history {
		return false, fmt.Errorf("%w: bootstrap history", ErrConflict)
	}
	if s.checkpoint == nil {
		if s.cursor != nil {
			return false, fmt.Errorf("%w: cursor without bootstrap checkpoint", ErrConflict)
		}
		return false, nil
	}
	checkpoint := s.checkpoint
	if checkpoint.HistoryID != history {
		return false, fmt.Errorf("%w: bootstrap history", ErrConflict)
	}
	if checkpoint.Phase == "complete" {
		if checkpoint.Position != checkpoint.Watermark || checkpoint.Position == 0 && s.cursor != nil || checkpoint.Position > 0 && (s.cursor == nil || s.cursor.Position != checkpoint.Position) {
			return false, fmt.Errorf("%w: invalid complete bootstrap", ErrConflict)
		}
		return true, nil
	}
	if checkpoint.Position < 1 || checkpoint.Watermark < checkpoint.Position || s.cursor == nil || s.cursor.Position != checkpoint.Position {
		return false, fmt.Errorf("%w: bootstrap checkpoint mismatch", ErrConflict)
	}
	return false, nil
}

func (s bootstrapState) validateLocal() error {
	if s.checkpoint == nil {
		if s.cursor != nil {
			return fmt.Errorf("%w: cursor without bootstrap checkpoint", ErrConflict)
		}
		return nil
	}
	checkpoint := s.checkpoint
	if checkpoint.Phase == "complete" {
		if checkpoint.Position != checkpoint.Watermark || checkpoint.Position == 0 && s.cursor != nil || checkpoint.Position > 0 && (s.cursor == nil || s.cursor.HistoryID != checkpoint.HistoryID || s.cursor.Position != checkpoint.Position) {
			return fmt.Errorf("%w: invalid complete bootstrap", ErrConflict)
		}
		return nil
	}
	if checkpoint.Position < 1 || s.cursor == nil || s.cursor.HistoryID != checkpoint.HistoryID || s.cursor.Position != checkpoint.Position {
		return fmt.Errorf("%w: bootstrap checkpoint mismatch", ErrConflict)
	}
	return nil
}

func validateBootstrapPage(page syncservice.PullPage, cursor syncservice.Cursor) error {
	if validatePulledPage(page, nil) != nil || len(page.Changes) > syncapi.MaxPullLimit || page.Cursor.HistoryID != cursor.HistoryID || cursor.Watermark != 0 && page.Cursor.Watermark != cursor.Watermark {
		return fmt.Errorf("%w: invalid bootstrap page", ErrInvalid)
	}
	if len(page.Changes) == 0 {
		if page.Cursor.Position != cursor.Position || page.Cursor.Position != page.Cursor.Watermark {
			return fmt.Errorf("%w: bootstrap did not progress", ErrConflict)
		}
		return nil
	}
	if cursor.Position == int64(^uint64(0)>>1) || page.Changes[0].Sequence != cursor.Position+1 || page.Cursor.Position <= cursor.Position {
		return fmt.Errorf("%w: bootstrap page gap", ErrConflict)
	}
	return nil
}

func (s *Store) ConfigureSyncProfile(ctx context.Context, profile SyncProfile) (SyncProfile, error) {
	if err := cancelled(ctx); err != nil {
		return SyncProfile{}, err
	}
	var err error
	if profile, err = validateSyncProfile(profile); err != nil {
		return SyncProfile{}, err
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return SyncProfile{}, fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	var enabled int
	var created, updated int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,previous_credential_ref,created_at,updated_at) VALUES(1,?,?,?,?,?,?,?) ON CONFLICT(singleton) DO UPDATE SET enabled=excluded.enabled,endpoint=excluded.endpoint,device_id=excluded.device_id,credential_ref=excluded.credential_ref,previous_credential_ref=excluded.previous_credential_ref,updated_at=excluded.updated_at RETURNING enabled,endpoint,device_id,credential_ref,COALESCE(previous_credential_ref,''),created_at,updated_at`, boolInt(profile.Enabled), profile.Endpoint, profile.DeviceID, profile.CredentialRef, nullSyncCredentialRef(profile.PreviousCredentialRef), now, now).Scan(&enabled, &profile.Endpoint, &profile.DeviceID, &profile.CredentialRef, &profile.PreviousCredentialRef, &created, &updated)
	if err != nil {
		return SyncProfile{}, writeError(ctx, err)
	}
	if enabled != 0 && enabled != 1 || !validStoredSyncTime(created, time.Unix(0, created)) || !validStoredSyncTime(updated, time.Unix(0, updated)) || updated < created {
		return SyncProfile{}, fmt.Errorf("%w: invalid sync profile", ErrCorrupt)
	}
	profile.Enabled = enabled == 1
	profile.CreatedAt, profile.UpdatedAt = time.Unix(0, created).UTC(), time.Unix(0, updated).UTC()
	return profile, nil
}

func (s *Store) GetSyncProfile(ctx context.Context) (SyncProfile, bool, error) {
	if err := cancelled(ctx); err != nil {
		return SyncProfile{}, false, err
	}
	return readStoredSyncProfile(ctx, s.db)
}

func readStoredSyncProfile(ctx context.Context, db syncSQL) (SyncProfile, bool, error) {
	var profile SyncProfile
	var enabled int
	var created, updated int64
	err := db.QueryRowContext(ctx, `SELECT enabled,endpoint,device_id,credential_ref,COALESCE(previous_credential_ref,''),created_at,updated_at FROM sync_profiles WHERE singleton=1`).Scan(&enabled, &profile.Endpoint, &profile.DeviceID, &profile.CredentialRef, &profile.PreviousCredentialRef, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncProfile{}, false, nil
	}
	if err != nil {
		return SyncProfile{}, false, writeError(ctx, err)
	}
	if enabled != 0 && enabled != 1 {
		return SyncProfile{}, false, fmt.Errorf("%w: invalid sync profile", ErrCorrupt)
	}
	profile.Enabled = enabled == 1
	profile.CreatedAt = time.Unix(0, created).UTC()
	profile.UpdatedAt = time.Unix(0, updated).UTC()
	normalized, err := validateSyncProfile(profile)
	if err != nil || normalized.Endpoint != profile.Endpoint || normalized.DeviceID != profile.DeviceID || !validStoredSyncTime(created, profile.CreatedAt) || !validStoredSyncTime(updated, profile.UpdatedAt) || profile.UpdatedAt.Before(profile.CreatedAt) {
		return SyncProfile{}, false, fmt.Errorf("%w: invalid sync profile", ErrCorrupt)
	}
	return profile, true, nil
}

// DueSyncOutbox returns at most sixteen diagnostic entries in durable insertion
// order. It intentionally does not expose mutation payloads.
func (s *Store) DueSyncOutbox(ctx context.Context, due time.Time) ([]SyncOutboxDiagnostic, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	dueNanos, ok := syncUnixNano(due.UTC().Round(0))
	if !ok {
		return nil, fmt.Errorf("%w: invalid due time", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT
		CASE WHEN typeof(mutation_id)='text' AND length(CAST(mutation_id AS BLOB))=36 THEN mutation_id ELSE NULL END,
		CASE WHEN record_kind IN ('project','session','observation') THEN record_kind ELSE NULL END,
		CASE WHEN typeof(record_id)='text' AND length(CAST(record_id AS BLOB)) BETWEEN 1 AND 1024 THEN record_id ELSE NULL END,
		CASE WHEN state IN ('pending','retry') THEN state ELSE NULL END,
		attempts,CASE WHEN typeof(last_error_code)='text' AND length(CAST(last_error_code AS BLOB))<=64 THEN last_error_code ELSE NULL END,next_attempt_at,created_at,updated_at
		FROM sync_outbox WHERE next_attempt_at<=? ORDER BY created_at,id LIMIT 16`, dueNanos)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	defer rows.Close()
	entries := make([]SyncOutboxDiagnostic, 0, maxDueSyncOutbox)
	for rows.Next() {
		var id, recordKind, recordID, state, lastErrorCode sql.NullString
		var attempts, nextAttempt, created, updated int64
		if err := rows.Scan(&id, &recordKind, &recordID, &state, &attempts, &lastErrorCode, &nextAttempt, &created, &updated); err != nil {
			return nil, writeError(ctx, err)
		}
		if !id.Valid || !canonicalUUIDPattern.MatchString(id.String) || !recordKind.Valid || !recordID.Valid || !state.Valid || !lastErrorCode.Valid || attempts < 0 {
			return nil, fmt.Errorf("%w: invalid sync outbox entry", ErrCorrupt)
		}
		entry := SyncOutboxDiagnostic{MutationID: id.String, RecordKind: syncservice.RecordKind(recordKind.String), RecordID: recordID.String, State: SyncOutboxState(state.String), Attempts: attempts, LastErrorCode: lastErrorCode.String, NextAttemptAt: time.Unix(0, nextAttempt).UTC(), CreatedAt: time.Unix(0, created).UTC(), UpdatedAt: time.Unix(0, updated).UTC()}
		if (entry.State != SyncOutboxPending || entry.Attempts != 0 || entry.LastErrorCode != "") && (entry.State != SyncOutboxRetry || entry.Attempts == 0 || !syncErrorCodePattern.MatchString(entry.LastErrorCode)) || !validStoredSyncTime(nextAttempt, entry.NextAttemptAt) || !validStoredSyncTime(created, entry.CreatedAt) || !validStoredSyncTime(updated, entry.UpdatedAt) || entry.UpdatedAt.Before(entry.CreatedAt) {
			return nil, fmt.Errorf("%w: invalid sync outbox entry", ErrCorrupt)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, writeError(ctx, err)
	}
	return entries, nil
}

// ClaimDueSyncOutbox atomically leases eligible oldest mutations per record.
func (s *Store) ClaimDueSyncOutbox(ctx context.Context, lease time.Duration, limit int) ([]SyncOutboxClaim, error) {
	return s.claimDueSyncOutbox(ctx, lease, limit, "")
}

// ClaimDueSyncOutboxForProject leases only mutations owned by project. It
// preserves the existing mutation bytes and identifiers while isolating other
// projects' pending work.
func (s *Store) ClaimDueSyncOutboxForProject(ctx context.Context, lease time.Duration, limit int, project string) ([]SyncOutboxClaim, error) {
	if project == "" {
		return nil, fmt.Errorf("%w: sync project", ErrInvalid)
	}
	return s.claimDueSyncOutbox(ctx, lease, limit, project)
}

// ClaimDueSyncOutboxForProjectTransition leases project work only while the
// selected transition incarnation remains current.
func (s *Store) ClaimDueSyncOutboxForProjectTransition(ctx context.Context, lease time.Duration, limit int, project, portableProject string, transitionIdentity int64) ([]SyncOutboxClaim, error) {
	if project == "" || !projectIDPattern.MatchString(portableProject) || transitionIdentity <= 0 {
		return nil, fmt.Errorf("%w: sync project transition", ErrInvalid)
	}
	return s.claimDueSyncOutboxForTransition(ctx, lease, limit, project, portableProject, transitionIdentity)
}

func (s *Store) claimDueSyncOutbox(ctx context.Context, lease time.Duration, limit int, project string) ([]SyncOutboxClaim, error) {
	return s.claimDueSyncOutboxForTransition(ctx, lease, limit, project, "", 0)
}

func (s *Store) claimDueSyncOutboxForTransition(ctx context.Context, lease time.Duration, limit int, project, portableProject string, transitionIdentity int64) ([]SyncOutboxClaim, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	now := s.now().UTC().Round(0)
	nowNanos, ok := syncUnixNano(now)
	if !ok || lease < time.Second || lease > 24*time.Hour || limit < 1 || limit > maxDueSyncOutbox {
		return nil, fmt.Errorf("%w: invalid sync claim", ErrInvalid)
	}
	untilNanos, ok := syncUnixNano(now.Add(lease).UTC().Round(0))
	if !ok || untilNanos < nowNanos {
		return nil, fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, writeError(ctx, err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if transitionIdentity != 0 {
		if err = activeTransitionIdentityMatches(ctx, conn, portableProject, project, transitionIdentity); err != nil {
			return nil, err
		}
	}
	repairPriority := `EXISTS(SELECT 1 FROM pending_project_repairs r WHERE r.repair_mutation_id=o.mutation_id)`
	query := `WITH pending_project_repairs AS (
		SELECT r.repair_mutation_id FROM sync_project_repairs r JOIN sync_outbox q ON q.mutation_id=r.repair_mutation_id
		WHERE r.status='pending' AND q.record_kind='project' AND q.mutation_kind='create' AND q.base_version=0 AND q.record_id=r.local_project_id
	)
	SELECT o.mutation_id,o.record_kind,o.record_id,o.mutation_kind,o.base_version,o.payload_version,o.payload,o.state,o.attempts,o.last_error_code,o.next_attempt_at,o.created_at,o.updated_at
		FROM sync_outbox o LEFT JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id
		WHERE o.next_attempt_at<=? AND (c.mutation_id IS NULL OR c.lease_until<=?)
		AND (` + repairPriority + ` OR NOT EXISTS (SELECT 1 FROM sync_outbox p WHERE p.record_kind=o.record_kind AND p.record_id=o.record_id AND (p.created_at<o.created_at OR p.created_at=o.created_at AND p.id<o.id)))
		AND NOT EXISTS (SELECT 1 FROM sync_conflicts f WHERE f.status='unresolved' AND f.record_kind=o.record_kind AND f.record_id=o.record_id)
		AND (` + repairPriority + ` OR o.base_version=CASE o.record_kind WHEN 'project' THEN COALESCE((SELECT sync_version FROM projects WHERE id=o.record_id),-1) WHEN 'session' THEN COALESCE((SELECT sync_version FROM sessions WHERE id=o.record_id),-1) WHEN 'observation' THEN COALESCE((SELECT sync_version FROM observations WHERE id=o.record_id),-1) ELSE -1 END)
		AND (o.record_kind<>'observation' OR o.mutation_kind<>'create' OR NOT EXISTS (
			SELECT 1 FROM observation_refs r
			JOIN observations source ON source.id=o.record_id
			JOIN observations target ON target.id=r.target_id AND target.project_id=source.project_id
			WHERE r.observation_id=o.record_id AND target.sync_version=0
			AND EXISTS (SELECT 1 FROM sync_outbox prerequisite WHERE prerequisite.record_kind='observation' AND prerequisite.record_id=r.target_id AND prerequisite.mutation_kind='create')
		))`
	args := []any{nowNanos, nowNanos}
	if project != "" {
		query += ` AND (o.record_kind='project' AND o.record_id=? OR o.record_kind='session' AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=o.record_id AND s.project_id=?) OR o.record_kind='observation' AND EXISTS(SELECT 1 FROM observations n WHERE n.id=o.record_id AND n.project_id=?))`
		args = append(args, project, project, project)
		query += ` AND (NOT EXISTS (SELECT 1 FROM sync_project_repairs r WHERE r.status='pending' AND r.local_project_id=?) OR EXISTS (SELECT 1 FROM sync_project_repairs r WHERE r.status='pending' AND r.local_project_id=? AND r.repair_mutation_id=o.mutation_id))`
		args = append(args, project, project)
	} else {
		query += ` AND (NOT EXISTS (SELECT 1 FROM sync_project_repairs WHERE status='pending') OR ` + repairPriority + `)`
	}
	query += ` ORDER BY CASE WHEN ` + repairPriority + ` THEN 0 ELSE 1 END,o.created_at,o.id LIMIT ?`
	args = append(args, limit)
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	entries := make([]SyncOutboxEntry, 0, limit)
	for rows.Next() {
		var id, recordKind, recordID, kind, state, code string
		var base, payloadVersion, attempts, next, created, updated int64
		var payload []byte
		if err := rows.Scan(&id, &recordKind, &recordID, &kind, &base, &payloadVersion, &payload, &state, &attempts, &code, &next, &created, &updated); err != nil {
			return nil, writeError(ctx, err)
		}
		entry, err := decodeSyncOutboxEntry(id, recordKind, recordID, kind, base, payloadVersion, payload, state, attempts, code, next, created, updated)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, writeError(ctx, err)
	}
	if err := rows.Close(); err != nil {
		return nil, writeError(ctx, err)
	}
	entries, err = claimableSyncOutboxEntries(ctx, conn, entries)
	if err != nil {
		return nil, err
	}
	claims := make([]SyncOutboxClaim, 0, len(entries))
	for _, entry := range entries {
		id := entry.Mutation.MutationID
		token, tokenErr := newSyncUUID()
		if tokenErr != nil {
			return nil, writeError(ctx, tokenErr)
		}
		var firstToken, currentToken string
		var firstClaimed, claimed, leaseUntil int64
		err = conn.QueryRowContext(ctx, `INSERT INTO sync_outbox_claims(mutation_id,first_claim_token,claim_token,first_claimed_at,claimed_at,lease_until) VALUES(?,?,?,?,?,?) ON CONFLICT(mutation_id) DO UPDATE SET claim_token=excluded.claim_token,claimed_at=excluded.claimed_at,lease_until=excluded.lease_until WHERE sync_outbox_claims.lease_until<=? RETURNING first_claim_token,claim_token,first_claimed_at,claimed_at,lease_until`, id, token, token, nowNanos, nowNanos, untilNanos, nowNanos).Scan(&firstToken, &currentToken, &firstClaimed, &claimed, &leaseUntil)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, writeError(ctx, err)
		}
		claim := SyncOutboxClaim{SyncOutboxEntry: entry, FirstClaimToken: firstToken, ClaimToken: currentToken, FirstClaimedAt: time.Unix(0, firstClaimed).UTC(), ClaimedAt: time.Unix(0, claimed).UTC(), LeaseUntil: time.Unix(0, leaseUntil).UTC()}
		if !canonicalUUIDPattern.MatchString(firstToken) || !canonicalUUIDPattern.MatchString(currentToken) || !validStoredSyncTime(firstClaimed, claim.FirstClaimedAt) || !validStoredSyncTime(claimed, claim.ClaimedAt) || !validStoredSyncTime(leaseUntil, claim.LeaseUntil) || claim.ClaimedAt.Before(claim.FirstClaimedAt) || claim.LeaseUntil.Before(claim.ClaimedAt) {
			return nil, fmt.Errorf("%w: invalid sync claim", ErrCorrupt)
		}
		claims = append(claims, claim)
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, writeError(ctx, err)
	}
	committed = true
	return claims, nil
}

func claimableSyncOutboxEntries(ctx context.Context, conn *sql.Conn, entries []SyncOutboxEntry) ([]SyncOutboxEntry, error) {
	ready := make([]SyncOutboxEntry, 0, len(entries))
	for _, entry := range entries {
		mutation := entry.Mutation
		if mutation.RecordKind != syncservice.RecordKindObservation || mutation.Kind != syncservice.MutationCreate {
			ready = append(ready, entry)
			continue
		}
		if mutation.Observation == nil {
			return nil, fmt.Errorf("%w: observation sync prerequisite", ErrCorrupt)
		}
		available := true
		for _, reference := range mutation.Observation.References {
			var project string
			var version int64
			err := conn.QueryRowContext(ctx, `SELECT project_id,sync_version FROM observations WHERE id=?`, reference).Scan(&project, &version)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: missing observation sync prerequisite", ErrConflict)
			}
			if err != nil {
				return nil, writeError(ctx, err)
			}
			if project != mutation.Observation.ProjectID {
				return nil, fmt.Errorf("%w: observation sync prerequisite crosses project", ErrConflict)
			}
			if version < 0 {
				return nil, fmt.Errorf("%w: observation sync prerequisite version", ErrCorrupt)
			}
			if version > 0 {
				continue
			}
			var queued int
			if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox WHERE record_kind='observation' AND record_id=? AND mutation_kind='create'`, reference).Scan(&queued); err != nil {
				return nil, writeError(ctx, err)
			}
			if queued == 0 {
				return nil, fmt.Errorf("%w: unsynced observation prerequisite has no create", ErrConflict)
			}
			available = false
		}
		if available {
			ready = append(ready, entry)
		}
	}
	return ready, nil
}

// RenewSyncOutboxClaim extends a current, unexpired lease without changing proof.
func (s *Store) RenewSyncOutboxClaim(ctx context.Context, mutationID, claimToken string, lease time.Duration) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	now := s.now().UTC().Round(0)
	nowNanos, ok := syncUnixNano(now)
	if !ok || !canonicalUUIDPattern.MatchString(mutationID) || !canonicalUUIDPattern.MatchString(claimToken) || lease < time.Second || lease > 24*time.Hour {
		return fmt.Errorf("%w: invalid sync claim", ErrInvalid)
	}
	until, ok := syncUnixNano(now.Add(lease).UTC().Round(0))
	if !ok || until < nowNanos {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	entry, found, err := syncOutboxForResult(ctx, tx, mutationID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: sync outbox claim", ErrNotFound)
	}
	if err = claimMatches(ctx, tx, mutationID, claimToken, nowNanos); err != nil {
		return err
	}
	repair, err := syncProjectRepairForMutation(ctx, tx, entry.Mutation)
	if err != nil {
		return err
	}
	if !repair {
		if err = pendingProjectRepairForMutation(ctx, tx, entry.Mutation); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE sync_outbox_claims SET lease_until=? WHERE mutation_id=? AND claim_token=? AND lease_until>? AND lease_until<?`, until, mutationID, claimToken, nowNanos, until)
	if err != nil {
		return writeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return writeError(ctx, err)
	}
	if changed != 1 {
		return fmt.Errorf("%w: sync outbox claim", ErrNotFound)
	}
	return commit(ctx, tx)
}

// MarkSyncOutboxRetry makes a currently claimed entry eligible for a later retry.
func (s *Store) MarkSyncOutboxRetry(ctx context.Context, mutationID, claimToken string, next time.Time, code string) error {
	return s.markSyncOutboxRetry(ctx, mutationID, claimToken, next, code, s.now().UTC().Round(0), "", "", 0)
}

// MarkSyncOutboxRetryForProjectTransition retries only while the selected
// transition incarnation remains current.
func (s *Store) MarkSyncOutboxRetryForProjectTransition(ctx context.Context, mutationID, claimToken string, next time.Time, code, portableProject, localProject string, transitionIdentity int64) error {
	return s.markSyncOutboxRetry(ctx, mutationID, claimToken, next, code, s.now().UTC().Round(0), portableProject, localProject, transitionIdentity)
}

func (s *Store) markSyncOutboxRetry(ctx context.Context, mutationID, claimToken string, next time.Time, code string, now time.Time, portableProject, localProject string, transitionIdentity int64) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	now = now.UTC().Round(0)
	nowNanos, ok := syncUnixNano(now)
	nextNanos, nextOK := syncUnixNano(next.UTC().Round(0))
	if !ok || !nextOK || next.Before(now) || !canonicalUUIDPattern.MatchString(mutationID) || !canonicalUUIDPattern.MatchString(claimToken) || !syncErrorCodePattern.MatchString(code) {
		return fmt.Errorf("%w: invalid sync retry", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	if transitionIdentity != 0 {
		if err = activeTransitionIdentityMatches(ctx, tx, portableProject, localProject, transitionIdentity); err != nil {
			return err
		}
	}
	entry, found, err := syncOutboxForResult(ctx, tx, mutationID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: sync outbox claim", ErrNotFound)
	}
	if err = claimMatches(ctx, tx, mutationID, claimToken, nowNanos); err != nil {
		return err
	}
	repair, err := syncProjectRepairForMutation(ctx, tx, entry.Mutation)
	if err != nil {
		return err
	}
	if !repair {
		if err = pendingProjectRepairForMutation(ctx, tx, entry.Mutation); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE sync_outbox SET state='retry',attempts=attempts+1,next_attempt_at=?,last_error_code=?,updated_at=? WHERE mutation_id=? AND EXISTS (SELECT 1 FROM sync_outbox_claims WHERE mutation_id=? AND claim_token=? AND lease_until>? AND claimed_at<=?)`, nextNanos, code, nowNanos, mutationID, mutationID, claimToken, nowNanos, nowNanos)
	if err != nil {
		return writeError(ctx, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return writeError(ctx, err)
	}
	if updated != 1 {
		return fmt.Errorf("%w: sync outbox claim", ErrNotFound)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_outbox_claims SET lease_until=? WHERE mutation_id=? AND claim_token=?`, nowNanos, mutationID, claimToken); err != nil {
		return writeError(ctx, err)
	}
	if err = tx.Commit(); err != nil {
		return writeError(ctx, err)
	}
	return nil
}

// ApplySyncPushResult completes only the currently leased mutation. Terminal
// results, the local version change, and the receipt are one transaction.
func (s *Store) ApplySyncPushResult(ctx context.Context, mutationID, claimToken string, result syncservice.Result) error {
	return s.applySyncPushResult(ctx, mutationID, claimToken, result, "", "", 0)
}

// ApplySyncPushResultForProjectTransition records a push result only while the
// selected transition incarnation remains current.
func (s *Store) ApplySyncPushResultForProjectTransition(ctx context.Context, mutationID, claimToken string, result syncservice.Result, portableProject, localProject string, transitionIdentity int64) error {
	return s.applySyncPushResult(ctx, mutationID, claimToken, result, portableProject, localProject, transitionIdentity)
}

func (s *Store) applySyncPushResult(ctx context.Context, mutationID, claimToken string, result syncservice.Result, portableProject, localProject string, transitionIdentity int64) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if !canonicalUUIDPattern.MatchString(mutationID) || !canonicalUUIDPattern.MatchString(claimToken) || result.MutationID != mutationID || !validSyncResult(result) {
		return fmt.Errorf("%w: invalid sync push result", ErrInvalid)
	}
	if result.Retryable {
		now := s.now().UTC().Round(0)
		return s.markSyncOutboxRetry(ctx, mutationID, claimToken, now, result.Code, now, portableProject, localProject, transitionIdentity)
	}
	now := s.now().UTC().Round(0)
	nanos, ok := syncUnixNano(now)
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	if transitionIdentity != 0 {
		if err = activeTransitionIdentityMatches(ctx, tx, portableProject, localProject, transitionIdentity); err != nil {
			return err
		}
	}
	entry, found, err := syncOutboxForResult(ctx, tx, mutationID)
	if err != nil {
		return err
	}
	if !found {
		return syncReceiptMatches(ctx, tx, result, nil)
	}
	if err = claimMatches(ctx, tx, mutationID, claimToken, nanos); err != nil {
		return err
	}
	repair, err := syncProjectRepairForMutation(ctx, tx, entry.Mutation)
	if err != nil {
		return err
	}
	if !repair {
		if err = pendingProjectRepairForMutation(ctx, tx, entry.Mutation); err != nil {
			return err
		}
	}
	if repair {
		err = applyProjectRepairResultVersion(ctx, tx, entry.Mutation, result)
	} else {
		err = applyResultVersion(ctx, tx, entry.Mutation, result)
	}
	if err != nil {
		return err
	}
	hash, err := syncMutationHash(entry.Mutation)
	if err != nil {
		return fmt.Errorf("%w: sync mutation hash", ErrCorrupt)
	}
	if err = insertSyncReceipt(ctx, tx, entry.Mutation, result, hash, nanos); err != nil {
		return err
	}
	if repair {
		if err = completeProjectRepair(ctx, tx, entry.Mutation.MutationID, result, nanos); err != nil {
			return err
		}
	}
	if result.Disposition == syncservice.DispositionAccepted || result.Disposition == syncservice.DispositionPreviouslyAccepted {
		if err = s.rebaseFollowingSyncOutbox(ctx, tx, entry.Mutation, result.Version); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE mutation_id=?`, mutationID); err != nil {
		return writeError(ctx, err)
	}
	return commit(ctx, tx)
}

func syncProjectRepairForMutation(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation) (bool, error) {
	if mutation.RecordKind != syncservice.RecordKindProject || mutation.Kind != syncservice.MutationCreate || mutation.BaseVersion != 0 {
		return false, nil
	}
	var local, status string
	err := tx.QueryRowContext(ctx, `SELECT local_project_id,status FROM sync_project_repairs WHERE repair_mutation_id=?`, mutation.MutationID).Scan(&local, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, writeError(ctx, err)
	}
	if local != mutation.RecordID || status != "pending" {
		return false, fmt.Errorf("%w: project sync repair", ErrCorrupt)
	}
	return true, nil
}

func applyProjectRepairResultVersion(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation, result syncservice.Result) error {
	if result.Disposition == syncservice.DispositionRejected || result.Disposition == syncservice.DispositionConflict {
		return nil
	}
	if result.Disposition != syncservice.DispositionAccepted && result.Disposition != syncservice.DispositionPreviouslyAccepted || result.Version != 1 {
		return fmt.Errorf("%w: invalid project sync repair result", ErrInvalid)
	}
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT sync_version FROM projects WHERE id=?`, mutation.RecordID).Scan(&version); err != nil {
		return writeError(ctx, err)
	}
	if version != 1 {
		return fmt.Errorf("%w: project sync repair version", ErrConflict)
	}
	return nil
}

func completeProjectRepair(ctx context.Context, tx *sql.Tx, mutationID string, result syncservice.Result, now int64) error {
	status, code := "completed", ""
	if result.Disposition == syncservice.DispositionRejected || result.Disposition == syncservice.DispositionConflict {
		status = "rejected"
		code = result.Code
		if code == "" {
			code = string(result.Disposition)
		}
	}
	updated, err := tx.ExecContext(ctx, `UPDATE sync_project_repairs SET status=?,terminal_code=?,completed_at=? WHERE repair_mutation_id=? AND status='pending'`, status, code, now, mutationID)
	if err != nil {
		return writeError(ctx, err)
	}
	n, err := updated.RowsAffected()
	if err != nil {
		return writeError(ctx, err)
	}
	if n != 1 {
		return fmt.Errorf("%w: project sync repair", ErrConflict)
	}
	return nil
}

func (s *Store) validateRetryClaim(ctx context.Context, mutationID, claimToken string, now time.Time) error {
	nowNanos, ok := syncUnixNano(now.UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	_, found, err := syncOutboxForResult(ctx, tx, mutationID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: sync outbox claim", ErrNotFound)
	}
	return claimMatches(ctx, tx, mutationID, claimToken, nowNanos)
}

func validSyncResult(result syncservice.Result) bool {
	if result.Retryable {
		return result.Disposition == syncservice.DispositionRejected && result.Sequence == nil && result.Version == 0 && syncErrorCodePattern.MatchString(result.Code)
	}
	switch result.Disposition {
	case syncservice.DispositionAccepted, syncservice.DispositionPreviouslyAccepted, syncservice.DispositionConflict:
		return result.Sequence != nil && *result.Sequence > 0 && result.Version > 0 && result.Code == ""
	case syncservice.DispositionRejected:
		return result.Sequence == nil && result.Version == 0 && syncErrorCodePattern.MatchString(result.Code)
	default:
		return false
	}
}

func syncOutboxForResult(ctx context.Context, tx *sql.Tx, mutationID string) (SyncOutboxEntry, bool, error) {
	var kind, recordID, mutationKind, state, code string
	var base, payloadVersion, attempts, next, created, updated int64
	var payload []byte
	err := tx.QueryRowContext(ctx, `SELECT record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,last_error_code,next_attempt_at,created_at,updated_at FROM sync_outbox WHERE mutation_id=?`, mutationID).Scan(&kind, &recordID, &mutationKind, &base, &payloadVersion, &payload, &state, &attempts, &code, &next, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncOutboxEntry{}, false, nil
	}
	if err != nil {
		return SyncOutboxEntry{}, false, writeError(ctx, err)
	}
	entry, err := decodeSyncOutboxEntry(mutationID, kind, recordID, mutationKind, base, payloadVersion, payload, state, attempts, code, next, created, updated)
	return entry, true, err
}

func claimMatches(ctx context.Context, tx *sql.Tx, mutationID, claimToken string, now int64) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox_claims WHERE mutation_id=? AND claim_token=? AND lease_until>? AND claimed_at<=?`, mutationID, claimToken, now, now).Scan(&count); err != nil {
		return writeError(ctx, err)
	}
	if count != 1 {
		return fmt.Errorf("%w: sync outbox claim", ErrNotFound)
	}
	return nil
}

func applyResultVersion(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation, result syncservice.Result) error {
	if result.Disposition == syncservice.DispositionRejected {
		return nil
	}
	if result.Disposition == syncservice.DispositionConflict {
		if mutation.RecordKind != syncservice.RecordKindObservation || result.Version < mutation.BaseVersion {
			return fmt.Errorf("%w: invalid sync conflict result", ErrInvalid)
		}
	} else if result.Version != mutation.BaseVersion+1 {
		return fmt.Errorf("%w: invalid sync accepted result", ErrInvalid)
	}
	selectVersion := map[syncservice.RecordKind]string{
		syncservice.RecordKindProject:     `SELECT sync_version FROM projects WHERE id=?`,
		syncservice.RecordKindSession:     `SELECT sync_version FROM sessions WHERE id=?`,
		syncservice.RecordKindObservation: `SELECT sync_version FROM observations WHERE id=?`,
	}[mutation.RecordKind]
	current, err := syncVersion(ctx, tx, selectVersion, mutation.RecordID)
	if err != nil {
		return err
	}
	if current == result.Version && result.Disposition == syncservice.DispositionConflict {
		return nil
	}
	if current != mutation.BaseVersion {
		return fmt.Errorf("%w: sync result version", ErrConflict)
	}
	query := map[syncservice.RecordKind]string{
		syncservice.RecordKindProject:     `UPDATE projects SET sync_version=? WHERE id=? AND sync_version=?`,
		syncservice.RecordKindSession:     `UPDATE sessions SET sync_version=? WHERE id=? AND sync_version=?`,
		syncservice.RecordKindObservation: `UPDATE observations SET sync_version=? WHERE id=? AND sync_version=?`,
	}[mutation.RecordKind]
	updated, err := tx.ExecContext(ctx, query, result.Version, mutation.RecordID, mutation.BaseVersion)
	if err != nil {
		return writeError(ctx, err)
	}
	n, err := updated.RowsAffected()
	if err != nil {
		return writeError(ctx, err)
	}
	if n != 1 {
		return fmt.Errorf("%w: sync result version", ErrConflict)
	}
	return nil
}

func syncMutationHash(m syncservice.Mutation) ([]byte, error) {
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return sum[:], nil
}

func insertSyncReceipt(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation, result syncservice.Result, hash []byte, now int64) error {
	var sequence any
	if result.Sequence != nil {
		sequence = *result.Sequence
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO sync_push_results(mutation_id,disposition,retryable,code,sequence,canonical_version,record_kind,record_id,mutation_kind,base_version,mutation_hash,completed_at) VALUES(?,?,0,?,?,?,?,?,?,?,?,?)`, mutation.MutationID, result.Disposition, result.Code, sequence, result.Version, mutation.RecordKind, mutation.RecordID, mutation.Kind, mutation.BaseVersion, hash, now)
	if err != nil {
		return conflictOrWrite(ctx, err)
	}
	return nil
}

func syncReceiptMatches(ctx context.Context, tx *sql.Tx, result syncservice.Result, change *syncservice.Change) error {
	var disposition, code, kind, recordID, mutationKind string
	var retryable, canonical, base int64
	var sequence sql.NullInt64
	var hash []byte
	err := tx.QueryRowContext(ctx, `SELECT disposition,retryable,code,sequence,canonical_version,record_kind,record_id,mutation_kind,base_version,mutation_hash FROM sync_push_results WHERE mutation_id=?`, result.MutationID).Scan(&disposition, &retryable, &code, &sequence, &canonical, &kind, &recordID, &mutationKind, &base, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: sync outbox claim", ErrNotFound)
	}
	if err != nil || retryable != 0 || disposition != string(result.Disposition) || code != result.Code || canonical != result.Version || len(hash) != sha256.Size || (result.Sequence == nil) != !sequence.Valid || result.Sequence != nil && sequence.Int64 != *result.Sequence {
		return fmt.Errorf("%w: sync push receipt", ErrConflict)
	}
	if change == nil {
		return nil
	}
	mutationHash, hashErr := syncMutationHash(change.Mutation)
	if hashErr != nil || sequence.Int64 != change.Sequence || canonical != change.CanonicalVersion || kind != string(change.Mutation.RecordKind) || recordID != change.Mutation.RecordID || mutationKind != string(change.Mutation.Kind) || base != change.Mutation.BaseVersion || !bytes.Equal(hash, mutationHash) {
		return fmt.Errorf("%w: sync pull receipt", ErrConflict)
	}
	if disposition == string(syncservice.DispositionConflict) && change.ChangeDisposition != syncservice.ChangeDispositionConflict || (disposition == string(syncservice.DispositionAccepted) || disposition == string(syncservice.DispositionPreviouslyAccepted)) && change.ChangeDisposition != syncservice.ChangeDispositionAccepted && change.ChangeDisposition != "" {
		return fmt.Errorf("%w: sync pull disposition", ErrConflict)
	}
	return nil
}

func ownPulledReceipt(ctx context.Context, tx *sql.Tx, change syncservice.Change) (bool, error) {
	var disposition, code string
	var retryable, canonical int64
	var sequence sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT disposition,retryable,code,sequence,canonical_version FROM sync_push_results WHERE mutation_id=?`, change.Mutation.MutationID).Scan(&disposition, &retryable, &code, &sequence, &canonical)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || retryable != 0 || !sequence.Valid {
		return false, fmt.Errorf("%w: invalid sync push receipt", ErrCorrupt)
	}
	result := syncservice.Result{MutationID: change.Mutation.MutationID, Disposition: syncservice.Disposition(disposition), Code: code, Version: canonical, Sequence: &sequence.Int64}
	if err = syncReceiptMatches(ctx, tx, result, &change); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) rebaseFollowingSyncOutbox(ctx context.Context, tx *sql.Tx, completed syncservice.Mutation, version int64) error {
	var claimed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox o JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id WHERE o.record_kind=? AND o.record_id=? AND (o.created_at>(SELECT created_at FROM sync_outbox WHERE mutation_id=?) OR o.created_at=(SELECT created_at FROM sync_outbox WHERE mutation_id=?) AND o.id>(SELECT id FROM sync_outbox WHERE mutation_id=?))`, completed.RecordKind, completed.RecordID, completed.MutationID, completed.MutationID, completed.MutationID).Scan(&claimed); err != nil {
		return writeError(ctx, err)
	}
	if claimed != 0 {
		return fmt.Errorf("%w: later sync work is claimed", ErrConflict)
	}
	rows, err := tx.QueryContext(ctx, `SELECT mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,last_error_code,next_attempt_at,created_at,updated_at FROM sync_outbox WHERE record_kind=? AND record_id=? AND (created_at>(SELECT created_at FROM sync_outbox WHERE mutation_id=?) OR created_at=(SELECT created_at FROM sync_outbox WHERE mutation_id=?) AND id>(SELECT id FROM sync_outbox WHERE mutation_id=?)) ORDER BY created_at DESC,id DESC`, completed.RecordKind, completed.RecordID, completed.MutationID, completed.MutationID, completed.MutationID)
	if err != nil {
		return writeError(ctx, err)
	}
	defer rows.Close()
	var later []SyncOutboxEntry
	for rows.Next() {
		var id, kind, recordID, mutationKind, state, code string
		var base, payloadVersion, attempts, next, created, updated int64
		var payload []byte
		if err = rows.Scan(&id, &kind, &recordID, &mutationKind, &base, &payloadVersion, &payload, &state, &attempts, &code, &next, &created, &updated); err != nil {
			return writeError(ctx, err)
		}
		entry, decodeErr := decodeSyncOutboxEntry(id, kind, recordID, mutationKind, base, payloadVersion, payload, state, attempts, code, next, created, updated)
		if decodeErr != nil {
			return decodeErr
		}
		later = append(later, entry)
	}
	if err = rows.Err(); err != nil {
		return writeError(ctx, err)
	}
	if len(later) == 0 {
		return nil
	}
	return rebaseSyncOutboxEntries(ctx, tx, later, version)
}

// enqueueSyncOutbox is intentionally transaction-bound for a future local-write integration.
func (s *Store) enqueueSyncOutbox(ctx context.Context, tx syncSQL, mutation syncservice.Mutation) (syncservice.Mutation, error) {
	if err := cancelled(ctx); err != nil {
		return syncservice.Mutation{}, err
	}
	if tx == nil {
		return syncservice.Mutation{}, fmt.Errorf("%w: missing transaction", ErrInvalid)
	}
	if mutation.MutationID == "" {
		id, err := newSyncUUID()
		if err != nil {
			return syncservice.Mutation{}, writeError(ctx, err)
		}
		mutation.MutationID = id
	}
	if !canonicalUUIDPattern.MatchString(mutation.MutationID) || !allowedSyncMutation(mutation) || syncservice.ValidateMutation(mutation) != nil {
		return syncservice.Mutation{}, fmt.Errorf("%w: invalid sync mutation", ErrInvalid)
	}
	payload, err := json.Marshal(mutation)
	if err != nil || len(payload) > maxSyncPayloadBytes {
		return syncservice.Mutation{}, fmt.Errorf("%w: invalid sync payload", ErrInvalid)
	}
	return mutation, s.insertSyncOutbox(ctx, tx, mutation, payload)
}

func (s *Store) insertSyncOutbox(ctx context.Context, tx syncSQL, mutation syncservice.Mutation, payload []byte) error {
	now := s.now().UTC().Round(0)
	nanos, ok := syncUnixNano(now)
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	var completed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_push_results WHERE mutation_id=?`, mutation.MutationID).Scan(&completed); err != nil {
		return writeError(ctx, err)
	}
	if completed != 0 {
		return fmt.Errorf("%w: sync mutation identity", ErrConflict)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox(mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,1,?,'pending',0,?,?,?) ON CONFLICT(mutation_id) DO NOTHING`, mutation.MutationID, mutation.RecordKind, mutation.RecordID, mutation.Kind, mutation.BaseVersion, payload, nanos, nanos, nanos)
	if err != nil {
		return writeError(ctx, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return writeError(ctx, err)
	}
	if inserted == 1 {
		return nil
	}
	var recordKind, recordID, kind sql.NullString
	var baseVersion, payloadVersion int64
	var existingPayload []byte
	err = tx.QueryRowContext(ctx, `SELECT
		CASE WHEN record_kind IN ('project','session','observation') THEN record_kind ELSE NULL END,
		CASE WHEN typeof(record_id)='text' AND length(CAST(record_id AS BLOB)) BETWEEN 1 AND 1024 THEN record_id ELSE NULL END,
		CASE WHEN mutation_kind IN ('create','update','archive','tombstone','resolve') THEN mutation_kind ELSE NULL END,
		base_version,payload_version,CASE WHEN length(CAST(payload AS BLOB)) BETWEEN 1 AND 1048576 THEN payload ELSE NULL END
		FROM sync_outbox WHERE mutation_id=?`, mutation.MutationID).Scan(&recordKind, &recordID, &kind, &baseVersion, &payloadVersion, &existingPayload)
	if err != nil {
		return writeError(ctx, err)
	}
	if !recordKind.Valid || !recordID.Valid || !kind.Valid || existingPayload == nil {
		return fmt.Errorf("%w: invalid sync outbox payload", ErrCorrupt)
	}
	if recordKind.String != string(mutation.RecordKind) || recordID.String != mutation.RecordID || kind.String != string(mutation.Kind) || baseVersion != mutation.BaseVersion || payloadVersion != 1 || !bytes.Equal(existingPayload, payload) {
		return fmt.Errorf("%w: sync mutation identity", ErrConflict)
	}
	return nil
}

// enqueueLocalWrite adds the local semantic mutation and any missing identity
// snapshots to the same transaction. Canonical versions only advance when a
// future remote acknowledgement or pull is accepted.
func (s *Store) enqueueLocalWrite(ctx context.Context, tx *sql.Tx, item Observation) error {
	if err := rejectTombstoned(ctx, tx, item.ID); err != nil {
		return err
	}
	if err := activeProjectTransitionForLocalProject(ctx, tx, item.Project); err != nil {
		return err
	}
	var enabled int
	err := tx.QueryRowContext(ctx, `SELECT enabled FROM sync_profiles WHERE singleton=1`).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return writeError(ctx, err)
	}
	if enabled != 0 && enabled != 1 {
		return fmt.Errorf("%w: invalid sync profile", ErrCorrupt)
	}
	projectVersion, err := syncVersion(ctx, tx, `SELECT sync_version FROM projects WHERE id=?`, item.Project)
	if err != nil {
		return err
	}
	if projectVersion == 0 {
		if err = s.enqueueIdentity(ctx, tx, syncservice.Mutation{RecordID: item.Project, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: item.Project}}); err != nil {
			return err
		}
	}
	if item.Session != "" {
		sessionVersion, sessionErr := syncVersion(ctx, tx, `SELECT sync_version FROM sessions WHERE id=? AND project_id=?`, item.Session, item.Project)
		if sessionErr != nil {
			return sessionErr
		}
		if sessionVersion == 0 {
			if err = s.enqueueIdentity(ctx, tx, syncservice.Mutation{RecordID: item.Session, RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: item.Session, ProjectID: item.Project}}); err != nil {
				return err
			}
		}
	}
	version, err := syncVersion(ctx, tx, `SELECT sync_version FROM observations WHERE id=?`, item.ID)
	if err != nil {
		return err
	}
	snapshot := syncObservation(item)
	kind := syncservice.MutationCreate
	if version > 0 {
		kind = syncservice.MutationUpdate
		if item.State == StateArchived {
			kind = syncservice.MutationArchive
		}
	}
	_, err = s.enqueueSyncOutbox(ctx, tx, syncservice.Mutation{RecordID: item.ID, RecordKind: syncservice.RecordKindObservation, Kind: kind, BaseVersion: version, Observation: &snapshot})
	return err
}

func syncVersion(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	var version int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&version); err != nil {
		return 0, writeError(ctx, err)
	}
	if version < 0 {
		return 0, fmt.Errorf("%w: invalid sync version", ErrCorrupt)
	}
	return version, nil
}

func (s *Store) enqueueIdentity(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation) error {
	var id sql.NullString
	var payload []byte
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN typeof(mutation_id)='text' AND length(CAST(mutation_id AS BLOB))=36 THEN mutation_id ELSE NULL END,CASE WHEN length(CAST(payload AS BLOB)) BETWEEN 1 AND 1048576 THEN payload ELSE NULL END FROM sync_outbox WHERE record_kind=? AND record_id=? AND mutation_kind='create' AND base_version=0 ORDER BY id LIMIT 1`, mutation.RecordKind, mutation.RecordID).Scan(&id, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.enqueueSyncOutbox(ctx, tx, mutation)
		return err
	}
	if err != nil {
		return writeError(ctx, err)
	}
	if !id.Valid || !canonicalUUIDPattern.MatchString(id.String) {
		return fmt.Errorf("%w: invalid sync outbox mutation id", ErrCorrupt)
	}
	if payload == nil {
		return fmt.Errorf("%w: invalid sync outbox payload", ErrCorrupt)
	}
	mutation.MutationID = id.String
	expected, marshalErr := json.Marshal(mutation)
	if marshalErr != nil || !bytes.Equal(payload, expected) {
		return fmt.Errorf("%w: sync identity snapshot", ErrConflict)
	}
	return nil
}

// ApplyPulledChange atomically records one verified, ordered remote change.
func (s *Store) ApplyPulledChange(ctx context.Context, historyID string, change syncservice.Change) error {
	return s.ApplyPulledPage(ctx, syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: historyID, Position: change.Sequence, Watermark: change.Sequence}, Changes: []syncservice.Change{change}}, nil)
}

// ApplyProjectPulledPage atomically materializes one sparse, portable-project
// pull page. Its cursor and inbox are intentionally separate from global pull
// state, because project pages omit unrelated history positions.
func (s *Store) ApplyProjectPulledPage(ctx context.Context, portableProject, localProject string, page syncservice.PullPage) error {
	return s.applyProjectPulledPage(ctx, portableProject, localProject, page, 0)
}

// ApplyProjectPulledPageForTransition materializes a page only while the
// selected transition incarnation remains current.
func (s *Store) ApplyProjectPulledPageForTransition(ctx context.Context, portableProject, localProject string, page syncservice.PullPage, transitionIdentity int64) error {
	return s.applyProjectPulledPage(ctx, portableProject, localProject, page, transitionIdentity)
}

func (s *Store) applyProjectPulledPage(ctx context.Context, portableProject, localProject string, page syncservice.PullPage, transitionIdentity int64) error {
	if s == nil || s.readOnly || !projectIDPattern.MatchString(portableProject) || localProject == "" || validateProjectPulledPage(portableProject, page) != nil {
		return fmt.Errorf("%w: project pulled page", ErrInvalid)
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	if transitionIdentity != 0 {
		if err = activeTransitionIdentityMatches(ctx, tx, portableProject, localProject, transitionIdentity); err != nil {
			return err
		}
	}
	if err = pendingProjectRepairForProject(ctx, tx, portableProject, localProject); err != nil {
		return err
	}
	if err = pulledProject(ctx, tx, localProject); err != nil {
		return err
	}
	position, watermark, err := projectPulledCursor(ctx, tx, portableProject, page.Cursor.HistoryID)
	if err != nil {
		return err
	}
	if page.Cursor.Position < position || (position < watermark && watermark != page.Cursor.Watermark) || (position == watermark && page.Cursor.Watermark < watermark) {
		return fmt.Errorf("%w: project pulled cursor", ErrConflict)
	}
	for _, change := range page.Changes {
		hash, _ := hex.DecodeString(change.ChangeHash)
		if change.Sequence <= position {
			stored, rowErr := projectPulledInboxRow(ctx, tx, portableProject, page.Cursor.HistoryID, change.Sequence)
			if rowErr != nil || !bytes.Equal(stored, hash) {
				return fmt.Errorf("%w: project pulled replay", ErrCorrupt)
			}
			continue
		}
		mapped, mapErr := s.mapProjectPulledChange(ctx, tx, portableProject, localProject, change)
		if mapErr != nil {
			return mapErr
		}
		handled, transitionErr := s.applyProjectTransitionPulledMutation(ctx, tx, portableProject, localProject, mapped)
		if transitionErr != nil {
			return transitionErr
		}
		if !handled {
			if err = s.applyProjectPulledMutation(ctx, tx, page.Cursor.HistoryID, mapped); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_project_inbox(portable_project_id,history_id,seq,change_hash,applied_at) VALUES(?,?,?,?,?)`, portableProject, page.Cursor.HistoryID, change.Sequence, hash, now); err != nil {
			return writeError(ctx, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_project_cursor(portable_project_id,history_id,position,watermark,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(portable_project_id) DO UPDATE SET history_id=excluded.history_id,position=excluded.position,watermark=excluded.watermark,updated_at=excluded.updated_at`, portableProject, page.Cursor.HistoryID, page.Cursor.Position, page.Cursor.Watermark, now); err != nil {
		return writeError(ctx, err)
	}
	return commit(ctx, tx)
}

func transitionIdentityMatches(ctx context.Context, db syncSQL, portableProject, localProject string, transitionIdentity int64) error {
	if !projectIDPattern.MatchString(portableProject) || localProject == "" || transitionIdentity <= 0 {
		return fmt.Errorf("%w: project sync transition", ErrInvalid)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sync_project_transitions WHERE portable_project_id=? AND local_project_id=? AND created_at=?`, portableProject, localProject, transitionIdentity).Scan(&count); err != nil {
		return writeError(ctx, err)
	}
	if count != 1 {
		return fmt.Errorf("%w: project sync transition", ErrConflict)
	}
	return nil
}

func activeTransitionIdentityMatches(ctx context.Context, db syncSQL, portableProject, localProject string, transitionIdentity int64) error {
	if !projectIDPattern.MatchString(portableProject) || localProject == "" || transitionIdentity <= 0 {
		return fmt.Errorf("%w: project sync transition", ErrInvalid)
	}
	var bound, mode, status string
	var createdAt int64
	var completedAt sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT local_project_id,mode,status,created_at,completed_at FROM sync_project_transitions WHERE portable_project_id=?`, portableProject).Scan(&bound, &mode, &status, &createdAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: project sync transition", ErrConflict)
	}
	if err != nil {
		return writeError(ctx, err)
	}
	if bound != localProject || createdAt != transitionIdentity {
		return fmt.Errorf("%w: project sync transition", ErrConflict)
	}
	if createdAt <= 0 || (mode != string(SyncProjectTransitionReseedSource) && mode != string(SyncProjectTransitionRejoinMerge)) || (status != SyncProjectTransitionPulling && status != SyncProjectTransitionPublishing && status != SyncProjectTransitionCompleted) || (mode == string(SyncProjectTransitionReseedSource) && status == SyncProjectTransitionPulling) || (status == SyncProjectTransitionCompleted && (!completedAt.Valid || completedAt.Int64 <= 0 || completedAt.Int64 < createdAt)) || (status != SyncProjectTransitionCompleted && completedAt.Valid) {
		return fmt.Errorf("%w: project sync transition", ErrCorrupt)
	}
	if status == SyncProjectTransitionCompleted {
		return fmt.Errorf("%w: project sync transition", ErrConflict)
	}
	return nil
}

func (s *Store) applyProjectTransitionPulledMutation(ctx context.Context, tx *sql.Tx, portableProject, localProject string, change syncservice.Change) (bool, error) {
	var bound, mode, status string
	err := tx.QueryRowContext(ctx, `SELECT local_project_id,mode,status FROM sync_project_transitions WHERE portable_project_id=?`, portableProject).Scan(&bound, &mode, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, writeError(ctx, err)
	}
	if bound != localProject || mode != string(SyncProjectTransitionReseedSource) && mode != string(SyncProjectTransitionRejoinMerge) || status != SyncProjectTransitionPulling && status != SyncProjectTransitionPublishing && status != SyncProjectTransitionCompleted {
		return false, fmt.Errorf("%w: project sync transition", ErrCorrupt)
	}
	if status == SyncProjectTransitionCompleted {
		return false, nil
	}
	mutation := change.Mutation
	var expected []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM sync_project_transition_records WHERE portable_project_id=? AND record_kind=? AND local_id=?`, portableProject, mutation.RecordKind, mutation.RecordID).Scan(&expected)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || len(expected) != sha256.Size {
		return false, fmt.Errorf("%w: project sync transition snapshot", ErrCorrupt)
	}
	actual, err := projectTransitionMutationHash(mutation)
	if err != nil {
		return false, err
	}
	if mutation.Kind != syncservice.MutationCreate || mutation.BaseVersion != 0 || change.CanonicalVersion != 1 || !bytes.Equal(actual, expected) {
		return false, fmt.Errorf("%w: project sync transition diverged", ErrConflict)
	}
	var result sql.Result
	switch mutation.RecordKind {
	case syncservice.RecordKindProject:
		result, err = tx.ExecContext(ctx, `UPDATE projects SET sync_version=1 WHERE id=?`, mutation.RecordID)
	case syncservice.RecordKindSession:
		result, err = tx.ExecContext(ctx, `UPDATE sessions SET sync_version=1 WHERE id=? AND project_id=?`, mutation.RecordID, localProject)
	case syncservice.RecordKindObservation:
		result, err = tx.ExecContext(ctx, `UPDATE observations SET sync_version=1 WHERE id=? AND project_id=?`, mutation.RecordID, localProject)
	default:
		return false, fmt.Errorf("%w: project sync transition kind", ErrCorrupt)
	}
	if err != nil {
		return false, writeError(ctx, err)
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return false, fmt.Errorf("%w: project sync transition record", ErrConflict)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_project_transition_records SET seen_remote=1 WHERE portable_project_id=? AND record_kind=? AND local_id=?`, portableProject, mutation.RecordKind, mutation.RecordID); err != nil {
		return false, writeError(ctx, err)
	}
	return true, nil
}

// ProjectPullCursor returns the durable cursor for one portable project and
// history. It never reads or updates the owner-global sync cursor.
func (s *Store) ProjectPullCursor(ctx context.Context, portableProject, historyID string) (syncservice.Cursor, error) {
	if s == nil || ctx == nil || !projectIDPattern.MatchString(portableProject) || !canonicalUUIDPattern.MatchString(historyID) {
		return syncservice.Cursor{}, fmt.Errorf("%w: project pull cursor", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return syncservice.Cursor{}, err
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return syncservice.Cursor{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	position, watermark, err := projectPulledCursor(ctx, tx, portableProject, historyID)
	if err != nil {
		return syncservice.Cursor{}, err
	}
	if position == watermark {
		watermark = 0
	}
	if err = tx.Commit(); err != nil {
		return syncservice.Cursor{}, writeError(ctx, err)
	}
	return syncservice.Cursor{HistoryID: historyID, Position: position, Watermark: watermark}, nil
}

// ReadProjectPullCursor returns the raw persisted cursor for one portable
// project. Unlike ProjectPullCursor, it neither accepts a remote history nor
// normalizes a completed watermark.
func (s *Store) ReadProjectPullCursor(ctx context.Context, portableProject string) (syncservice.Cursor, bool, error) {
	if s == nil || ctx == nil || !projectIDPattern.MatchString(portableProject) {
		return syncservice.Cursor{}, false, fmt.Errorf("%w: project pull cursor", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return syncservice.Cursor{}, false, err
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return syncservice.Cursor{}, false, writeError(ctx, err)
	}
	defer tx.Rollback()
	var cursor syncservice.Cursor
	err = tx.QueryRowContext(ctx, `SELECT history_id,position,watermark FROM sync_project_cursor WHERE portable_project_id=?`, portableProject).Scan(&cursor.HistoryID, &cursor.Position, &cursor.Watermark)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.Commit(); err != nil {
			return syncservice.Cursor{}, false, writeError(ctx, err)
		}
		return syncservice.Cursor{}, false, nil
	}
	if err != nil {
		return syncservice.Cursor{}, false, writeError(ctx, err)
	}
	if !canonicalUUIDPattern.MatchString(cursor.HistoryID) || cursor.Position < 0 || cursor.Watermark < cursor.Position {
		return syncservice.Cursor{}, false, fmt.Errorf("%w: project pull cursor", ErrCorrupt)
	}
	if err = tx.Commit(); err != nil {
		return syncservice.Cursor{}, false, writeError(ctx, err)
	}
	return cursor, true, nil
}

func validateProjectPulledPage(project string, page syncservice.PullPage) error {
	if !projectIDPattern.MatchString(project) || !canonicalUUIDPattern.MatchString(page.Cursor.HistoryID) || page.Cursor.Position < 0 || page.Cursor.Watermark < 0 || page.Cursor.Position > page.Cursor.Watermark || page.HasMore != (page.Cursor.Position < page.Cursor.Watermark) {
		return ErrInvalid
	}
	if len(page.Changes) == 0 {
		if page.HasMore || page.Cursor.Position != page.Cursor.Watermark {
			return ErrInvalid
		}
		return nil
	}
	if page.Changes[len(page.Changes)-1].Sequence != page.Cursor.Position {
		return ErrInvalid
	}
	previous := int64(0)
	for _, change := range page.Changes {
		if change.Sequence <= previous || change.Sequence > page.Cursor.Watermark || !validPulledChange(change) || syncapi.ValidateProjectPullChange(change, project) != nil {
			return ErrInvalid
		}
		previous = change.Sequence
	}
	return nil
}

func projectPulledCursor(ctx context.Context, tx *sql.Tx, project, history string) (int64, int64, error) {
	var storedHistory, historyType, positionType, watermarkType, updatedType string
	var position, watermark, updated int64
	err := tx.QueryRowContext(ctx, `SELECT history_id,position,watermark,updated_at,typeof(history_id),typeof(position),typeof(watermark),typeof(updated_at) FROM sync_project_cursor WHERE portable_project_id=?`, project).Scan(&storedHistory, &position, &watermark, &updated, &historyType, &positionType, &watermarkType, &updatedType)
	if errors.Is(err, sql.ErrNoRows) {
		if err = validateProjectPulledInbox(ctx, tx, project, history, 0); err != nil {
			return 0, 0, fmt.Errorf("%w: project cursor inbox", ErrCorrupt)
		}
		return 0, 0, nil
	}
	if err != nil || historyType != "text" || positionType != "integer" || watermarkType != "integer" || updatedType != "integer" || !canonicalUUIDPattern.MatchString(storedHistory) || storedHistory != history || position < 0 || watermark < position || !validStoredSyncTime(updated, time.Unix(0, updated)) {
		return 0, 0, fmt.Errorf("%w: project pulled cursor", ErrCorrupt)
	}
	if err = validateProjectPulledInbox(ctx, tx, project, history, position); err != nil {
		return 0, 0, fmt.Errorf("%w: project cursor inbox", ErrCorrupt)
	}
	return position, watermark, nil
}

func validateProjectPulledInbox(ctx context.Context, tx *sql.Tx, project, history string, position int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT portable_project_id,history_id,seq,change_hash,applied_at,typeof(portable_project_id),typeof(history_id),typeof(seq),typeof(change_hash),typeof(applied_at) FROM sync_project_inbox WHERE portable_project_id=?`, project)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var storedProject, storedHistory, projectType, historyType, sequenceType, hashType, appliedType string
		var sequence, applied int64
		var hash []byte
		if err = rows.Scan(&storedProject, &storedHistory, &sequence, &hash, &applied, &projectType, &historyType, &sequenceType, &hashType, &appliedType); err != nil || projectType != "text" || historyType != "text" || sequenceType != "integer" || hashType != "blob" || appliedType != "integer" || storedProject != project || storedHistory != history || sequence < 1 || sequence > position || len(hash) != sha256.Size || !validStoredSyncTime(applied, time.Unix(0, applied)) {
			return ErrCorrupt
		}
	}
	return rows.Err()
}

func projectPulledInboxRow(ctx context.Context, tx *sql.Tx, project, history string, sequence int64) ([]byte, error) {
	var hash []byte
	var storedProject, storedHistory string
	var storedSequence int64
	err := tx.QueryRowContext(ctx, `SELECT portable_project_id,history_id,seq,change_hash FROM sync_project_inbox WHERE portable_project_id=? AND history_id=? AND seq=?`, project, history, sequence).Scan(&storedProject, &storedHistory, &storedSequence, &hash)
	if err != nil || storedProject != project || storedHistory != history || storedSequence != sequence || len(hash) != sha256.Size {
		return nil, fmt.Errorf("%w: project cursor inbox", ErrCorrupt)
	}
	return hash, nil
}

func (s *Store) mapProjectPulledChange(ctx context.Context, tx *sql.Tx, portableProject, localProject string, change syncservice.Change) (syncservice.Change, error) {
	mapped := change
	mapped.Mutation = cloneSyncMutation(change.Mutation)
	m := &mapped.Mutation
	mapExisting := func(kind syncservice.RecordKind, wire string) (string, error) {
		var local, origin string
		err := tx.QueryRowContext(ctx, `SELECT local_id,origin_device_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND portable_id=?`, portableProject, kind, wire).Scan(&local, &origin)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: project portable identity", ErrConflict)
		}
		if err != nil {
			return "", writeError(ctx, err)
		}
		if len([]byte(local)) == 0 || len([]byte(local)) > 1024 || !canonicalUUIDPattern.MatchString(origin) {
			return "", fmt.Errorf("%w: project portable identity", ErrCorrupt)
		}
		portable, err := s.validSyncPortableIdentity(ctx, tx, portableProject, kind, local, wire, origin)
		if err != nil || portable != wire {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("%w: project portable identity", ErrCorrupt)
		}
		if !s.localSyncRecordBelongsToProject(ctx, tx, kind, local, localProject) {
			return "", fmt.Errorf("%w: project portable identity", ErrCorrupt)
		}
		return local, nil
	}
	adopt := func(kind syncservice.RecordKind, wire string) (string, error) {
		local, err := mapExisting(kind, wire)
		if err == nil {
			return local, nil
		}
		if !errors.Is(err, ErrConflict) {
			return "", err
		}
		profile, found, err := readStoredSyncProfile(ctx, tx)
		if err != nil || !found || !profile.Enabled {
			return "", fmt.Errorf("%w: sync profile", ErrConflict)
		}
		local = adoptedSyncLocalID(kind, wire)
		_, err = tx.ExecContext(ctx, `INSERT INTO sync_portable_identities(portable_project_id,record_kind,local_id,portable_id,origin_device_id,created_at) VALUES(?,?,?,?,?,?)`, portableProject, kind, local, wire, profile.DeviceID, s.now().UTC().UnixNano())
		if err != nil {
			return "", conflictOrWrite(ctx, err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sync_portable_identity_adoptions(portable_project_id,record_kind,local_id,portable_id,adopting_device_id,adopted_at) VALUES(?,?,?,?,?,?)`, portableProject, kind, local, wire, profile.DeviceID, s.now().UTC().UnixNano())
		if err != nil {
			return "", conflictOrWrite(ctx, err)
		}
		return local, nil
	}
	switch m.RecordKind {
	case syncservice.RecordKindProject:
		m.RecordID, m.Project.ID = localProject, localProject
	case syncservice.RecordKindSession:
		local, err := adopt(syncservice.RecordKindSession, m.RecordID)
		if err != nil {
			return mapped, err
		}
		m.RecordID, m.Session.ID, m.Session.ProjectID = local, local, localProject
	case syncservice.RecordKindObservation:
		mapRecord := adopt
		if m.Kind == syncservice.MutationTombstone || m.Kind == syncservice.MutationResolve {
			mapRecord = mapExisting
		}
		local, err := mapRecord(syncservice.RecordKindObservation, m.RecordID)
		if err != nil {
			return mapped, err
		}
		m.RecordID = local
		if m.Observation != nil {
			m.Observation.ID, m.Observation.ProjectID = local, localProject
			if m.Observation.SessionID != "" {
				session, err := mapExisting(syncservice.RecordKindSession, m.Observation.SessionID)
				if err != nil {
					return mapped, err
				}
				m.Observation.SessionID = session
			}
			for i, wire := range m.Observation.References {
				localRef, err := mapExisting(syncservice.RecordKindObservation, wire)
				if err != nil {
					return mapped, err
				}
				m.Observation.References[i] = localRef
			}
		}
		if m.Tombstone != nil {
			m.Tombstone.ProjectID = localProject
		}
		if m.Resolution != nil && m.Resolution.Observation != nil {
			winner := m.Resolution.Observation
			winner.ID, winner.ProjectID = local, localProject
			if winner.SessionID != "" {
				session, err := mapExisting(syncservice.RecordKindSession, winner.SessionID)
				if err != nil {
					return mapped, err
				}
				winner.SessionID = session
			}
			for i, wire := range winner.References {
				localRef, err := mapExisting(syncservice.RecordKindObservation, wire)
				if err != nil {
					return mapped, err
				}
				winner.References[i] = localRef
			}
		}
	}
	return mapped, nil
}

func (s *Store) applyProjectPulledMutation(ctx context.Context, tx *sql.Tx, history string, change syncservice.Change) error {
	if change.Mutation.Kind == syncservice.MutationCreate || change.Mutation.Kind == syncservice.MutationUpdate {
		own, err := ownPulledReceipt(ctx, tx, change)
		if err != nil {
			return err
		}
		if own && change.ChangeDisposition != syncservice.ChangeDispositionConflict {
			return nil
		}
	}
	if change.Mutation.RecordKind == syncservice.RecordKindProject && change.Mutation.Kind == syncservice.MutationCreate {
		var version int64
		if err := tx.QueryRowContext(ctx, `SELECT sync_version FROM projects WHERE id=?`, change.Mutation.RecordID).Scan(&version); err != nil || change.Mutation.BaseVersion != 0 || change.CanonicalVersion != 1 {
			return fmt.Errorf("%w: project pulled identity", ErrConflict)
		}
		if version == 1 {
			own, err := ownPulledReceipt(ctx, tx, change)
			if err != nil || !own {
				return fmt.Errorf("%w: project pulled identity", ErrConflict)
			}
			return nil
		}
		if version != 0 {
			return fmt.Errorf("%w: project pulled identity", ErrConflict)
		}
		_, err := tx.ExecContext(ctx, `UPDATE projects SET sync_version=1 WHERE id=?`, change.Mutation.RecordID)
		return writeError(ctx, err)
	}
	return s.applyPulledMutation(ctx, tx, history, change)
}

// ApplyPulledPage validates a page before opening SQLite, then commits its
// materialization, inbox entries, cursor, and optional checkpoint together.
func (s *Store) ApplyPulledPage(ctx context.Context, page syncservice.PullPage, checkpoint *BootstrapCheckpoint) error {
	return s.applyPulledPage(ctx, page, checkpoint, pulledPageDefault)
}

func (s *Store) applyPulledPage(ctx context.Context, page syncservice.PullPage, checkpoint *BootstrapCheckpoint, mode pulledPageMode) error {
	allowPendingBootstrap, stopAtPending := mode.allowsPendingBootstrap(), mode.stopsAtPending()
	if err := cancelled(ctx); err != nil || validatePulledPage(page, checkpoint) != nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: invalid pulled page", ErrInvalid)
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.syncInbox.known = false
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	if err = pendingProjectRepairBarrier(ctx, tx); err != nil {
		return err
	}
	epoch, err := syncDataVersion(ctx, tx)
	if err != nil {
		s.syncInbox.known = false
		return err
	}
	if checkpoint != nil && !allowPendingBootstrap {
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox WHERE state IN ('pending','retry')`).Scan(&pending); err != nil {
			s.syncInbox.known = false
			return writeError(ctx, err)
		}
		if pending != 0 {
			return fmt.Errorf("%w: local sync work is pending", ErrConflict)
		}
	}
	position := page.Cursor.Position
	if len(page.Changes) == 0 {
		if err = pulledEmptyCursor(ctx, tx, page.Cursor.HistoryID, position); err != nil {
			s.syncInbox.known = false
			return err
		}
	} else {
		first := page.Changes[0]
		firstHash, _ := hex.DecodeString(first.ChangeHash)
		var cached bool
		position, _, cached, err = cachedPulledCursor(ctx, tx, page.Cursor.HistoryID, first.Sequence, firstHash, s.syncInbox, epoch)
		if !cached && err == nil {
			position, _, err = pulledCursor(ctx, tx, page.Cursor.HistoryID, first.Sequence, firstHash)
		}
		if err != nil {
			s.syncInbox.known = false
			return err
		}
		if position == int64(^uint64(0)>>1) || first.Sequence > position+1 {
			return fmt.Errorf("%w: noncontiguous pulled change", ErrConflict)
		}
	}
	if checkpoint != nil {
		if err = verifyBootstrapCheckpoint(ctx, tx, *checkpoint, page.Cursor.HistoryID, position, mode.allowsResolutionAdvance()); err != nil {
			s.syncInbox.known = false
			return err
		}
	}
	priorPosition := position
	applied, blocked := false, false
	for _, change := range page.Changes {
		hash, _ := hex.DecodeString(change.ChangeHash)
		if change.Sequence <= position {
			stored, rowErr := pulledInboxRow(ctx, tx, page.Cursor.HistoryID, change.Sequence)
			if rowErr != nil || !bytes.Equal(stored, hash) {
				s.syncInbox.known = false
				return fmt.Errorf("%w: pulled replay does not match inbox", ErrCorrupt)
			}
			continue
		}
		if change.Sequence != position+1 {
			return fmt.Errorf("%w: noncontiguous pulled change", ErrConflict)
		}
		if stopAtPending && change.ChangeDisposition == syncservice.ChangeDispositionConflict {
			blocked = true
			break
		}
		own, ownErr := ownPulledReceipt(ctx, tx, change)
		if ownErr != nil {
			s.syncInbox.known = false
			return ownErr
		}
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox WHERE record_kind=? AND record_id=? AND state IN ('pending','retry')`, change.Mutation.RecordKind, change.Mutation.RecordID).Scan(&pending); err != nil {
			s.syncInbox.known = false
			return writeError(ctx, err)
		}
		if pending != 0 && !own && change.Mutation.Kind != syncservice.MutationResolve {
			if stopAtPending {
				blocked = true
				break
			}
			return fmt.Errorf("%w: local sync work is pending", ErrConflict)
		}
		if !own || change.ChangeDisposition == syncservice.ChangeDispositionConflict {
			if err = s.applyPulledMutation(ctx, tx, page.Cursor.HistoryID, change); err != nil {
				if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
					s.syncInbox.known = false
				}
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_inbox(history_id,seq,change_hash,applied_at) VALUES(?,?,?,?)`, page.Cursor.HistoryID, change.Sequence, hash, now); err != nil {
			s.syncInbox.known = false
			return writeError(ctx, err)
		}
		position = change.Sequence
		applied = true
	}
	if applied {
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_cursor(singleton,history_id,position,updated_at) VALUES(1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET history_id=excluded.history_id,position=excluded.position,updated_at=excluded.updated_at`, page.Cursor.HistoryID, position, now); err != nil {
			return writeError(ctx, err)
		}
	}
	if checkpoint != nil && (!stopAtPending || applied) {
		next := *checkpoint
		if stopAtPending {
			next.Position = position
			if position == next.Watermark {
				next.Phase = "complete"
			} else {
				next.Phase = "observations"
			}
			if err = verifyBootstrapCheckpoint(ctx, tx, next, page.Cursor.HistoryID, priorPosition, mode.allowsResolutionAdvance()); err != nil {
				return err
			}
		}
		payload, marshalErr := json.Marshal(next)
		if marshalErr != nil || len(payload) == 0 || len(payload) > maxSyncPayloadBytes {
			return fmt.Errorf("%w: invalid bootstrap checkpoint", ErrInvalid)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_bootstrap(singleton,phase,payload_version,checkpoint,created_at,updated_at) VALUES(1,?,1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET phase=excluded.phase,checkpoint=excluded.checkpoint,updated_at=excluded.updated_at`, next.Phase, payload, now, now); err != nil {
			return writeError(ctx, err)
		}
	}
	if err = commit(ctx, tx); err != nil {
		return err
	}
	s.syncInbox = syncInboxCache{known: true, dataVersion: epoch, historyID: page.Cursor.HistoryID, position: position}
	if blocked {
		return fmt.Errorf("%w: local sync work is pending", ErrConflict)
	}
	return nil
}

func validatePulledPage(page syncservice.PullPage, checkpoint *BootstrapCheckpoint) error {
	if !canonicalUUIDPattern.MatchString(page.Cursor.HistoryID) || page.Cursor.Position < 0 || page.Cursor.Watermark < page.Cursor.Position || page.HasMore != (page.Cursor.Position < page.Cursor.Watermark) {
		return ErrInvalid
	}
	if len(page.Changes) == 0 {
		if page.Cursor.Position != page.Cursor.Watermark || page.HasMore {
			return ErrInvalid
		}
	} else if page.Cursor.Position < 1 || page.Changes[len(page.Changes)-1].Sequence != page.Cursor.Position {
		return ErrInvalid
	}
	first := page.Cursor.Position - int64(len(page.Changes)) + 1
	for i, change := range page.Changes {
		if change.Sequence != first+int64(i) || !validPulledChange(change) {
			return ErrInvalid
		}
	}
	if checkpoint != nil && (!canonicalUUIDPattern.MatchString(checkpoint.HistoryID) || checkpoint.HistoryID != page.Cursor.HistoryID || checkpoint.Position != page.Cursor.Position || checkpoint.Watermark != page.Cursor.Watermark || checkpointPhaseRank(checkpoint.Phase) == 0 || checkpoint.Phase == "complete" && checkpoint.Position != checkpoint.Watermark) {
		return ErrInvalid
	}
	return nil
}

func checkpointPhaseRank(phase string) int {
	switch phase {
	case "projects":
		return 1
	case "sessions":
		return 2
	case "observations":
		return 3
	case "complete":
		return 4
	default:
		return 0
	}
}

func validPulledChange(change syncservice.Change) bool {
	if change.Sequence <= 0 || change.CanonicalVersion <= 0 || syncservice.ValidateMutation(change.Mutation) != nil || syncservice.VerifyChangeHash(change) != nil {
		return false
	}
	hash, err := hex.DecodeString(change.ChangeHash)
	if err != nil || len(hash) != 32 {
		return false
	}
	special := change.Mutation.Kind == syncservice.MutationTombstone || change.Mutation.Kind == syncservice.MutationResolve || change.ChangeDisposition == syncservice.ChangeDispositionConflict
	if !special {
		return (change.HashVersion == nil || *change.HashVersion == 1) && change.ChangeDisposition == "" && change.ConflictID == "" && ordinaryPulledMutation(change.Mutation)
	}
	if change.HashVersion == nil || *change.HashVersion != 2 {
		return false
	}
	if change.ChangeDisposition == syncservice.ChangeDispositionConflict {
		return change.Mutation.RecordKind == syncservice.RecordKindObservation && canonicalUUIDPattern.MatchString(change.ConflictID)
	}
	return change.ChangeDisposition == syncservice.ChangeDispositionAccepted && change.ConflictID == "" && (change.Mutation.Kind == syncservice.MutationTombstone || change.Mutation.Kind == syncservice.MutationResolve)
}

func verifyBootstrapCheckpoint(ctx context.Context, tx *sql.Tx, next BootstrapCheckpoint, historyID string, position int64, allowResolutionAdvance bool) error {
	var phase, phaseType, versionType, payloadType string
	var version int64
	var payload []byte
	err := tx.QueryRowContext(ctx, `SELECT phase,payload_version,checkpoint,typeof(phase),typeof(payload_version),typeof(checkpoint) FROM sync_bootstrap WHERE singleton=1`).Scan(&phase, &version, &payload, &phaseType, &versionType, &payloadType)
	if errors.Is(err, sql.ErrNoRows) {
		if position != 0 {
			return fmt.Errorf("%w: missing bootstrap checkpoint", ErrConflict)
		}
		return nil
	}
	if err != nil {
		return writeError(ctx, err)
	}
	if phaseType != "text" || versionType != "integer" || payloadType != "blob" || version != 1 || checkpointPhaseRank(phase) == 0 || len(payload) == 0 || len(payload) > maxSyncPayloadBytes {
		return fmt.Errorf("%w: invalid bootstrap checkpoint", ErrCorrupt)
	}
	var prior BootstrapCheckpoint
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&prior) != nil || decoder.Decode(&extra) != io.EOF || prior.Phase != "" {
		return fmt.Errorf("%w: bootstrap checkpoint mismatch", ErrConflict)
	}
	resolutionAdvance := allowResolutionAdvance && (phase == "observations" || phase == "complete") && next.Position > position && next.Position <= next.Watermark && next.Watermark >= prior.Watermark && (next.Position == next.Watermark && next.Phase == "complete" || next.Position < next.Watermark && next.Phase == "observations")
	if prior.HistoryID != historyID || prior.Position != position || (allowResolutionAdvance && !resolutionAdvance) || (prior.Watermark != next.Watermark && !resolutionAdvance) || (phase == "complete" && !resolutionAdvance && (next.Phase != "complete" || next.Position != position)) || (phase != "complete" && checkpointPhaseRank(next.Phase) < checkpointPhaseRank(phase)) {
		return fmt.Errorf("%w: bootstrap checkpoint mismatch", ErrConflict)
	}
	return nil
}

func ordinaryPulledMutation(m syncservice.Mutation) bool {
	if m.RecordKind == syncservice.RecordKindProject || m.RecordKind == syncservice.RecordKindSession {
		return m.Kind == syncservice.MutationCreate || m.Kind == syncservice.MutationUpdate
	}
	if m.RecordKind != syncservice.RecordKindObservation || m.Observation == nil || m.Observation.Review != syncservice.ReviewClear && m.Observation.Review != syncservice.ReviewNeedsReview {
		return false
	}
	switch m.Kind {
	case syncservice.MutationCreate:
		return m.Observation.Lifecycle == syncservice.LifecycleActive || m.Observation.Lifecycle == syncservice.LifecycleArchived
	case syncservice.MutationUpdate:
		return m.Observation.Lifecycle == syncservice.LifecycleActive
	case syncservice.MutationArchive:
		return m.Observation.Lifecycle == syncservice.LifecycleArchived
	default:
		return false
	}
}

func syncDataVersion(ctx context.Context, tx *sql.Tx) (int64, error) {
	var version int64
	if err := tx.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&version); err != nil {
		return 0, writeError(ctx, err)
	}
	if version < 0 {
		return 0, fmt.Errorf("%w: invalid sync data version", ErrCorrupt)
	}
	return version, nil
}

func cachedPulledCursor(ctx context.Context, tx *sql.Tx, historyID string, sequence int64, hash []byte, cache syncInboxCache, epoch int64) (int64, bool, bool, error) {
	if !cache.known || cache.dataVersion != epoch {
		return 0, false, false, nil
	}
	var durableHistory string
	var position, updated int64
	err := tx.QueryRowContext(ctx, `SELECT history_id,position,updated_at FROM sync_cursor WHERE singleton=1`).Scan(&durableHistory, &position, &updated)
	if errors.Is(err, sql.ErrNoRows) || err != nil || !canonicalUUIDPattern.MatchString(durableHistory) || position < 0 || !validStoredSyncTime(updated, time.Unix(0, updated)) || durableHistory != cache.historyID || position != cache.position || durableHistory != historyID {
		return 0, false, false, nil
	}
	if position > 0 {
		if _, err = pulledInboxRow(ctx, tx, historyID, position); err != nil {
			return 0, false, true, err
		}
	}
	if sequence <= position {
		stored, rowErr := pulledInboxRow(ctx, tx, historyID, sequence)
		if rowErr != nil || !bytes.Equal(stored, hash) {
			return 0, false, true, fmt.Errorf("%w: pulled replay does not match inbox", ErrCorrupt)
		}
		return position, true, true, nil
	}
	var next int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_inbox WHERE history_id=? AND seq=?`, historyID, position+1).Scan(&next); err != nil {
		return 0, false, true, writeError(ctx, err)
	}
	if next != 0 {
		return 0, false, true, fmt.Errorf("%w: cursor inbox mismatch", ErrCorrupt)
	}
	return position, false, true, nil
}

func pulledInboxRow(ctx context.Context, tx *sql.Tx, historyID string, sequence int64) ([]byte, error) {
	var history, historyType, sequenceType, hashType, appliedType string
	var storedSequence, applied int64
	var hash []byte
	err := tx.QueryRowContext(ctx, `SELECT history_id,seq,change_hash,applied_at,typeof(history_id),typeof(seq),typeof(change_hash),typeof(applied_at) FROM sync_inbox WHERE history_id=? AND seq=?`, historyID, sequence).Scan(&history, &storedSequence, &hash, &applied, &historyType, &sequenceType, &hashType, &appliedType)
	if err != nil || historyType != "text" || sequenceType != "integer" || hashType != "blob" || appliedType != "integer" || history != historyID || !canonicalUUIDPattern.MatchString(history) || storedSequence != sequence || sequence <= 0 || len(hash) != 32 || !validStoredSyncTime(applied, time.Unix(0, applied)) {
		return nil, fmt.Errorf("%w: cursor inbox mismatch", ErrCorrupt)
	}
	return hash, nil
}

func pulledCursor(ctx context.Context, tx *sql.Tx, historyID string, sequence int64, hash []byte) (int64, bool, error) {
	var storedHistory string
	var position, updated int64
	err := tx.QueryRowContext(ctx, `SELECT history_id,position,updated_at FROM sync_cursor WHERE singleton=1`).Scan(&storedHistory, &position, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		if sequence != 1 {
			return 0, false, fmt.Errorf("%w: pulled history gap", ErrConflict)
		}
		return 0, false, pulledInboxEmpty(ctx, tx)
	}
	if err != nil || !canonicalUUIDPattern.MatchString(storedHistory) || position < 0 || !validStoredSyncTime(updated, time.Unix(0, updated)) || storedHistory != historyID {
		return 0, false, fmt.Errorf("%w: invalid pulled cursor", ErrCorrupt)
	}
	if err := pulledInboxConsistent(ctx, tx, historyID, position); err != nil {
		return 0, false, err
	}
	if sequence > position {
		return position, false, nil
	}
	var storedHash []byte
	err = tx.QueryRowContext(ctx, `SELECT change_hash FROM sync_inbox WHERE history_id=? AND seq=?`, historyID, sequence).Scan(&storedHash)
	if err != nil || len(storedHash) != 32 || !bytes.Equal(storedHash, hash) {
		return 0, false, fmt.Errorf("%w: pulled replay does not match inbox", ErrCorrupt)
	}
	return position, true, nil
}

func pulledInboxEmpty(ctx context.Context, tx *sql.Tx) error {
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_inbox`).Scan(&count); err != nil {
		return writeError(ctx, err)
	}
	if count != 0 {
		return fmt.Errorf("%w: inbox without cursor", ErrCorrupt)
	}
	return nil
}

func pulledEmptyCursor(ctx context.Context, tx *sql.Tx, historyID string, position int64) error {
	var storedHistory, historyType, positionType, updatedType string
	var storedPosition, updated int64
	err := tx.QueryRowContext(ctx, `SELECT history_id,position,updated_at,typeof(history_id),typeof(position),typeof(updated_at) FROM sync_cursor WHERE singleton=1`).Scan(&storedHistory, &storedPosition, &updated, &historyType, &positionType, &updatedType)
	if errors.Is(err, sql.ErrNoRows) {
		if position != 0 {
			return fmt.Errorf("%w: missing pulled cursor", ErrConflict)
		}
		return pulledInboxEmpty(ctx, tx)
	}
	if err != nil || historyType != "text" || positionType != "integer" || updatedType != "integer" || !canonicalUUIDPattern.MatchString(storedHistory) || storedHistory != historyID || storedPosition != position || storedPosition < 0 || !validStoredSyncTime(updated, time.Unix(0, updated)) {
		return fmt.Errorf("%w: invalid pulled cursor", ErrCorrupt)
	}
	return pulledInboxConsistent(ctx, tx, historyID, position)
}

func pulledInboxConsistent(ctx context.Context, tx *sql.Tx, historyID string, position int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT history_id,seq,change_hash,applied_at,typeof(history_id),typeof(seq),typeof(change_hash),typeof(applied_at) FROM sync_inbox ORDER BY seq`)
	if err != nil {
		return writeError(ctx, err)
	}
	defer rows.Close()
	expected := int64(1)
	for rows.Next() {
		var storedHistory, historyType, sequenceType, hashType, appliedType string
		var sequence, applied int64
		var hash []byte
		if err := rows.Scan(&storedHistory, &sequence, &hash, &applied, &historyType, &sequenceType, &hashType, &appliedType); err != nil {
			return writeError(ctx, err)
		}
		if historyType != "text" || sequenceType != "integer" || hashType != "blob" || appliedType != "integer" || storedHistory != historyID || !canonicalUUIDPattern.MatchString(storedHistory) || sequence != expected || len(hash) != 32 || !validStoredSyncTime(applied, time.Unix(0, applied)) {
			return fmt.Errorf("%w: cursor inbox mismatch", ErrCorrupt)
		}
		expected++
	}
	if err := rows.Err(); err != nil {
		return writeError(ctx, err)
	}
	if expected-1 != position {
		return fmt.Errorf("%w: cursor inbox mismatch", ErrCorrupt)
	}
	return nil
}

func (s *Store) applyPulledMutation(ctx context.Context, tx *sql.Tx, historyID string, change syncservice.Change) error {
	m := change.Mutation
	if change.ChangeDisposition == syncservice.ChangeDispositionConflict {
		return s.applyPulledConflict(ctx, tx, historyID, change)
	}
	if m.Kind == syncservice.MutationTombstone {
		return applyPulledTombstone(ctx, tx, historyID, change)
	}
	if m.Kind == syncservice.MutationResolve {
		return s.applyPulledResolve(ctx, tx, historyID, change)
	}
	switch m.RecordKind {
	case syncservice.RecordKindProject:
		return applyPulledIdentity(ctx, tx, `SELECT sync_version FROM projects WHERE id=?`, `INSERT INTO projects(id,sync_version) VALUES(?,?)`, `UPDATE projects SET sync_version=? WHERE id=?`, m.RecordID, change.CanonicalVersion, m.BaseVersion, m.Kind)
	case syncservice.RecordKindSession:
		if err := pulledProject(ctx, tx, m.Session.ProjectID); err != nil {
			return err
		}
		return applyPulledSession(ctx, tx, m.Session, change)
	case syncservice.RecordKindObservation:
		return applyPulledObservation(ctx, tx, m.Observation, change)
	}
	return fmt.Errorf("%w: unsupported pulled record", ErrInvalid)
}

func (s *Store) applyPulledConflict(ctx context.Context, tx *sql.Tx, historyID string, change syncservice.Change) error {
	m := change.Mutation
	payload, err := json.Marshal(m)
	if err != nil || len(payload) == 0 || len(payload) > maxSyncPayloadBytes {
		return fmt.Errorf("%w: invalid conflict snapshot", ErrInvalid)
	}
	var existing Observation
	found := false
	existing, found, err = loadOne(tx.QueryRowContext(ctx, observationSelect+` WHERE o.id=?`, m.RecordID))
	if err != nil {
		return err
	}
	if found && (existing.Project != m.Observation.ProjectID || existing.Scope != Scope(m.Observation.Scope)) {
		return fmt.Errorf("%w: conflict identity", ErrConflict)
	}
	if found {
		version, versionErr := syncVersion(ctx, tx, `SELECT sync_version FROM observations WHERE id=?`, m.RecordID)
		if versionErr != nil {
			return versionErr
		}
		if version != change.CanonicalVersion {
			return fmt.Errorf("%w: conflict version", ErrConflict)
		}
		if existing.State == StateActive {
			if _, err = tx.ExecContext(ctx, `UPDATE observations SET state='needs_review' WHERE id=?`, m.RecordID); err != nil {
				return writeError(ctx, err)
			}
		}
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO sync_conflicts(conflict_id,history_id,created_seq,record_kind,record_id,canonical_version,competing_version_id,status,resolved_seq,payload_version,snapshot,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'unresolved',NULL,1,?,?,?) ON CONFLICT(history_id,created_seq) DO NOTHING`, change.ConflictID, historyID, change.Sequence, m.RecordKind, m.RecordID, change.CanonicalVersion, strings.ToLower(m.MutationID), payload, now, now)
	if err != nil {
		return writeError(ctx, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return writeError(ctx, err)
	}
	if inserted == 0 {
		var conflictID, storedHistory, kind, recordID, competitor, status string
		var sequence, canonical, payloadVersion int64
		var snapshot []byte
		err = tx.QueryRowContext(ctx, `SELECT conflict_id,history_id,created_seq,record_kind,record_id,canonical_version,competing_version_id,status,payload_version,snapshot FROM sync_conflicts WHERE history_id=? AND created_seq=? AND resolved_seq IS NULL AND typeof(conflict_id)='text' AND typeof(history_id)='text' AND typeof(created_seq)='integer' AND typeof(record_kind)='text' AND typeof(record_id)='text' AND typeof(canonical_version)='integer' AND typeof(competing_version_id)='text' AND typeof(status)='text' AND typeof(payload_version)='integer' AND typeof(snapshot)='blob'`, historyID, change.Sequence).Scan(&conflictID, &storedHistory, &sequence, &kind, &recordID, &canonical, &competitor, &status, &payloadVersion, &snapshot)
		if err != nil || conflictID != change.ConflictID || storedHistory != historyID || sequence != change.Sequence || kind != string(m.RecordKind) || recordID != m.RecordID || canonical != change.CanonicalVersion || competitor != strings.ToLower(m.MutationID) || status != "unresolved" || payloadVersion != 1 || !bytes.Equal(snapshot, payload) {
			return fmt.Errorf("%w: conflict collision", ErrCorrupt)
		}
	}
	return nil
}

func applyPulledTombstone(ctx context.Context, tx *sql.Tx, historyID string, change syncservice.Change) error {
	m := change.Mutation
	item, found, err := loadOne(tx.QueryRowContext(ctx, observationSelect+` WHERE o.id=?`, m.RecordID))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: tombstone record missing", ErrConflict)
	}
	version, err := syncVersion(ctx, tx, `SELECT sync_version FROM observations WHERE id=?`, m.RecordID)
	if err != nil {
		return err
	}
	if version != m.BaseVersion || change.CanonicalVersion != version+1 {
		return fmt.Errorf("%w: tombstone version", ErrConflict)
	}
	deleted, ok := syncUnixNano(m.Tombstone.DeletedAt.UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: tombstone time", ErrInvalid)
	}
	provenance, err := json.Marshal(struct {
		ChangeHash  string    `json:"change_hash"`
		MutationID  string    `json:"mutation_id"`
		BaseVersion int64     `json:"base_version"`
		DeletedAt   time.Time `json:"deleted_at"`
	}{change.ChangeHash, m.MutationID, m.BaseVersion, m.Tombstone.DeletedAt.UTC().Round(0)})
	if err != nil || len(provenance) == 0 || len(provenance) > maxSyncPayloadBytes {
		return fmt.Errorf("%w: tombstone provenance", ErrInvalid)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_tombstones(history_id,seq,record_kind,record_id,canonical_version,payload_version,provenance,deleted_at) VALUES(?,?,?,?,?,1,?,?)`, historyID, change.Sequence, m.RecordKind, item.ID, change.CanonicalVersion, provenance, deleted); err != nil {
		return conflictOrWrite(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE observations SET topic_key=NULL WHERE id=?`, item.ID); err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM observations_fts WHERE id=?`, item.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM observation_refs WHERE observation_id=?`, item.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE observations SET sync_version=? WHERE id=?`, change.CanonicalVersion, item.ID)
	}
	return writeApplyError(ctx, err)
}

func (s *Store) applyPulledResolve(ctx context.Context, tx *sql.Tx, historyID string, change syncservice.Change) error {
	m, winner := change.Mutation, change.Mutation.Resolution.Observation
	seen := map[string]bool{}
	for _, id := range m.Resolution.ConflictIDs {
		if seen[id] {
			return fmt.Errorf("%w: duplicate conflict id", ErrInvalid)
		}
		seen[id] = true
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_conflicts WHERE conflict_id=? AND history_id=? AND record_kind='observation' AND record_id=? AND status='unresolved'`, id, historyID, m.RecordID).Scan(&n); err != nil {
			return writeError(ctx, err)
		}
		if n != 1 {
			return fmt.Errorf("%w: unresolved conflicts", ErrConflict)
		}
	}
	plain := change
	plain.Mutation = syncservice.Mutation{MutationID: m.MutationID, RecordID: m.RecordID, RecordKind: m.RecordKind, Kind: syncservice.MutationUpdate, BaseVersion: m.BaseVersion, Observation: winner}
	if err := applyPulledObservation(ctx, tx, winner, plain); err != nil {
		return err
	}
	args := make([]any, 0, len(m.Resolution.ConflictIDs)+1)
	args = append(args, change.Sequence)
	for _, id := range m.Resolution.ConflictIDs {
		args = append(args, id)
	}
	marks := strings.TrimRight(strings.Repeat("?,", len(m.Resolution.ConflictIDs)), ",")
	args = append(args, historyID, m.RecordID)
	result, err := tx.ExecContext(ctx, `UPDATE sync_conflicts SET status='resolved',resolved_seq=? WHERE status='unresolved' AND conflict_id IN (`+marks+`) AND history_id=? AND record_kind='observation' AND record_id=?`, args...)
	if err != nil {
		return writeError(ctx, err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != int64(len(m.Resolution.ConflictIDs)) {
		return fmt.Errorf("%w: resolve conflicts", ErrConflict)
	}
	return s.rebasePendingConflictOutbox(ctx, tx, m, change.CanonicalVersion)
}

func applyPulledIdentity(ctx context.Context, tx *sql.Tx, selectVersion, insert, update, id string, canonical, base int64, kind syncservice.MutationKind) error {
	var version int64
	err := tx.QueryRowContext(ctx, selectVersion, id).Scan(&version)
	if kind == syncservice.MutationCreate {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return writeError(ctx, err)
		}
		if !errors.Is(err, sql.ErrNoRows) || base != 0 || canonical != 1 {
			return fmt.Errorf("%w: pulled create version", ErrConflict)
		}
		if _, err = tx.ExecContext(ctx, insert, id, canonical); err != nil {
			return conflictOrWrite(ctx, err)
		}
		return nil
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: pulled record missing", ErrConflict)
		}
		return writeError(ctx, err)
	}
	if version < 0 {
		return fmt.Errorf("%w: pulled record version", ErrCorrupt)
	}
	if version != base || canonical != base+1 {
		return fmt.Errorf("%w: pulled version", ErrConflict)
	}
	if _, err = tx.ExecContext(ctx, update, canonical, id); err != nil {
		return writeError(ctx, err)
	}
	return nil
}

func pulledProject(ctx context.Context, tx *sql.Tx, id string) error {
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT sync_version FROM projects WHERE id=?`, id).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: session project missing", ErrConflict)
		}
		return writeError(ctx, err)
	}
	if version < 0 {
		return fmt.Errorf("%w: project version", ErrCorrupt)
	}
	return nil
}

func applyPulledSession(ctx context.Context, tx *sql.Tx, session *syncservice.Session, change syncservice.Change) error {
	var project string
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT project_id,sync_version FROM sessions WHERE id=?`, session.ID).Scan(&project, &version)
	if change.Mutation.Kind == syncservice.MutationCreate {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return writeError(ctx, err)
		}
		if !errors.Is(err, sql.ErrNoRows) || change.CanonicalVersion != 1 {
			return fmt.Errorf("%w: pulled session create", ErrConflict)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,project_id,sync_version) VALUES(?,?,?)`, session.ID, session.ProjectID, 1)
		if err != nil {
			return conflictOrWrite(ctx, err)
		}
		return nil
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: pulled session missing", ErrConflict)
		}
		return writeError(ctx, err)
	}
	if version < 0 {
		return fmt.Errorf("%w: pulled session version", ErrCorrupt)
	}
	if project != session.ProjectID || version != change.Mutation.BaseVersion || change.CanonicalVersion != version+1 {
		return fmt.Errorf("%w: pulled session version", ErrConflict)
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET sync_version=? WHERE id=?`, change.CanonicalVersion, session.ID)
	return writeApplyError(ctx, err)
}

func applyPulledObservation(ctx context.Context, tx *sql.Tx, observation *syncservice.Observation, change syncservice.Change) error {
	item, err := pulledObservation(observation)
	if err != nil {
		return err
	}
	if err = pulledProject(ctx, tx, item.Project); err != nil {
		return err
	}
	if item.Session != "" {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=? AND project_id=?`, item.Session, item.Project).Scan(&n); err != nil {
			return writeError(ctx, err)
		}
		if n != 1 {
			return fmt.Errorf("%w: observation session missing", ErrConflict)
		}
	}
	if err = rejectTombstoned(ctx, tx, item.ID); err != nil {
		return err
	}
	if err = validateReferences(ctx, tx, item); err != nil {
		return err
	}
	var existing Observation
	var found bool
	existing, found, err = loadOne(tx.QueryRowContext(ctx, observationSelect+` WHERE o.id=?`, item.ID))
	if err != nil {
		return err
	}
	if change.Mutation.Kind == syncservice.MutationCreate {
		if found || change.Mutation.BaseVersion != 0 || change.CanonicalVersion != 1 {
			return fmt.Errorf("%w: pulled observation create", ErrConflict)
		}
		if err = insertObservation(ctx, tx, item); err != nil {
			return err
		}
		if item.State == StateArchived {
			if _, err = tx.ExecContext(ctx, `DELETE FROM observations_fts WHERE id=?`, item.ID); err != nil {
				return writeError(ctx, err)
			}
		}
	} else {
		if !found || existing.Project != item.Project || existing.Scope != item.Scope || !existing.CreatedAt.Equal(item.CreatedAt) {
			return fmt.Errorf("%w: pulled observation identity", ErrConflict)
		}
		existingVersion, versionErr := syncVersion(ctx, tx, `SELECT sync_version FROM observations WHERE id=?`, item.ID)
		if versionErr != nil {
			return versionErr
		}
		if existingVersion != change.Mutation.BaseVersion || change.CanonicalVersion != existingVersion+1 {
			return fmt.Errorf("%w: pulled observation version", ErrConflict)
		}
		_, err = tx.ExecContext(ctx, `UPDATE observations SET title=?,session_id=?,type=?,content=?,topic_key=?,producer=?,source_provider=?,source_id=?,state=?,updated_at=?,review_after=? WHERE id=?`, item.Title, nullable(item.Session), item.Type, item.Content, nullable(item.TopicKey), item.Provenance.Producer, item.Provenance.SourceProvider, item.Provenance.SourceID, item.State, item.UpdatedAt.UnixNano(), nullableTime(item.ReviewAfter), item.ID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM observation_refs WHERE observation_id=?`, item.ID)
		}
		if err == nil {
			err = insertReferences(ctx, tx, item)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM observations_fts WHERE id=?`, item.ID)
		}
		if err == nil && item.State != StateArchived {
			_, err = tx.ExecContext(ctx, `INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)`, item.ID, item.Title, item.TopicKey, item.Type, item.Content)
		}
		if err != nil {
			return conflictOrWrite(ctx, err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE observations SET sync_version=? WHERE id=?`, change.CanonicalVersion, item.ID)
	return writeApplyError(ctx, err)
}

func pulledObservation(value *syncservice.Observation) (Observation, error) {
	created, createdOK := syncUnixNano(value.CreatedAt.UTC().Round(0))
	updated, updatedOK := syncUnixNano(value.UpdatedAt.UTC().Round(0))
	if !createdOK || !updatedOK || updated < created {
		return Observation{}, fmt.Errorf("%w: invalid pulled timestamp", ErrInvalid)
	}
	var reviewAfter *time.Time
	if value.ReviewAfter != nil {
		nanos, ok := syncUnixNano(value.ReviewAfter.UTC().Round(0))
		if !ok {
			return Observation{}, fmt.Errorf("%w: invalid pulled review time", ErrInvalid)
		}
		normalized := time.Unix(0, nanos).UTC()
		reviewAfter = &normalized
	}
	state := StateActive
	if value.Lifecycle == syncservice.LifecycleArchived {
		state = StateArchived
	} else if value.Review == syncservice.ReviewNeedsReview {
		state = StateNeedsReview
	}
	return Observation{ID: value.ID, Title: value.Title, Project: value.ProjectID, Session: value.SessionID, Scope: Scope(value.Scope), Type: value.Type, Content: value.Content, TopicKey: value.TopicKey, References: append([]string(nil), value.References...), Provenance: Provenance{Producer: value.Provenance.Producer, SourceProvider: value.Provenance.SourceProvider, SourceID: value.Provenance.SourceID}, State: state, CreatedAt: time.Unix(0, created).UTC(), UpdatedAt: time.Unix(0, updated).UTC(), ReviewAfter: reviewAfter}, nil
}

func writeApplyError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return writeError(ctx, err)
}

func syncObservation(item Observation) syncservice.Observation {
	review := syncservice.ReviewClear
	if item.State == StateNeedsReview {
		review = syncservice.ReviewNeedsReview
	}
	lifecycle := syncservice.LifecycleActive
	if item.State == StateArchived {
		lifecycle = syncservice.LifecycleArchived
	}
	return syncservice.Observation{ID: item.ID, Title: item.Title, ProjectID: item.Project, SessionID: item.Session, Scope: string(item.Scope), Type: item.Type, Content: item.Content, TopicKey: item.TopicKey, References: append([]string(nil), item.References...), Provenance: syncservice.Provenance{Producer: item.Provenance.Producer, SourceProvider: item.Provenance.SourceProvider, SourceID: item.Provenance.SourceID}, Lifecycle: lifecycle, Review: review, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, ReviewAfter: item.ReviewAfter}
}

func decodeSyncOutboxEntry(id, recordKind, recordID, kind string, baseVersion, payloadVersion int64, payload []byte, state string, attempts int64, lastErrorCode string, nextAttempt, created, updated int64) (SyncOutboxEntry, error) {
	var mutation syncservice.Mutation
	pending := state == string(SyncOutboxPending) && attempts == 0 && lastErrorCode == ""
	retry := state == string(SyncOutboxRetry) && attempts > 0 && syncErrorCodePattern.MatchString(lastErrorCode)
	if payloadVersion != 1 || len(payload) > maxSyncPayloadBytes || json.Unmarshal(payload, &mutation) != nil || !canonicalUUIDPattern.MatchString(id) || mutation.MutationID != id || string(mutation.RecordKind) != recordKind || mutation.RecordID != recordID || string(mutation.Kind) != kind || mutation.BaseVersion != baseVersion || !allowedSyncMutation(mutation) || syncservice.ValidateMutation(mutation) != nil || !pending && !retry {
		return SyncOutboxEntry{}, fmt.Errorf("%w: invalid sync outbox entry", ErrCorrupt)
	}
	entry := SyncOutboxEntry{Mutation: mutation, State: SyncOutboxState(state), Attempts: attempts, LastErrorCode: lastErrorCode, NextAttemptAt: time.Unix(0, nextAttempt).UTC(), CreatedAt: time.Unix(0, created).UTC(), UpdatedAt: time.Unix(0, updated).UTC()}
	if !validStoredSyncTime(nextAttempt, entry.NextAttemptAt) || !validStoredSyncTime(created, entry.CreatedAt) || !validStoredSyncTime(updated, entry.UpdatedAt) || entry.UpdatedAt.Before(entry.CreatedAt) {
		return SyncOutboxEntry{}, fmt.Errorf("%w: invalid sync outbox timestamp", ErrCorrupt)
	}
	return entry, nil
}

func validateSyncProfile(profile SyncProfile) (SyncProfile, error) {
	endpoint, err := url.Parse(profile.Endpoint)
	if err != nil || len(profile.Endpoint) > 2048 || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return SyncProfile{}, fmt.Errorf("%w: invalid sync endpoint", ErrInvalid)
	}
	endpoint.Scheme = "https"
	endpoint.Host = strings.ToLower(endpoint.Host)
	endpoint.RawPath = ""
	if strings.HasSuffix(endpoint.Host, ":443") {
		endpoint.Host = strings.TrimSuffix(endpoint.Host, ":443")
	}
	profile.Endpoint = endpoint.String()
	profile.DeviceID = strings.ToLower(profile.DeviceID)
	credentialRef, err := normalizeCredentialReference(profile.CredentialRef)
	if !canonicalUUIDPattern.MatchString(profile.DeviceID) || err != nil {
		return SyncProfile{}, fmt.Errorf("%w: invalid sync profile", ErrInvalid)
	}
	profile.CredentialRef = credentialRef
	if profile.PreviousCredentialRef != "" {
		previous, err := normalizeCredentialReference(profile.PreviousCredentialRef)
		if err != nil || previous == profile.CredentialRef {
			return SyncProfile{}, fmt.Errorf("%w: invalid sync profile", ErrInvalid)
		}
		profile.PreviousCredentialRef = previous
	}
	return profile, nil
}

func nullSyncCredentialRef(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// ValidateSyncProfile normalizes a profile using the same checks enforced by
// durable profile storage. It never persists the profile or reads credentials.
func ValidateSyncProfile(profile SyncProfile) (SyncProfile, error) {
	return validateSyncProfile(profile)
}

func normalizeCredentialReference(value string) (string, error) {
	if len(value) > 512 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t") || strings.Contains(value, "%") || strings.Contains(strings.ToLower(value), "bearer") || bearerReference.MatchString(value) {
		return "", ErrInvalid
	}
	for _, character := range value {
		if character < '!' || character > '~' {
			return "", ErrInvalid
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "secret" || parsed.Host == "" || parsed.Host != strings.ToLower(parsed.Host) || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.RawPath != "" || !credentialProvider.MatchString(parsed.Host) {
		return "", ErrInvalid
	}
	segments := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if !strings.HasPrefix(parsed.Path, "/") || len(segments) > 8 {
		return "", ErrInvalid
	}
	for _, segment := range segments {
		if !credentialSegment.MatchString(segment) {
			return "", ErrInvalid
		}
	}
	return "secret://" + parsed.Host + "/" + strings.Join(segments, "/"), nil
}

func allowedSyncMutation(mutation syncservice.Mutation) bool {
	return mutation.RecordKind == syncservice.RecordKindProject || mutation.RecordKind == syncservice.RecordKindSession || mutation.RecordKind == syncservice.RecordKindObservation
}

func syncUnixNano(value time.Time) (int64, bool) {
	if value.IsZero() {
		return 0, false
	}
	nanos := value.UnixNano()
	return nanos, nanos > 0 && time.Unix(0, nanos).Equal(value)
}

func validStoredSyncTime(nanos int64, value time.Time) bool {
	return nanos > 0 && time.Unix(0, nanos).Equal(value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func newSyncUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	hexValue := hex.EncodeToString(value[:])
	return hexValue[0:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32], nil
}
