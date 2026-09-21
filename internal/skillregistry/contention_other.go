//go:build !windows

package skillregistry

// retryableContention is false off Windows: exclusive-create contention is
// already covered by the fs.ErrExist wait path, and every other error is a real
// failure that must fail closed.
func retryableContention(error) bool { return false }
