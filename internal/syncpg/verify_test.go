package syncpg

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

const (
	verifyOwner  = "11111111-1111-1111-1111-111111111111"
	verifyDevice = "22222222-2222-2222-2222-222222222222"
	verifyFirst  = "33333333-3333-3333-3333-333333333333"
)

func verifyConn(t *testing.T) (context.Context, *pgx.Conn) {
	t.Helper()
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	return ctx, conn
}

func addVerifyOwner(t *testing.T, ctx context.Context, conn *pgx.Conn, next int64) {
	t.Helper()
	if _, err := conn.Exec(ctx, "INSERT INTO owners(id) VALUES ($1)", verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO owner_sync_state(owner_id,history_id,next_seq) VALUES ($1,$2,$3)", verifyOwner, "44444444-4444-4444-4444-444444444444", next); err != nil {
		t.Fatal(err)
	}
}

func requireRecoveryError(t *testing.T, err error) {
	t.Helper()
	if err == nil || !errors.Is(err, ErrRecovery) {
		t.Fatalf("VerifyRecovery() error = %v, want ErrRecovery", err)
	}
}

func TestVerifyRecoveryValidHistory(t *testing.T) {
	ctx, conn := verifyConn(t)
	addVerifyOwner(t, ctx, conn, 1)
	if err := VerifyRecovery(ctx, conn); err != nil {
		t.Fatalf("VerifyRecovery() empty history: %v", err)
	}

	if _, err := conn.Exec(ctx, "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ($1,$2,'device',decode(repeat('00',32),'hex'),'p')", verifyDevice, verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO projects(owner_id,id,version,payload) VALUES ($1,'project',1,'{}')", verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ($1,$2,$3,decode('00','hex'),'create','project',0,'accepted',1,1)", verifyOwner, verifyDevice, verifyFirst); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	if err := conn.QueryRow(ctx, `
		INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot)
		VALUES ($1,'project','project',1,$2,$3,0,'accepted','{}') RETURNING id`, verifyOwner, verifyDevice, verifyFirst).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,canonical_version,version_id) VALUES ($1,1,$2,$3,'accepted','project','project',1,$4)", verifyOwner, verifyDevice, verifyFirst, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "UPDATE owner_sync_state SET next_seq=2 WHERE owner_id=$1", verifyOwner); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecovery(ctx, conn); err != nil {
		t.Fatalf("VerifyRecovery() canonical history: %v", err)
	}
}

func TestVerifyRecoveryConflictHistoryRequiresCanonicalLinkage(t *testing.T) {
	ctx, conn := verifyConn(t)
	addVerifyOwner(t, ctx, conn, 2)
	if _, err := conn.Exec(ctx, "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ($1,$2,'device',decode(repeat('00',32),'hex'),'p')", verifyDevice, verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ($1,$2,$3,decode('00','hex'),'update','project',1,'conflict',1,3)", verifyOwner, verifyDevice, verifyFirst); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	if err := conn.QueryRow(ctx, "INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,'project','project',2,$2,$3,1,'conflict','{}') RETURNING id", verifyOwner, verifyDevice, verifyFirst).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,canonical_version,version_id) VALUES ($1,1,$2,$3,'conflict','project','project',3,$4)", verifyOwner, verifyDevice, verifyFirst, versionID); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecovery(ctx, conn); err != nil {
		t.Fatalf("VerifyRecovery() valid conflict history: %v", err)
	}
	if _, err := conn.Exec(ctx, "UPDATE changes SET canonical_version=2"); err != nil {
		t.Fatal(err)
	}
	requireRecoveryError(t, VerifyRecovery(ctx, conn))
}

func TestVerifyRecoveryRejectsInvalidRecoveryState(t *testing.T) {
	for name, prepare := range map[string]func(*testing.T, context.Context, *pgx.Conn){
		"missing state": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			if _, err := conn.Exec(ctx, "INSERT INTO owners(id) VALUES ($1)", verifyOwner); err != nil {
				t.Fatal(err)
			}
		},
		"two owners": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 1)
			if _, err := conn.Exec(ctx, "INSERT INTO owners(id) VALUES ('55555555-5555-5555-5555-555555555555')"); err != nil {
				t.Fatal(err)
			}
		},
		"sequence gap and next mismatch": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 3)
			addCanonicalChange(t, ctx, conn, 1, verifyFirst, "one")
			addCanonicalChange(t, ctx, conn, 3, "66666666-6666-6666-6666-666666666666", "three")
		},
		"canonical mutation without change": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 2)
			addCanonicalMutation(t, ctx, conn, 1, verifyFirst, "one")
		},
		"tombstoned observation without tombstone": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 1)
			if _, err := conn.Exec(ctx, "INSERT INTO projects(owner_id,id,version,payload) VALUES ($1,'project',1,'{}')", verifyOwner); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, "INSERT INTO observations(owner_id,id,project_id,scope,type,title,content,lifecycle,review_state,version) VALUES ($1,'observation','project','project','type','title','content','tombstoned','clear',1)", verifyOwner); err != nil {
				t.Fatal(err)
			}
		},
		"migration checksum drift": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 1)
			if _, err := conn.Exec(ctx, "UPDATE sync_schema_migrations SET checksum = 'wrong'"); err != nil {
				t.Fatal(err)
			}
		},
		"catalog drift": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 1)
			if _, err := conn.Exec(ctx, "ALTER TABLE projects DROP COLUMN payload"); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, conn := verifyConn(t)
			prepare(t, ctx, conn)
			requireRecoveryError(t, VerifyRecovery(ctx, conn))
		})
	}
}

func TestVerifyRecoveryRejectsEmptyDatabase(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	requireRecoveryError(t, VerifyRecovery(ctx, conn))
}

func TestVerifyRecoveryPreservesCanceledContext(t *testing.T) {
	_, conn := verifyConn(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyRecovery(ctx, conn); err != context.Canceled {
		t.Fatalf("VerifyRecovery() error = %v, want context.Canceled", err)
	}
}

func TestVerifyRecoveryRejectsReviewGaps(t *testing.T) {
	for name, prepare := range map[string]func(*testing.T, context.Context, *pgx.Conn){
		"canonical change without version": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 2)
			addCanonicalChange(t, ctx, conn, 1, verifyFirst, "project")
		},
		"canonical version differs from record version": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 2)
			addVersionedCanonicalChange(t, ctx, conn, "project", "project", "create", "accepted", 2)
		},
		"active observation has create tombstone": func(t *testing.T, ctx context.Context, conn *pgx.Conn) {
			addVerifyOwner(t, ctx, conn, 2)
			if _, err := conn.Exec(ctx, "INSERT INTO projects(owner_id,id,version,payload) VALUES ($1,'project',1,'{}')", verifyOwner); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, "INSERT INTO observations(owner_id,id,project_id,scope,type,title,content,lifecycle,review_state,version) VALUES ($1,'observation','project','project','type','title','content','active','clear',1)", verifyOwner); err != nil {
				t.Fatal(err)
			}
			versionID := addVersionedCanonicalChange(t, ctx, conn, "observation", "observation", "create", "accepted", 1)
			if _, err := conn.Exec(ctx, "INSERT INTO tombstones(owner_id,record_kind,record_id,version_id,mutation_device_id,mutation_id) VALUES ($1,'observation','observation',$2,$3,$4)", verifyOwner, versionID, verifyDevice, verifyFirst); err != nil {
				t.Fatal(err)
			}
		},
		"temporary tables shadow invalid target": prepareShadowedHistory,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, conn := verifyConn(t)
			prepare(t, ctx, conn)
			requireRecoveryError(t, VerifyRecovery(ctx, conn))
		})
	}
}

func TestVerifyRecoveryAcceptsProjectTombstone(t *testing.T) {
	ctx, conn := verifyConn(t)
	addVerifyOwner(t, ctx, conn, 2)
	if _, err := conn.Exec(ctx, "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ($1,$2,'device',decode(repeat('00',32),'hex'),'p')", verifyDevice, verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ($1,$2,$3,decode('00','hex'),'tombstone','project',1,'accepted',1,2)", verifyOwner, verifyDevice, verifyFirst); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	if err := conn.QueryRow(ctx, "INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,'project','project',2,$2,$3,1,'accepted','{}') RETURNING id", verifyOwner, verifyDevice, verifyFirst).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,canonical_version,version_id) VALUES ($1,1,$2,$3,'accepted','project','project',2,$4)", verifyOwner, verifyDevice, verifyFirst, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO tombstones(owner_id,record_kind,record_id,version_id,mutation_device_id,mutation_id) VALUES ($1,'project','project',$2,$3,$4)", verifyOwner, versionID, verifyDevice, verifyFirst); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecovery(ctx, conn); err != nil {
		t.Fatalf("VerifyRecovery() project tombstone: %v", err)
	}
}

func TestMigrateIgnoresTemporaryLedgerShadow(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if _, err := conn.Exec(ctx, "CREATE TEMP TABLE sync_schema_migrations(version bigint)"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("Migrate() with temporary ledger shadow: %v", err)
	}
}

func addCanonicalMutation(t *testing.T, ctx context.Context, conn *pgx.Conn, seq int64, mutationID, recordID string) {
	t.Helper()
	if _, err := conn.Exec(ctx, "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ($1,$2,'device',decode(repeat('00',32),'hex'),'p') ON CONFLICT DO NOTHING", verifyDevice, verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ($1,$2,$3,decode('00','hex'),'create',$4,0,'accepted',$5,1)", verifyOwner, verifyDevice, mutationID, recordID, seq); err != nil {
		t.Fatal(err)
	}
}

func addCanonicalChange(t *testing.T, ctx context.Context, conn *pgx.Conn, seq int64, mutationID, recordID string) {
	t.Helper()
	addCanonicalMutation(t, ctx, conn, seq, mutationID, recordID)
	if _, err := conn.Exec(ctx, `
		INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,canonical_version)
		VALUES ($1,$2,$3,$4,'accepted','project',$5,1)`, verifyOwner, seq, verifyDevice, mutationID, recordID); err != nil {
		t.Fatal(err)
	}
}

func addVersionedCanonicalChange(t *testing.T, ctx context.Context, conn *pgx.Conn, recordKind, recordID, kind, disposition string, recordVersion int64) int64 {
	t.Helper()
	if _, err := conn.Exec(ctx, "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ($1,$2,'device',decode(repeat('00',32),'hex'),'p') ON CONFLICT DO NOTHING", verifyDevice, verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ($1,$2,$3,decode('00','hex'),$4,$5,0,$6,1,1)", verifyOwner, verifyDevice, verifyFirst, kind, recordID, disposition); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	if err := conn.QueryRow(ctx, "INSERT INTO record_versions(owner_id,record_kind,record_id,record_version,source_device_id,source_mutation_id,base_version,disposition,snapshot) VALUES ($1,$2,$3,$4,$5,$6,0,$7,'{}') RETURNING id", verifyOwner, recordKind, recordID, recordVersion, verifyDevice, verifyFirst, disposition).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO changes(owner_id,seq,mutation_device_id,mutation_id,change_kind,record_kind,record_id,canonical_version,version_id) VALUES ($1,1,$2,$3,$4,$5,$6,1,$7)", verifyOwner, verifyDevice, verifyFirst, disposition, recordKind, recordID, versionID); err != nil {
		t.Fatal(err)
	}
	return versionID
}

func prepareShadowedHistory(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	addVerifyOwner(t, ctx, conn, 1)
	if _, err := conn.Exec(ctx, "INSERT INTO owners(id) VALUES ('55555555-5555-5555-5555-555555555555')"); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	ledger := pgx.Identifier{schema, "sync_schema_migrations"}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE TEMP TABLE sync_schema_migrations AS TABLE "+ledger); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE TEMP TABLE owners(id uuid)",
		"CREATE TEMP TABLE owner_sync_state(owner_id uuid, history_id uuid, next_seq bigint)",
		"CREATE TEMP TABLE changes(owner_id uuid, seq bigint, mutation_device_id uuid, mutation_id uuid, change_kind text, record_kind text, record_id text, canonical_version bigint)",
		"CREATE TEMP TABLE mutations(owner_id uuid, device_id uuid, mutation_id uuid, record_id text, disposition text, canonical_seq bigint, canonical_version bigint)",
		"CREATE TEMP TABLE observations(owner_id uuid, id text, lifecycle text)",
		"CREATE TEMP TABLE tombstones(owner_id uuid, record_kind text, record_id text)",
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(ctx, "INSERT INTO owners VALUES ($1)", verifyOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO owner_sync_state VALUES ($1,$2,1)", verifyOwner, "44444444-4444-4444-4444-444444444444"); err != nil {
		t.Fatal(err)
	}
}
