package skillregistry

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// errInjectedTransient stands in for the Windows delete-pending/sharing class so
// the loop-level classification is deterministic on every platform without
// claiming the exact Windows root cause.
var errInjectedTransient = errors.New("injected transient contention")

// transientOps classifies only errInjectedTransient as retryable and uses a
// short bound so exhaustion and cancellation tests stay deterministic.
func transientOps() ops {
	o := realOps()
	o.contention = func(err error) bool { return errors.Is(err, errInjectedTransient) }
	o.retryDelay = time.Millisecond
	o.maxWait = 40 * time.Millisecond
	return o
}

type fakeRoot struct {
	lstat  func(name string) (os.FileInfo, error)
	open   func(name string, flag int, perm os.FileMode) (*os.File, error)
	remove func(name string) error
	rename func(oldname, newname string) error
}

func (f fakeRoot) Lstat(name string) (os.FileInfo, error) {
	if f.lstat == nil {
		return nil, fs.ErrNotExist
	}
	return f.lstat(name)
}

func (f fakeRoot) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if f.open == nil {
		return nil, errors.New("unexpected open")
	}
	return f.open(name, flag, perm)
}

func (f fakeRoot) Remove(name string) error {
	if f.remove == nil {
		return nil
	}
	return f.remove(name)
}

func (f fakeRoot) Rename(oldname, newname string) error {
	if f.rename == nil {
		return nil
	}
	return f.rename(oldname, newname)
}

type fakeInfo struct {
	mode os.FileMode
	mod  time.Time
	size int64
}

func (f fakeInfo) Name() string       { return "skill-registry.lock" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() os.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return f.mod }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

func regularLockInfo() os.FileInfo { return fakeInfo{mode: 0o600, mod: time.Now()} }

func mustCancelAfter(t *testing.T, cancel context.CancelFunc, delay time.Duration) {
	t.Helper()
	go func() {
		time.Sleep(delay)
		cancel()
	}()
}

func assertNoLockOrTemp(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "skill-registry.lock")); !os.IsNotExist(err) {
		t.Fatalf("lock must be absent: %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".skill-registry-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("no temporary files may remain: %v", leftovers)
	}
}

func TestAcquireLockTransientPrecheckWaitsThenCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	opened := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) { return nil, errInjectedTransient },
		open: func(string, int, os.FileMode) (*os.File, error) {
			opened++
			return nil, errInjectedTransient
		},
	}
	mustCancelAfter(t, cancel, 5*time.Millisecond)
	if _, err := acquireLockWith(ctx, root, "skill-registry.lock", transientOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a transient precheck stat must wait for cancellation, got %v", err)
	}
	if opened != 0 {
		t.Fatalf("a transient precheck must not attempt to create the lock (opened=%d)", opened)
	}
}

func TestAcquireLockTransientOpenWaitsThenCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist },
		open:  func(string, int, os.FileMode) (*os.File, error) { return nil, errInjectedTransient },
	}
	mustCancelAfter(t, cancel, 5*time.Millisecond)
	if _, err := acquireLockWith(ctx, root, "skill-registry.lock", transientOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a transient open must wait for cancellation, got %v", err)
	}
}

func TestAcquireLockTransientStatAfterOpenWaitsThenCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) {
			calls++
			if calls <= 1 {
				return nil, fs.ErrNotExist
			}
			return nil, errInjectedTransient
		},
		open: func(string, int, os.FileMode) (*os.File, error) { return nil, fs.ErrExist },
	}
	mustCancelAfter(t, cancel, 5*time.Millisecond)
	if _, err := acquireLockWith(ctx, root, "skill-registry.lock", transientOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a transient post-open stat must wait for cancellation, got %v", err)
	}
}

func TestAcquireLockTransientRecoveryWaitsThenCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) {
			calls++
			if calls <= 2 {
				return regularLockInfo(), nil
			}
			return nil, errInjectedTransient
		},
		open: func(string, int, os.FileMode) (*os.File, error) { return nil, fs.ErrExist },
	}
	mustCancelAfter(t, cancel, 5*time.Millisecond)
	if _, err := acquireLockWith(ctx, root, "skill-registry.lock", transientOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a transient recovery stat must wait for cancellation, got %v", err)
	}
}

func TestAcquireLockExhaustsToBusy(t *testing.T) {
	o := transientOps()
	o.maxWait = 5 * time.Millisecond
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist },
		open:  func(string, int, os.FileMode) (*os.File, error) { return nil, fs.ErrExist },
	}
	if _, err := acquireLockWith(context.Background(), root, "skill-registry.lock", o); !errors.Is(err, ErrBusy) {
		t.Fatalf("contention must exhaust to ErrBusy, got %v", err)
	}
}

func TestAcquireLockUnknownOpenErrorFailsClosedImmediately(t *testing.T) {
	sentinel := errors.New("injected permission failure")
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist },
		open:  func(string, int, os.FileMode) (*os.File, error) { return nil, sentinel },
	}
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", transientOps())
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, sentinel) {
		t.Fatalf("unknown open error must fail closed preserving cause, got %v", err)
	}
}

func TestAcquireLockUnknownStatErrorFailsClosedImmediately(t *testing.T) {
	sentinel := errors.New("injected stat failure")
	calls := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) {
			calls++
			if calls <= 1 {
				return nil, fs.ErrNotExist
			}
			return nil, sentinel
		},
		open: func(string, int, os.FileMode) (*os.File, error) { return nil, fs.ErrExist },
	}
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", transientOps())
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, sentinel) {
		t.Fatalf("unknown stat error must fail closed preserving cause, got %v", err)
	}
}

func TestAcquireLockPreCancelledCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireLock(ctx, root, "skill-registry.lock"); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled acquire must not run, got %v", err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestAcquireLockWaitingDoesNotDeleteForeignLock(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lockPath := filepath.Join(dir, "skill-registry.lock")
	if err := os.WriteFile(lockPath, []byte("foreign-owner"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	o := realOps()
	o.retryDelay = time.Millisecond
	o.maxWait = 200 * time.Millisecond
	mustCancelAfter(t, cancel, 10*time.Millisecond)
	if _, err := acquireLockWith(ctx, root, "skill-registry.lock", o); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting must end on cancellation, got %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil || string(data) != "foreign-owner" {
		t.Fatalf("a foreign lock must survive: %q err=%v", data, err)
	}
}

func TestFinalizeLockWriteFailureDiscardsOwnLockAndCloses(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	o := realOps()
	closed := 0
	o.lockWrite = func(*os.File, []byte) (int, error) { return 0, errors.New("write boom") }
	o.lockClose = func(file *os.File) error { closed++; return file.Close() }
	if _, err := acquireLockWith(context.Background(), root, "skill-registry.lock", o); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed lock write must be ErrInvalid, got %v", err)
	}
	assertNoLockOrTemp(t, dir)
	if closed != 1 {
		t.Fatalf("failed acquisition must close its descriptor once, closed=%d", closed)
	}
}

func TestFinalizeLockWriteFailurePreservesReplacement(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lockPath := filepath.Join(dir, "skill-registry.lock")
	o := realOps()
	o.lockWrite = func(*os.File, []byte) (int, error) {
		if err := os.Remove(lockPath); err != nil {
			t.Errorf("remove: %v", err)
		}
		if err := os.WriteFile(lockPath, []byte("replacement"), 0o600); err != nil {
			t.Errorf("write replacement: %v", err)
		}
		return 0, errors.New("write boom")
	}
	o.lockClose = func(file *os.File) error { return file.Close() }
	if _, err := acquireLockWith(context.Background(), root, "skill-registry.lock", o); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed lock write must be ErrInvalid, got %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("cleanup must not delete a replacement lock: %q err=%v", data, err)
	}
}

func TestFinalizeLockSyncFailureDiscardsOwnLock(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	o := realOps()
	o.lockSync = func(*os.File) error { return errors.New("sync boom") }
	if _, err := acquireLockWith(context.Background(), root, "skill-registry.lock", o); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed lock sync must be ErrInvalid, got %v", err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestLockReleaseClosesDescriptor(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := acquireLock(context.Background(), root, "skill-registry.lock")
	if err != nil {
		t.Fatal(err)
	}
	o := realOps()
	closed := 0
	o.lockClose = func(file *os.File) error { closed++; return file.Close() }
	lock.releaseWith(root, o)
	if closed != 1 {
		t.Fatalf("release must close the held descriptor once, closed=%d", closed)
	}
	if lock.file.Fd() != ^uintptr(0) {
		t.Fatal("the release descriptor must be closed after release")
	}
}

func cacheRegistry(dir string) Registry {
	return Registry{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Workspace:     dir,
		Complete:      true,
	}
}

func TestPublishTempWriteFailurePreservesCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	if err := os.WriteFile(cache, []byte("old-cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := realOps()
	o.tempWrite = func(*os.File, []byte) (int, error) { return 0, errors.New("write boom") }
	if err := publishWith(context.Background(), cache, cacheRegistry(dir), o); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed temp write must be ErrInvalid, got %v", err)
	}
	data, err := os.ReadFile(cache)
	if err != nil || string(data) != "old-cache" {
		t.Fatalf("previous cache must be intact: %q err=%v", data, err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestPublishTempSyncFailurePreservesCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	if err := os.WriteFile(cache, []byte("old-cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := realOps()
	o.tempSync = func(*os.File) error { return errors.New("sync boom") }
	if err := publishWith(context.Background(), cache, cacheRegistry(dir), o); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed temp sync must be ErrInvalid, got %v", err)
	}
	if data, err := os.ReadFile(cache); err != nil || string(data) != "old-cache" {
		t.Fatalf("previous cache must be intact: %q err=%v", data, err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestPublishTempCloseFailureRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	if err := os.WriteFile(cache, []byte("old-cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := realOps()
	o.tempClose = func(file *os.File) error {
		_ = file.Close()
		return errors.New("close boom")
	}
	if err := publishWith(context.Background(), cache, cacheRegistry(dir), o); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed temp close must be ErrInvalid, got %v", err)
	}
	if data, err := os.ReadFile(cache); err != nil || string(data) != "old-cache" {
		t.Fatalf("previous cache must be intact: %q err=%v", data, err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestPublishRenameFailurePreservesCacheAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	if err := os.WriteFile(cache, []byte("old-cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := realOps()
	o.rename = func(rootedFS, string, string) error { return errors.New("rename boom") }
	if err := publishWith(context.Background(), cache, cacheRegistry(dir), o); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed rename must be ErrInvalid, got %v", err)
	}
	if data, err := os.ReadFile(cache); err != nil || string(data) != "old-cache" {
		t.Fatalf("previous cache must be intact: %q err=%v", data, err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestPublishPreCancelledPreservesExistingCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	if err := os.WriteFile(cache, []byte("old-cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publishWith(ctx, cache, cacheRegistry(dir), realOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled publish must not run, got %v", err)
	}
	if data, err := os.ReadFile(cache); err != nil || string(data) != "old-cache" {
		t.Fatalf("previous cache must be intact: %q err=%v", data, err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestPublishPreCancelledAbsentCacheStaysAbsent(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publishWith(ctx, cache, cacheRegistry(dir), realOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled publish must not run, got %v", err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("absent cache must stay absent: %v", err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestPublishCancelBeforeRenamePreservesCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "skill-registry.json")
	if err := os.WriteFile(cache, []byte("old-cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	o := realOps()
	o.tempClose = func(file *os.File) error {
		err := file.Close()
		cancel()
		return err
	}
	if err := publishWith(ctx, cache, cacheRegistry(dir), o); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation before the staged publication boundary must win, got %v", err)
	}
	if data, err := os.ReadFile(cache); err != nil || string(data) != "old-cache" {
		t.Fatalf("previous cache must be intact: %q err=%v", data, err)
	}
	assertNoLockOrTemp(t, dir)
}

func TestRecoverStaleLockTransientAndUnknown(t *testing.T) {
	removed, err := recoverStaleLockWith(fakeRoot{lstat: func(string) (os.FileInfo, error) {
		return nil, errInjectedTransient
	}}, "skill-registry.lock", transientOps())
	if removed || err != nil {
		t.Fatalf("transient recovery stat must yield false,nil: removed=%t err=%v", removed, err)
	}
	sentinel := errors.New("injected recovery failure")
	removed, err = recoverStaleLockWith(fakeRoot{lstat: func(string) (os.FileInfo, error) {
		return nil, sentinel
	}}, "skill-registry.lock", realOps())
	if removed || !errors.Is(err, ErrInvalid) || !errors.Is(err, sentinel) {
		t.Fatalf("unknown recovery stat must fail closed preserving cause: removed=%t err=%v", removed, err)
	}
}

// errInjectedDenied stands in for the Windows access-denied metadata class so the
// bounded re-observation path is deterministic on every platform without
// claiming the exact Windows root cause.
var errInjectedDenied = errors.New("injected access-denied metadata")

// ambiguousOps classifies only errInjectedTransient as retryable contention and
// only errInjectedDenied as an ambiguous metadata observation, with a short
// bound so exhaustion, re-observation, and cancellation tests stay
// deterministic. It uses the per-call ops seam, never a mutable global.
func ambiguousOps() ops {
	o := realOps()
	o.contention = func(err error) bool { return errors.Is(err, errInjectedTransient) }
	o.metadataAmbiguous = func(err error) bool { return errors.Is(err, errInjectedDenied) }
	o.retryDelay = time.Millisecond
	o.maxWait = 40 * time.Millisecond
	return o
}

func TestAcquireLockAmbiguousMetadataReobservedThenSucceeds(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "skill-registry.lock")
	calls := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) {
			calls++
			if calls == 1 {
				return nil, errInjectedDenied
			}
			return nil, fs.ErrNotExist
		},
		open: func(name string, flag int, perm os.FileMode) (*os.File, error) {
			return os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		},
	}
	lock, err := acquireLockWith(context.Background(), root, "skill-registry.lock", ambiguousOps())
	if err != nil {
		t.Fatalf("a re-observed metadata denial must not fail the acquisition: %v", err)
	}
	if lock == nil || lock.file == nil {
		t.Fatal("acquisition must return a held lock")
	}
	if calls < 2 {
		t.Fatalf("the denied observation must be re-observed, calls=%d", calls)
	}
	_ = lock.file.Close()
}

func TestAcquireLockPersistentAmbiguousMetadataFailsInvalidNotBusy(t *testing.T) {
	opened := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) { return nil, errInjectedDenied },
		open: func(string, int, os.FileMode) (*os.File, error) {
			opened++
			return nil, errInjectedDenied
		},
	}
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", ambiguousOps())
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) {
		t.Fatalf("a persistent metadata denial must fail closed preserving cause, got %v", err)
	}
	if errors.Is(err, ErrBusy) {
		t.Fatalf("a persistent metadata denial must not be reported as ErrBusy: %v", err)
	}
	if opened != 0 {
		t.Fatalf("no create may follow a denied metadata probe (opened=%d)", opened)
	}
}

func TestAcquireLockAmbiguousMetadataWaitsThenCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := fakeRoot{lstat: func(string) (os.FileInfo, error) { return nil, errInjectedDenied }}
	mustCancelAfter(t, cancel, 5*time.Millisecond)
	if _, err := acquireLockWith(ctx, root, "skill-registry.lock", ambiguousOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("an ambiguous metadata probe must wait for cancellation, got %v", err)
	}
}

func TestAcquireLockAmbiguousPostOpenMetadataFailsInvalid(t *testing.T) {
	calls := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) {
			calls++
			if calls == 1 {
				return nil, fs.ErrNotExist
			}
			return nil, errInjectedDenied
		},
		open: func(string, int, os.FileMode) (*os.File, error) { return nil, fs.ErrExist },
	}
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", ambiguousOps())
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) || errors.Is(err, ErrBusy) {
		t.Fatalf("a persistent post-open metadata denial must be ErrInvalid with cause, got %v", err)
	}
}

func TestAcquireLockAmbiguousRecoveryMetadataFailsInvalid(t *testing.T) {
	calls := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) {
			calls++
			switch calls % 3 {
			case 1:
				return nil, fs.ErrNotExist
			case 2:
				return regularLockInfo(), nil
			default:
				return nil, errInjectedDenied
			}
		},
		open: func(string, int, os.FileMode) (*os.File, error) { return nil, fs.ErrExist },
	}
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", ambiguousOps())
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) || errors.Is(err, ErrBusy) {
		t.Fatalf("a persistent recovery metadata denial must be ErrInvalid with cause, got %v", err)
	}
}

func TestAcquireLockAmbiguousMetadataSharesOneDeadline(t *testing.T) {
	o := ambiguousOps()
	o.maxWait = 30 * time.Millisecond
	root := fakeRoot{lstat: func(string) (os.FileInfo, error) { return nil, errInjectedDenied }}
	start := time.Now()
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", o)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("exhausted ambiguous metadata must be ErrInvalid, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("re-observation must share the acquisition deadline, elapsed=%s", elapsed)
	}
}

func TestAcquireLockSymlinkMetadataStillRejected(t *testing.T) {
	opened := 0
	root := fakeRoot{
		lstat: func(string) (os.FileInfo, error) {
			return fakeInfo{mode: os.ModeSymlink | 0o777, mod: time.Now()}, nil
		},
		open: func(string, int, os.FileMode) (*os.File, error) {
			opened++
			return nil, errors.New("must not open a symlink")
		},
	}
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", ambiguousOps())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("a symlink lock path must stay rejected, got %v", err)
	}
	if opened != 0 {
		t.Fatalf("a symlink lock path must not be opened (opened=%d)", opened)
	}
}

func TestRecoverStaleLockBoundedPersistentAmbiguousPreservesForeignBytes(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "skill-registry.lock")
	if err := os.WriteFile(lockPath, []byte("foreign-owner"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := fakeRoot{
		lstat:  func(string) (os.FileInfo, error) { return nil, errInjectedDenied },
		remove: func(name string) error { return os.Remove(filepath.Join(dir, name)) },
	}
	recovered, err := recoverStaleLockBounded(context.Background(), root, "skill-registry.lock", ambiguousOps())
	if recovered || !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) {
		t.Fatalf("a persistent recovery denial must fail closed preserving cause: recovered=%t err=%v", recovered, err)
	}
	if data, readErr := os.ReadFile(lockPath); readErr != nil || string(data) != "foreign-owner" {
		t.Fatalf("a denied probe must not mutate the lock: %q err=%v", data, readErr)
	}
}

func TestRecoverStaleLockBoundedAmbiguousThenVanishes(t *testing.T) {
	calls := 0
	root := fakeRoot{lstat: func(string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return nil, errInjectedDenied
		}
		return nil, fs.ErrNotExist
	}}
	recovered, err := recoverStaleLockBounded(context.Background(), root, "skill-registry.lock", ambiguousOps())
	if recovered || err != nil {
		t.Fatalf("a denial that clears to absence must yield false,nil: recovered=%t err=%v", recovered, err)
	}
}

func TestRecoverStaleLockBoundedCancelsWhileProbing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := fakeRoot{lstat: func(string) (os.FileInfo, error) { return nil, errInjectedDenied }}
	mustCancelAfter(t, cancel, 5*time.Millisecond)
	if _, err := recoverStaleLockBounded(ctx, root, "skill-registry.lock", ambiguousOps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a recovery probe must honor cancellation, got %v", err)
	}
}

// staleLockRoot writes a lock owned by a dead PID with an old mtime and returns
// its rooted directory handle and on-disk path. It lets the remove-denial
// regressions reach the mutation stage deterministically on every platform.
func staleLockRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	lockPath := filepath.Join(dir, "skill-registry.lock")
	if err := os.WriteFile(lockPath, []byte("1073741824"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleLockAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	return root, lockPath
}

// removeDeniedOps classifies errInjectedDenied as the ambiguous metadata class,
// exactly as the Windows classifier does for access-denied, while making every
// remove fail with that same sentinel. If the loops re-observed on an errno
// match rather than on typed metadata provenance, a remove denial would be
// retried as if it were a metadata probe.
func removeDeniedOps(removes *int) ops {
	o := realOps()
	o.contention = func(err error) bool { return errors.Is(err, errInjectedTransient) }
	o.metadataAmbiguous = func(err error) bool { return errors.Is(err, errInjectedDenied) }
	o.retryDelay = time.Millisecond
	o.maxWait = 40 * time.Millisecond
	o.remove = func(rootedFS, string) error {
		*removes++
		return errInjectedDenied
	}
	return o
}

func TestAcquireLockRemoveDeniedIsNotReobserved(t *testing.T) {
	root, lockPath := staleLockRoot(t)
	removes := 0
	_, err := acquireLockWith(context.Background(), root, "skill-registry.lock", removeDeniedOps(&removes))
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) {
		t.Fatalf("a remove denial must fail closed preserving cause, got %v", err)
	}
	if errors.Is(err, ErrBusy) {
		t.Fatalf("a remove denial must not be re-observed as ambiguous metadata: %v", err)
	}
	if removes != 1 {
		t.Fatalf("a remove denial must not be retried, removes=%d", removes)
	}
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("a denied remove must not delete the lock: %v", statErr)
	}
}

func TestRecoverStaleLockBoundedRemoveDeniedIsImmediate(t *testing.T) {
	root, lockPath := staleLockRoot(t)
	removes := 0
	recovered, err := recoverStaleLockBounded(context.Background(), root, "skill-registry.lock", removeDeniedOps(&removes))
	if recovered || !errors.Is(err, ErrInvalid) || !errors.Is(err, errInjectedDenied) {
		t.Fatalf("a remove denial must fail closed preserving cause: recovered=%t err=%v", recovered, err)
	}
	if removes != 1 {
		t.Fatalf("a remove denial must not be retried, removes=%d", removes)
	}
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("a denied remove must not delete the lock: %v", statErr)
	}
}

func TestRecoverStaleLockBoundedUnknownRemoveFailsImmediately(t *testing.T) {
	root, _ := staleLockRoot(t)
	sentinel := errors.New("injected remove failure")
	removes := 0
	o := realOps()
	o.retryDelay = time.Millisecond
	o.maxWait = 40 * time.Millisecond
	o.remove = func(rootedFS, string) error {
		removes++
		return sentinel
	}
	recovered, err := recoverStaleLockBounded(context.Background(), root, "skill-registry.lock", o)
	if recovered || !errors.Is(err, ErrInvalid) || !errors.Is(err, sentinel) || removes != 1 {
		t.Fatalf("an unknown remove failure must fail closed once: recovered=%t removes=%d err=%v", recovered, removes, err)
	}
}
