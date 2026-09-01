//go:build darwin || linux

package codex

import "os"

func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func syncHeldDirectory(f *os.File) error { return f.Sync() }
