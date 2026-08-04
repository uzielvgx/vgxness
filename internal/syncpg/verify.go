package syncpg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ErrRecovery indicates that a PostgreSQL synchronization database cannot be
// safely used to recover canonical synchronization state.
var ErrRecovery = errors.New("syncpg recovery")

// VerifyRecovery confirms that the migrated schema contains one complete,
// internally consistent owner history without reading synchronized content.
func VerifyRecovery(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return recoveryError(ctx, "begin")
	}
	defer tx.Rollback(context.Background())

	schema, err := recoverySchema(ctx, tx)
	if err != nil {
		return recoveryError(ctx, "schema")
	}
	if err := validateMigrations(ctx, tx, migrations, schema); err != nil {
		return recoveryError(ctx, "validate")
	}
	table := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	ledger, owners, state := table("sync_schema_migrations"), table("owners"), table("owner_sync_state")
	changes, mutations, versions := table("changes"), table("mutations"), table("record_versions")
	observations, tombstones := table("observations"), table("tombstones")
	var ok bool
	if err := tx.QueryRow(ctx, "SELECT count(*) = $1 FROM "+ledger, len(migrations)).Scan(&ok); err != nil || !ok {
		return recoveryError(ctx, "validate")
	}
	for _, query := range []string{
		fmt.Sprintf(`SELECT count(*) = 1 AND (SELECT count(*) FROM %s) = 1 FROM %s`, state, owners),
		fmt.Sprintf(`SELECT s.next_seq = COALESCE(MAX(c.seq), 0) + 1
		 AND (COUNT(c.seq) = 0 OR MIN(c.seq) = 1)
		 AND COUNT(c.seq) = COALESCE(MAX(c.seq), 0)
		 FROM %s s LEFT JOIN %s c ON c.owner_id = s.owner_id
		 GROUP BY s.next_seq`, state, changes),
		fmt.Sprintf(`SELECT NOT EXISTS (
		 SELECT 1 FROM %s c
		 LEFT JOIN %s m ON m.owner_id = c.owner_id
		  AND m.device_id = c.mutation_device_id AND m.mutation_id = c.mutation_id
		 LEFT JOIN %s v ON v.owner_id = c.owner_id AND v.id = c.version_id
		  AND v.record_kind = c.record_kind AND v.record_id = c.record_id
		 WHERE c.version_id IS NULL OR m.owner_id IS NULL OR v.id IS NULL
		  OR m.canonical_seq IS DISTINCT FROM c.seq
		  OR m.record_id IS DISTINCT FROM c.record_id
		  OR m.disposition IS DISTINCT FROM c.change_kind
		  OR m.canonical_version IS DISTINCT FROM c.canonical_version
			 OR (c.change_kind = 'conflict' AND (v.record_version IS DISTINCT FROM m.base_version + 1 OR v.base_version IS DISTINCT FROM m.base_version))
			 OR (c.change_kind = 'accepted' AND (v.base_version IS DISTINCT FROM m.base_version OR v.record_version IS DISTINCT FROM m.base_version + 1 OR v.record_version IS DISTINCT FROM c.canonical_version))
		  OR v.source_device_id IS DISTINCT FROM c.mutation_device_id
		  OR v.source_mutation_id IS DISTINCT FROM c.mutation_id
		  OR v.disposition IS DISTINCT FROM c.change_kind
		 UNION ALL
		 SELECT 1 FROM %s m
		 LEFT JOIN %s c ON c.owner_id = m.owner_id AND c.seq = m.canonical_seq
		 WHERE m.disposition IN ('accepted', 'conflict')
		  AND (c.owner_id IS NULL OR c.mutation_device_id IS DISTINCT FROM m.device_id
		   OR c.mutation_id IS DISTINCT FROM m.mutation_id
		   OR c.record_id IS DISTINCT FROM m.record_id
		   OR c.change_kind IS DISTINCT FROM m.disposition
		   OR c.canonical_version IS DISTINCT FROM m.canonical_version)
		)`, changes, mutations, versions, mutations, changes),
		fmt.Sprintf(`SELECT NOT EXISTS (
		 SELECT 1 FROM %s t
		 LEFT JOIN %s m ON m.owner_id = t.owner_id AND m.device_id = t.mutation_device_id
		  AND m.mutation_id = t.mutation_id AND m.record_id = t.record_id
		  AND m.kind = 'tombstone' AND m.disposition = 'accepted'
		 LEFT JOIN %s c ON c.owner_id = t.owner_id AND c.seq = m.canonical_seq
		  AND c.mutation_device_id = m.device_id AND c.mutation_id = m.mutation_id
		  AND c.change_kind = 'accepted' AND c.record_kind = t.record_kind AND c.record_id = t.record_id
		 LEFT JOIN %s v ON v.owner_id = t.owner_id AND v.id = t.version_id
		  AND v.record_kind = t.record_kind AND v.record_id = t.record_id
		 WHERE m.owner_id IS NULL OR c.owner_id IS NULL OR v.id IS NULL
		  OR c.version_id IS DISTINCT FROM t.version_id
		  OR c.canonical_version IS DISTINCT FROM v.record_version
		  OR m.canonical_version IS DISTINCT FROM v.record_version
		  OR v.source_device_id IS DISTINCT FROM m.device_id
		  OR v.source_mutation_id IS DISTINCT FROM m.mutation_id
		  OR v.disposition IS DISTINCT FROM 'accepted'
		)`, tombstones, mutations, changes, versions),
		fmt.Sprintf(`SELECT NOT EXISTS (SELECT 1 FROM %s WHERE kind = 'resolve' AND resolution_conflict_ids IS NULL)`, mutations),
		fmt.Sprintf(`SELECT NOT EXISTS (
		 SELECT 1 FROM %s o FULL JOIN (
		  SELECT owner_id, record_id FROM %s WHERE record_kind = 'observation'
		 ) t ON t.owner_id = o.owner_id AND t.record_id = o.id
		 WHERE (o.lifecycle = 'tombstoned') IS DISTINCT FROM (t.owner_id IS NOT NULL)
		)`, observations, tombstones),
	} {
		if err := tx.QueryRow(ctx, query).Scan(&ok); err != nil || !ok {
			return recoveryError(ctx, "verify")
		}
	}
	if !resolveArraysValid(ctx, tx, table, nil) {
		return recoveryError(ctx, "verify")
	}
	if err := tx.Commit(ctx); err != nil {
		return recoveryError(ctx, "commit")
	}
	return nil
}

func recoverySchema(ctx context.Context, tx pgx.Tx) (string, error) {
	var schemas []string
	if err := tx.QueryRow(ctx, "SELECT current_schemas(false)").Scan(&schemas); err != nil || len(schemas) != 1 {
		return "", errors.New("recovery schema")
	}
	schema := schemas[0]
	if schema == "" || schema == "pg_catalog" || strings.HasPrefix(schema, "pg_temp") {
		return "", errors.New("recovery schema")
	}
	return schema, nil
}

func recoveryError(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrRecovery, operation)
}
