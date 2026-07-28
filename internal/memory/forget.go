package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Forget performs the native archive transition in one transaction. The
// observation remains addressable for audit/lifecycle compatibility but is
// removed from FTS, so normal Recall cannot return forgotten content.
func (s *Store) Forget(ctx context.Context, id, project string, scope Scope) (Observation, error) {
	if err := cancelled(ctx); err != nil {
		return Observation{}, err
	}
	if id == "" || project == "" || scope != ScopeProject && scope != ScopePersonal {
		return Observation{}, fmt.Errorf("%w: invalid forget input", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Observation{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	item, found, err := loadOne(tx.QueryRowContext(ctx, observationSelect+` WHERE o.id=? AND o.project_id=? AND o.scope=?`, id, project, scope))
	if err != nil {
		return Observation{}, err
	}
	if !found {
		return Observation{}, fmt.Errorf("%w: observation not found", ErrNotFound)
	}
	if item.State != StateArchived {
		item.State = StateArchived
		item.UpdatedAt = s.now().UTC()
		result, updateErr := tx.ExecContext(ctx, `UPDATE observations SET state=?,updated_at=? WHERE id=? AND project_id=? AND scope=? AND state<>?`, StateArchived, item.UpdatedAt.UnixNano(), id, project, scope, StateArchived)
		if updateErr != nil {
			return Observation{}, writeError(ctx, updateErr)
		}
		if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
			return Observation{}, fmt.Errorf("%w: forget conflict", ErrConflict)
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM observations_fts WHERE id=?`, id); err != nil {
		return Observation{}, writeError(ctx, err)
	}
	if err = tx.Commit(); err != nil {
		if errors.Is(err, sql.ErrTxDone) {
			return Observation{}, fmt.Errorf("%w: forget transaction", ErrCorrupt)
		}
		return Observation{}, writeError(ctx, err)
	}
	return item, nil
}
