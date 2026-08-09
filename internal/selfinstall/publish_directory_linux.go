//go:build linux

package selfinstall

import (
	"os"

	"golang.org/x/sys/unix"
)

func publishRootDirectoryNoReplace(root *os.Root, source, destination string) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	fd := int(directory.Fd())
	return unix.Renameat2(fd, source, fd, destination, unix.RENAME_NOREPLACE)
}
