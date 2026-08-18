//go:build windows

package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/vgxness/vgxness/internal/testutil"
	"golang.org/x/sys/windows"
)

func TestCreateSQLiteBackupAcceptsPermissiveWindowsParent(t *testing.T) {
	root := createPermissiveWindowsParent(t)
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

func createPermissiveWindowsParent(t *testing.T) string {
	t.Helper()
	user, err := currentProcessUserSID()
	testutil.NoError(t, err)
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;OICI;GA;;;WD)(A;OICI;GA;;;%s)", user.String()))
	testutil.NoError(t, err)
	path := filepath.Join(t.TempDir(), "permissive")
	testutil.NoError(t, windows.CreateDirectory(
		windows.StringToUTF16Ptr(path),
		&windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor},
	))
	return path
}
