//go:build windows

package memory

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateSQLiteBackupParent(os.FileMode) bool { return true }

func privateSQLiteBackupOutput(info os.FileInfo) bool { return info.Mode().IsRegular() }

const privateSQLiteBackupAccess = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE

func openPrivateSQLiteBackupOutput(destination string) (*os.File, error) {
	user, err := currentProcessUserSID()
	if err != nil {
		return nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;0x%x;;;%s)", uint32(privateSQLiteBackupAccess), user.String()))
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name, uint32(privateSQLiteBackupAccess|windows.WRITE_DAC), windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		&windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor},
		windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), destination)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("backup output handle")
	}
	return file, nil
}

func verifyPrivateSQLiteBackupOutput(file *os.File) error {
	info, err := file.Stat()
	if err != nil || !privateSQLiteBackupOutput(info) {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	user, err := currentProcessUserSID()
	if err != nil {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	descriptor, err := windows.GetSecurityInfo(
		windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil || descriptor == nil {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 || ace.Mask != privateSQLiteBackupAccess {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if aceSID == nil || !aceSID.Equals(user) {
		return fmt.Errorf("%w: backup output", ErrCorrupt)
	}
	return nil
}

func currentProcessUserSID() (*windows.SID, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	if user.User.Sid == nil {
		return nil, fmt.Errorf("current process user SID is unavailable")
	}
	return user.User.Sid.Copy()
}

func syncProjectMarkerDir(string) error { return nil }

func syncProjectMarkerParent(string) error { return nil }

func syncSQLiteBackupDirectory(string) error { return nil }
