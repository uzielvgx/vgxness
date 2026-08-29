package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/vgxness/vgxness/internal/config"
	_ "modernc.org/sqlite"
)

type Store struct {
	db         *sql.DB
	now        func() time.Time
	readOnly   bool
	checkpoint func() (busy, log, checkpointed int, err error)
	close      func() error
	syncMu     sync.Mutex
	closed     bool
	syncInbox  syncInboxCache
}

type syncInboxCache struct {
	known       bool
	dataVersion int64
	historyID   string
	position    int64
}

func Open(ctx context.Context, path string, now func() time.Time) (*Store, error) {
	return open(ctx, path, now, nil)
}

func open(ctx context.Context, path string, now func() time.Time, afterConfigure func() error) (*Store, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if err := rejectSymlink(path); err != nil {
		return nil, err
	}
	path, err := canonicalizeStoragePath(path)
	if err != nil {
		return nil, err
	}
	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil || !parentInfo.IsDir() {
		return nil, fmt.Errorf("open memory store: %w", ErrCorrupt)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", ErrCorrupt)
	}
	db.SetMaxOpenConns(1)
	cleanup := func() {
		_ = db.Close()
	}
	if err := validateDatabaseParent(path, parentInfo); err != nil {
		cleanup()
		return nil, err
	}
	for _, pragma := range []string{`PRAGMA busy_timeout=25`, `PRAGMA foreign_keys=ON`, `PRAGMA journal_mode=WAL`} {
		for attempt := 0; ; attempt++ {
			_, err = db.ExecContext(ctx, pragma)
			if err == nil {
				break
			}
			if err = waitForSQLite(ctx, attempt, err); err != nil {
				cleanup()
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, fmt.Errorf("configure memory store: %w", ErrCorrupt)
			}
		}
	}
	if err := secureSQLiteArtifacts(path); err != nil {
		cleanup()
		return nil, err
	}
	if err := validateDatabaseParent(path, parentInfo); err != nil {
		cleanup()
		return nil, err
	}
	if afterConfigure != nil {
		if err := afterConfigure(); err != nil {
			cleanup()
			return nil, err
		}
	}
	if err := applyMigrations(ctx, db, migrations); err != nil {
		cleanup()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		cleanup()
		return nil, fmt.Errorf("configure memory store: %w", ErrCorrupt)
	}
	store := &Store{db: db, now: now}
	if store.now == nil {
		store.now = time.Now
	}
	if _, err := store.Health(ctx); err != nil {
		cleanup()
		return nil, err
	}
	return store, nil
}

func OpenRead(ctx context.Context, path string) (*Store, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if err := rejectSymlink(path); err != nil {
		return nil, err
	}
	path, err := canonicalizeStoragePath(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: memory storage is absent", ErrCorrupt)
	} else if err != nil {
		return nil, fmt.Errorf("%w: memory storage is unavailable", ErrCorrupt)
	}
	dsn := sqliteReadURI(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: memory storage is unavailable", ErrCorrupt)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, now: time.Now, readOnly: true}
	if _, err := store.Health(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}
func HealthFile(ctx context.Context, path string) (int, error) {
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	if err := rejectSymlink(path); err != nil {
		return 0, err
	}
	path, err := canonicalizeStoragePath(path)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("open memory store: %w", ErrCorrupt)
	}
	dsn := sqliteReadURI(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return (&Store{db: db}).Health(ctx)
}

func sqliteReadURI(path string) string {
	uriPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	return (&url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}).String()
}

func rejectSymlink(path string) error {
	for candidate := filepath.Clean(path); ; candidate = filepath.Dir(candidate) {
		if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSymlink != 0 && !isSystemStorageSymlink(candidate) {
			return fmt.Errorf("open memory store: %w", ErrCorrupt)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("open memory store: %w", ErrCorrupt)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
	}
	return nil
}

// SQLite accepts only path strings. Config.Prepare creates a private parent;
// revalidation detects replacement. A same-user attacker can still race SQLite
// between these checks, which is outside this path-based API's boundary.
func canonicalizeStoragePath(path string) (string, error) {
	clean := filepath.Clean(path)
	ancestor := clean
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("open memory store: %w", ErrCorrupt)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	canonical, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("open memory store: %w", ErrCorrupt)
	}
	relative, err := filepath.Rel(ancestor, clean)
	if err != nil {
		return "", fmt.Errorf("open memory store: %w", ErrCorrupt)
	}
	return filepath.Join(canonical, relative), nil
}

func isSystemStorageSymlink(path string) bool {
	if runtime.GOOS != "darwin" || path != "/var" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && resolved == "/private/var"
}

func validateDatabaseParent(path string, expected os.FileInfo) error {
	if err := rejectSymlink(path); err != nil {
		return err
	}
	current, err := os.Stat(filepath.Dir(path))
	if err != nil || !os.SameFile(expected, current) {
		return fmt.Errorf("open memory store: %w", ErrCorrupt)
	}
	return nil
}

func secureSQLiteArtifacts(path string) error {
	for _, artifact := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(artifact)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("open memory store: %w", ErrCorrupt)
		}
		if err := os.Chmod(artifact, 0o600); err != nil {
			return fmt.Errorf("secure memory store: %w", err)
		}
	}
	return nil
}
func (s *Store) Close() error {
	if s == nil || s.db == nil && s.close == nil {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.syncInbox = syncInboxCache{}
	var checkpointErr error
	if !s.readOnly {
		// Keep the main database complete by checkpointing committed WAL state
		// whenever a writable application operation releases its store.
		if s.checkpoint != nil {
			busy, _, _, err := s.checkpoint()
			checkpointErr = checkpointResult(busy, err)
		} else {
			var busy, log, checkpointed int
			err := s.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &log, &checkpointed)
			checkpointErr = checkpointResult(busy, err)
		}
	}
	if s.close != nil {
		return errors.Join(checkpointErr, s.close())
	}
	return errors.Join(checkpointErr, s.db.Close())
}

func checkpointResult(_ int, err error) error {
	if err != nil {
		return fmt.Errorf("checkpoint memory store: %w", err)
	}
	return nil
}
func (s *Store) Health(ctx context.Context) (int, error) {
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return 0, healthError(ctx, "integrity check unavailable")
	}
	integrityRows, integrityOK := 0, true
	for rows.Next() {
		var result string
		integrityRows++
		if rows.Scan(&result) != nil || result != "ok" {
			integrityOK = false
		}
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	if rowsErr != nil || integrityRows != 1 || !integrityOK {
		return 0, fmt.Errorf("%w: integrity check failed", ErrCorrupt)
	}
	rows, err = s.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return 0, healthError(ctx, "foreign-key check unavailable")
	}
	foreignKeyViolation := rows.Next()
	rowsErr = rows.Err()
	_ = rows.Close()
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	if rowsErr != nil || foreignKeyViolation {
		return 0, fmt.Errorf("%w: foreign-key check failed", ErrCorrupt)
	}
	var version, probe int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, healthError(ctx, "migration ledger unavailable")
	}
	if version > migrations[len(migrations)-1].version {
		return 0, fmt.Errorf("%w: database schema version %d is newer than supported version %d", ErrMigration, version, migrations[len(migrations)-1].version)
	}
	if version != migrations[len(migrations)-1].version {
		return 0, fmt.Errorf("%w: unsupported database schema version %d", ErrCorrupt, version)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN ('projects','sessions','observations','observation_refs','legacy_imports','project_roots','portable_project_identities','sync_portable_identities','sync_portable_identity_adoptions','sdd_changes','sdd_artifacts','sdd_revisions','sdd_revision_links','sdd_projections','sync_profiles','sync_outbox','sync_inbox','sync_cursor','sync_tombstones','sync_conflicts','sync_bootstrap','sync_push_results','sync_outbox_claims','sync_project_cursor','sync_project_inbox','sync_project_repairs','sync_project_transitions','sync_project_transition_records','sync_project_backup_intents','local_provider_sessions','local_provider_session_drafts')`).Scan(&probe); err != nil || probe != 31 {
		if contextErr := cancelled(ctx); contextErr != nil {
			return 0, contextErr
		}
		return 0, fmt.Errorf("%w: required schema unavailable", ErrCorrupt)
	}
	if !s.portableProjectIdentitySchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: portable project identity schema unavailable", ErrCorrupt)
	}
	if !s.syncSchemaHealthy(ctx) {
		if err := cancelled(ctx); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("%w: sync schema unavailable", ErrCorrupt)
	}
	if !s.syncPortableIdentitySchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: sync portable identity schema unavailable", ErrCorrupt)
	}
	if !s.syncProjectCursorSchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: sync project cursor schema unavailable", ErrCorrupt)
	}
	if !s.syncProjectRepairSchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: sync project repair schema unavailable", ErrCorrupt)
	}
	if !s.syncProjectTransitionSchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: sync project transition schema unavailable", ErrCorrupt)
	}
	if !s.syncProjectBackupIntentSchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: sync project backup intent schema unavailable", ErrCorrupt)
	}
	if !s.syncPortableIdentityAdoptionSchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: sync portable identity adoption schema unavailable", ErrCorrupt)
	}
	if !s.providerSessionSchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: provider session schema unavailable", ErrCorrupt)
	}
	if !s.providerSessionDraftSchemaHealthy(ctx) {
		return 0, fmt.Errorf("%w: provider session draft schema unavailable", ErrCorrupt)
	}
	if !s.sddSchemaHealthy(ctx) {
		if err := cancelled(ctx); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("%w: SDD schema unavailable", ErrCorrupt)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM observations_fts WHERE observations_fts MATCH 'healthchecknomatch'`).Scan(&probe); err != nil {
		return 0, healthError(ctx, "FTS5 unavailable")
	}
	return version, nil
}

func (s *Store) providerSessionDraftSchemaHealthy(ctx context.Context) bool {
	normalize := func(value string) string { return strings.ReplaceAll(normalizeSchemaSQL(value), "if not exists ", "") }
	parts := strings.Split(schemaV21, ";")
	table, ok := s.schemaSQL(ctx, "table", "local_provider_session_drafts")
	if !ok || normalize(table) != normalize(parts[0]) || !s.schemaColumns(ctx, "local_provider_session_drafts", "handle", "project_id", "summary", "updated_at") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "local_provider_session_drafts_project_updated_idx")
	return ok && normalize(index) == normalize(parts[1]) && s.schemaIndexColumns(ctx, "local_provider_session_drafts_project_updated_idx", "project_id", "updated_at", "handle")
}

func (s *Store) providerSessionSchemaHealthy(ctx context.Context) bool {
	normalize := func(value string) string { return strings.ReplaceAll(normalizeSchemaSQL(value), "if not exists ", "") }
	parts := strings.Split(schemaV20, ";")
	table, ok := s.schemaSQL(ctx, "table", "local_provider_sessions")
	if !ok || normalize(table) != normalize(parts[0]) || !s.schemaColumns(ctx, "local_provider_sessions", "handle", "project_id", "provider", "external_id_hash", "state", "checkpointed", "final_observation_id", "created_at", "updated_at", "completed_at") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "local_provider_sessions_project_state_updated_idx")
	return ok && normalize(index) == normalize(parts[1]) && s.schemaIndexColumns(ctx, "local_provider_sessions_project_state_updated_idx", "project_id", "state", "updated_at", "handle")
}

func (s *Store) syncProjectBackupIntentSchemaHealthy(ctx context.Context) bool {
	normalize := func(value string) string { return strings.ReplaceAll(normalizeSchemaSQL(value), "if not exists ", "") }
	parts := strings.Split(schemaV19, ";")
	table, ok := s.schemaSQL(ctx, "table", "sync_project_backup_intents")
	if !ok || normalize(table) != normalize(parts[0]) || !s.schemaColumns(ctx, "sync_project_backup_intents", "portable_project_id", "local_project_id", "mode", "intent_id", "backup_path", "backup_sha256", "created_at") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "sync_project_backup_intents_local_idx")
	return ok && normalize(index) == normalize(parts[1]) && s.schemaIndexColumns(ctx, "sync_project_backup_intents_local_idx", "local_project_id", "portable_project_id")
}

func (s *Store) syncProjectTransitionSchemaHealthy(ctx context.Context) bool {
	normalize := func(value string) string { return strings.ReplaceAll(normalizeSchemaSQL(value), "if not exists ", "") }
	parts := strings.Split(schemaV18, ";")
	transition, ok := s.schemaSQL(ctx, "table", "sync_project_transitions")
	if !ok || normalize(transition) != normalize(parts[0]) || !s.schemaColumns(ctx, "sync_project_transitions", "portable_project_id", "local_project_id", "mode", "status", "created_at", "completed_at") {
		return false
	}
	records, ok := s.schemaSQL(ctx, "table", "sync_project_transition_records")
	if !ok || normalize(records) != normalize(parts[1]) || !s.schemaColumns(ctx, "sync_project_transition_records", "portable_project_id", "record_kind", "local_id", "payload_hash", "seen_remote") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "sync_project_transitions_status_idx")
	return ok && normalize(index) == normalize(parts[2]) && s.schemaIndexColumns(ctx, "sync_project_transitions_status_idx", "status", "portable_project_id")
}

func (s *Store) syncProjectRepairSchemaHealthy(ctx context.Context) bool {
	normalizeV17 := func(value string) string { return strings.ReplaceAll(normalizeSchemaSQL(value), "if not exists ", "") }
	table, ok := s.schemaSQL(ctx, "table", "sync_project_repairs")
	if !ok || normalizeV17(table) != normalizeV17(strings.Split(schemaV17, ";")[0]) || !s.schemaColumns(ctx, "sync_project_repairs", "portable_project_id", "local_project_id", "original_mutation_id", "repair_mutation_id", "status", "terminal_code", "created_at", "completed_at") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "sync_project_repairs_pending_idx")
	return ok && normalizeV17(index) == normalizeV17(strings.Split(schemaV17, ";")[1]) && s.schemaIndexColumns(ctx, "sync_project_repairs_pending_idx", "status", "repair_mutation_id")
}

func (s *Store) syncPortableIdentityAdoptionSchemaHealthy(ctx context.Context) bool {
	table, ok := s.schemaSQL(ctx, "table", "sync_portable_identity_adoptions")
	return ok && normalizeSchemaSQL(table) == normalizeSchemaSQL(strings.TrimSuffix(strings.TrimSpace(schemaV15), ";")) && s.schemaColumns(ctx, "sync_portable_identity_adoptions", "portable_project_id", "record_kind", "local_id", "portable_id", "adopting_device_id", "adopted_at")
}

func (s *Store) syncPortableIdentitySchemaHealthy(ctx context.Context) bool {
	table, ok := s.schemaSQL(ctx, "table", "sync_portable_identities")
	if !ok || normalizeSchemaSQL(table) != normalizeSchemaSQL(strings.Split(schemaV14, ";")[0]) || !s.schemaColumns(ctx, "sync_portable_identities", "portable_project_id", "record_kind", "local_id", "portable_id", "origin_device_id", "created_at") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "sync_portable_identities_inverse_idx")
	return ok && normalizeSchemaSQL(index) == normalizeSchemaSQL(strings.Split(schemaV14, ";")[1]) && s.schemaIndexColumns(ctx, "sync_portable_identities_inverse_idx", "portable_project_id", "record_kind", "portable_id")
}

func (s *Store) syncProjectCursorSchemaHealthy(ctx context.Context) bool {
	normalizeV16 := func(value string) string { return strings.ReplaceAll(normalizeSchemaSQL(value), "if not exists ", "") }
	cursor, ok := s.schemaSQL(ctx, "table", "sync_project_cursor")
	if !ok || normalizeV16(cursor) != normalizeV16(strings.Split(schemaV16, ";")[0]) || !s.schemaColumns(ctx, "sync_project_cursor", "portable_project_id", "history_id", "position", "watermark", "updated_at") {
		return false
	}
	inbox, ok := s.schemaSQL(ctx, "table", "sync_project_inbox")
	if !ok || normalizeV16(inbox) != normalizeV16(strings.Split(schemaV16, ";")[1]) || !s.schemaColumns(ctx, "sync_project_inbox", "portable_project_id", "history_id", "seq", "change_hash", "applied_at") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "sync_project_inbox_cursor_idx")
	return ok && normalizeV16(index) == normalizeV16(strings.Split(schemaV16, ";")[2]) && s.schemaIndexColumns(ctx, "sync_project_inbox_cursor_idx", "portable_project_id", "history_id", "seq")
}

func (s *Store) portableProjectIdentitySchemaHealthy(ctx context.Context) bool {
	if table, ok := s.schemaSQL(ctx, "table", "portable_project_identities"); !ok || normalizeSchemaSQL(table) != normalizeSchemaSQL(strings.Split(schemaV13, ";")[0]) || !s.schemaColumns(ctx, "portable_project_identities", "portable_id", "project_id", "workspace_hash", "source", "bound_at") {
		return false
	}
	index, ok := s.schemaSQL(ctx, "index", "portable_project_identities_project_id_idx")
	return ok && normalizeSchemaSQL(index) == normalizeSchemaSQL(strings.Split(schemaV13, ";")[1]) && s.schemaIndexColumns(ctx, "portable_project_identities_project_id_idx", "project_id")
}

func healthError(ctx context.Context, message string) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrCorrupt, message)
}

func (s *Store) sddSchemaHealthy(ctx context.Context) bool {
	schema, ok := s.schemaSQL(ctx, "table", "sdd_changes")
	return ok && schemaHas(schema, "model_plan text not null") && schemaHasExact(schema, "model_planTEXTNOTNULLCHECK(model_planIN('low','medium','high','ultra'))")
}

func (s *Store) syncSchemaHealthy(ctx context.Context) bool {
	for _, table := range []string{"projects", "sessions", "observations"} {
		schema, ok := s.schemaSQL(ctx, "table", table)
		if !ok || !schemaHas(schema, "sync_version integer not null default 0 check (sync_version >= 0)") {
			return false
		}
	}
	profileSQL, ok := s.schemaSQL(ctx, "table", "sync_profiles")
	if !ok || !s.schemaColumns(ctx, "sync_profiles", "singleton", "enabled", "endpoint", "device_id", "credential_ref", "created_at", "updated_at", "previous_credential_ref") || !schemaHas(profileSQL,
		"singleton integer primary key check (singleton = 1)",
		"enabled integer not null check (enabled in (0, 1))",
		"endpoint text not null check (length(endpoint) between 1 and 2048)",
		"device_id text not null check (length(device_id) = 36)",
		"credential_ref text not null check (length(credential_ref) between 10 and 512)",
		"previous_credential_ref text null check (previous_credential_ref is null or length(previous_credential_ref) between 10 and 512)",
		"created_at integer not null check (created_at > 0)",
		"updated_at integer not null check (updated_at >= created_at)") {
		return false
	}
	outboxSQL, ok := s.schemaSQL(ctx, "table", "sync_outbox")
	if !ok || !s.schemaColumns(ctx, "sync_outbox", "id", "mutation_id", "record_kind", "record_id", "mutation_kind", "base_version", "payload_version", "payload", "state", "attempts", "next_attempt_at", "last_error_code", "created_at", "updated_at") || !schemaHas(outboxSQL,
		"id integer primary key",
		"mutation_id text not null unique check (length(mutation_id) = 36)",
		"record_kind text not null check (record_kind in ('project', 'session', 'observation'))",
		"record_id text not null check (length(cast(record_id as blob)) between 1 and 1024)",
		"mutation_kind text not null check (mutation_kind in ('create', 'update', 'archive', 'tombstone', 'resolve'))",
		"base_version integer not null check (base_version >= 0)",
		"payload_version integer not null check (payload_version = 1)",
		"payload blob not null check (length(payload) between 1 and 1048576)",
		"state text not null check (state in ('pending', 'retry'))",
		"attempts integer not null default 0 check (attempts >= 0)",
		"next_attempt_at integer not null check (next_attempt_at > 0)",
		"last_error_code text not null default '' check (length(last_error_code) <= 64)",
		"created_at integer not null check (created_at > 0)",
		"updated_at integer not null check (updated_at >= created_at)") {
		return false
	}
	indexSQL, ok := s.schemaSQL(ctx, "index", "sync_outbox_due_idx")
	if !ok || strings.TrimSpace(strings.ToLower(indexSQL)) != "create index sync_outbox_due_idx on sync_outbox(next_attempt_at, created_at, id)" || !s.schemaIndexColumns(ctx, "sync_outbox_due_idx", "next_attempt_at", "created_at", "id") {
		return false
	}
	return s.syncV8SchemaHealthy(ctx) && s.syncV9SchemaHealthy(ctx) && s.syncV10SchemaHealthy(ctx)
}

func (s *Store) syncV10SchemaHealthy(ctx context.Context) bool {
	tableSQL, ok := s.schemaSQL(ctx, "table", "sync_outbox_claims")
	if !ok || normalizeV10TableSQL(tableSQL) != normalizeV10TableSQL(strings.Split(schemaV10, ";")[0]) || !s.schemaColumns(ctx, "sync_outbox_claims", "mutation_id", "first_claim_token", "claim_token", "first_claimed_at", "claimed_at", "lease_until") {
		return false
	}
	indexSQL, ok := s.schemaSQL(ctx, "index", "sync_outbox_claims_lease_idx")
	return ok && strings.TrimSpace(strings.ToLower(indexSQL)) == "create index sync_outbox_claims_lease_idx on sync_outbox_claims(lease_until, mutation_id)" && s.schemaIndexColumns(ctx, "sync_outbox_claims_lease_idx", "lease_until", "mutation_id")
}

func normalizeV10TableSQL(sql string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(sql)), " ")
}

func (s *Store) syncV9SchemaHealthy(ctx context.Context) bool {
	schema, ok := s.schemaSQL(ctx, "table", "sync_push_results")
	expected := strings.Split(schemaV9, ";")[0]
	return ok && normalizeSchemaSQL(schema) == normalizeSchemaSQL(expected) && s.schemaColumns(ctx, "sync_push_results", "mutation_id", "disposition", "retryable", "code", "sequence", "canonical_version", "record_kind", "record_id", "mutation_kind", "base_version", "mutation_hash", "completed_at")
}

func (s *Store) syncV8SchemaHealthy(ctx context.Context) bool {
	want := schemaV8Objects()
	for _, table := range []struct {
		name    string
		columns []string
	}{
		{"sync_inbox", []string{"history_id", "seq", "change_hash", "applied_at"}},
		{"sync_cursor", []string{"singleton", "history_id", "position", "updated_at"}},
		{"sync_tombstones", []string{"history_id", "seq", "record_kind", "record_id", "canonical_version", "payload_version", "provenance", "deleted_at"}},
		{"sync_conflicts", []string{"conflict_id", "history_id", "created_seq", "record_kind", "record_id", "canonical_version", "competing_version_id", "status", "resolved_seq", "payload_version", "snapshot", "created_at", "updated_at"}},
		{"sync_bootstrap", []string{"singleton", "phase", "payload_version", "checkpoint", "created_at", "updated_at"}},
	} {
		schema, ok := s.schemaSQL(ctx, "table", table.name)
		if !ok || normalizeSchemaSQL(schema) != want["table:"+table.name] || !s.schemaColumns(ctx, table.name, table.columns...) {
			return false
		}
	}
	for _, index := range []struct {
		name    string
		columns []string
	}{
		{"sync_tombstones_record_idx", []string{"record_kind", "record_id", "canonical_version"}},
		{"sync_conflicts_unresolved_idx", []string{"status", "record_kind", "record_id", "created_seq"}},
	} {
		schema, ok := s.schemaSQL(ctx, "index", index.name)
		if !ok || normalizeSchemaSQL(schema) != want["index:"+index.name] || !s.schemaIndexColumns(ctx, index.name, index.columns...) {
			return false
		}
	}
	return true
}

func schemaV8Objects() map[string]string {
	objects := make(map[string]string, 7)
	for _, statement := range strings.Split(schemaV8, ";") {
		fields := strings.Fields(statement)
		if len(fields) < 3 || !strings.EqualFold(fields[0], "create") || !(strings.EqualFold(fields[1], "table") || strings.EqualFold(fields[1], "index")) {
			continue
		}
		objects[strings.ToLower(fields[1])+":"+strings.ToLower(fields[2])] = normalizeSchemaSQL(statement)
	}
	return objects
}

func normalizeSchemaSQL(schema string) string {
	return strings.ToLower(strings.Join(strings.Fields(schema), " "))
}

func schemaHas(schema string, fragments ...string) bool {
	schema = strings.ToLower(schema)
	for _, fragment := range fragments {
		if !strings.Contains(schema, fragment) {
			return false
		}
	}
	return true
}

func schemaHasExact(schema, fragment string) bool {
	schema = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, schema)
	return strings.Contains(schema, fragment)
}

func (s *Store) schemaSQL(ctx context.Context, kind, name string) (string, bool) {
	var value string
	if s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type=? AND name=?`, kind, name).Scan(&value) != nil {
		return "", false
	}
	return value, value != ""
}

func (s *Store) schemaColumns(ctx context.Context, name string, want ...string) bool {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, name)
	if err != nil {
		return false
	}
	defer rows.Close()
	for _, expected := range want {
		if !rows.Next() {
			return false
		}
		var actual string
		if rows.Scan(&actual) != nil || actual != expected {
			return false
		}
	}
	return !rows.Next() && rows.Err() == nil
}

func (s *Store) schemaIndexColumns(ctx context.Context, name string, want ...string) bool {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, name)
	if err != nil {
		return false
	}
	defer rows.Close()
	for _, expected := range want {
		if !rows.Next() {
			return false
		}
		var actual string
		if rows.Scan(&actual) != nil || actual != expected {
			return false
		}
	}
	return !rows.Next() && rows.Err() == nil
}

// StableProjectID derives the stable project identity for a canonical workspace.
// It is pure and performs no filesystem access.
func StableProjectID(canonicalWorkspace string) (string, error) {
	if canonicalWorkspace == "" || !filepath.IsAbs(canonicalWorkspace) || filepath.Clean(canonicalWorkspace) != canonicalWorkspace || filepath.Dir(canonicalWorkspace) == canonicalWorkspace {
		return "", fmt.Errorf("%w: invalid workspace", ErrInvalid)
	}
	digest := sha256.Sum256([]byte(canonicalWorkspace))
	name := []rune(filepath.Base(canonicalWorkspace))
	if len(name) > 243 {
		name = name[:243]
	}
	return fmt.Sprintf("%s-%x", string(name), digest[:6]), nil
}

// ResolveProject maps one canonical workspace to one durable project identity.
// Writable stores persist the binding; read-only stores resolve the same identity
// without creating a project or root binding.
func (s *Store) ResolveProject(ctx context.Context, workspace string) (string, error) {
	if err := cancelled(ctx); err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("%w: invalid workspace", ErrInvalid)
	}
	absolute = filepath.Clean(absolute)
	if absolute, err = filepath.EvalSymlinks(absolute); err != nil {
		return "", fmt.Errorf("%w: invalid workspace", ErrInvalid)
	}
	absolute = config.CanonicalizeExistingPathCase(absolute)
	if absolute == string(filepath.Separator) || filepath.Base(absolute) == "." || filepath.Base(absolute) == string(filepath.Separator) {
		return "", fmt.Errorf("%w: invalid workspace", ErrInvalid)
	}
	digest := sha256.Sum256([]byte(absolute))
	workspaceHash := hex.EncodeToString(digest[:])
	legacyID := filepath.Base(absolute)
	stableID, err := StableProjectID(absolute)
	if err != nil {
		return "", err
	}
	if s.readOnly {
		var projectID string
		err = s.db.QueryRowContext(ctx, `SELECT project_id FROM project_roots WHERE workspace_hash=?`, workspaceHash).Scan(&projectID)
		if err == nil {
			return projectID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", writeError(ctx, err)
		}
		var legacyAvailable int
		if err = s.db.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM projects p
				WHERE p.id=?
				  AND NOT EXISTS(SELECT 1 FROM project_roots r WHERE r.project_id=p.id)
			)`, legacyID).Scan(&legacyAvailable); err != nil {
			return "", writeError(ctx, err)
		}
		if legacyAvailable == 1 {
			return legacyID, nil
		}
		return stableID, nil
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", writeError(ctx, err)
	}
	defer conn.Close()
	for attempt := 0; ; attempt++ {
		_, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`)
		if err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return "", writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	var projectID string
	err = conn.QueryRowContext(ctx, `SELECT project_id FROM project_roots WHERE workspace_hash=?`, workspaceHash).Scan(&projectID)
	if err == nil {
		if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
			return "", writeError(ctx, err)
		}
		committed = true
		return projectID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", writeError(ctx, err)
	}

	var legacyAvailable int
	if err = conn.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM projects p
			WHERE p.id=?
			  AND NOT EXISTS(SELECT 1 FROM project_roots r WHERE r.project_id=p.id)
		)`, legacyID).Scan(&legacyAvailable); err != nil {
		return "", writeError(ctx, err)
	}
	projectID = stableID
	if legacyAvailable == 1 {
		projectID = legacyID
	}
	if _, err = conn.ExecContext(ctx, `INSERT OR IGNORE INTO projects(id) VALUES(?)`, projectID); err == nil {
		_, err = conn.ExecContext(ctx, `INSERT INTO project_roots(workspace_hash,project_id) VALUES(?,?)`, workspaceHash, projectID)
	}
	if err != nil {
		return "", conflictOrWrite(ctx, err)
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return "", writeError(ctx, err)
	}
	committed = true
	return projectID, nil
}

// BindPortableProjectID records explicit, local provenance for a published
// marker. It never changes the legacy project ID or any data that references it.
func (s *Store) BindPortableProjectID(ctx context.Context, workspace, portableID string) error {
	if s.readOnly || !projectIDPattern.MatchString(portableID) {
		return fmt.Errorf("%w: portable project identity", ErrInvalid)
	}
	localID, err := s.ResolveProject(ctx, workspace)
	if err != nil {
		return err
	}
	hash, err := portableWorkspaceHash(workspace)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return writeError(ctx, err)
	}
	defer tx.Rollback()
	var existingPortable, existingProject string
	err = tx.QueryRowContext(ctx, `SELECT portable_id, project_id FROM portable_project_identities WHERE portable_id=? OR workspace_hash=? LIMIT 1`, portableID, hash).Scan(&existingPortable, &existingProject)
	if err == nil {
		if existingPortable != portableID || existingProject != localID {
			return fmt.Errorf("%w: portable project identity binding", ErrConflict)
		}
		return commit(ctx, tx)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return writeError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES(?,?,?,?,?)`, portableID, localID, hash, "explicit-init", s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return conflictOrWrite(ctx, err)
	}
	return commit(ctx, tx)
}

func (s *Store) PortableProjectID(ctx context.Context, workspace string) (string, bool, error) {
	hash, err := portableWorkspaceHash(workspace)
	if err != nil {
		return "", false, err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT portable_id FROM portable_project_identities WHERE workspace_hash=?`, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, writeError(ctx, err)
	}
	return id, true, nil
}

// BoundPortableProject resolves the local project explicitly bound to a
// portable marker for one canonical workspace. It never creates a project.
func (s *Store) BoundPortableProject(ctx context.Context, workspace, portableID string) (string, bool, error) {
	if !projectIDPattern.MatchString(portableID) {
		return "", false, fmt.Errorf("%w: portable project identity", ErrInvalid)
	}
	hash, err := portableWorkspaceHash(workspace)
	if err != nil {
		return "", false, err
	}
	var projectID string
	err = s.db.QueryRowContext(ctx, `SELECT project_id FROM portable_project_identities WHERE workspace_hash=? AND portable_id=?`, hash, portableID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, writeError(ctx, err)
	}
	return projectID, true, nil
}

func portableWorkspaceHash(workspace string) (string, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("%w: invalid workspace", ErrInvalid)
	}
	abs, err = filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return "", fmt.Errorf("%w: invalid workspace", ErrInvalid)
	}
	digest := sha256.Sum256([]byte(config.CanonicalizeExistingPathCase(abs)))
	return hex.EncodeToString(digest[:]), nil
}

func (s *Store) Save(ctx context.Context, item Observation) (Observation, error) {
	if err := validateObservation(item); err != nil {
		return Observation{}, err
	}
	if err := cancelled(ctx); err != nil {
		return Observation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Observation{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	if err := rejectTombstoned(ctx, tx, item.ID); err != nil {
		return Observation{}, err
	}
	if item.TopicKey != "" {
		existing, found, err := loadOne(tx.QueryRowContext(ctx, observationSelect+` WHERE o.project_id=? AND o.scope=? AND o.topic_key=?`, item.Project, item.Scope, item.TopicKey))
		if err != nil {
			return Observation{}, err
		}
		if found {
			item.ID = existing.ID
			return s.updateTx(ctx, tx, existing, item)
		}
	}
	if item.ID == "" {
		item.ID, err = newID()
		if err != nil {
			return Observation{}, fmt.Errorf("create observation id: %w", err)
		}
	}
	if err := validateReferences(ctx, tx, item); err != nil {
		return Observation{}, err
	}
	now := s.now().UTC()
	item.CreatedAt, item.UpdatedAt = now, now
	if err := insertObservation(ctx, tx, item); err != nil {
		return Observation{}, err
	}
	if err := s.enqueueLocalWrite(ctx, tx, item); err != nil {
		return Observation{}, err
	}
	if err := commit(ctx, tx); err != nil {
		return Observation{}, err
	}
	return item, nil
}
func (s *Store) Update(ctx context.Context, item Observation) (Observation, error) {
	if err := validateObservation(item); err != nil {
		return Observation{}, err
	}
	if item.ID == "" {
		return Observation{}, fmt.Errorf("%w: update requires id", ErrInvalid)
	}
	if err := cancelled(ctx); err != nil {
		return Observation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Observation{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	existing, found, err := loadOne(tx.QueryRowContext(ctx, observationSelect+` WHERE id=?`, item.ID))
	if err != nil {
		return Observation{}, err
	}
	if !found {
		return Observation{}, fmt.Errorf("%w: observation not found", ErrNotFound)
	}
	return s.updateTx(ctx, tx, existing, item)
}
func (s *Store) updateTx(ctx context.Context, tx *sql.Tx, existing, item Observation) (Observation, error) {
	if isProviderSessionSummary(existing) {
		return Observation{}, fmt.Errorf("%w: provider session summary is immutable", ErrConflict)
	}
	if err := rejectTombstoned(ctx, tx, existing.ID); err != nil {
		return Observation{}, err
	}
	if existing.Project != item.Project || existing.Scope != item.Scope || existing.Provenance != item.Provenance || existing.CreatedAt != item.CreatedAt && !item.CreatedAt.IsZero() {
		return Observation{}, fmt.Errorf("%w: identity boundary cannot change", ErrConflict)
	}
	if existing.State == StateArchived || !(existing.State == item.State || existing.State == StateActive && (item.State == StateNeedsReview || item.State == StateArchived) || existing.State == StateNeedsReview && (item.State == StateActive || item.State == StateArchived)) {
		return Observation{}, fmt.Errorf("%w: illegal lifecycle transition", ErrInvalid)
	}
	if err := validateReferences(ctx, tx, item); err != nil {
		return Observation{}, err
	}
	item.ID, item.CreatedAt, item.UpdatedAt = existing.ID, existing.CreatedAt, s.now().UTC()
	review := nullableTime(item.ReviewAfter)
	_, err := tx.ExecContext(ctx, `UPDATE observations SET title=?,session_id=?,type=?,content=?,topic_key=?,producer=?,source_provider=?,source_id=?,state=?,updated_at=?,review_after=? WHERE id=?`, item.Title, nullable(item.Session), item.Type, item.Content, nullable(item.TopicKey), item.Provenance.Producer, item.Provenance.SourceProvider, item.Provenance.SourceID, item.State, item.UpdatedAt.UnixNano(), review, item.ID)
	if err != nil {
		return Observation{}, conflictOrWrite(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM observation_refs WHERE observation_id=?`, item.ID); err == nil {
		err = insertReferences(ctx, tx, item)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM observations_fts WHERE id=?`, item.ID)
	}
	if err == nil && item.State != StateArchived {
		_, err = tx.ExecContext(ctx, `INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)`, item.ID, item.Title, item.TopicKey, item.Type, item.Content)
	}
	if err != nil {
		return Observation{}, writeError(ctx, err)
	}
	if err := s.enqueueLocalWrite(ctx, tx, item); err != nil {
		return Observation{}, err
	}
	if err := commit(ctx, tx); err != nil {
		return Observation{}, err
	}
	return item, nil
}

func isProviderSessionSummary(item Observation) bool {
	return strings.HasPrefix(item.TopicKey, "provider-session-summary-")
}
func (s *Store) Search(ctx context.Context, filter Search) ([]Observation, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(filter.Query) == "" || filter.Project == "" || filter.Scope != ScopeProject && filter.Scope != ScopePersonal || filter.Limit < 0 || filter.Limit > 100 {
		return nil, fmt.Errorf("%w: invalid search input", ErrInvalid)
	}
	for _, value := range filter.Types {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: invalid type filter", ErrInvalid)
		}
	}
	for _, value := range filter.States {
		if value != StateActive && value != StateNeedsReview && value != StateArchived {
			return nil, fmt.Errorf("%w: invalid lifecycle filter", ErrInvalid)
		}
	}
	query := observationColumns + ` FROM observations o JOIN observations_fts ON observations_fts.id=o.id WHERE o.project_id=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id)`
	args := []any{filter.Project}
	if filter.Scope != "" {
		query += ` AND o.scope=?`
		args = append(args, filter.Scope)
	}
	query, args = addStrings(query, args, "o.type", filter.Types)
	if filter.TopicKey != "" {
		query += ` AND o.topic_key=?`
		args = append(args, filter.TopicKey)
	}
	states := make([]string, len(filter.States))
	for i, state := range filter.States {
		states[i] = string(state)
	}
	query, args = addStrings(query, args, "o.state", states)
	query += ` AND observations_fts MATCH ? ORDER BY bm25(observations_fts), o.updated_at DESC, o.id ASC LIMIT ?`
	limit := filter.Limit
	if limit == 0 {
		limit = 20
	}
	args = append(args, filter.Query, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		if ctx.Err() != nil {
			return nil, cancelled(ctx)
		}
		return nil, fmt.Errorf("%w: invalid search query", ErrInvalid)
	}
	defer rows.Close()
	var found []Observation
	for rows.Next() {
		item, err := scanObservation(rows)
		if err != nil {
			return nil, fmt.Errorf("%w: read observation", ErrCorrupt)
		}
		found = append(found, item)
	}
	if err := rows.Err(); err != nil {
		return nil, writeError(ctx, err)
	}
	return found, nil
}

func (s *Store) Recent(ctx context.Context, request Recent) ([]Observation, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if request.Project == "" || request.Scope != ScopeProject || request.Limit < 0 || request.Limit > 50 {
		return nil, fmt.Errorf("%w: invalid recent input", ErrInvalid)
	}
	states := append([]State(nil), request.States...)
	if len(states) == 0 {
		states = []State{StateActive}
	}
	stateValues := make([]string, len(states))
	for i, state := range states {
		if state != StateActive && state != StateNeedsReview && state != StateArchived {
			return nil, fmt.Errorf("%w: invalid recent lifecycle filter", ErrInvalid)
		}
		stateValues[i] = string(state)
	}
	query := observationColumns + ` FROM observations o WHERE o.project_id=? AND o.scope=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id)`
	args := []any{request.Project, request.Scope}
	query, args = addStrings(query, args, "o.state", stateValues)
	query += ` ORDER BY o.updated_at DESC, o.id ASC LIMIT ?`
	limit := request.Limit
	if limit == 0 {
		limit = 20
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, writeError(ctx, err)
	}
	defer rows.Close()
	var found []Observation
	for rows.Next() {
		item, scanErr := scanObservation(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: read observation", ErrCorrupt)
		}
		found = append(found, item)
	}
	if err := rows.Err(); err != nil {
		return nil, writeError(ctx, err)
	}
	return found, nil
}

func (s *Store) Get(ctx context.Context, id, project string, scope Scope) (Observation, error) {
	if err := cancelled(ctx); err != nil {
		return Observation{}, err
	}
	if id == "" || project == "" || scope != ScopeProject && scope != ScopePersonal {
		return Observation{}, fmt.Errorf("%w: invalid get input", ErrInvalid)
	}
	item, found, err := loadOne(s.db.QueryRowContext(ctx, observationSelect+` WHERE o.id=? AND o.project_id=? AND o.scope=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id)`, id, project, scope))
	if err != nil {
		return Observation{}, err
	}
	if !found {
		return Observation{}, fmt.Errorf("%w: observation not found", ErrNotFound)
	}
	return item, nil
}

func rejectTombstoned(ctx context.Context, tx *sql.Tx, id string) error {
	if id == "" {
		return nil
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_tombstones WHERE record_kind='observation' AND record_id=?`, id).Scan(&n); err != nil {
		return writeError(ctx, err)
	}
	if n != 0 {
		return fmt.Errorf("%w: observation tombstoned", ErrConflict)
	}
	return nil
}
func validateObservation(item Observation) error {
	if item.Project == "" || item.Scope != ScopeProject && item.Scope != ScopePersonal || strings.TrimSpace(item.Type) == "" || strings.TrimSpace(item.Content) == "" || len([]rune(item.Content)) > 4096 || len([]rune(item.Title)) > 256 || len(item.References) > 50 || item.Provenance.Producer == "" || item.State != StateActive && item.State != StateNeedsReview && item.State != StateArchived {
		return fmt.Errorf("%w: observation fields are invalid", ErrInvalid)
	}
	if (item.Provenance.SourceProvider == "") != (item.Provenance.SourceID == "") {
		return fmt.Errorf("%w: provenance source requires provider and id", ErrInvalid)
	}
	return nil
}
func validateReferences(ctx context.Context, tx *sql.Tx, item Observation) error {
	seen := map[string]bool{}
	for _, id := range item.References {
		if id == "" || id == item.ID || seen[id] {
			return fmt.Errorf("%w: invalid observation reference", ErrInvalid)
		}
		seen[id] = true
		var project string
		var scope Scope
		if err := tx.QueryRowContext(ctx, `SELECT project_id,scope FROM observations WHERE id=?`, id).Scan(&project, &scope); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: referenced observation not found", ErrNotFound)
		} else if err != nil {
			return writeError(ctx, err)
		}
		if project != item.Project || scope != item.Scope {
			return fmt.Errorf("%w: reference crosses boundary", ErrConflict)
		}
		if err := rejectTombstoned(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}
func insertObservation(ctx context.Context, tx *sql.Tx, item Observation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO observations(id,title,project_id,session_id,scope,type,content,topic_key,producer,source_provider,source_id,state,created_at,updated_at,review_after) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Title, item.Project, nullable(item.Session), item.Scope, item.Type, item.Content, nullable(item.TopicKey), item.Provenance.Producer, item.Provenance.SourceProvider, item.Provenance.SourceID, item.State, item.CreatedAt.UnixNano(), item.UpdatedAt.UnixNano(), nullableTime(item.ReviewAfter))
	if err != nil {
		return conflictOrWrite(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)`, item.ID, item.Title, item.TopicKey, item.Type, item.Content); err != nil {
		return writeError(ctx, err)
	}
	return insertReferences(ctx, tx, item)
}
func insertReferences(ctx context.Context, tx *sql.Tx, item Observation) error {
	for _, target := range item.References {
		if _, err := tx.ExecContext(ctx, `INSERT INTO observation_refs(observation_id,target_id) VALUES(?,?)`, item.ID, target); err != nil {
			return writeError(ctx, err)
		}
	}
	return nil
}

const observationColumns = `SELECT o.id,o.title,o.project_id,COALESCE(o.session_id,''),o.scope,o.type,o.content,COALESCE(o.topic_key,''),o.producer,o.source_provider,o.source_id,o.state,o.created_at,o.updated_at,o.review_after,COALESCE((SELECT group_concat(target_id,char(31)) FROM observation_refs WHERE observation_id=o.id),'')`
const observationSelect = observationColumns + ` FROM observations o`

type scanner interface{ Scan(...any) error }

func loadOne(row scanner) (Observation, bool, error) {
	item, err := scanObservation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, false, nil
	}
	if err != nil {
		return Observation{}, false, fmt.Errorf("%w: read observation", ErrCorrupt)
	}
	return item, true, nil
}
func scanObservation(row scanner) (Observation, error) {
	var item Observation
	var created, updated int64
	var review sql.NullInt64
	var refs string
	err := row.Scan(&item.ID, &item.Title, &item.Project, &item.Session, &item.Scope, &item.Type, &item.Content, &item.TopicKey, &item.Provenance.Producer, &item.Provenance.SourceProvider, &item.Provenance.SourceID, &item.State, &created, &updated, &review, &refs)
	if err != nil {
		return Observation{}, err
	}
	item.CreatedAt, item.UpdatedAt = time.Unix(0, created).UTC(), time.Unix(0, updated).UTC()
	if review.Valid {
		value := time.Unix(0, review.Int64).UTC()
		item.ReviewAfter = &value
	}
	if refs != "" {
		item.References = strings.Split(refs, "\x1f")
	}
	return item, err
}
func addStrings(query string, args []any, column string, values []string) (string, []any) {
	if len(values) == 0 {
		return query, args
	}
	query += ` AND ` + column + ` IN (` + strings.TrimRight(strings.Repeat("?,", len(values)), ",") + `)`
	for _, value := range values {
		args = append(args, value)
	}
	return query, args
}
func commit(ctx context.Context, tx *sql.Tx) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return writeError(ctx, err)
	}
	return nil
}
func cancelled(ctx context.Context) error {
	return ctx.Err()
}
func writeError(ctx context.Context, _ error) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	return fmt.Errorf("%w: memory operation failed", ErrCorrupt)
}
func conflictOrWrite(ctx context.Context, err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
		return fmt.Errorf("%w: observation already exists", ErrConflict)
	}
	return writeError(ctx, err)
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixNano()
}
func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "obs-" + hex.EncodeToString(value[:]), nil
}
