package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ImportLegacy merges one retired project-local memory database into the
// unified store. The source remains untouched as a recovery backup, and the
// import ledger makes retries safe across process restarts.
func (s *Store) ImportLegacy(ctx context.Context, path string, targetProjects ...string) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	targetPath, sourcePath := databasePath(s.db), filepath.Clean(path)
	if targetPath != "" && samePath(targetPath, sourcePath) {
		return nil
	}
	if err := rejectSymlink(sourcePath); err != nil {
		return err
	}
	info, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: legacy memory storage is unavailable", ErrCorrupt)
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return writeError(ctx, err)
	}
	defer conn.Close()
	sourceURI := sqliteReadURI(sourcePath)
	if _, err = conn.ExecContext(ctx, `ATTACH DATABASE ? AS legacy`, sourceURI); err != nil {
		return fmt.Errorf("%w: legacy memory storage is unavailable", ErrCorrupt)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), `DETACH DATABASE legacy`) }()

	version, err := legacyVersion(ctx, conn)
	if err != nil {
		return err
	}
	title := `''`
	if version >= 2 {
		title = `l.title`
	}
	targetProject := ""
	if len(targetProjects) > 1 {
		return fmt.Errorf("%w: multiple legacy import targets", ErrInvalid)
	}
	if len(targetProjects) == 1 {
		targetProject = targetProjects[0]
	}
	sourceID := fmt.Sprintf("%x", sha256.Sum256([]byte(sourcePath)))

	for attempt := 0; ; attempt++ {
		_, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`)
		if err == nil {
			break
		}
		if err = waitForSQLite(ctx, attempt, err); err != nil {
			return writeError(ctx, err)
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	var imported int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM main.legacy_imports WHERE source_id=?`, sourceID).Scan(&imported); err != nil {
		return writeError(ctx, err)
	}
	if imported != 0 {
		if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
			return writeError(ctx, err)
		}
		committed = true
		return nil
	}

	var conflicts int
	projectExpression, projectArgs := `l.project_id`, []any{}
	if targetProject != "" {
		projectExpression, projectArgs = `?`, []any{targetProject}
	}
	conflictQuery := fmt.Sprintf(`
		SELECT count(*)
		FROM legacy.observations l
		JOIN main.observations m ON m.id=l.id
		WHERE NOT (
			m.project_id IS %s AND m.session_id IS l.session_id AND
			m.scope IS l.scope AND m.type IS l.type AND m.content IS l.content AND
			m.topic_key IS l.topic_key AND m.producer IS l.producer AND
			m.source_provider IS l.source_provider AND m.source_id IS l.source_id AND
			m.state IS l.state AND m.created_at IS l.created_at AND
			m.updated_at IS l.updated_at AND m.review_after IS l.review_after AND
			m.title IS %s
		)`, projectExpression, title)
	if err = conn.QueryRowContext(ctx, conflictQuery, projectArgs...).Scan(&conflicts); err != nil {
		return writeError(ctx, err)
	}
	if conflicts != 0 {
		return fmt.Errorf("%w: legacy observation identity collision", ErrConflict)
	}
	topicQuery := fmt.Sprintf(`
		SELECT count(*)
		FROM legacy.observations l
		JOIN main.observations m
		  ON m.project_id=%s AND m.scope=l.scope AND m.topic_key=l.topic_key
		WHERE l.topic_key IS NOT NULL AND m.id<>l.id`, projectExpression)
	if err = conn.QueryRowContext(ctx, topicQuery, projectArgs...).Scan(&conflicts); err != nil {
		return writeError(ctx, err)
	}
	if conflicts != 0 {
		return fmt.Errorf("%w: legacy topic key collision", ErrConflict)
	}

	type statement struct {
		query string
		args  []any
	}
	statements := []statement{
		{`INSERT OR IGNORE INTO main.projects(id) SELECT id FROM legacy.projects`, nil},
		{`INSERT OR IGNORE INTO main.sessions(id,project_id) SELECT id,project_id FROM legacy.sessions`, nil},
		{fmt.Sprintf(`INSERT OR IGNORE INTO main.observations(
			id,project_id,session_id,scope,type,content,topic_key,producer,
			source_provider,source_id,state,created_at,updated_at,review_after,title
		) SELECT
			l.id,l.project_id,l.session_id,l.scope,l.type,l.content,l.topic_key,l.producer,
			l.source_provider,l.source_id,l.state,l.created_at,l.updated_at,l.review_after,%s
		FROM legacy.observations l`, title), nil},
		{`INSERT OR IGNORE INTO main.observation_refs(observation_id,target_id)
		 SELECT observation_id,target_id FROM legacy.observation_refs`, nil},
		{`INSERT INTO main.observations_fts(id,content)
		 SELECT l.id,l.content FROM legacy.observations l
		 WHERE NOT EXISTS (SELECT 1 FROM main.observations_fts f WHERE f.id=l.id)`, nil},
	}
	if targetProject != "" {
		statements[0] = statement{`INSERT OR IGNORE INTO main.projects(id) VALUES(?)`, []any{targetProject}}
		statements[1] = statement{`INSERT OR IGNORE INTO main.sessions(id,project_id) SELECT id,? FROM legacy.sessions`, []any{targetProject}}
		statements[2] = statement{fmt.Sprintf(`INSERT OR IGNORE INTO main.observations(
			id,project_id,session_id,scope,type,content,topic_key,producer,
			source_provider,source_id,state,created_at,updated_at,review_after,title
		) SELECT
			l.id,?,l.session_id,l.scope,l.type,l.content,l.topic_key,l.producer,
			l.source_provider,l.source_id,l.state,l.created_at,l.updated_at,l.review_after,%s
		FROM legacy.observations l`, title), []any{targetProject}}
	}
	for _, statement := range statements {
		if _, err = conn.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return writeError(ctx, err)
		}
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO main.legacy_imports(source_id,imported_at) VALUES(?,?)`, sourceID, time.Now().UTC().UnixNano()); err != nil {
		return writeError(ctx, err)
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return writeError(ctx, err)
	}
	committed = true
	return nil
}

func legacyVersion(ctx context.Context, conn *sql.Conn) (int, error) {
	var version, tables int
	if err := conn.QueryRowContext(ctx, `PRAGMA legacy.user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("%w: legacy migration ledger unavailable", ErrCorrupt)
	}
	if version < 1 || version > 3 {
		return 0, fmt.Errorf("%w: unsupported legacy schema version %d", ErrMigration, version)
	}
	if err := conn.QueryRowContext(ctx, `
		SELECT count(*) FROM legacy.sqlite_schema
		WHERE type='table' AND name IN ('projects','sessions','observations','observation_refs','observations_fts')`).Scan(&tables); err != nil || tables != 5 {
		return 0, fmt.Errorf("%w: legacy schema unavailable", ErrCorrupt)
	}
	rows, err := conn.QueryContext(ctx, `PRAGMA legacy.integrity_check`)
	if err != nil {
		return 0, fmt.Errorf("%w: legacy integrity check unavailable", ErrCorrupt)
	}
	ok, count := true, 0
	for rows.Next() {
		var result string
		count++
		if rows.Scan(&result) != nil || result != "ok" {
			ok = false
		}
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil || count != 1 || !ok {
		return 0, fmt.Errorf("%w: legacy integrity check failed", ErrCorrupt)
	}
	return version, nil
}

func databasePath(db *sql.DB) string {
	var path string
	_ = db.QueryRow(`PRAGMA database_list`).Scan(new(int), new(string), &path)
	return filepath.Clean(path)
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
