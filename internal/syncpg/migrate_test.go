package syncpg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func testConn(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("VGXNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("VGXNESS_TEST_POSTGRES_DSN is not set; skipping real PostgreSQL test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test PostgreSQL: %v", err)
	}
	name := "syncpg_" + randomHex(t, 12)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize()); err != nil {
		conn.Close(ctx)
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{name}.Sanitize()); err != nil {
		conn.Close(ctx)
		t.Fatalf("set test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE")
		_ = conn.Close(context.Background())
	})
	return conn
}

func randomHex(t *testing.T, size int) string {
	t.Helper()
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate test schema name: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestMigrateIdempotentAndRejectsUnsafeLedger(t *testing.T) {
	ctx := context.Background()
	conn := testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	for name, mutation := range map[string]struct{ mutate, restore string }{
		"checksum": {"UPDATE sync_schema_migrations SET checksum = 'wrong'", "UPDATE sync_schema_migrations SET checksum = $1"},
		"dirty":    {"UPDATE sync_schema_migrations SET dirty = true", "UPDATE sync_schema_migrations SET dirty = false"},
		"newer":    {"INSERT INTO sync_schema_migrations(version, checksum, dirty) VALUES (999, 'x', false)", "DELETE FROM sync_schema_migrations WHERE version = 999"},
	} {
		t.Run(name, func(t *testing.T) {
			var checksum string
			if err := conn.QueryRow(ctx, "SELECT checksum FROM sync_schema_migrations WHERE version = 1").Scan(&checksum); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, mutation.mutate); err != nil {
				t.Fatal(err)
			}
			err := Migrate(ctx, conn)
			if err == nil || !strings.Contains(err.Error(), "syncpg migration") {
				t.Fatalf("Migrate() error = %v, want safe migration rejection", err)
			}
			if mutation.restore == "UPDATE sync_schema_migrations SET checksum = $1" {
				_, err = conn.Exec(ctx, mutation.restore, checksum)
			} else {
				_, err = conn.Exec(ctx, mutation.restore)
			}
			if err != nil {
				t.Fatalf("restore isolated test ledger: %v", err)
			}
		})
	}
}

func TestMigrationFailureRollsBackSchema(t *testing.T) {
	ctx := context.Background()
	conn := testConn(t)
	err := applyMigrations(ctx, conn, []migration{{version: 1, sql: "CREATE TABLE partial_schema(id int); SELECT no_such_function()"}})
	if err == nil {
		t.Fatal("applyMigrations accepted invalid migration")
	}
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'partial_schema')").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("failed migration left partial schema")
	}
}

func TestMigrateRejectsSchemaDrift(t *testing.T) {
	for name, drift := range map[string]string{
		"missing column": "ALTER TABLE projects DROP COLUMN payload",
		"missing table":  "DROP TABLE observation_references",
		"weakened index": "DROP INDEX observations_topic_identity; CREATE INDEX observations_topic_identity ON observations(owner_id,id)",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, conn := context.Background(), testConn(t)
			if err := Migrate(ctx, conn); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, drift); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(ctx, conn); err == nil {
				t.Fatal("schema drift accepted")
			}
		})
	}
}

func TestMigrateResolutionConflictIDsConstraint(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := applyMigrations(ctx, conn, migrations[:1]); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 3 {
		t.Fatalf("migrations = %d, want 3", len(migrations))
	}
	var checksum string
	if err := conn.QueryRow(ctx, "SELECT checksum FROM sync_schema_migrations WHERE version=2").Scan(&checksum); err != nil || checksum != migrationChecksum(migrations[1].sql) {
		t.Fatalf("v2 ledger = %q, %v", checksum, err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO owners(id) VALUES ('11111111-1111-1111-1111-111111111111')"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('22222222-2222-2222-2222-222222222222','11111111-1111-1111-1111-111111111111','device',decode(repeat('00',32),'hex'),'p')"); err != nil {
		t.Fatal(err)
	}
	valid := "INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version,resolution_conflict_ids) VALUES ('11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222222',$1,decode('00','hex'),'resolve','record',1,'accepted',$2,2,ARRAY['33333333-3333-3333-3333-333333333333'::uuid])"
	if _, err := conn.Exec(ctx, valid, "44444444-4444-4444-4444-444444444444", 1); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version,resolution_conflict_ids) VALUES ('11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222222','55555555-5555-5555-5555-555555555555',decode('00','hex'),'resolve','record',1,'accepted',2,2,'{}')",
		"INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version,resolution_conflict_ids) VALUES ('11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222222','66666666-6666-6666-6666-666666666666',decode('00','hex'),'update','record',1,'accepted',3,2,ARRAY['33333333-3333-3333-3333-333333333333'::uuid])",
	} {
		if _, err := conn.Exec(ctx, query); err == nil {
			t.Fatalf("constraint accepted %q", query)
		}
	}
}

func TestMigrateRefusesUnrecoverableV1ResolveAtomically(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	mustNoError(t, applyMigrations(ctx, conn, migrations[:1]))
	for _, statement := range []string{
		"INSERT INTO owners(id) VALUES ('11111111-1111-1111-1111-111111111111')",
		"INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ('22222222-2222-2222-2222-222222222222','11111111-1111-1111-1111-111111111111','device',decode(repeat('00',32),'hex'),'p')",
		"INSERT INTO mutations(owner_id,device_id,mutation_id,request_hash,kind,record_id,base_version,disposition,canonical_seq,canonical_version) VALUES ('11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222222','33333333-3333-3333-3333-333333333333',decode('00','hex'),'resolve','target',1,'accepted',1,2)",
	} {
		_, err := conn.Exec(ctx, statement)
		mustNoError(t, err)
	}
	if err := Migrate(ctx, conn); err == nil {
		t.Fatal("Migrate accepted unrecoverable v1 resolve")
	}
	var column, ledger, resolves int
	mustNoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='mutations' AND column_name='resolution_conflict_ids'").Scan(&column))
	mustNoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM sync_schema_migrations WHERE version=2").Scan(&ledger))
	mustNoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM mutations WHERE kind='resolve'").Scan(&resolves))
	if column != 0 || ledger != 0 || resolves != 1 {
		t.Fatalf("failed upgrade changed v1 state: %d/%d/%d", column, ledger, resolves)
	}
}
