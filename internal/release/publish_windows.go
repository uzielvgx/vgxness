//go:build windows

package release

import (
	"os"
	"syscall"
	"unsafe"
)

func publishNoReplace(source, destination string) error {
	sourcePath, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPath, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")
	result, _, callErr := proc.Call(uintptr(unsafe.Pointer(sourcePath)), uintptr(unsafe.Pointer(destinationPath)), moveFileWriteThrough)
	if result == 0 {
		return callErr
	}
	return nil
}

func defaultDurabilityHooks() durabilityHooks {
	return durabilityHooks{syncFile: func(string) error { return nil }, syncDirectory: func(string) error { return nil }, publish: publishNoReplace}
}

// Keep no directory handle open: MoveFileExW cannot rename a directory while
// a handle without delete sharing remains open.
type stagingIdentity struct{ info os.FileInfo }

func captureStagingIdentity(path string) (*stagingIdentity, error) {
	info, err := os.Stat(path)
	return &stagingIdentity{info: info}, err
}

func (identity *stagingIdentity) matches(path string) (bool, error) {
	info, err := os.Stat(path)
	return err == nil && os.SameFile(identity.info, info), err
}

func (*stagingIdentity) close() error { return nil }

const moveFileWriteThrough = 0x8
