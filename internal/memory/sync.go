package memory

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	maxSyncPayloadBytes = 1 << 20
	maxDueSyncOutbox    = 16
)

var (
	canonicalUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	syncErrorCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	credentialProvider   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)
	credentialSegment    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	bearerReference      = regexp.MustCompile(`(?i)vgx1\.[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[a-z0-9_-]+`)
)

type SyncProfile struct {
	Enabled       bool
	Endpoint      string
	DeviceID      string
	CredentialRef string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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

// BootstrapCheckpoint is the durable, caller-owned progress marker for a pull.
type BootstrapCheckpoint struct {
	HistoryID string `json:"history_id"`
	Position  int64  `json:"position"`
	Watermark int64  `json:"watermark"`
	Phase     string `json:"-"`
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
	err = s.db.QueryRowContext(ctx, `INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,?,?,?,?,?,?) ON CONFLICT(singleton) DO UPDATE SET enabled=excluded.enabled,endpoint=excluded.endpoint,device_id=excluded.device_id,credential_ref=excluded.credential_ref,updated_at=excluded.updated_at RETURNING enabled,endpoint,device_id,credential_ref,created_at,updated_at`, boolInt(profile.Enabled), profile.Endpoint, profile.DeviceID, profile.CredentialRef, now, now).Scan(&enabled, &profile.Endpoint, &profile.DeviceID, &profile.CredentialRef, &created, &updated)
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
	var profile SyncProfile
	var enabled int
	var created, updated int64
	err := s.db.QueryRowContext(ctx, `SELECT enabled,endpoint,device_id,credential_ref,created_at,updated_at FROM sync_profiles WHERE singleton=1`).Scan(&enabled, &profile.Endpoint, &profile.DeviceID, &profile.CredentialRef, &created, &updated)
	if err == sql.ErrNoRows {
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

// DueSyncOutbox returns at most sixteen due entries in durable insertion order.
func (s *Store) DueSyncOutbox(ctx context.Context, due time.Time) ([]SyncOutboxEntry, error) {
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
		CASE WHEN mutation_kind IN ('create','update','archive','tombstone','resolve') THEN mutation_kind ELSE NULL END,
		base_version,payload_version,CASE WHEN length(CAST(payload AS BLOB)) BETWEEN 1 AND 1048576 THEN payload ELSE NULL END,
		CASE WHEN state IN ('pending','retry') THEN state ELSE NULL END,
		attempts,CASE WHEN typeof(last_error_code)='text' AND length(CAST(last_error_code AS BLOB))<=64 THEN last_error_code ELSE NULL END,next_attempt_at,created_at,updated_at
		FROM sync_outbox WHERE next_attempt_at<=? ORDER BY created_at,id LIMIT 16`, dueNanos)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	defer rows.Close()
	entries := make([]SyncOutboxEntry, 0, maxDueSyncOutbox)
	for rows.Next() {
		var id, recordKind, recordID, kind, state, lastErrorCode sql.NullString
		var baseVersion, payloadVersion, attempts, nextAttempt, created, updated int64
		var payload []byte
		if err := rows.Scan(&id, &recordKind, &recordID, &kind, &baseVersion, &payloadVersion, &payload, &state, &attempts, &lastErrorCode, &nextAttempt, &created, &updated); err != nil {
			return nil, writeError(ctx, err)
		}
		if !id.Valid || !recordKind.Valid || !recordID.Valid || !kind.Valid || payload == nil || !state.Valid || !lastErrorCode.Valid {
			return nil, fmt.Errorf("%w: invalid sync outbox entry", ErrCorrupt)
		}
		entry, err := decodeSyncOutboxEntry(id.String, recordKind.String, recordID.String, kind.String, baseVersion, payloadVersion, payload, state.String, attempts, lastErrorCode.String, nextAttempt, created, updated)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, writeError(ctx, err)
	}
	return entries, nil
}

// MarkSyncOutboxRetry makes one entry eligible for a later retry.
func (s *Store) MarkSyncOutboxRetry(ctx context.Context, mutationID string, next time.Time, code string) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	nextNanos, ok := syncUnixNano(next.UTC().Round(0))
	if !ok || !canonicalUUIDPattern.MatchString(mutationID) || !syncErrorCodePattern.MatchString(code) {
		return fmt.Errorf("%w: invalid sync retry", ErrInvalid)
	}
	now, ok := syncUnixNano(s.now().UTC().Round(0))
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sync_outbox SET state='retry',attempts=attempts+1,next_attempt_at=?,last_error_code=?,updated_at=? WHERE mutation_id=?`, nextNanos, code, now, mutationID)
	if err != nil {
		return writeError(ctx, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return writeError(ctx, err)
	}
	if updated != 1 {
		return fmt.Errorf("%w: sync outbox entry", ErrNotFound)
	}
	return nil
}

// enqueueSyncOutbox is intentionally transaction-bound for a future local-write integration.
func (s *Store) enqueueSyncOutbox(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation) (syncservice.Mutation, error) {
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

func (s *Store) insertSyncOutbox(ctx context.Context, tx *sql.Tx, mutation syncservice.Mutation, payload []byte) error {
	now := s.now().UTC().Round(0)
	nanos, ok := syncUnixNano(now)
	if !ok {
		return fmt.Errorf("%w: invalid clock", ErrCorrupt)
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

// ApplyPulledPage validates a page before opening SQLite, then commits its
// materialization, inbox entries, cursor, and optional checkpoint together.
func (s *Store) ApplyPulledPage(ctx context.Context, page syncservice.PullPage, checkpoint *BootstrapCheckpoint) error {
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
	epoch, err := syncDataVersion(ctx, tx)
	if err != nil {
		s.syncInbox.known = false
		return err
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
		if err = verifyBootstrapCheckpoint(ctx, tx, *checkpoint, page.Cursor.HistoryID, position); err != nil {
			s.syncInbox.known = false
			return err
		}
	}
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
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_outbox WHERE record_kind=? AND record_id=? AND state IN ('pending','retry')`, change.Mutation.RecordKind, change.Mutation.RecordID).Scan(&pending); err != nil {
			s.syncInbox.known = false
			return writeError(ctx, err)
		}
		if pending != 0 {
			return fmt.Errorf("%w: local sync work is pending", ErrConflict)
		}
		if err = s.applyPulledMutation(ctx, tx, page.Cursor.HistoryID, change); err != nil {
			if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
				s.syncInbox.known = false
			}
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_inbox(history_id,seq,change_hash,applied_at) VALUES(?,?,?,?)`, page.Cursor.HistoryID, change.Sequence, hash, now); err != nil {
			s.syncInbox.known = false
			return writeError(ctx, err)
		}
		position = change.Sequence
	}
	if len(page.Changes) != 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_cursor(singleton,history_id,position,updated_at) VALUES(1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET history_id=excluded.history_id,position=excluded.position,updated_at=excluded.updated_at`, page.Cursor.HistoryID, position, now); err != nil {
			return writeError(ctx, err)
		}
	}
	if checkpoint != nil {
		payload, marshalErr := json.Marshal(checkpoint)
		if marshalErr != nil || len(payload) == 0 || len(payload) > maxSyncPayloadBytes {
			return fmt.Errorf("%w: invalid bootstrap checkpoint", ErrInvalid)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_bootstrap(singleton,phase,payload_version,checkpoint,created_at,updated_at) VALUES(1,?,1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET phase=excluded.phase,checkpoint=excluded.checkpoint,updated_at=excluded.updated_at`, checkpoint.Phase, payload, now, now); err != nil {
			return writeError(ctx, err)
		}
	}
	if err = commit(ctx, tx); err != nil {
		return err
	}
	s.syncInbox = syncInboxCache{known: true, dataVersion: epoch, historyID: page.Cursor.HistoryID, position: position}
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
	if checkpoint != nil && (!canonicalUUIDPattern.MatchString(checkpoint.HistoryID) || checkpoint.HistoryID != page.Cursor.HistoryID || checkpoint.Position != page.Cursor.Position || checkpoint.Watermark != page.Cursor.Watermark || checkpointPhaseRank(checkpoint.Phase) == 0) {
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

func verifyBootstrapCheckpoint(ctx context.Context, tx *sql.Tx, next BootstrapCheckpoint, historyID string, position int64) error {
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
	if decoder.Decode(&prior) != nil || decoder.Decode(&extra) != io.EOF || prior.Phase != "" || prior.HistoryID != historyID || prior.Position != position || prior.Watermark != next.Watermark || (phase == "complete" && (next.Phase != "complete" || next.Position != position)) || (phase != "complete" && checkpointPhaseRank(next.Phase) < checkpointPhaseRank(phase)) {
		return fmt.Errorf("%w: bootstrap checkpoint mismatch", ErrConflict)
	}
	return nil
}

func ordinaryPulledMutation(m syncservice.Mutation) bool {
	if m.RecordKind == syncservice.RecordKindProject || m.RecordKind == syncservice.RecordKindSession {
		return m.Kind == syncservice.MutationCreate || m.Kind == syncservice.MutationUpdate
	}
	if m.RecordKind != syncservice.RecordKindObservation || m.Observation == nil || m.Observation.Review != syncservice.ReviewClear {
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
	return nil
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
		if err == nil && item.State == StateActive {
			_, err = tx.ExecContext(ctx, `INSERT INTO observations_fts(id,content) VALUES(?,?)`, item.ID, item.Content)
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
	return profile, nil
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
