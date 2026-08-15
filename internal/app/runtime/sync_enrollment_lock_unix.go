//go:build !windows

package runtime

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

type syncEnrollmentFileLock struct{ file *os.File }

func acquireSyncEnrollmentFile(ctx context.Context, file *os.File) (*syncEnrollmentFileLock, error) {
	for {
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return &syncEnrollmentFileLock{file: file}, nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
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
		_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
		_ = lock.file.Close()
	}
}
