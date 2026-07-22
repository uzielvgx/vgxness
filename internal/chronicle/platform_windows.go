//go:build windows

package chronicle

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

const stateLockName = ".chronicle.lock"

func openNoFollow(path string, flag int, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}

func openStateLock(root string) (*os.File, error) {
	path := filepath.Join(root, stateLockName)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: Chronicle state lock must be a regular file", ErrCorrupt)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Chronicle state lock: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Chronicle state lock: %w", err)
	}
	opened, statErr := file.Stat()
	pathInfo, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !opened.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, pathInfo) {
		file.Close()
		return nil, fmt.Errorf("%w: Chronicle state lock path changed", ErrCorrupt)
	}
	return file, nil
}

func lockFile(ctx context.Context, file *os.File, mode fileLockMode) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if mode == lockExclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	for {
		var overlapped windows.Overlapped
		err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return fmt.Errorf("lock Chronicle file: %w", err)
		}
		if err := waitForLock(ctx); err != nil {
			return err
		}
	}
}

func unlockFile(file *os.File) {
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}

func waitForLock(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Windows does not expose a portable directory fsync equivalent. File writes
// and replacements are flushed before this durability boundary is reached.
func syncDirectory(string) error { return nil }
