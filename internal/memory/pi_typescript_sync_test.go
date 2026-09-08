package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/syncservice"
)

// TestPiTypeScriptSync fixes the JSON fixture vocabulary consumed by the Pi
// implementation.  It is deliberately an oracle over the shared Go contract,
// not a second implementation of the TypeScript service.
func TestPiTypeScriptSync(t *testing.T) {
	when := time.Date(2025, 1, 2, 3, 4, 5, 123456789, time.UTC)
	mutation := syncservice.Mutation{
		MutationID:  "550e8400-e29b-41d4-a716-446655440000",
		RecordID:    "550e8400-e29b-41d4-a716-446655440001",
		RecordKind:  syncservice.RecordKindObservation,
		Kind:        syncservice.MutationUpdate,
		BaseVersion: 9007199254740993,
		Observation: &syncservice.Observation{ID: "550e8400-e29b-41d4-a716-446655440001", ProjectID: "550e8400-e29b-41d4-a716-446655440002", Scope: "project", Type: "fact", Content: "fixture", Provenance: syncservice.Provenance{Producer: "test"}, Lifecycle: syncservice.LifecycleActive, Review: syncservice.ReviewClear, CreatedAt: when, UpdatedAt: when},
	}
	encoded, err := json.Marshal(mutation)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"record_kind":"observation"`)) || !bytes.Contains(encoded, []byte(`"kind":"update"`)) || !bytes.Contains(encoded, []byte(`"base_version":9007199254740993`)) {
		t.Fatalf("unexpected sync fixture: %s", encoded)
	}
	if string(encoded) == "" || !json.Valid(encoded) {
		t.Fatalf("invalid fixture JSON")
	}
}

// Exercise actual TypeScript writes through the Go reader, not a duplicated
// fixture. These rows are the shared database boundary of the two runtimes.
func TestPiTypeScriptSyncOutboxReadableByGo(t *testing.T) {
	root, rootErr := filepath.EvalSymlinks(t.TempDir())
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	source := piTypeScriptFileURI(filepath.Join(cwd, "..", "..", "packages", "pi", "src"))
	path := filepath.Join(root, "memory.db")
	script := fmt.Sprintf(`import {SQLiteDatabase} from '%s/sqlite/node-sqlite.ts';
import {applyMigrations} from '%s/sqlite/migrations.ts';
import {backfillSyncProject} from '%s/service/sync.ts';
import {translateMutation} from '%s/service/sync-state.ts';
const database=new SQLiteDatabase(process.argv[2]);applyMigrations(database);const db=database.db;
db.prepare("INSERT INTO projects VALUES('project',0)").run();
db.prepare("INSERT INTO observations(id,title,project_id,scope,type,content,producer,state,created_at,updated_at) VALUES('obs','Title','project','project','fact','Exact <content>','test','active',?,?)").run(1700000000123456789n,1700000000123456789n);
backfillSyncProject({database,project:'project',mode:'full',now:()=>1700000000123456789n},100);database.close();`, source, source, source, source)
	scriptPath := filepath.Join(root, "sync.mts")
	if err = os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "--experimental-strip-types", scriptPath, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Node: %s: %v", out, err)
	}
	store := openPath(t, path)
	defer store.Close()
	rows, err := store.db.Query(`SELECT mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,last_error_code,next_attempt_at,created_at,updated_at FROM sync_outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, rk, rid, kind, state, code string
		var base, pv, attempts, next, created, updated int64
		var payload []byte
		if err = rows.Scan(&id, &rk, &rid, &kind, &base, &pv, &payload, &state, &attempts, &code, &next, &created, &updated); err != nil {
			t.Fatal(err)
		}
		entry, err := decodeSyncOutboxEntry(id, rk, rid, kind, base, pv, payload, state, attempts, code, next, created, updated)
		if err != nil {
			t.Fatalf("Go rejects TS outbox: %v", err)
		}
		expected, err := json.Marshal(entry.Mutation)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(expected, payload) {
			t.Fatalf("Go/TS canonical bytes differ\nGo %s\nTS %s", expected, payload)
		}
		if id != backfillMutationID(rk, rid) || created != 1700000000123456789 {
			t.Fatalf("identity/timestamp mismatch")
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("rows %d", count)
	}
}

func TestPiTypeScriptClaimIsolatesGoCompositeSessions(t *testing.T) {
	root, rootErr := filepath.EvalSymlinks(t.TempDir())
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	path := filepath.Join(root, "memory.db")
	store := openPath(t, path)
	if _, err := store.db.Exec("INSERT INTO projects(id,sync_version) VALUES('A',1),('B',1); INSERT INTO sessions(id,project_id,sync_version) VALUES('same','A',0),('same','B',0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackfillSyncProject(t.Context(), "B", 100); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	source := piTypeScriptFileURI(filepath.Join(cwd, "..", "..", "packages", "pi", "src"))
	script := fmt.Sprintf(`import {SQLiteDatabase} from '%s/sqlite/node-sqlite.ts';import {claim} from '%s/service/sync.ts';const database=new SQLiteDatabase(process.argv[2]);const claims=claim({database,project:'A'},16);process.stdout.write(JSON.stringify({claimed:claims.length}));database.close();`, source, source)
	scriptPath := filepath.Join(root, "claim.mts")
	if err = os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", "--experimental-strip-types", scriptPath, path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"claimed":0}` {
		t.Fatalf("cross-project claim: %s", out)
	}
	store = openPath(t, path)
	defer store.Close()
	var claims int
	if err = store.db.QueryRow("SELECT count(*) FROM sync_outbox_claims").Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if claims != 0 {
		t.Fatal("foreign session lease was mutated")
	}
	var version int
	if err = store.db.QueryRow("SELECT sync_version FROM sessions WHERE id='same' AND project_id='B'").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Fatal("foreign session version changed")
	}
}
