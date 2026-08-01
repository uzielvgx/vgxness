//go:build !windows

package opencode

import "os"

func openDeleteSharingWriter(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY, 0)
}
