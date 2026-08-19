package syncpg

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func deviceRepository(t *testing.T) (*Repository, *testDB) {
	t.Helper()
	conn := testConn(t)
	if err := Migrate(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	repo, err := NewRepository(conn, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(context.Background()); err != nil {
		t.Fatal(err)
	}
	return repo, &testDB{conn: conn, owner: owner}
}

type testDB struct {
	conn  *pgx.Conn
	owner uuid.UUID
}

func TestDeviceIssuePersistsOnlyHashAndSafeAudit(t *testing.T) {
	repo, db := deviceRepository(t)
	var delivered DeviceCredential
	issued, err := repo.IssueDeviceWithDelivery(context.Background(), "desk", func(credential DeviceCredential) error {
		delivered = credential
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if delivered != issued {
		t.Fatal("committed credential differs from delivered credential")
	}
	assertBearer(t, issued.Bearer, issued.ID)
	if issued.Prefix != "vgx1."+issued.ID.String()[:8] {
		t.Fatalf("prefix = %q", issued.Prefix)
	}

	var hash []byte
	var prefix string
	if err := db.conn.QueryRow(context.Background(), "SELECT credential_hash, credential_prefix FROM devices WHERE owner_id=$1 AND id=$2", db.owner, issued.ID).Scan(&hash, &prefix); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(issued.Bearer))
	if string(hash) != string(wantHash[:]) || len(hash) != sha256.Size {
		t.Fatal("credential hash was not SHA-256(full bearer)")
	}
	if prefix != issued.Prefix {
		t.Fatalf("stored prefix = %q", prefix)
	}
	var action, outcome, reason string
	if err := db.conn.QueryRow(context.Background(), "SELECT action, outcome, COALESCE(reason_code,'') FROM audit_events WHERE device_id=$1", issued.ID).Scan(&action, &outcome, &reason); err != nil {
		t.Fatal(err)
	}
	if action != "device.issue" || outcome != "success" || reason != "" {
		t.Fatalf("issue audit = %q/%q/%q", action, outcome, reason)
	}
	if _, ok := any(issued).(fmt.Stringer); ok {
		t.Fatal("credential result must not implement Stringer")
	}
	assertNoBearer(t, issued.Bearer, issued.Prefix, action, outcome, reason)
}

func TestDeviceIssueDeliveryFailureRollsBackWrites(t *testing.T) {
	repo, db := deviceRepository(t)
	deliveryErr := errors.New("delivery failed")
	credential, err := repo.IssueDeviceWithDelivery(context.Background(), "undelivered", func(delivered DeviceCredential) error {
		if delivered.ID == uuid.Nil || delivered.Bearer == "" {
			t.Fatal("callback received empty credential")
		}
		return deliveryErr
	})
	if !errors.Is(err, deliveryErr) || credential != (DeviceCredential{}) {
		t.Fatalf("delivery result = %#v, %v", credential, err)
	}
	var devices, audits int
	if err := db.conn.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM devices), (SELECT count(*) FROM audit_events)").Scan(&devices, &audits); err != nil {
		t.Fatal(err)
	}
	if devices != 0 || audits != 0 {
		t.Fatalf("failed delivery persisted devices=%d audits=%d", devices, audits)
	}
}

func TestDeviceAuthenticateUpdatesLastSeenAndFailuresAreStaticAudited(t *testing.T) {
	repo, db := deviceRepository(t)
	issued, err := repo.IssueDevice(context.Background(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := repo.AuthenticateDevice(context.Background(), issued.Bearer)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID != issued.ID || identity.OwnerID != db.owner || identity.DisplayName != "laptop" {
		t.Fatalf("identity = %#v", identity)
	}
	var lastSeen time.Time
	if err := db.conn.QueryRow(context.Background(), "SELECT last_seen_at FROM devices WHERE id=$1", issued.ID).Scan(&lastSeen); err != nil || lastSeen.IsZero() {
		t.Fatalf("last_seen_at: %v %v", lastSeen, err)
	}

	wrong := issued.Bearer[:len(issued.Bearer)-1] + "A"
	if strings.HasSuffix(issued.Bearer, "A") {
		wrong = issued.Bearer[:len(issued.Bearer)-1] + "Q"
	}
	unknownID := uuid.New().String()
	unknown := "vgx1." + unknownID + "." + strings.Split(issued.Bearer, ".")[2]
	for name, input := range map[string]struct{ bearer, reason string }{
		"malformed":       {"not-a-bearer", "malformed_bearer"},
		"undersized":      {issued.Bearer[:84], "malformed_bearer"},
		"oversized":       {issued.Bearer + "A", "malformed_bearer"},
		"large malformed": {strings.Repeat("x", 1<<20), "malformed_bearer"},
		"unknown":         {unknown, "unknown_device"},
		"wrong":           {wrong, "invalid_credential"},
	} {
		t.Run(name, func(t *testing.T) {
			_, got := repo.AuthenticateDevice(context.Background(), input.bearer)
			if got != ErrUnauthenticated {
				t.Fatalf("error = %v, want static ErrUnauthenticated", got)
			}
			var action, outcome, gotReason string
			if err := db.conn.QueryRow(context.Background(), "SELECT action, outcome, reason_code FROM audit_events ORDER BY id DESC LIMIT 1").Scan(&action, &outcome, &gotReason); err != nil {
				t.Fatal(err)
			}
			if action != "device.authenticate" || outcome != "failure" || gotReason != input.reason {
				t.Fatalf("failure audit = %q/%q/%q", action, outcome, gotReason)
			}
			assertNoBearer(t, issued.Bearer, got.Error(), action, outcome, gotReason)
			assertNoBearer(t, input.bearer, got.Error(), action, outcome, gotReason)
		})
	}
}

func TestDeviceAuditRetentionIsBoundedAndOwnerScoped(t *testing.T) {
	ctx := context.Background()
	repo, db := deviceRepository(t)
	insert := func(owner uuid.UUID, count int, occurredAt time.Time) {
		t.Helper()
		if _, err := db.conn.Exec(ctx, "INSERT INTO audit_events(owner_id, action, outcome, reason_code, occurred_at) SELECT $1, 'test.audit', 'failure', 'old', $2 FROM generate_series(1,$3)", owner, occurredAt, count); err != nil {
			t.Fatal(err)
		}
	}
	count := func(owner uuid.UUID) int {
		t.Helper()
		var got int
		if err := db.conn.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE owner_id=$1", owner).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if _, err := db.conn.Exec(ctx, "DELETE FROM audit_events WHERE owner_id=$1", db.owner); err != nil {
		t.Fatal(err)
	}

	// Administrative audits do not pay two cleanup scans; sustained failed
	// authentications perform the bounded cleanup instead.
	insert(db.owner, 1, time.Now().Add(-auditRetention-time.Hour))
	if _, err := repo.IssueDevice(ctx, "administrative"); err != nil {
		t.Fatal(err)
	}
	if got := count(db.owner); got != 2 {
		t.Fatalf("administrative audit cleanup changed count to %d, want 2", got)
	}

	other := uuid.New()
	if _, err := db.conn.Exec(ctx, "INSERT INTO owners(id) VALUES ($1)", other); err != nil {
		t.Fatal(err)
	}
	insert(other, 1, time.Now().Add(-auditRetention-time.Hour))
	tx, err := db.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := trimAuditEvents(ctx, tx, "audit_events", db.owner); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := count(db.owner); got != 1 { // retained issue
		t.Fatalf("expired owner audit cleanup count = %d, want 1", got)
	}
	if got := count(other); got != 1 {
		t.Fatalf("owner isolation count = %d, want 1", got)
	}
	if _, err := db.conn.Exec(ctx, "DELETE FROM audit_events WHERE owner_id=$1", other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(ctx, "DELETE FROM owners WHERE id=$1", other); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AuthenticateDevice(ctx, "not-a-bearer"); err != ErrUnauthenticated {
		t.Fatalf("authenticate = %v", err)
	}
	if got := count(db.owner); got != 2 { // retained issue + new failure
		t.Fatalf("expired owner audit cleanup count = %d, want 2", got)
	}

	if _, err := db.conn.Exec(ctx, "DELETE FROM audit_events WHERE owner_id=$1", db.owner); err != nil {
		t.Fatal(err)
	}
	insert(db.owner, auditEventsPerOwner+auditCleanupBatchSize, time.Now())
	if _, err := repo.AuthenticateDevice(ctx, "not-a-bearer"); err != ErrUnauthenticated {
		t.Fatalf("first cap authentication = %v", err)
	}
	if got := count(db.owner); got != auditEventsPerOwner+1 {
		t.Fatalf("first bounded cleanup count = %d, want %d", got, auditEventsPerOwner+1)
	}
	var newest int
	if err := db.conn.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE owner_id=$1 AND reason_code='malformed_bearer'", db.owner).Scan(&newest); err != nil || newest != 1 {
		t.Fatalf("newest audit preserved = %d, %v", newest, err)
	}
	if _, err := repo.AuthenticateDevice(ctx, "not-a-bearer"); err != ErrUnauthenticated {
		t.Fatalf("second cap authentication = %v", err)
	}
	if got := count(db.owner); got != auditEventsPerOwner {
		t.Fatalf("cleanup did not converge to cap: %d", got)
	}

	tx, err = db.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := trimAuditEvents(canceled, tx, "audit_events", db.owner); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cleanup = %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if got := count(db.owner); got != auditEventsPerOwner {
		t.Fatalf("canceled cleanup changed rows: %d", got)
	}
}

func TestDeviceAuthenticationAndRevokeSerializeAtConditionalUpdate(t *testing.T) {
	ctx := context.Background()
	conn := testConn(t)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("VGXNESS_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	repo, err := NewRepository(pool, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	issued, err := repo.IssueDevice(ctx, "racy")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	authDone := make(chan error, 1)
	revokeDone := make(chan error, 1)
	go func() { <-start; _, err := repo.AuthenticateDevice(ctx, issued.Bearer); authDone <- err }()
	go func() { <-start; revokeDone <- repo.RevokeDevice(ctx, issued.ID) }()
	close(start)
	authErr, revokeErr := <-authDone, <-revokeDone
	if revokeErr != nil || (authErr != nil && authErr != ErrUnauthenticated) {
		t.Fatalf("concurrent results auth=%v revoke=%v", authErr, revokeErr)
	}
	if _, err := repo.AuthenticateDevice(ctx, issued.Bearer); err != ErrUnauthenticated {
		t.Fatalf("authentication after committed revoke = %v", err)
	}
}

func TestDeviceRevokeIsScopedIdempotentAndDenies(t *testing.T) {
	repo, db := deviceRepository(t)
	first, err := repo.IssueDevice(context.Background(), "phone")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.IssueDevice(context.Background(), "tablet")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeDevice(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	authErr := error(nil)
	if _, authErr = repo.AuthenticateDevice(context.Background(), first.Bearer); authErr != ErrUnauthenticated {
		t.Fatalf("revoked auth error = %v", authErr)
	}
	var action, outcome, reason string
	if err := db.conn.QueryRow(context.Background(), "SELECT action, outcome, reason_code FROM audit_events ORDER BY id DESC LIMIT 1").Scan(&action, &outcome, &reason); err != nil {
		t.Fatal(err)
	}
	if action != "device.authenticate" || outcome != "failure" || reason != "revoked" {
		t.Fatalf("revoked audit = %q/%q/%q", action, outcome, reason)
	}
	assertNoBearer(t, first.Bearer, authErr.Error(), action, outcome, reason)
	if _, err := repo.AuthenticateDevice(context.Background(), second.Bearer); err != nil {
		t.Fatalf("second device authentication: %v", err)
	}
	var auditsBefore int
	if err := db.conn.QueryRow(context.Background(), "SELECT count(*) FROM audit_events WHERE device_id=$1 AND action='device.revoke'", first.ID).Scan(&auditsBefore); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeDevice(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	var auditsAfter int
	if err := db.conn.QueryRow(context.Background(), "SELECT count(*) FROM audit_events WHERE device_id=$1 AND action='device.revoke'", first.ID).Scan(&auditsAfter); err != nil {
		t.Fatal(err)
	}
	if auditsBefore != 1 || auditsAfter != auditsBefore {
		t.Fatalf("revoke audits = %d then %d", auditsBefore, auditsAfter)
	}
	if err := repo.RevokeDevice(context.Background(), uuid.New()); err != ErrDeviceNotFound {
		t.Fatalf("unknown revoke = %v", err)
	}
	if err := repo.RevokeDevice(context.Background(), uuid.Nil); err != ErrInvalidDeviceID {
		t.Fatalf("nil revoke = %v", err)
	}
}

func TestDeviceOperationsEnforceSoleOwnerBeforeWrites(t *testing.T) {
	ctx := context.Background()
	for name, setup := range map[string]func(t *testing.T, conn *pgx.Conn) (*Repository, string, error){
		"no owner": func(t *testing.T, conn *pgx.Conn) (*Repository, string, error) {
			repo, err := NewRepository(conn, uuid.New())
			return repo, "vgx1." + uuid.New().String() + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", err
		},
		"wrong owner": func(t *testing.T, conn *pgx.Conn) (*Repository, string, error) {
			ownerRepo, err := NewRepository(conn, uuid.New())
			if err != nil {
				return nil, "", err
			}
			if err := ownerRepo.EnsureOwner(ctx); err != nil {
				return nil, "", err
			}
			issued, err := ownerRepo.IssueDevice(ctx, "owner-device")
			if err != nil {
				return nil, "", err
			}
			repo, err := NewRepository(conn, uuid.New())
			return repo, issued.Bearer, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			conn := testConn(t)
			if err := Migrate(ctx, conn); err != nil {
				t.Fatal(err)
			}
			repo, bearer, err := setup(t, conn)
			if err != nil {
				t.Fatal(err)
			}
			want := ErrOwnerNotInitialized
			if name == "wrong owner" {
				want = ErrOwnerConflict
			}
			if _, err := repo.IssueDevice(ctx, "denied"); err != want {
				t.Fatalf("issue error = %v", err)
			}
			if _, err := repo.AuthenticateDevice(ctx, bearer); err != want {
				t.Fatalf("authenticate error = %v", err)
			}
			if err := repo.RevokeDevice(ctx, uuid.New()); err != want {
				t.Fatalf("revoke error = %v", err)
			}
			var devices, audits int
			if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM devices), (SELECT count(*) FROM audit_events)").Scan(&devices, &audits); err != nil {
				t.Fatal(err)
			}
			if name == "no owner" && (devices != 0 || audits != 0) {
				t.Fatalf("unauthorized operation wrote devices=%d audits=%d", devices, audits)
			}
			if name == "wrong owner" && (devices != 1 || audits != 1) {
				t.Fatalf("wrong owner wrote devices=%d audits=%d", devices, audits)
			}
		})
	}
}

func TestDeviceRejectsUnsafeNamesAndCancellationHasNoWrites(t *testing.T) {
	repo, db := deviceRepository(t)
	for _, name := range []string{"", " desk", "desk ", "line\nfeed", "bidi\u202Ename", string([]byte{0xff}), strings.Repeat("a", 129)} {
		if _, err := repo.IssueDevice(context.Background(), name); err != ErrInvalidDeviceName {
			t.Fatalf("IssueDevice(%q) = %v", name, err)
		}
	}
	issued, err := repo.IssueDevice(context.Background(), "valid")
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.conn.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM devices) + (SELECT count(*) FROM audit_events)").Scan(&before); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.IssueDevice(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel issue = %v", err)
	}
	if _, err := repo.AuthenticateDevice(ctx, issued.Bearer); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel auth = %v", err)
	}
	if err := repo.RevokeDevice(ctx, issued.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel revoke = %v", err)
	}
	var after int
	if err := db.conn.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM devices) + (SELECT count(*) FROM audit_events)").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("cancellation wrote rows: %d -> %d", before, after)
	}
}

func TestDeviceRejectsCorruptOwnerStateWithoutWrites(t *testing.T) {
	repo, db := deviceRepository(t)
	issued, err := repo.IssueDevice(context.Background(), "stateful")
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.conn.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM devices) + (SELECT count(*) FROM audit_events)").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(context.Background(), "UPDATE owner_sync_state SET next_seq=9 WHERE owner_id=$1", db.owner); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"issue": func() error { _, err := repo.IssueDevice(context.Background(), "blocked"); return err },
		"auth":  func() error { _, err := repo.AuthenticateDevice(context.Background(), issued.Bearer); return err },
		"revoke": func() error {
			return repo.RevokeDevice(context.Background(), issued.ID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err != ErrRepository {
				t.Fatalf("error = %v", err)
			}
		})
	}
	var after int
	if err := db.conn.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM devices) + (SELECT count(*) FROM audit_events)").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("corrupt state wrote rows: %d -> %d", before, after)
	}
}

func assertBearer(t *testing.T, bearer string, id uuid.UUID) {
	t.Helper()
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 || parts[0] != "vgx1" || parts[1] != id.String() || parts[1] != strings.ToLower(parts[1]) {
		t.Fatalf("invalid bearer shape")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(raw) != 32 || strings.Contains(parts[2], "=") {
		t.Fatalf("invalid bearer randomness encoding")
	}
}

func assertNoBearer(t *testing.T, bearer string, values ...string) {
	t.Helper()
	if bearer == "" {
		t.Fatal("empty bearer would make leak assertion vacuous")
	}
	for _, value := range values {
		if strings.Contains(value, bearer) {
			t.Fatal("bearer leaked into persisted or surfaced value")
		}
	}
}
