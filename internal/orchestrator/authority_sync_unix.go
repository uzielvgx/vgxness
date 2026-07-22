//go:build !windows

package orchestrator

import "os"

func syncAuthorityDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
