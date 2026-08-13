//go:build linux

package release

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func publishNoReplace(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}

func defaultDurabilityHooks() durabilityHooks {
	return durabilityHooks{syncPath, syncPath, publishNoReplace}
}
func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

type stagingIdentity struct{ file *os.File }

func captureStagingIdentity(path string) (*stagingIdentity, error) {
	f, err := os.Open(path)
	return &stagingIdentity{f}, err
}
func (i *stagingIdentity) matches(path string) (bool, error) {
	got, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	want, err := i.file.Stat()
	return err == nil && os.SameFile(want, got), err
}
func (i *stagingIdentity) close() error { return i.file.Close() }
