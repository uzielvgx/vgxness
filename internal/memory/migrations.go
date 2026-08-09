package memory

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed migrations/001_memory.sql
var schemaV1 string

//go:embed migrations/002_observation_title.sql
var schemaV2 string

//go:embed migrations/003_global_projects.sql
var schemaV3 string

//go:embed migrations/004_project_roots.sql
var schemaV4 string

//go:embed migrations/005_sdd.sql
var schemaV5 string

//go:embed migrations/006_sync.sql
var schemaV6 string

//go:embed migrations/007_local_write_versions.sql
var schemaV7 string

//go:embed migrations/008_sync_inbox_cursor_conflicts_bootstrap.sql
var schemaV8 string

//go:embed migrations/009_sync_push_results.sql
var schemaV9 string

//go:embed migrations/010_sync_outbox_claims.sql
var schemaV10 string

//go:embed migrations/011_sdd_ultra_plan.sql
var schemaV11 string

type migration struct {
	version                     int
	sql                         string
	requiresForeignKeysDisabled bool
}

var migrations = []migration{
	{version: 1, sql: schemaV1},
	{version: 2, sql: schemaV2},
	{version: 3, sql: schemaV3},
	{version: 4, sql: schemaV4},
	{version: 5, sql: schemaV5},
	{version: 6, sql: schemaV6},
	{version: 7, sql: schemaV7},
	{version: 8, sql: schemaV8},
	{version: 9, sql: schemaV9},
	{version: 10, sql: schemaV10},
	{version: 11, sql: schemaV11, requiresForeignKeysDisabled: true},
}

func applyMigrations(ctx context.Context, db *sql.DB, steps []migration) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return migrationError(ctx, "acquire connection", err)
	}
	defer conn.Close()
	if err := beginMigration(ctx, conn); err != nil {
		return err
	}
	inTransaction := true
	foreignKeysDisabled := false
	defer func() {
		if inTransaction {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
		if foreignKeysDisabled {
			_, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
		}
	}()
	head, err := migrationHead(ctx, conn)
	if err != nil {
		return migrationError(ctx, "read migration ledger", err)
	}
	latest := 0
	for _, step := range steps {
		if step.version > latest {
			latest = step.version
		}
	}
	if head > latest {
		return fmt.Errorf("%w: database schema version %d is newer than supported version %d", ErrMigration, head, latest)
	}
	if requiresForeignKeysDisabled(steps, head) {
		if _, err = conn.ExecContext(ctx, `ROLLBACK`); err != nil {
			return migrationError(ctx, "release ownership for foreign-key rebuild", err)
		}
		inTransaction = false
		if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return migrationError(ctx, "disable foreign keys for rebuild", err)
		}
		foreignKeysDisabled = true
		if err = beginMigration(ctx, conn); err != nil {
			return err
		}
		inTransaction = true
		head, err = migrationHead(ctx, conn)
		if err != nil {
			return migrationError(ctx, "read migration ledger", err)
		}
		if head > latest {
			return fmt.Errorf("%w: database schema version %d is newer than supported version %d", ErrMigration, head, latest)
		}
	}
	for _, step := range steps {
		if step.version <= head {
			continue
		}
		_, err = conn.ExecContext(ctx, step.sql)
		if err == nil {
			_, err = conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version=%d`, step.version))
		}
		if err != nil {
			return migrationError(ctx, fmt.Sprintf("failed version %d", step.version), err)
		}
	}
	if foreignKeysDisabled {
		if err = foreignKeyCheck(ctx, conn); err != nil {
			return err
		}
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return migrationError(ctx, "commit", err)
	}
	inTransaction = false
	if foreignKeysDisabled {
		if err = enableForeignKeys(ctx, conn); err != nil {
			return err
		}
		foreignKeysDisabled = false
		if err = foreignKeyCheck(ctx, conn); err != nil {
			return err
		}
	}
	return nil
}

func beginMigration(ctx context.Context, conn *sql.Conn) error {
	for attempt := 0; ; attempt++ {
		_, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`)
		if err == nil {
			return nil
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return migrationError(ctx, "acquire ownership", err)
		}
	}
}

func migrationHead(ctx context.Context, conn *sql.Conn) (int, error) {
	var head int
	err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&head)
	return head, err
}

func requiresForeignKeysDisabled(steps []migration, head int) bool {
	for _, step := range steps {
		if step.version > head && step.requiresForeignKeysDisabled {
			return true
		}
	}
	return false
}

func enableForeignKeys(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return migrationError(ctx, "restore foreign keys", err)
	}
	var enabled int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		return migrationError(ctx, "verify foreign keys enabled", err)
	}
	if enabled != 1 {
		return fmt.Errorf("%w: verify foreign keys enabled", ErrMigration)
	}
	return nil
}

func foreignKeyCheck(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return migrationError(ctx, "verify foreign keys", err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("%w: verify foreign keys", ErrMigration)
	}
	if err = rows.Err(); err != nil {
		return migrationError(ctx, "verify foreign keys", err)
	}
	return nil
}

func waitForSQLite(ctx context.Context, attempt int, err error) error {
	locked := strings.Contains(strings.ToLower(err.Error()), "locked") || strings.Contains(strings.ToLower(err.Error()), "busy")
	if !locked || attempt >= 19 {
		return err
	}
	delay := time.Duration(attempt+1) * 10 * time.Millisecond
	if delay > 100*time.Millisecond {
		delay = 100 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func migrationError(ctx context.Context, operation string, _ error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrMigration, operation)
}
