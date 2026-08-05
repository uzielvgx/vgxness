package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestMemoryRuntime_LiteralV1UpgradeRestartPreservesDataAndTitles(t *testing.T) {
	const schemaV1Hash = "966bbade809fb4e68767c87e2e8aa1a96c35b0dd20f07a67108f8bb28baeb364"
	testutil.Require(t, fmt.Sprintf("%x", sha256.Sum256([]byte(schemaV1))) == schemaV1Hash, "migration 001 changed")
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(schemaV1 + ` PRAGMA user_version=1; INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at) VALUES('old','p','project','learning','literal old token','legacy','active',1,1); INSERT INTO observations_fts(id,content) VALUES('old','literal old token');`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())

	store := openPath(t, path)
	titled := observation("new", "p", "new restart token")
	titled.Title = "Durable title"
	mustSave(t, store, titled)
	mustSave(t, store, observation("untitled", "p", "untitled restart token"))
	testutil.NoError(t, store.Close())
	store = openPath(t, path)
	defer store.Close()
	for id, title := range map[string]string{"old": "", "new": "Durable title", "untitled": ""} {
		got, err := store.Get(context.Background(), id, "p", ScopeProject)
		testutil.Require(t, err == nil && got.ID == id && got.Title == title && (id != "old" || got.Content == "literal old token"), "get %s: %+v %v", id, got, err)
	}
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 9, "health=%d %v", version, err)
}

func TestMigrate_FreshRepeatedAndRestartSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	mustSave(t, store, observation("obs-1", "project-a", "restart token"))
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 9, "health=%d %v", version, err)
	_ = store.Close()
	store = openPath(t, path)
	defer store.Close()
	got, err := store.Search(context.Background(), Search{Query: "restart", Project: "project-a"})
	testutil.Require(t, err == nil && len(got) == 1 && got[0].ID == "obs-1", "restart lost data: %+v %v", got, err)
}

func TestResolveProject_AdoptsLegacyOnceAndSeparatesSameNamedWorkspaces(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	mustSave(t, store, observation("legacy", "same", "legacy workspace memory"))

	root := t.TempDir()
	first := filepath.Join(root, "one", "same")
	second := filepath.Join(root, "two", "same")
	testutil.NoError(t, os.MkdirAll(first, 0o700))
	testutil.NoError(t, os.MkdirAll(second, 0o700))
	firstProject, err := store.ResolveProject(context.Background(), first)
	testutil.Require(t, err == nil && firstProject == "same", "legacy adoption=%q err=%v", firstProject, err)
	repeated, err := store.ResolveProject(context.Background(), first)
	testutil.Require(t, err == nil && repeated == firstProject, "binding changed=%q err=%v", repeated, err)
	secondProject, err := store.ResolveProject(context.Background(), second)
	testutil.Require(t, err == nil && secondProject != "" && secondProject != firstProject && strings.HasPrefix(secondProject, "same-"), "collision project=%q err=%v", secondProject, err)
}

func TestResolveProject_ReadOnlyResolvesWithoutCreatingBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	root := t.TempDir()
	boundWorkspace := filepath.Join(root, "bound")
	legacyWorkspace := filepath.Join(root, "legacy")
	newWorkspace := filepath.Join(root, "new")
	for _, workspace := range []string{boundWorkspace, legacyWorkspace, newWorkspace} {
		testutil.NoError(t, os.MkdirAll(workspace, 0o700))
	}
	boundProject, err := store.ResolveProject(context.Background(), boundWorkspace)
	testutil.NoError(t, err)
	mustSave(t, store, observation("legacy-observation", "legacy", "legacy project token"))
	var projectsBefore, rootsBefore int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projectsBefore))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM project_roots`).Scan(&rootsBefore))
	testutil.NoError(t, store.Close())

	readOnly, err := OpenRead(context.Background(), path)
	testutil.NoError(t, err)
	resolvedBound, err := readOnly.ResolveProject(context.Background(), boundWorkspace)
	testutil.Require(t, err == nil && resolvedBound == boundProject, "bound=%q want=%q err=%v", resolvedBound, boundProject, err)
	resolvedLegacy, err := readOnly.ResolveProject(context.Background(), legacyWorkspace)
	testutil.Require(t, err == nil && resolvedLegacy == "legacy", "legacy=%q err=%v", resolvedLegacy, err)
	resolvedNew, err := readOnly.ResolveProject(context.Background(), newWorkspace)
	testutil.Require(t, err == nil && strings.HasPrefix(resolvedNew, "new-"), "new=%q err=%v", resolvedNew, err)
	testutil.NoError(t, readOnly.Close())

	store = openPath(t, path)
	defer store.Close()
	var projectsAfter, rootsAfter int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projectsAfter))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM project_roots`).Scan(&rootsAfter))
	testutil.Require(t, projectsAfter == projectsBefore && rootsAfter == rootsBefore, "read-only resolution mutated storage: projects=%d/%d roots=%d/%d", projectsBefore, projectsAfter, rootsBefore, rootsAfter)
}

func TestImportLegacy_MergesProjectsIdempotentlyWithoutMutatingSource(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", legacyPath)
	testutil.NoError(t, err)
	_, err = legacy.Exec(schemaV1 + schemaV2 + `
		PRAGMA user_version=2;
		INSERT INTO observations(
			id,project_id,session_id,scope,type,content,topic_key,producer,state,created_at,updated_at,title
		) VALUES(
			'legacy-observation','project-a','shared-session','project','learning',
			'legacy import token','shared-topic','legacy','active',1,1,'Legacy'
		);
		INSERT INTO observations_fts(id,content) VALUES('legacy-observation','legacy import token');
	`)
	testutil.NoError(t, err)
	testutil.NoError(t, legacy.Close())
	before := durableStorageSnapshot(t, legacyPath)

	targetPath := filepath.Join(t.TempDir(), "global.db")
	store := openPath(t, targetPath)
	current := observation("current-observation", "project-b", "current token")
	current.TopicKey = "shared-topic"
	current.Session = "shared-session"
	mustSave(t, store, current)

	testutil.NoError(t, store.ImportLegacy(context.Background(), legacyPath))
	testutil.NoError(t, store.ImportLegacy(context.Background(), legacyPath))
	found, err := store.Search(context.Background(), Search{Query: "legacy", Project: "project-a", Scope: ScopeProject})
	testutil.Require(t, err == nil && len(found) == 1 && found[0].ID == "legacy-observation" && found[0].Title == "Legacy", "legacy import mismatch: %+v %v", found, err)
	var imports, sessions int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM legacy_imports`).Scan(&imports))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sessions WHERE id='shared-session'`).Scan(&sessions))
	testutil.Require(t, imports == 1 && sessions == 2, "idempotency or project session isolation failed: imports=%d sessions=%d", imports, sessions)
	testutil.Require(t, before == durableStorageSnapshot(t, legacyPath), "legacy source was mutated")
	testutil.NoError(t, store.Close())
}

func TestImportLegacy_RemapsSameNamedWorkspacesToResolvedProjects(t *testing.T) {
	root := t.TempDir()
	firstWorkspace := filepath.Join(root, "one", "same")
	secondWorkspace := filepath.Join(root, "two", "same")
	testutil.NoError(t, os.MkdirAll(firstWorkspace, 0o700))
	testutil.NoError(t, os.MkdirAll(secondWorkspace, 0o700))
	store := openPath(t, filepath.Join(t.TempDir(), "global.db"))
	defer store.Close()
	firstProject, err := store.ResolveProject(context.Background(), firstWorkspace)
	testutil.NoError(t, err)
	secondProject, err := store.ResolveProject(context.Background(), secondWorkspace)
	testutil.Require(t, err == nil && firstProject != secondProject, "project collision: %q %q %v", firstProject, secondProject, err)

	createLegacy := func(path, id, content string) {
		db, openErr := sql.Open("sqlite", path)
		testutil.NoError(t, openErr)
		_, execErr := db.Exec(schemaV1+schemaV2+`
			PRAGMA user_version=2;
			INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,title)
			VALUES(?, 'same', 'project', 'learning', ?, 'legacy', 'active', 1, 1, 'Legacy');
			INSERT INTO observations_fts(id,content) VALUES(?, ?);`, id, content, id, content)
		testutil.NoError(t, execErr)
		testutil.NoError(t, db.Close())
	}
	firstLegacy := filepath.Join(t.TempDir(), "first.db")
	secondLegacy := filepath.Join(t.TempDir(), "second.db")
	createLegacy(firstLegacy, "first-observation", "first unique token")
	createLegacy(secondLegacy, "second-observation", "second unique token")
	testutil.NoError(t, store.ImportLegacy(context.Background(), firstLegacy, firstProject))
	testutil.NoError(t, store.ImportLegacy(context.Background(), secondLegacy, secondProject))

	first, err := store.Search(context.Background(), Search{Query: "first", Project: firstProject, Scope: ScopeProject})
	testutil.Require(t, err == nil && len(first) == 1 && first[0].ID == "first-observation", "first import=%+v err=%v", first, err)
	second, err := store.Search(context.Background(), Search{Query: "second", Project: secondProject, Scope: ScopeProject})
	testutil.Require(t, err == nil && len(second) == 1 && second[0].ID == "second-observation", "second import=%+v err=%v", second, err)
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

func TestHealthFile_RejectsFutureSchemaWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	testutil.NoError(t, store.Close())
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`PRAGMA user_version=99`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	before := durableStorageSnapshot(t, path)
	_, err = HealthFile(context.Background(), path)
	testutil.Require(t, errors.Is(err, ErrMigration), "expected newer-schema rejection, got %v", err)
	testutil.Require(t, before == durableStorageSnapshot(t, path), "health mutated future schema")
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

func TestOpen_RejectsDatabaseAncestorSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on some Windows hosts")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	testutil.NoError(t, os.Symlink(target, link))
	_, err := Open(context.Background(), filepath.Join(link, "memory.db"), nil)
	testutil.Require(t, err != nil, "expected database ancestor symlink rejection")
}

func TestOpen_MakesSQLiteArtifactsOwnerPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	defer store.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		testutil.NoError(t, err)
		testutil.Require(t, info.Mode().Perm() == 0o600, "%s mode = %o, want 0600", suffix, info.Mode().Perm())
	}
}

func TestOpen_FailingFreshAttemptDoesNotDeleteConcurrentStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	failure := errors.New("injected failure")
	var concurrent *Store
	_, err := open(context.Background(), path, nil, func() error {
		var openErr error
		concurrent, openErr = Open(context.Background(), path, nil)
		testutil.NoError(t, openErr)
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("Open error = %v, want injected failure", err)
	}
	defer concurrent.Close()
	if _, err := concurrent.Health(context.Background()); err != nil {
		t.Fatalf("concurrent store was removed: %v", err)
	}
}

func TestStoreClose_ReturnsCheckpointAndCloseFailures(t *testing.T) {
	checkpointErr := errors.New("checkpoint failed")
	closeErr := errors.New("close failed")
	store := &Store{
		checkpoint: func() (int, int, int, error) { return 0, 0, 0, checkpointErr },
		close:      func() error { return closeErr },
	}
	err := store.Close()
	testutil.Require(t, errors.Is(err, checkpointErr), "missing checkpoint failure: %v", err)
	testutil.Require(t, errors.Is(err, closeErr), "missing close failure: %v", err)
}

func TestStoreClose_IgnoresBusyCheckpointButReturnsCheckpointError(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		store := &Store{
			checkpoint: func() (int, int, int, error) { return 1, 0, 0, nil },
			close:      func() error { return nil },
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Store.Close() error = %v, want nil", err)
		}
	})
	t.Run("error", func(t *testing.T) {
		checkpointErr := errors.New("checkpoint failed")
		store := &Store{
			checkpoint: func() (int, int, int, error) { return 0, 0, 0, checkpointErr },
			close:      func() error { return nil },
		}
		if err := store.Close(); !errors.Is(err, checkpointErr) {
			t.Fatalf("Store.Close() error = %v, want checkpoint error", err)
		}
	})
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

func TestSQLiteMemoryStore_SaveTopicUpsertsSameBoundaryAtomically(t *testing.T) {
	store := openTestStore(t)
	first := observation("obs-1", "project-a", "first topic")
	first.Title = "First"
	first.TopicKey = "architecture/store"
	first = mustSave(t, store, first)
	second := first
	second.ID = "other"
	second.Title = "Second"
	second.Content = "second topic"
	second.Session = "updated"
	got, err := store.Save(context.Background(), second)
	testutil.Require(t, err == nil && got.ID == first.ID && got.CreatedAt == first.CreatedAt && got.Title == "Second" && got.Content == "second topic" && got.Session == "updated", "upsert mismatch: %+v %v", got, err)
	second.References = []string{first.ID}
	_, err = store.Save(context.Background(), second)
	testutil.Require(t, errors.Is(err, ErrInvalid), "resolved self-reference accepted: %v", err)
	got, err = store.Get(context.Background(), first.ID, first.Project, first.Scope)
	testutil.Require(t, err == nil && got.Content == second.Content && len(got.References) == 0, "rejected upsert mutated original: %+v %v", got, err)
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
	crossProject := observation("obs-2", "project-b", "cross project")
	crossProject.TopicKey = "shared"
	mustSave(t, store, crossProject)
	crossScope := observation("obs-3", "project-a", "cross scope")
	crossScope.Scope = ScopePersonal
	crossScope.TopicKey = "shared"
	mustSave(t, store, crossScope)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Save(ctx, observation("obs-4", "project-a", "cancelled"))
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

func TestStore_RecentOrdersAndIsolatesProjectScopeAndState(t *testing.T) {
	store := openTestStore(t)
	for _, item := range []Observation{
		observation("old", "project-a", "old active"),
		observation("tie-b", "project-a", "tied active b"),
		observation("tie-a", "project-a", "tied active a"),
		observation("new", "project-a", "new active"),
		observation("foreign", "project-b", "foreign active"),
		{ID: "personal", Project: "project-a", Scope: ScopePersonal, Type: "learning", Content: "personal active", Provenance: Provenance{Producer: "test"}, State: StateActive},
		{ID: "review", Project: "project-a", Scope: ScopeProject, Type: "learning", Content: "needs review", Provenance: Provenance{Producer: "test"}, State: StateNeedsReview},
	} {
		mustSave(t, store, item)
	}
	for id, updated := range map[string]int64{
		"old": 10, "tie-a": 20, "tie-b": 20, "new": 30,
		"foreign": 40, "personal": 40, "review": 40,
	} {
		_, err := store.db.Exec(`UPDATE observations SET updated_at=? WHERE id=?`, updated, id)
		testutil.NoError(t, err)
	}

	got, err := store.Recent(context.Background(), Recent{Project: "project-a", Scope: ScopeProject, States: []State{StateActive}, Limit: 10})
	testutil.Require(t, err == nil && len(got) == 4, "recent: %+v %v", got, err)
	want := []string{"new", "tie-a", "tie-b", "old"}
	for index := range want {
		testutil.Require(t, got[index].ID == want[index], "recent order=%+v", got)
	}
}

func TestSQLiteMemoryStore_SearchGetIsolationAndOrder(t *testing.T) {
	store := openTestStore(t)
	for _, item := range []Observation{
		{ID: "a", Title: "A", Project: "p", Scope: ScopeProject, Type: "learning", TopicKey: "topic/a", Content: "shared token", Provenance: Provenance{Producer: "test"}, State: StateActive},
		{ID: "b", Title: "B", Project: "p", Scope: ScopeProject, Type: "decision", TopicKey: "topic/b", Content: "shared token", Provenance: Provenance{Producer: "test"}, State: StateActive},
		{ID: "c", Title: "C", Project: "p", Scope: ScopePersonal, Type: "learning", Content: "shared token", Provenance: Provenance{Producer: "test"}, State: StateActive},
		{ID: "d", Title: "D", Project: "foreign", Scope: ScopeProject, Type: "learning", Content: "shared token", Provenance: Provenance{Producer: "test"}, State: StateActive},
	} {
		mustSave(t, store, item)
	}
	found, err := store.Search(context.Background(), Search{Query: "shared", Project: "p", Scope: ScopeProject, Types: []string{"learning"}, TopicKey: "topic/a"})
	testutil.Require(t, err == nil && len(found) == 1 && found[0].ID == "a" && found[0].Title == "A", "isolated search: %+v %v", found, err)
	got, err := store.Get(context.Background(), "a", "p", ScopeProject)
	testutil.Require(t, err == nil && got.Title == "A" && got.Content == "shared token", "get: %+v %v", got, err)
	_, err = store.Get(context.Background(), "a", "foreign", ScopeProject)
	testutil.Require(t, errors.Is(err, ErrNotFound) && !strings.Contains(err.Error(), "p"), "foreign metadata leaked: %v", err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Get(ctx, "a", "p", ScopeProject)
	testutil.Require(t, errors.Is(err, context.Canceled), "get cancellation: %v", err)
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
	testutil.Require(t, err == nil && version == 9, "isolated health=%d %v", version, err)
}

// durableStorageSnapshot hashes durable SQLite state. Read-only SQLite may
// create an empty WAL or update -shm; neither contains durable WAL frames.
func durableStorageSnapshot(t *testing.T, path string) string {
	t.Helper()
	parts := make([]string, 0, 2)
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(path + suffix)
		if err == nil {
			if suffix == "-wal" && len(data) == 0 {
				parts = append(parts, suffix+":empty")
				continue
			}
			parts = append(parts, fmt.Sprintf("%s:%x", suffix, sha256.Sum256(data)))
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		} else if suffix == "-wal" {
			parts = append(parts, suffix+":empty")
		} else {
			parts = append(parts, suffix+":absent")
		}
	}
	return strings.Join(parts, ",")
}

func migratedPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	testutil.NoError(t, store.Close())
	return path
}

func healthWithoutDurableMutation(t *testing.T, path string) (int, error) {
	t.Helper()
	before := durableStorageSnapshot(t, path)
	version, err := HealthFile(context.Background(), path)
	after := durableStorageSnapshot(t, path)
	testutil.Require(t, before == after, "health mutated durable state %s before=%s after=%s", path, before, after)
	return version, err
}

func TestHealthFile_HealthyDatabaseWithoutMutation(t *testing.T) {
	path := migratedPath(t)
	version, err := healthWithoutDurableMutation(t, path)
	testutil.Require(t, err == nil && version == 9, "health=%d err=%v", version, err)
	missing := filepath.Join(t.TempDir(), "missing.db")
	version, err = HealthFile(context.Background(), missing)
	testutil.Require(t, err == nil && version == 0, "missing health=%d err=%v", version, err)
	_, statErr := os.Stat(missing)
	testutil.Require(t, os.IsNotExist(statErr), "health created missing database")
}

func TestHealthFile_SeesCommittedWALState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	defer store.Close()
	mustSave(t, store, observation("wal-observation", "project-a", "WAL health token"))

	version, err := HealthFile(context.Background(), path)
	testutil.Require(t, err == nil && version == 9, "health=%d err=%v", version, err)
}

func TestSQLiteReadURI_IsReadOnly(t *testing.T) {
	path := migratedPath(t)
	db, err := sql.Open("sqlite", sqliteReadURI(path))
	testutil.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`PRAGMA user_version=99`)
	testutil.Require(t, err != nil, "read-only URI accepted a write")
}

func TestHealthFile_RejectsMissingRequiredTableWithoutMutation(t *testing.T) {
	for _, table := range []string{"projects", "sessions", "observations", "observation_refs", "legacy_imports", "project_roots"} {
		t.Run(table, func(t *testing.T) {
			path := migratedPath(t)
			db, err := sql.Open("sqlite", path)
			testutil.NoError(t, err)
			_, err = db.Exec(`PRAGMA foreign_keys=OFF; DROP TABLE ` + table)
			testutil.NoError(t, err)
			testutil.NoError(t, db.Close())
			_, err = healthWithoutDurableMutation(t, path)
			testutil.Require(t, errors.Is(err, ErrCorrupt), "expected missing-table corruption, got %v", err)
		})
	}
}

func TestHealthFile_RejectsUnusableFTSWithoutMutation(t *testing.T) {
	path := migratedPath(t)
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`DROP TABLE observations_fts; CREATE TABLE observations_fts(id TEXT, content TEXT)`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	_, err = healthWithoutDurableMutation(t, path)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "expected unusable FTS corruption, got %v", err)
}

func TestHealthFile_RejectsIntegrityFailureWithoutMutation(t *testing.T) {
	path := migratedPath(t)
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	testutil.NoError(t, err)
	_, err = file.WriteAt(make([]byte, 32), 100)
	testutil.NoError(t, err)
	testutil.NoError(t, file.Close())
	_, err = healthWithoutDurableMutation(t, path)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "expected integrity corruption, got %v", err)
}

func TestHealthFile_RejectsForeignKeyViolationWithoutMutation(t *testing.T) {
	path := migratedPath(t)
	store := openPath(t, path)
	source := mustSave(t, store, observation("source", "project-a", "source token"))
	testutil.NoError(t, store.Close())
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`INSERT INTO observation_refs(observation_id,target_id) VALUES(?,?)`, source.ID, "missing")
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	_, err = healthWithoutDurableMutation(t, path)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "expected foreign-key corruption, got %v", err)
}

func TestOpenHelperProcess(t *testing.T) {
	if os.Getenv("VGXNESS_OPEN_HELPER") != "1" {
		return
	}
	store, err := Open(context.Background(), os.Getenv("VGXNESS_DB_PATH"), nil)
	testutil.NoError(t, err)
	_ = store.Close()
}

func TestOpen_ConcurrentFreshProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	commands := make([]*exec.Cmd, 4)
	for index := range commands {
		commands[index] = exec.Command(os.Args[0], "-test.run=^TestOpenHelperProcess$")
		commands[index].Env = append(os.Environ(), "VGXNESS_OPEN_HELPER=1", "VGXNESS_DB_PATH="+path)
		testutil.NoError(t, commands[index].Start())
	}
	for _, command := range commands {
		testutil.NoError(t, command.Wait())
	}
	version, err := HealthFile(context.Background(), path)
	testutil.Require(t, err == nil && version == 9, "concurrent health=%d err=%v", version, err)
}

func TestOpen_MigrationRetryBoundAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`PRAGMA journal_mode=WAL`)
	testutil.NoError(t, err)
	conn, err := db.Conn(context.Background())
	testutil.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`)
	testutil.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = Open(ctx, path, nil)
	testutil.Require(t, errors.Is(err, context.DeadlineExceeded), "expected bounded cancellation, got %v", err)
	_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
	testutil.NoError(t, conn.Close())
	var version, tables int
	testutil.NoError(t, db.QueryRow(`PRAGMA user_version`).Scan(&version))
	testutil.NoError(t, db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='projects'`).Scan(&tables))
	testutil.Require(t, version == 0 && tables == 0, "partial schema version=%d tables=%d", version, tables)
	testutil.NoError(t, db.Close())
}
