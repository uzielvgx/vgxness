//go:build windows

package skillregistry

import (
	"errors"

	"golang.org/x/sys/windows"
)

// retryableContention reports whether an exclusive lock-create failure is a
// transient Windows contention condition that the bounded acquire loop may
// retry. It is deliberately narrow: a sharing violation or delete-pending
// result from a concurrent remove/recreate is retried within the existing wait
// bound, while access-denied, path, symlink, and unknown errors fail closed as
// ErrInvalid. Access-denied is not retried, because it can equally mean a real
// permission problem that must not be masked.
func retryableContention(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_DELETE_PENDING)
}

// ambiguousLockMetadata reports whether a lock-metadata observation failure is
// ambiguous on Windows. An access-denied result from Lstat on the known
// publication-lock name can be a transient artifact of a concurrent
// unlink/recreate, for example while a previous holder's unlink is pending,
// rather than a real permission problem; acquisition and recovery therefore
// re-observe it within their existing bound instead of failing closed at once.
// It is deliberately separate from retryableContention: access-denied is never
// retried for open, remove, read, or write, and a metadata denial on its own
// does not prove contention or staleness.
func ambiguousLockMetadata(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
