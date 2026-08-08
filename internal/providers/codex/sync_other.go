//go:build !darwin && !linux && !windows

package codex

// Unsupported platforms fail closed in ownership_other before sync is reachable.
func syncPath(string) error { return nil }
