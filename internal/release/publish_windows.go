//go:build windows

package release

import (
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

// Windows does not provide the directory fsync semantics required by the
// release transition; MoveFileExW requests write-through for the rename.
func syncFilePath(string) error      { return nil }
func syncDirectoryPath(string) error { return nil }

const moveFileWriteThrough = 0x8
