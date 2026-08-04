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
	var enabled int
	err := tx.QueryRowContext(ctx, `SELECT enabled FROM sync_profiles WHERE singleton=1`).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return writeError(ctx, err)
	}
	if enabled == 0 {
		return nil
	}
	if enabled != 1 {
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
