package memory

import (
	"context"
	"database/sql"
	"errors"
	"github.com/vgxness/vgxness/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCreateSQLiteBackupCreatesVerifiedPrivateSnapshot(t *testing.T) {
	root, database, _ := createBackupFixture(t)
	store, err := Open(context.Background(), database, nil)
	testutil.NoError(t, err)
	_, err = NewMemoryService(store, "test", nil).Remember(context.Background(), Remember{Content: "committed wal value"})
	testutil.NoError(t, err)
	testutil.NoError(t, store.Close())
	backup := filepath.Join(root, "backup.sqlite")
	testutil.NoError(t, CreateSQLiteBackup(context.Background(), database, backup))
	info, err := os.Stat(backup)
	testutil.NoError(t, err)
	testutil.Require(t, privateSQLiteBackupOutput(info), "backup=%v", info)
	db, err := sql.Open("sqlite", backup)
	testutil.NoError(t, err)
	defer db.Close()
	var count int
	testutil.NoError(t, db.QueryRow(`SELECT count(*) FROM observations WHERE content='committed wal value'`).Scan(&count))
	testutil.Require(t, count == 1, "count=%d", count)
	testutil.Require(t, CreateSQLiteBackup(context.Background(), database, backup) != nil, "second backup overwrote destination")
}
func TestCreateSQLiteBackupTerminalCloseErrorIsNotRetryable(t *testing.T) {
	original := closeSQLiteBackupOutput
	defer func() { closeSQLiteBackupOutput = original }()
	closeSentinel := errors.New("terminal close")
	closes := 0
	closeSQLiteBackupOutput = func(file *os.File) error {
		closes++
		_ = file.Close()
		return closeSentinel
	}
	_, database, backup := createBackupFixture(t)
	store, err := Open(context.Background(), database, nil)
	testutil.NoError(t, err)
	_, err = NewMemoryService(store, "test", nil).Remember(context.Background(), Remember{Content: "committed wal value"})
	testutil.NoError(t, err)
	testutil.NoError(t, store.Close())
	testutil.NoError(t, CreateSQLiteBackup(context.Background(), database, backup))
	testutil.Require(t, closes == 1, "close calls=%d", closes)
	info, err := os.Stat(backup)
	testutil.NoError(t, err)
	testutil.Require(t, privateSQLiteBackupOutput(info), "backup=%v", info)
	version, err := HealthFile(context.Background(), backup)
	testutil.NoError(t, err)
	testutil.Require(t, version == migrations[len(migrations)-1].version, "version=%d", version)
	db, err := sql.Open("sqlite", backup)
	testutil.NoError(t, err)
	defer db.Close()
	var count int
	testutil.NoError(t, db.QueryRow(`SELECT count(*) FROM observations WHERE content='committed wal value'`).Scan(&count))
	testutil.Require(t, count == 1, "count=%d", count)
	testutil.Require(t, CreateSQLiteBackup(context.Background(), database, backup) != nil, "backup retried after completion")
}
func TestCreateSQLiteBackupCancelledDoesNotCreateOutput(t *testing.T) {
	_, database, destination := createBackupFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := CreateSQLiteBackup(ctx, database, destination)
	testutil.Require(t, errors.Is(err, context.Canceled), "error=%v", err)
	requireBackupMissing(t, destination)
}
func TestCreateSQLiteBackupVacuumFailureOutputSafety(t *testing.T) {
	original := vacuumInto
	defer func() { vacuumInto = original }()
	for _, swapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "swapped"}[swapped], func(t *testing.T) {
			if swapped && runtime.GOOS == "windows" {
				t.Skip("Windows cannot rename an open reserved backup output")
			}
			root, database, destination := createBackupFixture(t)
			vacuumInto = func(_ context.Context, _ *sql.DB, path string) error {
				if !swapped {
					info, err := os.Stat(path)
					if err != nil || info.Mode().Perm() != 0o600 {
						return errors.New("destination was not private before VACUUM content")
					}
					_ = os.WriteFile(path, []byte("partial"), 0o666)
				} else if err := swapBackupOutput(root, path); err != nil {
					return err
				}
				return errors.New("vacuum failure")
			}
			testutil.Require(t, CreateSQLiteBackup(context.Background(), database, destination) != nil, "VACUUM failure was ignored")
			if swapped {
				contents, err := os.ReadFile(destination)
				testutil.NoError(t, err)
				testutil.Require(t, string(contents) == "replacement", "replacement=%q", contents)
				requireBackupScrubbed(t, filepath.Join(root, "reserved.sqlite"))
			} else {
				requireBackupScrubbed(t, destination)
			}
		})
	}
}
func TestCreateSQLiteBackupRejectsSwapAfterHealthVerification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot rename an open reserved backup output")
	}
	original := healthSQLiteBackup
	defer func() { healthSQLiteBackup = original }()
	root, database, destination := createBackupFixture(t)
	healthSQLiteBackup = func(_ context.Context, path string) (int, error) {
		if err := swapBackupOutput(root, path); err != nil {
			return 0, err
		}
		return migrations[len(migrations)-1].version, nil
	}
	testutil.Require(t, CreateSQLiteBackup(context.Background(), database, destination) != nil, "swap accepted")
	contents, err := os.ReadFile(destination)
	testutil.NoError(t, err)
	testutil.Require(t, string(contents) == "replacement", "replacement=%q", contents)
	requireBackupScrubbed(t, filepath.Join(root, "reserved.sqlite"))
}
func TestCreateSQLiteBackupRemovesOutputAfterDirectoryDurabilityFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows directory sync is explicitly unsupported")
	}
	for _, stage := range []string{"open", "sync", "close"} {
		t.Run(stage, func(t *testing.T) {
			original := projectMarkerFS
			defer func() { projectMarkerFS = original }()
			_, database, destination := createBackupFixture(t)
			projectMarkerFS.openDir = func(path string) (markerDirFile, error) {
				if stage == "open" {
					return nil, errors.New("durability")
				}
				file, err := os.Open(path)
				return markerTestDir{File: file, stage: stage, err: errors.New("durability")}, err
			}
			testutil.Require(t, CreateSQLiteBackup(context.Background(), database, destination) != nil, "directory durability failure was ignored")
			requireBackupScrubbed(t, destination)
		})
	}
}
func TestCreateSQLiteBackupRejectsUnsafePaths(t *testing.T) {
	root, database, _ := createBackupFixture(t)
	link := filepath.Join(root, "linked.db")
	testutil.NoError(t, os.Symlink(database, link))
	for _, source := range []string{filepath.Join(root, "missing.db"), root} {
		requireBackupRejected(t, source, filepath.Join(root, "backup-"+filepath.Base(source)))
	}
	requireBackupRejected(t, database, filepath.Join(t.TempDir(), "backup.sqlite"))
	backup := filepath.Join(root, "backup-link.sqlite")
	testutil.Require(t, CreateSQLiteBackup(context.Background(), link, backup) != nil, "symlink source accepted")
	requireBackupMissing(t, backup)
	testutil.Require(t, privateSQLiteBackupParent(0o700), "private parent rejected")
	if runtime.GOOS != "windows" {
		testutil.Require(t, !privateSQLiteBackupParent(0o755), "public parent accepted")
	}
}
func requireBackupRejected(t *testing.T, source, destination string) {
	err := CreateSQLiteBackup(context.Background(), source, destination)
	testutil.Require(t, errors.Is(err, ErrInvalid), "error=%v", err)
	requireBackupMissing(t, destination)
}
func requireBackupMissing(t *testing.T, path string) {
	_, err := os.Lstat(path)
	testutil.Require(t, os.IsNotExist(err), "backup exists: %v", err)
}
func requireBackupScrubbed(t *testing.T, path string) {
	info, err := os.Stat(path)
	testutil.NoError(t, err)
	testutil.Require(t, info.Size() == 0 && privateSQLiteBackupOutput(info), "backup=%v", info)
}
func swapBackupOutput(root, path string) error {
	if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(path, filepath.Join(root, "reserved.sqlite")); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("replacement"), 0o600)
}
func createBackupDatabase(t *testing.T, root string) string {
	testutil.NoError(t, os.Chmod(root, 0o700))
	database := filepath.Join(root, "memory.db")
	store, err := Open(context.Background(), database, nil)
	testutil.NoError(t, err)
	testutil.NoError(t, store.Close())
	return database
}
func createBackupFixture(t *testing.T) (string, string, string) {
	root, err := filepath.Abs(t.TempDir())
	testutil.NoError(t, err)
	return root, createBackupDatabase(t, root), filepath.Join(root, "backup.sqlite")
}
