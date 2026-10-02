//go:build windows

package app

import (
	"fmt"
	"os"
)

// writableDirectory only checks that dir exists on Windows: ACLs decide
// writability there, and probing them without writing is not reliable.
func writableDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return nil
}
