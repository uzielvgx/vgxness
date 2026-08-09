//go:build windows

package selfinstall

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

type installLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func acquireFile(ctx context.Context, file *os.File) (*installLock, error) {
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return nil, err
	}
	lock := &installLock{file: file}
	for {
		err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (lock *installLock) release() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = windows.UnlockFileEx(windows.Handle(lock.file.Fd()), 0, 1, 0, &lock.overlapped)
	_ = lock.file.Close()
}
