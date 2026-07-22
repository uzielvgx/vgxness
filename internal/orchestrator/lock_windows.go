//go:build windows

package orchestrator

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type heldSlot struct {
	file       *os.File
	overlapped windows.Overlapped
}

func (s heldSlot) release() {
	if s.file == nil {
		return
	}
	_ = windows.UnlockFileEx(windows.Handle(s.file.Fd()), 0, 1, 0, &s.overlapped)
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
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: unsafe coordination lock directory", ErrInvalidCoordinator)
	}
	return nil
}

func tryLock(path string) (heldSlot, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return heldSlot{}, fmt.Errorf("%w: unsafe coordination lock file", ErrInvalidCoordinator)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return heldSlot{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return heldSlot{}, err
	}
	opened, statErr := file.Stat()
	pathInfo, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !opened.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, pathInfo) {
		_ = file.Close()
		return heldSlot{}, fmt.Errorf("%w: unsafe coordination lock file", ErrInvalidCoordinator)
	}
	slot := heldSlot{file: file}
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &slot.overlapped)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return heldSlot{}, ErrCoordinatorBusy
		}
		return heldSlot{}, err
	}
	return slot, nil
}
