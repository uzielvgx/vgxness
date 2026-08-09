//go:build windows

package selfinstall

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func publishRootDirectoryNoReplace(root *os.Root, source, destination string) error {
	sourcePath, err := syscall.UTF16PtrFromString(filepath.Join(root.Name(), source))
	if err != nil {
		return err
	}
	destinationPath, err := syscall.UTF16PtrFromString(filepath.Join(root.Name(), destination))
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

const moveFileWriteThrough = 0x8
