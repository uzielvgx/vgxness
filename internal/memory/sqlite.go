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
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db         *sql.DB
	now        func() time.Time
	readOnly   bool
	checkpoint func() (busy, log, checkpointed int, err error)
	close      func() error
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
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
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
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return (&Store{db: db}).Health(ctx)
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
	var checkpointErr error
	if !s.readOnly {
		// Health inspection intentionally opens an immutable snapshot. Keep the
		// main database complete by checkpointing committed WAL state whenever
		// a writable application operation releases its store.
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

func checkpointResult(busy int, err error) error {
	if err != nil {
		return fmt.Errorf("checkpoint memory store: %w", err)
	}
	if busy != 0 {
		return errors.New("checkpoint memory store: busy")
	}
	return nil
}
func (s *Store) Health(ctx context.Context) (int, error) {
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return 0, fmt.Errorf("%w: integrity check unavailable", ErrCorrupt)
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
	if rowsErr != nil || integrityRows != 1 || !integrityOK {
		return 0, fmt.Errorf("%w: integrity check failed", ErrCorrupt)
	}
	rows, err = s.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return 0, fmt.Errorf("%w: foreign-key check unavailable", ErrCorrupt)
	}
	foreignKeyViolation := rows.Next()
	rowsErr = rows.Err()
	_ = rows.Close()
	if rowsErr != nil || foreignKeyViolation {
		return 0, fmt.Errorf("%w: foreign-key check failed", ErrCorrupt)
	}
	var version, probe int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("%w: migration ledger unavailable", ErrCorrupt)
	}
	if version > migrations[len(migrations)-1].version {
		return 0, fmt.Errorf("%w: database schema version %d is newer than supported version %d", ErrMigration, version, migrations[len(migrations)-1].version)
	}
	if version != migrations[len(migrations)-1].version {
		return 0, fmt.Errorf("%w: unsupported database schema version %d", ErrCorrupt, version)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN ('projects','sessions','observations','observation_refs','legacy_imports','project_roots','sdd_changes','sdd_artifacts','sdd_revisions','sdd_revision_links','sdd_projections')`).Scan(&probe); err != nil || probe != 11 {
		return 0, fmt.Errorf("%w: required schema unavailable", ErrCorrupt)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM observations_fts WHERE observations_fts MATCH 'healthchecknomatch'`).Scan(&probe); err != nil {
		return 0, fmt.Errorf("%w: FTS5 unavailable", ErrCorrupt)
	}
	return version, nil
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
	if absolute == string(filepath.Separator) || filepath.Base(absolute) == "." || filepath.Base(absolute) == string(filepath.Separator) {
		return "", fmt.Errorf("%w: invalid workspace", ErrInvalid)
	}
	digest := sha256.Sum256([]byte(absolute))
	workspaceHash := hex.EncodeToString(digest[:])
	legacyID := filepath.Base(absolute)
	name := []rune(legacyID)
	if len(name) > 243 {
		name = name[:243]
	}
	stableID := fmt.Sprintf("%s-%x", string(name), digest[:6])
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
	if existing.Project != item.Project || existing.Scope != item.Scope || existing.CreatedAt != item.CreatedAt && !item.CreatedAt.IsZero() {
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
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO observations_fts(id,content) VALUES(?,?)`, item.ID, item.Content)
	}
	if err != nil {
		return Observation{}, writeError(ctx, err)
	}
	if err := commit(ctx, tx); err != nil {
		return Observation{}, err
	}
	return item, nil
}
func (s *Store) Search(ctx context.Context, filter Search) ([]Observation, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(filter.Query) == "" || filter.Project == "" || filter.Scope != "" && filter.Scope != ScopeProject && filter.Scope != ScopePersonal || filter.Limit < 0 || filter.Limit > 100 {
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
	query := observationColumns + ` FROM observations o JOIN observations_fts ON observations_fts.id=o.id WHERE o.project_id=?`
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
	query := observationColumns + ` FROM observations o WHERE o.project_id=? AND o.scope=?`
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
	item, found, err := loadOne(s.db.QueryRowContext(ctx, observationSelect+` WHERE o.id=? AND o.project_id=? AND o.scope=?`, id, project, scope))
	if err != nil {
		return Observation{}, err
	}
	if !found {
		return Observation{}, fmt.Errorf("%w: observation not found", ErrNotFound)
	}
	return item, nil
}
func validateObservation(item Observation) error {
	if item.Project == "" || item.Scope != ScopeProject && item.Scope != ScopePersonal || strings.TrimSpace(item.Type) == "" || strings.TrimSpace(item.Content) == "" || item.Provenance.Producer == "" || item.State != StateActive && item.State != StateNeedsReview && item.State != StateArchived {
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
	}
	return nil
}
func insertObservation(ctx context.Context, tx *sql.Tx, item Observation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO observations(id,title,project_id,session_id,scope,type,content,topic_key,producer,source_provider,source_id,state,created_at,updated_at,review_after) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Title, item.Project, nullable(item.Session), item.Scope, item.Type, item.Content, nullable(item.TopicKey), item.Provenance.Producer, item.Provenance.SourceProvider, item.Provenance.SourceID, item.State, item.CreatedAt.UnixNano(), item.UpdatedAt.UnixNano(), nullableTime(item.ReviewAfter))
	if err != nil {
		return conflictOrWrite(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO observations_fts(id,content) VALUES(?,?)`, item.ID, item.Content); err != nil {
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
