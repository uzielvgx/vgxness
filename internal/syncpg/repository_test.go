package syncpg

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func TestRepositoryPushAcceptsProjectSessionObservation(t *testing.T) {
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
	if len(results) != 4 {
		t.Fatalf("results = %d, want 4", len(results))
	}
	for i, result := range results {
		if result.Disposition != syncservice.DispositionAccepted || result.Sequence == nil || result.Version != 1 || *result.Sequence != int64(i+1) {
			t.Fatalf("result[%d] = %+v, want accepted sequence/version", i, result)
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
	if err != nil || result[0].Version != 2 || result[0].Sequence == nil || *result[0].Sequence != 5 {
		t.Fatalf("update = %+v, %v", result, err)
	}
	var title, version string
	if err := conn.QueryRow(ctx, "SELECT title, version::text FROM observations WHERE id='observation'").Scan(&title, &version); err != nil || title != "updated" || version != "2" {
		t.Fatalf("canonical update = %q/%q, %v", title, version, err)
	}
	var histories, changes int
	if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM record_versions), (SELECT count(*) FROM changes)").Scan(&histories, &changes); err != nil || histories != 5 || changes != 5 {
		t.Fatalf("update history/change = %d/%d, %v", histories, changes, err)
	}
}

func TestRepositoryMutationReplaysEqualHashWithoutEffects(t *testing.T) {
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
	mutation := mutationProject("project", 0)
	first, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutation})
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutation})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Sequence == nil || again[0].Disposition != syncservice.DispositionPreviouslyAccepted || again[0].Sequence == nil || *again[0].Sequence != *first[0].Sequence {
		t.Fatalf("replay = %+v, want original accepted result", again[0])
	}
	var changes, mutations int
	if err := conn.QueryRow(ctx, "SELECT count(*), (SELECT count(*) FROM mutations) FROM changes").Scan(&changes, &mutations); err != nil || changes != 1 || mutations != 1 {
		t.Fatalf("effects = %d/%d, %v; want 1/1", changes, mutations, err)
	}
}

func TestRepositoryMutationRejectsMismatchAndUnsupportedWithoutEffects(t *testing.T) {
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
	mutation := mutationProject("project", 0)
	if _, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutation}); err != nil {
		t.Fatal(err)
	}
	mismatch := mutation
	mismatch.Project = &syncservice.Project{ID: "other"}
	mismatch.RecordID = "other"
	results, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mismatch})
	if err != nil {
		t.Fatal(err)
	}
	unsupported := mutationObservation("archived", "project", "", nil, 1)
	unsupported.Kind = syncservice.MutationArchive
	unsupported.Observation.Lifecycle = syncservice.LifecycleArchived
	unsupported.Observation.Review = syncservice.ReviewClear
	unsupportedResult, err := repo.Push(ctx, device.ID, []syncservice.Mutation{unsupported})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Disposition != syncservice.DispositionRejected || results[0].Code != "mutation_id_hash_mismatch" || unsupportedResult[0].Disposition != syncservice.DispositionRejected {
		t.Fatalf("rejections = %+v / %+v", results[0], unsupportedResult[0])
	}
	var changes, mutations int
	if err := conn.QueryRow(ctx, "SELECT count(*), (SELECT count(*) FROM mutations) FROM changes").Scan(&changes, &mutations); err != nil || changes != 1 || mutations != 1 {
		t.Fatalf("effects = %d/%d, %v; want 1/1", changes, mutations, err)
	}
}

func TestRepositoryMutationRollsBackWithoutAcknowledgementOrGap(t *testing.T) {
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
	if _, err := conn.Exec(ctx, "CREATE FUNCTION fail_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fail'; END $$"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE TRIGGER fail_change BEFORE INSERT ON changes FOR EACH ROW EXECUTE FUNCTION fail_change()"); err != nil {
		t.Fatal(err)
	}
	failed := mutationProject("project", 0)
	if _, err := repo.Push(ctx, device.ID, []syncservice.Mutation{failed}); !errors.Is(err, ErrRepository) {
		t.Fatalf("forced failure = %v, want ErrRepository", err)
	}
	var mutations, versions, changes, projects int
	if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM mutations), (SELECT count(*) FROM record_versions), (SELECT count(*) FROM changes), (SELECT count(*) FROM projects)").Scan(&mutations, &versions, &changes, &projects); err != nil || mutations != 0 || versions != 0 || changes != 0 || projects != 0 {
		t.Fatalf("rollback effects = %d/%d/%d/%d, %v", mutations, versions, changes, projects, err)
	}
	if _, err := conn.Exec(ctx, "DROP TRIGGER fail_change ON changes"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "DROP FUNCTION fail_change()"); err != nil {
		t.Fatal(err)
	}
	good := mutationProject("project", 0)
	result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{good})
	if err != nil {
		t.Fatal(err)
	}
	if result[0].Sequence == nil || *result[0].Sequence != 1 {
		t.Fatalf("good = %+v, want sequence 1", result[0])
	}
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM mutations").Scan(&mutations); err != nil || mutations != 1 {
		t.Fatalf("mutations = %d, %v; want 1", mutations, err)
	}
}

func TestRepositoryMutationRejectsDeferredPrerequisitesWithoutEffects(t *testing.T) {
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
	seed := []syncservice.Mutation{mutationProject("one", 0), mutationProject("two", 0), mutationSession("session", "one", 0), mutationObservation("reference", "two", "", nil, 0), mutationObservation("topic", "one", "", nil, 0)}
	seed[4].Observation.TopicKey = "shared"
	if _, err := repo.Push(ctx, device.ID, seed); err != nil {
		t.Fatal(err)
	}
	before := mutationEffectCount(t, ctx, conn)
	stale := seed[4]
	stale.MutationID, stale.Kind, stale.BaseVersion = uuid.NewString(), syncservice.MutationUpdate, 2
	collision := mutationObservation("collision", "one", "", nil, 0)
	collision.Observation.TopicKey = "shared"
	missingSession := mutationObservation("missing-session", "one", "missing", nil, 0)
	crossSession := mutationObservation("cross-session", "two", "session", nil, 0)
	crossReference := mutationObservation("cross-reference", "one", "", []string{"reference"}, 0)
	for _, test := range []struct {
		mutation syncservice.Mutation
		code     string
	}{{stale, "stale_base"}, {collision, "topic_collision"}, {missingSession, "invalid_prerequisite"}, {crossSession, "invalid_prerequisite"}, {crossReference, "invalid_prerequisite"}} {
		result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{test.mutation})
		if err != nil || result[0].Disposition != syncservice.DispositionRejected || result[0].Code != test.code {
			t.Fatalf("rejection = %+v, %v; want %s", result, err, test.code)
		}
	}
	if after := mutationEffectCount(t, ctx, conn); after != before {
		t.Fatalf("effects after deferred rejections = %d, want %d", after, before)
	}
	if err := repo.RevokeDevice(ctx, device.ID); err != nil {
		t.Fatal(err)
	}
	result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutationProject("revoked", 0)})
	if err != nil || result[0].Disposition != syncservice.DispositionRejected || result[0].Code != "revoked" {
		t.Fatalf("revoked rejection = %+v, %v", result, err)
	}
	if after := mutationEffectCount(t, ctx, conn); after != before {
		t.Fatalf("effects after revoked rejection = %d, want %d", after, before)
	}
}

func TestRepositoryPushRejectsCorruptOwnerSequenceWithoutEffects(t *testing.T) {
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
		t.Fatalf("corrupt owner push = %v, want ErrRepository", err)
	}
	if effects := mutationEffectCount(t, ctx, conn); effects != 0 {
		t.Fatalf("corrupt owner effects = %d, want 0", effects)
	}
}

func TestRepositoryMutationRejectsDependentProjectMovesWithoutEffects(t *testing.T) {
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
	seed := []syncservice.Mutation{mutationProject("one", 0), mutationProject("two", 0), mutationSession("session", "one", 0), mutationObservation("target", "one", "", nil, 0), mutationObservation("source", "one", "", []string{"target"}, 0), mutationObservation("dependent", "one", "session", nil, 0)}
	if _, err := repo.Push(ctx, device.ID, seed); err != nil {
		t.Fatal(err)
	}
	before := mutationEffectCount(t, ctx, conn)
	observationMove := seed[3]
	observationMove.MutationID, observationMove.Kind, observationMove.BaseVersion = uuid.NewString(), syncservice.MutationUpdate, 1
	observationMove.Observation.ProjectID = "two"
	sessionMove := seed[2]
	sessionMove.MutationID, sessionMove.Kind, sessionMove.BaseVersion = uuid.NewString(), syncservice.MutationUpdate, 1
	sessionMove.Session.ProjectID = "two"
	for _, mutation := range []syncservice.Mutation{observationMove, sessionMove} {
		result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{mutation})
		if err != nil || result[0].Disposition != syncservice.DispositionRejected || result[0].Code != "invalid_prerequisite" {
			t.Fatalf("move = %+v, %v", result, err)
		}
	}
	if effects := mutationEffectCount(t, ctx, conn); effects != before {
		t.Fatalf("move effects = %d, want %d", effects, before)
	}
	var observationProject, sessionProject string
	if err := conn.QueryRow(ctx, "SELECT (SELECT project_id FROM observations WHERE id='target'), (SELECT project_id FROM sessions WHERE id='session')").Scan(&observationProject, &sessionProject); err != nil || observationProject != "one" || sessionProject != "one" {
		t.Fatalf("canonical projects = %q/%q, %v", observationProject, sessionProject, err)
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
	before := mutationEffectCount(t, ctx, conn)
	state, err := repo.OwnerState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() error {
		for _, statement := range []string{"DROP POLICY IF EXISTS fail_prerequisite_select ON projects", "ALTER TABLE projects NO FORCE ROW LEVEL SECURITY", "ALTER TABLE projects DISABLE ROW LEVEL SECURITY", "DROP FUNCTION IF EXISTS fail_prerequisite()"} {
			if _, err := conn.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	for _, statement := range []string{"CREATE FUNCTION fail_prerequisite() RETURNS boolean LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fail'; END $$", "ALTER TABLE projects ENABLE ROW LEVEL SECURITY", "ALTER TABLE projects FORCE ROW LEVEL SECURITY", "CREATE POLICY fail_prerequisite_select ON projects FOR SELECT USING (fail_prerequisite())"} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	failed := mutationObservation("observation", "project", "", nil, 0)
	result, err := repo.Push(ctx, device.ID, []syncservice.Mutation{failed})
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, ErrRepository) || result != nil {
		t.Fatalf("prerequisite failure = %+v, %v; want ErrRepository and nil results", result, err)
	}
	if effects := mutationEffectCount(t, ctx, conn); effects != before {
		t.Fatalf("failure effects = %d, want %d", effects, before)
	}
	stateAfter, err := repo.OwnerState(ctx)
	if err != nil || stateAfter.NextSeq != state.NextSeq {
		t.Fatalf("failure next sequence = %d, %v; want %d", stateAfter.NextSeq, err, state.NextSeq)
	}
	result, err = repo.Push(ctx, device.ID, []syncservice.Mutation{failed})
	if err != nil || len(result) != 1 || result[0].Sequence == nil || *result[0].Sequence != state.NextSeq {
		t.Fatalf("restored push = %+v, %v", result, err)
	}
}

func mutationEffectCount(t *testing.T, ctx context.Context, conn *pgx.Conn) int {
	t.Helper()
	var total int
	if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM mutations) + (SELECT count(*) FROM record_versions) + (SELECT count(*) FROM changes) + (SELECT count(*) FROM projects) + (SELECT count(*) FROM sessions) + (SELECT count(*) FROM observations) + (SELECT count(*) FROM observation_references)").Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
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
