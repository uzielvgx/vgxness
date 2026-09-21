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
