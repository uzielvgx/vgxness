package syncpg

import (
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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
	stale := seed[4]
	stale.MutationID, stale.Kind, stale.BaseVersion = uuid.NewString(), syncservice.MutationUpdate, 2
	collision := mutationObservation("collision", "one", "", nil, 0)
	collision.Observation.TopicKey = "shared"
	crossSession := mutationObservation("cross-session", "two", "session", nil, 0)
	crossReference := mutationObservation("cross-reference", "one", "", []string{"reference"}, 0)
	observationMove := seed[5]
	observationMove.MutationID, observationMove.Kind, observationMove.BaseVersion, observationMove.Observation.ProjectID = uuid.NewString(), syncservice.MutationUpdate, 1, "two"
	sessionMove := seed[2]
	sessionMove.MutationID, sessionMove.Kind, sessionMove.BaseVersion, sessionMove.Session.ProjectID = uuid.NewString(), syncservice.MutationUpdate, 1, "two"
	for _, test := range []struct {
		mutation syncservice.Mutation
		code     string
	}{{stale, "stale_base"}, {collision, "topic_collision"}, {mutationObservation("missing", "one", "missing", nil, 0), "invalid_prerequisite"}, {crossSession, "invalid_prerequisite"}, {crossReference, "invalid_prerequisite"}, {observationMove, "invalid_prerequisite"}, {sessionMove, "invalid_prerequisite"}} {
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
