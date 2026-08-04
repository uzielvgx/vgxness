package syncpg

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/vgxness/vgxness/internal/syncservice"
)

var (
	// ErrRepository indicates a repository operation failed without exposing a
	// database error to callers.
	ErrRepository = errors.New("syncpg repository")
	// ErrInvalidRepository indicates invalid repository construction arguments.
	ErrInvalidRepository = errors.New("syncpg invalid repository")
	// ErrOwnerConflict indicates that the database belongs to another owner or
	// violates the single-owner invariant.
	ErrOwnerConflict = errors.New("syncpg owner conflict")
	// ErrOwnerNotInitialized indicates that no owner has been established.
	ErrOwnerNotInitialized = errors.New("syncpg owner not initialized")
)

// DB begins transactions but remains owned by its caller. Both *pgx.Conn and
// *pgxpool.Pool satisfy this interface.
type DB interface {
	Begin(context.Context) (pgx.Tx, error)
}

// OwnerSyncState is the owner-scoped synchronization sequence state.
type OwnerSyncState struct {
	OwnerID   uuid.UUID
	HistoryID uuid.UUID
	NextSeq   int64
}

// Repository owns no database resource and is bound to one owner.
type Repository struct {
	db      DB
	ownerID uuid.UUID
}

// NewRepository binds db to a non-zero owner without taking ownership of db.
func NewRepository(db DB, ownerID uuid.UUID) (*Repository, error) {
	if nilDB(db) || ownerID == uuid.Nil {
		return nil, ErrInvalidRepository
	}
	return &Repository{db: db, ownerID: ownerID}, nil
}

func nilDB(db DB) bool {
	if db == nil {
		return true
	}
	value := reflect.ValueOf(db)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// EnsureOwner atomically creates this repository's sole database owner and
// synchronization state, or rejects an existing incompatible owner.
func (r *Repository) EnsureOwner(ctx context.Context) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return repositoryError(ctx)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(835301948)); err != nil {
		return repositoryError(ctx)
	}
	schema, err := recoverySchema(ctx, tx)
	if err != nil || !repositoryMigrationsValid(ctx, tx, schema) {
		return repositoryError(ctx)
	}
	owners, err := r.owners(ctx, tx, schema)
	if err != nil {
		return repositoryError(ctx)
	}
	if len(owners) != 0 {
		if len(owners) != 1 || owners[0] != r.ownerID {
			return ErrOwnerConflict
		}
		if _, err := r.ownerState(ctx, tx, schema); err != nil {
			return repositoryError(ctx)
		}
		return commitRepository(ctx, tx)
	}
	historyID, err := uuid.NewRandom()
	if err != nil || historyID == uuid.Nil {
		return repositoryError(ctx)
	}
	table := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	if _, err := tx.Exec(ctx, "INSERT INTO "+table("owners")+" (id) VALUES ($1)", r.ownerID); err != nil {
		return repositoryError(ctx)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO "+table("owner_sync_state")+" (owner_id, history_id, next_seq) VALUES ($1, $2, 1)", r.ownerID, historyID); err != nil {
		return repositoryError(ctx)
	}
	return commitRepository(ctx, tx)
}

// OwnerState reads this repository's state without changing it.
func (r *Repository) OwnerState(ctx context.Context) (OwnerSyncState, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return OwnerSyncState{}, repositoryError(ctx)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return OwnerSyncState{}, repositoryError(ctx)
	}
	schema, err := recoverySchema(ctx, tx)
	if err != nil || !repositoryMigrationsValid(ctx, tx, schema) {
		return OwnerSyncState{}, repositoryError(ctx)
	}
	owners, err := r.owners(ctx, tx, schema)
	if err != nil {
		return OwnerSyncState{}, repositoryError(ctx)
	}
	if len(owners) == 0 {
		return OwnerSyncState{}, ErrOwnerNotInitialized
	}
	if len(owners) != 1 || owners[0] != r.ownerID {
		return OwnerSyncState{}, ErrOwnerConflict
	}
	state, err := r.ownerState(ctx, tx, schema)
	if err != nil {
		return OwnerSyncState{}, repositoryError(ctx)
	}
	if err := commitRepository(ctx, tx); err != nil {
		return OwnerSyncState{}, err
	}
	return state, nil
}

// Push applies each mutation in its own transaction.
func (r *Repository) Push(ctx context.Context, deviceID uuid.UUID, mutations []syncservice.Mutation) ([]syncservice.Result, error) {
	results := make([]syncservice.Result, 0, len(mutations))
	for _, mutation := range mutations {
		result, err := r.pushOne(ctx, deviceID, mutation)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Repository) pushOne(ctx context.Context, deviceID uuid.UUID, mutation syncservice.Mutation) (syncservice.Result, error) {
	tx, schema, err := r.pushTransaction(ctx)
	if err != nil {
		return syncservice.Result{}, err
	}
	defer tx.Rollback(context.Background())
	table := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	var next int64
	if err := tx.QueryRow(ctx, "SELECT next_seq FROM "+table("owner_sync_state")+" WHERE owner_id=$1 FOR UPDATE", r.ownerID).Scan(&next); err != nil || next < 1 {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if state, err := r.ownerState(ctx, tx, schema); err != nil || state.NextSeq != next {
		return syncservice.Result{}, repositoryError(ctx)
	}
	reject := func(code string) (syncservice.Result, error) {
		if err := commitRepository(ctx, tx); err != nil {
			return syncservice.Result{}, err
		}
		return syncservice.Result{MutationID: mutation.MutationID, Disposition: syncservice.DispositionRejected, Code: code}, nil
	}
	if deviceID == uuid.Nil {
		return reject("invalid_device")
	}
	var revoked bool
	if err := tx.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM "+table("devices")+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", r.ownerID, deviceID).Scan(&revoked); errors.Is(err, pgx.ErrNoRows) {
		return reject("invalid_device")
	} else if err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if revoked {
		return reject("revoked")
	}
	if err := syncservice.ValidateMutation(mutation); err != nil || !acceptedMutation(mutation) {
		return reject("unsupported_semantic")
	}
	hash, err := canonicalMutationHash(mutation)
	if err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	mutationID, err := uuid.Parse(mutation.MutationID)
	if err != nil {
		return reject("invalid_input")
	}
	var storedHash []byte
	var sequence, version int64
	var disposition, code string
	err = tx.QueryRow(ctx, "SELECT request_hash, disposition, canonical_seq, canonical_version, COALESCE(error_code,'') FROM "+table("mutations")+" WHERE owner_id=$1 AND device_id=$2 AND mutation_id=$3", r.ownerID, deviceID, mutationID).Scan(&storedHash, &disposition, &sequence, &version, &code)
	if err == nil {
		if len(storedHash) != sha256.Size || string(storedHash) != string(hash[:]) {
			return reject("mutation_id_hash_mismatch")
		}
		if (disposition != string(syncservice.DispositionAccepted) && disposition != string(syncservice.DispositionConflict)) || sequence < 1 {
			return reject("invalid_replay")
		}
		if err := commitRepository(ctx, tx); err != nil {
			return syncservice.Result{}, err
		}
		resultDisposition := syncservice.DispositionPreviouslyAccepted
		if disposition == string(syncservice.DispositionConflict) {
			resultDisposition = syncservice.DispositionConflict
		}
		return syncservice.Result{MutationID: mutation.MutationID, Disposition: resultDisposition, Sequence: &sequence, Version: version, Code: code}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return syncservice.Result{}, repositoryError(ctx)
	}
	current, exists, err := r.lockTarget(ctx, tx, table, mutation)
	if err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if mutation.Kind == syncservice.MutationCreate && (mutation.BaseVersion != 0 || exists) {
		return reject("invalid_base")
	}
	if mutation.Kind != syncservice.MutationCreate && (!exists || mutation.BaseVersion == math.MaxInt64) {
		return reject("invalid_base")
	}
	if exists && mutation.RecordKind == syncservice.RecordKindObservation && mutation.Kind != syncservice.MutationTombstone && current == mutation.BaseVersion && r.tombstonedObservation(ctx, tx, table, mutation.RecordID) {
		return reject("invalid_base")
	}
	payloadMutation := mutation
	if mutation.Kind == syncservice.MutationResolve {
		conflictIDs, ok := normalizeConflictIDs(mutation.Resolution.ConflictIDs)
		if !ok {
			return reject("invalid_prerequisite")
		}
		resolution := *mutation.Resolution
		resolution.ConflictIDs = conflictIDs
		payloadMutation.Resolution = &resolution
		payloadMutation.Observation = mutation.Resolution.Observation
	}
	if err := r.validatePrerequisites(ctx, tx, table, payloadMutation); err != nil {
		return reject("invalid_prerequisite")
	}
	topicCollision := false
	if err := r.lockTopic(ctx, tx, table, payloadMutation); err != nil {
		if !errors.Is(err, errTopicCollision) {
			return syncservice.Result{}, repositoryError(ctx)
		}
		topicCollision = true
	}
	conflict := mutation.RecordKind == syncservice.RecordKindObservation &&
		((mutation.Kind == syncservice.MutationUpdate && current != mutation.BaseVersion) || topicCollision)
	if conflict {
		return r.writeConflict(ctx, tx, table, deviceID, mutationID, hash, payloadMutation, current, exists, next)
	}
	if mutation.Kind != syncservice.MutationCreate && current != mutation.BaseVersion {
		return reject("stale_base")
	}
	if mutation.Kind == syncservice.MutationResolve {
		if !r.activeObservation(ctx, tx, table, mutation.RecordID) || !r.unresolvedConflicts(ctx, tx, table, mutation.RecordID, payloadMutation.Resolution.ConflictIDs) {
			return reject("invalid_prerequisite")
		}
	}
	if mutation.Kind == syncservice.MutationTombstone && mutation.RecordKind != syncservice.RecordKindObservation {
		return syncservice.Result{}, repositoryError(ctx)
	}
	version = mutation.BaseVersion + 1
	var state []byte
	if state, err = canonicalPayload(payloadMutation); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+table("mutations")+" (owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ($1,$2,$3,$4,$5,$6,$7,'accepted',$8,$9)", r.ownerID, deviceID, mutationID, hash[:], mutation.Kind, mutation.RecordID, mutation.BaseVersion, next, version); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	var versionID int64
	if err = tx.QueryRow(ctx, "INSERT INTO "+table("record_versions")+" (owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,$2,$3,$4,$5,$6,$7,'accepted',$8) RETURNING id", r.ownerID, mutation.RecordKind, mutation.RecordID, version, deviceID, mutationID, mutation.BaseVersion, state).Scan(&versionID); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if mutation.Kind == syncservice.MutationTombstone {
		err = r.writeTombstone(ctx, tx, table, mutation.RecordID, version)
	} else {
		err = r.writeCanonical(ctx, tx, table, payloadMutation, version, state)
	}
	if err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+table("changes")+" (owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,canonical_version,version_id) VALUES ($1,$2,$3,$4,'accepted',$5,$6,$7,$8)", r.ownerID, next, deviceID, mutationID, mutation.RecordKind, mutation.RecordID, version, versionID); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if mutation.Kind == syncservice.MutationTombstone {
		if _, err = tx.Exec(ctx, "INSERT INTO "+table("tombstones")+" (owner_id,record_kind,record_id,version_id,mutation_device_id,mutation_id,deleted_at) VALUES ($1,'observation',$2,$3,$4,$5,$6)", r.ownerID, mutation.RecordID, versionID, deviceID, mutationID, mutation.Tombstone.DeletedAt); err != nil {
			return syncservice.Result{}, repositoryError(ctx)
		}
	}
	if mutation.Kind == syncservice.MutationResolve {
		if _, err = tx.Exec(ctx, "UPDATE "+table("observation_conflicts")+" SET status='resolved',resolved_seq=$3 WHERE owner_id=$1 AND observation_id=$2 AND status='unresolved' AND conflict_id::text=ANY($4)", r.ownerID, mutation.RecordID, next, payloadMutation.Resolution.ConflictIDs); err != nil {
			return syncservice.Result{}, repositoryError(ctx)
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE "+table("owner_sync_state")+" SET next_seq=$2 WHERE owner_id=$1 AND next_seq=$3", r.ownerID, next+1, next); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if err = commitRepository(ctx, tx); err != nil {
		return syncservice.Result{}, err
	}
	return syncservice.Result{MutationID: mutation.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &next, Version: version}, nil
}

func (r *Repository) pushTransaction(ctx context.Context) (pgx.Tx, string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, "", repositoryError(ctx)
	}
	schema, err := recoverySchema(ctx, tx)
	if err != nil || !repositoryMigrationsValid(ctx, tx, schema) {
		tx.Rollback(context.Background())
		return nil, "", repositoryError(ctx)
	}
	owners, err := r.owners(ctx, tx, schema)
	if err != nil {
		tx.Rollback(context.Background())
		return nil, "", repositoryError(ctx)
	}
	if len(owners) == 0 {
		tx.Rollback(context.Background())
		return nil, "", ErrOwnerNotInitialized
	}
	if len(owners) != 1 || owners[0] != r.ownerID {
		tx.Rollback(context.Background())
		return nil, "", ErrOwnerConflict
	}
	return tx, schema, nil
}

func acceptedMutation(m syncservice.Mutation) bool {
	if m.RecordKind != syncservice.RecordKindObservation {
		return m.Kind == syncservice.MutationCreate || m.Kind == syncservice.MutationUpdate
	}
	switch m.Kind {
	case syncservice.MutationCreate, syncservice.MutationUpdate:
		return m.Observation.Lifecycle == syncservice.LifecycleActive && m.Observation.Review == syncservice.ReviewClear
	case syncservice.MutationArchive:
		return m.Observation.Lifecycle == syncservice.LifecycleArchived && m.Observation.Review == syncservice.ReviewClear
	case syncservice.MutationTombstone:
		return true
	case syncservice.MutationResolve:
		return m.Resolution.Observation.Lifecycle == syncservice.LifecycleActive && m.Resolution.Observation.Review == syncservice.ReviewClear
	default:
		return false
	}
}

func canonicalMutationHash(m syncservice.Mutation) ([sha256.Size]byte, error) {
	b, err := json.Marshal(m)
	return sha256.Sum256(b), err
}
func canonicalPayload(m syncservice.Mutation) ([]byte, error) {
	switch m.RecordKind {
	case syncservice.RecordKindProject:
		return json.Marshal(m.Project)
	case syncservice.RecordKindSession:
		return json.Marshal(m.Session)
	default:
		if m.Tombstone != nil {
			return json.Marshal(m.Tombstone)
		}
		return json.Marshal(m.Observation)
	}
}

func (r *Repository) writeConflict(ctx context.Context, tx pgx.Tx, table func(string) string, device, mutationID uuid.UUID, hash [sha256.Size]byte, m syncservice.Mutation, current int64, exists bool, seq int64) (syncservice.Result, error) {
	version := m.BaseVersion + 1
	canonical := current
	if !exists {
		canonical = version
	}
	if _, err := tx.Exec(ctx, "INSERT INTO "+table("mutations")+" (owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ($1,$2,$3,$4,$5,$6,$7,'conflict',$8,$9)", r.ownerID, device, mutationID, hash[:], m.Kind, m.RecordID, m.BaseVersion, seq, canonical); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	state, err := canonicalPayload(m)
	if err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if !exists {
		o := m.Observation
		provenance, err := json.Marshal(o.Provenance)
		if err != nil {
			return syncservice.Result{}, repositoryError(ctx)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO "+table("observations")+" (owner_id,id,project_id,session_id,scope,type,title,content,topic_key,provenance,lifecycle,review_state,created_at,updated_at,review_after,version) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,NULLIF($9,''),$10,'archived','needs_review',$11,$12,$13,$14)", r.ownerID, o.ID, o.ProjectID, o.SessionID, o.Scope, o.Type, o.Title, o.Content, o.TopicKey, provenance, o.CreatedAt, o.UpdatedAt, o.ReviewAfter, version); err != nil {
			return syncservice.Result{}, repositoryError(ctx)
		}
		for _, reference := range o.References {
			if _, err = tx.Exec(ctx, "INSERT INTO "+table("observation_references")+" (owner_id,observation_id,target_observation_id) VALUES ($1,$2,$3)", r.ownerID, o.ID, reference); err != nil {
				return syncservice.Result{}, repositoryError(ctx)
			}
		}
	} else if _, err = tx.Exec(ctx, "UPDATE "+table("observations")+" SET review_state='needs_review' WHERE owner_id=$1 AND id=$2 AND lifecycle<>'tombstoned'", r.ownerID, m.RecordID); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	var versionID int64
	if err = tx.QueryRow(ctx, "INSERT INTO "+table("record_versions")+" (owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,'observation',$2,$3,$4,$5,$6,'conflict',$7) RETURNING id", r.ownerID, m.RecordID, version, device, mutationID, m.BaseVersion, state).Scan(&versionID); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+table("changes")+" (owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,canonical_version,version_id) VALUES ($1,$2,$3,$4,'conflict','observation',$5,$6,$7)", r.ownerID, seq, device, mutationID, m.RecordID, canonical, versionID); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	conflictID, err := uuid.NewRandom()
	if err != nil || conflictID == uuid.Nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+table("observation_conflicts")+" (owner_id,conflict_id,observation_id,canonical_version,competing_version_id,status,created_seq) VALUES ($1,$2,$3,$4,$5,'unresolved',$6)", r.ownerID, conflictID, m.RecordID, canonical, versionID, seq); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if _, err = tx.Exec(ctx, "UPDATE "+table("owner_sync_state")+" SET next_seq=$2 WHERE owner_id=$1 AND next_seq=$3", r.ownerID, seq+1, seq); err != nil {
		return syncservice.Result{}, repositoryError(ctx)
	}
	if err = commitRepository(ctx, tx); err != nil {
		return syncservice.Result{}, err
	}
	return syncservice.Result{MutationID: m.MutationID, Disposition: syncservice.DispositionConflict, Sequence: &seq, Version: canonical}, nil
}

func (r *Repository) activeObservation(ctx context.Context, tx pgx.Tx, table func(string) string, id string) bool {
	var lifecycle string
	return tx.QueryRow(ctx, "SELECT lifecycle FROM "+table("observations")+" WHERE owner_id=$1 AND id=$2", r.ownerID, id).Scan(&lifecycle) == nil && lifecycle == string(syncservice.LifecycleActive)
}

func (r *Repository) tombstonedObservation(ctx context.Context, tx pgx.Tx, table func(string) string, id string) bool {
	var lifecycle string
	return tx.QueryRow(ctx, "SELECT lifecycle FROM "+table("observations")+" WHERE owner_id=$1 AND id=$2", r.ownerID, id).Scan(&lifecycle) == nil && lifecycle == string(syncservice.LifecycleTombstoned)
}

func (r *Repository) unresolvedConflicts(ctx context.Context, tx pgx.Tx, table func(string) string, id string, ids []string) bool {
	var count int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM (SELECT conflict_id FROM "+table("observation_conflicts")+" WHERE owner_id=$1 AND observation_id=$2 AND status='unresolved' AND conflict_id::text=ANY($3) FOR UPDATE) conflicts", r.ownerID, id, ids).Scan(&count)
	return err == nil && count == len(ids)
}

func normalizeConflictIDs(ids []string) ([]string, bool) {
	normalized := make([]string, len(ids))
	for index, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, false
		}
		normalized[index] = parsed.String()
	}
	return normalized, true
}

func (r *Repository) writeTombstone(ctx context.Context, tx pgx.Tx, table func(string) string, id string, version int64) error {
	_, err := tx.Exec(ctx, "UPDATE "+table("observations")+" SET content='',topic_key=NULL,lifecycle='tombstoned',review_state='clear',version=$3 WHERE owner_id=$1 AND id=$2", r.ownerID, id, version)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "DELETE FROM "+table("observation_references")+" WHERE owner_id=$1 AND observation_id=$2", r.ownerID, id)
	return err
}

func (r *Repository) lockTarget(ctx context.Context, tx pgx.Tx, table func(string) string, m syncservice.Mutation) (int64, bool, error) {
	var version int64
	var err error
	switch m.RecordKind {
	case syncservice.RecordKindProject:
		err = tx.QueryRow(ctx, "SELECT version FROM "+table("projects")+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", r.ownerID, m.RecordID).Scan(&version)
	case syncservice.RecordKindSession:
		err = tx.QueryRow(ctx, "SELECT version FROM "+table("sessions")+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", r.ownerID, m.RecordID).Scan(&version)
	default:
		err = tx.QueryRow(ctx, "SELECT version FROM "+table("observations")+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", r.ownerID, m.RecordID).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return version, err == nil, err
}

func (r *Repository) validatePrerequisites(ctx context.Context, tx pgx.Tx, table func(string) string, m syncservice.Mutation) error {
	project := ""
	if m.Session != nil {
		project = m.Session.ProjectID
	}
	if m.Observation != nil {
		project = m.Observation.ProjectID
	}
	if project == "" {
		return nil
	}
	var found bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table("projects")+" WHERE owner_id=$1 AND id=$2)", r.ownerID, project).Scan(&found); err != nil || !found {
		return errors.New("project")
	}
	if m.Observation != nil && m.Observation.SessionID != "" {
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table("sessions")+" WHERE owner_id=$1 AND id=$2 AND project_id=$3)", r.ownerID, m.Observation.SessionID, project).Scan(&found); err != nil || !found {
			return errors.New("session")
		}
	}
	if m.Observation != nil {
		if m.Kind == syncservice.MutationUpdate {
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table("observation_references")+" r JOIN "+table("observations")+" o ON o.owner_id=r.owner_id AND o.id=r.observation_id WHERE r.owner_id=$1 AND r.target_observation_id=$2 AND o.project_id<>$3)", r.ownerID, m.RecordID, project).Scan(&found); err != nil || found {
				return errors.New("incoming reference")
			}
		}
		for _, reference := range m.Observation.References {
			if reference == m.RecordID {
				return errors.New("reference")
			}
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table("observations")+" WHERE owner_id=$1 AND id=$2 AND project_id=$3)", r.ownerID, reference, project).Scan(&found); err != nil || !found {
				return errors.New("reference")
			}
		}
	}
	if m.Session != nil && m.Kind == syncservice.MutationUpdate {
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table("observations")+" WHERE owner_id=$1 AND session_id=$2 AND project_id<>$3)", r.ownerID, m.RecordID, project).Scan(&found); err != nil || found {
			return errors.New("session dependent")
		}
	}
	return nil
}

var errTopicCollision = errors.New("topic collision")

func (r *Repository) lockTopic(ctx context.Context, tx pgx.Tx, table func(string) string, m syncservice.Mutation) error {
	if m.Observation == nil || m.Observation.TopicKey == "" {
		return nil
	}
	o := m.Observation
	key := r.ownerID.String() + ":" + o.ProjectID + ":" + o.Scope + ":" + o.TopicKey
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key); err != nil {
		return err
	}
	var id string
	err := tx.QueryRow(ctx, "SELECT id FROM "+table("observations")+" WHERE owner_id=$1 AND project_id=$2 AND scope=$3 AND topic_key=$4 AND lifecycle='active' FOR UPDATE", r.ownerID, o.ProjectID, o.Scope, o.TopicKey).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || id == m.RecordID {
		return nil
	}
	if err != nil {
		return err
	}
	return errTopicCollision
}

func (r *Repository) writeCanonical(ctx context.Context, tx pgx.Tx, table func(string) string, m syncservice.Mutation, version int64, payload []byte) error {
	switch m.RecordKind {
	case syncservice.RecordKindProject:
		if m.Kind == syncservice.MutationCreate {
			_, err := tx.Exec(ctx, "INSERT INTO "+table("projects")+" (owner_id,id,version,payload) VALUES ($1,$2,$3,$4)", r.ownerID, m.RecordID, version, payload)
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE "+table("projects")+" SET version=$3,payload=$4 WHERE owner_id=$1 AND id=$2", r.ownerID, m.RecordID, version, payload)
		return err
	case syncservice.RecordKindSession:
		s := m.Session
		if m.Kind == syncservice.MutationCreate {
			_, err := tx.Exec(ctx, "INSERT INTO "+table("sessions")+" (owner_id,id,project_id,version,payload) VALUES ($1,$2,$3,$4,$5)", r.ownerID, m.RecordID, s.ProjectID, version, payload)
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE "+table("sessions")+" SET project_id=$3,version=$4,payload=$5 WHERE owner_id=$1 AND id=$2", r.ownerID, s.ID, s.ProjectID, version, payload)
		return err
	default:
		o := m.Observation
		provenance, err := json.Marshal(o.Provenance)
		if err != nil {
			return err
		}
		if m.Kind == syncservice.MutationCreate {
			_, err = tx.Exec(ctx, "INSERT INTO "+table("observations")+" (owner_id,id,project_id,session_id,scope,type,title,content,topic_key,provenance,lifecycle,review_state,created_at,updated_at,review_after,version) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,NULLIF($9,''),$10,'active','clear',$11,$12,$13,$14)", r.ownerID, o.ID, o.ProjectID, o.SessionID, o.Scope, o.Type, o.Title, o.Content, o.TopicKey, provenance, o.CreatedAt, o.UpdatedAt, o.ReviewAfter, version)
		} else {
			lifecycle := "active"
			if m.Kind == syncservice.MutationArchive {
				lifecycle = "archived"
			}
			_, err = tx.Exec(ctx, "UPDATE "+table("observations")+" SET project_id=$3,session_id=NULLIF($4,''),scope=$5,type=$6,title=$7,content=$8,topic_key=NULLIF($9,''),provenance=$10,lifecycle=$11,review_state='clear',created_at=$12,updated_at=$13,review_after=$14,version=$15 WHERE owner_id=$1 AND id=$2", r.ownerID, o.ID, o.ProjectID, o.SessionID, o.Scope, o.Type, o.Title, o.Content, o.TopicKey, provenance, lifecycle, o.CreatedAt, o.UpdatedAt, o.ReviewAfter, version)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM "+table("observation_references")+" WHERE owner_id=$1 AND observation_id=$2", r.ownerID, o.ID); err != nil {
			return err
		}
		for _, reference := range o.References {
			if _, err = tx.Exec(ctx, "INSERT INTO "+table("observation_references")+" (owner_id,observation_id,target_observation_id) VALUES ($1,$2,$3)", r.ownerID, o.ID, reference); err != nil {
				return err
			}
		}
		return nil
	}
}

func (r *Repository) owners(ctx context.Context, tx pgx.Tx, schema string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM "+pgx.Identifier{schema, "owners"}.Sanitize())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var owners []uuid.UUID
	for rows.Next() {
		var owner uuid.UUID
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

func (r *Repository) ownerState(ctx context.Context, tx pgx.Tx, schema string) (OwnerSyncState, error) {
	var state OwnerSyncState
	state.OwnerID = r.ownerID
	var maximum, minimum, count int64
	err := tx.QueryRow(ctx, `SELECT s.history_id, s.next_seq, COALESCE(MAX(c.seq), 0),
		COALESCE(MIN(c.seq), 0), COUNT(c.seq)
		FROM `+pgx.Identifier{schema, "owner_sync_state"}.Sanitize()+` s
		LEFT JOIN `+pgx.Identifier{schema, "changes"}.Sanitize()+` c ON c.owner_id = s.owner_id
		WHERE s.owner_id = $1
		GROUP BY s.history_id, s.next_seq`, r.ownerID).Scan(&state.HistoryID, &state.NextSeq, &maximum, &minimum, &count)
	if err != nil {
		return state, err
	}
	if state.HistoryID == uuid.Nil || state.NextSeq != maximum+1 || (count != 0 && (minimum != 1 || count != maximum)) {
		return state, ErrRepository
	}
	return state, nil
}

func repositoryMigrationsValid(ctx context.Context, tx pgx.Tx, schema string) bool {
	if err := validateMigrations(ctx, tx, migrations, schema); err != nil {
		return false
	}
	var count int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "sync_schema_migrations"}.Sanitize()).Scan(&count)
	return err == nil && count == len(migrations)
}

func commitRepository(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return repositoryError(ctx)
	}
	return nil
}

func repositoryError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrRepository
}
