package syncpg

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	auditRetention        = 30 * 24 * time.Hour
	auditEventsPerOwner   = 10_000
	auditCleanupBatchSize = 250
)

var (
	// ErrInvalidDeviceName indicates a display name that cannot be stored safely.
	ErrInvalidDeviceName = errors.New("syncpg invalid device name")
	// ErrUnauthenticated is deliberately identical for all credential failures.
	ErrUnauthenticated = errors.New("syncpg unauthenticated")
	// ErrDeviceNotFound indicates that this owner's device selector is absent.
	ErrDeviceNotFound = errors.New("syncpg device not found")
	// ErrInvalidDeviceID indicates an invalid device selector.
	ErrInvalidDeviceID = errors.New("syncpg invalid device id")
)

// DeviceCredential is the one-time credential issued for an enrolled device.
// Callers must treat Bearer as a secret and avoid persistence or logging.
type DeviceCredential struct {
	ID          uuid.UUID
	DisplayName string
	Prefix      string
	Bearer      string
}

// DeviceIdentity is the non-secret result of credential authentication.
type DeviceIdentity struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	DisplayName string
}

// IssueDevice creates a device and commits its issuance audit before returning
// the newly generated bearer credential.
func (r *Repository) IssueDevice(ctx context.Context, name string) (DeviceCredential, error) {
	if err := ctx.Err(); err != nil {
		return DeviceCredential{}, err
	}
	if !validDeviceName(name) {
		return DeviceCredential{}, ErrInvalidDeviceName
	}
	id, err := uuid.NewRandom()
	if err != nil || id == uuid.Nil {
		return DeviceCredential{}, repositoryError(ctx)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		clearBytes(raw)
		return DeviceCredential{}, repositoryError(ctx)
	}
	bearer := "vgx1." + id.String() + "." + base64.RawURLEncoding.EncodeToString(raw)
	clearBytes(raw)
	hash := sha256.Sum256([]byte(bearer))
	prefix := "vgx1." + id.String()[:8]

	tx, schema, err := r.deviceTransaction(ctx)
	if err != nil {
		return DeviceCredential{}, err
	}
	defer tx.Rollback(context.Background())
	table := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	if _, err = tx.Exec(ctx, "INSERT INTO "+table("devices")+" (id, owner_id, display_name, credential_hash, credential_prefix) VALUES ($1,$2,$3,$4,$5)", id, r.ownerID, name, hash[:], prefix); err != nil {
		return DeviceCredential{}, repositoryError(ctx)
	}
	if err = deviceAudit(ctx, tx, schema, r.ownerID, id, "device.issue", "success", ""); err != nil {
		return DeviceCredential{}, repositoryError(ctx)
	}
	if err = commitRepository(ctx, tx); err != nil {
		return DeviceCredential{}, err
	}
	return DeviceCredential{ID: id, DisplayName: name, Prefix: prefix, Bearer: bearer}, nil
}

// AuthenticateDevice verifies a bearer against this repository's owner and
// returns only non-secret device identity data.
func (r *Repository) AuthenticateDevice(ctx context.Context, bearer string) (DeviceIdentity, error) {
	if err := ctx.Err(); err != nil {
		return DeviceIdentity{}, err
	}
	id, ok := parseDeviceBearer(bearer)
	if !ok {
		return r.authenticationFailure(ctx, uuid.Nil, "malformed_bearer")
	}
	hash := sha256.Sum256([]byte(bearer))
	tx, schema, err := r.deviceTransaction(ctx)
	if err != nil {
		return DeviceIdentity{}, err
	}
	defer tx.Rollback(context.Background())
	table := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	var identity DeviceIdentity
	var stored []byte
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, "SELECT id, owner_id, display_name, credential_hash, revoked_at FROM "+table("devices")+" WHERE owner_id=$1 AND id=$2", r.ownerID, id).Scan(&identity.ID, &identity.OwnerID, &identity.DisplayName, &stored, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.authenticationFailureTx(ctx, tx, schema, uuid.Nil, "unknown_device")
	}
	if err != nil {
		return DeviceIdentity{}, repositoryError(ctx)
	}
	if len(stored) != sha256.Size {
		return DeviceIdentity{}, repositoryError(ctx)
	}
	if subtle.ConstantTimeCompare(stored, hash[:]) != 1 {
		return r.authenticationFailureTx(ctx, tx, schema, identity.ID, "invalid_credential")
	}
	if revokedAt != nil {
		return r.authenticationFailureTx(ctx, tx, schema, identity.ID, "revoked")
	}
	result, err := tx.Exec(ctx, "UPDATE "+table("devices")+" SET last_seen_at=now() WHERE owner_id=$1 AND id=$2 AND revoked_at IS NULL", r.ownerID, identity.ID)
	if err != nil {
		return DeviceIdentity{}, repositoryError(ctx)
	}
	if result.RowsAffected() != 1 {
		return r.authenticationFailureTx(ctx, tx, schema, identity.ID, "revoked")
	}
	if err = commitRepository(ctx, tx); err != nil {
		return DeviceIdentity{}, err
	}
	return identity, nil
}

// RevokeDevice makes a device unusable for subsequent authentication. Repeats
// are successful and do not produce another audit event.
func (r *Repository) RevokeDevice(ctx context.Context, id uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == uuid.Nil {
		return ErrInvalidDeviceID
	}
	tx, schema, err := r.deviceTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	table := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, "SELECT revoked_at FROM "+table("devices")+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", r.ownerID, id).Scan(&revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return repositoryError(ctx)
	}
	if revokedAt != nil {
		return commitRepository(ctx, tx)
	}
	if _, err = tx.Exec(ctx, "UPDATE "+table("devices")+" SET revoked_at=now() WHERE owner_id=$1 AND id=$2", r.ownerID, id); err != nil {
		return repositoryError(ctx)
	}
	if err = deviceAudit(ctx, tx, schema, r.ownerID, id, "device.revoke", "success", ""); err != nil {
		return repositoryError(ctx)
	}
	return commitRepository(ctx, tx)
}

func (r *Repository) authenticationFailure(ctx context.Context, id uuid.UUID, reason string) (DeviceIdentity, error) {
	tx, schema, err := r.deviceTransaction(ctx)
	if err != nil {
		return DeviceIdentity{}, err
	}
	defer tx.Rollback(context.Background())
	return r.authenticationFailureTx(ctx, tx, schema, id, reason)
}

func (r *Repository) authenticationFailureTx(ctx context.Context, tx pgx.Tx, schema string, id uuid.UUID, reason string) (DeviceIdentity, error) {
	if err := deviceAudit(ctx, tx, schema, r.ownerID, id, "device.authenticate", "failure", reason); err != nil {
		return DeviceIdentity{}, repositoryError(ctx)
	}
	if err := commitRepository(ctx, tx); err != nil {
		return DeviceIdentity{}, err
	}
	return DeviceIdentity{}, ErrUnauthenticated
}

func (r *Repository) deviceTransaction(ctx context.Context) (pgx.Tx, string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, "", repositoryError(ctx)
	}
	schema, err := recoverySchema(ctx, tx)
	if err != nil || !repositoryMigrationsValid(ctx, tx, schema) {
		tx.Rollback(context.Background())
		return nil, "", repositoryError(ctx)
	}
	owners, err := r.owners(ctx, tx, schema)
	if err != nil {
		tx.Rollback(context.Background())
		return nil, "", repositoryError(ctx)
	}
	if len(owners) == 0 {
		tx.Rollback(context.Background())
		return nil, "", ErrOwnerNotInitialized
	}
	if len(owners) != 1 || owners[0] != r.ownerID {
		tx.Rollback(context.Background())
		return nil, "", ErrOwnerConflict
	}
	if _, err = r.ownerState(ctx, tx, schema); err != nil {
		tx.Rollback(context.Background())
		return nil, "", repositoryError(ctx)
	}
	return tx, schema, nil
}

func deviceAudit(ctx context.Context, tx pgx.Tx, schema string, owner, device uuid.UUID, action, outcome, reason string) error {
	var deviceID any
	if device != uuid.Nil {
		deviceID = device
	}
	table := pgx.Identifier{schema, "audit_events"}.Sanitize()
	if _, err := tx.Exec(ctx, "INSERT INTO "+table+" (owner_id, device_id, action, outcome, reason_code) VALUES ($1,$2,$3,$4,NULLIF($5,''))", owner, deviceID, action, outcome, reason); err != nil {
		return err
	}
	// Failed bearer authentication is the untrusted, sustained-write path.
	// Device issuance and revocation are operator actions, so avoiding cleanup
	// scans there keeps their transaction cost bounded to the audit insert.
	if action != "device.authenticate" || outcome != "failure" {
		return nil
	}
	return trimAuditEvents(ctx, tx, table, owner)
}

// trimAuditEvents keeps audit evidence useful but bounded. It is deliberately
// synchronous and cancellation-aware: no cleanup goroutine can outlive a
// request or transaction. Each delete is batch-limited.
func trimAuditEvents(ctx context.Context, tx pgx.Tx, table string, owner uuid.UUID) error {
	cutoff := time.Now().Add(-auditRetention)
	if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE id IN (SELECT id FROM "+table+" WHERE owner_id=$1 AND occurred_at<$2 ORDER BY id LIMIT $3)", owner, cutoff, auditCleanupBatchSize); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE id IN (SELECT id FROM "+table+" WHERE owner_id=$1 ORDER BY id DESC OFFSET $2 LIMIT $3)", owner, auditEventsPerOwner, auditCleanupBatchSize)
	return err
}

func validDeviceName(name string) bool {
	if name == "" || len(name) > 128 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}

func parseDeviceBearer(bearer string) (uuid.UUID, bool) {
	if len(bearer) != 85 {
		return uuid.Nil, false
	}
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 || parts[0] != "vgx1" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil || id.String() != parts[1] {
		return uuid.Nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != parts[2] {
		clearBytes(raw)
		return uuid.Nil, false
	}
	clearBytes(raw)
	return id, true
}

func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
