package syncpg

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryValidatesArguments(t *testing.T) {
	if _, err := NewRepository(nil, uuid.New()); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("NewRepository(nil) error = %v, want ErrInvalidRepository", err)
	}
	if _, err := NewRepository((*pgx.Conn)(nil), uuid.New()); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("NewRepository(typed nil conn) error = %v, want ErrInvalidRepository", err)
	}
	if _, err := NewRepository((*pgxpool.Pool)(nil), uuid.New()); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("NewRepository(typed nil pool) error = %v, want ErrInvalidRepository", err)
	}
	if _, err := NewRepository(nil, uuid.Nil); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("NewRepository(nil owner) error = %v, want ErrInvalidRepository", err)
	}
}

func TestRepositoryRequiresMigrationAndBootstrapsOwner(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	owner := uuid.New()
	repo, err := NewRepository(conn, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); !errors.Is(err, ErrRepository) {
		t.Fatalf("EnsureOwner before migration error = %v, want ErrRepository", err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatalf("EnsureOwner: %v", err)
	}
	state, err := repo.OwnerState(ctx)
	if err != nil {
		t.Fatalf("OwnerState: %v", err)
	}
	if state.OwnerID != owner || state.HistoryID == uuid.Nil || state.NextSeq != 1 {
		t.Fatalf("OwnerState() = %+v, want owner, non-zero history, next sequence 1", state)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatalf("idempotent EnsureOwner: %v", err)
	}
	again, err := repo.OwnerState(ctx)
	if err != nil || again != state {
		t.Fatalf("idempotent state = %+v, %v; want %+v, nil", again, err, state)
	}
}

func TestRepositoryOwnerConflictAndReadOnlyState(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	firstOwner := uuid.New()
	first, err := NewRepository(conn, firstOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	secondOwner := uuid.New()
	second, err := NewRepository(conn, secondOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.EnsureOwner(ctx); !errors.Is(err, ErrOwnerConflict) {
		t.Fatalf("EnsureOwner different owner error = %v, want ErrOwnerConflict", err)
	}
	if _, err := second.OwnerState(ctx); !errors.Is(err, ErrOwnerConflict) {
		t.Fatalf("OwnerState different owner error = %v, want ErrOwnerConflict", err)
	}
	var owners int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM owners").Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners after conflict = %d, %v; want 1, nil", owners, err)
	}
}

func TestRepositoryNotInitializedCancellationAndGenericFailure(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	repo, err := NewRepository(conn, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.OwnerState(ctx); !errors.Is(err, ErrOwnerNotInitialized) {
		t.Fatalf("OwnerState before bootstrap error = %v, want ErrOwnerNotInitialized", err)
	}
	var schema string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := repo.EnsureOwner(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureOwner canceled error = %v, want context.Canceled", err)
	}
	verify, err := pgx.Connect(ctx, os.Getenv("VGXNESS_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close(ctx)
	if _, err := verify.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	var owners int
	if err := verify.QueryRow(ctx, "SELECT count(*) FROM owners").Scan(&owners); err != nil || owners != 0 {
		t.Fatalf("owners after canceled bootstrap = %d, %v; want 0, nil", owners, err)
	}
	if _, err := verify.Exec(ctx, "DROP TABLE owner_sync_state"); err != nil {
		t.Fatal(err)
	}
	repo, err = NewRepository(verify, owner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.OwnerState(ctx)
	if !errors.Is(err, ErrRepository) || strings.Contains(err.Error(), "owner_sync_state") {
		t.Fatalf("OwnerState SQL failure = %v, want content-free ErrRepository", err)
	}
}

func TestRepositoryRejectsCorruptOwnerState(t *testing.T) {
	for name, corrupt := range map[string]func(context.Context, *pgx.Conn) error{
		"zero history": func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "UPDATE owner_sync_state SET history_id = $1", uuid.Nil)
			return err
		},
		"sequence gap": func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "UPDATE owner_sync_state SET next_seq = 2")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, conn := context.Background(), testConn(t)
			if err := Migrate(ctx, conn); err != nil {
				t.Fatal(err)
			}
			owner := uuid.New()
			repo, err := NewRepository(conn, owner)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.EnsureOwner(ctx); err != nil {
				t.Fatal(err)
			}
			if err := corrupt(ctx, conn); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.OwnerState(ctx); !errors.Is(err, ErrRepository) {
				t.Fatalf("OwnerState corrupt state error = %v, want ErrRepository", err)
			}
			if err := repo.EnsureOwner(ctx); !errors.Is(err, ErrRepository) {
				t.Fatalf("EnsureOwner corrupt state error = %v, want ErrRepository", err)
			}
		})
	}
}

func TestRepositoryConcurrentPoolBootstrap(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(os.Getenv("VGXNESS_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{schema}.Sanitize()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	owner := uuid.New()
	const workers = 8
	states := make(chan OwnerSyncState, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			repo, err := NewRepository(pool, owner)
			if err == nil {
				err = repo.EnsureOwner(ctx)
			}
			if err == nil {
				var state OwnerSyncState
				state, err = repo.OwnerState(ctx)
				states <- state
			}
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	close(states)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var want OwnerSyncState
	for state := range states {
		if want.OwnerID == uuid.Nil {
			want = state
		} else if state != want {
			t.Fatalf("concurrent state = %+v, want %+v", state, want)
		}
	}
	var owners, statesCount int
	if err := conn.QueryRow(ctx, "SELECT count(*), (SELECT count(*) FROM owner_sync_state) FROM owners").Scan(&owners, &statesCount); err != nil || owners != 1 || statesCount != 1 {
		t.Fatalf("bootstrap rows = %d/%d, %v; want 1/1, nil", owners, statesCount, err)
	}
}
