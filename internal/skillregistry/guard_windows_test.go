//go:build windows

package skillregistry

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// tryLockGuardFile probes the guard file with an independent handle and reports
// whether the byte-range lock was free. A held guard makes LockFileEx fail with
// ERROR_LOCK_VIOLATION.
func tryLockGuardFile(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open guard probe: %v", err)
	}
	defer file.Close()
	var overlapped windows.Overlapped
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	switch {
	case err == nil:
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		return true
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return false
	default:
		t.Fatalf("unexpected guard probe error: %v", err)
		return false
	}
}

func TestWindowsKernelGuardMutualExclusion(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held := realOps()
	held.retryDelay = 2 * time.Millisecond
	held.maxWait = time.Second
	first, err := acquireKernelGuardWith(context.Background(), root, held)
	if err != nil {
		t.Fatalf("first guard: %v", err)
	}
	contender := realOps()
	contender.retryDelay = 2 * time.Millisecond
	contender.maxWait = 40 * time.Millisecond
	start := time.Now()
	if _, err := acquireKernelGuardWith(context.Background(), root, contender); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second handle must exhaust to ErrBusy, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("a guarded contender must wait within its bound, elapsed=%s", elapsed)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "skill-registry.lock")); !os.IsNotExist(statErr) {
		t.Fatalf("a guard-blocked contender must not create the inner lock: %v", statErr)
	}
	first.releaseWith(held)
	second, err := acquireKernelGuardWith(context.Background(), root, held)
	if err != nil {
		t.Fatalf("after release the guard must be acquirable: %v", err)
	}
	if tryLockGuardFile(t, filepath.Join(dir, guardName)) {
		t.Fatal("the second handle must hold the guard")
	}
	second.releaseWith(held)
	if !tryLockGuardFile(t, filepath.Join(dir, guardName)) {
		t.Fatal("the guard must be free after release")
	}
	info, statErr := os.Stat(filepath.Join(dir, guardName))
	if statErr != nil || info.Size() != 0 {
		t.Fatalf("the guard file must persist empty: info=%v err=%v", info, statErr)
	}
}

// TestWindowsPublishTakesKernelGuardFirst proves the production publication path
// takes the guard before the inner lock: a blocked guard returns ErrBusy and no
// inner lock or temporary file is created, and publication succeeds after the
// guard is released.
func TestWindowsPublishTakesKernelGuardFirst(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held := realOps()
	held.retryDelay = 2 * time.Millisecond
	held.maxWait = time.Second
	guard, err := acquireKernelGuardWith(context.Background(), root, held)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "skill-registry.json")
	blocked := realOps()
	blocked.retryDelay = 2 * time.Millisecond
	blocked.maxWait = 40 * time.Millisecond
	if err := publishWith(context.Background(), cache, cacheRegistry(dir), blocked); !errors.Is(err, ErrBusy) {
		t.Fatalf("a guard-blocked publish must exhaust to ErrBusy, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "skill-registry.lock")); !os.IsNotExist(statErr) {
		t.Fatalf("a guard-blocked publish must not create the inner lock: %v", statErr)
	}
	assertNoLockOrTemp(t, dir)
	guard.releaseWith(held)
	if err := publishWith(context.Background(), cache, cacheRegistry(dir), realOps()); err != nil {
		t.Fatalf("publish after guard release: %v", err)
	}
	if data, readErr := os.ReadFile(cache); readErr != nil || len(data) == 0 {
		t.Fatalf("the cache must be written after release: %v", readErr)
	}
	assertNoLockOrTemp(t, dir)
	info, statErr := os.Stat(filepath.Join(dir, guardName))
	if statErr != nil || info.Size() != 0 {
		t.Fatalf("the guard file must persist empty: info=%v err=%v", info, statErr)
	}
}

// TestWindowsKernelGuardReleaseOrdering proves the guard is still held while the
// inner lock descriptor closes and is released only after publication returns.
func TestWindowsKernelGuardReleaseOrdering(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	guardPath := filepath.Join(dir, guardName)
	o := realOps()
	o.retryDelay = time.Millisecond
	o.maxWait = time.Second
	heldAtInnerClose := false
	o.lockClose = func(file *os.File) error {
		err := file.Close()
		heldAtInnerClose = !tryLockGuardFile(t, guardPath)
		return err
	}
	if err := publishWith(context.Background(), cache, cacheRegistry(dir), o); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !heldAtInnerClose {
		t.Fatal("the guard must remain held while the inner lock descriptor closes")
	}
	if !tryLockGuardFile(t, guardPath) {
		t.Fatal("the guard must be released after publication returns")
	}
	if _, err := os.Stat(filepath.Join(dir, "skill-registry.lock")); !os.IsNotExist(err) {
		t.Fatalf("the inner lock must be removed: %v", err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".skill-registry-*.tmp")); len(leftovers) != 0 {
		t.Fatalf("no temporary file may remain: %v", leftovers)
	}
	info, err := os.Stat(guardPath)
	if err != nil || info.Size() != 0 {
		t.Fatalf("the guard must persist empty: info=%v err=%v", info, err)
	}
}

func TestWindowsKernelGuardPreCancelledCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireKernelGuardWith(ctx, root, realOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a pre-cancelled guard must not run, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, guardName)); !os.IsNotExist(statErr) {
		t.Fatalf("a pre-cancelled guard must create nothing: %v", statErr)
	}
}

func TestWindowsKernelGuardCancellationClosesHandle(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held := realOps()
	held.retryDelay = 2 * time.Millisecond
	held.maxWait = time.Second
	first, err := acquireKernelGuardWith(context.Background(), root, held)
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	o := realOps()
	o.retryDelay = 2 * time.Millisecond
	o.maxWait = time.Second
	o.guardClose = func(file *os.File) error { closed++; return file.Close() }
	ctx, cancel := context.WithCancel(context.Background())
	mustCancelAfter(t, cancel, 5*time.Millisecond)
	if _, err := acquireKernelGuardWith(ctx, root, o); !errors.Is(err, context.Canceled) {
		t.Fatalf("a waiting guard must honor cancellation, got %v", err)
	}
	if closed != 1 {
		t.Fatalf("a cancelled guard wait must close its handle once, closed=%d", closed)
	}
	first.releaseWith(held)
}

func TestWindowsKernelGuardOpenDeniedFailsClosedOnce(t *testing.T) {
	opened := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist },
		open: func(string, int, os.FileMode) (*os.File, error) {
			opened++
			return nil, errInjectedDenied
		},
	}
	_, err := acquireKernelGuardWith(context.Background(), root, realOps())
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) {
		t.Fatalf("a guard open denial must fail closed preserving cause, got %v", err)
	}
	if errors.Is(err, ErrBusy) {
		t.Fatalf("a guard open denial must not be reported as ErrBusy: %v", err)
	}
	if opened != 1 {
		t.Fatalf("a guard open denial must not be retried, opened=%d", opened)
	}
}

func TestWindowsKernelGuardUnknownLockErrorFailsClosedOnce(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	calls, closed := 0, 0
	o := realOps()
	o.retryDelay = time.Millisecond
	o.maxWait = time.Second
	o.guardLock = func(*os.File) error { calls++; return errInjectedDenied }
	o.guardClose = func(file *os.File) error { closed++; return file.Close() }
	_, err = acquireKernelGuardWith(context.Background(), root, o)
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) {
		t.Fatalf("an unknown guard lock error must fail closed preserving cause, got %v", err)
	}
	if calls != 1 || closed != 1 {
		t.Fatalf("an unknown guard lock error must fail closed once: calls=%d closed=%d", calls, closed)
	}
}

func TestWindowsKernelGuardLockViolationExhaustsToBusy(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	closed := 0
	o := realOps()
	o.retryDelay = time.Millisecond
	o.maxWait = 30 * time.Millisecond
	o.guardLock = func(*os.File) error { return windows.ERROR_LOCK_VIOLATION }
	o.guardClose = func(file *os.File) error { closed++; return file.Close() }
	start := time.Now()
	if _, err := acquireKernelGuardWith(context.Background(), root, o); !errors.Is(err, ErrBusy) {
		t.Fatalf("a persistent lock violation must exhaust to ErrBusy, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the guard wait must stay bounded, elapsed=%s", elapsed)
	}
	if closed != 1 {
		t.Fatalf("an exhausted guard wait must close its handle once, closed=%d", closed)
	}
}

// TestWindowsKernelGuardSharesBudgetWithInnerLock holds the guard briefly and
// keeps an active inner lock for the rest of the publication budget. The guard
// wait and the inner wait must come out of the same maxWait, so the total stays
// near one budget rather than doubling it.
func TestWindowsKernelGuardSharesBudgetWithInnerLock(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held := realOps()
	held.retryDelay = 2 * time.Millisecond
	held.maxWait = time.Second
	guard, err := acquireKernelGuardWith(context.Background(), root, held)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := acquireLock(context.Background(), root, "skill-registry.lock")
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "skill-registry.json")
	budget := 300 * time.Millisecond
	o := realOps()
	o.retryDelay = 2 * time.Millisecond
	o.maxWait = budget
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- publishWith(context.Background(), cache, cacheRegistry(dir), o) }()
	time.Sleep(100 * time.Millisecond)
	guard.releaseWith(held)
	err = <-done
	elapsed := time.Since(start)
	inner.release(root)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("a contended publish must exhaust the shared budget to ErrBusy, got %v", err)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("the shared budget must not collapse early, elapsed=%s", elapsed)
	}
	if elapsed >= 2*budget {
		t.Fatalf("the guard and inner waits must share one budget, elapsed=%s budget=%s", elapsed, budget)
	}
}

func TestWindowsKernelGuardPreservesForeignFiles(t *testing.T) {
	t.Run("nonempty-guard-unchanged", func(t *testing.T) {
		dir := t.TempDir()
		guardPath := filepath.Join(dir, guardName)
		if err := os.WriteFile(guardPath, []byte("foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if _, err := acquireKernelGuardWith(context.Background(), root, realOps()); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a foreign non-empty guard must fail closed, got %v", err)
		}
		if data, readErr := os.ReadFile(guardPath); readErr != nil || string(data) != "foreign" {
			t.Fatalf("a foreign guard must be unchanged: %q err=%v", data, readErr)
		}
	})
	t.Run("directory-rejected", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, guardName), 0o700); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if _, err := acquireKernelGuardWith(context.Background(), root, realOps()); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a non-regular guard must fail closed, got %v", err)
		}
	})
	t.Run("symlink-rejected", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, guardName)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if _, err := acquireKernelGuardWith(context.Background(), root, realOps()); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a symlink guard must fail closed, got %v", err)
		}
	})
	t.Run("existing-guard-reused", func(t *testing.T) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		o := realOps()
		o.retryDelay = time.Millisecond
		o.maxWait = time.Second
		first, err := acquireKernelGuardWith(context.Background(), root, o)
		if err != nil {
			t.Fatal(err)
		}
		first.releaseWith(o)
		second, err := acquireKernelGuardWith(context.Background(), root, o)
		if err != nil {
			t.Fatalf("an existing guard must be reusable: %v", err)
		}
		second.releaseWith(o)
		info, err := os.Stat(filepath.Join(dir, guardName))
		if err != nil || info.Size() != 0 {
			t.Fatalf("a reused guard must stay empty: info=%v err=%v", info, err)
		}
	})
}
