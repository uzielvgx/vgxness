package syncpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgxness/vgxness/internal/syncapi"
	"github.com/vgxness/vgxness/internal/syncservice"
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

func TestRepositoryPushPersistsCanonicalHistory(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(conn, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	device, err := repo.IssueDevice(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	project := mutationProject("project", 0)
	session := mutationSession("session", "project", 0)
	reference := mutationObservation("reference", "project", "session", nil, 0)
	observation := mutationObservation("observation", "project", "session", []string{"reference"}, 0)
	observation.Observation.TopicKey = "topic"
	observation.Observation.Provenance = syncservice.Provenance{Producer: "test", SourceProvider: "provider", SourceID: "source"}
	var results []syncservice.Result
	for _, mutation := range []syncservice.Mutation{project, session, reference, observation} {
		result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutation})
		if err != nil {
			t.Fatalf("push %s: %v", mutation.RecordID, err)
		}
		results = append(results, result...)
	}
	for i, result := range results {
		if result.Disposition != syncservice.DispositionAccepted || result.Version != 1 || result.Sequence == nil || *result.Sequence != int64(i+1) {
			t.Fatalf("result[%d] = %+v", i, result)
		}
	}
	for table, want := range map[string]int{"projects": 1, "sessions": 1, "observations": 2, "record_versions": 4, "mutations": 4, "changes": 4, "observation_references": 1} {
		var got int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d, %v; want %d", table, got, err, want)
		}
	}
	var provenance, snapshot string
	if err := conn.QueryRow(ctx, "SELECT provenance::text, (SELECT snapshot::text FROM record_versions WHERE record_kind='observation' AND record_id='observation') FROM observations WHERE id='observation'").Scan(&provenance, &snapshot); err != nil || !strings.Contains(provenance, `"producer": "test"`) || strings.Contains(provenance, `"content"`) || !strings.Contains(snapshot, `"content"`) {
		t.Fatalf("provenance/snapshot = %q/%q, %v", provenance, snapshot, err)
	}
	update := observation
	update.MutationID, update.Kind, update.BaseVersion = uuid.NewString(), syncservice.MutationUpdate, 1
	update.Observation.Title, update.Observation.Content = "updated", "updated content"
	result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{update})
	if err != nil || len(result) != 1 || result[0].Version != 2 || result[0].Sequence == nil || *result[0].Sequence != 5 {
		t.Fatalf("update = %+v, %v", result, err)
	}
	var title string
	if err := conn.QueryRow(ctx, "SELECT title FROM observations WHERE id='observation'").Scan(&title); err != nil || title != "updated" {
		t.Fatalf("canonical update = %q, %v", title, err)
	}
	var versions, sequences []int64
	var original string
	if err := conn.QueryRow(ctx, "SELECT array_agg(record_version ORDER BY record_version), (SELECT array_agg(seq ORDER BY seq) FROM changes), (SELECT snapshot::text FROM record_versions WHERE record_id='observation' AND record_version=1) FROM record_versions WHERE record_id='observation'").Scan(&versions, &sequences, &original); err != nil || strings.Contains(original, "updated content") || len(versions) != 2 || versions[0] != 1 || versions[1] != 2 || len(sequences) != 5 || sequences[0] != 1 || sequences[4] != 5 {
		t.Fatalf("immutable history = %v/%v/%q, %v", versions, sequences, original, err)
	}
}

func TestRepositoryPushReplaysAndRejectsWithoutEffects(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(conn, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	device, err := repo.IssueDevice(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	project := mutationProject("project", 0)
	first, err := repo.Push(ctx, device.ID, []syncservice.Mutation{project})
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.Push(ctx, device.ID, []syncservice.Mutation{project})
	if err != nil || again[0].Disposition != syncservice.DispositionPreviouslyAccepted || again[0].Sequence == nil || *again[0].Sequence != *first[0].Sequence {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	before := mutationEffects(t, ctx, conn)
	mismatch := project
	mismatch.RecordID, mismatch.Project = "other", &syncservice.Project{ID: "other"}
	archive := mutationObservation("archive", "project", "", nil, 0)
	archive.Kind, archive.Observation.Lifecycle = syncservice.MutationArchive, syncservice.LifecycleArchived
	for _, test := range []struct {
		mutation syncservice.Mutation
		code     string
	}{{mismatch, "mutation_id_hash_mismatch"}, {archive, "unsupported_semantic"}} {
		got, err := repo.Push(ctx, device.ID, []syncservice.Mutation{test.mutation})
		if err != nil || got[0].Disposition != syncservice.DispositionRejected || got[0].Code != test.code {
			t.Fatalf("rejection = %+v, %v; want %s", got, err, test.code)
		}
	}
	if got := mutationEffects(t, ctx, conn); got != before {
		t.Fatalf("effects = %d, want %d", got, before)
	}
}
func TestRepositoryPushRejectsMaxVersionUpdateWithoutEffects(t *testing.T) {
	for _, test := range []struct {
		name, table string
		observation bool
	}{
		{"project", "projects", false},
		{"observation", "observations", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, conn := context.Background(), testConn(t)
			mustNoError(t, Migrate(ctx, conn))
			repo, err := NewRepository(conn, uuid.New())
			mustNoError(t, err)
			mustNoError(t, repo.EnsureOwner(ctx))
			device, err := repo.IssueDevice(ctx, "test")
			mustNoError(t, err)
			mutation := mutationProject("project", 0)
			if test.observation {
				_, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutation})
				mustNoError(t, err)
				mutation = mutationObservation("observation", "project", "", nil, 0)
			}
			_, err = repo.Push(ctx, device.ID, []syncservice.Mutation{mutation})
			mustNoError(t, err)
			_, err = conn.Exec(ctx, "UPDATE "+test.table+" SET version=$1 WHERE id=$2", int64(math.MaxInt64), mutation.RecordID)
			mustNoError(t, err)
			before, err := repo.OwnerState(ctx)
			mustNoError(t, err)
			effects := mutationEffects(t, ctx, conn)
			mutation.MutationID, mutation.Kind, mutation.BaseVersion = uuid.NewString(), syncservice.MutationUpdate, math.MaxInt64
			got, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutation})
			if err != nil || len(got) != 1 || got[0].Disposition != syncservice.DispositionRejected || got[0].Code != "invalid_base" {
				t.Fatalf("overflow update = %+v, %v; want rejected invalid_base", got, err)
			}
			if mutationEffects(t, ctx, conn) != effects {
				t.Fatal("overflow update changed stored effects")
			}
			var version int64
			if err := conn.QueryRow(ctx, "SELECT version FROM "+test.table+" WHERE id=$1", mutation.RecordID).Scan(&version); err != nil || version != math.MaxInt64 {
				t.Fatalf("canonical version = %d, %v; want %d", version, err, int64(math.MaxInt64))
			}
			after, err := repo.OwnerState(ctx)
			if err != nil || after.NextSeq != before.NextSeq {
				t.Fatalf("next sequence = %d, %v; want %d", after.NextSeq, err, before.NextSeq)
			}
			independent, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutationProject("independent", 0)})
			if err != nil || len(independent) != 1 || independent[0].Sequence == nil || *independent[0].Sequence != before.NextSeq {
				t.Fatalf("independent create = %+v, %v; want sequence %d", independent, err, before.NextSeq)
			}
		})
	}
}
func TestRepositoryPushRejectsPrerequisitesAndMoves(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(conn, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	device, err := repo.IssueDevice(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	seed := []syncservice.Mutation{mutationProject("one", 0), mutationProject("two", 0), mutationSession("session", "one", 0), mutationObservation("reference", "two", "", nil, 0), mutationObservation("topic", "one", "", nil, 0), mutationObservation("target", "one", "", nil, 0), mutationObservation("source", "one", "", []string{"target"}, 0), mutationObservation("dependent", "one", "session", nil, 0)}
	seed[4].Observation.TopicKey = "shared"
	if _, err := repo.Push(ctx, device.ID, seed); err != nil {
		t.Fatal(err)
	}
	before := mutationEffects(t, ctx, conn)
	crossSession := mutationObservation("cross-session", "two", "session", nil, 0)
	crossReference := mutationObservation("cross-reference", "one", "", []string{"reference"}, 0)
	observationMove := seed[5]
	observationMove.MutationID, observationMove.Kind, observationMove.BaseVersion, observationMove.Observation.ProjectID = uuid.NewString(), syncservice.MutationUpdate, 1, "two"
	sessionMove := seed[2]
	sessionMove.MutationID, sessionMove.Kind, sessionMove.BaseVersion, sessionMove.Session.ProjectID = uuid.NewString(), syncservice.MutationUpdate, 1, "two"
	for _, test := range []struct {
		mutation syncservice.Mutation
		code     string
	}{{mutationObservation("missing", "one", "missing", nil, 0), "invalid_prerequisite"}, {crossSession, "invalid_prerequisite"}, {crossReference, "invalid_prerequisite"}, {observationMove, "invalid_prerequisite"}, {sessionMove, "invalid_prerequisite"}} {
		got, err := repo.Push(ctx, device.ID, []syncservice.Mutation{test.mutation})
		if err != nil || got[0].Disposition != syncservice.DispositionRejected || got[0].Code != test.code {
			t.Fatalf("rejection = %+v, %v; want %s", got, err, test.code)
		}
	}
	if err := repo.RevokeDevice(ctx, device.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutationProject("revoked", 0)}); err != nil || got[0].Disposition != syncservice.DispositionRejected || got[0].Code != "revoked" {
		t.Fatalf("revoked = %+v, %v", got, err)
	}
	if got := mutationEffects(t, ctx, conn); got != before {
		t.Fatalf("effects = %d, want %d", got, before)
	}
}

type changeFailDB struct {
	conn *pgx.Conn
	fail bool
}

func (db *changeFailDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := db.conn.Begin(ctx)
	return changeFailTx{Tx: tx, fail: &db.fail}, err
}

type changeFailTx struct {
	pgx.Tx
	fail *bool
}

func (tx changeFailTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if *tx.fail && strings.Contains(sql, "INSERT INTO") && strings.Contains(sql, "changes") {
		return pgconn.CommandTag{}, errors.New("forced change failure")
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func TestRepositoryPushRollsBackPostWriteFailureWithoutGap(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	db := &changeFailDB{conn: conn}
	repo, err := NewRepository(db, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	device, err := repo.IssueDevice(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.OwnerState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db.fail = true
	result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutationProject("project", 0)})
	db.fail = false
	if !errors.Is(err, ErrRepository) || result != nil {
		t.Fatalf("post-write failure = %+v, %v", result, err)
	}
	if got := mutationEffects(t, ctx, conn); got != 0 {
		t.Fatalf("effects = %d, want 0", got)
	}
	after, err := repo.OwnerState(ctx)
	if err != nil || after.NextSeq != before.NextSeq {
		t.Fatalf("next sequence = %d, %v; want %d", after.NextSeq, err, before.NextSeq)
	}
	result, err = repo.Push(ctx, device.ID, []syncservice.Mutation{mutationProject("project", 0)})
	if err != nil || result[0].Sequence == nil || *result[0].Sequence != before.NextSeq {
		t.Fatalf("retry = %+v, %v", result, err)
	}
}

func TestRepositoryPushFailsClosedOnCorruptOwnerSequence(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(conn, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	device, err := repo.IssueDevice(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "UPDATE owner_sync_state SET next_seq=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutationProject("project", 0)}); !errors.Is(err, ErrRepository) {
		t.Fatalf("push = %v, want ErrRepository", err)
	}
	if got := mutationEffects(t, ctx, conn); got != 0 {
		t.Fatalf("effects = %d, want 0", got)
	}
}

func TestRepositoryMutationRollsBackPrerequisiteQueryFailure(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(conn, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	device, err := repo.IssueDevice(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutationProject("project", 0)}); err != nil {
		t.Fatal(err)
	}
	before := mutationEffects(t, ctx, conn)
	state, err := repo.OwnerState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	locker, err := pgx.Connect(ctx, os.Getenv("VGXNESS_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	var lock pgx.Tx
	timeout := false
	cleanup := func() {
		if timeout {
			if _, err := conn.Exec(context.Background(), "RESET statement_timeout"); err != nil {
				t.Error(err)
			}
			timeout = false
		}
		if lock != nil {
			if err := lock.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Error(err)
			}
			lock = nil
		}
		if locker != nil {
			locker.Close(context.Background())
			locker = nil
		}
	}
	t.Cleanup(cleanup)
	if _, err := locker.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	lock, err = locker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, "LOCK TABLE projects IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "SET statement_timeout='250ms'"); err != nil {
		t.Fatal(err)
	}
	timeout = true
	result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutationObservation("observation", "project", "", nil, 0)})
	cleanup()
	if !errors.Is(err, ErrRepository) || result != nil {
		t.Fatalf("locked prerequisite = %+v, %v", result, err)
	}
	if got := mutationEffects(t, ctx, conn); got != before {
		t.Fatalf("effects = %d, want %d", got, before)
	}
	after, err := repo.OwnerState(ctx)
	if err != nil || after.NextSeq != state.NextSeq {
		t.Fatalf("next sequence = %d, %v; want %d", after.NextSeq, err, state.NextSeq)
	}
	result, err = repo.Push(ctx, device.ID, []syncservice.Mutation{mutationObservation("observation", "project", "", nil, 0)})
	if err != nil || result[0].Sequence == nil || *result[0].Sequence != state.NextSeq {
		t.Fatalf("retry = %+v, %v", result, err)
	}
}

func TestRepositoryConflictRetainsCompetingObservationAndReplays(t *testing.T) {
	ctx, repo, conn, first, second := conflictRepository(t)
	seed := []syncservice.Mutation{mutationProject("project", 0), mutationSession("session", "project", 0), mutationObservation("reference", "project", "session", nil, 0), mutationObservation("target", "project", "session", []string{"reference"}, 0)}
	if _, err := repo.Push(ctx, first, seed); err != nil {
		t.Fatal(err)
	}
	accepted := updateObservation(seed[3], 1, "canonical")
	if got, err := repo.Push(ctx, first, []syncservice.Mutation{accepted}); err != nil || got[0].Disposition != syncservice.DispositionAccepted {
		t.Fatalf("accepted update = %+v, %v", got, err)
	}
	conflict := updateObservation(seed[3], 1, "competing")
	got, err := repo.Push(ctx, second, []syncservice.Mutation{conflict})
	if err != nil || len(got) != 1 || got[0].Disposition != syncservice.DispositionConflict || got[0].Sequence == nil || *got[0].Sequence != 6 || got[0].Version != 2 {
		t.Fatalf("stale update = %+v, %v", got, err)
	}
	var content, lifecycle, review, snapshot string
	var version int64
	if err := conn.QueryRow(ctx, "SELECT content, lifecycle, review_state, version, (SELECT snapshot::text FROM record_versions WHERE record_id='target' AND disposition='conflict') FROM observations WHERE id='target'").Scan(&content, &lifecycle, &review, &version, &snapshot); err != nil || content != "canonical" || lifecycle != "active" || review != "needs_review" || version != 2 || !strings.Contains(snapshot, "competing") {
		t.Fatalf("canonical/conflict snapshot = %q/%q/%q/%d/%q, %v", content, lifecycle, review, version, snapshot, err)
	}
	if err := VerifyRecovery(ctx, conn); err != nil {
		t.Fatalf("deep stale conflict recovery: %v", err)
	}
	var mutations, versions, changes, conflicts, next int
	if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM mutations), (SELECT count(*) FROM record_versions), (SELECT count(*) FROM changes), (SELECT count(*) FROM observation_conflicts), (SELECT next_seq FROM owner_sync_state)").Scan(&mutations, &versions, &changes, &conflicts, &next); err != nil || mutations != 6 || versions != 6 || changes != 6 || conflicts != 1 || next != 7 {
		t.Fatalf("conflict effects = %d/%d/%d/%d/%d, %v", mutations, versions, changes, conflicts, next, err)
	}
	replay, err := repo.Push(ctx, second, []syncservice.Mutation{conflict})
	if err != nil || len(replay) != 1 || replay[0].MutationID != got[0].MutationID || replay[0].Disposition != got[0].Disposition || replay[0].Version != got[0].Version || replay[0].Sequence == nil || got[0].Sequence == nil || *replay[0].Sequence != *got[0].Sequence || replay[0].Code != got[0].Code || replay[0].Retryable != got[0].Retryable {
		t.Fatalf("conflict replay = %+v, %v; want %+v", replay, err, got[0])
	}
	before := mutationEffects(t, ctx, conn)
	mismatch := conflict
	mismatch.Observation = &syncservice.Observation{}
	*mismatch.Observation = *conflict.Observation
	mismatch.Observation.Content = "changed hash"
	if rejected, err := repo.Push(ctx, second, []syncservice.Mutation{mismatch}); err != nil || rejected[0].Disposition != syncservice.DispositionRejected || rejected[0].Code != "mutation_id_hash_mismatch" || mutationEffects(t, ctx, conn) != before {
		t.Fatalf("hash mismatch = %+v, %v", rejected, err)
	}
	wrongOwner, err := NewRepository(conn, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if rejected, err := wrongOwner.Push(ctx, second, []syncservice.Mutation{updateObservation(seed[3], 1, "wrong owner")}); !errors.Is(err, ErrOwnerConflict) || rejected != nil {
		t.Fatalf("wrong owner conflict = %+v, %v", rejected, err)
	}
}

func TestRepositoryMutationArchiveFreesTopicAfterConflicts(t *testing.T) {
	ctx, repo, conn, first, second := conflictRepository(t)
	project := mutationProject("project", 0)
	holder := mutationObservation("holder", "project", "", nil, 0)
	holder.Observation.TopicKey = "shared"
	other := mutationObservation("other", "project", "", nil, 0)
	if _, err := repo.Push(ctx, first, []syncservice.Mutation{project, holder, other}); err != nil {
		t.Fatal(err)
	}
	occupied := updateObservation(other, 1, "competing update")
	occupied.Observation.TopicKey = "shared"
	if got, err := repo.Push(ctx, second, []syncservice.Mutation{occupied}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
		t.Fatalf("occupied update = %+v, %v", got, err)
	}
	create := mutationObservation("create-conflict", "project", "", nil, 0)
	create.Observation.TopicKey = "shared"
	if got, err := repo.Push(ctx, second, []syncservice.Mutation{create}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
		t.Fatalf("occupied create = %+v, %v", got, err)
	}
	var holderState, createdState, createdReview string
	if err := conn.QueryRow(ctx, "SELECT (SELECT lifecycle FROM observations WHERE id='holder'), (SELECT lifecycle FROM observations WHERE id='create-conflict'), (SELECT review_state FROM observations WHERE id='create-conflict')").Scan(&holderState, &createdState, &createdReview); err != nil || holderState != "active" || createdState != "archived" || createdReview != "needs_review" {
		t.Fatalf("topic holders = %q/%q/%q, %v", holderState, createdState, createdReview, err)
	}
	var mutationVersion, changeVersion int64
	if err := conn.QueryRow(ctx, "SELECT m.canonical_version, c.canonical_version FROM mutations m JOIN changes c ON c.owner_id=m.owner_id AND c.seq=m.canonical_seq WHERE m.record_id='create-conflict'").Scan(&mutationVersion, &changeVersion); err != nil || mutationVersion < 1 || mutationVersion != changeVersion {
		t.Fatalf("topic conflict canonical versions = %d/%d, %v", mutationVersion, changeVersion, err)
	}
	if err := VerifyRecovery(ctx, conn); err != nil {
		t.Fatalf("topic create conflict recovery: %v", err)
	}
	archive := updateObservation(holder, 1, "archived")
	archive.Kind, archive.Observation.Lifecycle, archive.Observation.Review = syncservice.MutationArchive, syncservice.LifecycleArchived, syncservice.ReviewClear
	if got, err := repo.Push(ctx, first, []syncservice.Mutation{archive}); err != nil || got[0].Disposition != syncservice.DispositionAccepted {
		t.Fatalf("archive = %+v, %v", got, err)
	}
	claim := mutationObservation("claim", "project", "", nil, 0)
	claim.Observation.TopicKey = "shared"
	if got, err := repo.Push(ctx, first, []syncservice.Mutation{claim}); err != nil || got[0].Disposition != syncservice.DispositionAccepted {
		t.Fatalf("topic reuse = %+v, %v", got, err)
	}
}

func TestRepositoryLifecycleResolveAndTombstone(t *testing.T) {
	ctx, repo, conn, first, second := conflictRepository(t)
	project, target := mutationProject("project", 0), mutationObservation("target", "project", "", nil, 0)
	if _, err := repo.Push(ctx, first, []syncservice.Mutation{project, target}); err != nil {
		t.Fatal(err)
	}
	accepted := updateObservation(target, 1, "canonical")
	mustNoError(t, pushAccepted(ctx, repo, first, accepted))
	stale := updateObservation(target, 1, "competing")
	if got, err := repo.Push(ctx, second, []syncservice.Mutation{stale}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
		t.Fatalf("stale = %+v, %v", got, err)
	}
	var conflictID string
	mustNoError(t, conn.QueryRow(ctx, "SELECT conflict_id FROM observation_conflicts WHERE observation_id='target' AND status='unresolved'").Scan(&conflictID))
	resolved := updateObservation(target, 2, "resolved")
	resolved.Kind, resolved.Observation, resolved.Project, resolved.Session = syncservice.MutationResolve, nil, nil, nil
	resolved.Resolution = &syncservice.Resolution{ConflictIDs: []string{strings.ToUpper(conflictID)}, Observation: updateObservation(target, 2, "resolved").Observation}
	mustNoError(t, pushAccepted(ctx, repo, first, resolved))
	var status string
	var resolvedSeq int64
	mustNoError(t, conn.QueryRow(ctx, "SELECT status, resolved_seq FROM observation_conflicts WHERE conflict_id=$1", conflictID).Scan(&status, &resolvedSeq))
	if status != "resolved" || resolvedSeq != 5 {
		t.Fatalf("resolution linkage = %q/%d", status, resolvedSeq)
	}
	tombstone := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "target", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 3, Tombstone: &syncservice.Tombstone{DeletedAt: time.Now().UTC()}}
	mustNoError(t, pushAccepted(ctx, repo, first, tombstone))
	postTombstone := updateObservation(target, 3, "resurrection")
	if got, err := repo.Push(ctx, second, []syncservice.Mutation{postTombstone}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
		t.Fatalf("post-tombstone stale = %+v, %v", got, err)
	}
	var lifecycle, content string
	mustNoError(t, conn.QueryRow(ctx, "SELECT lifecycle, content FROM observations WHERE id='target'").Scan(&lifecycle, &content))
	if lifecycle != "tombstoned" || content != "" {
		t.Fatalf("tombstone canonical = %q/%q", lifecycle, content)
	}
	for _, mutation := range []syncservice.Mutation{
		updateObservation(target, 4, "current-base resurrection"),
		func() syncservice.Mutation {
			m := updateObservation(target, 4, "current-base archive")
			m.Kind, m.Observation.Lifecycle = syncservice.MutationArchive, syncservice.LifecycleArchived
			return m
		}(),
	} {
		if got, err := repo.Push(ctx, first, []syncservice.Mutation{mutation}); err != nil || got[0].Disposition != syncservice.DispositionRejected {
			t.Fatalf("tombstoned current-base mutation = %+v, %v", got, err)
		}
	}
	cannotResolve := resolved
	cannotResolve.MutationID, cannotResolve.BaseVersion = uuid.NewString(), 4
	if got, err := repo.Push(ctx, first, []syncservice.Mutation{cannotResolve}); err != nil || got[0].Disposition != syncservice.DispositionRejected {
		t.Fatalf("tombstoned resolution = %+v, %v", got, err)
	}
	if err := conn.QueryRow(ctx, "SELECT lifecycle, content FROM observations WHERE id='target'").Scan(&lifecycle, &content); err != nil || lifecycle != "tombstoned" || content != "" {
		t.Fatalf("tombstone after current-base mutations = %q/%q, %v", lifecycle, content, err)
	}
}

func TestRepositoryPullWatermarkAndResolveRoundTrip(t *testing.T) {
	ctx, repo, conn, first, second := conflictRepository(t)
	project, target := mutationProject("project", 0), mutationObservation("target", "project", "", nil, 0)
	mustNoError(t, pushAccepted(ctx, repo, first, project))
	mustNoError(t, pushAccepted(ctx, repo, first, target))
	mustNoError(t, pushAccepted(ctx, repo, first, updateObservation(target, 1, "canonical")))
	stale := updateObservation(target, 1, "competing")
	if got, err := repo.Push(ctx, second, []syncservice.Mutation{stale}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
		t.Fatalf("conflict = %+v, %v", got, err)
	}
	var conflictID string
	mustNoError(t, conn.QueryRow(ctx, "SELECT conflict_id FROM observation_conflicts WHERE observation_id='target'").Scan(&conflictID))
	resolve := updateObservation(target, 2, "resolved")
	resolve.Kind, resolve.Observation, resolve.Resolution = syncservice.MutationResolve, nil, &syncservice.Resolution{ConflictIDs: []string{conflictID}, Observation: updateObservation(target, 2, "resolved").Observation}
	mustNoError(t, pushAccepted(ctx, repo, first, resolve))
	page, err := repo.Pull(ctx, first, syncservice.Cursor{HistoryID: mustHistory(t, repo)}, 2)
	mustNoError(t, err)
	if !page.HasMore || page.Cursor.Watermark != 5 || page.Cursor.Position != 2 {
		t.Fatalf("first page = %+v", page)
	}
	mustNoError(t, pushAccepted(ctx, repo, first, mutationObservation("later", "project", "", nil, 0)))
	page, err = repo.Pull(ctx, first, page.Cursor, 4)
	mustNoError(t, err)
	if page.HasMore || len(page.Changes) != 3 || page.Changes[2].Mutation.Kind != syncservice.MutationResolve || len(page.Changes[2].Mutation.Resolution.ConflictIDs) != 1 || page.Changes[2].Mutation.Resolution.ConflictIDs[0] != conflictID {
		t.Fatalf("fixed-watermark page = %+v", page)
	}
	if _, err := conn.Exec(ctx, "UPDATE mutations SET resolution_conflict_ids=NULL WHERE kind='resolve'"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Pull(ctx, first, syncservice.Cursor{HistoryID: mustHistory(t, repo)}, 10); !errors.Is(err, ErrRepository) {
		t.Fatalf("old resolve pull = %v", err)
	}
	if err := VerifyRecovery(ctx, conn); !errors.Is(err, ErrRecovery) {
		t.Fatalf("old resolve recovery = %v", err)
	}
}

func TestRepositoryPullRejectsSwappedResolveIdentityAndSequence(t *testing.T) {
	ctx, repo, conn, first, second := conflictRepository(t)
	project, target := mutationProject("project", 0), mutationObservation("target", "project", "", nil, 0)
	mustNoError(t, pushAccepted(ctx, repo, first, project))
	mustNoError(t, pushAccepted(ctx, repo, first, target))
	mustNoError(t, pushAccepted(ctx, repo, first, updateObservation(target, 1, "canonical")))

	resolve := func(base int64, content string) syncservice.Mutation {
		if got, err := repo.Push(ctx, second, []syncservice.Mutation{updateObservation(target, base, "competing")}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
			t.Fatalf("conflict = %+v, %v", got, err)
		}
		var conflictID string
		mustNoError(t, conn.QueryRow(ctx, "SELECT conflict_id FROM observation_conflicts WHERE observation_id='target' AND status='unresolved'").Scan(&conflictID))
		resolved := updateObservation(target, base+1, content)
		resolved.Kind, resolved.Resolution = syncservice.MutationResolve, &syncservice.Resolution{ConflictIDs: []string{conflictID}, Observation: resolved.Observation}
		resolved.Observation = nil
		mustNoError(t, pushAccepted(ctx, repo, first, resolved))
		return resolved
	}
	firstResolve, secondResolve := resolve(1, "first resolved"), resolve(2, "second resolved")

	var firstSeq, secondSeq, temporarySeq int64
	var firstIDs, secondIDs []uuid.UUID
	mustNoError(t, conn.QueryRow(ctx, "SELECT canonical_seq, resolution_conflict_ids FROM mutations WHERE mutation_id=$1", firstResolve.MutationID).Scan(&firstSeq, &firstIDs))
	mustNoError(t, conn.QueryRow(ctx, "SELECT canonical_seq, resolution_conflict_ids FROM mutations WHERE mutation_id=$1", secondResolve.MutationID).Scan(&secondSeq, &secondIDs))
	mustNoError(t, conn.QueryRow(ctx, "SELECT COALESCE(MAX(canonical_seq), 0)+1 FROM mutations WHERE owner_id=$1", repo.ownerID).Scan(&temporarySeq))
	tx, err := conn.Begin(ctx)
	mustNoError(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, "UPDATE mutations SET canonical_seq=$1 WHERE owner_id=$2 AND mutation_id=$3", temporarySeq, repo.ownerID, firstResolve.MutationID)
	mustNoError(t, err)
	_, err = tx.Exec(ctx, "UPDATE mutations SET canonical_seq=$1, resolution_conflict_ids=$2 WHERE owner_id=$3 AND mutation_id=$4", firstSeq, firstIDs, repo.ownerID, secondResolve.MutationID)
	mustNoError(t, err)
	_, err = tx.Exec(ctx, "UPDATE mutations SET canonical_seq=$1, resolution_conflict_ids=$2 WHERE owner_id=$3 AND mutation_id=$4", secondSeq, secondIDs, repo.ownerID, firstResolve.MutationID)
	mustNoError(t, err)
	mustNoError(t, tx.Commit(ctx))

	page, err := repo.Pull(ctx, first, syncservice.Cursor{HistoryID: mustHistory(t, repo)}, 20)
	if !errors.Is(err, ErrRepository) || len(page.Changes) != 0 {
		t.Fatalf("swapped resolve pull = %+v, %v", page, err)
	}
	if err := VerifyRecovery(ctx, conn); !errors.Is(err, ErrRecovery) {
		t.Fatalf("swapped resolve recovery = %v", err)
	}
}

func TestRepositoryPullBoundsEncodedResponsesAndContinues(t *testing.T) {
	ctx, repo, _, device, _ := conflictRepository(t)
	mustNoError(t, pushAccepted(ctx, repo, device, mutationProject("project", 0)))
	references := make([]string, syncservice.MaxReferences)
	for index := range references {
		references[index] = fmt.Sprintf("seed-%03d-%s", index, strings.Repeat("r", syncservice.MaxRecordIDBytes-9))
		mustNoError(t, pushAccepted(ctx, repo, device, mutationObservation(references[index], "project", "", nil, 0)))
	}
	for index := 0; index < 25; index++ {
		mutation := mutationObservation(fmt.Sprintf("large-%03d", index), "project", "", references, 0)
		mutation.Observation.Content = strings.Repeat("c", syncservice.MaxContentBytes)
		mutation.Observation.Title = strings.Repeat("t", syncservice.MaxFieldBytes)
		mutation.Observation.TopicKey = fmt.Sprintf("topic-%03d-%s", index, strings.Repeat("k", syncservice.MaxFieldBytes-10))
		mutation.Observation.Type = strings.Repeat("y", syncservice.MaxFieldBytes)
		mutation.Observation.Provenance = syncservice.Provenance{Producer: strings.Repeat("p", syncservice.MaxFieldBytes), SourceProvider: strings.Repeat("s", syncservice.MaxFieldBytes), SourceID: strings.Repeat("i", syncservice.MaxFieldBytes)}
		mustNoError(t, pushAccepted(ctx, repo, device, mutation))
	}
	history := mustHistory(t, repo)
	oldPage, err := repo.Pull(ctx, device, syncservice.Cursor{HistoryID: history, Position: 129}, 25)
	mustNoError(t, err)
	oldResponse, err := json.Marshal(syncapi.PullResponse{ProtocolVersion: syncapi.ProtocolVersion, HistoryID: history, Position: oldPage.Cursor.Position, Watermark: oldPage.Cursor.Watermark, HasMore: oldPage.HasMore, Changes: oldPage.Changes})
	mustNoError(t, err)
	if len(oldPage.Changes) == 25 {
		t.Logf("unbounded-shaped response = %d bytes", len(oldResponse))
	}
	seen := make([]int64, 0, 154)
	cursor := syncservice.Cursor{HistoryID: history}
	for {
		page, err := repo.Pull(ctx, device, cursor, 25)
		mustNoError(t, err)
		response, err := json.Marshal(syncapi.PullResponse{ProtocolVersion: syncapi.ProtocolVersion, HistoryID: history, Position: page.Cursor.Position, Watermark: page.Cursor.Watermark, HasMore: page.HasMore, Changes: page.Changes})
		mustNoError(t, err)
		if len(response) > syncapi.MaxPullResponseBytes {
			t.Fatalf("response = %d bytes, want at most %d", len(response), syncapi.MaxPullResponseBytes)
		}
		if _, err := syncapi.DecodePullResponse(response); err != nil || page.HasMore != (page.Cursor.Position < page.Cursor.Watermark) {
			t.Fatalf("response/page = %+v, %v", page, err)
		}
		for _, change := range page.Changes {
			seen = append(seen, change.Sequence)
		}
		if !page.HasMore {
			break
		}
		cursor = page.Cursor
	}
	if len(seen) != 154 {
		t.Fatalf("sequences = %d, want 154", len(seen))
	}
	for index, sequence := range seen {
		if sequence != int64(index+1) {
			t.Fatalf("sequence %d = %d", index, sequence)
		}
	}
}

func TestRepositoryPullRejectsCorruptCanonicalVersionAndTombstoneTime(t *testing.T) {
	ctx, repo, conn, device, _ := conflictRepository(t)
	mustNoError(t, pushAccepted(ctx, repo, device, mutationProject("project", 0)))
	target := mutationObservation("target", "project", "", nil, 0)
	mustNoError(t, pushAccepted(ctx, repo, device, target))
	deletedAt := time.Unix(1_700_000_000, 123_456_789).UTC()
	tombstone := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: target.RecordID, RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: deletedAt}}
	mustNoError(t, pushAccepted(ctx, repo, device, tombstone))
	history := mustHistory(t, repo)
	if _, err := repo.Pull(ctx, device, syncservice.Cursor{HistoryID: history}, 10); err != nil {
		t.Fatalf("nanosecond tombstone pull = %v", err)
	}
	if _, err := conn.Exec(ctx, "UPDATE tombstones SET deleted_at=deleted_at + interval '2 microseconds' WHERE record_id=$1", target.RecordID); err != nil {
		t.Fatal(err)
	}
	if page, err := repo.Pull(ctx, device, syncservice.Cursor{HistoryID: history}, 10); !errors.Is(err, ErrRepository) || len(page.Changes) != 0 {
		t.Fatalf("tampered tombstone pull = %+v, %v", page, err)
	}
	if _, err := conn.Exec(ctx, "UPDATE tombstones SET deleted_at=$1 WHERE record_id=$2", deletedAt, target.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "UPDATE mutations SET canonical_version=canonical_version+1 WHERE mutation_id=$1", tombstone.MutationID); err != nil {
		t.Fatal(err)
	}
	if page, err := repo.Pull(ctx, device, syncservice.Cursor{HistoryID: history}, 10); !errors.Is(err, ErrRepository) || len(page.Changes) != 0 {
		t.Fatalf("tampered canonical version pull = %+v, %v", page, err)
	}
}

func TestPullMutationRejectsTimestampOutsideDuration(t *testing.T) {
	deletedAt := time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)
	databaseTime := time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC)
	snapshot, err := json.Marshal(syncservice.Tombstone{DeletedAt: deletedAt})
	mustNoError(t, err)
	if _, ok := pullMutation(string(syncservice.RecordKindObservation), "target", string(syncservice.MutationTombstone), uuid.New(), 1, nil, snapshot, &databaseTime); ok {
		t.Fatal("overflowed tombstone timestamp accepted")
	}
}

func TestRepositoryPullAndRecoveryRejectAcceptedBaseMetadataCorruption(t *testing.T) {
	ctx, repo, conn, device, _ := conflictRepository(t)
	mustNoError(t, pushAccepted(ctx, repo, device, mutationProject("project", 0)))
	target := mutationObservation("target", "project", "", nil, 0)
	mustNoError(t, pushAccepted(ctx, repo, device, target))
	accepted := updateObservation(target, 1, "canonical")
	mustNoError(t, pushAccepted(ctx, repo, device, accepted))
	if _, err := conn.Exec(ctx, "UPDATE mutations SET base_version=0 WHERE mutation_id=$1", accepted.MutationID); err != nil {
		t.Fatal(err)
	}
	history := mustHistory(t, repo)
	if page, err := repo.Pull(ctx, device, syncservice.Cursor{HistoryID: history}, 10); !errors.Is(err, ErrRepository) || len(page.Changes) != 0 {
		t.Fatalf("corrupt accepted base pull = %+v, %v", page, err)
	}
	if err := VerifyRecovery(ctx, conn); !errors.Is(err, ErrRecovery) {
		t.Fatalf("corrupt accepted base recovery = %v", err)
	}
}

func TestRepositoryPullRejectsUnsafeCursorDeviceOwnerAndGap(t *testing.T) {
	ctx, repo, conn, device, _ := conflictRepository(t)
	mustNoError(t, pushAccepted(ctx, repo, device, mutationProject("project", 0)))
	mustNoError(t, pushAccepted(ctx, repo, device, mutationProject("project-two", 0)))
	history := mustHistory(t, repo)
	_, err := repo.OwnerState(ctx)
	mustNoError(t, err)
	effects := mutationEffects(t, ctx, conn)
	revoked, err := repo.IssueDevice(ctx, "revoked")
	mustNoError(t, err)
	mustNoError(t, repo.RevokeDevice(ctx, revoked.ID))
	wrongOwner, err := NewRepository(conn, uuid.New())
	mustNoError(t, err)
	for _, test := range []struct {
		repo   *Repository
		device uuid.UUID
		cursor syncservice.Cursor
		err    error
	}{
		{repo, uuid.New(), syncservice.Cursor{HistoryID: history}, ErrUnauthenticated},
		{repo, revoked.ID, syncservice.Cursor{HistoryID: history}, ErrUnauthenticated},
		{repo, device, syncservice.Cursor{HistoryID: history, Position: 3}, ErrRepository},
		{repo, device, syncservice.Cursor{HistoryID: history, Watermark: 3}, ErrRepository},
		{repo, device, syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 1}, ErrRepository},
		{repo, device, syncservice.Cursor{HistoryID: history, Position: 2}, nil},
		{repo, device, syncservice.Cursor{HistoryID: uuid.NewString()}, ErrRepository},
		{wrongOwner, device, syncservice.Cursor{HistoryID: history}, ErrRepository},
	} {
		_, got := test.repo.Pull(ctx, test.device, test.cursor, 1)
		if !errors.Is(got, test.err) {
			t.Fatalf("Pull(%+v) = %v, want %v", test.cursor, got, test.err)
		}
	}
	var snapshot []byte
	mustNoError(t, conn.QueryRow(ctx, "SELECT v.snapshot FROM changes c JOIN record_versions v ON v.id=c.version_id WHERE c.seq=1").Scan(&snapshot))
	if _, err := conn.Exec(ctx, "UPDATE record_versions SET snapshot='null' WHERE id=(SELECT version_id FROM changes WHERE seq=1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Pull(ctx, device, syncservice.Cursor{HistoryID: history}, 1); !errors.Is(err, ErrRepository) {
		t.Fatalf("corrupt snapshot pull = %v", err)
	}
	var one int
	mustNoError(t, conn.QueryRow(ctx, "SELECT 1").Scan(&one))
	if _, err := conn.Exec(ctx, "UPDATE record_versions SET snapshot=$1 WHERE id=(SELECT version_id FROM changes WHERE seq=1)", snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "DELETE FROM changes WHERE seq=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Pull(ctx, device, syncservice.Cursor{HistoryID: history}, 1); !errors.Is(err, ErrRepository) {
		t.Fatalf("gapped pull = %v", err)
	}
	_, err = repo.OwnerState(ctx)
	if got := mutationEffects(t, ctx, conn); got != effects-1 || err == nil {
		t.Fatalf("pull side effects = %d/%v", got, err)
	}
}

func TestRepositoryPullRetainsResolveIDsAndLifecyclePayloads(t *testing.T) {
	ctx, repo, conn, first, second := conflictRepository(t)
	project := mutationProject("project", 0)
	holder := mutationObservation("holder", "project", "", nil, 0)
	holder.Observation.TopicKey = "taken"
	target := mutationObservation("target", "project", "", nil, 0)
	if _, err := repo.Push(ctx, first, []syncservice.Mutation{project, holder, target}); err != nil {
		t.Fatal(err)
	}
	mustNoError(t, pushAccepted(ctx, repo, first, updateObservation(target, 1, "canonical")))
	for range 2 {
		if got, err := repo.Push(ctx, second, []syncservice.Mutation{updateObservation(target, 1, "competing")}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
			t.Fatalf("conflict = %+v, %v", got, err)
		}
	}
	var ids []uuid.UUID
	mustNoError(t, conn.QueryRow(ctx, "SELECT array_agg(conflict_id ORDER BY created_seq) FROM observation_conflicts WHERE observation_id='target'").Scan(&ids))
	accepted := updateObservation(target, 2, "resolved")
	accepted.Kind, accepted.Resolution = syncservice.MutationResolve, &syncservice.Resolution{ConflictIDs: []string{ids[1].String(), ids[0].String()}, Observation: accepted.Observation}
	accepted.Observation = nil
	mustNoError(t, pushAccepted(ctx, repo, first, accepted))
	competing := updateObservation(target, 3, "conflict resolve")
	competing.Observation.TopicKey = "taken"
	competing.Kind, competing.Resolution = syncservice.MutationResolve, &syncservice.Resolution{ConflictIDs: []string{ids[0].String(), ids[1].String()}, Observation: competing.Observation}
	competing.Observation = nil
	if got, err := repo.Push(ctx, second, []syncservice.Mutation{competing}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
		t.Fatalf("resolve conflict = %+v, %v", got, err)
	}
	archive := updateObservation(target, 3, "archived")
	archive.Kind, archive.Observation.Lifecycle = syncservice.MutationArchive, syncservice.LifecycleArchived
	mustNoError(t, pushAccepted(ctx, repo, first, archive))
	tombstone := syncservice.Mutation{MutationID: uuid.NewString(), RecordID: "target", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 4, Tombstone: &syncservice.Tombstone{DeletedAt: time.Now().UTC()}}
	mustNoError(t, pushAccepted(ctx, repo, first, tombstone))
	page, err := repo.Pull(ctx, first, syncservice.Cursor{HistoryID: mustHistory(t, repo)}, 20)
	mustNoError(t, err)
	if page.HasMore || len(page.Changes) != 10 {
		t.Fatalf("page = %+v", page)
	}
	for _, change := range page.Changes {
		if err := syncservice.ValidateMutation(change.Mutation); err != nil {
			t.Fatalf("change %d = %v", change.Sequence, err)
		}
	}
	if got := page.Changes[6].Mutation.Resolution.ConflictIDs; !reflect.DeepEqual(got, []string{ids[1].String(), ids[0].String()}) || !reflect.DeepEqual(page.Changes[7].Mutation.Resolution.ConflictIDs, []string{ids[0].String(), ids[1].String()}) || page.Changes[8].Mutation.Observation.Lifecycle != syncservice.LifecycleArchived || page.Changes[9].Mutation.Tombstone.DeletedAt.IsZero() {
		t.Fatalf("pulled lifecycle/resolve payload = %+v", page.Changes)
	}
	var nonResolve int
	mustNoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM mutations WHERE kind<>'resolve' AND resolution_conflict_ids IS NOT NULL").Scan(&nonResolve))
	if nonResolve != 0 {
		t.Fatalf("non-resolve IDs = %d", nonResolve)
	}
	var original []uuid.UUID
	mustNoError(t, conn.QueryRow(ctx, "SELECT resolution_conflict_ids FROM mutations WHERE kind='resolve' AND disposition='accepted'").Scan(&original))
	for _, ids := range [][]uuid.UUID{{uuid.New()}, {original[0], original[0]}, {original[0]}} {
		if _, err := conn.Exec(ctx, "UPDATE mutations SET resolution_conflict_ids=$1 WHERE kind='resolve' AND disposition='accepted'", ids); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Pull(ctx, first, syncservice.Cursor{HistoryID: mustHistory(t, repo)}, 20); !errors.Is(err, ErrRepository) {
			t.Fatalf("corrupt resolve pull = %v", err)
		}
		if err := VerifyRecovery(ctx, conn); !errors.Is(err, ErrRecovery) {
			t.Fatalf("corrupt resolve recovery = %v", err)
		}
		if _, err := conn.Exec(ctx, "UPDATE mutations SET resolution_conflict_ids=$1 WHERE kind='resolve' AND disposition='accepted'", original); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepositoryResolveRejectsCaseDuplicateBeforeTopicEffects(t *testing.T) {
	ctx, repo, conn, device, second := conflictRepository(t)
	holder, target := mutationObservation("holder", "project", "", nil, 0), mutationObservation("target", "project", "", nil, 0)
	holder.Observation.TopicKey = "taken"
	mustNoError(t, pushAccepted(ctx, repo, device, mutationProject("project", 0)))
	mustNoError(t, pushAccepted(ctx, repo, device, holder))
	mustNoError(t, pushAccepted(ctx, repo, device, target))
	mustNoError(t, pushAccepted(ctx, repo, device, updateObservation(target, 1, "canonical")))
	if got, err := repo.Push(ctx, second, []syncservice.Mutation{updateObservation(target, 1, "competing")}); err != nil || got[0].Disposition != syncservice.DispositionConflict {
		t.Fatalf("seed conflict = %+v, %v", got, err)
	}
	var id string
	mustNoError(t, conn.QueryRow(ctx, "SELECT conflict_id FROM observation_conflicts WHERE observation_id='target'").Scan(&id))
	bad := updateObservation(target, 2, "resolve")
	bad.Observation.TopicKey = "taken"
	bad.Kind, bad.Resolution = syncservice.MutationResolve, &syncservice.Resolution{ConflictIDs: []string{strings.ToUpper(id), id}, Observation: bad.Observation}
	bad.Observation = nil
	before := mutationEffects(t, ctx, conn)
	if got, err := repo.Push(ctx, device, []syncservice.Mutation{bad}); err != nil || got[0].Disposition != syncservice.DispositionRejected || got[0].Code != "invalid_prerequisite" || mutationEffects(t, ctx, conn) != before {
		t.Fatalf("case duplicate = %+v, %v", got, err)
	}
}

func mustHistory(t *testing.T, repo *Repository) string {
	t.Helper()
	state, err := repo.OwnerState(context.Background())
	mustNoError(t, err)
	return state.HistoryID.String()
}

func TestRepositoryConflictRollbackWithoutSequenceGap(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	mustNoError(t, Migrate(ctx, conn))
	db := &changeFailDB{conn: conn}
	repo, err := NewRepository(db, uuid.New())
	mustNoError(t, err)
	mustNoError(t, repo.EnsureOwner(ctx))
	first, err := repo.IssueDevice(ctx, "first")
	mustNoError(t, err)
	second, err := repo.IssueDevice(ctx, "second")
	mustNoError(t, err)
	project, target := mutationProject("project", 0), mutationObservation("target", "project", "", nil, 0)
	if _, err := repo.Push(ctx, first.ID, []syncservice.Mutation{project, target}); err != nil {
		t.Fatal(err)
	}
	mustNoError(t, pushAccepted(ctx, repo, first.ID, updateObservation(target, 1, "canonical")))
	before, err := repo.OwnerState(ctx)
	mustNoError(t, err)
	db.fail = true
	got, err := repo.Push(ctx, second.ID, []syncservice.Mutation{updateObservation(target, 1, "competing")})
	db.fail = false
	if !errors.Is(err, ErrRepository) || got != nil {
		t.Fatalf("conflict persistence failure = %+v, %v", got, err)
	}
	after, err := repo.OwnerState(ctx)
	if err != nil || after.NextSeq != before.NextSeq {
		t.Fatalf("sequence after rollback = %d, %v; want %d", after.NextSeq, err, before.NextSeq)
	}
	if got, err := repo.Push(ctx, second.ID, []syncservice.Mutation{updateObservation(target, 1, "competing")}); err != nil || got[0].Sequence == nil || *got[0].Sequence != before.NextSeq {
		t.Fatalf("conflict retry = %+v, %v", got, err)
	}
}

func conflictRepository(t *testing.T) (context.Context, *Repository, *pgx.Conn, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, conn := context.Background(), testConn(t)
	mustNoError(t, Migrate(ctx, conn))
	repo, err := NewRepository(conn, uuid.New())
	mustNoError(t, err)
	mustNoError(t, repo.EnsureOwner(ctx))
	first, err := repo.IssueDevice(ctx, "first")
	mustNoError(t, err)
	second, err := repo.IssueDevice(ctx, "second")
	mustNoError(t, err)
	return ctx, repo, conn, first.ID, second.ID
}

func updateObservation(m syncservice.Mutation, base int64, content string) syncservice.Mutation {
	m.MutationID, m.Kind, m.BaseVersion = uuid.NewString(), syncservice.MutationUpdate, base
	o := *m.Observation
	o.Content, o.UpdatedAt = content, time.Now().UTC()
	m.Observation = &o
	return m
}

func pushAccepted(ctx context.Context, repo *Repository, device uuid.UUID, mutation syncservice.Mutation) error {
	got, err := repo.Push(ctx, device, []syncservice.Mutation{mutation})
	if err != nil || len(got) != 1 || got[0].Disposition != syncservice.DispositionAccepted {
		return errors.New("mutation was not accepted")
	}
	return nil
}

func mutationEffects(t *testing.T, ctx context.Context, conn *pgx.Conn) int {
	t.Helper()
	var total int
	if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM mutations)+(SELECT count(*) FROM record_versions)+(SELECT count(*) FROM changes)+(SELECT count(*) FROM projects)+(SELECT count(*) FROM sessions)+(SELECT count(*) FROM observations)+(SELECT count(*) FROM observation_references)").Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

func mustNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mutationProject(id string, base int64) syncservice.Mutation {
	return syncservice.Mutation{MutationID: uuid.NewString(), RecordID: id, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, BaseVersion: base, Project: &syncservice.Project{ID: id}}
}
func mutationSession(id, project string, base int64) syncservice.Mutation {
	return syncservice.Mutation{MutationID: uuid.NewString(), RecordID: id, RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, BaseVersion: base, Session: &syncservice.Session{ID: id, ProjectID: project}}
}
func mutationObservation(id, project, session string, references []string, base int64) syncservice.Mutation {
	now := time.Now().UTC()
	return syncservice.Mutation{MutationID: uuid.NewString(), RecordID: id, RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationCreate, BaseVersion: base, Observation: &syncservice.Observation{ID: id, ProjectID: project, SessionID: session, Scope: "project", Type: "test", Content: "content", References: references, Provenance: syncservice.Provenance{Producer: "test"}, Lifecycle: syncservice.LifecycleActive, Review: syncservice.ReviewClear, CreatedAt: now, UpdatedAt: now}}
}
