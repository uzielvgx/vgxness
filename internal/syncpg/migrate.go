package syncpg

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrMigration = errors.New("syncpg migration")

//go:embed migrations/001_initial.sql
var initialSchema string

type migration struct {
	version int64
	sql     string
}

var migrations = []migration{{version: 1, sql: initialSchema}}

// Migrate applies embedded, forward-only PostgreSQL schema migrations.
func Migrate(ctx context.Context, conn *pgx.Conn) error {
	return applyMigrations(ctx, conn, migrations)
}

func applyMigrations(ctx context.Context, conn *pgx.Conn, steps []migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return migrationError(ctx, "begin")
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(835301947)); err != nil {
		return migrationError(ctx, "lock")
	}
	schema, err := currentSchema(ctx, tx)
	if err != nil {
		return migrationError(ctx, "ledger")
	}
	if _, err = tx.Exec(ctx, "SET LOCAL search_path TO "+pgx.Identifier{schema}.Sanitize()+", pg_catalog, pg_temp"); err != nil {
		return migrationError(ctx, "ledger")
	}
	ledger := pgx.Identifier{schema, "sync_schema_migrations"}.Sanitize()
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+ledger+` (
		version bigint PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now(),
		checksum text NOT NULL, fingerprint text NOT NULL DEFAULT '', dirty boolean NOT NULL DEFAULT false)`); err != nil {
		return migrationError(ctx, "ledger")
	}
	if err = validateMigrations(ctx, tx, steps, schema); err != nil {
		return err
	}
	for _, step := range steps {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+ledger+" WHERE version = $1)", step.version).Scan(&exists); err != nil {
			return migrationError(ctx, "ledger")
		}
		if exists {
			continue
		}
		checksum := migrationChecksum(step.sql)
		if _, err = tx.Exec(ctx, "INSERT INTO "+ledger+"(version, checksum, dirty) VALUES ($1, $2, true)", step.version, checksum); err != nil {
			return migrationError(ctx, "mark")
		}
		if _, err = tx.Exec(ctx, step.sql); err != nil {
			return migrationError(ctx, "apply")
		}
		fingerprint, err := schemaFingerprint(ctx, tx, schema)
		if err != nil {
			return migrationError(ctx, "fingerprint")
		}
		if _, err = tx.Exec(ctx, "UPDATE "+ledger+" SET dirty = false, fingerprint = $2 WHERE version = $1", step.version, fingerprint); err != nil {
			return migrationError(ctx, "mark")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return migrationError(ctx, "commit")
	}
	return nil
}

type migrationQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func validateMigrations(ctx context.Context, q migrationQuerier, steps []migration, schema string) error {
	want := make(map[int64]string, len(steps))
	var wantFingerprint string
	for _, step := range steps {
		want[step.version] = migrationChecksum(step.sql)
	}
	ledger := pgx.Identifier{schema, "sync_schema_migrations"}.Sanitize()
	rows, err := q.Query(ctx, "SELECT version, checksum, dirty, fingerprint FROM "+ledger+" ORDER BY version")
	if err != nil {
		return migrationError(ctx, "ledger")
	}
	defer rows.Close()
	for rows.Next() {
		var version int64
		var checksum string
		var dirty bool
		var fingerprint string
		if err := rows.Scan(&version, &checksum, &dirty, &fingerprint); err != nil {
			return migrationError(ctx, "ledger")
		}
		if dirty || want[version] == "" || want[version] != checksum || fingerprint == "" {
			return migrationError(ctx, "validate")
		}
		wantFingerprint = fingerprint
	}
	if rows.Err() != nil {
		return migrationError(ctx, "ledger")
	}
	if wantFingerprint != "" {
		fingerprint, err := schemaFingerprint(ctx, q, schema)
		if err != nil || fingerprint != wantFingerprint {
			return migrationError(ctx, "validate")
		}
	}
	return nil
}

func schemaFingerprint(ctx context.Context, q migrationQuerier, schema string) (string, error) {
	rows, err := q.Query(ctx, `SELECT item FROM (
		SELECT 'r|' || relkind::text || '|' || relname item FROM pg_class WHERE relnamespace = $1::regnamespace AND relkind IN ('r','S')
		UNION ALL SELECT 'i|' || c.relname || '|' || replace(pg_get_indexdef(i.indexrelid), quote_ident($1::text) || '.', '') || '|' || i.indisunique::text || '|' || i.indisvalid::text || '|' || coalesce(pg_get_expr(i.indpred, i.indrelid),'') FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relnamespace=$1::regnamespace
		UNION ALL SELECT 'c|' || r.relname || '|' || c.conname || '|' || c.contype::text || '|' || c.convalidated::text || '|' || replace(pg_get_constraintdef(c.oid), quote_ident($1::text) || '.', '') FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid WHERE c.connamespace=$1::regnamespace
		UNION ALL SELECT 'a|' || c.relname || '|' || a.attnum::text || '|' || a.attname || '|' || format_type(a.atttypid,a.atttypmod) || '|' || a.attnotnull::text || '|' || a.attidentity::text || '|' || a.attgenerated::text || '|' || replace(coalesce(pg_get_expr(d.adbin,d.adrelid),''),quote_ident($1::text)||'.','') FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE c.relnamespace=$1::regnamespace AND a.attnum>0 AND NOT a.attisdropped
		UNION ALL SELECT 's|' || c.relname || '|' || s.seqstart::text || '|' || s.seqincrement::text || '|' || s.seqmax::text || '|' || s.seqmin::text || '|' || s.seqcache::text || '|' || s.seqcycle::text FROM pg_sequence s JOIN pg_class c ON c.oid=s.seqrelid WHERE c.relnamespace=$1::regnamespace
	) x ORDER BY item`, schema)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err != nil {
			return "", err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return migrationChecksum(strings.Join(items, "\n")), nil
}

func currentSchema(ctx context.Context, q migrationQuerier) (string, error) {
	var schema string
	if err := q.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil || schema == "" {
		return "", errors.New("current schema")
	}
	return schema, nil
}

func migrationChecksum(sql string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(sql))) }

func migrationError(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrMigration, operation)
}
