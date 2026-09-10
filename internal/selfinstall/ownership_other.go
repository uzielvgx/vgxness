//go:build !darwin && !linux

package selfinstall

import (
	"io/fs"
	"os"
)

// Platforms other than Linux and macOS retain the existing directory-type check; this does not
// establish an ACL ownership or writability guarantee.
func safeInstallAncestor(info fs.FileInfo) bool {
	return info != nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}
func ownedInstallDirectory(info fs.FileInfo) bool { return safeInstallAncestor(info) }
