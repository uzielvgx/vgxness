package memory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/testutil"
)

var fixedTime = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "memory.db"), func() time.Time { return fixedTime })
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func openPath(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path, nil)
	testutil.NoError(t, err)
	return store
}
func mustSave(t *testing.T, store *Store, item Observation) Observation {
	t.Helper()
	saved, err := store.Save(context.Background(), item)
	testutil.NoError(t, err)
	return saved
}

func observation(id, project, content string) Observation {
	return Observation{ID: id, Project: project, Scope: ScopeProject, Type: "learning", Content: content, Provenance: Provenance{Producer: "test"}, State: StateActive}
}

func TestMigrate_FreshRepeatedAndRestartSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	mustSave(t, store, observation("obs-1", "project-a", "restart token"))
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 1, "health=%d %v", version, err)
	_ = store.Close()
	store = openPath(t, path)
	defer store.Close()
	got, err := store.Search(context.Background(), Search{Query: "restart", Project: "project-a"})
	testutil.Require(t, err == nil && len(got) == 1 && got[0].ID == "obs-1", "restart lost data: %+v %v", got, err)
}

func TestMigrate_RejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	_ = store.Close()
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`PRAGMA user_version=99`)
	testutil.NoError(t, err)
	_ = db.Close()
	_, err = Open(context.Background(), path, nil)
	testutil.Require(t, errors.Is(err, ErrMigration), "expected newer-schema rejection, got %v", err)
}

func TestHealthFile_RejectsNewerSchemaWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	testutil.NoError(t, store.Close())
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`PRAGMA user_version=99`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	_, err = HealthFile(context.Background(), path)
	testutil.Require(t, errors.Is(err, ErrMigration), "expected newer-schema rejection, got %v", err)
	db, err = sql.Open("sqlite", path)
	testutil.NoError(t, err)
	defer db.Close()
	var version int
	testutil.NoError(t, db.QueryRow(`PRAGMA user_version`).Scan(&version))
	testutil.Require(t, version == 99, "health mutated schema version to %d", version)
}

func TestOpen_RejectsDatabaseSymlinkWithoutMigratingTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target.db")
	testutil.NoError(t, os.WriteFile(target, nil, 0o600))
	link := filepath.Join(t.TempDir(), "memory.db")
	testutil.NoError(t, os.Symlink(target, link))
	_, err := Open(context.Background(), link, nil)
	testutil.Require(t, err != nil, "expected database symlink rejection")
	db, err := sql.Open("sqlite", target)
	testutil.NoError(t, err)
	defer db.Close()
	var version int
	testutil.NoError(t, db.QueryRow(`PRAGMA user_version`).Scan(&version))
	testutil.Require(t, version == 0, "symlink target migrated to version %d", version)
}

func TestMigrate_RollsBackAndReportsVersion(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "memory.db"))
	testutil.NoError(t, err)
	defer db.Close()
	err = applyMigrations(context.Background(), db, []migration{{version: 1, sql: `CREATE TABLE partial(id); CREATE VIRTUAL TABLE broken USING unavailable_module;`}})
	testutil.Require(t, errors.Is(err, ErrMigration) && strings.Contains(err.Error(), "version 1"), "unexpected migration error: %v", err)
	var count int
	err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='partial'`).Scan(&count)
	testutil.Require(t, err == nil && count == 0, "migration was not rolled back: count=%d err=%v", count, err)
}

func TestStore_SaveUpdateScopedTopicUpsert(t *testing.T) {
	store := openTestStore(t)
	first := observation("obs-1", "project-a", "first topic")
	first.TopicKey = "architecture/store"
	saved, err := store.Save(context.Background(), first)
	testutil.NoError(t, err)
	second := first
	second.ID = "ignored"
	second.Content = "second topic"
	upserted, err := store.Save(context.Background(), second)
	testutil.Require(t, err == nil && upserted.ID == saved.ID && upserted.CreatedAt.Equal(saved.CreatedAt), "topic upsert changed identity: %+v %v", upserted, err)
	fixedTime = fixedTime.Add(time.Second)
	upserted.Content = "explicit update"
	updated, err := store.Update(context.Background(), upserted)
	testutil.Require(t, err == nil && updated.UpdatedAt.After(updated.CreatedAt), "update failed: %+v %v", updated, err)
}

func TestStore_RejectsInvalidDuplicateBoundaryAndCancelledWrites(t *testing.T) {
	store := openTestStore(t)
	_, err := store.Save(context.Background(), Observation{})
	testutil.Require(t, errors.Is(err, ErrInvalid), "expected invalid error, got %v", err)
	item := observation("obs-1", "project-a", "atomic token")
	item.TopicKey = "shared"
	mustSave(t, store, item)
	duplicate := observation("obs-1", "project-a", "duplicate")
	_, err = store.Save(context.Background(), duplicate)
	testutil.Require(t, errors.Is(err, ErrConflict), "expected duplicate conflict, got %v", err)
	cross := observation("obs-2", "project-b", "cross")
	cross.TopicKey = "shared"
	_, err = store.Save(context.Background(), cross)
	testutil.Require(t, errors.Is(err, ErrConflict), "expected boundary conflict, got %v", err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Save(ctx, observation("obs-3", "project-a", "cancelled"))
	testutil.Require(t, errors.Is(err, context.Canceled), "expected cancellation, got %v", err)
}

func TestStore_MetadataRoundTrip(t *testing.T) {
	store := openTestStore(t)
	review := fixedTime.Add(24 * time.Hour)
	item := observation("obs-meta", "project-a", "metadata token")
	item.Session = "session-1"
	item.Provenance = Provenance{Producer: "agent", SourceProvider: "engram", SourceID: "external-1"}
	item.ReviewAfter = &review
	mustSave(t, store, item)
	got, err := store.Search(context.Background(), Search{Query: "metadata", Project: "project-a"})
	testutil.Require(t, err == nil && len(got) == 1 && got[0].Session == item.Session && got[0].Provenance == item.Provenance && got[0].ReviewAfter.Equal(review), "metadata mismatch: %+v %v", got, err)
}

func TestStore_RejectsLifecycleAndReferenceViolations(t *testing.T) {
	store := openTestStore(t)
	target := mustSave(t, store, observation("target", "project-a", "target token"))
	valid := observation("source", "project-a", "source token")
	valid.References = []string{target.ID}
	mustSave(t, store, valid)
	bad := observation("bad", "project-a", "bad token")
	bad.References = []string{"missing"}
	_, err := store.Save(context.Background(), bad)
	testutil.Require(t, errors.Is(err, ErrNotFound), "expected reference rejection, got %v", err)
	target.State = StateArchived
	archived, err := store.Update(context.Background(), target)
	testutil.NoError(t, err)
	archived.State = StateNeedsReview
	_, err = store.Update(context.Background(), archived)
	testutil.Require(t, errors.Is(err, ErrInvalid), "expected lifecycle rejection, got %v", err)
}

func TestStore_SearchFiltersAndStableTies(t *testing.T) {
	store := openTestStore(t)
	for _, item := range []Observation{observation("a", "project-a", "alpha shared"), observation("b", "project-a", "alpha shared"), observation("c", "project-b", "alpha shared")} {
		mustSave(t, store, item)
	}
	filter := Search{Query: "alpha", Project: "project-a", Scope: ScopeProject, Types: []string{"learning"}, States: []State{StateActive}}
	first, err := store.Search(context.Background(), filter)
	testutil.Require(t, err == nil && len(first) == 2 && first[0].ID == "a" && first[1].ID == "b", "unexpected filtered order: %+v %v", first, err)
	second, _ := store.Search(context.Background(), filter)
	testutil.Require(t, first[0].ID == second[0].ID && first[1].ID == second[1].ID, "search order is not stable")
}

func TestStore_SearchRejectsUnsafeInputAndCancellation(t *testing.T) {
	store := openTestStore(t)
	for _, search := range []Search{{Project: "p"}, {Query: `"`, Project: "p"}, {Query: "ok", Project: "p", Scope: "global"}} {
		_, err := store.Search(context.Background(), search)
		testutil.Require(t, errors.Is(err, ErrInvalid) && !strings.Contains(strings.ToUpper(err.Error()), "SELECT"), "unsafe error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Search(ctx, Search{Query: "ok", Project: "p"})
	testutil.Require(t, errors.Is(err, context.Canceled), "expected cancellation, got %v", err)
}

func TestEnvironmentIsolation_NoAmbientHomeOrNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := openTestStore(t)
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 1, "isolated health=%d %v", version, err)
}
