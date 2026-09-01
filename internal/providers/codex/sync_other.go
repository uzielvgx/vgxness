//go:build !darwin && !linux && !windows

package codex

import "os"

// Unsupported platforms fail closed in ownership_other before sync is reachable.
func syncPath(string) error { return nil }

func syncHeldDirectory(*os.File) error { return nil }
