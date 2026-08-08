//go:build windows

package codex

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type tokenOwner struct {
	owner *windows.SID
}

func safeAncestor(path string, info os.FileInfo) bool {
	return directoryHandle(path, info, false)
}

func ownedDir(path string, info os.FileInfo) bool {
	return directoryHandle(path, info, true)
}

// directoryHandle binds the inspected FileInfo to an open, non-reparse handle
// before inspecting its owner. Default-profile ACL policy is the Windows trust
// boundary; nonstandard grants and same-SID writers are out of scope.
func directoryHandle(path string, want os.FileInfo, requireOwner bool) bool {
	if want == nil || !want.IsDir() {
		return false
	}
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path), windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0,
	)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return false
	}
	defer file.Close()
	var attributes windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &attributes); err != nil || attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(want, opened) {
		return false
	}
	if !requireOwner {
		return true
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil || descriptor == nil {
		return false
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		return false
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	var size uint32
	if err := windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &size); err != windows.ERROR_INSUFFICIENT_BUFFER || size < uint32(unsafe.Sizeof(tokenOwner{})) {
		return false
	}
	buffer := make([]byte, size)
	tokenOwner := (*tokenOwner)(unsafe.Pointer(&buffer[0]))
	if err := windows.GetTokenInformation(token, windows.TokenOwner, &buffer[0], size, &size); err != nil || tokenOwner.owner == nil {
		return false
	}
	equal := owner.Equals(tokenOwner.owner)
	runtime.KeepAlive(buffer)
	return equal
}
