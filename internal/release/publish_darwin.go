//go:build darwin

package release

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func publishNoReplace(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}

func syncFilePath(path string) error      { return syncPath(path) }
func syncDirectoryPath(path string) error { return syncPath(path) }

func syncPath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
