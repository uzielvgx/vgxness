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

// publishRootDirectoryNoReplace renames through handles opened beneath root,
// so a replacement of root.Name() cannot redirect the operation.
func publishRootDirectoryNoReplace(root *os.Root, source, destination string) error {
	destinationParent, err := root.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer destinationParent.Close()
	sourceParent, err := root.Open(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer sourceParent.Close()
	handle, err := openForRename(windows.Handle(sourceParent.Fd()), filepath.Base(source))
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
	info.RootDirectory = windows.Handle(destinationParent.Fd())
	info.FileNameLength = uint32(nameLength)
	copy((*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&info.FileName[0]))[:nameLength/2:nameLength/2], name)
	var status windows.IO_STATUS_BLOCK
	err = windows.NtSetInformationFile(handle, &status, &buffer[0], uint32(size), windows.FileRenameInformation)
	runtime.KeepAlive(destinationParent)
	runtime.KeepAlive(sourceParent)
	return err
}

func openForRename(parent windows.Handle, name string) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, windows.DELETE|windows.SYNCHRONIZE, &attributes, &status, nil, windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	runtime.KeepAlive(objectName)
	return handle, err
}
