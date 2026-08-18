//go:build windows

package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
	"golang.org/x/sys/windows"
)

func TestCreateSQLiteBackupAcceptsPermissiveWindowsParent(t *testing.T) {
	root := t.TempDir()
	makeWindowsParentPermissive(t, root)
	database, backup := filepath.Join(root, "memory.db"), filepath.Join(root, "backup.sqlite")
	store, err := Open(context.Background(), database, nil)
	testutil.NoError(t, err)
	testutil.NoError(t, store.Close())
	testutil.NoError(t, CreateSQLiteBackup(context.Background(), database, backup))
	output, err := os.Open(backup)
	testutil.NoError(t, err)
	defer output.Close()
	testutil.NoError(t, verifyPrivateSQLiteBackupOutput(output))
}

func TestVerifyPrivateSQLiteBackupOutputRejectsTamperedDACL(t *testing.T) {
	file, err := openPrivateSQLiteBackupOutput(filepath.Join(t.TempDir(), "backup.sqlite"))
	testutil.NoError(t, err)
	defer file.Close()
	testutil.NoError(t, verifyPrivateSQLiteBackupOutput(file))
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	testutil.NoError(t, err)
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_GENERIC_READ,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(world),
		},
	}}, nil)
	testutil.NoError(t, err)
	testutil.NoError(t, windows.SetSecurityInfo(
		windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	))
	testutil.Require(t, errors.Is(verifyPrivateSQLiteBackupOutput(file), ErrCorrupt), "tampered DACL accepted")
}

func makeWindowsParentPermissive(t *testing.T, path string) {
	t.Helper()
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	testutil.NoError(t, err)
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(world),
		},
	}}, nil)
	testutil.NoError(t, err)
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path), windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0,
	)
	testutil.NoError(t, err)
	defer windows.CloseHandle(handle)
	testutil.NoError(t, windows.SetSecurityInfo(
		handle, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	))
}
