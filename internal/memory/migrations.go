package memory

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed migrations/001_memory.sql
var schemaV1 string

type migration struct {
	version int
	sql     string
}

var migrations = []migration{{version: 1, sql: schemaV1}}

func applyMigrations(ctx context.Context, db *sql.DB, steps []migration) error {
	var head int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&head); err != nil {
		return fmt.Errorf("%w: read migration ledger", ErrMigration)
	}
	if head > len(steps) {
		return fmt.Errorf("%w: database schema version %d is newer than supported version %d", ErrMigration, head, len(steps))
	}
	for _, step := range steps {
		if step.version <= head {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err == nil {
			_, err = tx.ExecContext(ctx, step.sql)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version=%d`, step.version))
		}
		if err == nil {
			err = tx.Commit()
		} else if tx != nil {
			_ = tx.Rollback()
		}
		if err != nil {
			return fmt.Errorf("%w: failed version %d", ErrMigration, step.version)
		}
	}
	return nil
}
