//go:build linux

package release

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func publishNoReplace(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}

func defaultDurabilityHooks() durabilityHooks {
	return durabilityHooks{syncFile: syncPath, syncDirectory: syncPath, publish: publishNoReplace}
}

func syncPath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

type stagingIdentity struct{ file *os.File }

func captureStagingIdentity(path string) (*stagingIdentity, error) {
	file, err := os.Open(path)
	return &stagingIdentity{file: file}, err
}

func (identity *stagingIdentity) matches(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	staged, err := identity.file.Stat()
	return err == nil && os.SameFile(staged, info), err
}

func (identity *stagingIdentity) close() error { return identity.file.Close() }
