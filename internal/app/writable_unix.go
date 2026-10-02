//go:build !windows

package app

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// writableDirectory reports whether the current user could create files in
// dir, using access(2) so the check itself writes nothing.
func writableDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return unix.Access(dir, unix.W_OK)
}
