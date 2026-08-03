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
