//go:build !windows

package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

type heldSlot struct{ file *os.File }

func (s heldSlot) release() {
	if s.file == nil {
		return
	}
	_ = syscall.Flock(int(s.file.Fd()), syscall.LOCK_UN)
	_ = s.file.Close()
}

func ensurePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: unsafe coordination lock directory", ErrInvalidCoordinator)
	}
	return nil
}

func tryLock(path string) (heldSlot, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return heldSlot{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return heldSlot{}, fmt.Errorf("%w: unsafe coordination lock file", ErrInvalidCoordinator)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return heldSlot{}, ErrCoordinatorBusy
		}
		return heldSlot{}, err
	}
	return heldSlot{file: file}, nil
}
