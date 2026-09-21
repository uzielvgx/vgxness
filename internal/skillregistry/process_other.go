//go:build !unix && !windows

package skillregistry

// processAlive is conservative on platforms without a supported liveness probe:
// it never claims an owner is gone, so stale-lock recovery fails closed instead
// of deleting a possibly active lock.
func processAlive(pid int) bool { return true }
