//go:build !windows

package skillregistry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestNonWindowsGuardIsNoop locks in that no guard file is ever created off
// Windows and that the no-op guard still honors context cancellation.
func TestNonWindowsGuardIsNoop(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	if err := publish(context.Background(), cache, cacheRegistry(dir)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, guardName)); !os.IsNotExist(err) {
		t.Fatalf("non-Windows publication must not create a guard file: %v", err)
	}
	guard, err := acquireKernelGuardWith(context.Background(), nil, realOps())
	if err != nil || guard == nil {
		t.Fatalf("the non-Windows guard must be a no-op: guard=%v err=%v", guard, err)
	}
	guard.releaseWith(realOps())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireKernelGuardWith(ctx, nil, realOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("the no-op guard must preserve context cancellation, got %v", err)
	}
}

// TestNonWindowsUnlockMakesNoGuard confirms manual Unlock keeps its prior
// false,nil nothing-to-recover result and its context propagation without
// creating guard state.
func TestNonWindowsUnlockMakesNoGuard(t *testing.T) {
	options, _, _ := baseOptions(t)
	if err := os.MkdirAll(filepath.Dir(options.CachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	recovered, err := New().Unlock(context.Background(), options)
	if err != nil || recovered {
		t.Fatalf("nothing-to-recover unlock must be false,nil: recovered=%t err=%v", recovered, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(options.CachePath), guardName)); !os.IsNotExist(err) {
		t.Fatalf("non-Windows unlock must not create a guard file: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Unlock(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("unlock must propagate context cancellation, got %v", err)
	}
}
