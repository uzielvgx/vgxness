//go:build !windows

package chronicle

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"
)

func openNoFollow(path string, flag int, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, perm)
}

func openStateLock(root string) (*os.File, error) {
	return openNoFollow(root, os.O_RDONLY, 0)
}

func lockFile(ctx context.Context, file *os.File, mode fileLockMode) error {
	operation := syscall.LOCK_SH
	if mode == lockExclusive {
		operation = syscall.LOCK_EX
	}
	for {
		err := syscall.Flock(int(file.Fd()), operation|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("lock Chronicle file: %w", err)
		}
		if err := waitForLock(ctx); err != nil {
			return err
		}
	}
}

func unlockFile(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
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

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
