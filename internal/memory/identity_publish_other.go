//go:build !windows

package memory

import (
	"fmt"
	"os"
)

func privateSQLiteBackupParent(mode os.FileMode) bool { return mode.Perm()&0o077 == 0 }

func privateSQLiteBackupOutput(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode().Perm() == 0o600
}

func syncProjectMarkerDir(dir string) error {
	return syncProjectDirectory(dir, "project marker directory")
}
func syncProjectMarkerParent(workspace string) error {
	return syncProjectDirectory(workspace, "workspace directory")
}
func syncSQLiteBackupDirectory(dir string) error {
	return syncProjectDirectory(dir, "backup directory")
}
func syncProjectDirectory(path, description string) error {
	dirFile, err := projectMarkerFS.openDir(path)
	if err != nil {
		return fmt.Errorf("%w: open %s", ErrInvalid, description)
	}
	syncErr := dirFile.Sync()
	closeErr := dirFile.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("%w: sync %s", ErrInvalid, description)
	}
	return nil
}
