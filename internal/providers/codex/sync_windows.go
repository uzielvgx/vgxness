//go:build windows

package codex

// Windows flushes each regular file before publication. Directory namespace
// flushing is unavailable, so lifecycle results label this best-effort.
func syncPath(string) error { return nil }
