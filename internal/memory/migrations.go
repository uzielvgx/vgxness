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

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{version: 1, sql: schemaV1},
	{version: 2, sql: schemaV2},
	{version: 3, sql: schemaV3},
	{version: 4, sql: schemaV4},
	{version: 5, sql: schemaV5},
}

func applyMigrations(ctx context.Context, db *sql.DB, steps []migration) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return migrationError(ctx, "acquire connection", err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		_, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`)
		if err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return migrationError(ctx, "acquire ownership", err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	var head int
	if err = conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&head); err != nil {
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
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return migrationError(ctx, "commit", err)
	}
	committed = true
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
