//go:build !unix

package skillregistry

// processAlive is conservative off Unix: it never claims an owner is gone, so
// stale-lock recovery fails closed instead of deleting a possibly active lock.
func processAlive(pid int) bool { return true }
