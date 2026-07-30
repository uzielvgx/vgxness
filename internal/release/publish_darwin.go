//go:build darwin

package release

import "golang.org/x/sys/unix"

func publishNoReplace(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}
