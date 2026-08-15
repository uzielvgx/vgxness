//go:build windows

package runtime

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

type syncEnrollmentFileLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func acquireSyncEnrollmentFile(ctx context.Context, file *os.File) (*syncEnrollmentFileLock, error) {
	lock := &syncEnrollmentFileLock{file: file}
	for {
		err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (lock *syncEnrollmentFileLock) release() {
	if lock != nil && lock.file != nil {
		_ = windows.UnlockFileEx(windows.Handle(lock.file.Fd()), 0, 1, 0, &lock.overlapped)
		_ = lock.file.Close()
	}
}
