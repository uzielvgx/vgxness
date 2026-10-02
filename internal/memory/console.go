package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SessionHandoff is one completed provider session and the summary it left
// for the next session on the same project.
type SessionHandoff struct {
	Handle, Provider, ObservationID, Summary string
	StartedAt, CompletedAt                   time.Time
}

const maxSessionHandoffs = 50

// CountActive returns how many active project-scope memories a project has,
// excluding entries removed by sync tombstones. It never writes.
func (s *Store) CountActive(ctx context.Context, project string) (int, error) {
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	if !validText(project, 256, false) {
		return 0, fmt.Errorf("%w: project", ErrInvalid)
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations o WHERE o.project_id=? AND o.scope=? AND o.state=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id)`, project, ScopeProject, StateActive).Scan(&count)
	if err != nil {
		return 0, writeError(ctx, err)
	}
	return count, nil
}

// SessionHandoffs lists the most recently completed provider sessions of a
// project that left a summary, newest first. It never writes.
func (s *Store) SessionHandoffs(ctx context.Context, project string, limit int) ([]SessionHandoff, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if !validText(project, 256, false) || limit < 0 || limit > maxSessionHandoffs {
		return nil, fmt.Errorf("%w: session handoffs", ErrInvalid)
	}
	if limit == 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.handle,p.provider,o.id,o.content,p.created_at,p.completed_at FROM local_provider_sessions p JOIN observations o ON o.id=p.final_observation_id WHERE p.project_id=? AND p.state='completed' AND p.completed_at IS NOT NULL ORDER BY p.completed_at DESC, p.handle ASC LIMIT ?`, project, limit)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	defer rows.Close()
	var found []SessionHandoff
	for rows.Next() {
		var item SessionHandoff
		var started int64
		var completed sql.NullInt64
		if err := rows.Scan(&item.Handle, &item.Provider, &item.ObservationID, &item.Summary, &started, &completed); err != nil || !completed.Valid {
			return nil, fmt.Errorf("%w: read session handoff", ErrCorrupt)
		}
		item.StartedAt, item.CompletedAt = time.Unix(0, started).UTC(), time.Unix(0, completed.Int64).UTC()
		found = append(found, item)
	}
	if err := rows.Err(); err != nil {
		return nil, writeError(ctx, err)
	}
	return found, nil
}

// UserVersion reads the schema version recorded in the database without
// checking it, so a pending migration can be told apart from damage.
func (s *Store) UserVersion(ctx context.Context) (int, error) {
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, writeError(ctx, err)
	}
	return version, nil
}
