//go:build !windows

package memory

import "fmt"

func syncProjectMarkerDir(dir string) error {
	return syncProjectDirectory(dir, "project marker directory")
}
func syncProjectMarkerParent(workspace string) error {
	return syncProjectDirectory(workspace, "workspace directory")
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
