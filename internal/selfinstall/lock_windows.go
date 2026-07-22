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

func acquire(ctx context.Context, path string) (*installLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	lock := &installLock{file: file}
	for {
		err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
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
