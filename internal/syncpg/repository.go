package syncpg

import (
	"context"
	"errors"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
