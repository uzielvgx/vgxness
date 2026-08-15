//go:build windows

package selfinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type fileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

var reOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

// publishRootDirectoryNoReplace renames through handles opened beneath root,
// so a replacement of root.Name() cannot redirect the operation.
func publishRootDirectoryNoReplace(root *os.Root, source, destination string) error {
	parent, err := root.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	sourceFile, err := root.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	handle, err := reopenForRename(windows.Handle(sourceFile.Fd()))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	name, err := windows.UTF16FromString(filepath.Base(destination))
	if err != nil {
		return err
	}
	nameLength := len(name)*2 - 2
	var dummy fileRenameInformation
	size := int(unsafe.Offsetof(dummy.FileName)) + nameLength
	buffer := make([]byte, size)
	info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = windows.Handle(parent.Fd())
	info.FileNameLength = uint32(nameLength)
	copy((*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&info.FileName[0]))[:nameLength/2:nameLength/2], name)
	var status windows.IO_STATUS_BLOCK
	err = windows.NtSetInformationFile(handle, &status, &buffer[0], uint32(size), windows.FileRenameInformation)
	runtime.KeepAlive(parent)
	runtime.KeepAlive(sourceFile)
	return err
}

func reopenForRename(handle windows.Handle) (windows.Handle, error) {
	value, _, err := reOpenFile.Call(uintptr(handle), uintptr(windows.DELETE|windows.SYNCHRONIZE), uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE), uintptr(windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT))
	if value == 0 {
		return 0, err
	}
	return windows.Handle(value), nil
}
