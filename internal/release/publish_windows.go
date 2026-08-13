//go:build windows

package release

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
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
	return durabilityHooks{
		syncFile:      func(string) error { return nil },
		syncDirectory: func(string) error { return nil },
		publish:       publishNoReplace,
	}
}

type stagingIdentity struct {
	handle windows.Handle
	info   windows.ByHandleFileInformation
}

func captureStagingIdentity(path string) (*stagingIdentity, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	identity := &stagingIdentity{handle: handle}
	if err := windows.GetFileInformationByHandle(handle, &identity.info); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return identity, nil
}

func (identity *stagingIdentity) matches(path string) (bool, error) {
	other, err := captureStagingIdentity(path)
	if err != nil {
		return false, err
	}
	defer other.close()
	return identity.info.VolumeSerialNumber == other.info.VolumeSerialNumber && identity.info.FileIndexHigh == other.info.FileIndexHigh && identity.info.FileIndexLow == other.info.FileIndexLow, nil
}

func (identity *stagingIdentity) close() error { return windows.CloseHandle(identity.handle) }

const moveFileWriteThrough = 0x8
