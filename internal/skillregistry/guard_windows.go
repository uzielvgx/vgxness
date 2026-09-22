//go:build windows

package skillregistry

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// kernelGuard is one held Windows kernel guard. It owns an open descriptor to
// the persistent guard file and the exclusive byte-range lock that a second
// handle, even in the same process, cannot take.
type kernelGuard struct {
	file       *os.File
	overlapped windows.Overlapped
}

// acquireKernelGuardWith opens the persistent guard file through the rooted
// directory handle and takes its exclusive byte-range lock, waiting within the
// shared per-call deadline. The guard file is never removed or truncated, and a
// foreign non-empty, symlink, or non-regular guard is rejected unchanged. An
// open or lock failure other than a lock violation fails closed immediately;
// only ERROR_LOCK_VIOLATION is treated as contention and retried.
func acquireKernelGuardWith(ctx context.Context, root rootedFS, o ops) (*kernelGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pre, preErr := root.Lstat(guardName)
	if preErr == nil {
		if guardInfoInvalid(pre) {
			return nil, ErrInvalid
		}
	} else if !errors.Is(preErr, fs.ErrNotExist) {
		// The guard name is persistent, so it has no delete-pending ambiguity;
		// any metadata failure fails closed immediately rather than being
		// re-observed.
		return nil, invalid(preErr)
	}
	file, err := root.OpenFile(guardName, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		// Open failures, including access-denied, fail closed without retry:
		// only a lock violation is transient contention for the guard.
		return nil, invalid(err)
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() != 0 {
		_ = file.Close()
		return nil, ErrInvalid
	}
	if preErr == nil {
		if !os.SameFile(pre, opened) {
			_ = file.Close()
			return nil, ErrInvalid
		}
	} else {
		// The guard was absent at the precheck; a concurrent create must still
		// resolve to the exact regular, empty file just opened.
		post, lstatErr := root.Lstat(guardName)
		if lstatErr != nil || guardInfoInvalid(post) || !os.SameFile(opened, post) {
			_ = file.Close()
			return nil, ErrInvalid
		}
	}
	guard := &kernelGuard{file: file}
	deadline := o.deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(o.maxWait)
	}
	for {
		lockErr := guard.lock(o)
		if lockErr == nil {
			return guard, nil
		}
		if !errors.Is(lockErr, windows.ERROR_LOCK_VIOLATION) {
			guard.close(o)
			return nil, invalid(lockErr)
		}
		if !time.Now().Before(deadline) {
			guard.close(o)
			return nil, ErrBusy
		}
		select {
		case <-ctx.Done():
			guard.close(o)
			return nil, ctx.Err()
		case <-time.After(o.retryDelay):
		}
	}
}

// guardInfoInvalid reports whether a guard name is a symlink, a directory, a
// non-regular file, or carries foreign bytes. Any such file is left unchanged
// and fails closed.
func guardInfoInvalid(info os.FileInfo) bool {
	if info == nil {
		return true
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return true
	}
	return info.Size() != 0
}

func (guard *kernelGuard) lock(o ops) error {
	if o.guardLock != nil {
		return o.guardLock(guard.file)
	}
	return windows.LockFileEx(windows.Handle(guard.file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &guard.overlapped)
}

func (guard *kernelGuard) close(o ops) {
	if guard == nil || guard.file == nil {
		return
	}
	closeFile := o.guardClose
	if closeFile == nil {
		closeFile = func(file *os.File) error { return file.Close() }
	}
	_ = closeFile(guard.file)
}

// releaseWith drops the kernel lock and closes the held descriptor, never
// touching the persistent guard file itself.
func (guard *kernelGuard) releaseWith(o ops) {
	if guard == nil || guard.file == nil {
		return
	}
	unlock := o.guardUnlock
	if unlock == nil {
		unlock = func(file *os.File) error {
			return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &guard.overlapped)
		}
	}
	_ = unlock(guard.file)
	guard.close(o)
}
