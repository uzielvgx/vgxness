//go:build windows

package skillregistry

import (
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
)

// TestRetryableContentionClassification locks in the narrow classification:
// delete-pending and sharing violations are retried within the bounded wait,
// while access-denied and other real failures are not masked as contention.
func TestRetryableContentionClassification(t *testing.T) {
	retryable := []error{
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_DELETE_PENDING,
		fmt.Errorf("wrapped delete pending: %w", windows.ERROR_DELETE_PENDING),
	}
	for _, err := range retryable {
		if !retryableContention(err) {
			t.Fatalf("transient contention %v must be retryable", err)
		}
	}
	failClosed := []error{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_FILE_NOT_FOUND,
		windows.ERROR_INVALID_PARAMETER,
		errReadSymlink,
	}
	for _, err := range failClosed {
		if retryableContention(err) {
			t.Fatalf("non-contention %v must fail closed", err)
		}
	}
}

// TestAmbiguousLockMetadataClassification locks in the Windows metadata
// classification: access-denied is ambiguous for a lock-metadata observation,
// while sharing, delete-pending, missing, and unrelated errors are not, and
// access-denied stays outside retryableContention so data operations fail
// closed.
func TestAmbiguousLockMetadataClassification(t *testing.T) {
	ambiguous := []error{
		windows.ERROR_ACCESS_DENIED,
		fmt.Errorf("wrapped access denied: %w", windows.ERROR_ACCESS_DENIED),
	}
	for _, err := range ambiguous {
		if !ambiguousLockMetadata(err) {
			t.Fatalf("metadata observation %v must be ambiguous", err)
		}
	}
	for _, err := range []error{
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_DELETE_PENDING,
		windows.ERROR_FILE_NOT_FOUND,
		windows.ERROR_INVALID_PARAMETER,
		errReadSymlink,
	} {
		if ambiguousLockMetadata(err) {
			t.Fatalf("non-metadata %v must not be ambiguous", err)
		}
	}
	if retryableContention(windows.ERROR_ACCESS_DENIED) {
		t.Fatal("access-denied must stay outside retryableContention")
	}
}
