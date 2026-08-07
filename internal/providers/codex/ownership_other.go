//go:build !darwin && !linux

package codex

import "os"

func owned(os.FileInfo) bool { return false }
