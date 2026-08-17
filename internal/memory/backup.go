package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var vacuumInto = func(ctx context.Context, db *sql.DB, destination string) error {
	quoted := strings.ReplaceAll(destination, "'", "''")
	_, err := db.ExecContext(ctx, "VACUUM INTO '"+quoted+"'")
	return err
}

var healthSQLiteBackup = HealthFile

var closeSQLiteBackupOutput = func(file *os.File) error { return file.Close() }

// CreateSQLiteBackup writes a same-parent, O_EXCL private SQLite snapshot.
// Pre-terminal failures scrub the retained handle; terminal Close is non-fatal only after durability and identity gates.
func CreateSQLiteBackup(ctx context.Context, database, destination string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := validateSQLiteBackupPaths(database, destination)
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", sqliteReadURI(database))
	if err != nil {
		return err
	}
	defer func() {
		if db != nil {
			err = errors.Join(err, db.Close())
		}
	}()
	output, err := reserveSQLiteBackupOutput(destination)
	if err != nil {
		return err
	}
	defer func() {
		if output != nil && err != nil {
			err = errors.Join(err, scrubSQLiteBackupOutput(output))
		}
	}()
	if err = vacuumInto(ctx, db, destination); err != nil {
		return err
	}
	if err = verifyReservedSQLiteBackupOutput(destination, output); err == nil {
		err = output.Chmod(0o600)
	}
	if err == nil {
		err = verifyReservedSQLiteBackupOutput(destination, output)
	}
	if err != nil {
		return err
	}
	if err = output.Sync(); err != nil {
		return err
	}
	if err = verifyReservedSQLiteBackupOutput(destination, output); err != nil {
		return err
	}
	version, err := healthSQLiteBackup(ctx, destination)
	if err != nil {
		return err
	}
	if err = verifyReservedSQLiteBackupOutput(destination, output); err != nil {
		return err
	}
	if version != migrations[len(migrations)-1].version {
		return fmt.Errorf("%w: backup health", ErrCorrupt)
	}
	if err = syncSQLiteBackupDirectory(parent); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	db = nil
	if err = verifyReservedSQLiteBackupOutput(destination, output); err != nil {
		return err
	}
	// After all durability and identity gates pass, Close errors are non-fatal:
	// os.File is invalidated even when Close reports an underlying error, so scrub cannot safely recover.
	_ = closeSQLiteBackupOutput(output)
	output = nil
	return nil
}
func reserveSQLiteBackupOutput(destination string) (*os.File, error) {
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: backup destination", ErrConflict)
	}
	if err = file.Chmod(0o600); err == nil {
		err = verifyPrivateSQLiteBackupOutput(file)
	}
	if err != nil {
		return nil, errors.Join(err, scrubSQLiteBackupOutput(file))
	}
	return file, nil
}
func verifyPrivateSQLiteBackupOutput(file *os.File) error {
	info, err := file.Stat()
	if err != nil || !privateSQLiteBackupOutput(info) {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	return nil
}
func verifyReservedSQLiteBackupOutput(destination string, reserved *os.File) error {
	reservedInfo, err := reserved.Stat()
	if err != nil {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(reservedInfo, info) {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	return nil
}
func scrubSQLiteBackupOutput(file *os.File) error {
	return errors.Join(file.Truncate(0), file.Sync(), file.Close())
}
func validateSQLiteBackupPaths(database, destination string) (string, error) {
	if !filepath.IsAbs(database) || database != filepath.Clean(database) || !filepath.IsAbs(destination) || destination != filepath.Clean(destination) || database == destination {
		return "", fmt.Errorf("%w: backup paths", ErrInvalid)
	}
	parent := filepath.Dir(database)
	if parent != filepath.Dir(destination) {
		return "", fmt.Errorf("%w: backup destination", ErrInvalid)
	}
	if err := rejectSymlink(database); err != nil {
		return "", err
	}
	if err := rejectSymlink(destination); err != nil {
		return "", err
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() || !privateSQLiteBackupParent(parentInfo.Mode()) {
		return "", fmt.Errorf("%w: backup parent", ErrInvalid)
	}
	source, err := os.Lstat(database)
	if err != nil || source.Mode()&os.ModeSymlink != 0 || !source.Mode().IsRegular() {
		return "", fmt.Errorf("%w: backup source", ErrInvalid)
	}
	if _, err := os.Lstat(destination); err == nil || !os.IsNotExist(err) {
		return "", fmt.Errorf("%w: backup destination exists", ErrConflict)
	}
	return parent, nil
}
