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
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(schemaV1 + schemaV2 + schemaV3 + schemaV4 + schemaV5 + `
		PRAGMA user_version=5;
		INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at)
		VALUES('existing','project','project','learning','durable memory','test','active',1,1);`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	store := openPath(t, path)
	defer store.Close()
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 6, "health=%d err=%v", version, err)
	got, err := store.Get(context.Background(), "existing", "project", ScopeProject)
	testutil.Require(t, err == nil && got.Content == "durable memory", "memory=%+v err=%v", got, err)
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
