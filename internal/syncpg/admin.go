package syncpg

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxAdminPageLimit  = 100
	maxAdminPageOffset = 10000
)

type AdminPage struct {
	Limit  int
	Offset int
}
type AdminHealth struct {
	Database     bool
	HistoryID    uuid.UUID
	HeadSequence int64
}
type AdminDevice struct {
	ID         uuid.UUID
	Name       string
	IssuedAt   time.Time
	RevokedAt  *time.Time
	LastSeenAt *time.Time
}
type AdminAuditEvent struct {
	ID         int64
	OccurredAt time.Time
	DeviceID   *uuid.UUID
	Action     string
	Outcome    string
	Reason     string
}
type AdminOverview struct {
	Health      AdminHealth
	Devices     []AdminDevice
	AuditEvents []AdminAuditEvent
}

func (r *Repository) AdminOverview(ctx context.Context, devices, events AdminPage) (AdminOverview, error) {
	if !validAdminPage(devices) || !validAdminPage(events) {
		return AdminOverview{}, ErrRepository
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminOverview{}, repositoryError(ctx)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); err != nil {
		return AdminOverview{}, repositoryError(ctx)
	}
	schema, err := recoverySchema(ctx, tx)
	if err != nil || !repositoryMigrationsValid(ctx, tx, schema) {
		return AdminOverview{}, repositoryError(ctx)
	}
	state, err := r.ownerState(ctx, tx, schema)
	if err != nil {
		return AdminOverview{}, repositoryError(ctx)
	}
	view := AdminOverview{Health: AdminHealth{Database: true, HistoryID: state.HistoryID, HeadSequence: state.NextSeq - 1}}
	table := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	rows, err := tx.Query(ctx, `SELECT id, display_name, issued_at, revoked_at, last_seen_at
		FROM `+table("devices")+` WHERE owner_id=$1 ORDER BY issued_at DESC, id LIMIT $2 OFFSET $3`, r.ownerID, devices.Limit, devices.Offset)
	if err != nil {
		return AdminOverview{}, repositoryError(ctx)
	}
	for rows.Next() {
		var item AdminDevice
		if err = rows.Scan(&item.ID, &item.Name, &item.IssuedAt, &item.RevokedAt, &item.LastSeenAt); err != nil {
			rows.Close()
			return AdminOverview{}, repositoryError(ctx)
		}
		view.Devices = append(view.Devices, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return AdminOverview{}, repositoryError(ctx)
	}
	rows, err = tx.Query(ctx, `SELECT id, occurred_at, device_id, action, outcome, reason_code
		FROM `+table("audit_events")+` WHERE owner_id=$1 ORDER BY occurred_at DESC, id DESC LIMIT $2 OFFSET $3`, r.ownerID, events.Limit, events.Offset)
	if err != nil {
		return AdminOverview{}, repositoryError(ctx)
	}
	for rows.Next() {
		var item AdminAuditEvent
		var reason *string
		if err = rows.Scan(&item.ID, &item.OccurredAt, &item.DeviceID, &item.Action, &item.Outcome, &reason); err != nil {
			rows.Close()
			return AdminOverview{}, repositoryError(ctx)
		}
		if reason != nil {
			item.Reason = *reason
		}
		view.AuditEvents = append(view.AuditEvents, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return AdminOverview{}, repositoryError(ctx)
	}
	if err = commitRepository(ctx, tx); err != nil {
		return AdminOverview{}, err
	}
	return view, nil
}
func validAdminPage(page AdminPage) bool {
	return page.Limit >= 1 && page.Limit <= maxAdminPageLimit && page.Offset >= 0 && page.Offset <= maxAdminPageOffset
}
