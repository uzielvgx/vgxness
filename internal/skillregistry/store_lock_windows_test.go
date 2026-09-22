//go:build windows

package skillregistry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestWindowsAmbiguousLockMetadataClassification locks in that access-denied is
// an ambiguous metadata class on Windows while remaining outside the general
// contention classifier, and that unrelated failures are not retried.
func TestWindowsAmbiguousLockMetadataClassification(t *testing.T) {
	if !ambiguousLockMetadata(windows.ERROR_ACCESS_DENIED) {
		t.Fatal("access-denied must be an ambiguous metadata observation")
	}
	if !ambiguousLockMetadata(fmt.Errorf("wrapped: %w", windows.ERROR_ACCESS_DENIED)) {
		t.Fatal("a wrapped access-denied must remain an ambiguous metadata observation")
	}
	for _, err := range []error{
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_DELETE_PENDING,
		windows.ERROR_FILE_NOT_FOUND,
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

// TestWindowsHeldLockRemovalAndMetadataStress exercises the Windows behavior the
// acquisition loop must tolerate: repeated metadata observations of a lock whose
// descriptor is still held, removal of that name while the descriptor is open
// (a delete-pending window), and a normal release. It is repeated enough to
// surface the prior access-denied symptom with bounded synchronization only.
func TestWindowsHeldLockRemovalAndMetadataStress(t *testing.T) {
	for cycle := 0; cycle < 20; cycle++ {
		t.Run(fmt.Sprintf("cycle-%d", cycle), func(t *testing.T) {
			dir := t.TempDir()
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			lock, err := acquireLock(context.Background(), root, "skill-registry.lock")
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			for probe := 0; probe < 8; probe++ {
				if _, err := root.Lstat("skill-registry.lock"); err != nil {
					t.Fatalf("metadata observation %d: %v", probe, err)
				}
			}
			if err := os.Remove(filepath.Join(dir, "skill-registry.lock")); err != nil {
				t.Fatalf("remove held lock: %v", err)
			}
			lock.release(root)
		})
	}
}

// TestWindowsConcurrentRefreshStress repeats the concurrent refresh journey
// enough times to expose the prior intermittent access-denied symptom on
// Windows CI. It reuses the shared registry test helpers and stays bounded, so
// the package suite remains fast.
func TestWindowsConcurrentRefreshStress(t *testing.T) {
	for cycle := 0; cycle < 12; cycle++ {
		t.Run(fmt.Sprintf("cycle-%d", cycle), func(t *testing.T) {
			options, workspace, _ := baseOptions(t)
			writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
			service := New()
			var waitGroup sync.WaitGroup
			errs := make([]error, 6)
			for index := range errs {
				waitGroup.Add(1)
				go func(slot int) {
					defer waitGroup.Done()
					_, errs[slot] = service.Refresh(context.Background(), options)
				}(index)
			}
			waitGroup.Wait()
			for _, err := range errs {
				if err != nil {
					t.Fatalf("concurrent refresh: %v", err)
				}
			}
		})
	}
}

// TestWindowsRemoveDeniedIsNotReobserved exercises the provenance fix with the
// real Windows errno: a remove failure carrying ERROR_ACCESS_DENIED must fail
// immediately and must not be re-observed as an ambiguous metadata probe, even
// though the same errno is ambiguous for an Lstat observation.
func TestWindowsRemoveDeniedIsNotReobserved(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lockPath := filepath.Join(dir, "skill-registry.lock")
	if err := os.WriteFile(lockPath, []byte("1073741824"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleLockAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	removes := 0
	o := realOps()
	o.retryDelay = time.Millisecond
	o.maxWait = 40 * time.Millisecond
	o.remove = func(rootedFS, string) error {
		removes++
		return windows.ERROR_ACCESS_DENIED
	}
	recovered, err := recoverStaleLockBounded(context.Background(), root, "skill-registry.lock", o)
	if recovered || !errors.Is(err, ErrInvalid) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("remove access-denied must fail closed preserving cause: recovered=%t err=%v", recovered, err)
	}
	if removes != 1 {
		t.Fatalf("remove access-denied must not be re-observed, removes=%d", removes)
	}
}
