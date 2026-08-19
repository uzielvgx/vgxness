package syncpg

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type adminRejectDB struct{ begins int }

func (db *adminRejectDB) Begin(context.Context) (pgx.Tx, error) {
	db.begins++
	return nil, errors.New("unexpected begin")
}
func TestAdminOverviewRejectsUnboundedPaginationBeforeDatabase(t *testing.T) {
	db := &adminRejectDB{}
	repository, err := NewRepository(db, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, pages := range [][2]AdminPage{{{Limit: 0}, {Limit: 1}}, {{Limit: 101}, {Limit: 1}}, {{Limit: 1, Offset: -1}, {Limit: 1}}, {{Limit: 1, Offset: 10001}, {Limit: 1}}} {
		if _, err := repository.AdminOverview(context.Background(), pages[0], pages[1]); !errors.Is(err, ErrRepository) {
			t.Fatalf("AdminOverview(%+v, %+v) error = %v", pages[0], pages[1], err)
		}
	}
	if db.begins != 0 {
		t.Fatal("invalid pagination reached the database")
	}
}
func TestAdminOverviewIsOwnerScopedAndContainsNoCredentials(t *testing.T) {
	ctx, conn := context.Background(), testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	owner, other := uuid.New(), uuid.New()
	repository, err := NewRepository(conn, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	credential, err := repository.IssueDevice(ctx, "owner-device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO owners(id) VALUES ($1);
		INSERT INTO owner_sync_state(owner_id,history_id,next_seq) VALUES ($1,$2,1);
		INSERT INTO devices(id,owner_id,display_name,credential_hash,credential_prefix) VALUES ($3,$1,'other-device',decode(repeat('00',32),'hex'),'secret-prefix');
		INSERT INTO audit_events(owner_id,device_id,action,outcome,reason_code) VALUES ($1,$3,'other.action','ok','private')`, other, uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	view, err := repository.AdminOverview(ctx, AdminPage{Limit: 10}, AdminPage{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Health.Database || len(view.Devices) != 1 || view.Devices[0].ID != credential.ID || view.Devices[0].Name != "owner-device" {
		t.Fatalf("unexpected owner view: %+v", view)
	}
	for _, event := range view.AuditEvents {
		if event.Action == "other.action" || event.Reason == "private" {
			t.Fatal("cross-owner audit event leaked")
		}
	}
	if len(view.AuditEvents) == 0 {
		t.Fatal("owner audit event missing")
	}
}
