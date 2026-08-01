//go:build !windows

package opencode

import "os"

func directoryDurability() string { return "fsync" }

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
