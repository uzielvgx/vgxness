package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/syncservice"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestSyncMigrationPreservesExistingMemory(t *testing.T) {
	for version, schema := range map[int]string{5: schemaV1 + schemaV2 + schemaV3 + schemaV4 + schemaV5, 6: schemaV1 + schemaV2 + schemaV3 + schemaV4 + schemaV5 + schemaV6} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "memory.db")
			db, err := sql.Open("sqlite", path)
			testutil.NoError(t, err)
			_, err = db.Exec(schema + fmt.Sprintf(`PRAGMA user_version=%d;
				INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at)
				VALUES('existing','project','project','learning','durable memory','test','active',1,1);`, version))
			testutil.NoError(t, err)
			testutil.NoError(t, db.Close())
			store := openPath(t, path)
			defer store.Close()
			gotVersion, err := store.Health(context.Background())
			testutil.Require(t, err == nil && gotVersion == 7, "health=%d err=%v", gotVersion, err)
			got, err := store.Get(context.Background(), "existing", "project", ScopeProject)
			testutil.Require(t, err == nil && got.Content == "durable memory", "memory=%+v err=%v", got, err)
		})
	}
}

func TestSyncProfileRejectsRawCredentials(t *testing.T) {
	store := openTestStore(t)
	_, err := store.ConfigureSyncProfile(context.Background(), SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "bearer secret"})
	testutil.Require(t, errors.Is(err, ErrInvalid), "credential error=%v", err)
	_, err = store.ConfigureSyncProfile(context.Background(), SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/vgx1.550e8400-e29b-41d4-a716-446655440000.abcdefghijklmnopqrstuv"})
	testutil.Require(t, errors.Is(err, ErrInvalid), "embedded bearer error=%v", err)
}

func TestSyncProfileRoundTripAndDisable(t *testing.T) {
	store := openTestStore(t)
	_, found, err := store.GetSyncProfile(context.Background())
	testutil.Require(t, err == nil && !found, "absent profile found=%t err=%v", found, err)
	created, err := store.ConfigureSyncProfile(context.Background(), SyncProfile{Enabled: true, Endpoint: "HTTPS://Sync.Example.Test:443", DeviceID: "550E8400-E29B-41D4-A716-446655440000", CredentialRef: "secret://keychain/sync"})
	testutil.NoError(t, err)
	testutil.Require(t, created.Endpoint == "https://sync.example.test" && created.DeviceID == "550e8400-e29b-41d4-a716-446655440000" && created.CreatedAt.Equal(fixedTime), "created=%+v", created)
	disabled, err := store.ConfigureSyncProfile(context.Background(), SyncProfile{Endpoint: created.Endpoint, DeviceID: created.DeviceID, CredentialRef: created.CredentialRef})
	testutil.NoError(t, err)
	got, found, err := store.GetSyncProfile(context.Background())
	testutil.Require(t, err == nil && found && !got.Enabled && got.CreatedAt.Equal(created.CreatedAt) && got.UpdatedAt.Equal(disabled.UpdatedAt), "profile=%+v found=%t err=%v", got, found, err)

	first := SyncProfile{Enabled: true, Endpoint: "https://one.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440001", CredentialRef: "secret://keychain/one"}
	second := SyncProfile{Enabled: true, Endpoint: "https://two.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440002", CredentialRef: "secret://keychain/two"}
	start := make(chan struct{})
	errs := make(chan error, 32)
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		profile := first
		if i%2 == 1 {
			profile = second
		}
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			returned, configureErr := store.ConfigureSyncProfile(context.Background(), profile)
			if configureErr != nil || returned.Endpoint != profile.Endpoint || returned.DeviceID != profile.DeviceID || returned.CredentialRef != profile.CredentialRef {
				errs <- fmt.Errorf("returned=%+v err=%v", returned, configureErr)
			}
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for configureErr := range errs {
		t.Error(configureErr)
	}
	final, found, err := store.GetSyncProfile(context.Background())
	testutil.Require(t, err == nil && found && (final.Endpoint == first.Endpoint || final.Endpoint == second.Endpoint) && ((final.Endpoint == first.Endpoint && final.DeviceID == first.DeviceID && final.CredentialRef == first.CredentialRef) || (final.Endpoint == second.Endpoint && final.DeviceID == second.DeviceID && final.CredentialRef == second.CredentialRef)), "final=%+v found=%t err=%v", final, found, err)
}

func TestSyncOutboxStableIdentityAndOrdering(t *testing.T) {
	store := openTestStore(t)
	first := syncMutation("550e8400-e29b-41d4-a716-446655440001", "project-a")
	second := syncMutation(first.MutationID, "project-b")
	enqueueMutation(t, store, first)
	enqueueMutation(t, store, first)
	testutil.Require(t, errors.Is(enqueueMutationError(t, store, second), ErrConflict), "different duplicate accepted")
	generated := syncMutation("", "project-c")
	returned := enqueueMutation(t, store, generated)
	testutil.Require(t, canonicalUUIDPattern.MatchString(returned.MutationID), "generated ID=%q", returned.MutationID)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 2 && entries[0].Mutation.MutationID == first.MutationID && entries[0].Mutation.RecordID == "project-a" && entries[1].Mutation.MutationID == returned.MutationID, "entries=%+v err=%v", entries, err)
	legacy := syncMutation("550e8400-e29b-41d4-a716-446655440006", strings.Repeat("a", 513))
	enqueueMutation(t, store, legacy)
	entries, err = store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 3 && entries[2].Mutation.RecordID == legacy.RecordID, "legacy entries=%+v err=%v", entries, err)
}

func TestSyncOutboxRetryStateTransitions(t *testing.T) {
	store := openTestStore(t)
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440003", "project")
	enqueueMutation(t, store, mutation)
	next := fixedTime.Add(time.Minute)
	testutil.NoError(t, store.MarkSyncOutboxRetry(context.Background(), mutation.MutationID, next, "temporary"))
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 0, "premature entries=%+v err=%v", entries, err)
	entries, err = store.DueSyncOutbox(context.Background(), next)
	testutil.Require(t, err == nil && len(entries) == 1 && entries[0].Attempts == 1 && entries[0].State == SyncOutboxRetry && entries[0].LastErrorCode == "temporary" && entries[0].Mutation.MutationID == mutation.MutationID, "entries=%+v err=%v", entries, err)
}

func TestSyncOutboxRejectsInvalidOrExcludedPayload(t *testing.T) {
	store := openTestStore(t)
	invalid := syncMutation("not-a-uuid", "project")
	testutil.Require(t, errors.Is(enqueueMutationError(t, store, invalid), ErrInvalid), "invalid mutation accepted")
	excluded := syncMutation("550e8400-e29b-41d4-a716-446655440004", "project")
	excluded.RecordKind = syncservice.RecordKind("prompt")
	testutil.Require(t, errors.Is(enqueueMutationError(t, store, excluded), ErrInvalid), "excluded mutation accepted")
	valid := syncMutation("550e8400-e29b-41d4-a716-446655440005", "project-c")
	enqueueMutation(t, store, valid)
	_, err := store.db.Exec(`UPDATE sync_outbox SET attempts=1 WHERE mutation_id=?`, valid.MutationID)
	testutil.NoError(t, err)
	_, err = store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "inconsistent pending entry error=%v", err)
	tooLong := syncMutation("550e8400-e29b-41d4-a716-446655440007", strings.Repeat("a", 1025))
	testutil.Require(t, errors.Is(enqueueMutationError(t, store, tooLong), ErrInvalid), "oversized ID error=%v", enqueueMutationError(t, store, tooLong))
}

func TestSyncSchemaAndEnabledCorruptionFailsClosed(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		store := openTestStore(t)
		_, err := store.ConfigureSyncProfile(context.Background(), SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"})
		testutil.NoError(t, err)
		_, err = store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_profiles SET enabled=2; PRAGMA ignore_check_constraints=OFF`)
		testutil.NoError(t, err)
		_, _, err = store.GetSyncProfile(context.Background())
		testutil.Require(t, errors.Is(err, ErrCorrupt), "enabled corruption error=%v", err)
	})
	t.Run("table", func(t *testing.T) {
		store := openTestStore(t)
		_, err := store.db.Exec(`DROP TABLE sync_profiles; CREATE TABLE sync_profiles(singleton INTEGER PRIMARY KEY, enabled INTEGER)`)
		testutil.NoError(t, err)
		_, err = store.Health(context.Background())
		testutil.Require(t, errors.Is(err, ErrCorrupt), "table corruption error=%v", err)
	})
	t.Run("index", func(t *testing.T) {
		store := openTestStore(t)
		_, err := store.db.Exec(`DROP INDEX sync_outbox_due_idx`)
		testutil.NoError(t, err)
		_, err = store.Health(context.Background())
		testutil.Require(t, errors.Is(err, ErrCorrupt), "index corruption error=%v", err)
	})
	t.Run("outbox payload check", func(t *testing.T) {
		store := openTestStore(t)
		_, err := store.db.Exec(`DROP TABLE sync_outbox;
			CREATE TABLE sync_outbox (
				id INTEGER PRIMARY KEY,
				mutation_id TEXT NOT NULL UNIQUE CHECK (length(mutation_id) = 36),
				record_kind TEXT NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
				record_id TEXT NOT NULL CHECK (length(CAST(record_id AS BLOB)) BETWEEN 1 AND 1024),
				mutation_kind TEXT NOT NULL CHECK (mutation_kind IN ('create', 'update', 'archive', 'tombstone', 'resolve')),
				base_version INTEGER NOT NULL CHECK (base_version >= 0),
				payload_version INTEGER NOT NULL CHECK (payload_version = 1),
				payload BLOB NOT NULL,
				state TEXT NOT NULL CHECK (state IN ('pending', 'retry')),
				attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
				next_attempt_at INTEGER NOT NULL CHECK (next_attempt_at > 0),
				last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
				created_at INTEGER NOT NULL CHECK (created_at > 0),
				updated_at INTEGER NOT NULL CHECK (updated_at >= created_at)
			);
			CREATE INDEX sync_outbox_due_idx ON sync_outbox(next_attempt_at, created_at, id)`)
		testutil.NoError(t, err)
		_, err = store.Health(context.Background())
		testutil.Require(t, errors.Is(err, ErrCorrupt), "payload-check corruption error=%v", err)
	})
}

func TestSyncOutboxCorruptPayloadFailsClosed(t *testing.T) {
	for name, payload := range map[string]string{"empty": "X''", "oversized": "zeroblob(1048577)"} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			mutation := syncMutation("550e8400-e29b-41d4-a716-446655440008", "project")
			enqueueMutation(t, store, mutation)
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET payload=`+payload+` WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
			testutil.NoError(t, err)
			entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
			testutil.Require(t, errors.Is(err, ErrCorrupt) && len(entries) == 0, "payload corruption entries=%+v err=%v", entries, err)
		})
	}
}

func TestSyncOutboxDuplicateCorruptPayloadFailsClosed(t *testing.T) {
	store := openTestStore(t)
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440009", "project")
	enqueueMutation(t, store, mutation)
	_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET payload=zeroblob(1048577) WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
	testutil.NoError(t, err)
	err = enqueueMutationError(t, store, mutation)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "duplicate corrupt payload error=%v", err)
}

func TestSyncOutboxDuplicateCorruptRecordIDFailsClosed(t *testing.T) {
	store := openTestStore(t)
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440010", "project")
	enqueueMutation(t, store, mutation)
	_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET record_id=zeroblob(1025) WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
	testutil.NoError(t, err)
	err = enqueueMutationError(t, store, mutation)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "duplicate corrupt record ID error=%v", err)
}

func TestDueSyncOutboxCorruptScalarsFailClosed(t *testing.T) {
	for _, test := range []struct{ column, value string }{
		{"mutation_id", "X''"},
		{"record_kind", "'invalid'"},
		{"record_id", "zeroblob(1025)"},
		{"mutation_kind", "'invalid'"},
		{"payload", "X''"},
		{"state", "'invalid'"},
		{"last_error_code", "printf('%065d',0)"},
	} {
		t.Run(test.column, func(t *testing.T) {
			store := openTestStore(t)
			mutation := syncMutation("550e8400-e29b-41d4-a716-446655440011", "project")
			enqueueMutation(t, store, mutation)
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET `+test.column+`=`+test.value+` WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
			testutil.NoError(t, err)
			entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
			testutil.Require(t, errors.Is(err, ErrCorrupt) && len(entries) == 0, "entries=%+v err=%v", entries, err)
		})
	}
}

func TestDueSyncOutboxTextPayloadByteOverflowFailsClosed(t *testing.T) {
	store := openTestStore(t)
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440012", "project")
	enqueueMutation(t, store, mutation)
	_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET payload=printf('%1048576s','') || char(0) || 'x' WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
	testutil.NoError(t, err)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, errors.Is(err, ErrCorrupt) && len(entries) == 0, "entries=%+v err=%v", entries, err)
}

func TestDueSyncOutboxNULSuffixScalarsFailClosed(t *testing.T) {
	for _, test := range []struct{ column, value string }{
		{"mutation_id", "printf('%036d',0) || char(0) || printf('%1048576s','')"},
		{"last_error_code", "printf('%064d',0) || char(0) || printf('%1048576s','')"},
	} {
		t.Run(test.column, func(t *testing.T) {
			store := openTestStore(t)
			mutation := syncMutation("550e8400-e29b-41d4-a716-446655440013", "project")
			enqueueMutation(t, store, mutation)
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET `+test.column+`=`+test.value+` WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
			testutil.NoError(t, err)
			entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
			testutil.Require(t, errors.Is(err, ErrCorrupt) && len(entries) == 0, "entries=%+v err=%v", entries, err)
		})
	}
}

func TestSyncLocalWriteDisabledProfile(t *testing.T) {
	store := openTestStore(t)
	profile := SyncProfile{Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"}
	_, err := store.ConfigureSyncProfile(context.Background(), profile)
	testutil.NoError(t, err)
	mustSave(t, store, observation("local", "project", "local only"))
	var outbox int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.Require(t, outbox == 0, "outbox=%d", outbox)
}

func TestSyncLocalWriteProfileLookupFailureRollsBack(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	_, err := store.db.Exec(`DROP TABLE sync_profiles`)
	testutil.NoError(t, err)
	item := observation("profile-failure", "project", "must roll back")
	_, err = store.Save(context.Background(), item)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "save error=%v", err)
	_, err = store.Get(context.Background(), item.ID, item.Project, item.Scope)
	testutil.Require(t, errors.Is(err, ErrNotFound), "observation survived profile lookup failure: %v", err)
}

func TestSyncLocalWriteIdentityPayloadCorruptionRollsBack(t *testing.T) {
	for _, test := range []struct{ recordKind, payload string }{{"project", "X''"}, {"session", "zeroblob(1048577)"}} {
		t.Run(test.recordKind, func(t *testing.T) {
			store := openTestStore(t)
			enableSync(t, store)
			first := observation("first", "project", "first")
			first.Session = "session"
			mustSave(t, store, first)
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET payload=`+test.payload+` WHERE record_kind=?; PRAGMA ignore_check_constraints=OFF`, test.recordKind)
			testutil.NoError(t, err)
			second := observation("second", "project", "must roll back")
			second.Session = "session"
			_, err = store.Save(context.Background(), second)
			testutil.Require(t, errors.Is(err, ErrCorrupt), "save error=%v", err)
			_, err = store.Get(context.Background(), second.ID, second.Project, second.Scope)
			testutil.Require(t, errors.Is(err, ErrNotFound), "observation survived corrupt identity reuse: %v", err)
		})
	}
}

func TestSyncLocalWriteIdentityMutationIDCorruptionRollsBack(t *testing.T) {
	for _, test := range []struct{ recordKind, mutationID string }{{"project", "X''"}, {"session", "zeroblob(37)"}} {
		t.Run(test.recordKind, func(t *testing.T) {
			store := openTestStore(t)
			enableSync(t, store)
			first := observation("first", "project", "first")
			first.Session = "session"
			mustSave(t, store, first)
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET mutation_id=`+test.mutationID+` WHERE record_kind=?; PRAGMA ignore_check_constraints=OFF`, test.recordKind)
			testutil.NoError(t, err)
			second := observation("second", "project", "must roll back")
			second.Session = "session"
			_, err = store.Save(context.Background(), second)
			testutil.Require(t, errors.Is(err, ErrCorrupt), "save error=%v", err)
			_, err = store.Get(context.Background(), second.ID, second.Project, second.Scope)
			testutil.Require(t, errors.Is(err, ErrNotFound), "observation survived corrupt identity reuse: %v", err)
		})
	}
}

func TestSyncLocalWriteIdentityMutationIDNULSuffixRollsBack(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	first := observation("first", "project", "first")
	mustSave(t, store, first)
	_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET mutation_id=printf('%036d',0) || char(0) || printf('%1048576s','') WHERE record_kind='project'; PRAGMA ignore_check_constraints=OFF`)
	testutil.NoError(t, err)
	second := observation("second", "project", "must roll back")
	_, err = store.Save(context.Background(), second)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "save error=%v", err)
	_, err = store.Get(context.Background(), second.ID, second.Project, second.Scope)
	testutil.Require(t, errors.Is(err, ErrNotFound), "observation survived corrupt identity reuse: %v", err)
}

func TestSyncLocalWriteUpdateAndForgetEnqueueFailureRollBack(t *testing.T) {
	for _, operation := range []string{"update", "forget"} {
		t.Run(operation, func(t *testing.T) {
			store := openTestStore(t)
			enableSync(t, store)
			item := mustSave(t, store, observation(operation, "project", "original token"))
			_, err := store.db.Exec(`DROP TABLE sync_outbox`)
			testutil.NoError(t, err)
			if operation == "update" {
				item.Content = "changed token"
				_, err = store.Update(context.Background(), item)
			} else {
				_, err = store.Forget(context.Background(), item.ID, item.Project, item.Scope)
			}
			testutil.Require(t, errors.Is(err, ErrCorrupt), "%s error=%v", operation, err)
			got, getErr := store.Get(context.Background(), item.ID, item.Project, item.Scope)
			found, searchErr := store.Search(context.Background(), Search{Query: "original", Project: item.Project})
			testutil.Require(t, getErr == nil && got.State == StateActive && got.Content == "original token" && searchErr == nil && len(found) == 1 && found[0].ID == item.ID, "%s state=%+v get=%v search=%+v/%v", operation, got, getErr, found, searchErr)
		})
	}
}

func TestSyncLocalWriteTopicUpsertSnapshot(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	first := observation("topic", "project", "first")
	first.TopicKey = "topic/key"
	first = mustSave(t, store, first)
	latest := first
	latest.Content = "latest"
	latest.Title = "Latest"
	got, err := store.Save(context.Background(), latest)
	testutil.Require(t, err == nil && got.ID == first.ID, "upsert=%+v err=%v", got, err)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 3, "entries=%+v err=%v", entries, err)
	mutation := entries[2].Mutation
	testutil.Require(t, mutation.RecordKind == syncservice.RecordKindObservation && mutation.Kind == syncservice.MutationCreate && mutation.BaseVersion == 0 && mutation.Observation != nil && mutation.Observation.Content == "latest" && mutation.Observation.Title == "Latest", "mutation=%+v", mutation)
}

func TestForgetSyncVersionZeroIsArchivedCreate(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	item := mustSave(t, store, observation("zero-archive", "project", "archive me"))
	_, err := store.Forget(context.Background(), item.ID, item.Project, item.Scope)
	testutil.NoError(t, err)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 3, "entries=%+v err=%v", entries, err)
	mutation := entries[2].Mutation
	testutil.Require(t, mutation.Kind == syncservice.MutationCreate && mutation.BaseVersion == 0 && mutation.Tombstone == nil && mutation.Observation != nil && mutation.Observation.Lifecycle == syncservice.LifecycleArchived, "mutation=%+v", mutation)
}

func TestSyncLocalWriteAtomicity(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	_, err := store.db.Exec(`DROP TABLE sync_outbox`)
	testutil.NoError(t, err)
	_, err = store.Save(context.Background(), observation("atomic", "project", "must roll back"))
	testutil.Require(t, errors.Is(err, ErrCorrupt), "save error=%v", err)
	_, err = store.Get(context.Background(), "atomic", "project", ScopeProject)
	testutil.Require(t, errors.Is(err, ErrNotFound), "observation survived failed enqueue: %v", err)
}

func TestSyncLocalWriteBasesAndOrder(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	item := observation("ordered", "project", "first")
	item.Session = "session"
	item = mustSave(t, store, item)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 3, "entries=%+v err=%v", entries, err)
	for i, want := range []syncservice.RecordKind{syncservice.RecordKindProject, syncservice.RecordKindSession, syncservice.RecordKindObservation} {
		testutil.Require(t, entries[i].Mutation.RecordKind == want && entries[i].Mutation.Kind == syncservice.MutationCreate && entries[i].Mutation.BaseVersion == 0, "entry[%d]=%+v", i, entries[i])
	}
	_, err = store.db.Exec(`UPDATE projects SET sync_version=4 WHERE id='project'; UPDATE sessions SET sync_version=5 WHERE id='session' AND project_id='project'; UPDATE observations SET sync_version=6 WHERE id='ordered'`)
	testutil.NoError(t, err)
	item.Content = "changed"
	updated, err := store.Update(context.Background(), item)
	testutil.NoError(t, err)
	item = updated
	entries, err = store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 4, "entries=%+v err=%v", entries, err)
	got := entries[3].Mutation
	testutil.Require(t, got.RecordKind == syncservice.RecordKindObservation && got.Kind == syncservice.MutationUpdate && got.BaseVersion == 6, "mutation=%+v", got)
	var project, session, observationVersion int64
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM projects WHERE id='project'`).Scan(&project))
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM sessions WHERE id='session' AND project_id='project'`).Scan(&session))
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM observations WHERE id='ordered'`).Scan(&observationVersion))
	testutil.Require(t, project == 4 && session == 5 && observationVersion == 6, "versions=%d/%d/%d", project, session, observationVersion)
}

func TestForgetSyncArchiveAtomicity(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	item := mustSave(t, store, observation("archive", "project", "archive me"))
	_, err := store.db.Exec(`UPDATE observations SET sync_version=2 WHERE id=?`, item.ID)
	testutil.NoError(t, err)
	forgotten, err := store.Forget(context.Background(), item.ID, item.Project, item.Scope)
	testutil.Require(t, err == nil && forgotten.State == StateArchived, "forgotten=%+v err=%v", forgotten, err)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 3, "entries=%+v err=%v", entries, err)
	got := entries[2].Mutation
	testutil.Require(t, got.Kind == syncservice.MutationArchive && got.BaseVersion == 2 && got.Tombstone == nil && got.Observation != nil && got.Observation.Lifecycle == syncservice.LifecycleArchived, "archive mutation=%+v", got)
}

func TestSyncLocalWriteRestartAndConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	enableSync(t, store)
	testutil.NoError(t, store.Close())
	store = openPath(t, path)
	defer store.Close()
	var group sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			_, err := store.Save(context.Background(), observation(fmt.Sprintf("concurrent-%d", i), "project", fmt.Sprintf("content-%d", i)))
			if err != nil {
				errs <- err
			}
		}(i)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var version, observations, outbox int
	testutil.NoError(t, store.db.QueryRow(`PRAGMA user_version`).Scan(&version))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations`).Scan(&observations))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.Require(t, version == 7 && observations == 4 && outbox == 5, "version=%d observations=%d outbox=%d", version, observations, outbox)
}

func enableSync(t *testing.T, store *Store) {
	t.Helper()
	_, err := store.ConfigureSyncProfile(context.Background(), SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"})
	testutil.NoError(t, err)
}

func enqueueMutation(t *testing.T, store *Store, mutation syncservice.Mutation) syncservice.Mutation {
	t.Helper()
	returned, err := enqueueMutationResult(t, store, mutation)
	testutil.NoError(t, err)
	return returned
}

func enqueueMutationError(t *testing.T, store *Store, mutation syncservice.Mutation) error {
	_, err := enqueueMutationResult(t, store, mutation)
	return err
}

func enqueueMutationResult(t *testing.T, store *Store, mutation syncservice.Mutation) (syncservice.Mutation, error) {
	t.Helper()
	tx, err := store.db.BeginTx(context.Background(), nil)
	testutil.NoError(t, err)
	returned, err := store.enqueueSyncOutbox(context.Background(), tx, mutation)
	if err != nil {
		_ = tx.Rollback()
		return syncservice.Mutation{}, err
	}
	if err = tx.Commit(); err != nil {
		return syncservice.Mutation{}, err
	}
	return returned, nil
}

func syncMutation(id, recordID string) syncservice.Mutation {
	return syncservice.Mutation{MutationID: id, RecordID: recordID, RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: recordID}}
}
