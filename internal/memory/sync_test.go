package memory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/syncapi"
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
			testutil.Require(t, err == nil && gotVersion == 15, "health=%d err=%v", gotVersion, err)
			got, err := store.Get(context.Background(), "existing", "project", ScopeProject)
			testutil.Require(t, err == nil && got.Content == "durable memory", "memory=%+v err=%v", got, err)
		})
	}
}

func TestBackfillSyncProjectQueuesLegacyObservationsWithoutMutatingThem(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	item, err := store.Save(context.Background(), Observation{Project: "project", Scope: ScopeProject, Type: "learning", Content: "legacy", State: StateActive, Provenance: Provenance{Producer: "test"}})
	testutil.NoError(t, err)
	before := item
	result, err := store.BackfillSyncProject(context.Background(), "project", 100)
	testutil.NoError(t, err)
	testutil.Require(t, result.SchemaVersion == 1 && result.Observations == 1 && result.Projects == 1, "result=%+v", result)
	after, err := store.Get(context.Background(), item.ID, "project", ScopeProject)
	testutil.Require(t, err == nil && after.Content == before.Content && after.CreatedAt.Equal(before.CreatedAt) && after.UpdatedAt.Equal(before.UpdatedAt), "before=%+v after=%+v err=%v", before, after, err)
	again, err := store.BackfillSyncProject(context.Background(), "project", 100)
	testutil.Require(t, err == nil && again.Queued == 0, "again=%+v err=%v", again, err)
}

func TestBackfillSyncProjectLimitAndExistingOutboxCollision(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	for _, content := range []string{"one", "two"} {
		_, err := store.Save(context.Background(), Observation{Project: "project", Scope: ScopeProject, Type: "learning", Content: content, State: StateActive, Provenance: Provenance{Producer: "test"}})
		testutil.NoError(t, err)
	}
	first, err := store.BackfillSyncProject(context.Background(), "project", 1)
	testutil.Require(t, err == nil && first.Queued == 1 && first.Remaining && first.Limit == 1, "first=%+v err=%v", first, err)
	_, err = store.db.Exec(`UPDATE sync_outbox SET payload='{}' WHERE record_kind='project' AND record_id='project'`)
	testutil.NoError(t, err)
	_, err = store.BackfillSyncProject(context.Background(), "project", 1)
	testutil.Require(t, errors.Is(err, ErrConflict), "collision error=%v", err)
}

func TestBackfillSyncProjectConcurrentCallsDoNotDuplicateOutbox(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	_, err := store.Save(context.Background(), Observation{Project: "project", Scope: ScopeProject, Type: "learning", Content: "concurrent", State: StateActive, Provenance: Provenance{Producer: "test"}})
	testutil.NoError(t, err)
	results := make(chan SyncBackfillResult, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			result, err := store.BackfillSyncProject(context.Background(), "project", 100)
			results <- result
			errs <- err
		}()
	}
	queued := 0
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		queued += (<-results).Queued
	}
	var outbox int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.Require(t, queued == 2 && outbox == 2, "queued=%d outbox=%d", queued, outbox)
}

func TestBackfillSyncProjectSkipsTombstonedObservation(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	item, err := store.Save(context.Background(), Observation{Project: "project", Scope: ScopeProject, Type: "learning", Content: "deleted", State: StateActive, Provenance: Provenance{Producer: "test"}})
	testutil.NoError(t, err)
	_, err = store.db.Exec(`INSERT INTO sync_tombstones(history_id,seq,record_kind,record_id,canonical_version,payload_version,provenance,deleted_at) VALUES('550e8400-e29b-41d4-a716-446655440099',1,'observation',?,1,1,CAST('{}' AS BLOB),1)`, item.ID)
	testutil.NoError(t, err)
	result, err := store.BackfillSyncProject(context.Background(), "project", 100)
	testutil.Require(t, err == nil && result.Observations == 0 && result.Projects == 1, "result=%+v err=%v", result, err)
	var queued int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox WHERE record_id=?`, item.ID).Scan(&queued))
	testutil.Require(t, queued == 0, "resurrected outbox=%d", queued)
}

func TestSyncOutboxClaimsMigrationV9PreservesDataAndOutbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(schemaV1 + schemaV2 + schemaV3 + schemaV4 + schemaV5 + schemaV6 + schemaV7 + schemaV8 + schemaV9 + `
		PRAGMA user_version=9;
		INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at)
		VALUES('existing','project','project','learning','durable memory','test','active',1,1);
		INSERT INTO sync_outbox(mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,next_attempt_at,last_error_code,created_at,updated_at)
		VALUES('550e8400-e29b-41d4-a716-446655440020','project','project','create',0,1,'{}','pending',0,1,'',1,1);`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())

	store := openPath(t, path)
	defer store.Close()
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 15, "health=%d err=%v", version, err)
	got, err := store.Get(context.Background(), "existing", "project", ScopeProject)
	testutil.Require(t, err == nil && got.Content == "durable memory", "memory=%+v err=%v", got, err)
	var outbox, claims int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox_claims`).Scan(&claims))
	testutil.Require(t, outbox == 1 && claims == 0, "outbox=%d claims=%d", outbox, claims)
}

func TestSyncOutboxClaimsConstraintsCascadeAndHealth(t *testing.T) {
	insertOutbox := func(t *testing.T, store *Store, mutationID string) {
		t.Helper()
		_, err := store.db.Exec(`INSERT INTO sync_outbox(mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,next_attempt_at,last_error_code,created_at,updated_at)
			VALUES(?,'project','project','create',0,1,'{}','pending',0,1,'',1,1)`, mutationID)
		testutil.NoError(t, err)
	}
	insertClaim := func(t *testing.T, store *Store, mutationID, firstToken, token string, firstClaimedAt, claimedAt, leaseUntil any) error {
		t.Helper()
		_, err := store.db.Exec(`INSERT INTO sync_outbox_claims(mutation_id,first_claim_token,claim_token,first_claimed_at,claimed_at,lease_until) VALUES(?,?,?,?,?,?)`, mutationID, firstToken, token, firstClaimedAt, claimedAt, leaseUntil)
		return err
	}

	t.Run("rejects malformed tokens and times", func(t *testing.T) {
		for name, mutate := range map[string]func(*[]any){
			"uppercase first token":    func(values *[]any) { (*values)[1] = "550E8400-E29B-41D4-A716-446655440031" },
			"malformed claim token":    func(values *[]any) { (*values)[2] = "not-a-uuid" },
			"zero first claimed at":    func(values *[]any) { (*values)[3] = 0 },
			"text claimed at":          func(values *[]any) { (*values)[4] = "not-a-time" },
			"lease before claim":       func(values *[]any) { (*values)[5] = 1 },
			"claim before first claim": func(values *[]any) { (*values)[4] = 1 },
		} {
			t.Run(name, func(t *testing.T) {
				store := openTestStore(t)
				mutationID := "550e8400-e29b-41d4-a716-446655440030"
				insertOutbox(t, store, mutationID)
				values := []any{mutationID, "550e8400-e29b-41d4-a716-446655440031", "550e8400-e29b-41d4-a716-446655440032", 2, 3, 4}
				mutate(&values)
				err := insertClaim(t, store, values[0].(string), values[1].(string), values[2].(string), values[3], values[4], values[5])
				testutil.Require(t, err != nil, "invalid claim accepted")
			})
		}
	})
	t.Run("missing outbox is rejected", func(t *testing.T) {
		store := openTestStore(t)
		err := insertClaim(t, store, "550e8400-e29b-41d4-a716-446655440039", "550e8400-e29b-41d4-a716-446655440031", "550e8400-e29b-41d4-a716-446655440032", 1, 2, 3)
		testutil.Require(t, err != nil, "claim without outbox accepted")
	})
	t.Run("null mutation ID is rejected", func(t *testing.T) {
		store := openTestStore(t)
		_, err := store.db.Exec(`INSERT INTO sync_outbox_claims(mutation_id,first_claim_token,claim_token,first_claimed_at,claimed_at,lease_until)
			VALUES(NULL,'550e8400-e29b-41d4-a716-446655440031','550e8400-e29b-41d4-a716-446655440032',1,2,3)`)
		testutil.Require(t, err != nil, "null mutation ID accepted")
	})
	t.Run("outbox delete cascades", func(t *testing.T) {
		store := openTestStore(t)
		mutationID := "550e8400-e29b-41d4-a716-446655440040"
		insertOutbox(t, store, mutationID)
		testutil.NoError(t, insertClaim(t, store, mutationID, "550e8400-e29b-41d4-a716-446655440041", "550e8400-e29b-41d4-a716-446655440042", 1, 2, 3))
		_, err := store.db.Exec(`DELETE FROM sync_outbox WHERE mutation_id=?`, mutationID)
		testutil.NoError(t, err)
		var claims int
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox_claims WHERE mutation_id=?`, mutationID).Scan(&claims))
		testutil.Require(t, claims == 0, "claims=%d", claims)
	})
	for name, mutation := range map[string]string{
		"weakened table":            `DROP TABLE sync_outbox_claims; CREATE TABLE sync_outbox_claims(mutation_id TEXT PRIMARY KEY)`,
		"weakened index":            `DROP INDEX sync_outbox_claims_lease_idx; CREATE INDEX sync_outbox_claims_lease_idx ON sync_outbox_claims(mutation_id, lease_until)`,
		"case-altered type literal": `DROP TABLE sync_outbox_claims; ` + strings.Replace(strings.Split(schemaV10, ";")[0], "'text'", "'TEXT'", 1) + `;` + strings.Split(schemaV10, ";")[1] + `;`,
	} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			_, err := store.db.Exec(mutation)
			testutil.NoError(t, err)
			_, err = store.Health(context.Background())
			testutil.Require(t, errors.Is(err, ErrCorrupt), "health error=%v", err)
		})
	}
}

func TestSyncOutboxClaimsMigrationFreshSchema(t *testing.T) {
	store := openTestStore(t)
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 15, "health=%d err=%v", version, err)
	var table, index string
	testutil.NoError(t, store.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='table' AND name='sync_outbox_claims'`).Scan(&table))
	testutil.NoError(t, store.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='index' AND name='sync_outbox_claims_lease_idx'`).Scan(&index))
	testutil.Require(t, normalizeV10TableSQL(table) == normalizeV10TableSQL(strings.Split(schemaV10, ";")[0]) && strings.TrimSpace(strings.ToLower(index)) == "create index sync_outbox_claims_lease_idx on sync_outbox_claims(lease_until, mutation_id)", "table=%q index=%q", table, index)
}

func TestSyncMigrationV8PreservesV7DataAndStartsSyncPrimitivesEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(schemaV1 + schemaV2 + schemaV3 + schemaV4 + schemaV5 + schemaV6 + schemaV7 + `
		PRAGMA user_version=7;
		INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at)
		VALUES('existing','project','project','learning','durable memory','test','active',1,1);
		INSERT INTO sync_outbox(mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,next_attempt_at,last_error_code,created_at,updated_at)
		VALUES('550e8400-e29b-41d4-a716-446655440000','project','project','create',0,1,'{}','pending',0,1,'',1,1);`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())

	store := openPath(t, path)
	defer store.Close()
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 15, "health=%d err=%v", version, err)
	got, err := store.Get(context.Background(), "existing", "project", ScopeProject)
	testutil.Require(t, err == nil && got.Content == "durable memory", "memory=%+v err=%v", got, err)
	var count int
	err = store.db.QueryRow(`SELECT (SELECT count(*) FROM sync_inbox) + (SELECT count(*) FROM sync_cursor) + (SELECT count(*) FROM sync_tombstones) + (SELECT count(*) FROM sync_conflicts) + (SELECT count(*) FROM sync_bootstrap)`).Scan(&count)
	testutil.Require(t, err == nil && count == 0, "new sync rows=%d err=%v", count, err)
	var outbox int
	err = store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox)
	testutil.Require(t, err == nil && outbox == 1, "outbox=%d err=%v", outbox, err)
}

func TestSyncV8SchemaAndIndexesFailClosed(t *testing.T) {
	for name, mutation := range map[string]string{
		"inbox table":       `DROP TABLE sync_inbox; CREATE TABLE sync_inbox(history_id TEXT, seq INTEGER)`,
		"cursor table":      `DROP TABLE sync_cursor; CREATE TABLE sync_cursor(singleton INTEGER PRIMARY KEY, history_id TEXT, position INTEGER, updated_at INTEGER)`,
		"tombstone index":   `DROP INDEX sync_tombstones_record_idx; CREATE INDEX sync_tombstones_record_idx ON sync_tombstones(record_id, record_kind, canonical_version)`,
		"conflict index":    `DROP INDEX sync_conflicts_unresolved_idx; CREATE INDEX sync_conflicts_unresolved_idx ON sync_conflicts(status, record_id, record_kind, created_seq)`,
		"bootstrap table":   `DROP TABLE sync_bootstrap; CREATE TABLE sync_bootstrap(singleton INTEGER PRIMARY KEY, phase TEXT, payload_version INTEGER, checkpoint BLOB, created_at INTEGER, updated_at INTEGER)`,
		"push result table": `DROP TABLE sync_push_results; CREATE TABLE sync_push_results(mutation_id TEXT PRIMARY KEY, sequence INTEGER)`,
	} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			_, err := store.db.Exec(mutation)
			testutil.NoError(t, err)
			_, err = store.Health(context.Background())
			testutil.Require(t, errors.Is(err, ErrCorrupt), "health error=%v", err)
		})
	}
}

func TestSyncV8Constraints(t *testing.T) {
	t.Run("duplicate conflict ID", func(t *testing.T) {
		store := openTestStore(t)
		insert := `INSERT INTO sync_conflicts(conflict_id,history_id,created_seq,record_kind,record_id,canonical_version,competing_version_id,status,resolved_seq,payload_version,snapshot,created_at,updated_at)
			VALUES('550e8400-e29b-41d4-a716-446655440000','550e8400-e29b-41d4-a716-446655440001',?,'observation','record',1,'550e8400-e29b-41d4-a716-446655440002','unresolved',NULL,1,X'7B7D',1,1)`
		_, err := store.db.Exec(insert, 1)
		testutil.NoError(t, err)
		_, err = store.db.Exec(insert, 2)
		testutil.Require(t, err != nil, "duplicate conflict ID accepted")
	})
	t.Run("weakened inbox check", func(t *testing.T) {
		store := openTestStore(t)
		_, err := store.db.Exec(`DROP TABLE sync_inbox;
			CREATE TABLE sync_inbox (
				history_id TEXT NOT NULL CHECK (typeof(history_id) = 'text' AND length(CAST(history_id AS BLOB)) = 36 AND history_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND history_id NOT GLOB '*[^0-9a-f-]*' OR 1=1),
				seq INTEGER NOT NULL CHECK (typeof(seq) = 'integer' AND seq > 0),
				change_hash BLOB NOT NULL CHECK (typeof(change_hash) = 'blob' AND length(change_hash) = 32),
				applied_at INTEGER NOT NULL CHECK (typeof(applied_at) = 'integer' AND applied_at > 0),
				PRIMARY KEY (history_id, seq)
			)`)
		testutil.NoError(t, err)
		_, err = store.Health(context.Background())
		testutil.Require(t, errors.Is(err, ErrCorrupt), "health error=%v", err)
	})
}

func TestSyncPushReceiptSequenceIsUniqueWhenPresent(t *testing.T) {
	store := openTestStore(t)
	insert := `INSERT INTO sync_push_results(mutation_id,disposition,retryable,code,sequence,canonical_version,record_kind,record_id,mutation_kind,base_version,mutation_hash,completed_at)
		VALUES(?,?,0,'',?,1,'project',?,'create',0,zeroblob(32),1)`
	_, err := store.db.Exec(insert, "550e8400-e29b-41d4-a716-446655440060", "accepted", 1, "project-a")
	testutil.NoError(t, err)
	_, err = store.db.Exec(insert, "550e8400-e29b-41d4-a716-446655440061", "accepted", 1, "project-b")
	testutil.Require(t, err != nil, "duplicate sequence accepted")
	rejected := `INSERT INTO sync_push_results(mutation_id,disposition,retryable,code,sequence,canonical_version,record_kind,record_id,mutation_kind,base_version,mutation_hash,completed_at)
		VALUES(?,'rejected',0,'invalid_input',NULL,0,'project',?,'create',0,zeroblob(32),1)`
	_, err = store.db.Exec(rejected, "550e8400-e29b-41d4-a716-446655440062", "project-c")
	testutil.NoError(t, err)
	_, err = store.db.Exec(rejected, "550e8400-e29b-41d4-a716-446655440063", "project-d")
	testutil.NoError(t, err)
}

func TestApplySyncPushResultCompletesClaimAndRebasesLatest(t *testing.T) {
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	first := syncMutation("550e8400-e29b-41d4-a716-446655440070", "project")
	latest := syncMutation("550e8400-e29b-41d4-a716-446655440071", "project")
	enqueueMutation(t, store, first)
	enqueueMutation(t, store, latest)
	claims, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1, "claims=%+v err=%v", claims, err)
	seq := int64(1)
	result := syncservice.Result{MutationID: first.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &seq, Version: 1}
	testutil.Require(t, errors.Is(store.ApplySyncPushResult(context.Background(), first.MutationID, "550e8400-e29b-41d4-a716-446655440072", result), ErrNotFound), "stale completion accepted")
	testutil.NoError(t, store.ApplySyncPushResult(context.Background(), first.MutationID, claims[0].ClaimToken, result))
	var version, outbox, claimsLeft, receipts int
	var kind string
	var base int64
	var payload []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM projects WHERE id='project'`).Scan(&version))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox_claims`).Scan(&claimsLeft))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_push_results WHERE mutation_id=? AND sequence=1`, first.MutationID).Scan(&receipts))
	testutil.NoError(t, store.db.QueryRow(`SELECT mutation_kind,base_version,payload FROM sync_outbox WHERE mutation_id=?`, latest.MutationID).Scan(&kind, &base, &payload))
	var rebased syncservice.Mutation
	testutil.NoError(t, json.Unmarshal(payload, &rebased))
	testutil.Require(t, version == 1 && outbox == 1 && claimsLeft == 0 && receipts == 1 && kind == string(syncservice.MutationUpdate) && base == 1 && rebased.Kind == syncservice.MutationUpdate && rebased.BaseVersion == 1, "version=%d rows=%d/%d/%d rebase=%q/%d/%+v", version, outbox, claimsLeft, receipts, kind, base, rebased)
	testutil.NoError(t, store.ApplySyncPushResult(context.Background(), first.MutationID, claims[0].ClaimToken, result))
}

func TestPendingOwnConflictReceiptsAreBoundedAndSkipMaterialized(t *testing.T) {
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',1); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,1)`, fixedTime.UnixNano(), fixedTime.UnixNano())
	testutil.NoError(t, err)
	first := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "first", nil)
	first.MutationID = "550e8400-e29b-41d4-a716-4466554400a1"
	later := first
	later.MutationID = "550e8400-e29b-41d4-a716-4466554400a2"
	enqueueMutation(t, store, first)
	enqueueMutation(t, store, later)
	claims, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.NoError(t, err)
	_, err = store.db.Exec(`UPDATE observations SET sync_version=2 WHERE id='record'`)
	testutil.NoError(t, err)
	sequence := int64(1)
	testutil.NoError(t, store.ApplySyncPushResult(context.Background(), first.MutationID, claims[0].ClaimToken, syncservice.Result{MutationID: first.MutationID, Disposition: syncservice.DispositionConflict, Sequence: &sequence, Version: 2}))
	ids, err := store.PendingOwnConflictReceipts(context.Background())
	testutil.Require(t, err == nil && len(ids) == 1 && ids[0] == first.MutationID, "ids=%v err=%v", ids, err)
	_, err = store.db.Exec(`INSERT INTO sync_conflicts(conflict_id,history_id,created_seq,record_kind,record_id,canonical_version,competing_version_id,status,resolved_seq,payload_version,snapshot,created_at,updated_at) VALUES('550e8400-e29b-41d4-a716-4466554400a3','550e8400-e29b-41d4-a716-4466554400a4',1,'observation','record',2,?,'unresolved',NULL,1,X'7B7D',?,?)`, first.MutationID, fixedTime.UnixNano(), fixedTime.UnixNano())
	testutil.NoError(t, err)
	ids, err = store.PendingOwnConflictReceipts(context.Background())
	testutil.Require(t, err == nil && len(ids) == 0, "materialized ids=%v err=%v", ids, err)
	_, err = store.db.Exec(`UPDATE sync_conflicts SET status='resolved',resolved_seq=2`)
	testutil.NoError(t, err)
	ids, err = store.PendingOwnConflictReceipts(context.Background())
	testutil.Require(t, err == nil && len(ids) == 0, "resolved ids=%v err=%v", ids, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.PendingOwnConflictReceipts(ctx)
	testutil.Require(t, errors.Is(err, context.Canceled), "cancellation err=%v", err)
}

func TestApplySyncPushResultRetriesWithAdvancingClock(t *testing.T) {
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440081", "project")
	enqueueMutation(t, store, mutation)
	claims, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1, "claims=%+v err=%v", claims, err)

	now := fixedTime
	store.now = func() time.Time {
		now = now.Add(time.Nanosecond)
		return now
	}
	result := syncservice.Result{MutationID: mutation.MutationID, Disposition: syncservice.DispositionRejected, Retryable: true, Code: "temporary"}
	testutil.NoError(t, store.ApplySyncPushResult(context.Background(), mutation.MutationID, claims[0].ClaimToken, result))

	var state, code string
	var attempts, nextAttempt, leaseUntil int64
	testutil.NoError(t, store.db.QueryRow(`SELECT state,attempts,last_error_code,next_attempt_at FROM sync_outbox WHERE mutation_id=?`, mutation.MutationID).Scan(&state, &attempts, &code, &nextAttempt))
	testutil.NoError(t, store.db.QueryRow(`SELECT lease_until FROM sync_outbox_claims WHERE mutation_id=?`, mutation.MutationID).Scan(&leaseUntil))
	expected := fixedTime.Add(time.Nanosecond).UnixNano()
	testutil.Require(t, state == string(SyncOutboxRetry) && attempts == 1 && code == "temporary" && nextAttempt == expected && leaseUntil == expected, "retry=%q/%d/%q next=%d lease=%d", state, attempts, code, nextAttempt, leaseUntil)
}

func TestSyncOutboxClaimRetryApplyContract(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := fixedTime
	store.now = func() time.Time { return now }
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
	testutil.NoError(t, err)
	mutation := syncMutation("550e8400-e29b-41d4-a716-4466554400d1", "project")
	enqueueMutation(t, store, mutation)

	claims, err := store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1 && claims[0].State == SyncOutboxPending && claims[0].Attempts == 0 && claims[0].LastErrorCode == "", "pending claims=%+v err=%v", claims, err)
	first := claims[0]
	next := now.Add(time.Second)
	testutil.NoError(t, store.MarkSyncOutboxRetry(ctx, mutation.MutationID, first.ClaimToken, next, "transport"))

	now = next
	claims, err = store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1 && claims[0].State == SyncOutboxRetry && claims[0].Attempts == 1 && claims[0].LastErrorCode == "transport" && claims[0].FirstClaimToken == first.FirstClaimToken && claims[0].ClaimToken != first.ClaimToken, "retry claims=%+v err=%v", claims, err)
	retry := claims[0]
	testutil.Require(t, errors.Is(store.MarkSyncOutboxRetry(ctx, mutation.MutationID, first.ClaimToken, now.Add(time.Second), "transport"), ErrNotFound), "stale claim retry accepted")

	sequence := int64(1)
	result := syncservice.Result{MutationID: mutation.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &sequence, Version: 1}
	testutil.NoError(t, store.ApplySyncPushResult(ctx, mutation.MutationID, retry.ClaimToken, result))
	testutil.NoError(t, store.ApplySyncPushResult(ctx, mutation.MutationID, retry.ClaimToken, result))
	var outbox, receipts, version int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox WHERE mutation_id=?`, mutation.MutationID).Scan(&outbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_push_results WHERE mutation_id=?`, mutation.MutationID).Scan(&receipts))
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM projects WHERE id='project'`).Scan(&version))
	testutil.Require(t, outbox == 0 && receipts == 1 && version == 1, "applied rows=%d/%d version=%d", outbox, receipts, version)
}

func TestApplySyncPushResultRebasesArchivedCreateAsArchive(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	enableSync(t, store)
	item := mustSave(t, store, observation("archive-before-push", "project", "archive me"))
	_, err := store.Forget(ctx, item.ID, item.Project, item.Scope)
	testutil.NoError(t, err)

	claims, err := store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1 && claims[0].Mutation.RecordKind == syncservice.RecordKindProject, "project claim=%+v err=%v", claims, err)
	projectSequence := int64(1)
	testutil.NoError(t, store.ApplySyncPushResult(ctx, claims[0].Mutation.MutationID, claims[0].ClaimToken, syncservice.Result{MutationID: claims[0].Mutation.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &projectSequence, Version: 1}))

	claims, err = store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1 && claims[0].Mutation.RecordKind == syncservice.RecordKindObservation && claims[0].Mutation.RecordID == item.ID && claims[0].Mutation.Kind == syncservice.MutationCreate, "observation claim=%+v err=%v", claims, err)
	observationSequence := int64(2)
	testutil.NoError(t, store.ApplySyncPushResult(ctx, claims[0].Mutation.MutationID, claims[0].ClaimToken, syncservice.Result{MutationID: claims[0].Mutation.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &observationSequence, Version: 1}))

	var syncVersion, outbox, receipts int
	var kind string
	var base int64
	var payload []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM observations WHERE id=?`, item.ID).Scan(&syncVersion))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox WHERE record_kind='observation' AND record_id=?`, item.ID).Scan(&outbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_push_results WHERE mutation_id=?`, claims[0].Mutation.MutationID).Scan(&receipts))
	testutil.NoError(t, store.db.QueryRow(`SELECT mutation_kind,base_version,payload FROM sync_outbox WHERE record_kind='observation' AND record_id=?`, item.ID).Scan(&kind, &base, &payload))
	var rebased syncservice.Mutation
	testutil.NoError(t, json.Unmarshal(payload, &rebased))
	testutil.Require(t, syncVersion == 1 && receipts == 1 && outbox == 1 && kind == string(syncservice.MutationArchive) && base == 1 && rebased.Kind == syncservice.MutationArchive && rebased.BaseVersion == 1 && syncservice.ValidateMutation(rebased) == nil, "version=%d receipts=%d outbox=%d rebase=%q/%d/%+v", syncVersion, receipts, outbox, kind, base, rebased)
}

func TestApplySyncPushResultReceiptCollisionsAndEnqueueIDCollision(t *testing.T) {
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440073", "project")
	enqueueMutation(t, store, mutation)
	claims, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1, "claims=%+v err=%v", claims, err)
	hash, err := syncMutationHash(mutation)
	testutil.NoError(t, err)
	_, err = store.db.Exec(`INSERT INTO sync_push_results(mutation_id,disposition,retryable,code,sequence,canonical_version,record_kind,record_id,mutation_kind,base_version,mutation_hash,completed_at) VALUES(?,'accepted',0,'',1,1,'project','other','create',0,?,?)`, "550e8400-e29b-41d4-a716-446655440074", hash, fixedTime.UnixNano())
	testutil.NoError(t, err)
	seq := int64(1)
	err = store.ApplySyncPushResult(context.Background(), mutation.MutationID, claims[0].ClaimToken, syncservice.Result{MutationID: mutation.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &seq, Version: 1})
	var outbox, version int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM projects WHERE id='project'`).Scan(&version))
	testutil.Require(t, errors.Is(err, ErrConflict) && outbox == 1 && version == 0, "err=%v outbox=%d version=%d", err, outbox, version)
	testutil.Require(t, errors.Is(enqueueMutationError(t, store, syncMutation("550e8400-e29b-41d4-a716-446655440074", "other")), ErrConflict), "receipt mutation ID reused")
}

func TestOwnPulledReceiptsAdvanceWithoutOverwriteAndMaterializeConflict(t *testing.T) {
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440075"
	t.Run("accepted v1 and v2 are metadata only", func(t *testing.T) {
		store := openTestStore(t)
		store.now = func() time.Time { return fixedTime }
		testutil.NoError(t, func() error {
			_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,1)`, fixedTime.UnixNano(), fixedTime.UnixNano())
			return err
		}())
		first := syncMutation("550e8400-e29b-41d4-a716-446655440076", "project")
		enqueueMutation(t, store, first)
		claim, err := store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
		testutil.Require(t, err == nil && len(claim) == 1, "claim=%+v err=%v", claim, err)
		seq := int64(1)
		testutil.NoError(t, store.ApplySyncPushResult(ctx, first.MutationID, claim[0].ClaimToken, syncservice.Result{MutationID: first.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &seq, Version: 1}))
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, pulledChange(t, 1, 1, first)))
		tombstone := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440077", RecordID: "record", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}}
		enqueueMutation(t, store, tombstone)
		claim, err = store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
		testutil.Require(t, err == nil && len(claim) == 1, "claim=%+v err=%v", claim, err)
		seq = 2
		testutil.NoError(t, store.ApplySyncPushResult(ctx, tombstone.MutationID, claim[0].ClaimToken, syncservice.Result{MutationID: tombstone.MutationID, Disposition: syncservice.DispositionAccepted, Sequence: &seq, Version: 2}))
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, specialChange(t, 2, 2, syncservice.ChangeDispositionAccepted, "", tombstone)))
		var state string
		testutil.NoError(t, store.db.QueryRow(`SELECT state FROM observations WHERE id='record'`).Scan(&state))
		testutil.Require(t, state == string(StateActive), "own tombstone overwrote local state=%q", state)
	})
	t.Run("conflict bypasses pending guard but preserves later work", func(t *testing.T) {
		store := openTestStore(t)
		store.now = func() time.Time { return fixedTime }
		testutil.NoError(t, func() error {
			_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',1); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,1)`, fixedTime.UnixNano(), fixedTime.UnixNano())
			return err
		}())
		first := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "first", nil)
		first.MutationID = "550e8400-e29b-41d4-a716-446655440078"
		later := first
		later.MutationID = "550e8400-e29b-41d4-a716-446655440079"
		enqueueMutation(t, store, first)
		enqueueMutation(t, store, later)
		claim, err := store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
		testutil.Require(t, err == nil && len(claim) == 1, "claim=%+v err=%v", claim, err)
		_, err = store.db.Exec(`UPDATE observations SET sync_version=2 WHERE id='record'`)
		testutil.NoError(t, err)
		seq := int64(1)
		testutil.NoError(t, store.ApplySyncPushResult(ctx, first.MutationID, claim[0].ClaimToken, syncservice.Result{MutationID: first.MutationID, Disposition: syncservice.DispositionConflict, Sequence: &seq, Version: 2}))
		change := specialChange(t, 1, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440080", first)
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, change))
		var state string
		var pending int
		testutil.NoError(t, store.db.QueryRow(`SELECT state FROM observations WHERE id='record'`).Scan(&state))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox WHERE mutation_id=?`, later.MutationID).Scan(&pending))
		testutil.Require(t, state == string(StateNeedsReview) && pending == 1, "state=%q pending=%d", state, pending)
	})
}

func TestBootstrapOwnConflictMaterializesAndBlocksLaterWorkUntilResolved(t *testing.T) {
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440081"
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',1); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,1)`, fixedTime.UnixNano(), fixedTime.UnixNano())
		return err
	}())
	first := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "first", nil)
	first.MutationID = "550e8400-e29b-41d4-a716-446655440082"
	later := first
	later.MutationID = "550e8400-e29b-41d4-a716-446655440083"
	later.Observation.Content = "later"
	enqueueMutation(t, store, first)
	enqueueMutation(t, store, later)
	claim, err := store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claim) == 1, "claim=%+v err=%v", claim, err)
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`UPDATE observations SET sync_version=2 WHERE id='record'`)
		return err
	}())
	sequence := int64(1)
	testutil.NoError(t, store.ApplySyncPushResult(ctx, first.MutationID, claim[0].ClaimToken, syncservice.Result{MutationID: first.MutationID, Disposition: syncservice.DispositionConflict, Sequence: &sequence, Version: 2}))
	claims, err := store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 0, "pre-bootstrap claims=%+v err=%v", claims, err)
	conflict := specialChange(t, 1, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440084", first)
	discovery := syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}
	remote := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 1}, Changes: []syncservice.Change{conflict}}}}
	testutil.Require(t, errors.Is(store.BootstrapSync(ctx, remote), ErrConflict) && remote.discovers == 0, "ordinary bootstrap calls=%d", remote.discovers)
	testutil.NoError(t, store.BootstrapOwnConflict(ctx, remote, first.MutationID))
	var conflicts, inbox, cursor, outbox int
	var base int64
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts WHERE status='unresolved'`).Scan(&conflicts))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT base_version FROM sync_outbox WHERE mutation_id=?`, later.MutationID).Scan(&base))
	claims, err = store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 0, "unresolved claims=%+v err=%v", claims, err)
	testutil.Require(t, conflicts == 1 && inbox == 1 && cursor == 1 && outbox == 1 && base == 2, "rows=%d/%d/%d/%d base=%d", conflicts, inbox, cursor, outbox, base)
	winner := pulledObservationMutation("record", syncservice.MutationResolve, 2, syncservice.LifecycleActive, "winner", nil)
	winner.Observation, winner.Resolution = nil, &syncservice.Resolution{ConflictIDs: []string{conflict.ConflictID}, Observation: pulledObservationMutation("record", syncservice.MutationUpdate, 2, syncservice.LifecycleActive, "winner", nil).Observation}
	resolution := specialChange(t, 2, 3, syncservice.ChangeDispositionAccepted, "", winner)
	testutil.NoError(t, store.PullConflictResolutions(ctx, &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{resolution}}}}))
	var phase string
	var payload []byte
	var checkpoint BootstrapCheckpoint
	testutil.NoError(t, store.db.QueryRow(`SELECT phase,checkpoint FROM sync_bootstrap`).Scan(&phase, &payload))
	testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
	testutil.Require(t, phase == "complete" && checkpoint.Position == 2 && checkpoint.Watermark == 2, "checkpoint=%q/%+v", phase, checkpoint)
	claims, err = store.ClaimDueSyncOutbox(ctx, time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1 && claims[0].Mutation.MutationID == later.MutationID, "resolved claims=%+v err=%v", claims, err)
}

func TestPullConflictResolutionsPrefixAndFailureSafety(t *testing.T) {
	setup := func(t *testing.T) (*Store, string, string, syncservice.Mutation) {
		t.Helper()
		store := openTestStore(t)
		store.now = func() time.Time { return fixedTime }
		history, conflictID := "550e8400-e29b-41d4-a716-4466554400b1", "550e8400-e29b-41d4-a716-4466554400b2"
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',1); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,2)`, fixedTime.UnixNano(), fixedTime.UnixNano())
		testutil.NoError(t, err)
		_, err = store.db.Exec(`INSERT INTO sync_conflicts(conflict_id,history_id,created_seq,record_kind,record_id,canonical_version,competing_version_id,status,resolved_seq,payload_version,snapshot,created_at,updated_at) VALUES(?,?,1,'observation','record',2,'550e8400-e29b-41d4-a716-4466554400b3','unresolved',NULL,1,X'7B7D',?,?)`, conflictID, history, fixedTime.UnixNano(), fixedTime.UnixNano())
		testutil.NoError(t, err)
		winner := pulledObservationMutation("record", syncservice.MutationResolve, 2, syncservice.LifecycleActive, "winner", nil)
		winner.Observation, winner.Resolution = nil, &syncservice.Resolution{ConflictIDs: []string{conflictID}, Observation: pulledObservationMutation("record", syncservice.MutationUpdate, 2, syncservice.LifecycleActive, "winner", nil).Observation}
		return store, history, conflictID, winner
	}
	page := func(history string, position, watermark int64, changes ...syncservice.Change) syncservice.PullPage {
		return syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: position, Watermark: watermark}, HasMore: position < watermark, Changes: changes}
	}
	t.Run("safe prefix resumes without skipping", func(t *testing.T) {
		store, history, conflictID, winner := setup(t)
		block := syncMutation("550e8400-e29b-41d4-a716-4466554400b4", "blocked")
		enqueueMutation(t, store, block)
		prefix := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-4466554400b5", "prefix"))
		blocked := pulledChange(t, 2, 1, block)
		remote := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{page(history, 2, 3, prefix, blocked)}}
		err := store.PullConflictResolutions(context.Background(), remote)
		var cursor, inbox, unresolved, outbox int
		var phase string
		var checkpoint BootstrapCheckpoint
		var payload []byte
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts WHERE status='unresolved'`).Scan(&unresolved))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox WHERE mutation_id=?`, block.MutationID).Scan(&outbox))
		testutil.NoError(t, store.db.QueryRow(`SELECT checkpoint FROM sync_bootstrap`).Scan(&payload))
		testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
		testutil.Require(t, errors.Is(err, ErrConflict) && cursor == 1 && inbox == 1 && unresolved == 1 && outbox == 1 && checkpoint.Position == 1 && checkpoint.Watermark == 3, "err=%v rows=%d/%d/%d/%d checkpoint=%+v", err, cursor, inbox, unresolved, outbox, checkpoint)
		regression := pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-4466554400bc", "regression"))
		err = store.PullConflictResolutions(context.Background(), &bootstrapFake{discovery: remote.discovery, pages: []syncservice.PullPage{page(history, 2, 2, regression)}})
		testutil.Require(t, errors.Is(err, ErrConflict), "watermark regression error=%v", err)
		var projects int
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
		testutil.NoError(t, store.db.QueryRow(`SELECT checkpoint FROM sync_bootstrap`).Scan(&payload))
		testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
		testutil.Require(t, cursor == 1 && inbox == 1 && projects == 2 && checkpoint.Position == 1 && checkpoint.Watermark == 3, "regression cursor=%d inbox=%d projects=%d checkpoint=%+v", cursor, inbox, projects, checkpoint)
		testutil.NoError(t, func() error {
			_, err := store.db.Exec(`DELETE FROM sync_outbox WHERE mutation_id=?`, block.MutationID)
			return err
		}())
		resolve := specialChange(t, 3, 3, syncservice.ChangeDispositionAccepted, "", winner)
		remote = &bootstrapFake{discovery: remote.discovery, pages: []syncservice.PullPage{page(history, 3, 4, blocked, resolve)}}
		testutil.NoError(t, store.PullConflictResolutions(context.Background(), remote))
		testutil.NoError(t, store.db.QueryRow(`SELECT phase,checkpoint FROM sync_bootstrap`).Scan(&phase, &payload))
		testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
		testutil.Require(t, remote.cursors[0].HistoryID == history && remote.cursors[0].Position == 1 && phase == "observations" && checkpoint.Position == 3 && checkpoint.Watermark == 4 && conflictID != "", "cursor=%+v checkpoint=%q/%+v", remote.cursors, phase, checkpoint)
	})
	t.Run("complete checkpoint advances to observations prefix", func(t *testing.T) {
		store, history, _, _ := setup(t)
		initial := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-4466554400b9", "initial"))
		testutil.NoError(t, store.ApplyPulledPage(context.Background(), page(history, 1, 1, initial), &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 1, Phase: "complete"}))
		block := syncMutation("550e8400-e29b-41d4-a716-4466554400ba", "blocked")
		enqueueMutation(t, store, block)
		prefix := pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-4466554400bb", "prefix"))
		err := store.PullConflictResolutions(context.Background(), &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{page(history, 3, 3, prefix, pulledChange(t, 3, 1, block))}})
		var cursor int64
		var phase string
		var payload []byte
		var checkpoint BootstrapCheckpoint
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
		testutil.NoError(t, store.db.QueryRow(`SELECT phase,checkpoint FROM sync_bootstrap`).Scan(&phase, &payload))
		testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
		testutil.Require(t, errors.Is(err, ErrConflict) && cursor == 2 && phase == "observations" && checkpoint.Position == 2 && checkpoint.Watermark == 3, "err=%v cursor=%d checkpoint=%q/%+v", err, cursor, phase, checkpoint)
	})
	for name, prepare := range map[string]func(*Store, syncservice.Mutation) (syncservice.PullPage, context.Context){
		"blocker before resolution": func(store *Store, winner syncservice.Mutation) (syncservice.PullPage, context.Context) {
			block := syncMutation("550e8400-e29b-41d4-a716-4466554400b6", "blocked")
			enqueueMutation(t, store, block)
			return page("550e8400-e29b-41d4-a716-4466554400b1", 2, 2, pulledChange(t, 1, 1, block), specialChange(t, 2, 3, syncservice.ChangeDispositionAccepted, "", winner)), context.Background()
		},
		"invalid resolution rolls back": func(store *Store, winner syncservice.Mutation) (syncservice.PullPage, context.Context) {
			later := pulledObservationMutation("record", syncservice.MutationUpdate, 2, syncservice.LifecycleActive, "later", nil)
			later.MutationID = "550e8400-e29b-41d4-a716-4466554400b8"
			enqueueMutation(t, store, later)
			winner.Resolution.ConflictIDs = []string{"550e8400-e29b-41d4-a716-4466554400b7"}
			return page("550e8400-e29b-41d4-a716-4466554400b1", 1, 1, specialChange(t, 1, 3, syncservice.ChangeDispositionAccepted, "", winner)), context.Background()
		},
		"cancellation rolls back": func(_ *Store, winner syncservice.Mutation) (syncservice.PullPage, context.Context) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return page("550e8400-e29b-41d4-a716-4466554400b1", 1, 1, specialChange(t, 1, 3, syncservice.ChangeDispositionAccepted, "", winner)), ctx
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, history, _, winner := setup(t)
			pulled, ctx := prepare(store, winner)
			err := store.PullConflictResolutions(ctx, &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{pulled}})
			var conflicts, inbox, cursor, checkpoint int
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts WHERE status='unresolved'`).Scan(&conflicts))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_bootstrap`).Scan(&checkpoint))
			testutil.Require(t, (errors.Is(err, ErrConflict) || errors.Is(err, context.Canceled)) && conflicts == 1 && inbox == 0 && cursor == 0 && checkpoint == 0, "err=%v rows=%d/%d/%d/%d", err, conflicts, inbox, cursor, checkpoint)
			if name == "invalid resolution rolls back" {
				var base int64
				testutil.NoError(t, store.db.QueryRow(`SELECT base_version FROM sync_outbox WHERE mutation_id='550e8400-e29b-41d4-a716-4466554400b8'`).Scan(&base))
				testutil.Require(t, base == 2, "rebased outbox base=%d", base)
			}
		})
	}
}

func TestRebasePendingConflictOutboxConvertsArchivedCreateToArchive(t *testing.T) {
	store := openTestStore(t)
	archived := pulledObservationMutation("record", syncservice.MutationCreate, 0, syncservice.LifecycleArchived, "later", nil)
	archived.MutationID = "550e8400-e29b-41d4-a716-446655440087"
	enqueueMutation(t, store, archived)
	tx, err := store.db.BeginTx(context.Background(), nil)
	testutil.NoError(t, err)
	defer tx.Rollback()
	testutil.NoError(t, store.rebasePendingConflictOutbox(context.Background(), tx, archived, 2))
	testutil.NoError(t, tx.Commit())
	var kind string
	var payload []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT mutation_kind,payload FROM sync_outbox WHERE mutation_id=?`, archived.MutationID).Scan(&kind, &payload))
	var rebased syncservice.Mutation
	testutil.NoError(t, json.Unmarshal(payload, &rebased))
	testutil.Require(t, kind == string(syncservice.MutationArchive) && rebased.Kind == syncservice.MutationArchive && rebased.BaseVersion == 2, "rebased=%q/%+v", kind, rebased)
}

func insertConflictReceipt(t *testing.T, store *Store, mutation syncservice.Mutation, sequence, version int64, hash []byte) {
	t.Helper()
	if hash == nil {
		var err error
		hash, err = syncMutationHash(mutation)
		testutil.NoError(t, err)
	}
	_, err := store.db.Exec(`INSERT INTO sync_push_results(mutation_id,disposition,retryable,code,sequence,canonical_version,record_kind,record_id,mutation_kind,base_version,mutation_hash,completed_at) VALUES(?,'conflict',0,'',?,?,?,?,?,?,?,?)`, mutation.MutationID, sequence, version, mutation.RecordKind, mutation.RecordID, mutation.Kind, mutation.BaseVersion, hash, fixedTime.UnixNano())
	testutil.NoError(t, err)
}

func TestBootstrapOwnConflictFailsClosedOnReceiptMismatchOrAbsentTarget(t *testing.T) {
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440088"
	for name, mutate := range map[string]func(*syncservice.Change, *Store){
		"forged hash": func(_ *syncservice.Change, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_push_results SET mutation_hash=zeroblob(32)`)
			testutil.NoError(t, err)
		},
		"wrong sequence": func(_ *syncservice.Change, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_push_results SET sequence=2`)
			testutil.NoError(t, err)
		},
		"wrong record": func(_ *syncservice.Change, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_push_results SET record_id='other'`)
			testutil.NoError(t, err)
		},
		"wrong version": func(_ *syncservice.Change, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_push_results SET canonical_version=3`)
			testutil.NoError(t, err)
		},
		"wrong disposition": func(_ *syncservice.Change, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_push_results SET disposition='accepted'`)
			testutil.NoError(t, err)
		},
		"target absent": func(change *syncservice.Change, _ *Store) {
			change.Mutation.MutationID = "550e8400-e29b-41d4-a716-446655440089"
			change.ChangeHash, _ = syncservice.CanonicalChangeHash(*change)
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			store.now = func() time.Time { return fixedTime }
			testutil.NoError(t, func() error {
				_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',1); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,2)`, fixedTime.UnixNano(), fixedTime.UnixNano())
				return err
			}())
			mutation := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "first", nil)
			mutation.MutationID = "550e8400-e29b-41d4-a716-446655440090"
			insertConflictReceipt(t, store, mutation, 1, 2, nil)
			change := specialChange(t, 1, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440091", mutation)
			mutate(&change, store)
			remote := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 1}, Changes: []syncservice.Change{change}}}}
			err := store.BootstrapOwnConflict(ctx, remote, mutation.MutationID)
			var inbox, cursor, conflicts int
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts`).Scan(&conflicts))
			testutil.Require(t, errors.Is(err, ErrConflict) && inbox == 0 && cursor == 0 && conflicts == 0, "err=%v rows=%d/%d/%d", err, inbox, cursor, conflicts)
		})
	}
}

func TestBootstrapOwnConflictRejectsUnrelatedPendingBeforeTargetWithoutWrites(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	target := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "target", nil)
	target.MutationID = "550e8400-e29b-41d4-a716-446655440092"
	insertConflictReceipt(t, store, target, 2, 2, nil)
	pending := syncMutation("550e8400-e29b-41d4-a716-446655440093", "other")
	enqueueMutation(t, store, pending)
	first := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440094", "other"))
	conflict := specialChange(t, 2, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440095", target)
	history := "550e8400-e29b-41d4-a716-446655440096"
	remote := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{first, conflict}}}}
	err := store.BootstrapOwnConflict(ctx, remote, target.MutationID)
	var inbox, cursor, conflicts, outbox int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts`).Scan(&conflicts))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.Require(t, errors.Is(err, ErrConflict) && inbox == 0 && cursor == 0 && conflicts == 0 && outbox == 1, "err=%v rows=%d/%d/%d/%d", err, inbox, cursor, conflicts, outbox)
}

func TestBootstrapOwnConflictApplyFailureRollsBackConflictCursorAndRebase(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',1); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,2); CREATE TRIGGER fail_conflict BEFORE INSERT ON sync_conflicts BEGIN SELECT RAISE(ABORT, 'test'); END`, fixedTime.UnixNano(), fixedTime.UnixNano())
		return err
	}())
	first := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "first", nil)
	first.MutationID = "550e8400-e29b-41d4-a716-446655440097"
	later := first
	later.MutationID = "550e8400-e29b-41d4-a716-446655440098"
	enqueueMutation(t, store, later)
	insertConflictReceipt(t, store, first, 1, 2, nil)
	conflict := specialChange(t, 1, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440099", first)
	history := "550e8400-e29b-41d4-a716-446655440100"
	remote := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 1}, Changes: []syncservice.Change{conflict}}}}
	err := store.BootstrapOwnConflict(ctx, remote, first.MutationID)
	var inbox, cursor, conflicts int
	var base int64
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts`).Scan(&conflicts))
	testutil.NoError(t, store.db.QueryRow(`SELECT base_version FROM sync_outbox WHERE mutation_id=?`, later.MutationID).Scan(&base))
	testutil.Require(t, errors.Is(err, ErrCorrupt) && inbox == 0 && cursor == 0 && conflicts == 0 && base == 1, "err=%v rows=%d/%d/%d base=%d", err, inbox, cursor, conflicts, base)
}

func TestBootstrapOwnConflictCancellationBeforeApplyLeavesNoDurableState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := openTestStore(t)
	mutation := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "target", nil)
	mutation.MutationID = "550e8400-e29b-41d4-a716-446655440101"
	insertConflictReceipt(t, store, mutation, 1, 2, nil)
	change := specialChange(t, 1, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440102", mutation)
	history := "550e8400-e29b-41d4-a716-446655440103"
	remote := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 1}, Changes: []syncservice.Change{change}}}, pullProbe: func() error { cancel(); return nil }}
	err := store.BootstrapOwnConflict(ctx, remote, mutation.MutationID)
	var inbox, cursor, conflicts int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts`).Scan(&conflicts))
	testutil.Require(t, errors.Is(err, context.Canceled) && inbox == 0 && cursor == 0 && conflicts == 0, "err=%v rows=%d/%d/%d", err, inbox, cursor, conflicts)
}

func TestBootstrapOwnConflictResumesAndUpsertsExistingCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	history := "550e8400-e29b-41d4-a716-446655440104"
	project := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440105", "project"))
	first := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}, HasMore: true, Changes: []syncservice.Change{project}}
	testutil.NoError(t, store.ApplyPulledPage(ctx, first, &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 2, Phase: "projects"}))
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,2)`, fixedTime.UnixNano(), fixedTime.UnixNano())
		return err
	}())
	mutation := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "target", nil)
	mutation.MutationID = "550e8400-e29b-41d4-a716-446655440106"
	insertConflictReceipt(t, store, mutation, 2, 2, nil)
	conflict := specialChange(t, 2, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440107", mutation)
	remote := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{conflict}}}}
	testutil.NoError(t, store.BootstrapOwnConflict(ctx, remote, mutation.MutationID))
	var phase string
	var checkpoint BootstrapCheckpoint
	var payload []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT phase,checkpoint FROM sync_bootstrap`).Scan(&phase, &payload))
	testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
	testutil.Require(t, phase == "complete" && checkpoint.Position == 2 && checkpoint.Watermark == 2 && remote.cursors[0] == (syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}), "phase=%q checkpoint=%+v cursor=%+v", phase, checkpoint, remote.cursors)
}

func TestBootstrapOwnConflictRejectsMissingAndNonConflictReceiptsBeforeNetwork(t *testing.T) {
	ctx := context.Background()
	discovery := syncservice.Discovery{ProtocolVersion: 1, HistoryID: "550e8400-e29b-41d4-a716-446655440085", Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}
	for name, setup := range map[string]func(*Store){
		"missing": func(*Store) {},
		"non-conflict": func(store *Store) {
			_, err := store.db.Exec(`INSERT INTO sync_push_results(mutation_id,disposition,retryable,code,sequence,canonical_version,record_kind,record_id,mutation_kind,base_version,mutation_hash,completed_at) VALUES(?,'accepted',0,'',1,1,'project','record','create',0,zeroblob(32),?)`, "550e8400-e29b-41d4-a716-446655440086", fixedTime.UnixNano())
			testutil.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			setup(store)
			remote := &bootstrapFake{discovery: discovery}
			err := store.BootstrapOwnConflict(ctx, remote, "550e8400-e29b-41d4-a716-446655440086")
			testutil.Require(t, errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict), "err=%v", err)
			testutil.Require(t, remote.discovers == 0 && remote.pulls == 0, "network calls=%d/%d", remote.discovers, remote.pulls)
		})
	}
}

func TestBootstrapOwnConflictRejectsEmptyEOFAtTargetWithoutWrites(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	mutation := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "target", nil)
	mutation.MutationID = "550e8400-e29b-41d4-a716-446655440108"
	insertConflictReceipt(t, store, mutation, 1, 2, nil)
	history := "550e8400-e29b-41d4-a716-446655440109"
	remote := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 1}}}}
	err := store.BootstrapOwnConflict(ctx, remote, mutation.MutationID)
	var inbox, cursor, checkpoint, conflicts int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_bootstrap`).Scan(&checkpoint))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts`).Scan(&conflicts))
	testutil.Require(t, errors.Is(err, ErrConflict) && inbox == 0 && cursor == 0 && checkpoint == 0 && conflicts == 0, "err=%v rows=%d/%d/%d/%d", err, inbox, cursor, checkpoint, conflicts)
}

func TestBootstrapOwnConflictCheckpointsPrefixBeforeTargetAndResumes(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',1); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('record','project','project','learning','local','test','active',?,?,2)`, fixedTime.UnixNano(), fixedTime.UnixNano())
		return err
	}())
	history := "550e8400-e29b-41d4-a716-446655440110"
	target := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "target", nil)
	target.MutationID = "550e8400-e29b-41d4-a716-446655440111"
	insertConflictReceipt(t, store, target, 2, 2, nil)
	prefix := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440112", "prefix-project"))
	conflict := specialChange(t, 2, 2, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440113", target)
	discovery := syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}
	interrupted := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}, HasMore: true, Changes: []syncservice.Change{prefix}}}, errAt: 2}
	testutil.Require(t, interrupted != nil && store.BootstrapOwnConflict(ctx, interrupted, target.MutationID) != nil, "bootstrap unexpectedly completed")
	var checkpoint BootstrapCheckpoint
	var phase string
	var payload []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT phase,checkpoint FROM sync_bootstrap`).Scan(&phase, &payload))
	testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
	testutil.Require(t, phase == "observations" && checkpoint.Position == 1 && checkpoint.Watermark == 2, "checkpoint=%q/%+v", phase, checkpoint)
	resume := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{conflict}}}}
	testutil.NoError(t, store.BootstrapOwnConflict(ctx, resume, target.MutationID))
	var inbox, cursor, conflicts int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_conflicts WHERE status='unresolved'`).Scan(&conflicts))
	testutil.Require(t, resume.cursors[0] == (syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}) && inbox == 2 && cursor == 2 && conflicts == 1, "cursor=%+v rows=%d/%d/%d", resume.cursors, inbox, cursor, conflicts)
}

func TestRebasePendingConflictOutboxHonorsClaimLease(t *testing.T) {
	ctx := context.Background()
	for name, leaseOffset := range map[string]time.Duration{"expired claim rebases": -time.Second, "active claim blocks": time.Minute} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			store.now = func() time.Time { return fixedTime }
			mutation := pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "later", nil)
			mutation.MutationID = "550e8400-e29b-41d4-a716-446655440114"
			enqueueMutation(t, store, mutation)
			_, err := store.db.Exec(`INSERT INTO sync_outbox_claims(mutation_id,first_claim_token,claim_token,first_claimed_at,claimed_at,lease_until) VALUES(?,?,?,?,?,?)`, mutation.MutationID, "550e8400-e29b-41d4-a716-446655440115", "550e8400-e29b-41d4-a716-446655440116", fixedTime.Add(-2*time.Minute).UnixNano(), fixedTime.Add(-time.Minute).UnixNano(), fixedTime.Add(leaseOffset).UnixNano())
			testutil.NoError(t, err)
			tx, err := store.db.BeginTx(ctx, nil)
			testutil.NoError(t, err)
			err = store.rebasePendingConflictOutbox(ctx, tx, mutation, 2)
			if leaseOffset < 0 {
				testutil.NoError(t, err)
				testutil.NoError(t, tx.Commit())
				var base int64
				testutil.NoError(t, store.db.QueryRow(`SELECT base_version FROM sync_outbox WHERE mutation_id=?`, mutation.MutationID).Scan(&base))
				testutil.Require(t, base == 2, "base=%d", base)
				return
			}
			defer tx.Rollback()
			testutil.Require(t, errors.Is(err, ErrConflict), "err=%v", err)
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
	testutil.Require(t, err == nil && len(entries) == 2 && entries[0].MutationID == first.MutationID && entries[0].RecordID == "project-a" && entries[1].MutationID == returned.MutationID, "entries=%+v err=%v", entries, err)
	legacy := syncMutation("550e8400-e29b-41d4-a716-446655440006", strings.Repeat("a", 513))
	enqueueMutation(t, store, legacy)
	entries, err = store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 3 && entries[2].RecordID == legacy.RecordID, "legacy entries=%+v err=%v", entries, err)
}

func TestSyncOutboxRetryStateTransitions(t *testing.T) {
	store := openTestStore(t)
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440003", "project")
	enqueueMutation(t, store, mutation)
	next := fixedTime.Add(time.Minute)
	_, err := store.db.Exec(`UPDATE sync_outbox SET state='retry',attempts=1,next_attempt_at=?,last_error_code='temporary' WHERE mutation_id=?`, next.UnixNano(), mutation.MutationID)
	testutil.NoError(t, err)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 0, "premature entries=%+v err=%v", entries, err)
	entries, err = store.DueSyncOutbox(context.Background(), next)
	testutil.Require(t, err == nil && len(entries) == 1 && entries[0].Attempts == 1 && entries[0].State == SyncOutboxRetry && entries[0].LastErrorCode == "temporary" && entries[0].MutationID == mutation.MutationID, "entries=%+v err=%v", entries, err)
}

func TestClaimDueSyncOutboxLeasesOldestAndRotatesExpiredToken(t *testing.T) {
	store := openTestStore(t)
	now := fixedTime
	store.now = func() time.Time { return now }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	first := syncMutation("550e8400-e29b-41d4-a716-446655440026", "project")
	second := syncMutation("550e8400-e29b-41d4-a716-446655440027", "project")
	enqueueMutation(t, store, first)
	enqueueMutation(t, store, second)
	claims, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 16)
	testutil.Require(t, err == nil && len(claims) == 1 && claims[0].Mutation.MutationID == first.MutationID && canonicalUUIDPattern.MatchString(claims[0].ClaimToken), "claims=%+v err=%v", claims, err)
	again, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 16)
	testutil.Require(t, err == nil && len(again) == 0, "active lease=%+v err=%v", again, err)
	now = fixedTime.Add(time.Minute)
	rotated, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 16)
	firstPayload, _ := json.Marshal(claims[0].Mutation)
	rotatedPayload, _ := json.Marshal(rotated[0].Mutation)
	testutil.Require(t, err == nil && len(rotated) == 1 && string(rotatedPayload) == string(firstPayload) && rotated[0].ClaimToken != claims[0].ClaimToken && rotated[0].FirstClaimToken == claims[0].ClaimToken && rotated[0].FirstClaimedAt.Equal(fixedTime), "rotated=%+v err=%v", rotated, err)
}

func TestClaimDueSyncOutboxForProjectDoesNotLeaseOtherProjectRelations(t *testing.T) {
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	ctx := context.Background()
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('a',0),('b',0);
		INSERT INTO sessions(id,project_id,sync_version) VALUES('a-session','a',0),('b-session','b',0);
		INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES
		('a-observation','a','project','learning','a','test','active',?,?,0),
		('b-observation','b','project','learning','b','test','active',?,?,0)`, fixedTime.UnixNano(), fixedTime.UnixNano(), fixedTime.UnixNano(), fixedTime.UnixNano())
	testutil.NoError(t, err)
	mutations := []syncservice.Mutation{
		syncMutation("550e8400-e29b-41d4-a716-446655440301", "a"),
		{MutationID: "550e8400-e29b-41d4-a716-446655440302", RecordID: "a-session", RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: "a-session", ProjectID: "a"}},
		pulledObservationMutation("a-observation", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "a", nil),
		syncMutation("550e8400-e29b-41d4-a716-446655440304", "b"),
		{MutationID: "550e8400-e29b-41d4-a716-446655440305", RecordID: "b-session", RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: "b-session", ProjectID: "b"}},
		pulledObservationMutation("b-observation", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "b", nil),
	}
	mutations[2].MutationID = "550e8400-e29b-41d4-a716-446655440303"
	mutations[2].Observation.ProjectID = "a"
	mutations[5].MutationID = "550e8400-e29b-41d4-a716-446655440306"
	mutations[5].Observation.ProjectID = "b"
	for _, mutation := range mutations {
		enqueueMutation(t, store, mutation)
	}
	claims, err := store.ClaimDueSyncOutboxForProject(ctx, time.Minute, 16, "a")
	testutil.Require(t, err == nil && len(claims) == 3, "claims=%+v err=%v", claims, err)
	for _, claim := range claims {
		testutil.Require(t, claim.Mutation.RecordID != "b" && claim.Mutation.RecordID != "b-session" && claim.Mutation.RecordID != "b-observation", "claimed project b mutation=%+v", claim)
	}
	var bClaims int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox_claims c JOIN sync_outbox o ON o.mutation_id=c.mutation_id WHERE o.record_id IN ('b','b-session','b-observation')`).Scan(&bClaims))
	testutil.Require(t, bClaims == 0, "project b claims=%d", bClaims)
}

func TestTranslateSyncMutationsUsesDeterministicPortableIDs(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0); INSERT INTO sessions(id,project_id,sync_version) VALUES('session','project',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','project','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`)
	testutil.NoError(t, err)
	local := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440302", RecordID: "session", RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: "session", ProjectID: "project"}}
	got, err := store.TranslateSyncMutations(ctx, "550e8400-e29b-41d4-a716-446655440001", "project", []syncservice.Mutation{local})
	testutil.NoError(t, err)
	testutil.Require(t, len(got) == 1 && got[0].RecordID != local.RecordID && got[0].Session.ID == got[0].RecordID && got[0].Session.ProjectID != local.Session.ProjectID, "translated=%+v", got)
	testutil.Require(t, local.RecordID == "session" && local.Session.ID == "session" && local.Session.ProjectID == "project", "local=%+v", local)
	var mappings int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappings))
	testutil.Require(t, mappings == 1, "mappings=%d", mappings)
	_, err = store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_portable_identities SET origin_device_id='zzzzzzzz-zzzz-4zzz-8zzz-zzzzzzzzzzzz'; PRAGMA ignore_check_constraints=OFF`)
	testutil.NoError(t, err)
	_, err = store.TranslateSyncMutations(ctx, "550e8400-e29b-41d4-a716-446655440001", "project", []syncservice.Mutation{local})
	testutil.Require(t, errors.Is(err, ErrCorrupt), "tampered session err=%v", err)
}

func TestAdoptSyncPortableIdentityRetainsInboundWireID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	ctx := context.Background()
	portableProject, wireID := "550e8400-e29b-41d4-a716-446655440001", "550e8400-e29b-41d4-a716-446655440302"
	deviceA, deviceB := "550e8400-e29b-41d4-a716-446655440099", "550e8400-e29b-41d4-a716-446655440098"
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','local','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`)
	testutil.NoError(t, err)
	local, err := store.AdoptSyncPortableIdentity(ctx, portableProject, "local", syncservice.RecordKindSession, wireID)
	testutil.NoError(t, err)
	testutil.NoError(t, store.Close())
	store = openPath(t, path)
	defer store.Close()
	_, err = store.db.Exec(`UPDATE sync_profiles SET device_id=?`, deviceB)
	testutil.NoError(t, err)
	again, err := store.AdoptSyncPortableIdentity(ctx, portableProject, "local", syncservice.RecordKindSession, wireID)
	testutil.Require(t, err == nil && again == local, "local=%q again=%q err=%v", local, again, err)
	got, found, err := store.LocalSyncPortableIdentity(ctx, portableProject, syncservice.RecordKindSession, wireID)
	testutil.Require(t, err == nil && found && got == local, "local=%q got=%q found=%t err=%v", local, got, found, err)
	var origin, adopter string
	testutil.NoError(t, store.db.QueryRow(`SELECT i.origin_device_id,a.adopting_device_id FROM sync_portable_identities i JOIN sync_portable_identity_adoptions a USING(portable_project_id,record_kind,local_id)`).Scan(&origin, &adopter))
	_, err = store.db.Exec(`INSERT INTO sessions(id,project_id,sync_version) VALUES(?, 'local', 0)`, local)
	testutil.NoError(t, err)
	mutation := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440303", RecordID: local, RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: local, ProjectID: "local"}}
	translated, err := store.TranslateSyncMutations(ctx, portableProject, "local", []syncservice.Mutation{mutation})
	testutil.Require(t, err == nil && origin == deviceA && adopter == deviceA && len(translated) == 1 && translated[0].RecordID == wireID, "translated=%+v origin=%q adopter=%q err=%v", translated, origin, adopter, err)
}

func TestAdoptSyncPortableIdentityFailsClosedAndIsIdempotent(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	project := "550e8400-e29b-41d4-a716-446655440001"
	wire := "550e8400-e29b-41d4-a716-446655440302"
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local',0),('other',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES(?,?,?,?,?); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`, project, "local", "hash", "test", "now")
	testutil.NoError(t, err)
	local, err := store.AdoptSyncPortableIdentity(ctx, project, "local", syncservice.RecordKindObservation, wire)
	again, retryErr := store.AdoptSyncPortableIdentity(ctx, project, "local", syncservice.RecordKindObservation, wire)
	testutil.Require(t, err == nil && retryErr == nil && local == again, "local=%q again=%q errors=%v/%v", local, again, err, retryErr)
	_, err = store.AdoptSyncPortableIdentity(ctx, project, "other", syncservice.RecordKindObservation, wire)
	testutil.Require(t, errors.Is(err, ErrConflict), "cross-project err=%v", err)
	_, err = store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_portable_identity_adoptions SET portable_id='550e8400-e29b-41d4-a716-446655440303'; PRAGMA ignore_check_constraints=OFF`)
	testutil.NoError(t, err)
	_, _, err = store.LocalSyncPortableIdentity(ctx, project, syncservice.RecordKindObservation, wire)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "tampered adoption err=%v", err)
}

func TestAdoptSyncPortableIdentityRejectsCollisionsAndDeviceMismatch(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	projectA, projectB, wire := "550e8400-e29b-41d4-a716-446655440001", "550e8400-e29b-41d4-a716-446655440002", "550e8400-e29b-41d4-a716-446655440302"
	deviceA, deviceB := "550e8400-e29b-41d4-a716-446655440099", "550e8400-e29b-41d4-a716-446655440098"
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local-a',0),('local-b',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES(?,?,?,?,?),(?,?,?,?,?); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test',?,'secret://keychain/sync/test',1,1)`, projectA, "local-a", "hash-a", "test", "now", projectB, "local-b", "hash-b", "test", "now", deviceA)
	testutil.NoError(t, err)
	local := adoptedSyncLocalID(syncservice.RecordKindSession, wire)
	_, err = store.db.Exec(`INSERT INTO sessions(id,project_id,sync_version) VALUES(?,'local-a',0)`, local)
	testutil.NoError(t, err)
	_, err = store.AdoptSyncPortableIdentity(context.Background(), projectA, "local-a", syncservice.RecordKindSession, wire)
	testutil.Require(t, errors.Is(err, ErrConflict), "local collision err=%v", err)
	var mappings, adoptions int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappings))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identity_adoptions`).Scan(&adoptions))
	testutil.Require(t, mappings == 0 && adoptions == 0, "partial local collision rows=%d/%d", mappings, adoptions)
	_, err = store.AdoptSyncPortableIdentity(context.Background(), projectA, "local-a", syncservice.RecordKindSession, wire)
	testutil.Require(t, errors.Is(err, ErrConflict), "occupied retry err=%v", err)
	_, err = store.db.Exec(`DELETE FROM sessions WHERE id=?`, local)
	testutil.NoError(t, err)
	_, err = store.AdoptSyncPortableIdentity(context.Background(), projectA, "local-a", syncservice.RecordKindSession, wire)
	testutil.NoError(t, err)
	_, err = store.AdoptSyncPortableIdentity(context.Background(), projectB, "local-b", syncservice.RecordKindSession, wire)
	testutil.Require(t, errors.Is(err, ErrConflict), "cross-project wire err=%v", err)
	_, err = store.AdoptSyncPortableIdentity(context.Background(), projectA, "local-a", syncservice.RecordKindObservation, wire)
	testutil.Require(t, errors.Is(err, ErrConflict), "cross-kind wire err=%v", err)
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappings))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identity_adoptions`).Scan(&adoptions))
	testutil.Require(t, mappings == 1 && adoptions == 1, "partial wire collision rows=%d/%d", mappings, adoptions)
	_, err = store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_portable_identity_adoptions SET adopting_device_id=?; PRAGMA ignore_check_constraints=OFF`, deviceB)
	testutil.NoError(t, err)
	_, _, err = store.LocalSyncPortableIdentity(context.Background(), projectA, syncservice.RecordKindSession, wire)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "device mismatch err=%v", err)
}

func TestAdoptSyncPortableIdentityRejectsInvalidProfileWithoutWrites(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(t *testing.T, store *Store)
		want   error
	}{
		{"absent", func(t *testing.T, store *Store) {
			_, err := store.db.Exec(`DELETE FROM sync_profiles`)
			testutil.NoError(t, err)
		}, ErrNotFound},
		{"disabled", func(t *testing.T, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_profiles SET enabled=0`)
			testutil.NoError(t, err)
		}, ErrConflict},
		{"invalid enabled", func(t *testing.T, store *Store) {
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_profiles SET enabled=2; PRAGMA ignore_check_constraints=OFF`)
			testutil.NoError(t, err)
		}, ErrCorrupt},
		{"endpoint", func(t *testing.T, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_profiles SET endpoint='http://example.test'`)
			testutil.NoError(t, err)
		}, ErrCorrupt},
		{"credential", func(t *testing.T, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_profiles SET credential_ref='not-a-reference'`)
			testutil.NoError(t, err)
		}, ErrCorrupt},
		{"previous credential", func(t *testing.T, store *Store) {
			_, err := store.db.Exec(`UPDATE sync_profiles SET previous_credential_ref='not-a-reference'`)
			testutil.NoError(t, err)
		}, ErrCorrupt},
		{"timestamps", func(t *testing.T, store *Store) {
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_profiles SET updated_at=0; PRAGMA ignore_check_constraints=OFF`)
			testutil.NoError(t, err)
		}, ErrCorrupt},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store := openTestStore(t)
			defer store.Close()
			project := "550e8400-e29b-41d4-a716-446655440001"
			_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','local','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`)
			testutil.NoError(t, err)
			scenario.mutate(t, store)
			_, err = store.AdoptSyncPortableIdentity(context.Background(), project, "local", syncservice.RecordKindSession, "550e8400-e29b-41d4-a716-446655440302")
			var mappings, adoptions int
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappings))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identity_adoptions`).Scan(&adoptions))
			testutil.Require(t, errors.Is(err, scenario.want) && mappings == 0 && adoptions == 0, "err=%v rows=%d/%d", err, mappings, adoptions)
		})
	}
}

func TestAdoptSyncPortableIdentityTwoHandleRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first := openPath(t, path)
	defer first.Close()
	project := "550e8400-e29b-41d4-a716-446655440001"
	wire := "550e8400-e29b-41d4-a716-446655440302"
	_, err := first.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES(?,?,?,?,?); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`, project, "local", "hash", "test", "now")
	testutil.NoError(t, err)
	second := openPath(t, path)
	defer second.Close()
	start := make(chan struct{})
	results := make(chan struct {
		local string
		err   error
	}, 2)
	for _, candidate := range []*Store{first, second} {
		go func(candidate *Store) {
			<-start
			local, err := candidate.AdoptSyncPortableIdentity(context.Background(), project, "local", syncservice.RecordKindSession, wire)
			results <- struct {
				local string
				err   error
			}{local, err}
		}(candidate)
	}
	close(start)
	left, right := <-results, <-results
	var mappings, adoptions int
	var origin, adopter string
	testutil.NoError(t, first.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappings))
	testutil.NoError(t, first.db.QueryRow(`SELECT count(*) FROM sync_portable_identity_adoptions`).Scan(&adoptions))
	testutil.NoError(t, first.db.QueryRow(`SELECT i.origin_device_id,a.adopting_device_id FROM sync_portable_identities i JOIN sync_portable_identity_adoptions a USING(portable_project_id,record_kind,local_id)`).Scan(&origin, &adopter))
	testutil.Require(t, left.err == nil && right.err == nil && left.local == right.local && mappings == 1 && adoptions == 1 && origin == adopter, "left=%+v right=%+v mappings=%d adoptions=%d origin=%q adopter=%q", left, right, mappings, adoptions, origin, adopter)
}

func TestTranslateSyncMutationTombstoneRequiresRetainedIdentity(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('observation','project','project','learning','x','test','active',1,1,0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','project','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`)
	testutil.NoError(t, err)
	project := "550e8400-e29b-41d4-a716-446655440001"
	create := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440303", RecordID: "observation", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationCreate, Observation: &syncservice.Observation{ID: "observation", ProjectID: "project", Scope: "project", Type: "learning", Content: "x", Provenance: syncservice.Provenance{Producer: "test"}, Lifecycle: syncservice.LifecycleActive, Review: syncservice.ReviewClear, CreatedAt: fixedTime, UpdatedAt: fixedTime}}
	translated, err := store.TranslateSyncMutations(ctx, project, "project", []syncservice.Mutation{create})
	testutil.NoError(t, err)
	tombstone := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440304", RecordID: "observation", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}}
	got, err := store.TranslateSyncMutations(ctx, project, "project", []syncservice.Mutation{tombstone})
	testutil.Require(t, err == nil && got[0].RecordID == translated[0].RecordID, "got=%+v translated=%+v err=%v", got, translated, err)
	local, found, err := store.LocalSyncPortableIdentity(ctx, project, syncservice.RecordKindObservation, translated[0].RecordID)
	testutil.Require(t, err == nil && found && local == "observation", "local=%q found=%t err=%v", local, found, err)
	_, err = store.db.Exec(`UPDATE sync_portable_identities SET origin_device_id='zzzzzzzz-zzzz-4zzz-8zzz-zzzzzzzzzzzz' WHERE portable_project_id=?`, project)
	testutil.Require(t, err != nil, "malformed origin accepted")
	_, err = store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_portable_identities SET origin_device_id='zzzzzzzz-zzzz-4zzz-8zzz-zzzzzzzzzzzz' WHERE portable_project_id='550e8400-e29b-41d4-a716-446655440001'; PRAGMA ignore_check_constraints=OFF`)
	testutil.NoError(t, err)
	_, err = store.TranslateSyncMutations(ctx, project, "project", []syncservice.Mutation{tombstone})
	testutil.Require(t, errors.Is(err, ErrCorrupt), "tampered mapping err=%v", err)
	_, _, err = store.LocalSyncPortableIdentity(ctx, project, syncservice.RecordKindObservation, translated[0].RecordID)
	testutil.Require(t, errors.Is(err, ErrCorrupt), "tampered inverse err=%v", err)
	_, err = store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_portable_identities SET origin_device_id='550e8400-e29b-41d4-a716-446655440099',portable_id='550e8400-e29b-41d4-a716-446655440098' WHERE portable_project_id='550e8400-e29b-41d4-a716-446655440001'; PRAGMA ignore_check_constraints=OFF`)
	testutil.NoError(t, err)
	_, err = store.TranslateSyncMutations(ctx, project, "project", []syncservice.Mutation{tombstone})
	testutil.Require(t, errors.Is(err, ErrCorrupt), "tampered portable err=%v", err)
	var retained string
	testutil.NoError(t, store.db.QueryRow(`SELECT portable_id FROM sync_portable_identities WHERE portable_project_id=?`, project).Scan(&retained))
	testutil.Require(t, retained == "550e8400-e29b-41d4-a716-446655440098", "portable overwritten=%q", retained)
	var mappingsBefore, adoptionsBefore, mappingsAfter, adoptionsAfter int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappingsBefore))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identity_adoptions`).Scan(&adoptionsBefore))
	_, err = store.TranslateSyncMutations(ctx, project, "project", []syncservice.Mutation{{MutationID: "550e8400-e29b-41d4-a716-446655440305", RecordID: "missing", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}}})
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappingsAfter))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identity_adoptions`).Scan(&adoptionsAfter))
	testutil.Require(t, errors.Is(err, ErrNotFound) && mappingsBefore == mappingsAfter && adoptionsBefore == adoptionsAfter, "err=%v mappings=%d/%d adoptions=%d/%d", err, mappingsBefore, mappingsAfter, adoptionsBefore, adoptionsAfter)
}

func TestTranslateSyncMutationsScopesSameLocalIDByPortableProject(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('a',0),('b',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','a','hash-a','test','now'),('550e8400-e29b-41d4-a716-446655440002','b','hash-b','test','now'); INSERT INTO sync_portable_identities(portable_project_id,record_kind,local_id,portable_id,origin_device_id,created_at) VALUES('550e8400-e29b-41d4-a716-446655440001','session','same','550e8400-e29b-41d4-a716-446655440010','550e8400-e29b-41d4-a716-446655440099',1),('550e8400-e29b-41d4-a716-446655440002','session','same','550e8400-e29b-41d4-a716-446655440011','550e8400-e29b-41d4-a716-446655440099',1)`)
	testutil.NoError(t, err)
	var mappings int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities WHERE record_kind='session' AND local_id='same'`).Scan(&mappings))
	testutil.Require(t, mappings == 2, "mappings=%d", mappings)
}

func TestTranslateSyncMutationsReopenRetainsWireIDAndFirstDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	ctx := context.Background()
	project, deviceA, deviceB := "550e8400-e29b-41d4-a716-446655440001", "550e8400-e29b-41d4-a716-446655440099", "550e8400-e29b-41d4-a716-446655440098"
	_, err := store.db.Exec(fmt.Sprintf(`INSERT INTO projects(id,sync_version) VALUES('local',0); INSERT INTO sessions(id,project_id,sync_version) VALUES('session','local',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('%s','local','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','%s','secret://keychain/sync/test',1,1)`, project, deviceA))
	testutil.NoError(t, err)
	local := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440307", RecordID: "session", RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: "session", ProjectID: "local"}}
	first, err := store.TranslateSyncMutations(ctx, project, "local", []syncservice.Mutation{local})
	testutil.NoError(t, err)
	firstJSON, _ := json.Marshal(first)
	var origin string
	testutil.NoError(t, store.db.QueryRow(`SELECT origin_device_id FROM sync_portable_identities`).Scan(&origin))
	testutil.NoError(t, store.Close())
	store = openPath(t, path)
	defer store.Close()
	_, err = store.db.Exec(`UPDATE sync_profiles SET device_id=?`, deviceB)
	testutil.NoError(t, err)
	second, err := store.TranslateSyncMutations(ctx, project, "local", []syncservice.Mutation{local})
	secondJSON, _ := json.Marshal(second)
	inverse, found, inverseErr := store.LocalSyncPortableIdentity(ctx, project, syncservice.RecordKindSession, first[0].RecordID)
	var reopenedOrigin string
	testutil.NoError(t, store.db.QueryRow(`SELECT origin_device_id FROM sync_portable_identities`).Scan(&reopenedOrigin))
	testutil.Require(t, err == nil && string(firstJSON) == string(secondJSON) && inverseErr == nil && found && inverse == "session" && origin == deviceA && reopenedOrigin == deviceA && local.Session.ID == "session", "first=%s second=%s inverse=%q/%t origins=%q/%q err=%v", firstJSON, secondJSON, inverse, found, origin, reopenedOrigin, err)
}

func TestTranslateSyncObservationSetPreservesOutbox(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	project := "550e8400-e29b-41d4-a716-446655440001"
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local',0); INSERT INTO sessions(id,project_id,sync_version) VALUES('session','local',0); INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('obs','local','project','learning','x','test','active',1,1,0),('ref-a','local','project','learning','x','test','active',1,1,0),('ref-b','local','project','learning','x','test','active',1,1,0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES(?,?,?,?,?); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`, project, "local", "hash", "test", "now")
	testutil.NoError(t, err)
	obs := func(l syncservice.Lifecycle) *syncservice.Observation {
		return &syncservice.Observation{ID: "obs", ProjectID: "local", SessionID: "session", Scope: "project", Type: "learning", Content: "x", References: []string{"ref-a", "ref-b"}, Provenance: syncservice.Provenance{Producer: "test"}, Lifecycle: l, Review: syncservice.ReviewClear, CreatedAt: fixedTime, UpdatedAt: fixedTime}
	}
	mutations := []syncservice.Mutation{{MutationID: "550e8400-e29b-41d4-a716-446655440308", RecordID: "obs", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationCreate, Observation: obs(syncservice.LifecycleActive)}, {MutationID: "550e8400-e29b-41d4-a716-446655440309", RecordID: "obs", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationUpdate, BaseVersion: 1, Observation: obs(syncservice.LifecycleActive)}, {MutationID: "550e8400-e29b-41d4-a716-446655440310", RecordID: "obs", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationArchive, BaseVersion: 2, Observation: obs(syncservice.LifecycleArchived)}}
	enqueueMutation(t, store, mutations[0])
	var beforeID, beforeRecord string
	var beforeBase int64
	var beforePayload []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT mutation_id,record_id,base_version,payload FROM sync_outbox`).Scan(&beforeID, &beforeRecord, &beforeBase, &beforePayload))
	wire, err := store.TranslateSyncMutations(ctx, project, "local", mutations)
	tomb := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440311", RecordID: "obs", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 3, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}}
	tombWire, tombErr := store.TranslateSyncMutations(ctx, project, "local", []syncservice.Mutation{tomb})
	var afterID, afterRecord string
	var afterBase int64
	var afterPayload []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT mutation_id,record_id,base_version,payload FROM sync_outbox`).Scan(&afterID, &afterRecord, &afterBase, &afterPayload))
	ok := err == nil && tombErr == nil && len(wire) == 3 && tombWire[0].RecordID == wire[0].RecordID && beforeID == afterID && beforeRecord == afterRecord && beforeBase == afterBase && bytes.Equal(beforePayload, afterPayload)
	for _, m := range append(wire, tombWire...) {
		ok = ok && syncservice.ValidateMutation(m) == nil
	}
	session, sessionFound, sessionErr := store.LocalSyncPortableIdentity(ctx, project, syncservice.RecordKindSession, wire[0].Observation.SessionID)
	refA, refAFound, refAErr := store.LocalSyncPortableIdentity(ctx, project, syncservice.RecordKindObservation, wire[0].Observation.References[0])
	refB, refBFound, refBErr := store.LocalSyncPortableIdentity(ctx, project, syncservice.RecordKindObservation, wire[0].Observation.References[1])
	testutil.Require(t, ok && wire[0].RecordID == wire[1].RecordID && wire[1].RecordID == wire[2].RecordID && wire[0].Observation.SessionID != "session" && wire[0].Observation.References[0] != wire[0].Observation.References[1] && sessionErr == nil && sessionFound && session == "session" && refAErr == nil && refAFound && refA == "ref-a" && refBErr == nil && refBFound && refB == "ref-b", "wire=%+v tomb=%+v inverse=%q/%q/%q errors=%v/%v", wire, tombWire, session, refA, refB, err, tombErr)
}

func TestTranslateSyncMutationsRejectsBindingMismatch(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('a',0),('b',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','a','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`)
	testutil.NoError(t, err)
	_, err = store.TranslateSyncMutations(context.Background(), "550e8400-e29b-41d4-a716-446655440001", "b", nil)
	testutil.Require(t, errors.Is(err, ErrConflict), "err=%v", err)
}

func TestTranslateSyncMutationsTwoHandleRaceIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first := openPath(t, path)
	project := "550e8400-e29b-41d4-a716-446655440001"
	_, err := first.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local',0); INSERT INTO sessions(id,project_id,sync_version) VALUES('session','local',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','local','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`)
	testutil.NoError(t, err)
	second := openPath(t, path)
	defer first.Close()
	defer second.Close()
	mutation := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440312", RecordID: "session", RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: "session", ProjectID: "local"}}
	start := make(chan struct{})
	results := make(chan struct {
		wire string
		err  error
	}, 2)
	for _, store := range []*Store{first, second} {
		go func(s *Store) {
			<-start
			got, callErr := s.TranslateSyncMutations(context.Background(), project, "local", []syncservice.Mutation{mutation})
			wire := ""
			if len(got) == 1 {
				wire = got[0].RecordID
			}
			results <- struct {
				wire string
				err  error
			}{wire, callErr}
		}(store)
	}
	close(start)
	left, right := <-results, <-results
	var count int
	var origin string
	testutil.NoError(t, first.db.QueryRow(`SELECT count(*),max(origin_device_id) FROM sync_portable_identities`).Scan(&count, &origin))
	local, found, inverseErr := first.LocalSyncPortableIdentity(context.Background(), project, syncservice.RecordKindSession, left.wire)
	testutil.Require(t, left.err == nil && right.err == nil && left.wire != "" && left.wire == right.wire && count == 1 && origin == "550e8400-e29b-41d4-a716-446655440099" && inverseErr == nil && found && local == "session", "left=%+v right=%+v count=%d origin=%q inverse=%q/%t/%v", left, right, count, origin, local, found, inverseErr)
}

func TestClaimDueSyncOutboxCannotBePreemptedByCallerClock(t *testing.T) {
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440025", "project")
	enqueueMutation(t, store, mutation)
	claimed, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(claimed) == 1, "claimed=%+v err=%v", claimed, err)
	preempted, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(preempted) == 0, "caller advanced lease=%+v err=%v", preempted, err)
}

func TestSyncOutboxClaimRenewRetryAndBoundsFailClosed(t *testing.T) {
	store := openTestStore(t)
	now := fixedTime
	store.now = func() time.Time { return now }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440028", "project")
	enqueueMutation(t, store, mutation)
	for _, request := range []struct {
		lease time.Duration
		limit int
	}{{0, 1}, {25 * time.Hour, 1}, {time.Minute, 0}, {time.Minute, 17}} {
		_, err := store.ClaimDueSyncOutbox(context.Background(), request.lease, request.limit)
		testutil.Require(t, errors.Is(err, ErrInvalid), "request=%+v err=%v", request, err)
	}
	claims, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 1, "claims=%+v err=%v", claims, err)
	claim := claims[0]
	now = fixedTime.Add(30 * time.Second)
	testutil.NoError(t, store.RenewSyncOutboxClaim(context.Background(), mutation.MutationID, claim.ClaimToken, 2*time.Minute))
	var firstToken, token string
	var firstAt, claimedAt, leaseUntil int64
	testutil.NoError(t, store.db.QueryRow(`SELECT first_claim_token,claim_token,first_claimed_at,claimed_at,lease_until FROM sync_outbox_claims WHERE mutation_id=?`, mutation.MutationID).Scan(&firstToken, &token, &firstAt, &claimedAt, &leaseUntil))
	testutil.Require(t, firstToken == claim.FirstClaimToken && token == claim.ClaimToken && firstAt == claim.FirstClaimedAt.UnixNano() && claimedAt == claim.ClaimedAt.UnixNano() && leaseUntil == fixedTime.Add(2*time.Minute+30*time.Second).UnixNano(), "claim proof=%q/%q %d/%d/%d", firstToken, token, firstAt, claimedAt, leaseUntil)
	now = fixedTime.Add(time.Minute)
	testutil.Require(t, errors.Is(store.RenewSyncOutboxClaim(context.Background(), mutation.MutationID, "550e8400-e29b-41d4-a716-446655440029", time.Minute), ErrNotFound), "stale renew accepted")
	testutil.Require(t, errors.Is(store.MarkSyncOutboxRetry(context.Background(), mutation.MutationID, "550e8400-e29b-41d4-a716-446655440029", fixedTime.Add(2*time.Minute), "temporary"), ErrNotFound), "stale retry accepted")
	testutil.NoError(t, store.MarkSyncOutboxRetry(context.Background(), mutation.MutationID, claim.ClaimToken, fixedTime.Add(2*time.Minute), "temporary"))
	now = fixedTime.Add(2 * time.Minute)
	testutil.Require(t, errors.Is(store.RenewSyncOutboxClaim(context.Background(), mutation.MutationID, claim.ClaimToken, time.Minute), ErrNotFound), "expired claim renewed")
	testutil.Require(t, errors.Is(store.MarkSyncOutboxRetry(context.Background(), mutation.MutationID, claim.ClaimToken, fixedTime.Add(3*time.Minute), "temporary"), ErrNotFound), "expired claim retried")
	testutil.Require(t, errors.Is(store.MarkSyncOutboxRetry(context.Background(), mutation.MutationID, claim.ClaimToken, fixedTime.Add(time.Minute), "temporary"), ErrInvalid), "past retry accepted")
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime.Add(2*time.Minute))
	testutil.Require(t, err == nil && len(entries) == 1 && entries[0].State == SyncOutboxRetry && entries[0].Attempts == 1 && entries[0].LastErrorCode == "temporary", "entries=%+v err=%v", entries, err)
	var ended int64
	testutil.NoError(t, store.db.QueryRow(`SELECT lease_until FROM sync_outbox_claims WHERE mutation_id=?`, mutation.MutationID).Scan(&ended))
	testutil.Require(t, ended == fixedTime.Add(time.Minute).UnixNano(), "lease=%d", ended)
}

func TestClaimDueSyncOutboxFuturePredecessorAndConcurrentHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first, second := openPath(t, path), openPath(t, path)
	defer first.Close()
	defer second.Close()
	first.now = func() time.Time { return fixedTime }
	second.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := first.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	older := syncMutation("550e8400-e29b-41d4-a716-446655440036", "project")
	newer := syncMutation("550e8400-e29b-41d4-a716-446655440037", "project")
	enqueueMutation(t, first, older)
	enqueueMutation(t, first, newer)
	testutil.NoError(t, func() error {
		_, err := first.db.Exec(`UPDATE sync_outbox SET next_attempt_at=? WHERE mutation_id=?`, fixedTime.Add(time.Minute).UnixNano(), older.MutationID)
		return err
	}())
	claims, err := first.ClaimDueSyncOutbox(context.Background(), time.Minute, 16)
	testutil.Require(t, err == nil && len(claims) == 0, "future predecessor claims=%+v err=%v", claims, err)
	testutil.NoError(t, func() error {
		_, err := first.db.Exec(`UPDATE sync_outbox SET next_attempt_at=? WHERE mutation_id=?`, fixedTime.UnixNano(), older.MutationID)
		return err
	}())
	start := make(chan struct{})
	results := make(chan []SyncOutboxClaim, 2)
	errs := make(chan error, 2)
	for _, store := range []*Store{first, second} {
		go func(store *Store) {
			<-start
			got, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
			results <- got
			errs <- err
		}(store)
	}
	close(start)
	total := 0
	for range 2 {
		got, err := <-results, <-errs
		testutil.NoError(t, err)
		total += len(got)
	}
	testutil.Require(t, total == 1, "concurrent claims=%d", total)
}

func TestClaimDueSyncOutboxSkipsConflictAndInconsistentBaseVersion(t *testing.T) {
	store := openTestStore(t)
	store.now = func() time.Time { return fixedTime }
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('project',0)`)
		return err
	}())
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440038", "project")
	enqueueMutation(t, store, mutation)
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`UPDATE projects SET sync_version=1 WHERE id='project'`)
		return err
	}())
	claims, err := store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 0, "inconsistent base claims=%+v err=%v", claims, err)
	testutil.NoError(t, func() error {
		_, err := store.db.Exec(`UPDATE projects SET sync_version=0 WHERE id='project'; INSERT INTO sync_conflicts(conflict_id,history_id,created_seq,record_kind,record_id,canonical_version,competing_version_id,status,resolved_seq,payload_version,snapshot,created_at,updated_at) VALUES('550e8400-e29b-41d4-a716-446655440039','550e8400-e29b-41d4-a716-446655440040',1,'project','project',1,'550e8400-e29b-41d4-a716-446655440041','unresolved',NULL,1,X'7B7D',?,?)`, fixedTime.UnixNano(), fixedTime.UnixNano())
		return err
	}())
	claims, err = store.ClaimDueSyncOutbox(context.Background(), time.Minute, 1)
	testutil.Require(t, err == nil && len(claims) == 0, "conflicted claims=%+v err=%v", claims, err)
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

func TestDueSyncOutboxDiagnosticDoesNotReadPayload(t *testing.T) {
	for name, payload := range map[string]string{"empty": "X''", "oversized": "zeroblob(1048577)"} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			mutation := syncMutation("550e8400-e29b-41d4-a716-446655440008", "project")
			enqueueMutation(t, store, mutation)
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET payload=`+payload+` WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
			testutil.NoError(t, err)
			entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
			testutil.Require(t, err == nil && len(entries) == 1 && entries[0].MutationID == mutation.MutationID, "diagnostic entries=%+v err=%v", entries, err)
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

func TestDueSyncOutboxDiagnosticAcceptsOpaquePayload(t *testing.T) {
	store := openTestStore(t)
	mutation := syncMutation("550e8400-e29b-41d4-a716-446655440012", "project")
	enqueueMutation(t, store, mutation)
	_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_outbox SET payload=printf('%1048576s','') || char(0) || 'x' WHERE mutation_id=?; PRAGMA ignore_check_constraints=OFF`, mutation.MutationID)
	testutil.NoError(t, err)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 1 && entries[0].MutationID == mutation.MutationID, "entries=%+v err=%v", entries, err)
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
	item := mustSave(t, store, observation("local", "project", "local only"))
	var outbox int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.Require(t, outbox == 2, "outbox=%d", outbox)
	remote := pulledChange(t, 1, 1, pulledObservationMutation(item.ID, syncservice.MutationCreate, 0, syncservice.LifecycleActive, "remote", nil))
	err = store.ApplyPulledChange(context.Background(), "550e8400-e29b-41d4-a716-446655440167", remote)
	got, getErr := store.Get(context.Background(), item.ID, item.Project, item.Scope)
	var cursor int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
	testutil.Require(t, errors.Is(err, ErrConflict) && getErr == nil && got.Content == "local only" && cursor == 0, "error=%v item=%+v get=%v cursor=%d", err, got, getErr, cursor)
	before, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.NoError(t, err)
	profile.Enabled = true
	_, err = store.ConfigureSyncProfile(context.Background(), profile)
	after, afterErr := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && afterErr == nil && len(before) == len(after) && before[0].MutationID == after[0].MutationID && before[1].MutationID == after[1].MutationID, "before=%+v after=%+v errors=%v/%v", before, after, err, afterErr)
	t.Run("no profile", func(t *testing.T) {
		store := openTestStore(t)
		mustSave(t, store, observation("none", "project", "local only"))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
		testutil.Require(t, outbox == 0, "outbox=%d", outbox)
	})
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
			found, searchErr := store.Search(context.Background(), Search{Query: "original", Project: item.Project, Scope: ScopeProject})
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
	testutil.Require(t, entries[2].RecordKind == syncservice.RecordKindObservation && entries[2].RecordID == first.ID, "entry=%+v", entries[2])
}

func TestForgetSyncVersionZeroIsArchivedCreate(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	item := mustSave(t, store, observation("zero-archive", "project", "archive me"))
	_, err := store.Forget(context.Background(), item.ID, item.Project, item.Scope)
	testutil.NoError(t, err)
	entries, err := store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 3, "entries=%+v err=%v", entries, err)
	testutil.Require(t, entries[2].RecordKind == syncservice.RecordKindObservation && entries[2].RecordID == item.ID, "entry=%+v", entries[2])
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
		testutil.Require(t, entries[i].RecordKind == want, "entry[%d]=%+v", i, entries[i])
	}
	_, err = store.db.Exec(`UPDATE projects SET sync_version=4 WHERE id='project'; UPDATE sessions SET sync_version=5 WHERE id='session' AND project_id='project'; UPDATE observations SET sync_version=6 WHERE id='ordered'`)
	testutil.NoError(t, err)
	item.Content = "changed"
	updated, err := store.Update(context.Background(), item)
	testutil.NoError(t, err)
	item = updated
	entries, err = store.DueSyncOutbox(context.Background(), fixedTime)
	testutil.Require(t, err == nil && len(entries) == 4, "entries=%+v err=%v", entries, err)
	testutil.Require(t, entries[3].RecordKind == syncservice.RecordKindObservation && entries[3].RecordID == item.ID, "entry=%+v", entries[3])
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
	testutil.Require(t, entries[2].RecordKind == syncservice.RecordKindObservation && entries[2].RecordID == item.ID, "entry=%+v", entries[2])
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
	testutil.Require(t, version == 15 && observations == 4 && outbox == 5, "version=%d observations=%d outbox=%d", version, observations, outbox)
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

func TestApplyPulledChangeOrderedObservationAndReplay(t *testing.T) {
	store := openTestStore(t)
	testutil.Require(t, !store.syncInbox.known, "cache=%+v", store.syncInbox)
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440100"
	project := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440101", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}})
	session := pulledChange(t, 2, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440102", RecordID: "session", RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: "session", ProjectID: "project"}})
	observation := pulledChange(t, 3, 1, pulledObservationMutation("observation", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "first", nil))
	observation.Mutation.Observation.SessionID = "session"
	observation.ChangeHash, _ = syncservice.CanonicalChangeHash(observation)
	var epoch int64
	for i, change := range []syncservice.Change{project, session, observation} {
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, change))
		if i == 0 {
			epoch = store.syncInbox.dataVersion
		}
		testutil.Require(t, store.syncInbox.known && store.syncInbox.historyID == history && store.syncInbox.position == int64(i+1) && store.syncInbox.dataVersion == epoch, "cache=%+v", store.syncInbox)
	}
	got, err := store.Get(ctx, "observation", "project", ScopeProject)
	testutil.Require(t, err == nil && got.Content == "first" && got.Session == "session" && got.State == StateActive, "observation=%+v err=%v", got, err)
	var version, inbox, outbox, position int
	testutil.NoError(t, store.db.QueryRow(`SELECT sync_version FROM observations WHERE id='observation'`).Scan(&version))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor WHERE singleton=1`).Scan(&position))
	testutil.Require(t, version == 1 && inbox == 3 && outbox == 0 && position == 3, "version=%d inbox=%d outbox=%d position=%d", version, inbox, outbox, position)
	testutil.NoError(t, store.ApplyPulledChange(ctx, history, observation))
	t.Run("position zero cursor", func(t *testing.T) {
		store := openTestStore(t)
		history := "550e8400-e29b-41d4-a716-446655440103"
		_, err := store.db.Exec(`INSERT INTO sync_cursor(singleton,history_id,position,updated_at) VALUES(1,?,?,?)`, history, 0, fixedTime.UnixNano())
		testutil.NoError(t, err)
		change := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440104", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}})
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, change))
		var position int
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor WHERE singleton=1`).Scan(&position))
		testutil.Require(t, position == 1, "position=%d", position)
	})
}

func TestApplyPulledChangeRejectsInvalidOrderingAndLocalWork(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440110"
	project := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440111", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}})
	badHash := project
	badHash.ChangeHash = strings.Repeat("0", 64)
	resolveMutation := pulledObservationMutation("resolved", syncservice.MutationResolve, 1, syncservice.LifecycleActive, "resolved", nil)
	resolveObservation := resolveMutation.Observation
	resolveMutation.Observation = nil
	resolveMutation.Resolution = &syncservice.Resolution{ConflictIDs: []string{"550e8400-e29b-41d4-a716-446655440114"}, Observation: resolveObservation}
	specialVersion := 2
	tombstone := syncservice.Change{Sequence: 1, CanonicalVersion: 1, HashVersion: &specialVersion, ChangeDisposition: syncservice.ChangeDispositionAccepted, Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440112", RecordID: "x", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}}}
	tombstone.ChangeHash, _ = syncservice.CanonicalChangeHash(tombstone)
	resolve := syncservice.Change{Sequence: 1, CanonicalVersion: 1, HashVersion: &specialVersion, ChangeDisposition: syncservice.ChangeDispositionAccepted, Mutation: resolveMutation}
	resolve.ChangeHash, _ = syncservice.CanonicalChangeHash(resolve)
	for name, change := range map[string]syncservice.Change{"hash": badHash, "gap": pulledChange(t, 2, 1, project.Mutation), "tombstone": tombstone, "resolve": resolve} {
		t.Run(name, func(t *testing.T) {
			err := store.ApplyPulledChange(ctx, history, change)
			if name == "gap" || name == "tombstone" || name == "resolve" {
				testutil.Require(t, errors.Is(err, ErrConflict), "error=%v", err)
			} else {
				testutil.Require(t, errors.Is(err, ErrInvalid), "error=%v", err)
			}
		})
	}
	enqueueMutation(t, store, syncMutation("550e8400-e29b-41d4-a716-446655440113", "project"))
	err := store.ApplyPulledChange(ctx, history, project)
	testutil.Require(t, errors.Is(err, ErrConflict), "pending outbox error=%v", err)
	_, err = store.db.Exec(`UPDATE sync_outbox SET state='retry',attempts=1,last_error_code='temporary' WHERE mutation_id=?`, "550e8400-e29b-41d4-a716-446655440113")
	testutil.NoError(t, err)
	err = store.ApplyPulledChange(ctx, history, project)
	testutil.Require(t, errors.Is(err, ErrConflict), "retry outbox error=%v", err)
	var cursor, records int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects WHERE id='project'`).Scan(&records))
	testutil.Require(t, cursor == 0 && records == 0, "cursor=%d records=%d", cursor, records)
}

func TestApplyPulledChangeUpdateArchiveAndReferenceValidation(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440120"
	project := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440121", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}})
	first := pulledChange(t, 2, 1, pulledObservationMutation("first", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "first token", nil))
	second := pulledChange(t, 3, 1, pulledObservationMutation("second", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "second token", []string{"first"}))
	for _, change := range []syncservice.Change{project, first, second} {
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, change))
	}
	update := pulledChange(t, 4, 2, pulledObservationMutation("second", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "updated token", []string{"first"}))
	archive := pulledChange(t, 5, 3, pulledObservationMutation("second", syncservice.MutationArchive, 2, syncservice.LifecycleArchived, "updated token", []string{"first"}))
	testutil.NoError(t, store.ApplyPulledChange(ctx, history, update))
	testutil.NoError(t, store.ApplyPulledChange(ctx, history, archive))
	got, err := store.Get(ctx, "second", "project", ScopeProject)
	testutil.Require(t, err == nil && got.State == StateArchived && got.Content == "updated token" && len(got.References) == 1, "observation=%+v err=%v", got, err)
	found, searchErr := store.Search(ctx, Search{Query: "updated", Project: "project", Scope: ScopeProject})
	testutil.Require(t, searchErr == nil && len(found) == 0, "fts=%+v err=%v", found, searchErr)
	forward := pulledChange(t, 6, 5, pulledObservationMutation("second", syncservice.MutationUpdate, 4, syncservice.LifecycleActive, "no", nil))
	err = store.ApplyPulledChange(ctx, history, forward)
	testutil.Require(t, errors.Is(err, ErrConflict), "forward error=%v", err)
}

func TestApplyPulledChangeFailsClosedOnReplayCursorAndWriteFailures(t *testing.T) {
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440130"
	project := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440131", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}})
	t.Run("replay mismatch, missing inbox, and history", func(t *testing.T) {
		store := openTestStore(t)
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, project))
		mismatch := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440133", RecordID: "other", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "other"}})
		testutil.Require(t, errors.Is(store.ApplyPulledChange(ctx, history, mismatch), ErrCorrupt), "replay mismatch accepted")
		testutil.Require(t, errors.Is(store.ApplyPulledChange(ctx, "550e8400-e29b-41d4-a716-446655440132", project), ErrCorrupt), "history mismatch accepted")
		_, err := store.db.Exec(`DELETE FROM sync_inbox WHERE history_id=? AND seq=1`, history)
		testutil.NoError(t, err)
		err = store.ApplyPulledChange(ctx, history, project)
		testutil.Require(t, errors.Is(err, ErrCorrupt), "missing replay inbox error=%v", err)
		var cursor, records int
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor WHERE singleton=1`).Scan(&cursor))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects WHERE id='project'`).Scan(&records))
		testutil.Require(t, cursor == 1 && records == 1, "cursor=%d records=%d", cursor, records)
	})
	t.Run("final inbox and cursor writes roll back", func(t *testing.T) {
		for name, setup := range map[string]func(*Store){
			"inbox insert": func(store *Store) {
				_, err := store.db.Exec(`CREATE TRIGGER fail_inbox BEFORE INSERT ON sync_inbox BEGIN SELECT RAISE(ABORT, 'test'); END`)
				testutil.NoError(t, err)
			},
			"cursor update": func(store *Store) {
				testutil.NoError(t, store.ApplyPulledChange(ctx, history, project))
				_, err := store.db.Exec(`CREATE TRIGGER fail_cursor BEFORE UPDATE ON sync_cursor BEGIN SELECT RAISE(ABORT, 'test'); END`)
				testutil.NoError(t, err)
			},
		} {
			t.Run(name, func(t *testing.T) {
				store := openTestStore(t)
				setup(store)
				change := project
				if name == "cursor update" {
					change = pulledChange(t, 2, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440134", RecordID: "other", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "other"}})
				}
				err := store.ApplyPulledChange(ctx, history, change)
				testutil.Require(t, errors.Is(err, ErrCorrupt), "apply error=%v", err)
				var records, inbox, cursor, position int
				testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&records))
				testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
				testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
				testutil.NoError(t, store.db.QueryRow(`SELECT COALESCE((SELECT position FROM sync_cursor WHERE singleton=1),0)`).Scan(&position))
				if name == "inbox insert" {
					testutil.Require(t, records == 0 && inbox == 0 && cursor == 0 && position == 0, "records=%d inbox=%d cursor=%d position=%d", records, inbox, cursor, position)
				} else {
					testutil.Require(t, records == 1 && inbox == 1 && cursor == 1 && position == 1, "records=%d inbox=%d cursor=%d position=%d", records, inbox, cursor, position)
				}
			})
		}
	})
	t.Run("reference insertion rolls back observation", func(t *testing.T) {
		store := openTestStore(t)
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, project))
		target := pulledChange(t, 2, 1, pulledObservationMutation("target", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "target", nil))
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, target))
		_, err := store.db.Exec(`CREATE TRIGGER fail_reference BEFORE INSERT ON observation_refs BEGIN SELECT RAISE(ABORT, 'test'); END`)
		testutil.NoError(t, err)
		change := pulledChange(t, 3, 1, pulledObservationMutation("next", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "content", []string{"target"}))
		err = store.ApplyPulledChange(ctx, history, change)
		testutil.Require(t, errors.Is(err, ErrCorrupt), "apply error=%v", err)
		var observations, fts, refs, inbox, cursor int
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations WHERE id='next'`).Scan(&observations))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations_fts WHERE id='next'`).Scan(&fts))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observation_refs WHERE observation_id='next'`).Scan(&refs))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor WHERE singleton=1`).Scan(&cursor))
		testutil.Require(t, observations == 0 && fts == 0 && refs == 0 && inbox == 2 && cursor == 2, "observations=%d fts=%d refs=%d inbox=%d cursor=%d", observations, fts, refs, inbox, cursor)
	})
	t.Run("fts insertion rolls back observation", func(t *testing.T) {
		store := openTestStore(t)
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, project))
		_, err := store.db.Exec(`DROP TABLE observations_fts`)
		testutil.NoError(t, err)
		change := pulledChange(t, 2, 1, pulledObservationMutation("observation", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "content", nil))
		err = store.ApplyPulledChange(ctx, history, change)
		testutil.Require(t, errors.Is(err, ErrCorrupt), "apply error=%v", err)
		var observations, inbox, cursor int
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations`).Scan(&observations))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor WHERE singleton=1`).Scan(&cursor))
		testutil.Require(t, observations == 0 && inbox == 1 && cursor == 1, "observations=%d inbox=%d cursor=%d", observations, inbox, cursor)
	})
}

func TestApplyPulledChangeRejectsInvalidReferencesAndTopicCollision(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440140"
	project := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440141", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}})
	first := pulledChange(t, 2, 1, pulledObservationMutation("first", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "first", nil))
	first.Mutation.Observation.TopicKey = "topic"
	first.ChangeHash, _ = syncservice.CanonicalChangeHash(first)
	for _, change := range []syncservice.Change{project, first} {
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, change))
	}
	badRef := pulledChange(t, 3, 1, pulledObservationMutation("bad", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "bad", []string{"missing"}))
	err := store.ApplyPulledChange(ctx, history, badRef)
	testutil.Require(t, errors.Is(err, ErrNotFound), "reference error=%v", err)
	collision := pulledChange(t, 3, 1, pulledObservationMutation("other", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "other", nil))
	collision.Mutation.Observation.TopicKey = "topic"
	collision.ChangeHash, _ = syncservice.CanonicalChangeHash(collision)
	testutil.Require(t, errors.Is(store.ApplyPulledChange(ctx, history, collision), ErrConflict), "topic collision accepted")
}

func TestApplyPulledChangeLookupFailureIsCorrupt(t *testing.T) {
	store := openTestStore(t)
	_, err := store.db.Exec(`DROP TABLE projects`)
	testutil.NoError(t, err)
	change := pulledChange(t, 1, 1, syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440142", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}})
	err = store.ApplyPulledChange(context.Background(), "550e8400-e29b-41d4-a716-446655440143", change)
	testutil.Require(t, errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrConflict), "lookup error=%v", err)
}

func TestApplyPulledChangeCacheRevalidation(t *testing.T) {
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440160"
	change := func(seq int64, id, mutation string) syncservice.Change {
		return pulledChange(t, seq, 1, syncMutation(mutation, id))
	}
	t.Run("failure invalidates and retry establishes", func(t *testing.T) {
		store := openTestStore(t)
		_, err := store.db.Exec(`CREATE TRIGGER fail_cache BEFORE INSERT ON sync_inbox BEGIN SELECT RAISE(ABORT, 'test'); END`)
		testutil.NoError(t, err)
		testutil.Require(t, errors.Is(store.ApplyPulledChange(ctx, history, change(1, "project", "550e8400-e29b-41d4-a716-446655440161")), ErrCorrupt) && !store.syncInbox.known, "cache=%+v", store.syncInbox)
		_, err = store.db.Exec(`DROP TRIGGER fail_cache`)
		testutil.NoError(t, err)
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, change(1, "project", "550e8400-e29b-41d4-a716-446655440161")))
		testutil.Require(t, store.syncInbox.known && store.syncInbox.position == 1, "cache=%+v", store.syncInbox)
		enqueueMutation(t, store, syncMutation("550e8400-e29b-41d4-a716-446655440165", "second"))
		testutil.Require(t, errors.Is(store.ApplyPulledChange(ctx, history, change(2, "second", "550e8400-e29b-41d4-a716-446655440166")), ErrConflict) && store.syncInbox.known && store.syncInbox.position == 1, "cache=%+v", store.syncInbox)
	})
	t.Run("concurrent duplicate and two handles", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		firstStore, secondStore := openPath(t, path), openPath(t, path)
		first, second, third := change(1, "project", "550e8400-e29b-41d4-a716-446655440162"), change(2, "second", "550e8400-e29b-41d4-a716-446655440163"), change(3, "third", "550e8400-e29b-41d4-a716-446655440164")
		defer firstStore.Close()
		defer secondStore.Close()
		var group sync.WaitGroup
		for range 2 {
			group.Add(1)
			go func() { defer group.Done(); testutil.NoError(t, firstStore.ApplyPulledChange(ctx, history, first)) }()
		}
		group.Wait()
		testutil.NoError(t, secondStore.ApplyPulledChange(ctx, history, second))
		testutil.NoError(t, firstStore.ApplyPulledChange(ctx, history, third))
		testutil.Require(t, firstStore.syncInbox.known && firstStore.syncInbox.position == 3, "cache=%+v", firstStore.syncInbox)
	})
}

func TestApplyPulledChangeArchivedAndCorruptInboxRegressions(t *testing.T) {
	ctx := context.Background()
	t.Run("archived create is not searchable", func(t *testing.T) {
		store := openTestStore(t)
		history := "550e8400-e29b-41d4-a716-446655440150"
		project := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440151", "project"))
		archive := pulledChange(t, 2, 1, pulledObservationMutation("archived", syncservice.MutationCreate, 0, syncservice.LifecycleArchived, "archived token", nil))
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, project))
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, archive))
		found, err := store.Search(ctx, Search{Query: "archived", Project: "project", Scope: ScopeProject})
		testutil.Require(t, err == nil && len(found) == 0, "search=%+v err=%v", found, err)
	})
	t.Run("invalid review after has no effects", func(t *testing.T) {
		store := openTestStore(t)
		history := "550e8400-e29b-41d4-a716-446655440152"
		project := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440156", "project"))
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, project))
		change := pulledChange(t, 2, 1, pulledObservationMutation("review", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "token", nil))
		change.Mutation.Observation.ReviewAfter = timePtr(time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC))
		change.ChangeHash, _ = syncservice.CanonicalChangeHash(change)
		err := store.ApplyPulledChange(ctx, history, change)
		var observations, inbox, cursor int
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations`).Scan(&observations))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
		testutil.Require(t, errors.Is(err, ErrInvalid) && observations == 0 && inbox == 1 && cursor == 1, "error=%v observations=%d inbox=%d cursor=%d", err, observations, inbox, cursor)
	})
	for name, corrupt := range map[string]string{"seq": `seq=2`, "hash": `change_hash=zeroblob(31)`, "timestamp": `applied_at=0`} {
		t.Run("corrupt "+name, func(t *testing.T) {
			store := openTestStore(t)
			history := "550e8400-e29b-41d4-a716-446655440153"
			first := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440154", "project"))
			next := pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-446655440155", "next"))
			testutil.NoError(t, store.ApplyPulledChange(ctx, history, first))
			_, err := store.db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE sync_inbox SET ` + corrupt + `; PRAGMA ignore_check_constraints=OFF`)
			testutil.NoError(t, err)
			store.syncInbox = syncInboxCache{}
			err = store.ApplyPulledChange(ctx, history, next)
			var records, cursor int
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&records))
			testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor WHERE singleton=1`).Scan(&cursor))
			testutil.Require(t, errors.Is(err, ErrCorrupt) && records == 1 && cursor == 1, "error=%v records=%d cursor=%d", err, records, cursor)
		})
	}
}

func timePtr(value time.Time) *time.Time { return &value }

func pulledObservationMutation(id string, kind syncservice.MutationKind, base int64, lifecycle syncservice.Lifecycle, content string, refs []string) syncservice.Mutation {
	return syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440199", RecordID: id, RecordKind: syncservice.RecordKindObservation, Kind: kind, BaseVersion: base, Observation: &syncservice.Observation{ID: id, ProjectID: "project", SessionID: "", Scope: "project", Type: "learning", Content: content, References: refs, Provenance: syncservice.Provenance{Producer: "test"}, Lifecycle: lifecycle, Review: syncservice.ReviewClear, CreatedAt: fixedTime, UpdatedAt: fixedTime}}
}

func pulledChange(t *testing.T, sequence, version int64, mutation syncservice.Mutation) syncservice.Change {
	t.Helper()
	change := syncservice.Change{Sequence: sequence, CanonicalVersion: version, Mutation: mutation}
	var err error
	change.ChangeHash, err = syncservice.CanonicalChangeHash(change)
	testutil.NoError(t, err)
	return change
}

func TestApplyPulledPageRejectsInvalidPageWithoutWrites(t *testing.T) {
	store := openTestStore(t)
	page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: "550e8400-e29b-41d4-a716-446655440201", Position: 2, Watermark: 2}, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440202", "project"))}}
	err := store.ApplyPulledPage(context.Background(), page, nil)
	var projects, inbox int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.Require(t, errors.Is(err, ErrInvalid) && projects == 0 && inbox == 0, "err=%v projects=%d inbox=%d", err, projects, inbox)
}

func TestApplyPulledPageRollsBackWholePage(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440203"
	page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{
		pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440204", "project")),
		pulledChange(t, 2, 1, pulledObservationMutation("bad", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "bad", []string{"missing"})),
	}}
	err := store.ApplyPulledPage(context.Background(), page, nil)
	var projects, inbox int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.Require(t, errors.Is(err, ErrNotFound) && projects == 0 && inbox == 0, "err=%v projects=%d inbox=%d", err, projects, inbox)
}

func TestApplyPulledChangeUsesPagePrimitive(t *testing.T) {
	store := openTestStore(t)
	change := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440205", "project"))
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), "550e8400-e29b-41d4-a716-446655440206", change))
}

func TestPulledConflictStoresLosslessSnapshot(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440207"
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440208", "project"))))
	mutation := pulledObservationMutation("conflict", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "competitor", nil)
	change := specialChange(t, 2, 1, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440209", mutation)
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, change))
	var snapshot []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT snapshot FROM sync_conflicts WHERE conflict_id=?`, change.ConflictID).Scan(&snapshot))
	var projects, observations int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations`).Scan(&observations))
	testutil.Require(t, len(snapshot) > 0 && projects == 1 && observations == 0, "snapshot=%q projects=%d observations=%d", snapshot, projects, observations)
}

func TestPulledTombstoneHidesButRetainsInboundReferences(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440210"
	project := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440211", "project"))
	target := pulledChange(t, 2, 1, pulledObservationMutation("target", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "target", nil))
	referrer := pulledChange(t, 3, 1, pulledObservationMutation("referrer", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "referrer", []string{"target"}))
	for _, change := range []syncservice.Change{project, target, referrer} {
		testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, change))
	}
	tombstone := specialChange(t, 4, 2, syncservice.ChangeDispositionAccepted, "", syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440212", RecordID: "target", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}})
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, tombstone))
	_, err := store.Get(context.Background(), "target", "project", ScopeProject)
	var inbound, outgoing int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observation_refs WHERE target_id='target'`).Scan(&inbound))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observation_refs WHERE observation_id='target'`).Scan(&outgoing))
	testutil.Require(t, errors.Is(err, ErrNotFound) && inbound == 1 && outgoing == 0, "err=%v refs=%d/%d", err, inbound, outgoing)
}

func TestPulledResolveRequiresExactUnresolvedIDsAndRollsBack(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440213"
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440214", "project"))))
	local := pulledChange(t, 2, 1, pulledObservationMutation("record", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "local", nil))
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, local))
	conflict := specialChange(t, 3, 1, syncservice.ChangeDispositionConflict, "550e8400-e29b-41d4-a716-446655440215", pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "other", nil))
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, conflict))
	winner := pulledObservationMutation("record", syncservice.MutationResolve, 1, syncservice.LifecycleActive, "winner", nil)
	winner.Observation, winner.Resolution = nil, &syncservice.Resolution{ConflictIDs: []string{"550e8400-e29b-41d4-a716-446655440216"}, Observation: pulledObservationMutation("record", syncservice.MutationUpdate, 1, syncservice.LifecycleActive, "winner", nil).Observation}
	err := store.ApplyPulledChange(context.Background(), history, specialChange(t, 4, 2, syncservice.ChangeDispositionAccepted, "", winner))
	got, getErr := store.Get(context.Background(), "record", "project", ScopeProject)
	testutil.Require(t, errors.Is(err, ErrConflict) && getErr == nil && got.Content == "local", "err=%v got=%+v get=%v", err, got, getErr)
}

func TestLocalAndOrdinaryPulledWritesRejectTombstonedID(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440217"
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440218", "project"))))
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, pulledChange(t, 2, 1, pulledObservationMutation("target", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "target", nil))))
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, specialChange(t, 3, 2, syncservice.ChangeDispositionAccepted, "", syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440219", RecordID: "target", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}})))
	ordinary := pulledChange(t, 4, 3, pulledObservationMutation("target", syncservice.MutationUpdate, 2, syncservice.LifecycleActive, "revive", nil))
	reference := observation("reference", "project", "reference")
	reference.References = []string{"target"}
	_, localErr := store.Save(context.Background(), reference)
	testutil.Require(t, errors.Is(store.ApplyPulledChange(context.Background(), history, ordinary), ErrConflict) && errors.Is(localErr, ErrConflict), "ordinary=%v local=%v", store.ApplyPulledChange(context.Background(), history, ordinary), localErr)
}

func TestForgetRejectsTombstonedObservationTransactionally(t *testing.T) {
	for _, profile := range []bool{false, true} {
		t.Run(fmt.Sprintf("sync profile=%t", profile), func(t *testing.T) {
			store := openTestStore(t)
			if profile {
				enableSync(t, store)
			}
			history := "550e8400-e29b-41d4-a716-446655440236"
			testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440237", "project"))))
			testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, pulledChange(t, 2, 1, pulledObservationMutation("target", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "retained content", nil))))
			testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, specialChange(t, 3, 2, syncservice.ChangeDispositionAccepted, "", syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440238", RecordID: "target", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationTombstone, BaseVersion: 1, Tombstone: &syncservice.Tombstone{DeletedAt: fixedTime}})))
			var beforeState, beforeContent, beforeTombstone string
			var beforeVersion, beforeOutbox, beforeInbox, beforeCursor int
			testutil.NoError(t, store.db.QueryRow(`SELECT state,content,sync_version FROM observations WHERE id='target'`).Scan(&beforeState, &beforeContent, &beforeVersion))
			testutil.NoError(t, store.db.QueryRow(`SELECT quote(provenance) FROM sync_tombstones WHERE record_id='target'`).Scan(&beforeTombstone))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&beforeOutbox))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&beforeInbox))
			testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&beforeCursor))

			err := func() error {
				_, err := store.Forget(context.Background(), "target", "project", ScopeProject)
				return err
			}()
			var state, content, tombstone string
			var version, outbox, inbox, cursor int
			testutil.NoError(t, store.db.QueryRow(`SELECT state,content,sync_version FROM observations WHERE id='target'`).Scan(&state, &content, &version))
			testutil.NoError(t, store.db.QueryRow(`SELECT quote(provenance) FROM sync_tombstones WHERE record_id='target'`).Scan(&tombstone))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_outbox`).Scan(&outbox))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
			testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
			_, getErr := store.Get(context.Background(), "target", "project", ScopeProject)
			testutil.Require(t, errors.Is(err, ErrConflict) && errors.Is(getErr, ErrNotFound) && state == beforeState && content == beforeContent && version == beforeVersion && tombstone == beforeTombstone && outbox == beforeOutbox && inbox == beforeInbox && cursor == beforeCursor, "forget=%v get=%v observation=%q/%q/%d tombstone=%q outbox=%d/%d inbox=%d/%d cursor=%d/%d", err, getErr, state, content, version, tombstone, outbox, beforeOutbox, inbox, beforeInbox, cursor, beforeCursor)
		})
	}
}

func TestApplyPulledPageTwoHandlesRereadDurableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first, second := openPath(t, path), openPath(t, path)
	defer first.Close()
	defer second.Close()
	history := "550e8400-e29b-41d4-a716-446655440220"
	page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 3}, HasMore: true, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440221", "project"))}}
	testutil.NoError(t, first.ApplyPulledPage(context.Background(), page, nil))
	secondPage := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 3, Watermark: 3}, Changes: []syncservice.Change{pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-446655440229", "second")), pulledChange(t, 3, 1, syncMutation("550e8400-e29b-41d4-a716-446655440230", "third"))}}
	testutil.NoError(t, second.ApplyPulledPage(context.Background(), secondPage, nil))
	testutil.NoError(t, first.ApplyPulledPage(context.Background(), page, nil))
	overlap := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 4, Watermark: 4}, Changes: []syncservice.Change{pulledChange(t, 3, 1, syncMutation("550e8400-e29b-41d4-a716-446655440230", "third")), pulledChange(t, 4, 1, syncMutation("550e8400-e29b-41d4-a716-446655440231", "fourth"))}}
	testutil.NoError(t, second.ApplyPulledPage(context.Background(), overlap, nil))
	var inbox, position int
	testutil.NoError(t, second.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, second.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&position))
	testutil.Require(t, inbox == 4 && position == 4, "inbox=%d position=%d", inbox, position)
}

func TestApplyPulledPageCursorFailureRollsBackAndPreservesCache(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440222"
	first := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}, HasMore: true, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440223", "project"))}}
	testutil.NoError(t, store.ApplyPulledPage(ctx, first, &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 2, Phase: "projects"}))
	_, err := store.db.Exec(`CREATE TRIGGER fail_cursor BEFORE UPDATE ON sync_cursor BEGIN SELECT RAISE(ABORT, 'test'); END`)
	testutil.NoError(t, err)
	next := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{pulledChange(t, 2, 1, pulledObservationMutation("rolled-back", syncservice.MutationCreate, 0, syncservice.LifecycleActive, "content", nil))}}
	checkpoint := &BootstrapCheckpoint{HistoryID: history, Position: 2, Watermark: 2, Phase: "observations"}
	err = store.ApplyPulledPage(ctx, next, checkpoint)
	var observations, position int
	var phase string
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations WHERE id='rolled-back'`).Scan(&observations))
	testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&position))
	testutil.NoError(t, store.db.QueryRow(`SELECT phase FROM sync_bootstrap`).Scan(&phase))
	testutil.Require(t, errors.Is(err, ErrCorrupt) && observations == 0 && position == 1 && phase == "projects" && store.syncInbox.known && store.syncInbox.position == 1, "err=%v observations=%d position=%d phase=%q cache=%+v", err, observations, position, phase, store.syncInbox)
	_, err = store.db.Exec(`DROP TRIGGER fail_cursor`)
	testutil.NoError(t, err)
	testutil.NoError(t, store.ApplyPulledPage(ctx, next, checkpoint))
	testutil.Require(t, store.syncInbox.known && store.syncInbox.position == 2, "cache=%+v", store.syncInbox)
}

func TestPulledPageCheckpointRereadsPriorDurableState(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440226"
	first := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}, HasMore: true, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440227", "project"))}}
	checkpoint := &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 2, Phase: "projects"}
	testutil.NoError(t, store.ApplyPulledPage(context.Background(), first, checkpoint))
	second := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-446655440228", "next"))}}
	checkpoint.Position, checkpoint.Phase = 2, "sessions"
	testutil.NoError(t, store.ApplyPulledPage(context.Background(), second, checkpoint))
	var payload string
	testutil.NoError(t, store.db.QueryRow(`SELECT checkpoint FROM sync_bootstrap`).Scan(&payload))
	testutil.Require(t, !strings.Contains(payload, "phase"), "checkpoint=%s", payload)
}

func TestApplyPulledPageAcceptsEmptyEOFAndPreservesCursor(t *testing.T) {
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440224"
	t.Run("initial checkpoint", func(t *testing.T) {
		store := openTestStore(t)
		checkpoint := &BootstrapCheckpoint{HistoryID: history, Position: 0, Watermark: 0, Phase: "complete"}
		testutil.NoError(t, store.ApplyPulledPage(ctx, syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history}}, checkpoint))
		var cursors int
		var phase string
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursors))
		testutil.NoError(t, store.db.QueryRow(`SELECT phase FROM sync_bootstrap`).Scan(&phase))
		testutil.Require(t, cursors == 0 && phase == "complete" && store.syncInbox.known && store.syncInbox.position == 0, "cursors=%d phase=%q cache=%+v", cursors, phase, store.syncInbox)
	})
	t.Run("resumed watermark", func(t *testing.T) {
		store := openTestStore(t)
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440225", "project"))))
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-446655440226", "next"))))
		testutil.NoError(t, store.ApplyPulledPage(ctx, syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}}, nil))
		var position int
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&position))
		testutil.Require(t, position == 2 && store.syncInbox.known && store.syncInbox.position == 2, "position=%d cache=%+v", position, store.syncInbox)
	})
}

func TestApplyPulledPageEmptyEOFFailsClosedOnCursorTypeCorruption(t *testing.T) {
	ctx := context.Background()
	for name, mutation := range map[string]string{
		"history":  `history_id=CAST(history_id AS BLOB)`,
		"position": `position=CAST(position AS BLOB)`,
		"updated":  `updated_at=CAST(updated_at AS BLOB)`,
	} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			store.db.SetMaxOpenConns(1)
			store.db.SetMaxIdleConns(1)
			history := "550e8400-e29b-41d4-a716-446655440234"
			page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 1}, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440235", "project"))}}
			testutil.NoError(t, store.ApplyPulledPage(ctx, page, nil))

			conn, err := store.db.Conn(ctx)
			testutil.NoError(t, err)
			testutil.NoError(t, func() error {
				defer conn.Close()
				if _, err := conn.ExecContext(ctx, `PRAGMA ignore_check_constraints=ON`); err != nil {
					return err
				}
				defer conn.ExecContext(ctx, `PRAGMA ignore_check_constraints=OFF`)
				_, err := conn.ExecContext(ctx, `UPDATE sync_cursor SET `+mutation+` WHERE singleton=1`)
				return err
			}())

			var before string
			testutil.NoError(t, store.db.QueryRow(`SELECT quote(history_id)||'/'||quote(position)||'/'||quote(updated_at) FROM sync_cursor WHERE singleton=1`).Scan(&before))
			err = store.ApplyPulledPage(ctx, syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 1}}, nil)
			var after string
			var projects, inbox int
			testutil.NoError(t, store.db.QueryRow(`SELECT quote(history_id)||'/'||quote(position)||'/'||quote(updated_at) FROM sync_cursor WHERE singleton=1`).Scan(&after))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
			testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
			testutil.Require(t, errors.Is(err, ErrCorrupt) && !store.syncInbox.known && before == after && projects == 1 && inbox == 1, "err=%v cache=%+v cursor=%q/%q projects=%d inbox=%d", err, store.syncInbox, before, after, projects, inbox)
		})
	}
}

func TestApplyPulledPageRejectsHasMoreMismatchBeforeTransaction(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440227"
	page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440228", "project"))}}
	err := store.ApplyPulledPage(context.Background(), page, nil)
	var projects, cursors int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursors))
	testutil.Require(t, errors.Is(err, ErrInvalid) && projects == 0 && cursors == 0, "err=%v projects=%d cursors=%d", err, projects, cursors)
	testutil.Require(t, errors.Is(store.ApplyPulledPage(context.Background(), syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history}, HasMore: true}, nil), ErrInvalid), "empty HasMore mismatch accepted")
}

func TestPulledPageCheckpointPhaseOrderAndCompleteTerminality(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440229"
	page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}, HasMore: true, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440230", "project"))}}
	checkpoint := func(phase string) *BootstrapCheckpoint {
		return &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 2, Phase: phase}
	}
	testutil.NoError(t, store.ApplyPulledPage(ctx, page, checkpoint("projects")))
	testutil.NoError(t, store.ApplyPulledPage(ctx, page, checkpoint("sessions")))
	testutil.Require(t, errors.Is(store.ApplyPulledPage(ctx, page, checkpoint("projects")), ErrConflict), "checkpoint phase regressed")
	testutil.NoError(t, store.ApplyPulledPage(ctx, page, checkpoint("observations")))
	eof := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-446655440233", "next"))}}
	complete := &BootstrapCheckpoint{HistoryID: history, Position: 2, Watermark: 2, Phase: "complete"}
	testutil.NoError(t, store.ApplyPulledPage(ctx, eof, complete))
	testutil.NoError(t, store.ApplyPulledPage(ctx, eof, complete))
	testutil.Require(t, errors.Is(store.ApplyPulledPage(ctx, eof, &BootstrapCheckpoint{HistoryID: history, Position: 2, Watermark: 2, Phase: "observations"}), ErrConflict), "complete downgrade accepted")
	advance := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 3, Watermark: 3}, Changes: []syncservice.Change{pulledChange(t, 3, 1, syncMutation("550e8400-e29b-41d4-a716-446655440234", "third"))}}
	testutil.Require(t, errors.Is(store.ApplyPulledPage(ctx, advance, &BootstrapCheckpoint{HistoryID: history, Position: 3, Watermark: 3, Phase: "complete"}), ErrConflict), "complete advance accepted")
}

func TestApplyPulledPageCloseInvalidatesPrimedCache(t *testing.T) {
	store := openTestStore(t)
	change := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440231", "project"))
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), "550e8400-e29b-41d4-a716-446655440232", change))
	testutil.Require(t, store.syncInbox.known, "cache was not primed: %+v", store.syncInbox)
	testutil.NoError(t, store.Close())
	testutil.Require(t, !store.syncInbox.known, "cache=%+v", store.syncInbox)
}

func TestApplyPulledPageCorruptionInvalidatesCache(t *testing.T) {
	store := openTestStore(t)
	_, err := store.db.Exec(`DROP TABLE sync_cursor`)
	testutil.NoError(t, err)
	page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: "550e8400-e29b-41d4-a716-446655440224", Position: 1, Watermark: 1}, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440225", "project"))}}
	err = store.ApplyPulledPage(context.Background(), page, nil)
	testutil.Require(t, errors.Is(err, ErrCorrupt) && !store.syncInbox.known, "err=%v cache=%+v", err, store.syncInbox)
}

type bootstrapFake struct {
	discovery syncservice.Discovery
	pages     []syncservice.PullPage
	errAt     int
	probe     func() error
	pullProbe func() error
	discovers int
	pulls     int
	cursors   []syncservice.Cursor
	limits    []int
}

func (r *bootstrapFake) Discover(context.Context) (syncservice.Discovery, error) {
	r.discovers++
	if r.probe != nil {
		if err := r.probe(); err != nil {
			return syncservice.Discovery{}, err
		}
	}
	return r.discovery, nil
}

func (r *bootstrapFake) Pull(_ context.Context, cursor syncservice.Cursor, limit int) (syncservice.PullPage, error) {
	r.pulls++
	r.cursors = append(r.cursors, cursor)
	r.limits = append(r.limits, limit)
	if r.pullProbe != nil {
		if err := r.pullProbe(); err != nil {
			return syncservice.PullPage{}, err
		}
	}
	if r.probe != nil {
		if err := r.probe(); err != nil {
			return syncservice.PullPage{}, err
		}
	}
	if limit < 1 || limit > syncapi.MaxPullLimit || r.errAt == r.pulls || r.pulls > len(r.pages) {
		return syncservice.PullPage{}, errors.New("interrupted")
	}
	page := r.pages[r.pulls-1]
	_ = cursor
	return page, nil
}

func TestBootstrapSync(t *testing.T) {
	ctx := context.Background()
	history := "550e8400-e29b-41d4-a716-446655440240"
	discovery := syncservice.Discovery{ProtocolVersion: 1, HistoryID: history, Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}
	page := func(sequence, watermark int64, id string) syncservice.PullPage {
		return syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: sequence, Watermark: watermark}, HasMore: sequence < watermark, Changes: []syncservice.Change{pulledChange(t, sequence, 1, syncMutation("550e8400-e29b-41d4-a716-44665544024"+id, "project-"+id))}}
	}
	t.Run("empty zero completes and releases local access", func(t *testing.T) {
		store := openTestStore(t)
		remote := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history}}}, probe: func() error {
			_, err := store.ConfigureSyncProfile(ctx, SyncProfile{Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"})
			return err
		}}
		testutil.NoError(t, store.BootstrapSync(ctx, remote))
		var phase string
		var cursor int
		testutil.NoError(t, store.db.QueryRow(`SELECT phase FROM sync_bootstrap`).Scan(&phase))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
		testutil.Require(t, phase == "complete" && cursor == 0 && remote.discovers == 1 && remote.pulls == 1 && remote.cursors[0] == (syncservice.Cursor{HistoryID: history}) && remote.limits[0] == syncapi.DefaultPullLimit, "phase=%q cursor=%d calls=%d/%d pull=%+v/%d", phase, cursor, remote.discovers, remote.pulls, remote.cursors, remote.limits)
	})
	t.Run("freezes pages, resumes after interruption, and completes", func(t *testing.T) {
		store := openTestStore(t)
		first, second := page(1, 2, "1"), page(2, 2, "2")
		remote := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{first, second}, errAt: 2}
		testutil.Require(t, store.BootstrapSync(ctx, remote) != nil, "interruption succeeded")
		resume := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{second}}
		testutil.NoError(t, store.BootstrapSync(ctx, resume))
		var checkpoint BootstrapCheckpoint
		var phase string
		var payload []byte
		testutil.NoError(t, store.db.QueryRow(`SELECT checkpoint FROM sync_bootstrap`).Scan(&payload))
		testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
		testutil.NoError(t, store.db.QueryRow(`SELECT phase FROM sync_bootstrap`).Scan(&phase))
		testutil.Require(t, checkpoint.Position == 2 && checkpoint.Watermark == 2 && phase == "complete" && len(remote.cursors) == 2 && remote.cursors[0] == (syncservice.Cursor{HistoryID: history}) && remote.cursors[1] == (syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}) && resume.pulls == 1 && resume.cursors[0] == (syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}), "checkpoint=%+v/%q pulls=%+v/%+v", checkpoint, phase, remote.cursors, resume.cursors)
	})
	t.Run("complete is idempotent and mismatches fail closed", func(t *testing.T) {
		store := openTestStore(t)
		remote := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history}}}}
		testutil.NoError(t, store.BootstrapSync(ctx, remote))
		testutil.NoError(t, store.BootstrapSync(ctx, remote))
		wrong := &bootstrapFake{discovery: syncservice.Discovery{ProtocolVersion: 1, HistoryID: "550e8400-e29b-41d4-a716-446655440241", Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}}
		err := store.BootstrapSync(ctx, wrong)
		testutil.Require(t, remote.pulls == 1 && errors.Is(err, ErrConflict) && wrong.pulls == 0, "calls=%d/%d err=%v", remote.pulls, wrong.pulls, err)
	})
	t.Run("outbox and closed stores fail before discovery", func(t *testing.T) {
		store := openTestStore(t)
		enqueueMutation(t, store, syncMutation("550e8400-e29b-41d4-a716-446655440242", "pending"))
		remote := &bootstrapFake{discovery: discovery}
		testutil.Require(t, errors.Is(store.BootstrapSync(ctx, remote), ErrConflict) && remote.discovers == 0, "outbox calls=%d", remote.discovers)
		closed := openTestStore(t)
		testutil.NoError(t, closed.Close())
		testutil.Require(t, closed.BootstrapSync(ctx, remote) != nil && remote.discovers == 0, "closed calls=%d", remote.discovers)
	})
	t.Run("read-only store fails before discovery", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		writable := openPath(t, path)
		testutil.NoError(t, writable.Close())
		readOnly, err := OpenRead(ctx, path)
		testutil.NoError(t, err)
		defer readOnly.Close()
		remote := &bootstrapFake{discovery: discovery}
		testutil.Require(t, errors.Is(readOnly.BootstrapSync(ctx, remote), ErrConflict) && remote.discovers == 0 && remote.pulls == 0, "calls=%d/%d", remote.discovers, remote.pulls)
	})
	t.Run("more than one request limit completes", func(t *testing.T) {
		store := openTestStore(t)
		pages := make([]syncservice.PullPage, 0, syncapi.MaxPullLimit+1)
		for sequence := int64(1); sequence <= int64(syncapi.MaxPullLimit+1); sequence++ {
			mutation := syncMutation(fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", 300+sequence), fmt.Sprintf("project-%d", sequence))
			pages = append(pages, syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: sequence, Watermark: int64(syncapi.MaxPullLimit + 1)}, HasMore: sequence < int64(syncapi.MaxPullLimit+1), Changes: []syncservice.Change{pulledChange(t, sequence, 1, mutation)}})
		}
		remote := &bootstrapFake{discovery: discovery, pages: pages}
		testutil.NoError(t, store.BootstrapSync(ctx, remote))
		var position int
		var phase string
		testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&position))
		testutil.NoError(t, store.db.QueryRow(`SELECT phase FROM sync_bootstrap`).Scan(&phase))
		testutil.Require(t, remote.pulls == len(pages) && position == len(pages) && phase == "complete", "pulls=%d position=%d phase=%q", remote.pulls, position, phase)
	})
	t.Run("outbox injected after pull rolls back the page", func(t *testing.T) {
		store := openTestStore(t)
		remote := &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{page(1, 1, "6")}, pullProbe: func() error {
			enqueueMutation(t, store, syncMutation("550e8400-e29b-41d4-a716-446655440246", "raced"))
			return nil
		}}
		err := store.BootstrapSync(ctx, remote)
		var inbox, cursor, checkpoint int
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
		testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_bootstrap`).Scan(&checkpoint))
		testutil.Require(t, errors.Is(err, ErrConflict) && inbox == 0 && cursor == 0 && checkpoint == 0, "err=%v rows=%d/%d/%d", err, inbox, cursor, checkpoint)
	})
	t.Run("cursor without checkpoint and corrupt checkpoint fail before discovery", func(t *testing.T) {
		store := openTestStore(t)
		testutil.NoError(t, store.ApplyPulledChange(ctx, history, pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440247", "project"))))
		remote := &bootstrapFake{discovery: discovery}
		testutil.Require(t, errors.Is(store.BootstrapSync(ctx, remote), ErrConflict) && remote.discovers == 0, "cursor calls=%d", remote.discovers)
		store = openTestStore(t)
		testutil.NoError(t, store.ApplyPulledPage(ctx, page(1, 1, "8"), &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 1, Phase: "complete"}))
		_, err := store.db.Exec(`UPDATE sync_bootstrap SET checkpoint=X'7B7D'`)
		testutil.NoError(t, err)
		remote = &bootstrapFake{discovery: discovery}
		testutil.Require(t, errors.Is(store.BootstrapSync(ctx, remote), ErrCorrupt) && remote.discovers == 0, "corrupt calls=%d", remote.discovers)
	})
	t.Run("two handles converge without a fork", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		first, second := openPath(t, path), openPath(t, path)
		defer first.Close()
		defer second.Close()
		page := page(1, 1, "9")
		errs := make(chan error, 2)
		go func() {
			errs <- first.BootstrapSync(ctx, &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{page}})
		}()
		go func() {
			errs <- second.BootstrapSync(ctx, &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{page}})
		}()
		for range 2 {
			err := <-errs
			testutil.Require(t, err == nil || errors.Is(err, ErrConflict), "two-handle error=%v", err)
		}
		var inbox, cursor int
		testutil.NoError(t, first.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
		testutil.NoError(t, first.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
		testutil.Require(t, inbox == 1 && cursor == 1, "inbox=%d cursor=%d", inbox, cursor)
	})
	for name, mutate := range map[string]func(*syncservice.PullPage){
		"changed watermark": func(p *syncservice.PullPage) { p.Cursor.Watermark = 3; p.HasMore = true },
		"changed history":   func(p *syncservice.PullPage) { p.Cursor.HistoryID = "550e8400-e29b-41d4-a716-446655440243" },
		"nonprogress":       func(p *syncservice.PullPage) { p.Changes = nil; p.Cursor.Position = 1; p.HasMore = true },
	} {
		t.Run(name, func(t *testing.T) {
			store := openTestStore(t)
			first := page(1, 2, "4")
			testutil.NoError(t, store.ApplyPulledPage(ctx, first, &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 2, Phase: "observations"}))
			bad := page(2, 2, "5")
			mutate(&bad)
			err := store.BootstrapSync(ctx, &bootstrapFake{discovery: discovery, pages: []syncservice.PullPage{bad}})
			var position int64
			var phase string
			var payload []byte
			testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&position))
			testutil.NoError(t, store.db.QueryRow(`SELECT phase,checkpoint FROM sync_bootstrap`).Scan(&phase, &payload))
			var checkpoint BootstrapCheckpoint
			testutil.NoError(t, json.Unmarshal(payload, &checkpoint))
			testutil.Require(t, (errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict)) && position == 1 && phase == "observations" && checkpoint.Position == 1 && checkpoint.Watermark == 2, "err=%v cursor=%d checkpoint=%q/%+v", err, position, phase, checkpoint)
		})
	}
}

func TestApplyPulledPageCannotRetrofitBootstrapCheckpoint(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440248"
	first := pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440249", "project"))
	testutil.NoError(t, store.ApplyPulledChange(context.Background(), history, first))
	second := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 2}, Changes: []syncservice.Change{pulledChange(t, 2, 1, syncMutation("550e8400-e29b-41d4-a716-446655440250", "second"))}}
	err := store.ApplyPulledPage(context.Background(), second, &BootstrapCheckpoint{HistoryID: history, Position: 2, Watermark: 2, Phase: "complete"})
	var cursor, projects, checkpoints int
	testutil.NoError(t, store.db.QueryRow(`SELECT position FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_bootstrap`).Scan(&checkpoints))
	testutil.Require(t, errors.Is(err, ErrConflict) && cursor == 1 && projects == 1 && checkpoints == 0, "err=%v cursor=%d projects=%d checkpoints=%d", err, cursor, projects, checkpoints)
}

func TestApplyPulledPageRejectsEarlyCompleteCheckpointBeforeWrites(t *testing.T) {
	store := openTestStore(t)
	history := "550e8400-e29b-41d4-a716-446655440251"
	page := syncservice.PullPage{Cursor: syncservice.Cursor{HistoryID: history, Position: 1, Watermark: 2}, HasMore: true, Changes: []syncservice.Change{pulledChange(t, 1, 1, syncMutation("550e8400-e29b-41d4-a716-446655440252", "project"))}}
	err := store.ApplyPulledPage(context.Background(), page, &BootstrapCheckpoint{HistoryID: history, Position: 1, Watermark: 2, Phase: "complete"})
	var projects, inbox, cursor, checkpoints int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_inbox`).Scan(&inbox))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_cursor`).Scan(&cursor))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_bootstrap`).Scan(&checkpoints))
	testutil.Require(t, errors.Is(err, ErrInvalid) && projects == 0 && inbox == 0 && cursor == 0 && checkpoints == 0, "err=%v rows=%d/%d/%d/%d", err, projects, inbox, cursor, checkpoints)
}

func specialChange(t *testing.T, sequence, version int64, disposition syncservice.ChangeDisposition, conflictID string, mutation syncservice.Mutation) syncservice.Change {
	t.Helper()
	hashVersion := 2
	change := syncservice.Change{Sequence: sequence, CanonicalVersion: version, HashVersion: &hashVersion, ChangeDisposition: disposition, ConflictID: conflictID, Mutation: mutation}
	change.ChangeHash, _ = syncservice.CanonicalChangeHash(change)
	return change
}
